package misc

import (
	"io"
	"log"
	"net/mail"
	"os/exec"
	"strings"
)

// maxMailBody caps the size of a notification body.
const maxMailBody = 4096

// mailSlots bounds how many mail processes may run at once, so a burst of
// clicks cannot fork-bomb the host.
var mailSlots = make(chan struct{}, 4)

// ValidRecipient reports whether addr is a plausible mail address that is safe
// to hand to the mail command as an argument.
//
// An address starting with "-" would be picked up as a command line flag by
// mail(1), so it is rejected outright.
func ValidRecipient(addr string) bool {
	if addr == "" || strings.HasPrefix(addr, "-") {
		return false
	}
	if strings.ContainsAny(addr, " \t\r\n\x00") {
		return false
	}
	if _, err := mail.ParseAddress(addr); err != nil {
		return false
	}
	return true
}

func NotifyMail(to, msg string) {
	if !ValidRecipient(to) {
		log.Printf("refusing to send notification to invalid recipient")
		return
	}
	select {
	case mailSlots <- struct{}{}:
	default:
		log.Println("too many pending notifications, dropping one")
		return
	}
	go func() {
		defer func() { <-mailSlots }()
		SendMail(to, "GJFY notice", msg)
	}()
}

func SendMail(to, subject, msg string) {
	if !ValidRecipient(to) {
		log.Printf("refusing to send mail to invalid recipient")
		return
	}
	// The body carries client-controlled data (User-Agent, proxy headers).
	// Strip control characters so it cannot alter how mail(1) reads stdin.
	msg = SanitizeForMailBody(msg, maxMailBody)

	sendmail := exec.Command("mail", "-s", subject, to)
	stdin, err := sendmail.StdinPipe()
	if err != nil {
		log.Println(err)
		return
	}
	stdout, err := sendmail.StdoutPipe()
	if err != nil {
		log.Println(err)
		return
	}
	if err := sendmail.Start(); err != nil {
		log.Println("could not run mail command:", err)
		return
	}
	if _, err := io.WriteString(stdin, msg+"\n"); err != nil {
		log.Println("could not write mail body:", err)
	}
	stdin.Close()
	io.Copy(io.Discard, stdout)
	if err := sendmail.Wait(); err != nil {
		log.Println("mail command failed:", err)
		return
	}
	log.Printf("sending notification done.\n")
}

// SanitizeForMailBody keeps newlines (the body is multi-line) but removes
// other control characters, and drops lines consisting of a single dot, which
// some mail agents treat as end-of-input.
func SanitizeForMailBody(s string, max int) string {
	if len(s) > max {
		s = s[:max]
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = SanitizeForMail(l, max)
		if strings.TrimSpace(l) == "." || strings.HasPrefix(l, "~") {
			l = " " + l
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}
