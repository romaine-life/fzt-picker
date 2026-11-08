# fzt-picker

fzt's fuzzy finder as a file picker, in two forms, plus `fzt-files`, a file manager built from the same pieces:

- **Windows dialog hook:** replaces the Windows file picker dialog (IFileOpenDialog). When any app opens File > Open, a fzt tree view appears instead of the standard dialog. Windows only.
- **Standalone frontend:** a terminal picker in `frontend/`, on Windows (the `explore` at-command) and macOS (the work Mac's `launch-claude` runs it in a tmux pane).
- **`fzt-files`:** file management by chained command palettes, in `frontend/fzt-files/`. Windows only. See [fzt-files](#fzt-files).

Forgejo is the source: https://forgejo.romaine.life/romaine-life/fzt-picker. GitHub's `romaine-life/fzt-picker` is its push mirror, so a branch pushed there is overwritten, and a machine that can't reach Forgejo can't land a change here.

## Architecture

The Windows dialog hook has two layers:

- **COM hook (Rust, `windows/`)**: Hook DLL + injector. Intercepts `CoCreateInstance` for `IFileOpenDialog` via `retour` detours. Returns a COM proxy implementing `IFileOpenDialog`, `IFileDialog`, `IFileDialog2`, and `IFileDialogCustomize`. When `Show()` is called, loads the Go CGo DLL in-process and calls `PickFile()`.
- **Frontend (Go, `frontend/cgo/`)**: CGo DLL (`fzt_picker_frontend.dll`) loaded in-process by the hook DLL. Creates a visible Win32 window owned by the host app, runs a headless fzt session (`tui.NewTreeSession`), renders via GDI `ExtTextOutW`, and runs a proper modal message loop (`GetMessage`/`DispatchMessage`). Uses `DirProvider` for lazy directory loading.

### Why a visible Win32 window?

COM's `IModalWindow::Show()` must present a visible window owned by the host app and run a proper modal message loop. Without this, the DWM marks the host app "not responding." This matches how Explorer's file dialog works — it creates a Win32 dialog window in-process, disables the parent, and runs a modal loop that services both the dialog and the host app's messages. Previous approaches (subprocess, AllocConsole, ConPTY, hidden windows) all failed because they didn't provide a visible owned window.

### COM hook components

- **hook DLL** (`fzt_picker_hook.dll`): Loaded into every GUI process via `SetWindowsHookEx`. Hooks `CoCreateInstance` in `combase.dll` to intercept `CLSID_FileOpenDialog`. Returns a COM proxy. A copy stays loaded after its injector exits, so one process can hold several releases' copies; only the copy holding the process's `Local\fzt-picker-detour-<pid>` mutex patches, and another takes over from `HookProc` once it unloads. Copies that each patched on top of the last crashed the host when one unloaded (FZT-61).
- **injector** (`fzt-picker.exe`): Windows GUI app (no console) that installs the global CBT hook and maintains a message loop. Shows a system tray icon ("fzt picker") with right-click Exit. Killing it removes the hook from new processes. It loads the hook DLL by its real version folder, not through the `current` junction it starts from: through the junction every release's copy had one name, so a process that held an older copy was handed it again and never took a new hook (FP-6). By version folder, the older copy unloads once its injector is gone and the new one takes over on the process's next window event.
- **test-trigger** (`test-trigger.exe`): Standalone binary that calls `CoCreateInstance(CLSID_FileOpenDialog)` and `Show()` to test the hook without needing a real app. `--folders` flag sets `FOS_PICKFOLDERS` to test folder-pick mode.

### Data flow

1. App calls `CoCreateInstance(CLSID_FileOpenDialog)` → hook returns proxy
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
| `rename file`, `rename folder` | what, new name (selected up to the extension, as with F2 in Explorer) |
| `move file`, `move folder`, `copy file`, `copy folder` | what, destination folder |
| `delete file`, `delete folder` | what, confirmation |
| `new folder` | where, name |

`fzt-files rename <path>` (also `move`, `copy`, `delete`, and `new-folder [<in>]`) skips the list and fills the first blank, which is how another app gives a single key to a command: it prints the new path and exits 0, or exits 130 on Escape. `--start-dir <folder>` opens the pickers there; otherwise they start in the working folder, then in the last folder used.

Files are never replaced: `moveFile` is `MoveFileEx` with no replace flag, and a copy is written to `<name>.fzt-files-part`, then named. Stopping or failing removes the part, and a move between drives removes its source only after the copy is complete. Escape stops a running copy. Delete uses the Recycle Bin (`SHFileOperationW`, which shows its own dialog in the one case it would otherwise destroy an item silently, too big for the bin). On a network or removable drive there is no bin, and the confirmation row says it can't be undone. A failure shows in the panel and asks the last blank again.

Log: `%LOCALAPPDATA%\fzt-files\fzt-files.log`, through `log-search --app fzt-files`. It's copied to the NAS, so a line holds what happened and never a path or a name. `FZT_FILES_DATA` moves it for a test run. Testing: [TESTING.md](TESTING.md).

## Dependencies

- **fzt / fzt-terminal**: Headless session, style constants, DirProvider for lazy directory loading.
- **MinGW-w64 (GCC)**: Required for CGo DLL compilation (`go build -buildmode=c-shared`).
- **Rust nightly**: Required by the `retour` crate's `static_detour!` macro.

### Shared vs rendering-specific code

The CGo DLL and standalone binary share item loading, DirProvider setup, hidden file filtering, and session config via the `frontend/picker/` package:

- **`frontend/picker/`** — shared: `DirProvider` (wraps `core.DirProvider` with hidden file filtering, which spares the start folder and the folders above it, and folder-only filtering; also sets `Item.Original = fullPath` on each item), `DriveRoots` (drive roots with `Original = "D:\"`, or `/` on macOS), `NewConfig` (shared tui.Config builder, which makes the `DirProvider` for the start folder), `HeaderItem`. Hidden-file rules are per OS: `hidden_windows.go` (hidden/system attributes), `hidden_darwin.go` (dot names and the `UF_HIDDEN` flag, as Finder hides them)
- **`frontend/cgo/`** — CGo DLL: Win32 window + GDI rendering + modal message loop (COM hook path)
- **`frontend/`** — standalone binary: `tui.Run()` via tcell (shell path, `explore` at-command). The only part that builds on macOS: the work Mac's `launch-claude` runs it in a tmux pane. `front_windows.go` brings the console window forward; `front_darwin.go` is a no-op.

Both paths produce identical fzt sessions. Only the rendering surface differs.

### Selection path output

Both paths return the selected item's `Item.Original`, its full filesystem path: `picker.DirProvider.LoadChildren` sets it on every item and `picker.DriveRoots` on the drive roots (whose `Fields[0]` is the bare `D:`). The standalone path gets it from `FormatOutput` (no `AcceptNth`); the CGo path from `HandleKeyResult`'s item. fzt core stays filesystem-agnostic.

## Building

```sh
# Rust (COM hook + injector)
cd windows
cargo build

# Go (CGo DLL) — requires GCC on PATH or CC env var
cd frontend/cgo
CGO_ENABLED=1 go build -buildmode=c-shared -o fzt_picker_frontend.dll .

# Go (standalone frontend for 'explore' at-command)
cd frontend
go build -o fzt-picker-frontend.exe .

# Go (fzt-files)
go build -o fzt-files.exe ./fzt-files

# Go (standalone frontend for the work Mac)
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o fzt-picker-frontend-darwin-arm64 .
```

CI runs on Forgejo (`.forgejo/workflows/build.yml`) on every pull request and main push. Its `windows` job builds, checks imports, vets, tests and signs on upstairspc (host mode: rustup nightly, the machine's Go and WinLibs gcc). The macOS build runs on GitHub from the push mirror (`.github/workflows/macos.yml`, vet and `go test ./picker` on `macos-latest`); Forgejo's `macos` job waits for that run, and on a release adds `fzt-picker-frontend-darwin-arm64` to the Forgejo release.

Testing, including the file dialog on a hidden desktop so nothing reaches the screen: [TESTING.md](TESTING.md).

## Running

After a complete Forgejo release, CI starts home-sync on every PC. It verifies all five signed Windows binaries, switches C:\Program Files\at-home\fzt-picker\current, and reconciles the tray injector and sign-in shortcut. Cut a release through Forgejo release.yml; the manual publish.ps1 was retired by HOMECFG-110.

```sh
# Standalone folder picker (used by the 'explore' at-command)
fzt-picker-frontend --folders-only

# File manager (in the at menu as "manage files")
fzt-files
```

## Development workflow

The hook DLL loads into every GUI process and the frontend DLL into every process that opened a dialog, so neither can be replaced while those run. Each published version is its own folder, so nothing is replaced: CI publishes the release and home-sync restarts the injector on the new version. New processes get the new DLLs; old processes keep the old ones until they restart.

## Logging

The hook DLL logs to `%TEMP%\picker.log` with the host process PID prefix. Each log line is append + close to handle concurrent writes from multiple hooked processes.

## Changelog

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
