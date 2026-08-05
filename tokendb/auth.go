package tokendb

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"log"

	"github.com/sstark/gjfy/misc"
	"github.com/sstark/gjfy/store"
)

const (
	AuthFileName = "auth.db"

	// minTokenLen is the shortest auth token accepted. Tokens are the only
	// thing standing between the internet and the ability to create secrets.
	minTokenLen = 16
)

type AuthToken struct {
	Token string `json:"token"`
	Email string `json:"email"`
}

type TokenDB []AuthToken

func MakeTokenDB(b []byte) TokenDB {
	var tokens TokenDB
	err := json.Unmarshal(b, &tokens)
	if err != nil {
		log.Println("error reading auth token db:", err)
		return nil
	}
	for i, entry := range tokens {
		if entry.Token == "" {
			log.Printf("token field empty or missing in entry #%d", i)
			return nil
		}
		if entry.Email == "" {
			log.Printf("email field empty or missing in entry #%d", i)
			return nil
		}
		if !misc.ValidRecipient(entry.Email) {
			// A recipient starting with "-" would be read as a flag by
			// mail(1); anything unparseable is refused as well.
			log.Printf("email field in entry #%d is not a usable address", i)
			return nil
		}
		if len(entry.Token) < minTokenLen {
			log.Printf("token in entry #%d is shorter than %d characters, refusing to load auth db",
				i, minTokenLen)
			return nil
		}
	}
	log.Printf("found %d auth tokens\n", len(tokens))
	return tokens
}

// findToken looks up a token and returns the associated email address.
//
// The comparison runs in constant time and always scans the whole database, so
// that response timing does not reveal whether a guessed token shares a prefix
// with a real one, nor where in the file it sits.
func (db TokenDB) findToken(token string) (email string) {
	want := sha256.Sum256([]byte(token))
	for _, i := range db {
		have := sha256.Sum256([]byte(i.Token))
		if subtle.ConstantTimeCompare(want[:], have[:]) == 1 {
			email = i.Email
		}
	}
	return
}

// IsAuthorized tries to find the auth token given in entry.
// It will then change the entry parameter by replacing the auth
// token with the associated email address. This is to have the
// auth token not end up in the secret database.
func (db TokenDB) IsAuthorized(entry *store.StoreEntry) bool {
	if entry.AuthToken == "" {
		return false
	}
	email := db.findToken(entry.AuthToken)
	if email == "" {
		return false
	}
	entry.AuthToken = email
	return true
}

// Knows reports whether the given token is in the database, without touching a
// store entry. Used to gate the metadata view.
func (db TokenDB) Knows(token string) bool {
	if token == "" {
		return false
	}
	return db.findToken(token) != ""
}
