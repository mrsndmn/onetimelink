package cmd

import (
	"flag"
	"fmt"
	"os"
)

var (
	Version string
)

const rootUsage = `gjfy is a web service for creating and providing one-time clickable links.

usage:
  gjfy server [flags]   run the web service (see: gjfy server --help)
  gjfy version          print the version
  gjfy help             print this text
`

// Execute dispatches the subcommand.
//
// This used to be built on cobra, which compiled ~12k lines of third party code
// into a service holding secrets in memory, in order to parse six flags. The
// standard library covers it, and the module now has no dependencies at all.
func Execute() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, rootUsage)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "server":
		runServer(os.Args[2:])
	case "version", "--version", "-V":
		fmt.Printf("gjfy %s\n", versionOrDevel())
	case "help", "--help", "-h":
		fmt.Print(rootUsage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		fmt.Fprint(os.Stderr, rootUsage)
		os.Exit(1)
	}
}

func versionOrDevel() string {
	if Version == "" {
		return "(devel)"
	}
	return Version
}

// The helpers below register a flag under both its long and short name, the
// way the previous cobra setup exposed them.

func stringVar(fs *flag.FlagSet, p *string, long, short, value, usage string) {
	fs.StringVar(p, long, value, usage)
	fs.StringVar(p, short, value, usage+" (shorthand)")
}

func boolVar(fs *flag.FlagSet, p *bool, long, short string, value bool, usage string) {
	fs.BoolVar(p, long, value, usage)
	fs.BoolVar(p, short, value, usage+" (shorthand)")
}

func intVar(fs *flag.FlagSet, p *int, long, short string, value int, usage string) {
	fs.IntVar(p, long, value, usage)
	fs.IntVar(p, short, value, usage+" (shorthand)")
}
