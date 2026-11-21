package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// editLine is one physical line of the play log. text excludes the line
// ending; cr records a trailing \r so CRLF logs round-trip unchanged.
type editLine struct {
	text     string
	cr       bool
	kind     lineKind
	listen   Listen
	fields   []string // tab fields, for entries
	rating   string   // original rating, L or S
	included bool
}

type editLog struct {
	lines           []editLine
	trailingNewline bool
}

// readEditLog loads the log for editing. Headers, malformed lines and
// unset-clock lines are kept verbatim and hidden; L entries start included,
// S entries start excluded.
func readEditLog(path string, loc *time.Location) (*editLog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	el := &editLog{}
	s := string(data)
	if strings.HasSuffix(s, "\n") {
		el.trailingNewline = true
		s = strings.TrimSuffix(s, "\n")
	}
	if s == "" {
		return el, nil
	}
	pieces := strings.Split(s, "\n")
	el.lines = make([]editLine, len(pieces))
	texts := make([]string, len(pieces))
	for i, piece := range pieces {
		el.lines[i].cr = strings.HasSuffix(piece, "\r")
		el.lines[i].text = strings.TrimSuffix(piece, "\r")
		texts[i] = el.lines[i].text
	}
	utc := logIsUTC(texts)
	for i := range el.lines {
		ln := &el.lines[i]
		ln.kind, ln.listen = classifyLine(ln.text, utc, loc)
		if ln.kind == lineListen || ln.kind == lineSkipped {
			ln.fields = strings.Split(ln.text, "\t")
			ln.rating = ln.fields[5]
			ln.included = ln.kind == lineListen
		}
	}
	return el, nil
}

// render returns the edited log: exactly the included entries, all rated L,
// plus every kept line verbatim in its original order.
func (el *editLog) render() string {
	var b strings.Builder
	first := true
	for _, ln := range el.lines {
		text := ln.text
		if ln.kind == lineListen || ln.kind == lineSkipped {
			if !ln.included {
				continue
			}
			f := append([]string(nil), ln.fields...)
			f[5] = "L"
			text = strings.Join(f, "\t")
		}
		if !first {
			b.WriteByte('\n')
		}
		first = false
		b.WriteString(text)
		if ln.cr {
			b.WriteByte('\r')
		}
	}
	if !first && el.trailingNewline {
		b.WriteByte('\n')
	}
	return b.String()
}

// writeEditLog replaces the log atomically: temp file in the same folder,
// synced, then renamed over the original.
func writeEditLog(path, content string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".tide-edit-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

type displayMode int

const (
	modeRaw displayMode = iota
	modePrettify
	modeCustom
)

func (m displayMode) String() string {
	switch m {
	case modePrettify:
		return "prettify"
	case modeCustom:
		return "custom"
	default:
		return "raw"
	}
}

const defaultTemplate = "{artist} - {title}"

var audioExts = []string{".mp3", ".flac", ".m4a", ".ogg", ".oga", ".opus", ".wav", ".wma", ".aac"}

// prettify cleans up untagged entries for display only: strip the audio
// extension, then split "Artist - Title" on the first dash separator.
func prettify(l Listen) (artist, title string) {
	artist, title = l.Artist, l.Title
	if artist != "<Untagged>" {
		return artist, title
	}
	if ext := filepath.Ext(title); ext != "" {
		for _, e := range audioExts {
			if strings.EqualFold(ext, e) {
				title = strings.TrimSuffix(title, ext)
				break
			}
		}
	}
	best, sep := -1, ""
	for _, s := range []string{" - ", " – ", " — "} {
		if i := strings.Index(title, s); i >= 0 && (best < 0 || i < best) {
			best, sep = i, s
		}
	}
	if best >= 0 {
		artist = strings.TrimSpace(title[:best])
		title = strings.TrimSpace(title[best+len(sep):])
	}
	return artist, title
}

func formatDuration(sec int) string {
	if sec >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", sec/3600, (sec%3600)/60, sec%60)
	}
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

func expandTemplate(tmpl string, l Listen, rating string, loc *time.Location) string {
	return strings.NewReplacer(
		"{artist}", l.Artist,
		"{album}", l.Album,
		"{title}", l.Title,
		"{track}", l.TrackNumber,
		"{duration}", formatDuration(l.DurationSec),
		"{time}", time.Unix(l.ListenedAt, 0).In(loc).Format("2006-01-02 15:04"),
		"{rating}", rating,
	).Replace(tmpl)
}

func formatName(l Listen, rating string, mode displayMode, tmpl string, loc *time.Location) string {
	switch mode {
	case modePrettify:
		a, t := prettify(l)
		return a + " - " + t
	case modeCustom:
		return expandTemplate(tmpl, l, rating, loc)
	default:
		return l.Artist + " - " + l.Title
	}
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	headerStyle = lipgloss.NewStyle().Faint(true)
	cursorStyle = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	helpStyle   = lipgloss.NewStyle().Faint(true)
)

type editModel struct {
	el        *editLog
	entries   []int // indexes into el.lines of toggleable entries
	cursor    int
	offset    int
	width     int
	height    int
	mode      displayMode
	tmpl      string
	tmplEdit  bool
	tmplDraft string
	loc       *time.Location
	saved     bool
}

func newEditModel(el *editLog, loc *time.Location) editModel {
	m := editModel{el: el, loc: loc, width: 80, height: 24, tmpl: defaultTemplate}
	for i, ln := range el.lines {
		if ln.kind == lineListen || ln.kind == lineSkipped {
			m.entries = append(m.entries, i)
		}
	}
	return m
}

func (m editModel) Init() tea.Cmd { return nil }

func (m editModel) visible() int {
	n := m.height - 4
	if n < 1 {
		return 1
	}
	return n
}

func (m editModel) nameWidth() int {
	n := m.width - 36
	if n < 8 {
		return 8
	}
	return n
}

func (m *editModel) clamp() {
	last := len(m.entries) - 1
	if last < 0 {
		last = 0
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > last {
		m.cursor = last
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+m.visible() {
		m.offset = m.cursor - m.visible() + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m editModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clamp()
	case tea.KeyMsg:
		if m.tmplEdit {
			return m.updateTemplate(msg)
		}
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "enter", "ctrl+j", "s":
			m.saved = true
			return m, tea.Quit
		case " ":
			if len(m.entries) > 0 {
				ln := &m.el.lines[m.entries[m.cursor]]
				ln.included = !ln.included
			}
		case "up", "k":
			m.cursor--
		case "down", "j":
			m.cursor++
		case "pgup":
			m.cursor -= m.visible()
		case "pgdown":
			m.cursor += m.visible()
		case "home":
			m.cursor = 0
		case "end":
			m.cursor = len(m.entries) - 1
		case "m":
			m.mode = (m.mode + 1) % 3
		case "t":
			m.tmplEdit = true
			m.tmplDraft = m.tmpl
			m.mode = modeCustom
		}
		m.clamp()
	}
	return m, nil
}

func (m editModel) updateTemplate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.tmpl = m.tmplDraft
		m.tmplEdit = false
	case "esc":
		m.tmplEdit = false
	case "ctrl+c":
		return m, tea.Quit
	case "backspace":
		r := []rune(m.tmplDraft)
		if len(r) > 0 {
			m.tmplDraft = string(r[:len(r)-1])
		}
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			m.tmplDraft += string(msg.Runes)
		}
	}
	return m, nil
}

