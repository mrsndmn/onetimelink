package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sstark/gjfy/store"
)

// The report is root-only because it is served on a unix socket and nowhere
// else. If it ever appeared on the public mux, it would be one nginx location
// away from the internet — so check that it is not there.
func TestPublicMuxHasNoStats(t *testing.T) {
	h := buildMux(store.New(0), "http://localhost")
	for _, path := range []string{"/stats", "/stats/", "/admin/stats"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code == http.StatusOK && rr.Body.Len() > 0 && rr.Header().Get("Content-Type") == "application/json; charset=UTF-8" {
			t.Errorf("%s answers with a json report on the public mux", path)
		}
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, wanted 404", path, rr.Code)
		}
	}
}
