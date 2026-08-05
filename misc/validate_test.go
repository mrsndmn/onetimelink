package misc

import "testing"

func TestValidEmail(t *testing.T) {
	tests := map[string]bool{
		"test@example.org":    true,
		"a.b+c@example.co.uk": true,
		// Would be read as a flag if the value ever reached a command line.
		"-Xsomething@example.org":  false,
		"--version":                false,
		"":                         false,
		"no-at-sign":               false,
		"with space@example.org":   false,
		"nl@example.org\nBcc: x@y": false,
		"nul@example.org\x00":      false,
	}
	for addr, want := range tests {
		if got := ValidEmail(addr); got != want {
			t.Errorf("ValidEmail(%q) = %v, wanted %v", addr, got, want)
		}
	}
}
