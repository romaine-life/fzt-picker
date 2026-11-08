package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/romaine-life/fzt-picker/frontend/picker"
	"github.com/romaine-life/fzt-terminal/tui"
	"github.com/romaine-life/fzt/core"
)

const (
	title     = "fzt files"
	waiting   = "▌" // the blank being filled in the sentence
	yesTarget = "yes"
	noTarget  = "no"
)

// blankKind is what a command needs next, and so which chooser asks for it.
type blankKind int

const (
	fileBlank    blankKind = iota // a file, from the file picker
	folderBlank                   // a folder, from the folder picker
	textBlank                     // typed into the text box
	confirmBlank                  // a yes or no row
)

// blank is one thing a command needs. before is the word ahead of it in the
// sentence the title builds up: "move take.wav to ▌".
type blank struct {
	kind   blankKind
	before string
	hint   string
}

// outcome is what a finished command leaves: the path it produced and the
// sentence the panel shows.
type outcome struct {
	path, said string
}

type job struct {
	ctx    context.Context
	report func(done, total int64)
}

// command is a row in the command list: the blanks it needs and what it does
// once they're filled.
type command struct {
	name, description string
	verb              string
	working           string // "Copying": a command that may take a while says so
	blanks            []blank
	run               func(j job, answers []string) (outcome, error)
}

func renameCommand(name, description string, kind blankKind) command {
	return command{
		name: name, description: description, verb: "rename", working: "Renaming",
		blanks: []blank{{kind: kind}, {kind: textBlank, before: "to", hint: "new name"}},
		run: func(_ job, a []string) (outcome, error) {
			target, err := renameEntry(a[0], a[1])
			return outcome{target, fmt.Sprintf("Renamed %s to %s", filepath.Base(a[0]), filepath.Base(target))}, err
		},
	}
}

func moveCommand(name, description string, kind blankKind) command {
	return command{
		name: name, description: description, verb: "move", working: "Moving",
		blanks: []blank{{kind: kind}, {kind: folderBlank, before: "to"}},
		run: func(j job, a []string) (outcome, error) {
			target, err := moveEntry(j.ctx, a[0], a[1], j.report)
			return outcome{target, fmt.Sprintf("Moved %s to %s", filepath.Base(a[0]), a[1])}, err
		},
	}
}

func copyCommand(name, description string, kind blankKind) command {
	return command{
		name: name, description: description, verb: "copy", working: "Copying",
		blanks: []blank{{kind: kind}, {kind: folderBlank, before: "to"}},
		run: func(j job, a []string) (outcome, error) {
			target, err := copyEntry(j.ctx, a[0], a[1], j.report)
			return outcome{target, fmt.Sprintf("Copied %s to %s", filepath.Base(a[0]), a[1])}, err
		},
	}
}

func deleteCommand(name, description string, kind blankKind) command {
	return command{
		name: name, description: description, verb: "delete", working: "Deleting",
		blanks: []blank{{kind: kind}, {kind: confirmBlank}},
		run: func(_ job, a []string) (outcome, error) {
			err := deleteEntry(a[0])
			return outcome{a[0], "Deleted " + filepath.Base(a[0])}, err
		},
	}
}

func newFolderCommand() command {
	return command{
		name: "new folder", description: "Make a folder", verb: "new folder", working: "Making",
		blanks: []blank{{kind: folderBlank, before: "in"}, {kind: textBlank, before: "named", hint: "folder name"}},
		run: func(_ job, a []string) (outcome, error) {
			target, err := newFolder(a[0], a[1])
			return outcome{target, "Made " + target}, err
		},
	}
}

func commands() []command {
	return []command{
		renameCommand("rename file", "Give a file a new name", fileBlank),
		renameCommand("rename folder", "Give a folder a new name", folderBlank),
		moveCommand("move file", "Move a file to another folder", fileBlank),
		moveCommand("move folder", "Move a folder into another folder", folderBlank),
		copyCommand("copy file", "Copy a file to another folder", fileBlank),
		copyCommand("copy folder", "Copy a folder and what's in it", folderBlank),
		deleteCommand("delete file", "Send a file to the Recycle Bin", fileBlank),
		deleteCommand("delete folder", "Send a folder to the Recycle Bin", folderBlank),
		newFolderCommand(),
	}
}

// menu is the part of *tui.Live the flow uses; tests stand in for it.
type menu interface {
	Show(items []core.Item, cfg tui.Config)
	Ask(hint, text string, selected int)
	Update(f func(*core.State))
	OnAbort(f func())
	Close()
}

