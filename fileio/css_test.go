package fileio

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)

// secretBox returns the declarations of the rule for the box a secret is shown
// in, with comments stripped: the comments explain which values are forbidden
// and would otherwise match the checks below.
func secretBox(t *testing.T) string {
	t.Helper()
	css := cssComment.ReplaceAllString(string(CustomCss), "")
	re := regexp.MustCompile(`(?s)pre\.secret \{(.*?)\}`)
	m := re.FindStringSubmatch(css)
	if m == nil {
		t.Fatal("no pre.secret rule in the stylesheet")
	}
	return m[1]
}

// user-select: all makes a single click select the whole node, and then a part
// of the secret — one line of a config, the password out of a URL — cannot be
// picked out at all. Copying everything is what the button is for, so this
// must not come back.
func TestSecretBoxAllowsPartialSelection(t *testing.T) {
	rule := secretBox(t)
	if strings.Contains(rule, "user-select: all") {
		t.Error("pre.secret uses user-select: all, partial selection is impossible")
	}
	if !strings.Contains(rule, "user-select: text") {
		t.Error("pre.secret does not state user-select: text")
	}
}

// The box should follow the viewport rather than a fixed pixel height: on a
// large screen a long secret is meant to be visible without scrolling.
func TestSecretBoxGrowsWithTheViewport(t *testing.T) {
	rule := secretBox(t)
	re := regexp.MustCompile(`max-height:\s*\d+vh`)
	if !re.MatchString(rule) {
		t.Errorf("pre.secret has no viewport-relative max-height: %s", rule)
	}
}

// A wider card on a wide screen, and only there: the rule has to sit inside a
// min-width query, or a phone gets it too. Checked by walking the braces
// rather than by a regexp — a regexp with .*? happily steps over a closing
// brace and calls a rule "inside" a block it has already left.
func TestWideScreensGetAWiderCard(t *testing.T) {
	prelude, ok := enclosingBlock(t, "body:has(.secret) .card")
	if !ok {
		t.Fatal("no wide-card rule for the page that shows a secret")
	}
	if !regexp.MustCompile(`@media \(min-width: \d+px\)`).MatchString(prelude) {
		t.Errorf("the wide-card rule sits in %q, not in a min-width query", prelude)
	}
}

// enclosingBlock returns the prelude of the innermost block containing needle.
func enclosingBlock(t *testing.T, needle string) (string, bool) {
	t.Helper()
	css := cssComment.ReplaceAllString(string(CustomCss), "")
	at := strings.Index(css, needle)
	if at < 0 {
		return "", false
	}
	var open []int // indices of the '{' of every block still open
	for i := 0; i < at; i++ {
		switch css[i] {
		case '{':
			open = append(open, i)
		case '}':
			if len(open) > 0 {
				open = open[:len(open)-1]
			}
		}
	}
	if len(open) == 0 {
		return "", true // at the top level: no enclosing block at all
	}
	start := open[len(open)-1]
	prelude := css[:start]
	if nl := strings.LastIndexByte(prelude, '\n'); nl >= 0 {
		prelude = prelude[nl+1:]
	}
	return strings.TrimSpace(prelude), true
}

// The comments explain which values are wrong; the checks must not read them
// as the values in force.
func TestSecretBoxChecksIgnoreComments(t *testing.T) {
	if strings.Contains(secretBox(t), "/*") {
		t.Error("comments are not stripped from the rule under test")
	}
}

// The copy button sits next to a box that is now most of the screen tall. The
// row stretches its children, so without this the button becomes a slab the
// height of the secret.
func TestCopyButtonDoesNotStretchNextToTheSecret(t *testing.T) {
	css := cssComment.ReplaceAllString(string(CustomCss), "")
	re := regexp.MustCompile(`\.copyrow[^{]*\.secret[^{]*\.btn \{[^}]*align-self:\s*flex-start`)
	if !re.MatchString(css) {
		t.Error("no align-self for the copy button next to the secret")
	}
}

// Below 16px iOS Safari zooms the page when the link field is focused, and on
// a zoomed page selection handles stop landing where they were aimed — which
// is the whole point of the change this test guards.
func TestMonoTextIsBigEnoughNotToZoomIOS(t *testing.T) {
	css := cssComment.ReplaceAllString(string(CustomCss), "")
	// .mono appears in more than one rule (the font family is set elsewhere),
	// so every rule that sizes it has to be checked, not just the first one.
	blocks := regexp.MustCompile(`(?s)\.mono[^{]*\{(.*?)\}`).FindAllStringSubmatch(css, -1)
	sized := 0
	for _, b := range blocks {
		size := regexp.MustCompile(`font-size:\s*([0-9.]+)px`).FindStringSubmatch(b[1])
		if size == nil {
			continue
		}
		sized++
		px, err := strconv.ParseFloat(size[1], 64)
		if err != nil {
			t.Fatalf("unparseable font-size %q", size[1])
		}
		if px < 16 {
			t.Errorf("font-size is %gpx, below the 16px iOS zoom threshold", px)
		}
	}
	if sized == 0 {
		t.Fatal("nothing sizes .mono text")
	}
}
