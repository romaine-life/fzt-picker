package picker

import (
	"path/filepath"
	"syscall"

	"github.com/romaine-life/fzt/core"
)

// DirProvider wraps core.DirProvider and filters out hidden/system files on Windows.
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
		// Original is what FormatOutput returns when AcceptNth is empty,
		// giving the standalone path a real filesystem path on select
		// (Fields[0] is just the display name). CGo path uses
		// session.SelectedItemPath post-select and doesn't rely on this.
		item.Original = fullPath
		filtered = append(filtered, item)
	}
	return filtered
}

func isHiddenFile(path string) bool {
	pathW, _ := syscall.UTF16PtrFromString(path)
	attrs, err := syscall.GetFileAttributes(pathW)
	if err != nil {
		return true // can't read attributes — likely a protected system folder
	}
	const FILE_ATTRIBUTE_HIDDEN = 0x2
	const FILE_ATTRIBUTE_SYSTEM = 0x4
	return attrs&(FILE_ATTRIBUTE_HIDDEN|FILE_ATTRIBUTE_SYSTEM) != 0
}
