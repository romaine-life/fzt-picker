// fzt-files is a file manager made of chained command palettes: pick a command
// from a list, then fzt asks for each thing it needs, so there are no keys to
// memorize. A program can skip the list and land on one command with
// `fzt-files rename <path>`.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/romaine-life/fzt-terminal/tui"
)

const usage = `fzt-files: file management by chained command palettes.

  fzt-files [--start-dir <in>]  Choose a command from the list
  fzt-files rename <path>       Rename this file or folder
  fzt-files move <path>         Move it: asks where
  fzt-files copy <path>         Copy it: asks where
  fzt-files delete <path>       Delete it: asks to confirm
  fzt-files new-folder [<in>]   Make a folder
  fzt-files help                This text

With a command given, the new path is printed when it's done.
Exit codes: 0 done, 1 failed to start, 2 bad arguments, 130 cancelled.
`

// direct resolves arguments to a command and its first answers. What the path
// is on disk chooses between a command's file and folder forms.
func direct(list []command, args []string) (*command, []string, error) {
	find := func(name string) *command {
		for i := range list {
			if list[i].name == name {
				return &list[i]
			}
		}
		return nil
	}
	verb := args[0]
	if verb == "new-folder" {
		switch len(args) {
		case 1:
			return find("new folder"), nil, nil
		case 2:
			path, err := filepath.Abs(args[1])
			return find("new folder"), []string{path}, err
		}
		return nil, nil, errUsage
	}
	if verb != "rename" && verb != "move" && verb != "copy" && verb != "delete" || len(args) != 2 {
		return nil, nil, errUsage
	}
	path, err := filepath.Abs(args[1])
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, fmt.Errorf("%s doesn't exist", path)
	}
	if err != nil {
		return nil, nil, err
	}
	kind := " file"
	if info.IsDir() {
		kind = " folder"
	}
	return find(verb + kind), []string{path}, nil
}

var errUsage = errors.New("usage")

func main() {
	os.Exit(start(os.Args[1:]))
}

func start(args []string) int {
	defer func() {
		if r := recover(); r != nil {
			logEvent("panic: %v | %s", r, debug.Stack())
			panic(r)
		}
	}()
	if len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		fmt.Print(usage)
		return 0
	}
	folder, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fzt-files: can't read this folder: %v\n", err)
		return 1
	}
	if len(args) == 2 && args[0] == "--start-dir" {
		if folder, err = filepath.Abs(args[1]); err != nil || !isFolder(folder) {
			fmt.Fprintf(os.Stderr, "fzt-files: %s isn't a folder\n", args[1])
			return 1
		}
		args = nil
	}
	var f *flow
	var chosen *command
	var given []string
	list := commands()
	if len(args) > 0 {
		chosen, given, err = direct(list, args)
		if errors.Is(err, errUsage) {
			fmt.Fprint(os.Stderr, usage)
			return 2
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "fzt-files: %v\n", err)
			return 1
		}
	}
	logEvent("start direct=%t", chosen != nil)
	setWindowTitle(title)
	items, cfg := homeItems(list), homeConfig()
	if chosen != nil {
		items = nil // the command's first step replaces this before it's seen
	}
	err = tui.RunLoop(items, cfg, func(live *tui.Live) {
		f = newFlow(live, folder, list)
		if chosen != nil {
			f.startDirect(chosen, given)
		}
	}, func(p tui.Pick, _ *tui.Live) { f.onPick(p) })
	if err != nil {
		logEvent("the terminal wouldn't open")
		fmt.Fprintf(os.Stderr, "fzt-files: %v\n", err)
		return 1
	}
	if f.output != "" {
		fmt.Println(f.output)
	}
	return f.exitCode
}
