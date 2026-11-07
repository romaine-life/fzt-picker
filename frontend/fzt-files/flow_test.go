package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/romaine-life/fzt-terminal/tui"
	"github.com/romaine-life/fzt/core"
)

// fakeMenu records what the flow asks of the menu. Update runs its function at
// once, since the flow's work reports through it.
type fakeMenu struct {
	titles []string
	asked  []string
	sel    int
	items  []core.Item
	panel  core.Panel
	state  core.State
	closed chan struct{}
	done   chan struct{}
}

func newFakeMenu() *fakeMenu {
	return &fakeMenu{closed: make(chan struct{}), done: make(chan struct{}, 8)}
}

func (m *fakeMenu) Show(items []core.Item, cfg tui.Config) {
	m.items = items
	m.titles = append(m.titles, cfg.Title)
}
func (m *fakeMenu) Ask(hint, text string, selected int) {
	m.asked = append(m.asked, text)
	m.sel = selected
}
func (m *fakeMenu) Update(f func(*core.State)) {
	f(&m.state)
	m.panel = m.state.Panel
}
func (m *fakeMenu) OnAbort(func()) {}
func (m *fakeMenu) Close()         { close(m.closed) }

func (m *fakeMenu) title() string { return m.titles[len(m.titles)-1] }

func pickRow(items []core.Item, name string) tui.Pick {
	for _, item := range items {
		if len(item.Fields) > 0 && item.Fields[0] == name {
			return tui.Pick{Kind: "select", Item: item, Output: name}
		}
	}
	return tui.Pick{}
}

func begin(t *testing.T, folder string) (*flow, *fakeMenu) {
	t.Helper()
	t.Setenv(dataEnv, t.TempDir())
	m := newFakeMenu()
	f := newFlow(m, folder, commands())
	f.home()
	return f, m
}

func path(p string) tui.Pick { return tui.Pick{Kind: "select", Output: p} }

// wait lets a run's goroutine finish: the panel leaves its running style.
func wait(t *testing.T, m *fakeMenu, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.titles) > 0 && m.title() == want && !m.state.Frozen {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("title = %q, want %q", m.title(), want)
}

func TestRenameFileWalksTheSentenceAndKeepsTheExtension(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "2026-11-07 00-24-32.wav")
	write(t, source, "a")
	f, m := begin(t, dir)

	f.onPick(pickRow(m.items, "rename file"))
	if m.title() != "rename "+waiting {
		t.Fatalf("title = %q", m.title())
	}
	f.onPick(path(source))
	if m.title() != "rename 2026-11-07 00-24-32.wav to "+waiting {
		t.Fatalf("title = %q", m.title())
	}
	if m.asked[0] != "2026-11-07 00-24-32.wav" || m.sel != len("2026-11-07 00-24-32") {
		t.Fatalf("asked %q selecting %d", m.asked[0], m.sel)
	}
	f.onPick(tui.Pick{Kind: "input", Text: "verse.wav"})
	wait(t, m, title)
	if read(t, filepath.Join(dir, "verse.wav")) != "a" || m.state.Panel.Style != core.PanelOK {
		t.Fatalf("panel = %+v", m.state.Panel)
	}
}

func TestRenameFolderSelectsTheWholeName(t *testing.T) {
	if got := stemLength("my.songs", false); got != len("my.songs") {
		t.Fatalf("folder selection = %d", got)
	}
	if got := stemLength(".gitignore", true); got != len(".gitignore") {
		t.Fatalf("dotfile selection = %d", got)
	}
	if got := stemLength("été.wav", true); got != 3 {
		t.Fatalf("selection counts runes: %d", got)
	}
}

func TestEscapeWalksBackOneBlankAtATime(t *testing.T) {
	dir := t.TempDir()
	f, m := begin(t, dir)
	f.onPick(pickRow(m.items, "move file"))
	f.onPick(path(filepath.Join(dir, "a.wav")))
	if m.title() != "move a.wav to "+waiting {
		t.Fatalf("title = %q", m.title())
	}
	f.onPick(tui.Pick{Kind: "cancel"})
	if m.title() != "move "+waiting {
		t.Fatalf("title = %q", m.title())
	}
	f.onPick(tui.Pick{Kind: "cancel"})
	if m.title() != title || f.current != nil {
		t.Fatalf("title = %q", m.title())
	}
	f.onPick(tui.Pick{Kind: "cancel"})
	select {
	case <-m.closed:
	default:
		t.Fatal("Escape on the command list should close the app")
	}
}

func TestAFailedRenameAsksAgainWithTheReason(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.wav"), "a")
	write(t, filepath.Join(dir, "b.wav"), "b")
	f, m := begin(t, dir)
	f.onPick(pickRow(m.items, "rename file"))
	f.onPick(path(filepath.Join(dir, "a.wav")))
	f.onPick(tui.Pick{Kind: "input", Text: "b.wav"})
	wait(t, m, "rename a.wav to "+waiting)
	if len(m.asked) != 2 || m.state.Panel.Style != core.PanelFailed {
		t.Fatalf("asked=%v panel=%+v", m.asked, m.state.Panel)
	}
	if !strings.Contains(strings.Join(m.state.Panel.Lines, " "), "already a b.wav") {
		t.Fatalf("panel = %+v", m.state.Panel)
	}
}

