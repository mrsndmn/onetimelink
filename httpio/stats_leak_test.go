package httpio

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sstark/gjfy/store"
)

// The operator report names who created a secret, and that has to stay the
// address rather than the token itself: the token creates links, the report
// is a thing people paste into chats and tickets.
//
// Nothing in the store enforces this — the swap happens in tokendb while
// authorising — so the guarantee is checked here, on the real path a secret
// takes from an API call into the report.
func TestStatsNamesTheAddressNotTheToken(t *testing.T) {
	st := store.New(0)
	body := bytes.NewReader([]byte(`{"auth_token": "` + testToken + `", "secret": "sekrit"}`))
	req := httptest.NewRequest("POST", urlbase+ApiNew, body)
	rr := httptest.NewRecorder()
	HandleApiNew(st, urlbase, testAuth(t)).ServeHTTP(rr, req)
	if rr.Code != 201 {
		t.Fatalf("creation failed: %s", rr.Body.String())
	}

	stats := st.Stats()
	if len(stats.Entries) != 1 {
		t.Fatalf("got %d entries, wanted 1", len(stats.Entries))
	}
	if stats.Entries[0].Author != "test@example.org" {
		t.Errorf("author is %q, wanted the address", stats.Entries[0].Author)
	}
	blob, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(blob), testToken) {
		t.Errorf("the report carries the auth token: %s", blob)
	}
	if strings.Contains(string(blob), "sekrit") {
		t.Errorf("the report carries the secret: %s", blob)
	}
}
