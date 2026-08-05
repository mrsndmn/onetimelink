package httpio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sstark/gjfy/store"
	"github.com/sstark/gjfy/tokendb"
)

const (
	testToken = "testtokentesttoken"
	urlbase   = "http://localhost:9154"
)

func testAuth(t *testing.T) AuthProvider {
	t.Helper()
	log.SetOutput(io.Discard)
	db := tokendb.MakeTokenDB([]byte(`[{"token": "` + testToken + `", "email": "test@example.org"}]`))
	if db == nil {
		t.Fatal("test auth db did not load")
	}
	return func() tokendb.TokenDB { return db }
}

func decode(t *testing.T, body string) store.StoreEntryInfo {
	t.Helper()
	var out store.StoreEntryInfo
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("response is not valid json (%v): %s", err, body)
	}
	return out
}

func TestHandleApiGet(t *testing.T) {
	st := store.New(0)
	if _, err := st.NewEntry("secret", 1, 1, "test@example.org", "testid"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", urlbase+ApiGet+"testid", nil)
	rr := httptest.NewRecorder()
	HandleApiGet(st, urlbase).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got status %v, wanted %v", rr.Code, http.StatusOK)
	}
	got := decode(t, rr.Body.String())
	if got.Secret != "secret" {
		t.Errorf("got secret %q, wanted %q", got.Secret, "secret")
	}
	if got.Id != "testid" {
		t.Errorf("got id %q, wanted testid", got.Id)
	}
	if got.Url != urlbase+"/g?id=testid" {
		t.Errorf("got url %q", got.Url)
	}
	// A one-time secret is gone after being fetched.
	if _, ok := st.GetEntry("testid"); ok {
		t.Error("secret still in the store after being fetched")
	}
}

func TestHandleApiGetNonExisting(t *testing.T) {
	st := store.New(0)
	req := httptest.NewRequest("GET", urlbase+ApiGet+"foo", nil)
	rr := httptest.NewRecorder()
	HandleApiGet(st, urlbase).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("got status %v, wanted %v", rr.Code, http.StatusNotFound)
	}
	if want := "{\"error\":\"not found\"}\n"; rr.Body.String() != want {
		t.Errorf("got body %q, wanted %q", rr.Body.String(), want)
	}
}

func TestHandleApiNew(t *testing.T) {
	st := store.New(0)
	body := bytes.NewReader([]byte(`{"auth_token": "` + testToken + `", "secret": "sekrit", "max_clicks": 3}`))
	req := httptest.NewRequest("POST", urlbase+ApiNew, body)
	rr := httptest.NewRecorder()
	HandleApiNew(st, urlbase, testAuth(t)).ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("got status %v, wanted %v: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}
	got := decode(t, rr.Body.String())
	// The creation response must never echo the secret back.
	if got.Secret == "sekrit" {
		t.Error("creation response contains the plaintext secret")
	}
	if got.MaxClicks != 3 {
		t.Errorf("got max_clicks %d, wanted 3", got.MaxClicks)
	}
	// The token is replaced by the address, so it is not kept in the store.
	if got.AuthToken != "test@example.org" {
		t.Errorf("got auth_token %q, wanted the email", got.AuthToken)
	}
	if strings.Contains(rr.Body.String(), testToken) {
		t.Error("creation response leaks the auth token")
	}
	if got.Id == "" {
		t.Fatal("no id returned")
	}
	stored, ok := st.GetEntry(got.Id)
	if !ok {
		t.Fatal("secret was not stored under the returned id")
	}
	if stored.Secret != "sekrit" {
		t.Errorf("stored secret is %q", stored.Secret)
	}
}

// The id is the only credential for a secret and must not be written to logs.
func TestHandleApiNewDoesNotLogTheID(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(io.Discard)

	st := store.New(0)
	body := bytes.NewReader([]byte(`{"auth_token": "` + testToken + `", "secret": "sekrit"}`))
	req := httptest.NewRequest("POST", urlbase+ApiNew, body)
	rr := httptest.NewRecorder()
	HandleApiNew(st, urlbase, testAuth(t)).ServeHTTP(rr, req)

	got := decode(t, rr.Body.String())
	if got.Id == "" {
		t.Fatal("no id returned")
	}
	if strings.Contains(buf.String(), got.Id) {
		t.Errorf("secret id was written to the log:\n%s", buf.String())
	}
}

func TestHandleApiNewUnauthorized(t *testing.T) {
	st := store.New(0)
	body := bytes.NewReader([]byte(`{"auth_token": "wrongtokenwrongtoken", "secret": "sekrit"}`))
	req := httptest.NewRequest("POST", urlbase+ApiNew, body)
	rr := httptest.NewRecorder()
	HandleApiNew(st, urlbase, testAuth(t)).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("got status %v, wanted %v", rr.Code, http.StatusUnauthorized)
	}
	if st.Len() != 0 {
		t.Error("an unauthorized request created a secret")
	}
}