func TestDeleteAsksFirstAndKeepItLeavesTheFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "a.wav")
	write(t, source, "a")
	f, m := begin(t, dir)
	f.onPick(pickRow(m.items, "delete file"))
	f.onPick(path(source))
	if m.title() != "delete a.wav "+waiting {
		t.Fatalf("title = %q", m.title())
	}
	f.onPick(pickRow(m.items, "Keep it"))
	if m.title() != "delete "+waiting || !exists(source) {
		t.Fatalf("title = %q there=%v", m.title(), exists(source))
	}
}

func TestDirectCommandEndsTheProgramAndGivesThePath(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "a.wav")
	write(t, source, "a")
	t.Setenv(dataEnv, t.TempDir())
	m := newFakeMenu()
	list := commands()
	c, given, err := direct(list, []string{"rename", source})
	if err != nil || c.name != "rename file" {
		t.Fatalf("c=%+v err=%v", c, err)
	}
	f := newFlow(m, dir, list)
	f.startDirect(c, given)
	if m.title() != "rename a.wav to "+waiting {
		t.Fatalf("title = %q", m.title())
	}
	f.onPick(tui.Pick{Kind: "input", Text: "b.wav"})
	select {
	case <-m.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("a direct rename should close when done")
	}
	if want := filepath.Join(dir, "b.wav"); f.output != want || f.exitCode != 0 {
		t.Fatalf("output=%q code=%d", f.output, f.exitCode)
	}
}

func TestDirectCancelExitsWithoutGoingToTheList(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "take")
	os.Mkdir(source, 0o777)
	t.Setenv(dataEnv, t.TempDir())
	m := newFakeMenu()
	list := commands()
	c, given, err := direct(list, []string{"move", source})
	if err != nil || c.name != "move folder" {
		t.Fatalf("c=%+v err=%v", c, err)
	}
	f := newFlow(m, dir, list)
	f.startDirect(c, given)
	f.onPick(tui.Pick{Kind: "cancel"})
	select {
	case <-m.closed:
	default:
		t.Fatal("Escape should end a direct command")
	}
	if f.exitCode != 130 || f.output != "" {
		t.Fatalf("code=%d output=%q", f.exitCode, f.output)
	}
}

func TestDirectArguments(t *testing.T) {
	list := commands()
	if _, _, err := direct(list, []string{"rename", filepath.Join(t.TempDir(), "missing")}); err == nil || !strings.Contains(err.Error(), "doesn't exist") {
		t.Fatalf("err = %v", err)
	}
	for _, args := range [][]string{{"rename"}, {"explode", "x"}, {"move", "a", "b"}, {"new-folder", "a", "b"}} {
		if _, _, err := direct(list, args); err != errUsage {
			t.Errorf("%v: err = %v", args, err)
		}
	}
	if c, given, err := direct(list, []string{"new-folder"}); err != nil || c.name != "new folder" || len(given) != 0 {
		t.Fatalf("c=%+v given=%v err=%v", c, given, err)
	}
}

func TestEveryRowOnTheListDoesSomething(t *testing.T) {
	f, m := begin(t, t.TempDir())
	for _, item := range m.items {
		if item.Action == nil || item.Action.Target == "" {
			t.Fatalf("%v has no action", item.Fields)
		}
	}
	f.onPick(pickRow(m.items, "info"))
	if !strings.HasPrefix(m.title(), title+" info") {
		t.Fatalf("title = %q", m.title())
	}
	f.onPick(tui.Pick{Kind: "cancel"})
	if m.title() != title {
		t.Fatalf("title = %q", m.title())
	}
}

func TestBuildYmlStampsEveryBuildVariable(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".forgejo", "workflows", "build.yml"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("info.go")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)var \(\n.*?\n\)`).Find([]byte(strings.ReplaceAll(string(source), "\r\n", "\n")))
	for _, line := range strings.Split(string(block), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if !strings.Contains(string(data), "-X main."+fields[0]+"=") {
			t.Errorf("build.yml doesn't stamp %s", fields[0])
		}
	}
}

func TestLogLinesStartWithATimeAndHoldNoNewline(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(dataEnv, dir)
	logEvent("rename failed:\n%s", "name taken")
	data, err := os.ReadFile(filepath.Join(dir, "fzt-files.log"))
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimRight(string(data), "\r\n")
	if strings.Contains(line, "\n") {
		t.Fatalf("line = %q", line)
	}
	if _, err := time.Parse(time.RFC3339, strings.Fields(line)[0]); err != nil {
		t.Fatalf("line = %q: %v", line, err)
	}
}
