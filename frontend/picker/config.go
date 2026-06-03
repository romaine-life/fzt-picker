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
	AcceptNth   []int
	Title       string // optional override; empty uses DefaultTitle
}

// DefaultTitle returns "Pick a file" or "Pick a folder".
func DefaultTitle(foldersOnly bool) string {
	if foldersOnly {
		return "Pick a folder"
	}
	return "Pick a file"
}

// NewConfig builds the shared tui.Config for picker sessions.
func NewConfig(opts Options) tui.Config {
	title := opts.Title
	if title == "" {
		title = DefaultTitle(opts.FoldersOnly)
	}
	return tui.Config{
		Layout:       "reverse",
		Border:       true,
		Tiered:       true,
		DepthPenalty: 5,
		HeaderLines:  1,
		Nth:          []int{1},
		AcceptNth:    opts.AcceptNth,
		Title:        title,
		TreeMode:     true,
		FoldersOnly:  opts.FoldersOnly,
		FrontendName: "picker",
		Provider:     opts.Provider,
		FocusedDir:   opts.StartDir,
	}
}
