package httpio

import (
	"fmt"
	"html/template"
	"log"
	"net/http"

	"github.com/sstark/gjfy/fileio"
	"github.com/sstark/gjfy/misc"
	"github.com/sstark/gjfy/store"
)

// MessageProvider returns the current user message shown above a secret.
type MessageProvider func() string

type viewInfoEntry struct {
	store.StoreEntryInfo
	UserMessageView string
}

var htmlTemplates *template.Template

func init() {
	htmlTemplates = template.Must(template.ParseFS(fileio.HtmlTemplates, "*.tmpl"))
}

func HandleIndex(fAllowAnonymous bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			w.WriteHeader(http.StatusNotFound)
			htmlTemplates.ExecuteTemplate(w, "error", nil)
			return
		}
		w.WriteHeader(http.StatusOK)
		type Data struct {
			AllowAnonymous bool
		}
		htmlTemplates.ExecuteTemplate(w, "index", &Data{AllowAnonymous: fAllowAnonymous})
	})
}

func HandleGet(memstore *store.SecretStore, urlbase string, getMessage MessageProvider) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		entry, ok := memstore.Claim(id, urlbase, Get, ApiGet)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			log.Printf("entry not found: %s", misc.RedactID(id))
			htmlTemplates.ExecuteTemplate(w, "error", nil)
			return
		}
		w.WriteHeader(http.StatusOK)
		htmlTemplates.ExecuteTemplate(w, "view", viewInfoEntry{entry, getMessage()})
	})
}

// HandleInfo shows metadata about a secret.
//
// It requires a valid auth token: the metadata reveals who created the secret
// and whether it has been read yet, and unlike the view handler it does not
// consume a click, so leaving it open lets anyone who intercepts a link probe
// it without the recipient ever noticing.
func HandleInfo(memstore *store.SecretStore, urlbase string, getAuth AuthProvider) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !getAuth().Knows(requestToken(r)) {
			w.WriteHeader(http.StatusUnauthorized)
			htmlTemplates.ExecuteTemplate(w, "error", nil)
			return
		}
		id := r.URL.Query().Get("id")
		// Metadata only: the secret itself is never rendered here.
		entry, ok := memstore.GetEntryInfoHidden(id, urlbase, Get, ApiGet)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			htmlTemplates.ExecuteTemplate(w, "error", nil)
			return
		}
		w.WriteHeader(http.StatusOK)
		htmlTemplates.ExecuteTemplate(w, "info", entry)
	})
}

// requestToken pulls the auth token out of a request, preferring the header
// so it does not end up in access logs.
func requestToken(r *http.Request) string {
	if t := r.Header.Get("X-Auth-Token"); t != "" {
		return t
	}
	return r.URL.Query().Get("token")
}

func HandleCreate(memstore *store.SecretStore, urlbase string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxData)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "could not read form", http.StatusBadRequest)
			return
		}
		id, err := memstore.NewEntry(r.Form.Get("secret"), 1, 0, "anonymous", "")
		if err != nil {
			log.Printf("could not store anonymous secret: %s", err)
			http.Error(w, err.Error(), statusForStoreError(err))
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(fmt.Appendf([]byte{}, "%s%s?id=%s", urlbase, Get, id))
	})
}
