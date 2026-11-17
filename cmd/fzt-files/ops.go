package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// partSuffix marks a copy in progress: a finished name in a folder is always a
// finished copy.
const partSuffix = ".fzt-files-part"

// refusal is an error the person can act on, already in their terms. Its text
// may name their files, so it never reaches the log.
type refusal struct{ reason, text string }

func (r refusal) Error() string { return r.text }

func refuse(reason, format string, args ...any) error {
	return refusal{reason, fmt.Sprintf(format, args...)}
}

// logReason names a failure for the log, which is copied to the NAS and so
// never holds a path: a refusal's kind or Windows' message for an error code.
func logReason(err error) string {
	var r refusal
	var code syscall.Errno
	switch {
	case errors.As(err, &r):
		return r.reason
	case errors.Is(err, context.Canceled):
		return "stopped"
	case errors.As(err, &code):
		return code.Error()
	}
	return "unclassified"
}

// plain drops the paths from an error about one of the person's files: the
// screen has already named the file.
func plain(err error) string {
	var code syscall.Errno
	if errors.As(err, &code) {
		return code.Error()
	}
	return err.Error()
}

// moveFile moves within a drive, failing rather than replacing anything. It is
// a variable so a test can stand in for a second drive.
var moveFile = func(from, to string) error {
	source, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(source, target, 0)
}

func checkName(name string) error {
	switch {
	case name == "." || name == "..":
		return refuse("bad name", "%q can't be a name.", name)
	case strings.ContainsAny(name, `<>:"/\|?*`):
		return refuse("bad name", `A name can't contain any of < > : " / \ | ? *`)
	case strings.HasSuffix(name, ".") || strings.HasSuffix(name, " "):
		return refuse("bad name", "A name can't end in a dot or a space.")
	case strings.HasSuffix(name, partSuffix):
		return refuse("bad name", "Names ending in %s are kept for copies in progress.", partSuffix)
	}
	for _, r := range name {
		if r < ' ' {
			return refuse("bad name", "A name can't contain control characters.")
		}
	}
	return nil
}

func checkNotDrive(path string) error {
	if filepath.Dir(path) == path {
		return refuse("drive", "%s is a whole drive.", path)
	}
	return nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil || !errors.Is(err, fs.ErrNotExist)
}

func checkFree(path string) error {
	if exists(path) {
		return refuse("name taken", "There's already a %s in %s.", filepath.Base(path), filepath.Dir(path))
	}
	return nil
}

func renameEntry(path, name string) (string, error) {
	if err := checkNotDrive(path); err != nil {
		return "", err
	}
	if err := checkName(name); err != nil {
		return "", err
	}
	target := filepath.Join(filepath.Dir(path), name)
	if target == path {
		return "", refuse("same name", "That's already its name.")
	}
	// A change of case alone names the same file, which is free to take it.
	if !strings.EqualFold(target, path) {
		if err := checkFree(target); err != nil {
			return "", err
		}
	}
	if err := moveFile(path, target); err != nil {
		return "", fmt.Errorf("Couldn't rename it: %s", plain(err))
	}
	return target, nil
}

func newFolder(parent, name string) (string, error) {
	if err := checkName(name); err != nil {
		return "", err
	}
	target := filepath.Join(parent, name)
	if err := checkFree(target); err != nil {
		return "", err
	}
	if err := os.Mkdir(target, 0o777); err != nil {
		return "", fmt.Errorf("Couldn't make the folder: %s", plain(err))
	}
	return target, nil
}

