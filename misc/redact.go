package misc

import (
	"strings"
	"unicode"
)

// idPrefixLen is how much of a secret id is kept in logs. Enough to correlate
// log lines, far too little to reconstruct the id.
const idPrefixLen = 6

// RedactID shortens a secret id so it can safely appear in a log file.
//
// The id is the only thing protecting a secret: anybody who can read it can
// read the secret. Logging it in full hands every secret to whoever can read
// the server's journal or a shipped log.
func RedactID(id string) string {
	if id == "" {
		return ""
	}
	if len(id) <= idPrefixLen {
		return "REDACTED"
	}
	return id[:idPrefixLen] + "...REDACTED"
}

// RedactPath removes secret ids embedded in a request path, e.g.
// /api/v1/get/<id>. Everything after the known prefix is redacted.
func RedactPath(path string) string {
	const apiGet = "/api/v1/get/"
	if strings.HasPrefix(path, apiGet) {
		return apiGet + RedactID(strings.TrimPrefix(path, apiGet))
	}
	return path
}

// SanitizeForMail strips control characters from client-controlled data before
// it is piped into the mail command, and truncates it to max bytes.
func SanitizeForMail(s string, max int) string {
	if len(s) > max {
		s = s[:max]
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == 0 || unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
