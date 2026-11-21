package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const logUnknown = "#AUDIOSCROBBLER/1.1\n" +
	"#TZ/UNKNOWN\n" +
	"#CLIENT/Rockbox ipod6g $Revision$\n" +
	"Test Artist\tTest Album\tFirst Song\t1\t245\tL\t1782907200\t9f0a1c2e-0000-4000-8000-000000000001\n" +
	"Test Artist\tTest Album\tSkipped Song\t2\t180\tS\t1782907500\t\n" +
	"Test Artist\tTest Album\tSecond Song\t3\t200\tL\t1782907800\n" +
	"Clock Artist\tClock Album\tUnset Clock\t1\t100\tL\t3600\t\n" +
	"this line is malformed\n"

const logUTC = "#AUDIOSCROBBLER/1.1\n" +
	"#TZ/UTC\n" +
	"Test Artist\tTest Album\tFirst Song\t1\t245\tL\t1782907200\t\r\n"

func TestParseLogUnknown(t *testing.T) {
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Fatal(err)
	}
	res, err := parseLog(strings.NewReader(logUnknown), oslo)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Listens) != 2 || res.Skipped != 1 || res.ClockUnset != 1 || res.Malformed != 1 {
		t.Fatalf("got %d listens, skipped %d, clockUnset %d, malformed %d; want 2, 1, 1, 1",
			len(res.Listens), res.Skipped, res.ClockUnset, res.Malformed)
	}
	first := res.Listens[0]
	if first.ListenedAt != 1782900000 {
		t.Errorf("first ListenedAt = %d, want 1782900000", first.ListenedAt)
	}
	if first.MBID != "9f0a1c2e-0000-4000-8000-000000000001" {
		t.Errorf("first MBID = %q", first.MBID)
	}
	if first.DurationSec != 245 {
		t.Errorf("first DurationSec = %d, want 245", first.DurationSec)
	}
	second := res.Listens[1]
	if second.ListenedAt != 1782900600 {
		t.Errorf("second ListenedAt = %d, want 1782900600", second.ListenedAt)
	}
	if second.MBID != "" {
		t.Errorf("second MBID = %q, want empty", second.MBID)
	}
	if second.TrackNumber != "3" {
		t.Errorf("second TrackNumber = %q, want 3", second.TrackNumber)
	}
}

func TestParseLogUTC(t *testing.T) {
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Fatal(err)
	}
	res, err := parseLog(strings.NewReader(logUTC), oslo)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Listens) != 1 {
		t.Fatalf("got %d listens, want 1", len(res.Listens))
	}
	if res.Listens[0].ListenedAt != 1782907200 {
		t.Errorf("ListenedAt = %d, want 1782907200 (unchanged)", res.Listens[0].ListenedAt)
	}
}

func TestParseLogNoTZHeader(t *testing.T) {
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Fatal(err)
	}
	log := "Test Artist\tTest Album\tFirst Song\t1\t245\tL\t1782907200\n"
	res, err := parseLog(strings.NewReader(log), oslo)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Listens) != 1 || res.Listens[0].ListenedAt != 1782900000 {
		t.Fatalf("no #TZ header: got %+v, want one listen at 1782900000", res.Listens)
	}
}

func TestParseLogSkippedBeforeClockUnset(t *testing.T) {
	log := "Test Artist\tTest Album\tSkipped Unset\t1\t100\tS\t3600\n"
	res, err := parseLog(strings.NewReader(log), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 || res.ClockUnset != 0 || len(res.Listens) != 0 {
		t.Fatalf("S with unset clock: skipped %d, clockUnset %d, listens %d; want 1, 0, 0",
			res.Skipped, res.ClockUnset, len(res.Listens))
	}
}

func TestLocalToUTCSummer(t *testing.T) {
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Fatal(err)
	}
	if got := localToUTC(1782907200, oslo); got != 1782900000 {
		t.Fatalf("localToUTC summer = %d, want 1782900000", got)
	}
}

func TestLocalToUTCWinter(t *testing.T) {
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Fatal(err)
	}
	if got := localToUTC(1768478400, oslo); got != 1768474800 {
		t.Fatalf("localToUTC winter = %d, want 1768474800", got)
	}
}

func TestArchiveAndRemove(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, ".scrobbler.log")
	if err := os.WriteFile(logPath, []byte(logUnknown), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(tmp, "state", "tide")
	now := time.Date(2026, 11, 21, 15, 30, 0, 0, time.UTC)
	got, err := archiveAndRemove(logPath, dir, now)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "scrobbler-20261121T153000Z.log")
	if got != want {
		t.Fatalf("archive path = %q, want %q", got, want)
	}
	archived, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(archived, []byte(logUnknown)) {
		t.Fatal("archive bytes differ from original log")
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("original log still exists (err = %v)", err)
	}
}
