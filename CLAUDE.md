# picker

Replaces the Windows file picker dialog (IFileOpenDialog) with fzt's fuzzy finder TUI. When any app opens File > Open, a fzt tree view appears instead of the standard dialog.

## Architecture

Two layers:

- **COM hook (Rust, `windows/`)**: Hook DLL + injector. Intercepts `CoCreateInstance` for `IFileOpenDialog` via `retour` detours. Returns a COM proxy implementing `IFileOpenDialog`, `IFileDialog`, `IFileDialog2`, and `IFileDialogCustomize`. When `Show()` is called, loads the Go CGo DLL in-process and calls `PickFile()`.
- **Frontend (Go, `frontend/cgo/`)**: CGo DLL (`picker_frontend.dll`) loaded in-process by the hook DLL. Creates a visible Win32 window owned by the host app, runs a headless fzt session (`tui.NewTreeSession`), renders via GDI `ExtTextOutW`, and runs a proper modal message loop (`GetMessage`/`DispatchMessage`). Uses `DirProvider` for lazy directory loading.

### Why a visible Win32 window?

COM's `IModalWindow::Show()` must present a visible window owned by the host app and run a proper modal message loop. Without this, the DWM marks the host app "not responding." This matches how Explorer's file dialog works — it creates a Win32 dialog window in-process, disables the parent, and runs a modal loop that services both the dialog and the host app's messages. Previous approaches (subprocess, AllocConsole, ConPTY, hidden windows) all failed because they didn't provide a visible owned window.

### COM hook components

- **hook DLL** (`picker-hook.dll`): Loaded into every GUI process via `SetWindowsHookEx`. Hooks `CoCreateInstance` in `combase.dll` to intercept `CLSID_FileOpenDialog`. Returns a COM proxy.
- **injector** (`picker.exe`): Windows GUI app (no console) that installs the global CBT hook and maintains a message loop. Shows a system tray icon ("fzt picker") with right-click Exit. Killing it removes the hook from new processes.
- **test-trigger** (`test-trigger.exe`): Standalone binary that calls `CoCreateInstance(CLSID_FileOpenDialog)` and `Show()` to test the hook without needing a real app. `--folders` flag sets `FOS_PICKFOLDERS` to test folder-pick mode.

### Data flow

