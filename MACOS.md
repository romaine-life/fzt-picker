# fzt-picker on macOS (handoff)

Goal (Nelson, 2026-10-30, OpenProject 2181): the work Mac's Ctrl+T then C chord
(tmux, `shell-config-profile-4`) opens the real folder picker, as the home PCs'
`launch-claude` does with `fzt-picker-frontend --folders-only --start-dir D:\repos`.
Until then the Mac's `bin/launch-claude` is a stand-in: `/d/repos/*` piped into
plain `fzt`. It should also carry the folder work from Nelson's other session
today (ask him which session; not in this branch).

## What stops a macOS build

`cd frontend && go build .` on macOS fails; only two spots are Windows-only:

- `frontend/picker/provider.go` `isHiddenFile`: `syscall.UTF16PtrFromString` and
  `syscall.GetFileAttributes` (hidden/system attributes). macOS equivalent: a
  leading dot, or the `UF_HIDDEN` flag (`syscall.Stat_t.Flags`).
- `frontend/main.go`: `kernel32`/`user32` lazy DLLs to bring the console window
  forward (`GetConsoleWindow`, `SetForegroundWindow`). Nothing needed on macOS:
  the picker runs in a tmux pane.

Split each into `_windows.go` / `_unix.go` files by build tag. The Rust
`windows/` tree (hook, injector, test-trigger) is the Win+... desktop hook and
stays Windows-only.

## Shipping it to the Mac

- CI: a `macos-latest` job (or `GOOS=darwin GOARCH=arm64`, no cgo needed for
  the frontend) and a release asset `fzt-picker-frontend-darwin-arm64`.
- `shell-config-profile-4`: `setup/init.sh` installs it to `~/.local/bin`, and
  `bin/launch-claude` calls `fzt-picker-frontend --folders-only --title "Launch
  Claude in" --start-dir /d/repos` in place of the stand-in.
