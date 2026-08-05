package misc

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("the secret"))
	}), false)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/g?id=abc", nil))

	// A page displaying a one-time secret must not be cached by the browser
	// or any intermediate proxy.
	cc := rr.Header().Get("Cache-Control")
	if !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control is %q, wanted it to contain no-store", cc)
	}
	// The secret id sits in the query string, so it must not leak via Referer.
	if got := rr.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy is %q, wanted no-referrer", got)
	}
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	} {
		if got := rr.Header().Get(header); got != want {
			t.Errorf("%s is %q, wanted %q", header, got, want)
		}
	}
	if rr.Header().Get("Content-Security-Policy") == "" {
		t.Error("Content-Security-Policy is missing")
	}
	if rr.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS must not be sent over plain HTTP")
	}
}

func TestSecurityHeadersHSTSOnTLS(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), true)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	if rr.Header().Get("Strict-Transport-Security") == "" {
		t.Error("HSTS is missing when running with TLS")
	}
}

func TestGetRealIPRejectsGarbage(t *testing.T) {
	tests := map[string]struct {
		header, value, want string
	}{
		"forwarded-for list takes the first entry": {"X-Forwarded-For", "1.2.3.4, 10.0.0.1", "1.2.3.4"},
		"non-ip forwarded-for":                     {"X-Forwarded-For", "not an ip; rm -rf /", "invalid"},
		"non-ip real-ip":                           {"X-Real-IP", "<script>", "invalid"},
		"ipv6 is fine":                             {"X-Real-IP", "2001:db8::1", "2001:db8::1"},
	}
	for label, tc := range tests {
		t.Run(label, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set(tc.header, tc.value)
			if got := GetRealIP(req); got != tc.want {
				t.Errorf("got %q, wanted %q", got, tc.want)
			}
		})
	}
}
