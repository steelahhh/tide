package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func editLogFrom(t *testing.T, content string) *editLog {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".scrobbler.log")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	el, err := readEditLog(p, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return el
}

func TestReadEditLog(t *testing.T) {
	el := editLogFrom(t, logUnknown)
	if len(el.lines) != 8 || !el.trailingNewline {
		t.Fatalf("got %d lines, trailingNewline %v; want 8, true", len(el.lines), el.trailingNewline)
	}
	wantKinds := []lineKind{lineSkip, lineSkip, lineSkip, lineListen, lineSkipped, lineListen, lineClockUnset, lineMalformed}
	for i, want := range wantKinds {
		if el.lines[i].kind != want {
			t.Errorf("line %d kind = %d, want %d", i, el.lines[i].kind, want)
		}
	}
	if !el.lines[3].included || el.lines[4].included || !el.lines[5].included {
		t.Error("L entries must start included, S entries excluded")
	}
	if el.lines[4].rating != "S" || el.lines[3].rating != "L" {
		t.Error("original ratings not preserved")
	}
	m := newEditModel(el, time.UTC)
	if len(m.entries) != 3 || m.entries[0] != 3 || m.entries[1] != 4 || m.entries[2] != 5 {
		t.Fatalf("entries = %v, want [3 4 5] (hidden lines not editable)", m.entries)
	}
}

func TestReadEditLogEmpty(t *testing.T) {
	for _, content := range []string{"", "\n"} {
		el := editLogFrom(t, content)
		if len(el.lines) != 0 {
			t.Fatalf("content %q: got %d lines, want 0", content, len(el.lines))
		}
		if got := el.render(); got != "" {
			t.Fatalf("content %q: render = %q, want empty", content, got)
		}
	}
}

func TestEditLogRender(t *testing.T) {
	el := editLogFrom(t, logUnknown)
	el.lines[3].included = false // First Song (L) excluded
	el.lines[4].included = true  // Skipped Song (S) included
	got := el.render()
	want := "#AUDIOSCROBBLER/1.1\n" +
		"#TZ/UNKNOWN\n" +
		"#CLIENT/Rockbox ipod6g $Revision$\n" +
		"Test Artist\tTest Album\tSkipped Song\t2\t180\tL\t1782907500\t\n" +
		"Test Artist\tTest Album\tSecond Song\t3\t200\tL\t1782907800\n" +
		"Clock Artist\tClock Album\tUnset Clock\t1\t100\tL\t3600\t\n" +
		"this line is malformed\n"
	if got != want {
		t.Fatalf("render =\n%q\nwant\n%q", got, want)
	}
	res, err := parseLog(strings.NewReader(got), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Listens) != 2 || res.Skipped != 0 || res.ClockUnset != 1 || res.Malformed != 1 {
		t.Fatalf("round trip: %d listens, skipped %d, clockUnset %d, malformed %d; want 2, 0, 1, 1",
			len(res.Listens), res.Skipped, res.ClockUnset, res.Malformed)
	}
	if res.Listens[0].Title != "Skipped Song" || res.Listens[1].Title != "Second Song" {
		t.Fatalf("round trip order wrong: %+v", res.Listens)
	}
}

func TestEditLogRenderCRLFNoTrailingNewline(t *testing.T) {
	content := "#TZ/UTC\r\nA\tB\tT1\t1\t100\tL\t1782907200\r\nA\tB\tT2\t2\t100\tS\t1782907300"
	el := editLogFrom(t, content)
	if el.trailingNewline {
		t.Fatal("trailingNewline = true, want false")
	}
	el.lines[2].included = true
	want := "#TZ/UTC\r\nA\tB\tT1\t1\t100\tL\t1782907200\r\nA\tB\tT2\t2\t100\tL\t1782907300"
	if got := el.render(); got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}

func TestEditLogRenderAllExcluded(t *testing.T) {
	content := "#TZ/UTC\nA\tB\tT1\t1\t100\tL\t1782907200\nA\tB\tT2\t2\t100\tS\t1782907300\n"
	el := editLogFrom(t, content)
	el.lines[1].included = false
	if got := el.render(); got != "#TZ/UTC\n" {
		t.Fatalf("render = %q, want %q", got, "#TZ/UTC\n")
	}
}

func TestPrettify(t *testing.T) {
	tests := []struct {
		artist, title, wantArtist, wantTitle string
	}{
		{"<Untagged>", "cavetown - 16 04 16 (jacks song).mp3", "cavetown", "16 04 16 (jacks song)"},
		{"<Untagged>", "Drop The Silence – Sink or Swim.mp3", "Drop The Silence", "Sink or Swim"},
		{"<Untagged>", "00.mp3", "<Untagged>", "00"},
		{"<Untagged>", "X - Y.FLAC", "X", "Y"},
		{"<Untagged>", "A - B - C.mp3", "A", "B - C"},
		{"Oxis", "Thypter", "Oxis", "Thypter"},
	}
	for _, tt := range tests {
		a, ti := prettify(Listen{Artist: tt.artist, Title: tt.title})
		if a != tt.wantArtist || ti != tt.wantTitle {
			t.Errorf("prettify(%q, %q) = (%q, %q), want (%q, %q)",
				tt.artist, tt.title, a, ti, tt.wantArtist, tt.wantTitle)
		}
	}
}

