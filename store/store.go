package store

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log"
	"sync"
	"time"
)

const (
	hiddenString     = "#HIDDEN#"
	defaultValidity  = 7 // days
	defaultMaxClicks = 1

	// idBytes is the amount of entropy in a secret id. The id is the only
	// thing protecting a secret, so it has to be unguessable.
	idBytes = 32

	// DefaultMaxEntries caps how many secrets are held at once. Without a
	// cap, anyone allowed to create secrets can exhaust the server's memory.
	//
	// The web form is open to everyone who knows the instance URL, so this is
	// the only thing standing between a bored visitor and the server's RAM:
	// 256 entries times MaxSecretLen is 1 MB of secrets, whatever happens.
	DefaultMaxEntries = 256

	// MaxSecretLen caps the size of a single secret, in bytes. A secret is a
	// password or a key, not a file: 4 KB is generous for that and keeps the
	// worst case (a full store) small enough to reason about.
	MaxSecretLen = 4096
)

var (
	expFactor = realExpFactor

	// ErrStoreFull is returned when the store has reached its capacity.
	ErrStoreFull = errors.New("secret store is full")
	// ErrSecretTooLong is returned for oversized secrets.
	ErrSecretTooLong = errors.New("secret too long")
	// ErrEmptySecret is returned when there is nothing to store.
	ErrEmptySecret = errors.New("secret is empty")
)

// In-memory representation of a secret.
type StoreEntry struct {
	Secret    string    `json:"secret"`
	MaxClicks int       `json:"max_clicks"`
	Clicks    int       `json:"clicks"`
	DateAdded time.Time `json:"date_added"`
	ValidFor  int       `json:"valid_for"`
	AuthToken string    `json:"auth_token"`
}

// Secret augmented with computed fields.
type StoreEntryInfo struct {
	StoreEntry
	Id        string `json:"id"`
	PathQuery string `json:"path_query"`
	Url       string `json:"url"`
	ApiUrl    string `json:"api_url"`
}

// SecretStore holds all secrets in memory. Every field is guarded by mu:
// handlers run concurrently in their own goroutines and the expiry loop runs
// in yet another one, so unsynchronised access is a data race that can (and
// does) abort the whole process with "fatal error: concurrent map writes",
// taking every outstanding secret with it.
type SecretStore struct {
	mu         sync.RWMutex
	entries    map[string]StoreEntry
	maxEntries int
}

// New returns an empty store holding at most maxEntries secrets.
// Pass 0 to use DefaultMaxEntries.
func New(maxEntries int) *SecretStore {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	return &SecretStore{
		entries:    make(map[string]StoreEntry),
		maxEntries: maxEntries,
	}
}

