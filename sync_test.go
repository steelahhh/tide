package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRsyncArgsDryRun(t *testing.T) {
	got := rsyncArgs("h:/m", "/V/I/Music", true)
	want := []string{
		"-rt", "--modify-window=2", "--delete",
		"--exclude=/playlists/", "--exclude=.*",
		"--dry-run", "--itemize-changes",
		"h:/m/", "/V/I/Music/",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rsyncArgs dry-run = %v, want %v", got, want)
	}
}

func TestRsyncArgsReal(t *testing.T) {
	got := rsyncArgs("h:/m/", "/V/I/Music/", false)
	want := []string{
		"-rt", "--modify-window=2", "--delete",
		"--exclude=/playlists/", "--exclude=.*",
		"--info=progress2",
		"h:/m/", "/V/I/Music/",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rsyncArgs real = %v, want %v", got, want)
	}
}

func TestFileChanges(t *testing.T) {
	itemize := ">f+++++++++ a/1.flac\n>f.st...... a/2.flac\n*deleting   b/3.flac\ncd+++++++++ c/\n"
	lines, copies, deletes := fileChanges(itemize)
	if copies != 2 || deletes != 1 {
		t.Fatalf("fileChanges = %d copies, %d deletes; want 2, 1", copies, deletes)
	}
	want := []string{">f+++++++++ a/1.flac", ">f.st...... a/2.flac", "*deleting   b/3.flac"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("fileChanges lines = %q, want %q", lines, want)
	}
}

func TestGuardTripped(t *testing.T) {
	cases := []struct {
		deletes, total int
		want           bool
	}{
		{51, 10000, true},
		{11, 100, true},
		{10, 100, false},
		{0, 0, false},
	}
	for _, c := range cases {
		if got := guardTripped(c.deletes, c.total); got != c.want {
			t.Errorf("guardTripped(%d, %d) = %v, want %v", c.deletes, c.total, got, c.want)
		}
	}
}

func TestCountFiles(t *testing.T) {
	tmp := t.TempDir()
	for _, f := range []string{"a.flac", "sub/b.flac"} {
		p := filepath.Join(tmp, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := countFiles(tmp)
	if err != nil || got != 2 {
		t.Fatalf("countFiles = %d, %v; want 2", got, err)
	}
	got, err = countFiles(filepath.Join(tmp, "missing"))
	if err != nil || got != 0 {
		t.Fatalf("countFiles(missing) = %d, %v; want 0, nil", got, err)
	}
}