func TestExpandTemplate(t *testing.T) {
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Fatal(err)
	}
	l := Listen{Artist: "A", Album: "Al", Title: "T", TrackNumber: "3", DurationSec: 245, ListenedAt: 1782900000}
	got := expandTemplate("{artist} / {title} [{album}] #{track} {duration} {time} {rating} {nope}", l, "L", oslo)
	want := "A / T [Al] #3 4:05 2026-07-01 12:00 L {nope}"
	if got != want {
		t.Fatalf("expandTemplate = %q, want %q", got, want)
	}
}

func TestFormatDuration(t *testing.T) {
	for sec, want := range map[int]string{0: "0:00", 60: "1:00", 245: "4:05", 5240: "1:27:20"} {
		if got := formatDuration(sec); got != want {
			t.Errorf("formatDuration(%d) = %q, want %q", sec, got, want)
		}
	}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	case "space":
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+j":
		return tea.KeyMsg{Type: tea.KeyCtrlJ}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func press(m editModel, k string) editModel {
	nm, _ := m.Update(key(k))
	return nm.(editModel)
}

func TestEditModelToggle(t *testing.T) {
	el := editLogFrom(t, logUnknown)
	m := newEditModel(el, time.UTC)
	m = press(m, "space") // First Song off
	m = press(m, "down")
	m = press(m, "space") // Skipped Song on
	if el.lines[3].included || !el.lines[4].included || !el.lines[5].included {
		t.Fatalf("toggle wrong: included = %v, %v, %v",
			el.lines[3].included, el.lines[4].included, el.lines[5].included)
	}
}

func TestEditModelSaveAndAbort(t *testing.T) {
	for _, k := range []string{"enter", "ctrl+j", "s"} {
		m := newEditModel(editLogFrom(t, logUnknown), time.UTC)
		nm, cmd := m.Update(key(k))
		if !nm.(editModel).saved || cmd == nil {
			t.Errorf("%q: saved = %v, quit cmd = %v; want true, non-nil", k, nm.(editModel).saved, cmd)
		}
	}
	for _, k := range []string{"q", "esc"} {
		m := newEditModel(editLogFrom(t, logUnknown), time.UTC)
		nm, cmd := m.Update(key(k))
		if nm.(editModel).saved || cmd == nil {
			t.Errorf("%q: saved = %v, quit cmd = %v; want false, non-nil", k, nm.(editModel).saved, cmd)
		}
	}
}

func TestEditModelModeCycle(t *testing.T) {
	m := newEditModel(editLogFrom(t, logUnknown), time.UTC)
	for _, want := range []displayMode{modePrettify, modeCustom, modeRaw} {
		m = press(m, "m")
		if m.mode != want {
			t.Fatalf("mode = %v, want %v", m.mode, want)
		}
	}
}

func TestEditModelTemplateInput(t *testing.T) {
	m := newEditModel(editLogFrom(t, logUnknown), time.UTC)
	m = press(m, "t")
	if !m.tmplEdit || m.mode != modeCustom {
		t.Fatal("t must open template input in custom mode")
	}
	m = press(m, "{title}")
	m = press(m, "backspace")
	m = press(m, "enter")
	if m.tmplEdit || m.tmpl != defaultTemplate+"{title" {
		t.Fatalf("tmpl = %q, tmplEdit = %v", m.tmpl, m.tmplEdit)
	}
	m = press(m, "t")
	m = press(m, "x")
	m = press(m, "esc")
	if m.tmplEdit || m.tmpl != defaultTemplate+"{title" {
		t.Fatal("esc must cancel template input without changing the template")
	}
}

func TestEditModelScroll(t *testing.T) {
	var b strings.Builder
	b.WriteString("#TZ/UTC\n")
	for i := 0; i < 100; i++ {
		b.WriteString("A\tB\tT\t1\t100\tL\t1782907200\n")
	}
	m := newEditModel(editLogFrom(t, b.String()), time.UTC)
	m.height = 10 // visible = 6
	m = press(m, "end")
	if m.cursor != 99 || m.offset != 94 {
		t.Fatalf("end: cursor = %d, offset = %d; want 99, 94", m.cursor, m.offset)
	}
	m = press(m, "home")
	if m.cursor != 0 || m.offset != 0 {
		t.Fatalf("home: cursor = %d, offset = %d; want 0, 0", m.cursor, m.offset)
	}
	m = press(m, "pgdown")
	if m.cursor != 6 {
		t.Fatalf("pgdown: cursor = %d, want 6", m.cursor)
	}
}

func TestRunEditorHeadless(t *testing.T) {
	el := editLogFrom(t, logUnknown)
	saved, err := runEditor(el, time.UTC,
		tea.WithInput(strings.NewReader(" \r")), tea.WithOutput(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	if !saved || el.lines[3].included {
		t.Fatalf("saved = %v, first entry included = %v; want true, false", saved, el.lines[3].included)
	}

	el = editLogFrom(t, logUnknown)
	saved, err = runEditor(el, time.UTC,
		tea.WithInput(strings.NewReader("q")), tea.WithOutput(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	if saved || !el.lines[3].included {
		t.Fatalf("abort: saved = %v, first entry included = %v; want false, true", saved, el.lines[3].included)
	}
}

func TestWriteEditLog(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".scrobbler.log")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeEditLog(p, "new\ncontent\n"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new\ncontent\n" {
		t.Fatalf("content = %q", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}
