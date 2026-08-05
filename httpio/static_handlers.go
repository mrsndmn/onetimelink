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

func HandleStaticLogoSmall() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.Write(fileio.GjfyLogoSmall)
	})
}

func HandleStaticCss(get AssetProvider) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		css, updated := get()
		http.ServeContent(w, r, fileio.CssFileName, updated, bytes.NewReader(css))
	})
}

func HandleStaticLogo(get AssetProvider) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logo, updated := get()
		http.ServeContent(w, r, fileio.LogoFileName, updated, bytes.NewReader(logo))
	})
}

func HandleStaticClientShellScript(urlbase string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-sh")
		w.WriteHeader(http.StatusOK)
		misc.ClientShellScript(w, urlbase+ApiNew)
	})
}
