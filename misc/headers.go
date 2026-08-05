package misc

import (
	"net"
	"net/http"
	"strings"
)

// GetRealIP returns the client address as reported by a reverse proxy.
//
// These headers are supplied by the client and are trivially spoofed unless
// gjfy sits behind a proxy that overwrites them; the value is only used for
// logging and notification text, never for an access decision. It is still
// validated as an IP address so that arbitrary client data does not end up
// verbatim in logs and mails.
func GetRealIP(r *http.Request) string {
	if xFF := r.Header.Get("X-Forwarded-For"); xFF != "" {
		// X-Forwarded-For is a list; the left-most entry is the client.
		first := strings.TrimSpace(strings.Split(xFF, ",")[0])
		if ip := net.ParseIP(first); ip != nil {
			return ip.String()
		}
		return "invalid"
	}
	if xRI := strings.TrimSpace(r.Header.Get("X-Real-IP")); xRI != "" {
		if ip := net.ParseIP(xRI); ip != nil {
			return ip.String()
		}
		return "invalid"
	}
	return "none"
}

// SecurityHeaders sets the response headers that keep a secret from being
// stored or leaked by the browser and intermediate caches.
func SecurityHeaders(h http.Handler, tls bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		head := w.Header()
		// A page showing a one-time secret must never be cached.
		head.Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
		head.Set("Pragma", "no-cache")
		head.Set("Expires", "0")
		head.Set("X-Content-Type-Options", "nosniff")
		head.Set("X-Frame-Options", "DENY")
		// Keep the secret id out of Referer headers sent to third parties.
		head.Set("Referrer-Policy", "no-referrer")
		head.Set("Content-Security-Policy",
			"default-src 'none'; img-src 'self'; style-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		if tls {
			head.Set("Strict-Transport-Security", "max-age=63072000")
		}
		h.ServeHTTP(w, r)
	})
}
