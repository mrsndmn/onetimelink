package httpio

import (
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/sstark/gjfy/fileio"
	"github.com/sstark/gjfy/misc"
	"github.com/sstark/gjfy/store"
)

// Bounds for what the web form may ask for. The API is not restricted this
// way: these exist so that an anonymous visitor cannot park a secret on the
// server for a year, or hand out a link that never burns.
const (
	formMaxClicks   = 10
	formMaxDays     = 30
	formDefaultDays = 7

	// maxFormBytes bounds the request body of the create form. It is well
	// above store.MaxSecretLen because form encoding inflates the payload:
	// every byte of a non-ASCII secret arrives as three percent-escaped ones.
	maxFormBytes = 64 * 1024
)

// MessageProvider returns the current user message shown above a secret.
type MessageProvider func() string

type viewInfoEntry struct {
	store.StoreEntryInfo
	UserMessageView string
	// Remaining is how often the link can still be opened after this view.
	Remaining int
}

// errorPage is the data behind the "this is not available" page.
type errorPage struct {
	Title   string
	Message string
}

var htmlTemplates *template.Template

func init() {
	htmlTemplates = template.Must(template.ParseFS(fileio.HtmlTemplates, "*.tmpl"))
}

// renderError renders the error page. Every failure a visitor can see goes
// through here, so that none of them ever grows a stack trace or a raw store
// error.
func renderError(w http.ResponseWriter, status int, title, message string) {
	w.WriteHeader(status)
	htmlTemplates.ExecuteTemplate(w, "error", errorPage{Title: title, Message: message})
}

func renderGone(w http.ResponseWriter) {
	renderError(w, http.StatusNotFound, "Ссылка недействительна",
		"Она уже открыта, истекла или введена с ошибкой. Попросите отправителя сделать новую.")
}

func HandleIndex(fAllowAnonymous bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			renderError(w, http.StatusNotFound, "Страница не найдена",
				"Такой страницы здесь нет.")
			return
		}
		w.WriteHeader(http.StatusOK)
		type Data struct {
			AllowAnonymous bool
			MaxSecretBytes int
			MaxSecretKB    int
		}
		htmlTemplates.ExecuteTemplate(w, "index", &Data{
			AllowAnonymous: fAllowAnonymous,
			MaxSecretBytes: store.MaxSecretLen,
			MaxSecretKB:    store.MaxSecretLen / 1024,
		})
	})
}

func HandleGet(memstore *store.SecretStore, urlbase string, getMessage MessageProvider) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		entry, ok := memstore.Claim(id, urlbase, Get, ApiGet)
		if !ok {
			log.Printf("entry not found: %s", misc.RedactID(id))
			renderGone(w)
			return
		}
		w.WriteHeader(http.StatusOK)
		htmlTemplates.ExecuteTemplate(w, "view", viewInfoEntry{
			StoreEntryInfo:  entry,
			UserMessageView: getMessage(),
			Remaining:       entry.MaxClicks - entry.Clicks,
		})
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
			renderError(w, http.StatusUnauthorized, "Нет доступа",
				"Для просмотра метаданных нужен токен.")
			return
		}
		id := r.URL.Query().Get("id")
		// Metadata only: the secret itself is never rendered here.
		entry, ok := memstore.GetEntryInfoHidden(id, urlbase, Get, ApiGet)
		if !ok {
			renderGone(w)
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

// HandleCreate takes a secret from the web form and answers with the link.
//
// Anything that is not a browser (curl, a script) gets the bare URL as
// text/plain, the way this endpoint always behaved; a browser gets a page with
// a copy button.
func HandleCreate(memstore *store.SecretStore, urlbase string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			replyCreateError(w, r, http.StatusMethodNotAllowed, "Так нельзя",
				"Секрет создаётся отправкой формы.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			replyCreateError(w, r, http.StatusBadRequest, "Не получилось",
				fmt.Sprintf("Секрет длиннее %d КБ или форма повреждена.", store.MaxSecretLen/1024))
			return
		}
		clicks := formInt(r.Form.Get("clicks"), 1, 1, formMaxClicks)
		days := formInt(r.Form.Get("days"), formDefaultDays, 1, formMaxDays)

		id, err := memstore.NewEntry(r.Form.Get("secret"), clicks, days, "форма", "")
		if err != nil {
			log.Printf("could not store secret from the web form: %s", err)
			status, title, msg := createErrorText(err, memstore.MaxEntries())
			replyCreateError(w, r, status, title, msg)
			return
		}

		link := fmt.Sprintf("%s%s?id=%s", urlbase, Get, id)
		if !wantsHTML(r) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte(link))
			return
		}
		w.WriteHeader(http.StatusOK)
		htmlTemplates.ExecuteTemplate(w, "created", struct {
			Url       string
			MaxClicks int
			ValidFor  int
		}{link, clicks, days})
	})
}

// createErrorText turns a store error into something a visitor can act on.
func createErrorText(err error, maxEntries int) (status int, title, message string) {
	switch {
	case errors.Is(err, store.ErrEmptySecret):
		return http.StatusUnprocessableEntity, "Пустой секрет",
			"Вставьте то, чем хотите поделиться."
	case errors.Is(err, store.ErrSecretTooLong):
		return http.StatusUnprocessableEntity, "Слишком длинный секрет",
			fmt.Sprintf("Один секрет — не больше %d КБ. Файлы через этот сервис не передают.",
				store.MaxSecretLen/1024)
	case errors.Is(err, store.ErrStoreFull):
		return http.StatusServiceUnavailable, "Сервер заполнен",
			fmt.Sprintf("Одновременно здесь живёт не больше %d секретов — так сервис защищён от переполнения памяти. Попробуйте позже.", maxEntries)
	default:
		return http.StatusInternalServerError, "Что-то сломалось",
			"Ссылка не создана. Попробуйте ещё раз."
	}
}

func replyCreateError(w http.ResponseWriter, r *http.Request, status int, title, message string) {
	if !wantsHTML(r) {
		http.Error(w, message, status)
		return
	}
	renderError(w, status, title, message)
}

// wantsHTML reports whether the caller is a browser. Command line clients send
// no Accept header, or */*, and are better served with the plain URL.
func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// formInt reads a number out of the form, falling back to def for anything
// missing, unparsable or out of range. There is nothing useful to tell a
// visitor about a dropdown they did not touch, so this never errors.
func formInt(raw string, def, min, max int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < min || n > max {
		return def
	}
	return n
}
