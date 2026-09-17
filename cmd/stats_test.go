package cmd

import (
	"bytes"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sstark/gjfy/admin"
	"github.com/sstark/gjfy/store"
)

func sampleReport(now time.Time) admin.Report {
	return admin.Report{
		Live:          2,
		LostOnRestart: 2,
		MaxEntries:    256,
		StartedAt:     now.Add(-50 * time.Hour),
		UptimeSeconds: int64(50 * time.Hour / time.Second),
		Created:       9,
		Claimed:       6,
		Expired:       1,
		Entries: []store.EntryMeta{
			{
				Created:   now.Add(-2 * time.Hour),
				Expires:   now.Add(5 * time.Hour),
				Clicks:    0,
				MaxClicks: 1,
				Bytes:     815,
				Author:    "a@example.org",
			},
			{
				Created:   now.Add(-time.Hour),
				Expires:   now.Add(72 * time.Hour),
				Clicks:    1,
				MaxClicks: 3,
				Bytes:     42,
				Author:    "форма",
			},
		},
	}
}

func TestFormatReportAnswersTheRestartQuestion(t *testing.T) {
	now := time.Now()
	out := formatReport(sampleReport(now), now, false)
	for _, want := range []string{"2 of 256", "a restart would lose all 2", "9 created, 6 read, 1 expired"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// The nearest expiry is the one worth showing: it is the deadline by which a
// link has to be used, and it is not always the oldest entry.
func TestFormatReportShowsTheNearestExpiry(t *testing.T) {
	now := time.Now()
	out := formatReport(sampleReport(now), now, false)
	if !strings.Contains(out, "in 5h 0m") {
		t.Errorf("nearest expiry not shown as 5h:\n%s", out)
	}
}

func TestFormatReportListsEntriesOnlyWhenAsked(t *testing.T) {
	now := time.Now()
	quiet := formatReport(sampleReport(now), now, false)
	if strings.Contains(quiet, "a@example.org") {
		t.Errorf("entries listed without -v:\n%s", quiet)
	}
	loud := formatReport(sampleReport(now), now, true)
	for _, want := range []string{"a@example.org", "форма", "0/1", "1/3", "815"} {
		if !strings.Contains(loud, want) {
			t.Errorf("missing %q in verbose output:\n%s", want, loud)
		}
	}
}

func TestFormatReportOnAnEmptyStore(t *testing.T) {
	now := time.Now()
	rep := admin.Report{MaxEntries: 256, StartedAt: now.Add(-time.Minute), UptimeSeconds: 60}
	out := formatReport(rep, now, true)
	if !strings.Contains(out, "a restart would lose nothing") {
		t.Errorf("empty store not spelled out:\n%s", out)
	}
	if strings.Contains(out, "next expiry") {
		t.Errorf("an empty store has no next expiry:\n%s", out)
	}
}

func TestRestartCostReadsAsASentence(t *testing.T) {
	cases := map[int]string{0: "nothing", 1: "lose it", 5: "all 5"}
	for live, want := range cases {
		if got := restartCost(live); !strings.Contains(got, want) {
			t.Errorf("restartCost(%d) = %q, wanted it to contain %q", live, got, want)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{50 * time.Hour, "2d 2h"},
		{5*time.Hour + 30*time.Minute, "5h 30m"},
		{90 * time.Second, "1m"},
		{-time.Hour, "now"},
	}
	for _, c := range cases {
		if got := humanDuration(c.in); got != c.want {
			t.Errorf("humanDuration(%v) = %q, wanted %q", c.in, got, c.want)
		}
	}
}

// The two failures an operator actually hits should say what to do next.
func TestStatsHint(t *testing.T) {
	if got := statsHint(fs.ErrPermission, "/run/gjfy/stats.sock"); !strings.Contains(got, "root") {
		t.Errorf("permission hint: %q", got)
	}
	if got := statsHint(fs.ErrNotExist, "/run/gjfy/stats.sock"); !strings.Contains(got, "--stats-socket") {
		t.Errorf("missing-socket hint: %q", got)
	}
	if got := statsHint(errUnknown{}, "/x"); got != "" {
		t.Errorf("unknown error should get no hint, got %q", got)
	}
}

type errUnknown struct{}

func (errUnknown) Error() string { return "something else" }

// The stop notice is the only place the cost of a restart is ever recorded:
// afterwards the store is empty and nothing remembers what was in it.
func TestStopNoticeNamesTheCount(t *testing.T) {
	got := stopNotice(syscall.SIGTERM, 3)
	if !strings.Contains(got, "3 live secret(s) will be lost") {
		t.Errorf("count missing from %q", got)
	}
	if !strings.Contains(got, "terminated") {
		t.Errorf("signal missing from %q", got)
	}
	if zero := stopNotice(syscall.SIGTERM, 0); !strings.Contains(zero, "0 live secret(s)") {
		t.Errorf("an empty store should still be stated: %q", zero)
	}
}

// The whole point of the stop path: a SIGTERM has to leave the count in the
// journal, because a moment later there is nothing left to count. Checked
// end to end — the notice is written and the admin socket is taken down.
func TestShutdownOnSignalRecordsTheCost(t *testing.T) {
	st := store.New(0)
	for _, id := range []string{"id1", "id2"} {
		if _, err := st.NewEntry("secret", 1, 7, "a@example.org", id); err != nil {
			t.Fatalf("NewEntry: %v", err)
		}
	}
	dir, err := os.MkdirTemp("", "gjfy")
	if err != nil {
		t.Fatalf("tempdir: %v", err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "s.sock")
	stats, err := admin.Serve(sock, st, time.Now())
	if err != nil {
		t.Fatalf("admin.Serve: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: http.NewServeMux()}
	go srv.Serve(ln)

	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)

	done := shutdownOnSignal(srv, stats, st)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("kill: %v", err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not finish")
	}

	if !strings.Contains(logged.String(), "2 live secret(s) will be lost") {
		t.Errorf("the stop was not recorded: %q", logged.String())
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Errorf("the admin socket outlived the service: %v", err)
	}
}

// Whatever happens during the stop, the signals have to go back to the
// runtime. Left registered, they are swallowed: the process stops reacting to
// SIGTERM and can then only be killed.
func TestShutdownReleasesTheSignals(t *testing.T) {
	sig := make(chan os.Signal, 1)
	released := make(chan struct{})
	srv := &http.Server{Handler: http.NewServeMux()}

	done := shutdownWhen(sig, func() { close(released) }, srv, nil, store.New(0))
	sig <- syscall.SIGTERM
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	select {
	case <-released:
	default:
		t.Error("the signal registration was not handed back")
	}
}
