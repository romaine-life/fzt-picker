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

// The work Mac's /d is a symlink; the folder picker must show it and go in.
func TestFoldersOnlyKeepsLinkedFolders(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "real", "repos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "d")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}

	p := NewDirProvider(true)
	var linked bool
	for _, it := range p.LoadChildren(dir) {
		if it.Fields[0] == "d" && it.HasChildren {
			linked = true
		}
	}
	if !linked {
		t.Fatal("folder reached through a link is missing from a folders-only listing")
	}
	inside := p.LoadChildren(filepath.Join(dir, "d"))
	if len(inside) != 1 || inside[0].Original != filepath.Join(dir, "d", "repos") {
		t.Fatalf("inside the link: got %v, want d/repos", inside)
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
