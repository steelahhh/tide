package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	_ "time/tzdata"
)

type options struct {
	device string
	dryRun bool
	tz     string
	source string
	yes    bool
	edit   bool
}

var errGuard = errors.New("delete guard tripped")

func main() {
	os.Exit(run(os.Args[1:]))
}

func newFlagSet(name string, o *options) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&o.device, "device", "", "iPod mount point (default: auto-detect)")
	fs.BoolVar(&o.dryRun, "dry-run", false, "print what would happen; change nothing")
	if name != "sync" {
		fs.StringVar(&o.tz, "tz", "", "IANA zone the iPod clock is set to (default: host zone)")
		fs.BoolVar(&o.edit, "edit", false, "edit the play log in a TUI before submitting")
	}
	if name != "scrobble" {
		fs.StringVar(&o.source, "source", "", "rsync source, e.g. homeserver:/srv/music (default: $TIDE_SOURCE)")
		fs.BoolVar(&o.yes, "yes", false, "allow a delete that trips the delete guard")
	}
	return fs
}

func run(args []string) int {
	name := "tide"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
		if name != "scrobble" && name != "sync" {
			fmt.Fprintf(os.Stderr, "tide: unknown command %q\n", name)
			return 1
		}
	}
	var o options
	fs := newFlagSet(name, &o)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "tide: %v\n", err)
		return 1
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "tide: unexpected argument %q\n", fs.Arg(0))
		return 1
	}
	dev, err := resolveDevice(o.device)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tide: %v\n", err)
		return 1
	}
	fmt.Println("device:", dev)
	switch name {
	case "scrobble":
		return report(runScrobble(dev, &o))
	case "sync":
		return report(runSync(dev, &o))
	}
	code := 0
	if err := runScrobble(dev, &o); err != nil {
		fmt.Fprintf(os.Stderr, "tide: %v\n", err)
		code = 1
	}
	if err := runSync(dev, &o); err != nil {
		if errors.Is(err, errGuard) {
			code = 2
		} else {
			fmt.Fprintf(os.Stderr, "tide: %v\n", err)
			if code == 0 {
				code = 1
			}
		}
	}
	return code
}

func report(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, errGuard) {
		return 2
	}
	fmt.Fprintf(os.Stderr, "tide: %v\n", err)
	return 1
}
