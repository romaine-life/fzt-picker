package picker

import (
	"github.com/romaine-life/fzt/core"
)

// HeaderItem builds a depth=-1 header row with the given field names.
func HeaderItem(fields ...string) core.Item {
	return core.Item{Fields: fields, Depth: -1}
}
