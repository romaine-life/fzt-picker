package picker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxHiddenFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"shown", ".dotted"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "broken")); err != nil {
		t.Fatal(err)
	}

	items := NewDirProvider(false).LoadChildren(dir)
	if len(items) != 1 || items[0].Fields[0] != "shown" {
		var names []string
		for _, it := range items {
			names = append(names, it.Fields[0])
		}
		t.Fatalf("got %v, want only shown", names)
	}
}
