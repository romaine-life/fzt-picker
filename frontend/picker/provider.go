package picker

import (
	"path/filepath"
	"strings"

	"github.com/romaine-life/fzt/core"
)

// DirProvider wraps core.DirProvider and filters out hidden files (hidden_<os>.go),
// except the folders on the start path: naming a hidden folder asks for it.
// When FoldersOnly is set, files are excluded entirely.
type DirProvider struct {
	inner       *core.DirProvider
	FoldersOnly bool
	startPath   []string // startDir and each folder above it
}

// NewDirProvider creates a DirProvider with hidden file filtering. startDir
// (optional) and the folders above it show even when hidden, so the picker can
// open there.
func NewDirProvider(foldersOnly bool, startDir string) *DirProvider {
	p := &DirProvider{inner: core.NewDirProvider(), FoldersOnly: foldersOnly}
	if startDir != "" {
		for dir := filepath.Clean(startDir); ; {
			p.startPath = append(p.startPath, dir)
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return p
}

// onStartPath reports whether path is the start folder or above it. Names
// compare without case, as Windows and macOS do.
func (p *DirProvider) onStartPath(path string) bool {
	for _, dir := range p.startPath {
		if strings.EqualFold(dir, path) {
			return true
		}
	}
	return false
}

func (p *DirProvider) LoadChildren(parentPath string) []core.Item {
	items := p.inner.LoadChildren(parentPath)
	var filtered []core.Item
	for _, item := range items {
		if p.FoldersOnly && !item.HasChildren {
			continue
		}
		name := item.Fields[0]
		fullPath := filepath.Join(parentPath, name)
		if isHiddenFile(fullPath) && !p.onStartPath(fullPath) {
			continue
		}
		// Original is the path a selection returns in both picker paths
		// (Fields[0] is just the display name).
		item.Original = fullPath
		filtered = append(filtered, item)
	}
	return filtered
}
