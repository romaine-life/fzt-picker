package picker

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/romaine-life/fzt/core"
	"github.com/romaine-life/fzt-terminal/tui"
)

// hiddenTree makes dir\hid\inner and dir\other, with hid and other hidden, and
// returns dir by its long name (a runner's TEMP can be an 8.3 short name).
func hiddenTree(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{`hid\inner`, `other`} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"hid", "other"} {
		p, _ := syscall.UTF16PtrFromString(filepath.Join(dir, name))
		if err := syscall.SetFileAttributes(p, syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_HIDDEN); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func names(items []core.Item) map[string]bool {
	got := map[string]bool{}
	for _, it := range items {
		got[it.Fields[0]] = true
	}
	return got
}

func TestStartPathShowsItsHiddenFolders(t *testing.T) {
	dir := hiddenTree(t)

	got := names(NewDirProvider(true, filepath.Join(dir, "hid", "inner")).LoadChildren(dir))
	if !got["hid"] {
		t.Error("hidden folder on the start path is missing")
	}
	if got["other"] {
		t.Error("hidden folder off the start path is shown")
	}

	if got := names(NewDirProvider(true, "").LoadChildren(dir)); got["hid"] || got["other"] {
		t.Errorf("no start folder: hidden folders shown: %v", got)
	}
}

// The file dialog's session, given an app's start folder inside a hidden one,
// opens there with nothing in the title but the mode.
func TestOpensInsideAHiddenStartFolder(t *testing.T) {
	inner := filepath.Join(hiddenTree(t), "hid", "inner")
	items := append([]core.Item{HeaderItem("Name")}, DriveRoots()...)
	sess := tui.NewTreeSession(items, NewConfig(Options{FoldersOnly: true, StartDir: inner}), 90, 20)

	s := sess.State()
	ctx := s.TopCtx()
	if got := core.ItemFullPath(ctx, ctx.Scope[len(ctx.Scope)-1].ParentIdx); got != inner {
		t.Errorf("opened in %q, want %q", got, inner)
	}
	if s.TitleOverride != "" {
		t.Errorf("title says %q", s.TitleOverride)
	}
}

// A start folder that isn't there opens at the last folder found and says so.
func TestMissingStartFolderSaysSo(t *testing.T) {
	missing := filepath.Join(hiddenTree(t), "gone")
	items := append([]core.Item{HeaderItem("Name")}, DriveRoots()...)
	sess := tui.NewTreeSession(items, NewConfig(Options{FoldersOnly: true, StartDir: missing}), 90, 20)

	if want := "no folder " + missing; sess.State().TitleOverride != want {
		t.Errorf("title = %q, want %q", sess.State().TitleOverride, want)
	}
}