1. App calls `CoCreateInstance(CLSID_FileOpenDialog)` → hook returns proxy
2. App configures proxy (SetFileTypes, SetOptions, SetFolder, etc.)
3. App calls `Show()` → proxy loads `picker_frontend.dll` via `LoadLibrary`, calls `PickFile(filter, foldersOnly, startDir, ownerHwnd)`
4. Go DLL creates a visible Win32 window owned by the host app's HWND
5. Go DLL disables the owner window (modal behavior)
6. Headless fzt session with `DirProvider` loads drive roots (or start directory's contents)
7. Session renders ANSI → parsed to styled character grid → GDI `ExtTextOutW` with `ETO_OPAQUE`
8. `WM_KEYDOWN`/`WM_CHAR` events feed `session.HandleKey()`, repainting on each change
9. `GetMessage`/`DispatchMessage` modal loop keeps the host app responsive
10. On selection: re-enables owner, destroys window, returns the selected path
11. App calls `GetResult()` → proxy returns `IShellItem` for the selected path

### Style system

Picker reads all colors from `tui/style.go` (`tui.PaletteRGB`, `tui.ColorToRGB()`) and font config from `tui.DefaultFontName`/`tui.DefaultFontSize`. The GDI renderer maps tcell styles to GDI calls — this is the only picker-specific rendering code. Style changes in fzt-terminal apply to picker on rebuild.

### Folder-only mode

When an app sets `FOS_PICKFOLDERS` (e.g. Electron's `dialog.showOpenDialog({ properties: ['openDirectory'] })`), the COM proxy detects the flag in `SetOptions` and passes `foldersOnly=true` to the frontend. This triggers two behaviors:

- **DirProvider filtering**: `picker.DirProvider` skips non-directory items in `LoadChildren`, so only folders appear in the tree.
- **Folder selection**: fzt's `Config.FoldersOnly` changes the Enter key behavior — Enter on a folder you're already scoped into returns `"select:"` instead of no-op, giving a two-press gesture (Enter to navigate in, Enter again to confirm selection).

Both the COM hook path and standalone binary use the same DirProvider and config, so behavior is identical. The standalone binary accepts `--folders-only` to enable this mode.

## Dependencies

- **fzt / fzt-terminal**: Headless session, style constants, DirProvider for lazy directory loading.
- **MinGW-w64 (GCC)**: Required for CGo DLL compilation (`go build -buildmode=c-shared`).
- **Rust nightly**: Required by the `retour` crate's `static_detour!` macro.

### Shared vs rendering-specific code

The CGo DLL and standalone binary share item loading, DirProvider setup, hidden file filtering, and session config via the `frontend/picker/` package:

- **`frontend/picker/`** — shared: `DirProvider` (wraps `core.DirProvider` with hidden file filtering and folder-only filtering; also sets `Item.Original = fullPath` on each item), `NewConfig` (shared tui.Config builder), `HeaderItem`
- **`frontend/cgo/`** — CGo DLL: Win32 window + GDI rendering + modal message loop (COM hook path)
- **`frontend/`** — standalone binary: `tui.Run()` via tcell (shell path, `explore` at-command)

Both paths produce identical fzt sessions. Only the rendering surface differs.

### Selection path output

The two paths get filesystem paths through different mechanisms because fzt core stays filesystem-agnostic (`core.DirProvider` returns items with `Fields=[name]` for directories, not full paths):

- **CGo path** calls `session.SelectedItemPath()` post-select — walks the `ParentIdx` chain via `ItemFullPath` to build the filesystem path at selection time. `AcceptNth: []int{1}` in the shared config is harmless here since CGo doesn't rely on `FormatOutput`'s return value.
- **Standalone path** relies on `FormatOutput`'s fallback: `AcceptNth` is `nil`, so `FormatOutput` returns `Item.Original`. `picker.DirProvider.LoadChildren` sets `Original = fullPath` on each filtered item; `main.go` sets `Original = "<drive>:\"` on drive-root items (since `core.ListDriveRoots` returns `Fields[0] = "D:"` without the trailing slash).

This keeps fzt core pure — no filesystem-specific logic in `input.go` — while letting both picker paths output real paths.

## Building

```sh
# Rust (COM hook + injector)
cd windows
cargo build

# Go (CGo DLL) — requires GCC on PATH or CC env var
cd frontend/cgo
CGO_ENABLED=1 go build -buildmode=c-shared -o picker_frontend.dll .

# Go (standalone frontend for 'explore' at-command)
cd frontend
go build -o picker-frontend.exe .
```

## Running

Deployed to `~/bin` via release build. Starts automatically at login via a startup folder shortcut (created by `init.ps1`). Shows as a system tray icon — right-click to exit.

```sh
# Standalone folder picker (used by the 'explore' at-command)
picker-frontend --folders-only
```

## Development workflow

The hook DLL loads into every GUI process and can't be replaced while those processes are running. To deploy a new DLL:
1. Kill `picker.exe`
2. Rename the old DLL (Windows allows renaming locked files)
3. Copy the new DLL
4. Restart `picker.exe`
5. New processes get the new DLL; old processes keep the old one until they restart

The CGo frontend DLL (`picker_frontend.dll`) also gets locked by processes that have triggered a file dialog. Same rename-and-swap approach.

## Logging

The hook DLL logs to `%TEMP%\picker.log` with the host process PID prefix. Each log line is append + close to handle concurrent writes from multiple hooked processes.

## Changelog

### 2026-04-05: Initial implementation

- COM hook via `retour` detours `CoCreateInstance` for `CLSID_FileOpenDialog`
- Full `IFileOpenDialog` proxy (26 methods): Set* methods store state, `Show()` spawns fzt, `GetResult()` returns `IShellItem`
- Injector via `SetWindowsHookEx(WH_CBT)` for system-wide DLL loading

### 2026-04-13–14: Architecture overhaul

- **IFileDialogCustomize stub** — Notepad requires this interface. Added no-op implementation for all 27 methods.
- **Go CGo DLL frontend** — Replaced subprocess model with in-process CGo DLL. The Rust hook loads `picker_frontend.dll` via `LoadLibrary` and calls `PickFile()` directly. No pipe, no subprocess, no stdout parsing.
- **Visible Win32 window** — Creates a real Win32 window owned by the host app. Runs a proper modal message loop via `GetMessage`/`DispatchMessage`. Solves the "not responding" issue that plagued all previous approaches.
- **GDI text rendering** — Headless fzt session renders ANSI, parsed to a styled character grid, drawn via `ExtTextOutW` with `ETO_OPAQUE` (no flicker). Italic/bold font variants for hint text and selection indicators. Wide character (Nerd Font icon) handling with surrogate pair support. DEFAULT_CHARSET for broad Unicode coverage.
- **Shared style system** — Colors from `tui.PaletteRGB`/`tui.ColorToRGB()`, font from `tui.DefaultFontName`/`tui.DefaultFontSize`. No hardcoded palette in picker.
- **DirProvider lazy loading** — Starts at drive roots, loads children on navigate via `DirProvider`. No more Everything query for the COM path.
- **Hidden file filtering** — Filters Windows hidden/system files via file attributes.
- **System tray icon** — Injector is a GUI app (no console). Mining pick icon embedded via `winresource` crate. Right-click to exit.
- **`explore` at-command** — Added to `at-commands.ps1`, calls `picker-frontend --folders-only` and opens the result in Explorer. Leaf added to at-menu cloud database.
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
