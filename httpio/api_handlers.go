package httpio

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"path"

	"github.com/sstark/gjfy/misc"
	"github.com/sstark/gjfy/store"
	"github.com/sstark/gjfy/tokendb"
)

const (
	maxData = 1048576 // 1MB
)

// AuthProvider returns the currently loaded token database. It is a function
// because the database is reloaded on SIGHUP while requests are in flight.
type AuthProvider func() tokendb.TokenDB

type jsonError struct {
	Error string `json:"error"`
}

func HandleApiGet(memstore *store.SecretStore, urlbase string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := path.Base(r.URL.Path)
		// Fetching and counting the click happen in one atomic step, so a
		// one-time link cannot be served twice by racing requests.
		entry, ok := memstore.Claim(id, urlbase, Get, ApiGet)
		if !ok {
			log.Printf("entry not found: %s", misc.RedactID(id))
			jsonRespond(w, http.StatusNotFound, jsonError{"not found"})
			return
		}
		jsonRespond(w, http.StatusOK, entry)
	})
}

func HandleApiNew(memstore *store.SecretStore, urlbase string, getAuth AuthProvider) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var entry store.StoreEntry

		if r.Method != http.MethodPost {
			jsonRespond(w, http.StatusMethodNotAllowed, jsonError{"method not allowed"})
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxData))
		if err != nil {
			log.Printf("error reading request body: %s", err)
			jsonRespond(w, http.StatusBadRequest, jsonError{"could not read request"})
			return
		}
		defer r.Body.Close()

		if err := json.Unmarshal(body, &entry); err != nil {
			log.Printf("error processing json: %s", err)
			jsonRespond(w, http.StatusUnprocessableEntity, jsonError{err.Error()})
			return
		}
		auth := getAuth()
		if !auth.IsAuthorized(&entry) {
			log.Printf("unauthorized when trying to make new entry")
			jsonRespond(w, http.StatusUnauthorized, jsonError{"unauthorized"})
			return
		}
		id, err := memstore.AddEntry(entry, "")
		if err != nil {
			log.Printf("could not store secret: %s", err)
			jsonRespond(w, statusForStoreError(err), jsonError{err.Error()})
			return
		}
		newEntry, _ := memstore.GetEntryInfoHidden(id, urlbase, Get, ApiGet)
		// The id is deliberately not logged: it is the sole credential for
		// the secret, and logs are read by more people than the recipient.
		jsonRespond(w, http.StatusCreated, newEntry)
	})
}

func statusForStoreError(err error) int {
	switch {
	case errors.Is(err, store.ErrStoreFull):
		return http.StatusServiceUnavailable
	case errors.Is(err, store.ErrSecretTooLong), errors.Is(err, store.ErrEmptySecret):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

func jsonRespond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(status)
	if jerr := json.NewEncoder(w).Encode(data); jerr != nil {
		log.Printf("error encoding json: %s", jerr)
		_, err := w.Write([]byte(`{"error":"internal error"}`))
		if err != nil {
			log.Printf("error writing response: %s", err)
		}
	}
}
