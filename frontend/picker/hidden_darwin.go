package picker

import (
	"path/filepath"
	"strings"
	"syscall"
)

// isHiddenFile hides what Finder hides: dot names and the UF_HIDDEN flag
// (chflags hidden, as on ~/Library).
func isHiddenFile(path string) bool {
	if strings.HasPrefix(filepath.Base(path), ".") {
		return true
	}
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return true // broken link or no permission: nothing to open
	}
	const UF_HIDDEN = 0x8000
	return st.Flags&UF_HIDDEN != 0
}