// flow walks a command through its blanks, one chooser at a time. Every method
// but the job in run's goroutine runs on the menu's goroutine.
type flow struct {
	menu     menu
	list     []command
	folder   string // where the next chooser opens: the last folder used
	current  *command
	answers  []string
	given    int  // answers that came from the command line
	direct   bool // started on one command: finishing or leaving it ends the program
	info     bool // the info rows are showing
	reported bool // something has run, so the result panel has a job
	exitCode int
	output   string // a direct command's result, for whoever started it
	cancel   context.CancelFunc
}

func newFlow(m menu, folder string, list []command) *flow {
	return &flow{menu: m, list: list, folder: folder}
}

// startDirect begins on a command with its first answers already given.
func (f *flow) startDirect(c *command, answers []string) {
	f.direct, f.current, f.answers, f.given = true, c, answers, len(answers)
	if len(answers) > 0 {
		f.folder = filepath.Dir(answers[0])
	}
	f.next()
}

func (f *flow) home() {
	f.current, f.answers, f.info = nil, nil, false
	f.menu.Show(homeItems(f.list), f.withPanel(homeConfig()))
}

// withPanel keeps the result panel off the screen until there's a result.
func (f *flow) withPanel(cfg tui.Config) tui.Config {
	cfg.ShowPanel = f.reported
	return cfg
}

func homeItems(list []command) []core.Item {
	var items []core.Item
	row := func(name, description, target string) {
		fields := []string{name, description}
		items = append(items, core.Item{
			Fields: fields, DisplayFields: fields, ParentIdx: -1,
			Action: &core.ItemAction{Type: "command", Target: target},
		})
	}
	for _, c := range list {
		row(c.name, c.description, c.name)
	}
	row("info", "Show this app's repo, build, install path and checkout", "info")
	row("quit", "Close "+title, "quit")
	return items
}

func homeConfig() tui.Config {
	return tui.Config{
		Layout: "reverse", Border: true, Tiered: true, DepthPenalty: 5, TreeMode: true,
		HidePalette: true, Nth: []int{1}, Title: title,
	}
}

// sentence is the command so far, with a mark at the blank being filled.
func (f *flow) sentence() string {
	words := []string{f.current.verb}
	for i, b := range f.current.blanks {
		if b.before != "" {
			words = append(words, b.before)
		}
		if i >= len(f.answers) {
			words = append(words, waiting)
			break
		}
		words = append(words, filepath.Base(f.answers[i]))
	}
	return strings.Join(words, " ")
}

// next shows the chooser for the first unfilled blank, or runs the command.
func (f *flow) next() {
	if len(f.answers) == len(f.current.blanks) {
		f.run()
		return
	}
	b := f.current.blanks[len(f.answers)]
	switch b.kind {
	case fileBlank, folderBlank:
		f.showPicker(b.kind == folderBlank)
	case textBlank:
		f.showText(b)
	case confirmBlank:
		f.showConfirm()
	}
}

func (f *flow) showPicker(foldersOnly bool) {
	items := append([]core.Item{picker.HeaderItem("Name")}, picker.DriveRoots()...)
	cfg := picker.NewConfig(picker.Options{
		FoldersOnly: foldersOnly,
		StartDir:    f.folder,
	})
	cfg.Title = f.sentence()
	f.menu.Show(items, f.withPanel(cfg))
}

func (f *flow) stepConfig() tui.Config {
	cfg := homeConfig()
	cfg.Title = f.sentence()
	return f.withPanel(cfg)
}

func (f *flow) showText(b blank) {
	text, selected := "", 0
	if f.current.verb == "rename" {
		text = filepath.Base(f.answers[0])
		selected = stemLength(text, f.current.blanks[0].kind == fileBlank)
	}
	f.menu.Show(nil, f.stepConfig())
	f.menu.Ask(b.hint, text, selected)
}

// stemLength is how many runes of a name a rename selects: all of a folder's,
// and a file's up to its extension, which a new name usually keeps.
func stemLength(name string, file bool) int {
	if file {
		if dot := strings.LastIndexByte(name, '.'); dot > 0 {
			return len([]rune(name[:dot]))
		}
	}
	return len([]rune(name))
}

func (f *flow) showConfirm() {
	yes, why := "Send it to the Recycle Bin", "You can restore it from there"
	if !hasRecycleBin(f.answers[0]) {
		yes, why = "Delete it for good", "This drive has no Recycle Bin, so this can't be undone"
	}
	row := func(name, description, target string) core.Item {
		fields := []string{name, description}
		return core.Item{
			Fields: fields, DisplayFields: fields, ParentIdx: -1,
			Action: &core.ItemAction{Type: "command", Target: target},
		}
	}
	f.menu.Show([]core.Item{row("Keep it", "Go back", noTarget), row(yes, why, yesTarget)}, f.stepConfig())
}

