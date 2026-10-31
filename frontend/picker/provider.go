package picker

import (
	"path/filepath"

	"github.com/romaine-life/fzt/core"
)

// DirProvider wraps core.DirProvider and filters out hidden files (hidden_<os>.go).
// When FoldersOnly is set, files are excluded entirely.
type DirProvider struct {
	inner       *core.DirProvider
	FoldersOnly bool
}

// NewDirProvider creates a DirProvider with hidden file filtering.
func NewDirProvider(foldersOnly bool) *DirProvider {
	return &DirProvider{inner: core.NewDirProvider(), FoldersOnly: foldersOnly}
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
		if isHiddenFile(fullPath) {
			continue
		}
		// Original is the path a selection returns in both picker paths
		// (Fields[0] is just the display name).
		item.Original = fullPath
		filtered = append(filtered, item)
	}
	return filtered
}
