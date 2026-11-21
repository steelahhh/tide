package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var rsyncVersionRE = regexp.MustCompile(`^rsync\s+version\s+3\.`)

func checkRsync() error {
	out, err := exec.Command("rsync", "--version").Output()
	first := strings.SplitN(string(out), "\n", 2)[0]
	if first == "" && err != nil {
		first = err.Error()
	}
	if !rsyncVersionRE.MatchString(first) {
		return fmt.Errorf("need GNU rsync 3.x (found: %s); install rsync via Nix or Homebrew", first)
	}
	return nil
}

func rsyncArgs(src, dst string, dryRun bool) []string {
	if !strings.HasSuffix(src, "/") {
		src += "/"
	}
	if !strings.HasSuffix(dst, "/") {
		dst += "/"
	}
	args := []string{"-rt", "--modify-window=2", "--delete", "--exclude=/playlists/", "--exclude=.*"}
	if dryRun {
		args = append(args, "--dry-run", "--itemize-changes")
	} else {
		args = append(args, "--info=progress2")
	}
	return append(args, src, dst)
}

func countChanges(itemize string) (copies, deletes int) {
	for _, line := range strings.Split(itemize, "\n") {
		switch {
		case strings.HasPrefix(line, "*deleting"):
			deletes++
		case strings.HasPrefix(line, ">f"):
			copies++
		}
	}
	return copies, deletes
}

func countFiles(dir string) (int, error) {
	n := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			n++
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	return n, err
}

func guardTripped(deletes, total int) bool {
	return deletes > 50 || (total > 0 && deletes*10 > total)
}

func runSync(device string, o *options) error {
	if err := checkRsync(); err != nil {
		return err
	}
	src := o.source
	if src == "" {
		src = os.Getenv("TIDE_SOURCE")
	}
	if src == "" {
		return errors.New("no source: use --source or set TIDE_SOURCE")
	}
	dst := filepath.Join(device, "Music")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	dry := exec.Command("rsync", rsyncArgs(src, dst, true)...)
	var stdout, stderr bytes.Buffer
	dry.Stdout = &stdout
	dry.Stderr = &stderr
	if err := dry.Run(); err != nil {
		return fmt.Errorf("rsync dry run: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	copies, deletes := countChanges(stdout.String())
	total, err := countFiles(dst)
	if err != nil {
		return err
	}
	fmt.Printf("sync: %d to copy, %d to delete\n", copies, deletes)
	if !o.dryRun {
		if guardTripped(deletes, total) && !o.yes {
			fmt.Printf("sync: would delete %d of %d files; rerun with --yes if that is right\n", deletes, total)
			return errGuard
		}

		real := exec.Command("rsync", rsyncArgs(src, dst, false)...)
		real.Stdout = os.Stdout
		real.Stderr = os.Stderr
		if err := real.Run(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) && (ee.ExitCode() == 23 || ee.ExitCode() == 24) {
				return errors.New(`some files were not copied; often a filename FAT32 rejects (: ? " < > | *)`)
			}
			return fmt.Errorf("rsync: %w", err)
		}
	}
	return syncPlaylists(device, src, o.dryRun)
}
