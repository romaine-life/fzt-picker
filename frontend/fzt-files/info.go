package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	_ "time/tzdata" // A PC without Go has no zone database to load Pacific time from.
	"unsafe"

	"golang.org/x/sys/windows"
)

// Stamped by build.yml with -ldflags -X. The linker ignores an -X naming a
// variable that doesn't exist, so a test holds the two together.
var (
	version     string // the release tag
	buildRepo   string // GITHUB_REPOSITORY, owner/name
	buildServer string // GITHUB_SERVER_URL
	buildTime   string // UTC, RFC 3339
)

const (
	unstamped = "development build, not from CI"
	reposRoot = `D:\repos`
	logLimit  = 1 << 20
	dataEnv   = "FZT_FILES_DATA"
)

var setConsoleTitle = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetConsoleTitleW")

// setWindowTitle names the window: the person sees the app, not its path.
func setWindowTitle(text string) {
	if ptr, err := windows.UTF16PtrFromString(text); err == nil {
		_, _, _ = setConsoleTitle.Call(uintptr(unsafe.Pointer(ptr)))
	}
}

type infoLine struct {
	label, value string
}

func infoLines() []infoLine {
	or := func(value string) string {
		if value == "" {
			return unstamped
		}
		return value
	}
	link := ""
	if buildRepo != "" && buildServer != "" {
		link = strings.TrimRight(buildServer, "/") + "/" + buildRepo
	}
	return []infoLine{
		{"Repo", or(buildRepo)},
		{"Link", or(link)},
		{"Version", or(version)},
		{"Built", builtText(buildTime, time.Now())},
		{"Installed", installedPath()},
		{"Checkout", checkoutText(buildRepo, reposRoot)},
	}
}

func builtText(stamp string, now time.Time) string {
	if stamp == "" {
		return unstamped
	}
	built, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return fmt.Sprintf("unreadable build time %q", stamp)
	}
	pacific, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return built.UTC().Format("Jan 2, 3:04 PM") + " UTC"
	}
	return fmt.Sprintf("%s Pacific (%s)", built.In(pacific).Format("Jan 2, 2006, 3:04 PM"), elapsedText(now.Sub(built)))
}

func elapsedText(age time.Duration) string {
	count := func(n int, unit string) string {
		if n != 1 {
			unit += "s"
		}
		return fmt.Sprintf("%d %s ago", n, unit)
	}
	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		return count(int(age/time.Minute), "minute")
	case age < 24*time.Hour:
		return count(int(age/time.Hour), "hour")
	}
	return count(int(age/(24*time.Hour)), "day")
}

// installedPath follows the managed current junction to the version folder.
func installedPath() string {
	path, err := os.Executable()
	if err != nil {
		return "could not read the running program's path: " + err.Error()
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func checkoutText(repo, root string) string {
	if repo == "" {
		return unstamped + ", so its checkout is unknown"
	}
	dir := filepath.Join(root, repo[strings.LastIndex(repo, "/")+1:])
	_, err := os.Stat(filepath.Join(dir, ".git"))
	switch {
	case err == nil:
		return dir
	case errors.Is(err, fs.ErrNotExist):
		return "no checkout on this PC"
	}
	return "could not check " + dir + ": " + err.Error()
}

// dataDir holds this PC's log. FZT_FILES_DATA points a test run somewhere else,
// because the real log is copied to the NAS.
func dataDir() string {
	if dir := os.Getenv(dataEnv); dir != "" {
		return dir
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "fzt-files")
}

// logEvent appends one line and closes the file, so log-ship sees it at once.
// A line never holds a path or a name: the log is copied to the NAS.
func logEvent(format string, args ...any) {
	dir := dataDir()
	if dir == "" {
		return
	}
	path := filepath.Join(dir, "fzt-files.log")
	if info, err := os.Stat(path); err == nil && info.Size() > logLimit {
		_ = os.Rename(path, path+".old")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	message := strings.ReplaceAll(fmt.Sprintf(format, args...), "\n", " | ")
	fmt.Fprintf(file, "%s pid=%d %s\n", time.Now().Format(time.RFC3339), os.Getpid(), message)
}