func TestHandleApiNewMalformed(t *testing.T) {
	st := store.New(0)
	body := bytes.NewReader([]byte(`{"auth_token": "x", "secret": 24, "max_clicks": "baz"}`))
	req := httptest.NewRequest("POST", urlbase+ApiNew, body)
	rr := httptest.NewRecorder()
	HandleApiNew(st, urlbase, testAuth(t)).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("got status %v, wanted %v", rr.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandleApiNewRejectsNonPost(t *testing.T) {
	st := store.New(0)
	req := httptest.NewRequest("GET", urlbase+ApiNew, nil)
	rr := httptest.NewRecorder()
	HandleApiNew(st, urlbase, testAuth(t)).ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("got status %v, wanted %v", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleApiNewStoreFull(t *testing.T) {
	st := store.New(1)
	auth := testAuth(t)
	for i := 0; i < 2; i++ {
		body := bytes.NewReader([]byte(`{"auth_token": "` + testToken + `", "secret": "sekrit"}`))
		req := httptest.NewRequest("POST", urlbase+ApiNew, body)
		rr := httptest.NewRecorder()
		HandleApiNew(st, urlbase, auth).ServeHTTP(rr, req)
		want := http.StatusCreated
		if i == 1 {
			want = http.StatusServiceUnavailable
		}
		if rr.Code != want {
			t.Errorf("request %d: got status %v, wanted %v", i, rr.Code, want)
		}
	}
}

// A body larger than the cap must not be buffered whole.
func TestHandleApiNewOversizedBody(t *testing.T) {
	st := store.New(0)
	huge := `{"auth_token": "` + testToken + `", "secret": "` + strings.Repeat("x", maxData*2) + `"}`
	req := httptest.NewRequest("POST", urlbase+ApiNew, strings.NewReader(huge))
	rr := httptest.NewRecorder()
	HandleApiNew(st, urlbase, testAuth(t)).ServeHTTP(rr, req)
	if rr.Code == http.StatusCreated {
		t.Error("oversized body was accepted")
	}
	if st.Len() != 0 {
		t.Error("oversized body created a secret")
	}
}

type mockFailingResponseWriter struct {
	statusCode int
	headers    http.Header
}

func (m *mockFailingResponseWriter) WriteHeader(_ int) {}

func (m *mockFailingResponseWriter) Header() http.Header {
	if m.headers == nil {
		m.headers = make(http.Header)
	}
	return m.headers
}

func (m *mockFailingResponseWriter) Write(_ []byte) (int, error) {
	return 0, fmt.Errorf("simulated write error")
}

func TestJsonRespond(t *testing.T) {
	t.Run("happy case", func(t *testing.T) {
		rr := httptest.NewRecorder()
		type testContent struct {
			SomeValue string `json:"somevalue"`
		}
		jsonRespond(rr, http.StatusOK, testContent{"foobar"})

		if want := "application/json; charset=UTF-8"; rr.Header().Get("Content-Type") != want {
			t.Errorf("got content type %v, wanted %v", rr.Header().Get("Content-Type"), want)
		}
		if rr.Code != http.StatusOK {
			t.Errorf("got status %v, wanted %v", rr.Code, http.StatusOK)
		}
		if want := "{\"somevalue\":\"foobar\"}\n"; rr.Body.String() != want {
			t.Errorf("got body %q, wanted %q", rr.Body.String(), want)
		}
	})

	t.Run("json encoding error", func(t *testing.T) {
		log.SetOutput(io.Discard)
		rr := httptest.NewRecorder()
		var invalidData chan int
		jsonRespond(rr, http.StatusOK, invalidData)
		if want := `{"error":"internal error"}`; rr.Body.String() != want {
			t.Errorf("got body %q, wanted %q", rr.Body.String(), want)
		}
	})

	t.Run("cannot write", func(t *testing.T) {
		var buf bytes.Buffer
		log.SetOutput(&buf)
		defer log.SetOutput(io.Discard)
		rr := &mockFailingResponseWriter{statusCode: http.StatusOK, headers: make(http.Header)}
		jsonRespond(rr, http.StatusOK, "foo")
		if want := "error writing response: simulated write error\n"; !strings.HasSuffix(buf.String(), want) {
			t.Errorf("got log %q, wanted suffix %q", buf.String(), want)
		}
	})
}
