package httpio

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
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
	h := HandleGet(st, urlbase, noMessage)

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
	HandleGet(st, urlbase, noMessage).ServeHTTP(rr, httptest.NewRequest("GET", "/g?id=xss", nil))

	if strings.Contains(rr.Body.String(), "<script>") {
		t.Errorf("secret was not escaped:\n%s", rr.Body.String())
	}
}

func TestHandleGetMissing(t *testing.T) {
	st := store.New(0)
	rr := httptest.NewRecorder()
	HandleGet(st, urlbase, noMessage).ServeHTTP(rr, httptest.NewRequest("GET", "/g?id=nope", nil))
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

	// A browser gets the page with the link on it; anything else keeps the
	// bare URL it has always received.
	t.Run("answers a browser with html", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, browserPost("secret=for-a-browser&clicks=3&days=2"))
		if rr.Code != http.StatusOK {
			t.Fatalf("got status %v: %s", rr.Code, rr.Body.String())
		}
		body := rr.Body.String()
		if !strings.Contains(body, "<!DOCTYPE html>") {
			t.Error("browser did not get an html page")
		}
		if !strings.Contains(body, urlbase+"/g?id=") {
			t.Errorf("the link is missing from the page:\n%s", body)
		}
		// The secret itself has no business being echoed back.
		if strings.Contains(body, "for-a-browser") {
			t.Error("the created page repeats the secret")
		}
	})

	t.Run("honours clicks and validity from the form", func(t *testing.T) {
		st := store.New(0)
		rr := httptest.NewRecorder()
		HandleCreate(st, urlbase).ServeHTTP(rr, browserPost("secret=s&clicks=3&days=2"))
		e := createdEntry(t, st, rr)
		if e.MaxClicks != 3 || e.ValidFor != 2 {
			t.Errorf("got max_clicks=%d valid_for=%d, wanted 3 and 2", e.MaxClicks, e.ValidFor)
		}
	})

	// Out of range or garbage values come from a hand-crafted request, not
	// from the dropdowns, and must not be able to park a secret forever or
	// mint a link that never burns.
	t.Run("clamps absurd values back to the defaults", func(t *testing.T) {
		for _, form := range []string{
			"secret=s&clicks=99999&days=99999",
			"secret=s&clicks=-1&days=0",
			"secret=s&clicks=lots&days=forever",
			"secret=s",
		} {
			st := store.New(0)
			rr := httptest.NewRecorder()
			HandleCreate(st, urlbase).ServeHTTP(rr, browserPost(form))
			e := createdEntry(t, st, rr)
			if e.MaxClicks < 1 || e.MaxClicks > formMaxClicks {
				t.Errorf("%q: max_clicks %d is out of range", form, e.MaxClicks)
			}
			if e.ValidFor < 1 || e.ValidFor > formMaxDays {
				t.Errorf("%q: valid_for %d is out of range", form, e.ValidFor)
			}
		}
	})

	t.Run("rejects an oversized secret", func(t *testing.T) {
		st := store.New(0)
		rr := httptest.NewRecorder()
		big := "secret=" + strings.Repeat("x", store.MaxSecretLen+1)
		HandleCreate(st, urlbase).ServeHTTP(rr, browserPost(big))
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("got status %v, wanted 422", rr.Code)
		}
		if st.Len() != 0 {
			t.Error("an oversized secret was stored anyway")
		}
	})

	// The store cap is what keeps a stranger with the URL from eating the
	// server's memory, so a full store has to be an ordinary, explained
	// refusal rather than a crash or a 500.
	t.Run("refuses politely when the store is full", func(t *testing.T) {
		st := store.New(1)
		HandleCreate(st, urlbase).ServeHTTP(httptest.NewRecorder(), browserPost("secret=first"))
		rr := httptest.NewRecorder()
		HandleCreate(st, urlbase).ServeHTTP(rr, browserPost("secret=second"))
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("got status %v, wanted 503", rr.Code)
		}
		if st.Len() != 1 {
			t.Errorf("store holds %d entries, wanted its cap of 1", st.Len())
		}
	})
}

func browserPost(form string) *http.Request {
	req := httptest.NewRequest("POST", "/create", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	return req
}

var idInPage = regexp.MustCompile(`\?id=([A-Za-z0-9_-]+)`)

// createdEntry pulls the entry just created back out of the store, by way of
// the id in the rendered link — nothing else knows it.
func createdEntry(t *testing.T, st *store.SecretStore, rr *httptest.ResponseRecorder) store.StoreEntry {
	t.Helper()
	m := idInPage.FindStringSubmatch(rr.Body.String())
	if m == nil {
		t.Fatalf("no link in the response (status %d):\n%s", rr.Code, rr.Body.String())
	}
	e, ok := st.GetEntry(m[1])
	if !ok {
		t.Fatalf("the store does not know the id it just handed out")
	}
	return e
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
	if strings.Contains(rr.Body.String(), "<form") {
		t.Error("anonymous create form is shown although it is disabled")
	}

	rr2 := httptest.NewRecorder()
	HandleIndex(true).ServeHTTP(rr2, httptest.NewRequest("GET", "/", nil))
	body := rr2.Body.String()
	if !strings.Contains(body, `action="create"`) {
		t.Error("anonymous create form is missing although it is enabled")
	}
	// The instance is usually reverse proxied under a path prefix that the
	// proxy strips. An absolute action would post outside that prefix and
	// hit the proxy's 404 instead of the service.
	if strings.Contains(body, `action="/`) {
		t.Error("the form action is absolute and breaks behind a path prefix")
	}
}
