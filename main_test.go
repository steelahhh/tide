package main

import (
	"bytes"
	"strings"
	"testing"
)

func executeCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestHelpPerCommand(t *testing.T) {
	cases := []struct {
		args    []string
		want    []string
		notWant []string
	}{
		{[]string{"--help"}, []string{"scrobble", "sync", "--edit", "--source"}, nil},
		{[]string{"scrobble", "--help"}, []string{"--tz", "--edit", "--device", "--dry-run"}, []string{"--source", "--yes"}},
		{[]string{"sync", "--help"}, []string{"--source", "--yes", "--device", "--dry-run"}, []string{"--tz", "--edit"}},
		{[]string{"help", "scrobble"}, []string{"--tz", "--edit"}, []string{"--source", "--yes"}},
		{[]string{"help", "sync"}, []string{"--source", "--yes"}, []string{"--tz", "--edit"}},
	}
	for _, tc := range cases {
		out, err := executeCmd(t, tc.args...)
		if err != nil {
			t.Fatalf("%v: Execute() = %v", tc.args, err)
		}
		for _, s := range tc.want {
			if !strings.Contains(out, s) {
				t.Errorf("%v: help output missing %q:\n%s", tc.args, s, out)
			}
		}
		for _, s := range tc.notWant {
			if strings.Contains(out, s) {
				t.Errorf("%v: help output unexpectedly contains %q:\n%s", tc.args, s, out)
			}
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	_, err := executeCmd(t, "bogus")
	if err == nil {
		t.Fatal("Execute(bogus) = nil, want error")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("error = %v, want unknown-command error", err)
	}
}

func TestUnknownFlagShowsUsage(t *testing.T) {
	out, err := executeCmd(t, "scrobble", "--nope")
	if err == nil {
		t.Fatal("Execute(scrobble --nope) = nil, want error")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("error = %v, want unknown-flag error", err)
	}
	if !strings.Contains(out, "Usage:") {
		t.Errorf("want usage on flag error:\n%s", out)
	}
}

func TestUnexpectedArgument(t *testing.T) {
	if _, err := executeCmd(t, "sync", "extra"); err == nil {
		t.Fatal("Execute(sync extra) = nil, want error")
	}
}

func TestRunHelpExitZero(t *testing.T) {
	if code := run([]string{"--help"}); code != 0 {
		t.Fatalf("run(--help) = %d, want 0", code)
	}
}