// onPick takes whatever the last chooser returned.
func (f *flow) onPick(p tui.Pick) {
	switch {
	case f.info:
		f.home()
	case f.current == nil:
		f.onHomePick(p)
	case p.Kind == "cancel":
		f.back()
	case p.Kind == "input":
		f.answer(p.Text)
	case p.Kind == "select":
		f.onChooserPick(p)
	}
}

func (f *flow) onHomePick(p tui.Pick) {
	if p.Kind == "cancel" {
		f.menu.Close()
		return
	}
	if p.Kind != "select" || p.Item.Action == nil {
		return
	}
	switch target := p.Item.Action.Target; target {
	case "quit":
		f.menu.Close()
	case "info":
		f.showInfo()
	default:
		for i := range f.list {
			if f.list[i].name == target {
				f.current, f.answers = &f.list[i], nil
				f.next()
				return
			}
		}
	}
}

func (f *flow) onChooserPick(p tui.Pick) {
	if f.current.blanks[len(f.answers)].kind == confirmBlank {
		if p.Item.Action != nil && p.Item.Action.Target == yesTarget {
			f.answer(yesTarget)
		} else {
			f.back()
		}
		return
	}
	if path := strings.TrimSpace(p.Output); path != "" {
		f.answer(path)
	}
}

func (f *flow) answer(text string) {
	f.answers = append(f.answers, text)
	f.next()
}

// back undoes the last answer. Past the first it leaves the command: for the
// command list, or the program if that's what was asked for.
func (f *flow) back() {
	switch {
	case len(f.answers) > f.given:
		f.answers = f.answers[:len(f.answers)-1]
		f.next()
	case f.direct:
		f.exitCode = 130
		f.menu.Close()
	default:
		f.home()
	}
}

// repeatLast asks for the last answer again, after a run it led to failed.
func (f *flow) repeatLast() {
	f.answers = f.answers[:len(f.answers)-1]
	f.next()
}

// run does the filled-in command off the menu's goroutine, with the menu frozen
// and Escape stopping what can be stopped.
func (f *flow) run() {
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	c, answers := f.current, append([]string(nil), f.answers...)
	subject := filepath.Base(answers[0])
	f.reported = true
	f.menu.Show(nil, f.stepConfig())
	f.menu.OnAbort(cancel)
	f.menu.Update(func(s *core.State) {
		s.Freeze()
		s.SetPanel(c.working+" "+subject+"…", core.PanelRunning)
	})
	shown := -1
	report := func(done, total int64) {
		percent := 100
		if total > 0 {
			percent = int(done * 100 / total)
		}
		if percent == shown {
			return
		}
		shown = percent
		f.menu.Update(func(s *core.State) {
			s.SetPanel(fmt.Sprintf("%s %s: %d%%. Escape stops it.", c.working, subject, percent), core.PanelRunning)
		})
	}
	go func() {
		result, err := c.run(job{ctx, report}, answers)
		f.menu.Update(func(*core.State) { f.finish(c, result, err) })
	}()
}

// finish reports a run. A failure goes back to the last blank, so the fix is
// one step away and nothing filled in is lost.
func (f *flow) finish(c *command, result outcome, err error) {
	f.cancel()
	f.menu.OnAbort(nil)
	if err != nil {
		logEvent("%s failed: %s", c.verb, logReason(err))
		message := err.Error()
		if logReason(err) == "stopped" {
			message = "Stopped. Nothing was left behind."
		}
		f.menu.Update(func(s *core.State) {
			s.Unfreeze()
			s.SetPanel(message, core.PanelFailed)
		})
		f.repeatLast()
		return
	}
	logEvent("%s done", c.verb)
	if f.direct {
		f.output = result.path
		f.menu.Close()
		return
	}
	if dir := filepath.Dir(result.path); isFolder(dir) {
		f.folder = dir
	}
	f.menu.Update(func(s *core.State) {
		s.Unfreeze()
		s.SetPanel(result.said, core.PanelOK)
	})
	f.home()
}

func isFolder(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (f *flow) showInfo() {
	f.info = true
	var items []core.Item
	for _, line := range infoLines() {
		fields := []string{line.label, line.value}
		items = append(items, core.Item{Fields: fields, DisplayFields: fields, ParentIdx: -1})
	}
	cfg := homeConfig()
	cfg.Title = title + " info: Escape goes back"
	f.menu.Show(items, f.withPanel(cfg))
}
