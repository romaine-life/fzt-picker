package picker

import (
	"os"
	"path/filepath"
	"strings"
)

// isHiddenFile hides dot names, as ls and file managers do on Linux.
func isHiddenFile(path string) bool {
	if strings.HasPrefix(filepath.Base(path), ".") {
		return true
	}
	if _, err := os.Stat(path); err != nil {
		return true // broken link or no permission: nothing to open
	}
	return false
}