// newID returns an unguessable, URL-safe id.
//
// Deriving the id from the secret's content (as earlier versions did) makes it
// possible to confirm a guessed secret by recomputing the hash, so the id is
// drawn from the CSPRNG instead and carries no information about the secret.
func newID() (string, error) {
	b := make([]byte, idBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// AddEntry adds a secret to the store. If id is empty a random one is
// generated. It returns the id under which the secret was stored.
func (st *SecretStore) AddEntry(e StoreEntry, id string) (string, error) {
	if e.Secret == "" {
		return "", ErrEmptySecret
	}
	if len(e.Secret) > MaxSecretLen {
		return "", ErrSecretTooLong
	}
	e.DateAdded = time.Now()
	e.Clicks = 0
	if e.ValidFor <= 0 {
		e.ValidFor = defaultValidity
	}
	if e.MaxClicks <= 0 {
		e.MaxClicks = defaultMaxClicks
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	if id == "" {
		for i := 0; i < 10; i++ {
			candidate, err := newID()
			if err != nil {
				return "", err
			}
			if _, taken := st.entries[candidate]; !taken {
				id = candidate
				break
			}
		}
		if id == "" {
			return "", errors.New("could not generate a free id")
		}
	}
	if _, exists := st.entries[id]; !exists && len(st.entries) >= st.maxEntries {
		return "", ErrStoreFull
	}
	st.entries[id] = e
	return id, nil
}

// NewEntry adds a new secret to the store. Set id to ""
// to have it auto-generated.
func (st *SecretStore) NewEntry(secret string, maxclicks int, validfor int, at string, id string) (string, error) {
	return st.AddEntry(StoreEntry{
		Secret:    secret,
		MaxClicks: maxclicks,
		ValidFor:  validfor,
		AuthToken: at,
	}, id)
}

// GetEntry retrieves a secret from the store without consuming a click.
func (st *SecretStore) GetEntry(id string) (se StoreEntry, ok bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	se, ok = st.entries[id]
	return
}

// Len returns the number of secrets currently held.
func (st *SecretStore) Len() int {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return len(st.entries)
}

// MaxEntries returns the capacity of the store, so that a handler can tell the
// user what the limit is instead of just refusing.
func (st *SecretStore) MaxEntries() int {
	return st.maxEntries
}

// makeInfo augments an entry with computed fields. It does no locking.
func makeInfo(entry StoreEntry, id, urlbase, urlget, urlapiget string) StoreEntryInfo {
	pathQuery := urlget + "?id=" + id
	return StoreEntryInfo{
		StoreEntry: entry,
		Id:         id,
		PathQuery:  pathQuery,
		Url:        urlbase + pathQuery,
		ApiUrl:     urlbase + urlapiget + id,
	}
}

// GetEntryInfo wraps GetEntry and adds some computed fields. It does not
// consume a click.
func (st *SecretStore) GetEntryInfo(id, urlbase, urlget, urlapiget string) (si StoreEntryInfo, ok bool) {
	entry, ok := st.GetEntry(id)
	return makeInfo(entry, id, urlbase, urlget, urlapiget), ok
}

// GetEntryInfoHidden is like GetEntryInfo but blanks out the secret value.
func (st *SecretStore) GetEntryInfoHidden(id, urlbase, urlget, urlapiget string) (si StoreEntryInfo, ok bool) {
	si, ok = st.GetEntryInfo(id, urlbase, urlget, urlapiget)
	si.Secret = hiddenString
	return
}

// Claim atomically fetches a secret and consumes one click, deleting the entry
// when its last click is used up.
//
// Looking the entry up and counting the click has to happen under one lock:
// done separately, several concurrent requests can each read a max_clicks=1
// entry before any of them deletes it, and a one-time link gets served twice.
func (st *SecretStore) Claim(id, urlbase, urlget, urlapiget string) (si StoreEntryInfo, ok bool) {
	st.mu.Lock()
	entry, ok := st.entries[id]
	if !ok {
		st.mu.Unlock()
		return StoreEntryInfo{}, false
	}
	entry.Clicks++
	if entry.Clicks < entry.MaxClicks {
		st.entries[id] = entry
	} else {
		delete(st.entries, id)
	}
	st.mu.Unlock()

	return makeInfo(entry, id, urlbase, urlget, urlapiget), true
}

// realExpFactor will scale the given int value to a certain amount of time
func realExpFactor(v int) time.Duration {
	return time.Hour * 24 * time.Duration(v)
}

// expireOnce removes all entries past their validity and returns how many
// were dropped.
func (st *SecretStore) expireOnce(now time.Time) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	n := 0
	for id, e := range st.entries {
		if now.After(e.DateAdded.Add(expFactor(e.ValidFor))) {
			delete(st.entries, id)
			n++
		}
	}
	return n
}

// Expiry checks for expired entries at regular intervals
func (st *SecretStore) Expiry(interval time.Duration) {
	tck := time.NewTicker(interval)
	defer tck.Stop()
	log.Printf("checking for expiration every %s\n", interval)
	for {
		if n := st.expireOnce(time.Now()); n > 0 {
			log.Printf("expired %d secret(s)\n", n)
		}
		<-tck.C
	}
}
