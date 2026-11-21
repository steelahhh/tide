package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Listen struct {
	Artist, Album, Title, TrackNumber string
	DurationSec                       int
	ListenedAt                        int64 // unix seconds, UTC
	MBID                              string
}

type ParseResult struct {
	Listens    []Listen
	Skipped    int // rating S
	ClockUnset int // timestamp before 2002-01-01 UTC, after conversion
	Malformed  int
}

const clockUnsetCutoff = 1009843200 // 2002-01-01 UTC

type lineKind int

const (
	lineSkip       lineKind = iota // header or empty line
	lineMalformed                  // counted, never scrobbled
	lineClockUnset                 // timestamp before 2002, never scrobbled
	lineSkipped                    // rating S
	lineListen                     // rating L, scrobblable
)

func logIsUTC(lines []string) bool {
	utc := false
	for _, line := range lines {
		if strings.HasPrefix(line, "#TZ/") {
			utc = line == "#TZ/UTC"
		}
	}
	return utc
}

// classifyLine classifies one log line, already stripped of its line ending.
// For lineSkipped, lineClockUnset and lineListen it returns the parsed Listen
// with ListenedAt converted to UTC.
func classifyLine(line string, utc bool, loc *time.Location) (lineKind, Listen) {
	if line == "" || strings.HasPrefix(line, "#") {
		return lineSkip, Listen{}
	}
	f := strings.Split(line, "\t")
	if len(f) < 7 || f[0] == "" || f[2] == "" {
		return lineMalformed, Listen{}
	}
	duration, err := strconv.Atoi(f[4])
	if err != nil {
		return lineMalformed, Listen{}
	}
	ts, err := strconv.ParseInt(f[6], 10, 64)
	if err != nil {
		return lineMalformed, Listen{}
	}
	if !utc {
		ts = localToUTC(ts, loc)
	}
	l := Listen{
		Artist:      f[0],
		Album:       f[1],
		Title:       f[2],
		TrackNumber: f[3],
		DurationSec: duration,
		ListenedAt:  ts,
	}
	if len(f) > 7 {
		l.MBID = f[7]
	}
	if f[5] == "S" {
		return lineSkipped, l
	}
	if ts < clockUnsetCutoff {
		return lineClockUnset, l
	}
	return lineListen, l
}

func parseLog(r io.Reader, loc *time.Location) (ParseResult, error) {
	var res ParseResult
	var lines []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		lines = append(lines, strings.TrimSuffix(sc.Text(), "\r"))
	}
	if err := sc.Err(); err != nil {
		return res, err
	}
	utc := logIsUTC(lines)
	for _, line := range lines {
		kind, l := classifyLine(line, utc, loc)
		switch kind {
		case lineMalformed:
			res.Malformed++
		case lineSkipped:
			res.Skipped++
		case lineClockUnset:
			res.ClockUnset++
		case lineListen:
			res.Listens = append(res.Listens, l)
		}
	}
	return res, nil
}

// localToUTC treats ts as a local wall-clock time in loc and returns the real
// unix time.
func localToUTC(ts int64, loc *time.Location) int64 {
	w := time.Unix(ts, 0).UTC()
	return time.Date(w.Year(), w.Month(), w.Day(),
		w.Hour(), w.Minute(), w.Second(), 0, loc).Unix()
}

// archiveAndRemove copies logPath to stateDir/scrobbler-<now>.log, syncs and
// closes the copy, then deletes logPath. Returns the archive path.
func archiveAndRemove(logPath, stateDir string, now time.Time) (string, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", err
	}
	archivePath := filepath.Join(stateDir, "scrobbler-"+now.UTC().Format("20060102T150405")+"Z.log")
	in, err := os.Open(logPath)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(archivePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if err := os.Remove(logPath); err != nil {
		return "", err
	}
	return archivePath, nil
}

func stateDir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "tide")
}

func printCounts(res ParseResult) {
	fmt.Printf("scrobble: %d listens, %d skipped, %d with unset clock, %d malformed\n",
		len(res.Listens), res.Skipped, res.ClockUnset, res.Malformed)
}

func printPreview(res ParseResult) {
	for i, l := range res.Listens {
		if i == 5 {
			break
		}
		fmt.Printf("scrobble: %s - %s (%s)\n", l.Artist, l.Title,
			time.Unix(l.ListenedAt, 0).UTC().Format(time.RFC3339))
	}
}

func runScrobble(device string, o *options) error {
	logPath := filepath.Join(device, ".scrobbler.log")
	if _, err := os.Stat(logPath); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("scrobble: no log, nothing to send")
			return nil
		}
		return err
	}
	loc := time.Local
	if o.tz != "" {
		var err error
		loc, err = time.LoadLocation(o.tz)
		if err != nil {
			return fmt.Errorf("--tz: %w", err)
		}
	}
	if o.edit {
		el, err := readEditLog(logPath, loc)
		if err != nil {
			return err
		}
		saved, err := runEditor(el, loc)
		if err != nil {
			return err
		}
		if !saved {
			fmt.Println("scrobble: edit cancelled, nothing sent")
			return nil
		}
		content := el.render()
		if o.dryRun {
			res, err := parseLog(strings.NewReader(content), loc)
			if err != nil {
				return err
			}
			printCounts(res)
			printPreview(res)
			return nil
		}
		if err := writeEditLog(logPath, content); err != nil {
			return err
		}
	}
	f, err := os.Open(logPath)
	if err != nil {
		return err
	}
	res, err := parseLog(f, loc)
	f.Close()
	if err != nil {
		return err
	}
	printCounts(res)
	if o.dryRun {
		printPreview(res)
		return nil
	}
	if len(res.Listens) == 0 {
		archivePath, err := archiveAndRemove(logPath, stateDir(), time.Now())
		if err != nil {
			return err
		}
		fmt.Println("scrobble: log archived to", archivePath)
		return nil
	}
	token, err := loadToken()
	if err != nil {
		return err
	}
	c := &LBClient{
		BaseURL: listenBrainzBaseURL,
		Token:   token,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
	ctx := context.Background()
	userName, err := c.Validate(ctx)
	if err != nil {
		return err
	}
	if err := c.Submit(ctx, res.Listens); err != nil {
		return err
	}
	fmt.Printf("scrobble: sent %d listens as user %s\n", len(res.Listens), userName)
	archivePath, err := archiveAndRemove(logPath, stateDir(), time.Now())
	if err != nil {
		return err
	}
	fmt.Println("scrobble: log archived to", archivePath)
	return nil
}
