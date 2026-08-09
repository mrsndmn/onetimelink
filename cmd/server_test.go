package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sstark/gjfy/fileio"
	"github.com/sstark/gjfy/httpio"
	"github.com/sstark/gjfy/store"
	"github.com/sstark/gjfy/tokendb"
)

const testToken = "testtokentesttoken"

func createAuthDBFile(dir, content string) {
	authDBFile, _ := os.Create(filepath.Join(dir, tokendb.AuthFileName))
	defer authDBFile.Close()
	authDBFile.WriteString(content)
}

func createUserMessageViewFile(dir, content string) {
	umvFile, _ := os.Create(filepath.Join(dir, fileio.UserMessageViewFilename))
	defer umvFile.Close()
	umvFile.WriteString(content)
}

// chdirTemp moves into a fresh directory holding a usable auth.db.
func chdirTemp(t *testing.T) string {
	t.Helper()
	log.SetOutput(io.Discard)
	old, _ := os.Getwd()
	tmpdir, err := os.MkdirTemp("", "gjfy_test")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpdir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.Chdir(old)
		os.RemoveAll(tmpdir)
	})
	return tmpdir
}

func TestUpdateFiles(t *testing.T) {
	tmpdir := chdirTemp(t)
	createAuthDBFile(tmpdir, `[
		{"token": "`+testToken+`", "email": "test@example.org"},
		{"token": "secondtokensecondtoken", "email": "other@example.org"}
	]`)
	auth1, css1, umv1, updated1 := updateFiles()
	if auth1 == nil {
		t.Fatal("auth db did not load")
	}
	time.Sleep(2 * time.Millisecond)
	auth2, css2, umv2, updated2 := updateFiles()
	if !reflect.DeepEqual(auth1, auth2) || !reflect.DeepEqual(css1, css2) ||
		umv1 != umv2 {
		t.Errorf("running updateFiles twice gives differing results")
	}
	if updated1 == updated2 {
		t.Errorf("timestamp did not change between updates")
	}

	createAuthDBFile(tmpdir, `[
		{"token": "`+testToken+`", "email": "test@example.org"},
		{"token": "thirdtokenthirdtoken", "email": "foobar@example.org"}
	]`)
	auth3, _, _, _ := updateFiles()
	if reflect.DeepEqual(auth2, auth3) {
		t.Errorf("auth.db was not updated after changing the file")
	}

	userMessage := "foo bar baz!"
	createUserMessageViewFile(tmpdir, userMessage)
	_, _, umv3, _ := updateFiles()
	if umv3 != userMessage {
		t.Errorf("userMessageView was not updated from file")
	}
}

// Post a secret without a valid token, then add the token to auth.db, reload
// and make sure posting works.
func TestAuthDBUpdatedAtRuntime(t *testing.T) {
	tmpdir := chdirTemp(t)
	createAuthDBFile(tmpdir, `[{"token": "`+testToken+`", "email": "test@example.org"}]`)
	reload()

	st := store.New(0)
	getAuth := func() tokendb.TokenDB { return loadedAssets().auth }
	handler := httpio.HandleApiNew(st, "http://localhost:9154", getAuth)

	post := func(token string) *httptest.ResponseRecorder {
		body := bytes.NewReader([]byte(`{"auth_token": "` + token + `", "secret": "sekrit"}`))
		req, _ := http.NewRequest("POST", "http://localhost:9154"+httpio.ApiNew, body)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}

	if rr := post("sometokensometoken"); rr.Code != http.StatusUnauthorized {
		t.Errorf("got status %v, wanted %v", rr.Code, http.StatusUnauthorized)
	}

	createAuthDBFile(tmpdir, `[
		{"token": "`+testToken+`", "email": "test@example.org"},
		{"token": "sometokensometoken", "email": "other@example.org"}
	]`)
	reload()

	rr := post("sometokensometoken")
	if rr.Code != http.StatusCreated {
		t.Fatalf("got status %v, wanted %v: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}
	var out store.StoreEntryInfo
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if out.AuthToken != "other@example.org" {
		t.Errorf("got auth_token %v, wanted other@example.org", out.AuthToken)
	}
}

// Reloading happens in its own goroutine while requests are being served, so
// swapping the configuration must not race with reading it.
func TestReloadIsConcurrencySafe(t *testing.T) {
	tmpdir := chdirTemp(t)
	createAuthDBFile(tmpdir, `[{"token": "`+testToken+`", "email": "test@example.org"}]`)
	reload()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				reload()
				time.Sleep(time.Microsecond)
			}
		}
	}()

	var readers sync.WaitGroup
	for i := 0; i < 20; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for j := 0; j < 200; j++ {
				a := loadedAssets()
				_ = a.auth
				_ = len(a.css)
				_ = a.userMessageView
				_ = a.updated
			}
		}()
	}
	readers.Wait()
	close(stop)
	wg.Wait()
}

