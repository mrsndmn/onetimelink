package misc

import "testing"

func TestValidRecipient(t *testing.T) {
	tests := map[string]bool{
		"test@example.org":    true,
		"a.b+c@example.co.uk": true,
		// Would be read as a flag by mail(1).
		"-Xsomething@example.org":  false,
		"--version":                false,
		"":                         false,
		"no-at-sign":               false,
		"with space@example.org":   false,
		"nl@example.org\nBcc: x@y": false,
		"nul@example.org\x00":      false,
	}
	for addr, want := range tests {
		if got := ValidRecipient(addr); got != want {
			t.Errorf("ValidRecipient(%q) = %v, wanted %v", addr, got, want)
		}
	}
}

func TestSendMailRefusesBadRecipient(t *testing.T) {
	// Must return without ever executing the mail command. If it did exec,
	// a "-..." recipient would be interpreted as command line flags.
	SendMail("-Xfoo", "subject", "body")
	NotifyMail("-Xfoo", "body")
}
