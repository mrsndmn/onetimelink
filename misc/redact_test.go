package misc

import (
	"strings"
	"testing"
)

func TestRedactID(t *testing.T) {
	const id = "qNML_qd24WV0X4ON-6ufsLz0_IyY3f4xuMIADlbLKXE"
	got := RedactID(id)
	if strings.Contains(got, id) {
		t.Errorf("full id survived redaction: %v", got)
	}
	if !strings.HasPrefix(got, "qNML_q") {
		t.Errorf("prefix for log correlation is missing: %v", got)
	}
	if len(got) >= len(id) {
		t.Errorf("redacted form is not shorter: %v", got)
	}
	if RedactID("") != "" {
		t.Error("empty id should stay empty")
	}
	if RedactID("short") != "REDACTED" {
		t.Errorf("short ids must be fully redacted, got %v", RedactID("short"))
	}
}

func TestRedactPath(t *testing.T) {
	tests := map[string]struct{ in, wantNotContain, wantPrefix string }{
		"api get path carries the id": {
			in:             "/api/v1/get/qNML_qd24WV0X4ON-6ufsLz0_IyY3f4xuMIADlbLKXE",
			wantNotContain: "qNML_qd24WV0X4ON-6ufsLz0_IyY3f4xuMIADlbLKXE",
			wantPrefix:     "/api/v1/get/",
		},
		"ordinary path is left alone": {
			in:             "/g",
			wantNotContain: "REDACTED",
			wantPrefix:     "/g",
		},
	}
	for label, tc := range tests {
		t.Run(label, func(t *testing.T) {
			got := RedactPath(tc.in)
			if strings.Contains(got, tc.wantNotContain) {
				t.Errorf("got %v, should not contain %v", got, tc.wantNotContain)
			}
			if !strings.HasPrefix(got, tc.wantPrefix) {
				t.Errorf("got %v, wanted prefix %v", got, tc.wantPrefix)
			}
		})
	}
}

func TestSanitizeForLog(t *testing.T) {
	got := SanitizeForLog("curl/8.0\nBcc: attacker@example.org\r\n", 200)
	if strings.ContainsAny(got, "\n\r") {
		t.Errorf("newlines survived sanitising: %q", got)
	}
	if strings.Contains(SanitizeForLog("a\x00b", 200), "\x00") {
		t.Error("NUL byte survived sanitising")
	}
	if len(SanitizeForLog(strings.Repeat("x", 500), 100)) > 100 {
		t.Error("value was not truncated")
	}
}
