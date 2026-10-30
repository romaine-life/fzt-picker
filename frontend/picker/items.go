package picker

import (
	"github.com/romaine-life/fzt/core"
)

// HeaderItem builds a depth=-1 header row with the given field names.
func HeaderItem(fields ...string) core.Item {
	return core.Item{Fields: fields, Depth: -1}
}

// DriveRoots lists the drive roots with Original set to the openable path
// ("D:\"), since their Fields[0] is the bare "D:". Selections in both picker
// paths return Item.Original.
func DriveRoots() []core.Item {
	items := core.ListDriveRoots()
	for i := range items {
		items[i].Original = items[i].Fields[0] + `\`
	}
	return items
}
