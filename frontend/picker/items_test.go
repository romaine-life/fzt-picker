package picker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDriveRootsOriginalIsOpenableRoot(t *testing.T) {
	roots := DriveRoots()
	if len(roots) == 0 {
		t.Fatal("no roots")
	}
	for _, r := range roots {
		if !strings.HasSuffix(r.Original, string(filepath.Separator)) {
			t.Errorf("root %q: Original %q lacks a trailing separator", r.Fields[0], r.Original)
		}
		if strings.HasSuffix(r.Original, string(filepath.Separator)+string(filepath.Separator)) {
			t.Errorf("root %q: Original %q has a doubled separator", r.Fields[0], r.Original)
		}
		if fi, err := os.Stat(r.Original); err != nil || !fi.IsDir() {
			t.Errorf("root %q: Original %q is not an openable directory", r.Fields[0], r.Original)
		}
	}
}

func TestLoadChildrenFoldersOnlyAndFullPaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	items := NewDirProvider(true).LoadChildren(dir)
	if len(items) != 1 || items[0].Fields[0] != "sub" {
		t.Fatalf("folders-only: got %v, want just sub", items)
	}
	if want := filepath.Join(dir, "sub"); items[0].Original != want {
		t.Errorf("Original = %q, want %q", items[0].Original, want)
	}

	if n := len(NewDirProvider(false).LoadChildren(dir)); n != 2 {
		t.Errorf("files and folders: got %d items, want 2", n)
	}
}