func newTestMux(t *testing.T) http.Handler {
	t.Helper()
	tmpdir := chdirTemp(t)
	createAuthDBFile(tmpdir, `[{"token": "`+testToken+`", "email": "test@example.org"}]`)
	reload()
	return buildMux(store.New(0), "http://localhost:9154")
}

// Earlier versions seeded the store with a hardcoded entry under the id
// "test", so every deployment served a known secret and could be fingerprinted.
func TestNoBuiltInTestSecret(t *testing.T) {
	mux := newTestMux(t)
	for _, path := range []string{"/g?id=test", "/api/v1/get/test"} {
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "127.0.0.1:1111"
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s returned %v, wanted 404 — a built-in secret is present", path, rr.Code)
		}
		if strings.Contains(rr.Body.String(), "gjfy-form-control") &&
			strings.Contains(rr.Body.String(), "value=\"secret\"") {
			t.Errorf("%s served a built-in secret", path)
		}
	}
}

func TestMuxSetsSecurityHeaders(t *testing.T) {
	mux := newTestMux(t)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1111"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if !strings.Contains(rr.Header().Get("Cache-Control"), "no-store") {
		t.Errorf("Cache-Control is %q", rr.Header().Get("Cache-Control"))
	}
	if rr.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("Referrer-Policy is %q", rr.Header().Get("Referrer-Policy"))
	}
}

func TestMuxRateLimitsCreation(t *testing.T) {
	mux := newTestMux(t)
	limited := false
	for i := 0; i < newBurst+5; i++ {
		body := strings.NewReader(`{"auth_token": "` + testToken + `", "secret": "s"}`)
		req := httptest.NewRequest("POST", httpio.ApiNew, body)
		req.RemoteAddr = "127.0.0.2:2222"
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Errorf("creation was never rate limited after %d requests", newBurst+5)
	}
}

// The access log must not carry the id, which is the sole credential for a
// secret.
func TestAccessLogRedactsSecretID(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(io.Discard)

	const id = "qNML_qd24WV0X4ON-6ufsLz0_IyY3f4xuMIADlbLKXE"
	h := Log(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest("GET", "/api/v1/get/"+id, nil)
	req.RemoteAddr = "127.0.0.1:1111"
	h.ServeHTTP(httptest.NewRecorder(), req)

	if strings.Contains(buf.String(), id) {
		t.Errorf("access log contains the full secret id:\n%s", buf.String())
	}
}

// Timeouts are what keep slow or idle connections from holding the server
// open indefinitely.
func TestServerHasTimeouts(t *testing.T) {
	srv := &http.Server{
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
	if srv.ReadTimeout == 0 || srv.WriteTimeout == 0 || srv.IdleTimeout == 0 || srv.ReadHeaderTimeout == 0 {
		t.Error("a timeout is left at zero, which means no timeout at all")
	}
	if srv.MaxHeaderBytes == 0 {
		t.Error("MaxHeaderBytes is unset")
	}
}