// inside reports whether path is folder or something under it.
func inside(path, folder string) bool {
	path, folder = strings.ToLower(path), strings.ToLower(folder)
	return path == folder || strings.HasPrefix(path, strings.TrimRight(folder, `\`)+`\`)
}

// destination works out where path lands in folder, refusing what can't go there.
func destination(path, folder string) (string, error) {
	if err := checkNotDrive(path); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("Couldn't read it: %s", plain(err))
	}
	if info.IsDir() && inside(folder, path) {
		return "", refuse("into itself", "A folder can't go inside itself.")
	}
	target := filepath.Join(folder, filepath.Base(path))
	if err := checkFree(target); err != nil {
		return "", err
	}
	return target, nil
}

func moveEntry(ctx context.Context, path, folder string, report func(done, total int64)) (string, error) {
	target, err := destination(path, folder)
	if err != nil {
		return "", err
	}
	err = moveFile(path, target)
	if err == nil {
		return target, nil
	}
	if !errors.Is(err, windows.ERROR_NOT_SAME_DEVICE) {
		return "", fmt.Errorf("Couldn't move it: %s", plain(err))
	}
	if err := copyTo(ctx, path, target, report); err != nil {
		return "", err
	}
	if err := os.RemoveAll(path); err != nil {
		return target, fmt.Errorf("The copy in %s is complete, but the original couldn't be removed: %s", folder, plain(err))
	}
	return target, nil
}

func copyEntry(ctx context.Context, path, folder string, report func(done, total int64)) (string, error) {
	target, err := destination(path, folder)
	if err != nil {
		return "", err
	}
	if err := copyTo(ctx, path, target, report); err != nil {
		return "", err
	}
	return target, nil
}

// measure adds up what a copy has to write, refusing a link before any of it
// starts: following one could copy far more than the folder holds, or loop.
func measure(path string) (int64, error) {
	var total int64
	err := filepath.WalkDir(path, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("Couldn't read it: %s", plain(err))
		}
		if entry.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			return refuse("link", "%s is a link, and copying links isn't supported.", p)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("Couldn't read it: %s", plain(err))
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// copyTo writes path to a part name beside target, then gives it target's name,
// so a stopped or failed copy leaves nothing behind that looks finished.
func copyTo(ctx context.Context, path, target string, report func(done, total int64)) error {
	total, err := measure(path)
	if err != nil {
		return err
	}
	part := target + partSuffix
	if err := checkFree(part); err != nil {
		return err
	}
	var done int64
	progress := func(n int64) {
		done += n
		report(done, total)
	}
	err = copyTree(ctx, path, part, progress)
	if err == nil {
		err = moveFile(part, target)
	}
	if err != nil {
		removeErr := os.RemoveAll(part)
		if errors.Is(err, context.Canceled) {
			if removeErr != nil {
				return fmt.Errorf("Stopped, but %s couldn't be removed: %s", part, plain(removeErr))
			}
			return err
		}
		if !errors.As(err, new(refusal)) {
			err = fmt.Errorf("Couldn't copy it: %s", plain(err))
		}
		return err
	}
	return nil
}

func copyTree(ctx context.Context, from, to string, progress func(int64)) error {
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(ctx, from, to, info, progress)
	}
	if err := os.Mkdir(to, 0o777); err != nil {
		return err
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyTree(ctx, filepath.Join(from, entry.Name()), filepath.Join(to, entry.Name()), progress); err != nil {
			return err
		}
	}
	return os.Chtimes(to, time.Time{}, info.ModTime())
}

func copyFile(ctx context.Context, from, to string, info fs.FileInfo, progress func(int64)) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer target.Close()
	buffer := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := source.Read(buffer)
		if n > 0 {
			if _, err := target.Write(buffer[:n]); err != nil {
				return err
			}
			progress(int64(n))
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err := target.Close(); err != nil {
		return err
	}
	return os.Chtimes(to, time.Time{}, info.ModTime())
}

// hasRecycleBin reports whether a delete on path's drive can be undone: Windows
// keeps no Recycle Bin for network or removable drives.
func hasRecycleBin(path string) bool {
	volume := filepath.VolumeName(path)
	if len(volume) != 2 || volume[1] != ':' {
		return false
	}
	root, err := windows.UTF16PtrFromString(volume + `\`)
	if err != nil {
		return false
	}
	return windows.GetDriveType(root) == windows.DRIVE_FIXED
}

// shFileOp is SHFILEOPSTRUCTW.
type shFileOp struct {
	hwnd    uintptr
	op      uint32
	from    *uint16
	to      *uint16
	flags   uint16
	aborted int32
	maps    uintptr
	title   *uint16
}

const (
	foDelete           = 3
	fofSilent          = 0x0004
	fofNoConfirmation  = 0x0010
	fofAllowUndo       = 0x0040
	fofNoErrorUI       = 0x0400
	fofWantNukeWarning = 0x4000
)

var shFileOperation = windows.NewLazySystemDLL("shell32.dll").NewProc("SHFileOperationW")

// recycle sends path to the Recycle Bin. Windows silently destroys an item too
// big for the bin unless asked to warn, so that one case shows its own dialog.
func recycle(path string) error {
	from, err := windows.UTF16FromString(path)
	if err != nil {
		return err
	}
	from = append(from, 0) // the list ends in a second NUL
	op := shFileOp{
		op: foDelete, from: &from[0],
		flags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI | fofWantNukeWarning,
	}
	code, _, _ := shFileOperation.Call(uintptr(unsafe.Pointer(&op)))
	if code != 0 {
		return fmt.Errorf("Windows wouldn't delete it (code %#x); a program may have it open.", code)
	}
	if op.aborted != 0 {
		return refuse("kept", "The delete was stopped, so it's still there.")
	}
	return nil
}

func deleteEntry(path string) error {
	if err := checkNotDrive(path); err != nil {
		return err
	}
	if hasRecycleBin(path) {
		err := recycle(path)
		if err == nil && exists(path) {
			return refuse("still there", "Windows reported it deleted, but it's still there.")
		}
		return err
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("Couldn't delete it: %s", plain(err))
	}
	return nil
}

// shellOpen runs a file's default action, as a double-click in Explorer does:
// a file with no app gets Windows' "How do you want to open this?". It is a
// variable so a test opens nothing.
var shellOpen = func(path string) error {
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	dir, err := windows.UTF16PtrFromString(filepath.Dir(path))
	if err != nil {
		return err
	}
	// Shell handlers may use COM, so the call gets a thread with an apartment.
	// S_FALSE, already set up, still takes an uninitialize.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err == nil || err == syscall.Errno(1) {
		defer windows.CoUninitialize()
	}
	return windows.ShellExecute(0, nil, file, nil, dir, windows.SW_SHOWNORMAL)
}

func openFile(path string) error {
	if !exists(path) {
		return refuse("missing", "It's no longer there.")
	}
	if err := shellOpen(path); err != nil {
		return fmt.Errorf("Windows wouldn't open it: %s", plain(err))
	}
	return nil
}
