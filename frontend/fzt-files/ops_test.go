package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o666); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func noReport(int64, int64) {}

func TestRenameNeverReplacesAFile(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.wav"), "a")
	write(t, filepath.Join(dir, "b.wav"), "b")
	_, err := renameEntry(filepath.Join(dir, "a.wav"), "b.wav")
	if err == nil || !strings.Contains(err.Error(), "already a b.wav") {
		t.Fatalf("err = %v", err)
	}
	if read(t, filepath.Join(dir, "a.wav")) != "a" || read(t, filepath.Join(dir, "b.wav")) != "b" {
		t.Fatal("a refused rename changed a file")
	}
}

func TestRenameMovesTheNameAndCanChangeCase(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.wav"), "a")
	target, err := renameEntry(filepath.Join(dir, "a.wav"), "A.WAV")
	if err != nil || filepath.Base(target) != "A.WAV" {
		t.Fatalf("target=%q err=%v", target, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "A.WAV" {
		t.Fatalf("entries = %v", entries)
	}
}

func TestRenameRefusesBadNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.wav")
	write(t, path, "a")
	for _, name := range []string{"..", `x\y`, "x/y", "x:y", "x?", "x.", "x ", "x\x01y", "a.wav", "x" + partSuffix} {
		if _, err := renameEntry(path, name); err == nil {
			t.Errorf("%q was accepted", name)
		}
	}
	if read(t, path) != "a" {
		t.Fatal("the file changed")
	}
}

func TestDrivesAreRefused(t *testing.T) {
	if _, err := renameEntry(`D:\`, "x"); err == nil {
		t.Error("rename took a drive")
	}
	if _, err := moveEntry(context.Background(), `D:\`, t.TempDir(), noReport); err == nil {
		t.Error("move took a drive")
	}
	if err := deleteEntry(`D:\`); err == nil {
		t.Error("delete took a drive")
	}
}

func TestMoveWithinADrive(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "a.wav")
	write(t, source, "a")
	folder := filepath.Join(dir, "sub")
	os.Mkdir(folder, 0o777)
	target, err := moveEntry(context.Background(), source, folder, noReport)
	if err != nil || read(t, target) != "a" || exists(source) {
		t.Fatalf("target=%q err=%v source there=%v", target, err, exists(source))
	}
}

func TestMoveAndCopyRefuseToReplaceAndToNest(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.wav"), "new")
	write(t, filepath.Join(dir, "sub", "a.wav"), "old")
	source, folder := filepath.Join(dir, "a.wav"), filepath.Join(dir, "sub")
	ctx := context.Background()
	if _, err := moveEntry(ctx, source, folder, noReport); err == nil {
		t.Error("move replaced a file")
	}
	if _, err := copyEntry(ctx, source, folder, noReport); err == nil {
		t.Error("copy replaced a file")
	}
	if read(t, filepath.Join(folder, "a.wav")) != "old" || read(t, source) != "new" {
		t.Fatal("a refused command changed a file")
	}
	if _, err := moveEntry(ctx, folder, folder, noReport); err == nil {
		t.Error("a folder moved into itself")
	}
	if _, err := copyEntry(ctx, folder, filepath.Join(folder, "deeper"), noReport); err == nil {
		t.Error("a folder copied into itself")
	}
	if _, err := copyEntry(ctx, source, dir, noReport); err == nil {
		t.Error("copy into its own folder replaced it")
	}
}

func TestInsideIsNotFooledByASharedPrefix(t *testing.T) {
	if inside(`D:\music2`, `D:\music`) {
		t.Fatal(`D:\music2 isn't inside D:\music`)
	}
	if !inside(`D:\MUSIC\a`, `d:\music`) || !inside(`D:\a`, `D:\`) {
		t.Fatal("inside missed a real parent")
	}
}

func TestCopyFolderKeepsContentsAndTimesAndReports(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "take")
	write(t, filepath.Join(source, "a.wav"), "aaa")
	write(t, filepath.Join(source, "deep", "b.wav"), "bb")
	stamp := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(source, "a.wav"), time.Time{}, stamp); err != nil {
		t.Fatal(err)
	}
	var last, total int64
	target, err := copyEntry(context.Background(), source, filepath.Join(dir), func(d, n int64) { last, total = d, n })
	if err == nil {
		t.Fatal("copied a folder over itself")
	}
	folder := filepath.Join(dir, "to")
	os.Mkdir(folder, 0o777)
	target, err = copyEntry(context.Background(), source, folder, func(d, n int64) { last, total = d, n })
	if err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(target, "deep", "b.wav")) != "bb" || last != 5 || total != 5 {
		t.Fatalf("copy wrong: last=%d total=%d", last, total)
	}
	info, _ := os.Stat(filepath.Join(target, "a.wav"))
	if !info.ModTime().Equal(stamp) {
		t.Fatalf("modified time = %v", info.ModTime())
	}
	if !exists(filepath.Join(source, "a.wav")) {
		t.Fatal("copy removed the source")
	}
	if exists(target + partSuffix) {
		t.Fatal("a part folder was left behind")
	}
}

func TestStoppedCopyLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "take")
	write(t, filepath.Join(source, "a.wav"), strings.Repeat("x", 3<<20))
	folder := filepath.Join(dir, "to")
	os.Mkdir(folder, 0o777)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := copyEntry(ctx, source, folder, func(int64, int64) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	entries, _ := os.ReadDir(folder)
	if len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
	if !exists(filepath.Join(source, "a.wav")) {
		t.Fatal("the source went")
	}
}

// A second drive, as Windows reports it: the first move is refused, so the
// command copies and then removes the source.
func TestMoveAcrossDrivesCopiesThenRemoves(t *testing.T) {
	real := moveFile
	t.Cleanup(func() { moveFile = real })
	calls := 0
	moveFile = func(from, to string) error {
		calls++
		if calls == 1 {
			return windows.ERROR_NOT_SAME_DEVICE
		}
		return real(from, to)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "take")
	write(t, filepath.Join(source, "a.wav"), "a")
	folder := filepath.Join(dir, "to")
	os.Mkdir(folder, 0o777)
	target, err := moveEntry(context.Background(), source, folder, noReport)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(target, "a.wav")) != "a" || exists(source) {
		t.Fatalf("source there=%v", exists(source))
	}
}

func TestMoveAcrossDrivesKeepsTheSourceWhenStopped(t *testing.T) {
	real := moveFile
	t.Cleanup(func() { moveFile = real })
	calls := 0
	moveFile = func(from, to string) error {
		calls++
		if calls == 1 {
			return windows.ERROR_NOT_SAME_DEVICE
		}
		return real(from, to)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "a.wav")
	write(t, source, strings.Repeat("x", 3<<20))
	folder := filepath.Join(dir, "to")
	os.Mkdir(folder, 0o777)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := moveEntry(ctx, source, folder, func(int64, int64) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if !exists(source) || exists(filepath.Join(folder, "a.wav")) {
		t.Fatal("a stopped move must leave the source and no copy")
	}
}

func TestCopyRefusesAFolderHoldingALink(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "take")
	write(t, filepath.Join(source, "a.wav"), "a")
	if err := os.Symlink(dir, filepath.Join(source, "loop")); err != nil {
		t.Skip("this account can't make links")
	}
	folder := filepath.Join(dir, "to")
	os.Mkdir(folder, 0o777)
	if _, err := copyEntry(context.Background(), source, folder, noReport); err == nil {
		t.Fatal("copied a link")
	}
	if entries, _ := os.ReadDir(folder); len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
}

func TestNewFolderRefusesATakenName(t *testing.T) {
	dir := t.TempDir()
	target, err := newFolder(dir, "songs")
	if err != nil || !isFolder(target) {
		t.Fatalf("target=%q err=%v", target, err)
	}
	if _, err := newFolder(dir, "songs"); err == nil {
		t.Fatal("made the same folder twice")
	}
}

func TestLogReasonNeverHoldsAPath(t *testing.T) {
	path := `D:\Music\secret.wav`
	for _, err := range []error{
		refuse("name taken", "There's already a %s", path),
		&os.PathError{Op: "open", Path: path, Err: windows.ERROR_ACCESS_DENIED},
		errors.New(path),
	} {
		if got := logReason(err); strings.Contains(got, "secret") {
			t.Fatalf("logReason(%v) = %q", err, got)
		}
	}
}
