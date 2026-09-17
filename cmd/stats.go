package cmd

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/sstark/gjfy/admin"
	"github.com/sstark/gjfy/store"
)

var (
	fStatsPath    string
	fStatsJSON    bool
	fStatsVerbose bool
)

func statsFlags() *flag.FlagSet {
	// Named fl, not fs: "io/fs" is imported here for its error values.
	fl := flag.NewFlagSet("gjfy stats", flag.ExitOnError)
	fl.Usage = func() {
		fmt.Fprint(fl.Output(), `Report how many secrets the running service holds, and therefore how many a
restart would destroy. Secrets live in memory only, so this is the one place
that number can be read, and it can only be read locally: the socket is
readable by root and the service user, nobody else.

usage: gjfy stats [flags]

flags:
`)
		fl.PrintDefaults()
	}
	stringVar(fl, &fStatsPath, "socket", "s", admin.Path, "Path of the stats socket")
	boolVar(fl, &fStatsJSON, "json", "j", false, "Print the raw report as JSON")
	boolVar(fl, &fStatsVerbose, "verbose", "v", false, "List the live secrets (metadata only, never ids or contents)")
	return fl
}

func runStats(args []string) {
	fl := statsFlags()
	_ = fl.Parse(args)
	if fl.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "unexpected argument %q\n", fl.Arg(0))
		os.Exit(1)
	}

	rep, err := admin.Fetch(fStatsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not read %s: %s\n", fStatsPath, err)
		fmt.Fprint(os.Stderr, statsHint(err, fStatsPath))
		os.Exit(1)
	}

	if fStatsJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
		return
	}
	fmt.Print(formatReport(rep, time.Now(), fStatsVerbose))
}

// statsHint turns the two failures an operator actually hits into the next
// step, instead of leaving them with a bare syscall error.
func statsHint(err error, path string) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "the socket is root-only by design; try again as root\n"
	case errors.Is(err, fs.ErrNotExist):
		return "no socket there: is the service running, and was it started with --stats-socket?\n"
	}
	return ""
}

// formatReport renders the report for a human. Times are shown in the local
// zone of whoever is reading, which on a server is normally UTC.
func formatReport(rep admin.Report, now time.Time, verbose bool) string {
	out := fmt.Sprintf("live secrets:  %d of %d  (%s)\n",
		rep.Live, rep.MaxEntries, restartCost(rep.Live))
	out += fmt.Sprintf("running since: %s, up %s\n",
		rep.StartedAt.Local().Format("2006-01-02 15:04 MST"),
		humanDuration(time.Duration(rep.UptimeSeconds)*time.Second))
	out += fmt.Sprintf("since then:    %d created, %d read, %d expired\n",
		rep.Created, rep.Claimed, rep.Expired)

	if next, ok := nextExpiry(rep.Entries); ok {
		out += fmt.Sprintf("next expiry:   %s (in %s)\n",
			next.Local().Format("2006-01-02 15:04 MST"), humanDuration(next.Sub(now)))
	}
	if !verbose || len(rep.Entries) == 0 {
		return out
	}

	out += "\ncreated           expires           reads  bytes  author\n"
	for _, e := range rep.Entries {
		out += fmt.Sprintf("%s  %s  %5s  %5d  %s\n",
			e.Created.Local().Format("2006-01-02 15:04"),
			e.Expires.Local().Format("2006-01-02 15:04"),
			fmt.Sprintf("%d/%d", e.Clicks, e.MaxClicks),
			e.Bytes, e.Author)
	}
	return out
}

func restartCost(live int) string {
	switch live {
	case 0:
		return "a restart would lose nothing"
	case 1:
		return "a restart would lose it"
	}
	return fmt.Sprintf("a restart would lose all %d", live)
}

func nextExpiry(entries []store.EntryMeta) (time.Time, bool) {
	var next time.Time
	for _, e := range entries {
		if next.IsZero() || e.Expires.Before(next) {
			next = e.Expires
		}
	}
	return next, !next.IsZero()
}

// humanDuration prints a duration the way an operator reads it: two units at
// most, largest first.
func humanDuration(d time.Duration) string {
	if d < 0 {
		return "now"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
