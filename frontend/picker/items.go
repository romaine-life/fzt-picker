package picker

import (
	"path/filepath"
	"strings"

	"github.com/romaine-life/fzt/core"
)

// HeaderItem builds a depth=-1 header row with the given field names.
func HeaderItem(fields ...string) core.Item {
	return core.Item{Fields: fields, Depth: -1}
}

// DriveRoots lists the filesystem roots with Original set to the openable path:
// "D:\" for Windows' bare "D:", "/" on macOS. Selections in both picker paths
// return Item.Original.
func DriveRoots() []core.Item {
	items := core.ListDriveRoots()
	for i := range items {
		root := items[i].Fields[0]
		if !strings.HasSuffix(root, string(filepath.Separator)) {
			root += string(filepath.Separator)
		}
		items[i].Original = root
	}
	return items
}
