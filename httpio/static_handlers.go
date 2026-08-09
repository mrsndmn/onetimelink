package httpio

import (
	"bytes"
	"net/http"
	"time"

	"github.com/sstark/gjfy/fileio"
	"github.com/sstark/gjfy/misc"
)

// AssetProvider returns a reloadable asset and the time it was last read.
// It is a function because assets are replaced on SIGHUP while requests are
// in flight; reading a shared pointer instead would be a data race.
type AssetProvider func() ([]byte, time.Time)

func HandleStaticFav() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/x-icon")
		w.WriteHeader(http.StatusOK)
		w.Write(fileio.Favicon)
	})
}

func HandleStaticCss(get AssetProvider) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		css, updated := get()
		http.ServeContent(w, r, fileio.CssFileName, updated, bytes.NewReader(css))
	})
}

// HandleStaticJs serves the copy-button script. It is embedded rather than
// reloadable: it is not styling, and a script served next to a secret should
// only ever be the one that was compiled in.
func HandleStaticJs(started time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, fileio.JsFileName, started, bytes.NewReader(fileio.AppJs))
	})
}

func HandleStaticClientShellScript(urlbase string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-sh")
		w.WriteHeader(http.StatusOK)
		misc.ClientShellScript(w, urlbase+ApiNew)
	})
}
