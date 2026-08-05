package tokendb

import (
	"io"
	"log"
	"testing"

	"github.com/sstark/gjfy/store"
)

const (
	tokenA = "aaaaaaaaaaaaaaaaaaaa"
	tokenB = "bbbbbbbbbbbbbbbbbbbb"
)

var goodDB = []byte(`[
	{"token": "` + tokenA + `", "email": "test@example.org"},
	{"token": "` + tokenB + `", "email": "other@example.org"}
]`)

func TestMakeTokenDB(t *testing.T) {
	log.SetOutput(io.Discard)
	defer log.SetOutput(io.Discard)

	tests := map[string]struct {
		in    []byte
		valid bool
	}{
		"garbage":       {[]byte("bla"), false},
		"well formed":   {goodDB, true},
		"missing token": {[]byte(`[{"email": "a@example.org"}]`), false},
		"missing email": {[]byte(`[{"token": "` + tokenA + `"}]`), false},
		// A token this short is guessable; the whole database is refused.
		"short token": {[]byte(`[{"token": "abc", "email": "a@example.org"}]`), false},
		// An address starting with "-" would reach mail(1) as a flag.
		"flag-like email": {[]byte(`[{"token": "` + tokenA + `", "email": "-Xfoo@example.org"}]`), false},
		"nonsense email":  {[]byte(`[{"token": "` + tokenA + `", "email": "not-an-address"}]`), false},
	}
	for label, tc := range tests {
		t.Run(label, func(t *testing.T) {
			tdb := MakeTokenDB(tc.in)
			if (tdb != nil) != tc.valid {
				t.Errorf("MakeTokenDB returned %v, wanted valid=%v", tdb, tc.valid)
			}
		})
	}
}

func TestFindToken(t *testing.T) {
	log.SetOutput(io.Discard)
	tdb := MakeTokenDB(goodDB)
	if tdb == nil {
		t.Fatal("test database did not load")
	}
	if got := tdb.findToken(tokenB); got != "other@example.org" {
		t.Errorf("got %v, wanted other@example.org", got)
	}
	if got := tdb.findToken("nosuchtokennosuchtoken"); got != "" {
		t.Errorf("unknown token resolved to %v", got)
	}
	if got := tdb.findToken(""); got != "" {
		t.Errorf("empty token resolved to %v", got)
	}
	// A prefix of a real token must not be accepted.
	if got := tdb.findToken(tokenA[:len(tokenA)-1]); got != "" {
		t.Errorf("prefix of a valid token was accepted: %v", got)
	}
}

func TestIsAuthorized(t *testing.T) {
	log.SetOutput(io.Discard)
	tdb := MakeTokenDB(goodDB)
	if tdb == nil {
		t.Fatal("test database did not load")
	}

	t.Run("valid token is swapped for the email", func(t *testing.T) {
		entry := store.StoreEntry{Secret: "s", AuthToken: tokenB}
		if !tdb.IsAuthorized(&entry) {
			t.Fatal("valid token was rejected")
		}
		// The token itself must not end up in the secret store.
		if entry.AuthToken != "other@example.org" {
			t.Errorf("AuthToken is %v, wanted the email address", entry.AuthToken)
		}
	})

	t.Run("unknown token is rejected", func(t *testing.T) {
		entry := store.StoreEntry{Secret: "s", AuthToken: "wrongtokenwrongtoken"}
		if tdb.IsAuthorized(&entry) {
			t.Error("unknown token was accepted")
		}
	})

	t.Run("empty token is rejected", func(t *testing.T) {
		entry := store.StoreEntry{Secret: "s"}
		if tdb.IsAuthorized(&entry) {
			t.Error("empty token was accepted")
		}
	})

	t.Run("nil database rejects everything", func(t *testing.T) {
		var empty TokenDB
		entry := store.StoreEntry{Secret: "s", AuthToken: tokenA}
		if empty.IsAuthorized(&entry) {
			t.Error("a database that failed to load accepted a token")
		}
	})
}

func TestKnows(t *testing.T) {
	log.SetOutput(io.Discard)
	tdb := MakeTokenDB(goodDB)
	if !tdb.Knows(tokenA) {
		t.Error("known token not recognised")
	}
	if tdb.Knows("someothertokenentirely") {
		t.Error("unknown token recognised")
	}
	if tdb.Knows("") {
		t.Error("empty token recognised")
	}
}
