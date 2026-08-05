package httpio

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sstark/gjfy/store"
)

func noMessage() string { return "" }

func TestHandleGetShowsAndConsumes(t *testing.T) {
	st := store.New(0)
	if _, err := st.NewEntry("hunter2", 1, 1, "test@example.org", "theid"); err != nil {
		t.Fatal(err)
	}
	h := HandleGet(st, urlbase, false, noMessage)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/g?id=theid", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %v", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "hunter2") {
		t.Error("secret was not rendered")
	}

	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, httptest.NewRequest("GET", "/g?id=theid", nil))
	if rr2.Code != http.StatusNotFound {
		t.Errorf("second view returned %v, wanted 404", rr2.Code)
	}
	if strings.Contains(rr2.Body.String(), "hunter2") {
		t.Error("a one-time secret was shown twice")
	}
}

// The secret is interpolated into an HTML attribute, so it has to be escaped.
func TestHandleGetEscapesSecret(t *testing.T) {
	st := store.New(0)
	const payload = `"><script>alert(1)</script>`
	if _, err := st.NewEntry(payload, 1, 1, "test@example.org", "xss"); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	HandleGet(st, urlbase, false, noMessage).ServeHTTP(rr, httptest.NewRequest("GET", "/g?id=xss", nil))

	if strings.Contains(rr.Body.String(), "<script>") {
		t.Errorf("secret was not escaped:\n%s", rr.Body.String())
	}
}

func TestHandleGetMissing(t *testing.T) {
	st := store.New(0)
	rr := httptest.NewRecorder()
	HandleGet(st, urlbase, false, noMessage).ServeHTTP(rr, httptest.NewRequest("GET", "/g?id=nope", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("got status %v, wanted 404", rr.Code)
	}
}

// The metadata view reveals who created a secret and whether it has been read,
// without consuming a click. Left open, it lets anyone holding an intercepted
// link probe it silently, so it requires a valid token.
func TestHandleInfoRequiresToken(t *testing.T) {
	log.SetOutput(io.Discard)
	st := store.New(0)
	if _, err := st.NewEntry("secret", 1, 1, "creator@example.org", "theid"); err != nil {
		t.Fatal(err)
	}
	h := HandleInfo(st, urlbase, testAuth(t))

	t.Run("without token", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", "/i?id=theid", nil))
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("got status %v, wanted 401", rr.Code)
		}
		if strings.Contains(rr.Body.String(), "creator@example.org") {
			t.Error("creator address leaked to an unauthenticated caller")
		}
	})

	t.Run("with wrong token", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", "/i?id=theid&token=nopenopenopenope", nil))
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("got status %v, wanted 401", rr.Code)
		}
	})

	t.Run("with valid token in header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/i?id=theid", nil)
		req.Header.Set("X-Auth-Token", testToken)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got status %v, wanted 200", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "creator@example.org") {
			t.Error("metadata is missing for an authorised caller")
		}
	})

	t.Run("with valid token in query", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", "/i?id=theid&token="+url.QueryEscape(testToken), nil))
		if rr.Code != http.StatusOK {
			t.Errorf("got status %v, wanted 200", rr.Code)
		}
	})

	// None of the above may have consumed a click or revealed the secret.
	e, ok := st.GetEntry("theid")
	if !ok {
		t.Fatal("metadata view consumed the secret")
	}
	if e.Clicks != 0 {
		t.Errorf("metadata view consumed %d click(s)", e.Clicks)
	}
}

func TestHandleInfoNeverShowsTheSecret(t *testing.T) {
	st := store.New(0)
	if _, err := st.NewEntry("do-not-render-me", 1, 1, "test@example.org", "theid"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/i?id=theid", nil)
	req.Header.Set("X-Auth-Token", testToken)
	rr := httptest.NewRecorder()
	HandleInfo(st, urlbase, testAuth(t)).ServeHTTP(rr, req)

	if strings.Contains(rr.Body.String(), "do-not-render-me") {
		t.Errorf("metadata view rendered the secret:\n%s", rr.Body.String())
	}
}

func TestHandleCreate(t *testing.T) {
	log.SetOutput(io.Discard)
	st := store.New(0)
	h := HandleCreate(st, urlbase)

	t.Run("creates a secret", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/create", strings.NewReader("secret=from-the-form"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got status %v: %s", rr.Code, rr.Body.String())
		}
		if !strings.HasPrefix(rr.Body.String(), urlbase+"/g?id=") {
			t.Errorf("unexpected body: %s", rr.Body.String())
		}
	})

	t.Run("rejects an empty secret", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/create", strings.NewReader("secret="))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code == http.StatusOK {
			t.Error("empty secret was accepted")
		}
	})

	t.Run("rejects GET", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", "/create", nil))
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("got status %v, wanted 405", rr.Code)
		}
	})
}

// The index handler is registered on "/" and would otherwise answer 200 for
// every unknown path.
func TestHandleIndexUnknownPath(t *testing.T) {
	rr := httptest.NewRecorder()
	HandleIndex(false).ServeHTTP(rr, httptest.NewRequest("GET", "/no/such/page", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("got status %v, wanted 404", rr.Code)
	}
}

func TestHandleIndexAnonymousForm(t *testing.T) {
	rr := httptest.NewRecorder()
	HandleIndex(false).ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(rr.Body.String(), `action="/create"`) {
		t.Error("anonymous create form is shown although it is disabled")
	}

	rr2 := httptest.NewRecorder()
	HandleIndex(true).ServeHTTP(rr2, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(rr2.Body.String(), `action="/create"`) {
		t.Error("anonymous create form is missing although it is enabled")
	}
}
