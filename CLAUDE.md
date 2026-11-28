# fzt-picker

fzt's fuzzy finder as a file picker, in two forms, plus `fzt-files`, a file manager built from the same pieces:

- **Windows dialog hook:** replaces the Windows file picker dialog (IFileOpenDialog). When any app opens File > Open, a fzt tree view appears instead of the standard dialog. Windows only.
- **Standalone frontend:** a terminal picker in `cmd/fzt-picker-frontend/`, on Windows (the `explore` at-command) and macOS (the work Mac's `launch-claude` runs it in a tmux pane).
- **`fzt-files`:** file management by chained command palettes, in `cmd/fzt-files/`. Windows only. See [fzt-files](#fzt-files).

Forgejo is the source: https://forgejo.romaine.life/romaine-life/fzt-picker. GitHub's `romaine-life/fzt-picker` is its push mirror, so a branch pushed there is overwritten, and a machine that can't reach Forgejo can't land a change here.

## Architecture

The Windows dialog hook has two layers:

- **COM hook (Rust, `windows/`)**: Hook DLL + injector. Intercepts `CoCreateInstance` for `IFileOpenDialog` via `retour` detours. Returns a COM proxy implementing `IFileOpenDialog`, `IFileDialog`, `IFileDialog2`, and `IFileDialogCustomize`. When `Show()` is called, loads the Go CGo DLL in-process and calls `PickFile()`.
- **Frontend (Go, `cmd/frontend-dll/`)**: CGo DLL (`fzt_picker_frontend.dll`) loaded in-process by the hook DLL. Creates a visible Win32 window owned by the host app, runs a headless fzt session (`tui.NewTreeSession`), renders via GDI `ExtTextOutW`, and runs a proper modal message loop (`GetMessage`/`DispatchMessage`). Uses `DirProvider` for lazy directory loading.

### Why a visible Win32 window?

COM's `IModalWindow::Show()` must present a visible window owned by the host app and run a proper modal message loop. Without this, the DWM marks the host app "not responding." This matches how Explorer's file dialog works — it creates a Win32 dialog window in-process, disables the parent, and runs a modal loop that services both the dialog and the host app's messages. Previous approaches (subprocess, AllocConsole, ConPTY, hidden windows) all failed because they didn't provide a visible owned window.

### COM hook components

- **shim DLL** (`fzt_picker_shim.dll`): the DLL `SetWindowsHookEx` registers, always through the `current` junction. Windows keeps a small fixed table of hook DLL paths for the sign-in, and on nelsonpc an earlier release's entry stayed after the apps holding it had exited, until the sign-in ended; a path per release filled it, and Windows refused every new release with "The specified module could not be found" (RLFP-1). One path for every release takes one entry. Its `HookProc` reads which hook DLL the injector last published (`windows/handoff`, a named mapping per shim path) and loads that one by its version folder, so a running app moves to a new release on its next window event (FP-6). Windows calls `HookProc` at its offset in whichever shim build a process holds, so `shim/build.rs` puts it first in the code section and the injector refuses a shim where it isn't.
- **hook DLL** (`fzt_picker_hook.dll`): loaded by the shim. Hooks `CoCreateInstance` in `combase.dll` to intercept `CLSID_FileOpenDialog` and returns a COM proxy. One process can hold several releases' copies; only the copy holding the process's `Local\fzt-picker-detour-<pid>` mutex patches. The shim detaches the old copy when a newer release publishes, and attaches its own on each event, so it takes over from a pre-shim copy once that unloads. Copies that each patched on top of the last crashed the host when one unloaded (FZT-61).
- **injector** (`fzt-picker.exe`): Windows GUI app (no console) that publishes its release's hook DLL, installs the global CBT hook with the shim and maintains a message loop. Shows a system tray icon ("fzt picker") with right-click Exit. Stopping it unloads the shims, which detach the hook, so dialogs go back to the system's. A failure to start goes to `fzt-picker.log`. One runs per sign-in: before publishing anything it takes one-copy's `fzt-picker` lock, and a second copy logs `injector already running, so this one exits` and exits 0 (FP-17). The claim waits 5 s for an injector still ending, and a lock it can't check is logged and the injector runs anyway.
- **test-trigger** (`test-trigger.exe`): Standalone binary that calls `CoCreateInstance(CLSID_FileOpenDialog)` and `Show()` to test the hook without needing a real app. `--folders` flag sets `FOS_PICKFOLDERS` to test folder-pick mode.

### Data flow

1. App calls `CoCreateInstance(CLSID_FileOpenDialog)` → hook (loaded by the shim on an earlier window event) returns proxy
2. App configures proxy (SetFileTypes, SetOptions, SetFolder, etc.)
3. App calls `Show()` → proxy loads `fzt_picker_frontend.dll` via `LoadLibrary`, calls `PickFile(filter, foldersOnly, startDir, ownerHwnd)`
4. Go DLL creates a visible Win32 window owned by the host app's HWND
5. Go DLL disables the owner window (modal behavior)
6. Headless fzt session with `DirProvider` loads drive roots and, when the app gave a start folder, enters it (`FocusedDir`)
7. Session renders ANSI → parsed to styled character grid → GDI `ExtTextOutW` into an off-screen bitmap, copied to the window in one `BitBlt`
8. `WM_KEYDOWN`/`WM_CHAR` events feed `session.HandleKeyResult()`, repainting on each change
9. `GetMessage`/`DispatchMessage` modal loop keeps the host app responsive
10. On selection: re-enables owner, destroys window, returns the selected item's `Original` (its full path)
11. App calls `GetResult()` → proxy returns `IShellItem` for the selected path

### Style system

Picker reads all colors from `tui/style.go` (`tui.PaletteRGB`, `tui.ColorToRGB()`) and font config from `tui.DefaultFontName`/`tui.DefaultFontSize`. GDI does no font fallback, so the font named there (`CaskaydiaMono NF`) must be one home-sync installs on every PC; a missing one draws every icon as an empty box. The GDI renderer maps tcell styles to GDI calls — this is the only picker-specific rendering code. Style changes in fzt-terminal apply to picker on rebuild.

### Scoped view

The picker shows one folder at a time (fzt's `Config.ScopedView`): entering a folder replaces the list with its contents, and the prompt bar shows the folder's full path. Right, Space, Tab or a click goes into a folder (Enter too, in a file picker); Left, Backspace on an empty query, or Escape goes up. Search covers the current folder only. A start folder (`--start-dir`, or the app's `SetFolder`) is entered from the drive roots, so going up past it still works. Hidden folders stay hidden except the ones on the start path, so a start folder under `AppData` opens there and going up shows `AppData` alone. A start folder that can't be reached (deleted, or unreadable) opens at the last folder found, with `no folder <path>` in red in the title until the next key or click (fzt's `ExpandToPath`). The `:` command palette is off (`HidePalette`).

### Folder-only mode

When an app sets `FOS_PICKFOLDERS` (e.g. Electron's `dialog.showOpenDialog({ properties: ['openDirectory'] })`), the COM proxy detects the flag in `SetOptions` and passes `foldersOnly=true` to the frontend. This triggers two behaviors:

- **DirProvider filtering**: `picker.DirProvider` skips non-directory items in `LoadChildren`, so only folders appear in the tree.
- **Folder selection**: with fzt's `Config.FoldersOnly` and the scoped view, Enter picks a folder instead of entering it: the highlighted one, else the top match of what you typed, else the folder you are in. Right, Space, Tab or a click goes in. The border title starts with the mode (`󰉋 Folder picker`, or `File picker` for files), followed by the caller's `--title`, so the different Enter is never a surprise.

Both the COM hook path and standalone binary use the same DirProvider and config, so behavior is identical. The standalone binary accepts `--folders-only` to enable this mode.

## fzt-files

Pick a command from a list, then fzt asks for each thing it needs, so there are no keys to memorize. Choosing is always command first: a file found on its own has nothing to be returned to, so there is no file-first mode.

Each `command` in `flow.go` is its blanks (a file, a folder, typed text, or a confirmation) and what it runs once they're filled. The chooser for each blank takes over the screen, and the title is the sentence so far: `move take.wav to ▌`. Escape takes the last word back, from any folder, and leaves the command after the first; a query is cleared first. `tui.RunLoop` keeps it on one screen, with `Live.Show` for each step and `Live.Ask` for the text box. A command's file and folder forms are separate rows because each uses the picker whose Enter does the obvious thing: in a file picker it opens a folder, in a folder picker it picks one.

| Command | Blanks |
| --- | --- |
| `open file` | what: opened as a double-click in Explorer does, so a file no app opens gets Windows' "How do you want to open this?" |
| `rename file`, `rename folder` | what, new name (selected up to the extension, as with F2 in Explorer) |
| `move file`, `move folder`, `copy file`, `copy folder` | what, destination folder |
| `delete file`, `delete folder` | what, confirmation |
| `new folder` | where, name |

`fzt-files rename <path>` (also `move`, `copy`, `delete`, and `new-folder [<in>]`) skips the list and fills the first blank, which is how another app gives a single key to a command: it prints the new path and exits 0, or exits 130 on Escape. `--start-dir <folder>` opens the pickers there; otherwise they start in the working folder, then in the last folder used.

The command list is one copy, opened from the at menu's "manage files" (FP-17): `fzt-files` with no command (`--start-dir` included) takes one-copy's `fzt-files` lock before the terminal opens, and a second prints `fzt-files is already open` and exits 1, after waiting 5 s for a copy still ending. A direct command and `help` take no lock, so another app's key works while the list is open. `ONE_COPY_SCOPE` gives a test run a lock of its own.

Files are never replaced: `moveFile` is `MoveFileEx` with no replace flag, and a copy is written to `<name>.fzt-files-part`, then named. Stopping or failing removes the part, and a move between drives removes its source only after the copy is complete. Escape stops a running copy. Delete uses the Recycle Bin (`SHFileOperationW`, which shows its own dialog in the one case it would otherwise destroy an item silently, too big for the bin). On a network or removable drive there is no bin, and the confirmation row says it can't be undone. A failure shows in the panel and asks the last blank again.

Log: `%LOCALAPPDATA%\fzt-files\fzt-files.log`, through `log-search --app fzt-files`. It's copied to the NAS, so a line holds what happened and never a path or a name. `FZT_FILES_DATA` moves it for a test run. Testing: [TESTING.md](TESTING.md).

## Dependencies

- **fzt / fzt-terminal**: Headless session, style constants, DirProvider for lazy directory loading.
- **MinGW-w64 (GCC)**: Required for CGo DLL compilation (`go build -buildmode=c-shared`).
- **Rust nightly**: Required by the `retour` crate's `static_detour!` macro.

### Shared vs rendering-specific code

The CGo DLL and standalone binary share item loading, DirProvider setup, hidden file filtering, and session config via the `picker/` package:

- **`picker/`** — shared: `DirProvider` (wraps `core.DirProvider` with hidden file filtering, which spares the start folder and the folders above it, and folder-only filtering; also sets `Item.Original = fullPath` on each item), `DriveRoots` (drive roots with `Original = "D:\"`, or `/` on macOS), `NewConfig` (shared tui.Config builder, which makes the `DirProvider` for the start folder), `HeaderItem`. Hidden-file rules are per OS: `hidden_windows.go` (hidden/system attributes), `hidden_darwin.go` (dot names and the `UF_HIDDEN` flag, as Finder hides them)
- **`cmd/frontend-dll/`** — CGo DLL: Win32 window + GDI rendering + modal message loop (COM hook path)
- **`cmd/fzt-picker-frontend/`** — standalone binary: `tui.Run()` via tcell (shell path, `explore` at-command). The only part that builds on macOS: the work Mac's `launch-claude` runs it in a tmux pane. `front_windows.go` brings the console window forward; `front_darwin.go` is a no-op.

Both paths produce identical fzt sessions. Only the rendering surface differs.

### Selection path output

Both paths return the selected item's `Item.Original`, its full filesystem path: `picker.DirProvider.LoadChildren` sets it on every item and `picker.DriveRoots` on the drive roots (whose `Fields[0]` is the bare `D:`). The standalone path gets it from `FormatOutput` (no `AcceptNth`); the CGo path from `HandleKeyResult`'s item. fzt core stays filesystem-agnostic.

## Building

The Go module and the Cargo workspace are at the repo root, as pipeline-templates' suites expect; the Rust crates stay in `windows/`. From the root, at most 4 cores on nelsonpc:

```sh
# Rust (COM hook + injector); rust-toolchain.toml selects the nightly
cargo build -j 4

# Go (CGo DLL) — requires GCC on PATH or CC env var
CGO_ENABLED=1 go build -p 4 -buildmode=c-shared -o fzt_picker_frontend.dll ./cmd/frontend-dll

# Go (standalone frontend for 'explore' at-command)
go build -p 4 -o fzt-picker-frontend.exe ./cmd/fzt-picker-frontend

# Go (fzt-files)
go build -p 4 -o fzt-files.exe ./cmd/fzt-files

# Go (standalone frontend for the work Mac)
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -p 4 -o fzt-picker-frontend-darwin-arm64 ./cmd/fzt-picker-frontend
```

CI is pipeline-templates' `go`, `rust` and `release` suites, with fzt-picker's settings in its `fleet.json` entry; `ci-check` runs their checks before a push. A pull request runs the checks whose files changed, on upstairspc's Windows runner: gofmt, `go vet` and `go test` (with the WinLibs gcc cgo needs), rustfmt, clippy (`-D warnings`) and `cargo test`, and the release build. A merge to main builds and releases: the next `v0.1.N`, the Rust binaries and the frontend DLL built on Windows (which fails if the DLL imports a MinGW runtime DLL), the Go programs and the Mac frontend cross-compiled on Linux, every Windows binary signed, then home-sync on every PC. A take-up of fzt or fzt-terminal comes from the Go suite once strip-owner delivers either.

The Mac frontend also builds on GitHub from the push mirror (`.github/workflows/macos.yml`), which nothing on Forgejo waits for: every push vets it and runs `go test ./picker` on a real Mac (`hidden_darwin_test.go` runs only there), and the release tag, which the mirror carries once the Forgejo release publishes, gets a GitHub release holding `fzt-picker-frontend-darwin-arm64` for the work Mac, which can't reach Forgejo.

Testing, including the file dialog on a hidden desktop so nothing reaches the screen: [TESTING.md](TESTING.md).

## Running

After a release, CI starts home-sync on every PC. It verifies all six signed Windows binaries, switches C:\Program Files\at-home\fzt-picker\current, and reconciles the tray injector and sign-in shortcut. Every merge that changes what the release builds is released; the manual publish.ps1 was retired by HOMECFG-110.

```sh
# Standalone folder picker (used by the 'explore' at-command)
fzt-picker-frontend --folders-only

# File manager (in the at menu as "manage files")
fzt-files
```

## Development workflow

The shim and hook DLLs load into every GUI process and the frontend DLL into every process that opened a dialog, so neither can be replaced while those run. Each published version is its own folder, so nothing is replaced: CI publishes the release and home-sync restarts the injector on the new version. A running process loads the new hook on its next window event and keeps the shim it holds, which is why the shim's contract with the handoff only grows by new names.

## Logging

The injector, shim and hook DLL log to `%LOCALAPPDATA%\fzt-picker\fzt-picker.log` (`windows/handoff`'s `log`), the estate's one log line written by logline (AHT-204): time with zone, the host process's `pid=`, level, a fixed message and `key=value` fields. The hook's and injector's values go through logline's `redact`; the shim's (`log::unmasked`) hold only the hook path the injector published and Windows' error text, and skip it, since its patterns would add 1.3 MB to the shim every GUI process loads. logline opens and closes the file for each line, so lines from many hooked processes never split, and moves it to `.old` past 1 MiB. log-ship sends it to the pile (`log-search --app fzt-picker`). `FZT_PICKER_LOG` names another file. Until 2026-11 it was `%TEMP%\picker.log`, `[pid] picker: ...` lines with no time; a process that still holds an older shim or hook keeps writing there until it exits.

## Changelog

### 2026-11-28: One copy

- The tray injector runs once per sign-in: a second, started while one runs, logs that and exits before it publishes or hooks anything. Two had run after a sign-in, each with a tray icon. fzt-files' command list is one copy too; a second prints `fzt-files is already open`, and its direct commands stay outside the lock. Both through one-copy v0.1.0; the hidden-desktop tests give their injectors a lock of their own. OpenProject FP-17.

### 2026-11-17: CI from the suites

- CI is pipeline-templates' `go`, `rust` and `release` suites; the repo's own Forgejo workflows are gone. The Go module and Cargo workspace moved to the root (`picker/`, `cmd/`), every merge that changes the build is released, the Rust code is held to rustfmt and clippy, and a take-up of fzt or fzt-terminal comes from the Go suite. The Mac build no longer holds up a pull request or a release: GitHub tests it on a real Mac and publishes it for the work Mac. Release asset names are unchanged. OpenProject PIPT-20.

### 2026-11-12: Every release starts

- The injector registers a small shim DLL from one path for every release, and the shim loads the release's hook. Registering each release's hook by its own folder had filled Windows' table of hook DLLs, and from v0.1.101 the injector exited at start with "The specified module could not be found". A failure to start is now in `picker.log`. OpenProject RLFP-1.

### 2026-11-07: Icons in the file dialog

- The file dialog drew every icon as an empty box: it asked GDI for FiraCode Nerd Font Mono, which only upstairspc has, and got Arial. It now uses CaskaydiaMono NF, the Nerd Font home-sync installs on every home PC. fzt-terminal v0.1.134; OpenProject FP-3, FP-8.

### 2026-11-07: A start folder under a hidden folder

- A start folder inside a hidden folder (`AppData`) now opens there; before, the picker stopped at the last folder it could see with nothing on screen to say why. A start folder that still can't be reached says `no folder <path>` in the title. fzt v0.2.98; OpenProject FP-2, FP-7.

### 2026-10-30: Folders reached through a link

- A symlinked or junctioned folder now shows in the picker and can be entered; before, it read as a file and a folder picker hid it. The work Mac's `/d` is one, so `--start-dir /d/repos` could not get there. fzt v0.2.83; OpenProject TMUX-21.

### 2026-10-30: macOS build of the standalone frontend

- The standalone frontend builds for macOS (arm64), released as `fzt-picker-frontend-darwin-arm64`, so the work Mac's `launch-claude` can use the real folder picker. Windows-only code moved to `_windows.go` files; macOS hides dot names and `UF_HIDDEN`. Drive roots' `Original` no longer gains a stray `\` where the root already ends in a separator (`/`). OpenProject TMUX-21.

### 2026-10-29: Enter picks the folder

- In a folder picker Enter picks the highlighted, typed or current folder; Right, Space, Tab or a click goes in. Before, picking meant entering a folder and pressing Enter again with nothing highlighted. The title names the mode. fzt v0.2.77, fzt-terminal v0.1.103; OpenProject FZT-51.

### 2026-10-29: Scoped view, flicker, published to the NAS

- **Scoped view** — one folder at a time with its full path in the prompt bar, instead of expanding folders in place (their contents could land below the window). `:` palette off. Engine and renderer changes in fzt and fzt-terminal.
- **Start folder** — the standalone binary starts at the drive roots and enters `--start-dir`, so you can still go up from it; the popup now honors the app's start folder too (it always opened at the drive roots before).
- **Flicker** — the terminal picker repainted the whole screen five times a second while idle; fzt-terminal v0.1.101 made that a diffed redraw and this bumps to it. The popup drew each run of text straight to the window; it now draws off-screen and copies once.
- **Selection** — both paths return `Item.Original`; `AcceptNth` and `SelectedItemPath` are no longer used.
- **Publishing** — `publish.ps1` to `\\NelsonNAS\tools\fzt-picker`, replacing the hand-installed copy in `~\bin`.

### 2026-04-05: Initial implementation

- COM hook via `retour` detours `CoCreateInstance` for `CLSID_FileOpenDialog`
- Full `IFileOpenDialog` proxy (26 methods): Set* methods store state, `Show()` spawns fzt, `GetResult()` returns `IShellItem`
- Injector via `SetWindowsHookEx(WH_CBT)` for system-wide DLL loading

### 2026-04-13–14: Architecture overhaul

- **IFileDialogCustomize stub** — Notepad requires this interface. Added no-op implementation for all 27 methods.
- **Go CGo DLL frontend** — Replaced subprocess model with in-process CGo DLL. The Rust hook loads `fzt_picker_frontend.dll` via `LoadLibrary` and calls `PickFile()` directly. No pipe, no subprocess, no stdout parsing.
- **Visible Win32 window** — Creates a real Win32 window owned by the host app. Runs a proper modal message loop via `GetMessage`/`DispatchMessage`. Solves the "not responding" issue that plagued all previous approaches.
- **GDI text rendering** — Headless fzt session renders ANSI, parsed to a styled character grid, drawn via `ExtTextOutW` with `ETO_OPAQUE`. Italic/bold font variants for hint text and selection indicators. Wide character (Nerd Font icon) handling with surrogate pair support. DEFAULT_CHARSET for broad Unicode coverage.
- **Shared style system** — Colors from `tui.PaletteRGB`/`tui.ColorToRGB()`, font from `tui.DefaultFontName`/`tui.DefaultFontSize`. No hardcoded palette in picker.
- **DirProvider lazy loading** — Starts at drive roots, loads children on navigate via `DirProvider`. No more Everything query for the COM path.
- **Hidden file filtering** — Filters Windows hidden/system files via file attributes.
- **System tray icon** — Injector is a GUI app (no console). Mining pick icon embedded via `winresource` crate. Right-click to exit.
- **`explore` at-command** — Added to `at-commands.ps1`, calls `fzt-picker-frontend --folders-only` and opens the result in Explorer. Leaf added to at-menu cloud database.
- **Shared package** — `frontend/picker/` extracts `DirProvider`, `NewConfig`, `HeaderItem` shared by CGo DLL and standalone binary.
- **Full path selection** — `Session.SelectedItemPath()` walks the tree to return filesystem paths. `ItemFullPath` fixed to handle bare drive letters (`D:` → `D:\`).
- **Selection indicator** — `▸` (U+25B8) replaced with FA chevron (U+F054, Nerd Font PUA) for GDI compatibility.

### 2026-04-15: Folder-only mode

- **Folder-only filtering** — `picker.DirProvider` takes a `foldersOnly` flag; `LoadChildren` skips non-directory items. Hidden/system folders filtered via `GetFileAttributes`.
- **Folder selection gesture** — `FoldersOnly` config in fzt core. Enter on an already-scoped folder returns `"select:"` (double-Enter: navigate in, then confirm).
- **Removed Everything code path** — Standalone binary no longer uses Everything/es.exe. Always uses DirProvider + drive roots, matching the COM hook path.
- **test-trigger `--folders` flag** — Sets `FOS_PICKFOLDERS` for testing folder-pick mode via COM hook.

### 2026-04-16: CI workflow

- **GitHub Actions build** (`.github/workflows/build.yml`) — Builds Rust (nightly) and Go (CGo DLL + standalone) on `windows-latest`. Resolves local `replace` directives, fetches fzt/fzt-terminal from Go module proxy. Creates versioned GitHub releases with all binaries.
- **Caching** — Cargo registry/target keyed on `Cargo.lock`, MSYS2 ucrt64 (skips GCC install on warm runs). Cached build: ~2min.
- **Container image** — Dockerfile with Go/Rust/GCC on servercore:ltsc2022, pushed to GHCR via `image.yml`. Not used in build workflow (image pull on ephemeral runners is slower than cached setup). Available for future self-hosted runner use.

### Known limitations

- **Icon sizing** — Nerd Font icons appear smaller than in Windows Terminal. GDI renders at cell height; WT/CSS scale them differently.
- **IFileSaveDialog**: Detected but passes through to standard dialog
- **No fallback**: If frontend DLL fails to load, the dialog fails with ERROR_CANCELLED
- **Multi-select**: Only returns the first selected item
- **fzt-files**: One file at a time, no previews, and copying a folder that holds a link is refused
