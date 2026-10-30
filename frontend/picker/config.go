package picker

import (
	"github.com/romaine-life/fzt/core"
	"github.com/romaine-life/fzt-terminal/tui"
)

// Options configures the differences between the CGo DLL and standalone binary paths.
type Options struct {
	FoldersOnly bool
	StartDir    string
	Provider    core.TreeProvider
	Title       string // optional purpose shown after the mode, e.g. "Launch Claude in"
}

// DefaultTitle returns "Pick a file" or "Pick a folder", the popup window's caption.
func DefaultTitle(foldersOnly bool) string {
	if foldersOnly {
		return "Pick a folder"
	}
	return "Pick a file"
}

// ModeTitle names what the picker returns, with its icon, so a folder picker
// (where Enter picks a folder) never looks like a file picker.
func ModeTitle(foldersOnly bool) string {
	if foldersOnly {
		return "\U000F024B Folder picker"
	}
	return "\uF15B File picker"
}

// NewConfig builds the shared tui.Config for picker sessions.
func NewConfig(opts Options) tui.Config {
	title := ModeTitle(opts.FoldersOnly)
	if opts.Title != "" {
		title += " \u00b7 " + opts.Title
	}
	return tui.Config{
		Layout:       "reverse",
		Border:       true,
		Tiered:       true,
		DepthPenalty: 5,
		HeaderLines:  1,
		Nth:          []int{1},
		Title:        title,
		TreeMode:     true,
		ScopedView:   true,
		HidePalette:  true,
		FoldersOnly:  opts.FoldersOnly,
		FrontendName: "picker",
		Provider:     opts.Provider,
		FocusedDir:   opts.StartDir,
	}
}
