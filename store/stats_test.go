package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestStatsCountsWhatPassedThrough(t *testing.T) {
	st := New(0)
	mustAdd(t, st, "one", 1, 7, "a@example.org", "id1")
	mustAdd(t, st, "two", 1, 7, "a@example.org", "id2")
	mustAdd(t, st, "three", 1, 7, "a@example.org", "id3")
	if _, ok := st.Claim("id1", "", "/g", "/api/v1/get/"); !ok {
		t.Fatal("claim of a live entry failed")
	}
	// Entry id2 is old enough to be expired by the sweeper.
	e := st.entries["id2"]
	e.DateAdded = time.Now().Add(-8 * 24 * time.Hour)
	st.entries["id2"] = e
	st.expireOnce(time.Now())

	s := st.Stats()
	if s.Live != 1 {
		t.Errorf("live: got %d, wanted 1", s.Live)
	}
	if s.Created != 3 || s.Claimed != 1 || s.Expired != 1 {
		t.Errorf("counters: got created=%d claimed=%d expired=%d, wanted 3/1/1",
			s.Created, s.Claimed, s.Expired)
	}
	if s.MaxEntries != DefaultMaxEntries {
		t.Errorf("max entries: got %d, wanted %d", s.MaxEntries, DefaultMaxEntries)
	}
}

// A claim that does not find anything is not a read and must not be counted:
// the number is used to reason about what the service actually handed out.
func TestStatsDoesNotCountMissedClaims(t *testing.T) {
	st := New(0)
	if _, ok := st.Claim("nope", "", "/g", "/api/v1/get/"); ok {
		t.Fatal("claim of a missing entry reported success")
	}
	if got := st.Stats().Claimed; got != 0 {
		t.Errorf("claimed: got %d, wanted 0", got)
	}
}

// A link with several clicks left stays in the store, and every reveal counts.
func TestStatsCountsEveryReveal(t *testing.T) {
	st := New(0)
	mustAdd(t, st, "multi", 3, 7, "a@example.org", "id1")
	for i := 0; i < 2; i++ {
		if _, ok := st.Claim("id1", "", "/g", "/api/v1/get/"); !ok {
			t.Fatalf("claim %d failed", i)
		}
	}
	s := st.Stats()
	if s.Live != 1 || s.Claimed != 2 {
		t.Errorf("got live=%d claimed=%d, wanted 1/2", s.Live, s.Claimed)
	}
	if s.Entries[0].Clicks != 2 || s.Entries[0].MaxClicks != 3 {
		t.Errorf("clicks: got %d/%d, wanted 2/3", s.Entries[0].Clicks, s.Entries[0].MaxClicks)
	}
}

func TestStatsEntriesAreOldestFirst(t *testing.T) {
	st := New(0)
	mustAdd(t, st, "new", 1, 7, "a@example.org", "id-new")
	mustAdd(t, st, "old", 1, 7, "a@example.org", "id-old")
	e := st.entries["id-old"]
	e.DateAdded = time.Now().Add(-time.Hour)
	st.entries["id-old"] = e

	got := st.Stats().Entries
	if len(got) != 2 {
		t.Fatalf("got %d entries, wanted 2", len(got))
	}
	if !got[0].Created.Before(got[1].Created) {
		t.Errorf("entries are not oldest first: %v", got)
	}
}

func TestStatsEntryCarriesExpiryAndSize(t *testing.T) {
	st := New(0)
	mustAdd(t, st, "0123456789", 1, 3, "a@example.org", "id1")
	e := st.Stats().Entries[0]
	if e.Bytes != 10 {
		t.Errorf("bytes: got %d, wanted 10", e.Bytes)
	}
	want := e.Created.Add(3 * 24 * time.Hour)
	if !e.Expires.Equal(want) {
		t.Errorf("expires: got %v, wanted %v", e.Expires, want)
	}
	if e.Author != "a@example.org" {
		t.Errorf("author: got %q", e.Author)
	}
}

// The report is read by an operator, but a secret is still a secret: neither
// the contents nor the id — the only credential protecting them — may appear
// in it, in any field.
func TestStatsLeaksNeitherSecretNorID(t *testing.T) {
	st := New(0)
	mustAdd(t, st, "hunter2-the-actual-secret", 1, 7, "a@example.org", "the-secret-id")
	blob, err := json.Marshal(st.Stats())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, forbidden := range []string{"hunter2-the-actual-secret", "the-secret-id"} {
		if strings.Contains(string(blob), forbidden) {
			t.Errorf("stats JSON contains %q: %s", forbidden, blob)
		}
	}
}
