package store

import (
	"strings"
	"testing"
	"time"
)

func mustAdd(t *testing.T, st *SecretStore, secret string, maxclicks, validfor int, at, id string) string {
	t.Helper()
	got, err := st.NewEntry(secret, maxclicks, validfor, at, id)
	if err != nil {
		t.Fatalf("NewEntry(%q) returned error: %v", secret, err)
	}
	return got
}

func TestNewEntryStoresValues(t *testing.T) {
	st := New(0)
	id := mustAdd(t, st, "secret1", 5, 3, "auth@example.org", "id1")
	if id != "id1" {
		t.Errorf("explicit id not honoured: got %v, wanted id1", id)
	}
	e, ok := st.GetEntry("id1")
	if !ok {
		t.Fatal("entry not found after adding")
	}
	if e.Secret != "secret1" || e.MaxClicks != 5 || e.ValidFor != 3 || e.AuthToken != "auth@example.org" {
		t.Errorf("entry stored with wrong values: %+v", e)
	}
	if e.Clicks != 0 {
		t.Errorf("fresh entry should have 0 clicks, got %d", e.Clicks)
	}
	if e.DateAdded.IsZero() {
		t.Error("DateAdded was not set")
	}
}

func TestNewEntryDefaults(t *testing.T) {
	st := New(0)
	id := mustAdd(t, st, "secret", 0, 0, "auth@example.org", "")
	e, _ := st.GetEntry(id)
	if e.MaxClicks != defaultMaxClicks {
		t.Errorf("got max_clicks %d, wanted default %d", e.MaxClicks, defaultMaxClicks)
	}
	if e.ValidFor != defaultValidity {
		t.Errorf("got valid_for %d, wanted default %d", e.ValidFor, defaultValidity)
	}
}

// The id must not be derived from the secret: doing so lets anyone who can
// guess a secret confirm the guess by recomputing the id.
func TestIDsAreRandomNotDerivedFromSecret(t *testing.T) {
	const n = 500
	// Explicit capacity: the default one is smaller than n on purpose.
	st := New(n)
	seen := make(map[string]bool)
	for i := 0; i < n; i++ {
		// Same secret, same everything: the ids must still all differ.
		id := mustAdd(t, st, "identical-secret", 1, 1, "auth@example.org", "")
		if seen[id] {
			t.Fatalf("duplicate id generated: %v", id)
		}
		if len(id) < 40 {
			t.Fatalf("id %q is suspiciously short (%d chars)", id, len(id))
		}
		if strings.Contains(id, "identical-secret") {
			t.Fatalf("id leaks the secret: %v", id)
		}
		seen[id] = true
	}
	if len(seen) != n {
		t.Errorf("got %d distinct ids out of %d", len(seen), n)
	}
}

func TestAddEntryRejectsBadInput(t *testing.T) {
	st := New(0)
	if _, err := st.NewEntry("", 1, 1, "auth@example.org", ""); err != ErrEmptySecret {
		t.Errorf("empty secret: got %v, wanted %v", err, ErrEmptySecret)
	}
	long := strings.Repeat("x", MaxSecretLen+1)
	if _, err := st.NewEntry(long, 1, 1, "auth@example.org", ""); err != ErrSecretTooLong {
		t.Errorf("oversized secret: got %v, wanted %v", err, ErrSecretTooLong)
	}
}

// Without a cap on the number of entries, anybody able to create secrets can
// drive the process out of memory.
func TestStoreCapacity(t *testing.T) {
	st := New(3)
	for i := 0; i < 3; i++ {
		mustAdd(t, st, "secret", 1, 1, "auth@example.org", "")
	}
	if _, err := st.NewEntry("one too many", 1, 1, "auth@example.org", ""); err != ErrStoreFull {
		t.Errorf("got %v, wanted %v", err, ErrStoreFull)
	}
	if st.Len() != 3 {
		t.Errorf("store grew past its cap: %d", st.Len())
	}
}

func TestGetEntryInfo(t *testing.T) {
	st := New(0)
	mustAdd(t, st, "secret", 1, 1, "auth@example.org", "testid")
	out, ok := st.GetEntryInfoHidden("testid", "http://localhost:9154", "/g", "/api/v1/get/")
	if !ok {
		t.Fatal("entry not found")
	}
	if want := "http://localhost:9154/api/v1/get/testid"; out.ApiUrl != want {
		t.Errorf("got %v, wanted %v", out.ApiUrl, want)
	}
	if want := "http://localhost:9154/g?id=testid"; out.Url != want {
		t.Errorf("got %v, wanted %v", out.Url, want)
	}
	if out.Secret != hiddenString {
		t.Errorf("secret was not hidden: got %v", out.Secret)
	}
}

// GetEntryInfo is used by the metadata view and must never consume a click.
func TestGetEntryInfoDoesNotConsumeClick(t *testing.T) {
	st := New(0)
	mustAdd(t, st, "secret", 1, 1, "auth@example.org", "testid")
	for i := 0; i < 5; i++ {
		if _, ok := st.GetEntryInfoHidden("testid", "b", "/g", "/a/"); !ok {
			t.Fatalf("entry disappeared after %d metadata reads", i)
		}
	}
	e, ok := st.GetEntry("testid")
	if !ok {
		t.Fatal("entry gone")
	}
	if e.Clicks != 0 {
		t.Errorf("metadata read consumed %d click(s)", e.Clicks)
	}
}

func TestClaimCountsAndDeletes(t *testing.T) {
	st := New(0)
	const clicks = 2
	mustAdd(t, st, "secret", clicks, 1, "auth@example.org", "testid")
	for i := 1; i <= clicks; i++ {
		si, ok := st.Claim("testid", "b", "/g", "/a/")
		if !ok {
			t.Fatalf("claim %d failed", i)
		}
		if si.Secret != "secret" {
			t.Errorf("claim %d returned wrong secret: %v", i, si.Secret)
		}
		if si.Clicks != i {
			t.Errorf("claim %d reported %d clicks", i, si.Clicks)
		}
	}
	if _, ok := st.Claim("testid", "b", "/g", "/a/"); ok {
		t.Error("entry still claimable after max_clicks was reached")
	}
	if _, ok := st.GetEntry("testid"); ok {
		t.Error("entry was not deleted after its last click")
	}
}

func TestClaimMissing(t *testing.T) {
	st := New(0)
	if _, ok := st.Claim("nope", "b", "/g", "/a/"); ok {
		t.Error("claiming a non-existing entry succeeded")
	}
}

func TestExpiry(t *testing.T) {
	old := expFactor
	expFactor = func(v int) time.Duration { return time.Millisecond * time.Duration(v) }
	defer func() { expFactor = old }()

	st := New(0)
	mustAdd(t, st, "secret", 1, 20, "auth@example.org", "shortlived")
	mustAdd(t, st, "secret", 1, 100000, "auth@example.org", "longlived")

	if n := st.expireOnce(time.Now()); n != 0 {
		t.Errorf("expired %d entries too early", n)
	}
	time.Sleep(50 * time.Millisecond)
	if n := st.expireOnce(time.Now()); n != 1 {
		t.Errorf("expired %d entries, wanted 1", n)
	}
	if _, ok := st.GetEntry("shortlived"); ok {
		t.Error("expired entry is still there")
	}
	if _, ok := st.GetEntry("longlived"); !ok {
		t.Error("unexpired entry was dropped")
	}
}
