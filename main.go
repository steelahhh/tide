package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
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

func run(args []string) int {
	root := newRootCmd()
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
		return 0
	}
	code := 1
	if errors.Is(err, errGuard) {
		code = 2
	}
	errs := []error{err}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		errs = joined.Unwrap()
	}
	for _, e := range errs {
		if !errors.Is(e, errGuard) {
			fmt.Fprintf(os.Stderr, "tide: %v\n", e)
		}
	}
	return code
}

func newRootCmd() *cobra.Command {
	var o options
	root := &cobra.Command{
		Use:   "tide",
		Short: "Scrobble a Rockbox iPod to ListenBrainz and mirror the music library onto it",
		Long: "tide scrobbles the Rockbox Last.fm log to ListenBrainz and mirrors the\n" +
			"music library onto the iPod. With no subcommand it scrobbles first, then syncs.",
		Example: "  tide                   scrobble, then sync\n" +
			"  tide scrobble --edit   edit the play log before submitting\n" +
			"  tide sync --dry-run    preview the mirror without changing anything",
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			dev, err := resolveDevice(o.device)
			if err != nil {
				return err
			}
			fmt.Println("device:", dev)
			var errs []error
			if err := runScrobble(dev, &o); err != nil {
				errs = append(errs, err)
			}
			if err := runSync(dev, &o); err != nil {
				errs = append(errs, err)
			}
			return errors.Join(errs...)
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	addCommonFlags(root, &o)
	addScrobbleFlags(root, &o)
	addSyncFlags(root, &o)
	root.AddCommand(newScrobbleCmd(), newSyncCmd())
	return root
}

func newScrobbleCmd() *cobra.Command {
	var o options
	cmd := &cobra.Command{
		Use:   "scrobble",
		Short: "Submit the iPod play log to ListenBrainz",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			dev, err := resolveDevice(o.device)
			if err != nil {
				return err
			}
			fmt.Println("device:", dev)
			return runScrobble(dev, &o)
		},
	}
	addCommonFlags(cmd, &o)
	addScrobbleFlags(cmd, &o)
	return cmd
}

func newSyncCmd() *cobra.Command {
	var o options
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Mirror the music library and playlists onto the iPod",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			dev, err := resolveDevice(o.device)
			if err != nil {
				return err
			}
			fmt.Println("device:", dev)
			return runSync(dev, &o)
		},
	}
	addCommonFlags(cmd, &o)
	addSyncFlags(cmd, &o)
	return cmd
}

func addCommonFlags(cmd *cobra.Command, o *options) {
	cmd.Flags().StringVar(&o.device, "device", "", "iPod mount point (default: auto-detect)")
	cmd.Flags().BoolVar(&o.dryRun, "dry-run", false, "print what would happen; change nothing")
}

func addScrobbleFlags(cmd *cobra.Command, o *options) {
	cmd.Flags().StringVar(&o.tz, "tz", "", "IANA zone the iPod clock is set to (default: host zone)")
	cmd.Flags().BoolVar(&o.edit, "edit", false, "edit the play log in a TUI before submitting")
}

func addSyncFlags(cmd *cobra.Command, o *options) {
	cmd.Flags().StringVar(&o.source, "source", "", "rsync source, e.g. homeserver:/srv/music (default: $TIDE_SOURCE)")
	cmd.Flags().BoolVar(&o.yes, "yes", false, "allow a delete that trips the delete guard")
}
