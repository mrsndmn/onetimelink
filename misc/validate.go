package misc

import (
	"net/mail"
	"strings"
)

// ValidEmail reports whether addr is a usable mail address.
//
// The address is only an identifier for the account that created a secret;
// gjfy never sends mail. It is still validated so that arbitrary strings from
// auth.db do not end up in the metadata view, and addresses beginning with "-"
// are refused so the field stays safe to pass to an external command should
// one ever be added back.
func ValidEmail(addr string) bool {
	if addr == "" || strings.HasPrefix(addr, "-") {
		return false
	}
	if strings.ContainsAny(addr, " \t\r\n\x00") {
		return false
	}
	_, err := mail.ParseAddress(addr)
	return err == nil
}