func (m editModel) row(cursor, check, name, when, length, was string) string {
	nameCell := lipgloss.NewStyle().Width(m.nameWidth()).MaxWidth(m.nameWidth()).Render(name)
	return fmt.Sprintf("%s%s %s  %-16s  %7s  %s", cursor, check, nameCell, when, length, was)
}

func (m editModel) View() string {
	var b strings.Builder
	included := 0
	for _, i := range m.entries {
		if m.el.lines[i].included {
			included++
		}
	}
	b.WriteString(titleStyle.Render(fmt.Sprintf("tide: edit scrobble log — %d/%d included", included, len(m.entries))))
	b.WriteByte('\n')
	b.WriteString(headerStyle.Render(m.row("  ", "   ", "TRACK", "TIME", "LENGTH", "WAS")))
	b.WriteByte('\n')
	if len(m.entries) == 0 {
		b.WriteString(dimStyle.Render("  (no editable entries)"))
		b.WriteByte('\n')
	}
	for vi := 0; vi < m.visible(); vi++ {
		ei := m.offset + vi
		if ei >= len(m.entries) {
			break
		}
		ln := m.el.lines[m.entries[ei]]
		cursor, check := "  ", "[ ]"
		if ln.included {
			check = "[x]"
		}
		if ei == m.cursor {
			cursor = "> "
		}
		name := formatName(ln.listen, ln.rating, m.mode, m.tmpl, m.loc)
		when := time.Unix(ln.listen.ListenedAt, 0).In(m.loc).Format("2006-01-02 15:04")
		row := m.row(cursor, check, name, when, formatDuration(ln.listen.DurationSec), ln.rating)
		if ei == m.cursor {
			row = cursorStyle.Render(row)
		} else if !ln.included {
			row = dimStyle.Render(row)
		}
		b.WriteString(row)
		b.WriteByte('\n')
	}
	if m.tmplEdit {
		b.WriteString("template: " + m.tmplDraft + "█\n")
		b.WriteString(helpStyle.Render("enter confirm · esc cancel"))
	} else {
		b.WriteString("mode: " + m.mode.String())
		if m.mode == modeCustom {
			b.WriteString(" · template: " + m.tmpl)
		}
		b.WriteByte('\n')
		b.WriteString(helpStyle.Render("space toggle · m mode · t template · enter save & send · q cancel"))
	}
	return b.String()
}

// runEditor opens the TUI and reports whether the user saved. Aborting
// (q/esc/ctrl+c) returns false with the log untouched.
func runEditor(el *editLog, loc *time.Location, opts ...tea.ProgramOption) (bool, error) {
	m := newEditModel(el, loc)
	p := tea.NewProgram(m, append([]tea.ProgramOption{tea.WithAltScreen()}, opts...)...)
	fm, err := p.Run()
	if err != nil {
		return false, err
	}
	return fm.(editModel).saved, nil
}
