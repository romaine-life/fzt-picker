#![windows_subsystem = "windows"]

use fzt_picker_handoff::{log, Handoff};
use windows::core::PCWSTR;
use windows::Win32::Foundation::{ERROR_MOD_NOT_FOUND, HINSTANCE, HMODULE, HWND, LPARAM, LRESULT, WPARAM};
use windows::Win32::System::LibraryLoader::{GetModuleHandleW, GetProcAddress, LoadLibraryW};
use windows::Win32::UI::Shell::{
    Shell_NotifyIconW, NIF_ICON, NIF_MESSAGE, NIF_TIP, NIM_ADD, NIM_DELETE, NOTIFYICONDATAW,
};
use windows::Win32::UI::WindowsAndMessaging::*;

const WM_TRAYICON: u32 = 0x8001;
const IDM_EXIT: u16 = 1;

/// A windowed program has nowhere to print, so a failure to start goes to the log.
fn main() {
    if let Err(e) = run() {
        log(&format!("picker: injector can't start: {e}"));
        std::process::exit(1);
    }
}

fn run() -> Result<(), Box<dyn std::error::Error>> {
    let paths = find_paths()?;
    // Published before the hook goes in, so a shim's first event finds it.
    let handoff = Handoff::create(&paths.shim)?;
    handoff.publish(&paths.hook)?;

    unsafe {
        let wide: Vec<u16> = paths.shim.encode_utf16().chain(std::iter::once(0)).collect();
        let hmod = LoadLibraryW(PCWSTR(wide.as_ptr()))?;
        let hinstance: HINSTANCE = std::mem::transmute(hmod);

        let hook_proc_addr = GetProcAddress(hmod, windows::core::s!("HookProc"))
            .ok_or("HookProc not found in the shim")?;
        if !entry_is_first_code(hmod, hook_proc_addr as usize) {
            return Err(format!("{}: HookProc is not first in its code section (shim/build.rs)", paths.shim).into());
        }
        let hook_proc: HOOKPROC = Some(std::mem::transmute(hook_proc_addr));

        let hook = SetWindowsHookExW(WH_CBT, hook_proc, hinstance, 0).map_err(|e| {
            if e.code() == ERROR_MOD_NOT_FOUND.to_hresult() {
                format!("Windows' table of hook DLLs is full, so it refused {} ({e})", paths.shim)
            } else {
                format!("SetWindowsHookEx: {e}")
            }
        })?;
        log(&format!("picker: injector hooked {} for {}", paths.shim, paths.hook));

        // Create hidden window for tray messages
        let hinst = GetModuleHandleW(None)?;
        let class_name = windows::core::w!("PickerTray");

        let wc = WNDCLASSEXW {
            cbSize: std::mem::size_of::<WNDCLASSEXW>() as u32,
            lpfnWndProc: Some(tray_wnd_proc),
            hInstance: hinst.into(),
            lpszClassName: class_name,
            ..Default::default()
        };
        RegisterClassExW(&wc);

        let hwnd = CreateWindowExW(
            WINDOW_EX_STYLE(0),
            class_name,
            PCWSTR::null(),
            WINDOW_STYLE(0),
            0, 0, 0, 0,
            None,
            None,
            hinst,
            None,
        )?;

        // Add tray icon
        let mut tip: [u16; 128] = [0; 128];
        for (i, c) in "fzt picker".encode_utf16().enumerate() {
            if i < 127 { tip[i] = c; }
        }

        let icon = LoadIconW(hinst, PCWSTR(1 as *const u16))?;

        let mut nid = NOTIFYICONDATAW {
            cbSize: std::mem::size_of::<NOTIFYICONDATAW>() as u32,
            hWnd: hwnd,
            uID: 1,
            uFlags: NIF_ICON | NIF_MESSAGE | NIF_TIP,
            uCallbackMessage: WM_TRAYICON,
            hIcon: icon,
            szTip: tip,
            ..Default::default()
        };
        let _ = Shell_NotifyIconW(NIM_ADD, &nid);

        // Message loop
        let mut msg = MSG::default();
        while GetMessageW(&mut msg, None, 0, 0).as_bool() {
            let _ = TranslateMessage(&msg);
            DispatchMessageW(&msg);
        }

        // Clean up
        nid.uFlags = NIF_ICON;
        let _ = Shell_NotifyIconW(NIM_DELETE, &nid);
        let _ = UnhookWindowsHookEx(hook);
    }

    Ok(())
}

unsafe extern "system" fn tray_wnd_proc(
    hwnd: HWND,
    msg: u32,
    wparam: WPARAM,
    lparam: LPARAM,
) -> LRESULT {
    match msg {
        WM_TRAYICON => {
            let event = (lparam.0 & 0xFFFF) as u32;
            if event == WM_RBUTTONUP {
                let menu = CreatePopupMenu().unwrap();
                let _ = AppendMenuW(menu, MENU_ITEM_FLAGS(0), IDM_EXIT as usize, windows::core::w!("Exit"));
                let mut pt = Default::default();
                let _ = GetCursorPos(&mut pt);
                let _ = SetForegroundWindow(hwnd);
                let _ = TrackPopupMenu(menu, TRACK_POPUP_MENU_FLAGS(0), pt.x, pt.y, 0, hwnd, None);
                let _ = DestroyMenu(menu);
            }
            LRESULT(0)
        }
        WM_COMMAND => {
            let id = (wparam.0 & 0xFFFF) as u16;
            if id == IDM_EXIT {
                PostQuitMessage(0);
            }
            LRESULT(0)
        }
        WM_DESTROY => {
            PostQuitMessage(0);
            LRESULT(0)
        }
        _ => DefWindowProcW(hwnd, msg, wparam, lparam),
    }
}

struct Paths {
    /// What Windows registers: one path for every release (fzt-picker-handoff).
    shim: String,
    /// What the shims load, by its real version folder, so a process holding an
    /// older release's copy loads this one beside it and the older one hands over
    /// (FP-6, hook::install).
    hook: String,
}

/// home-sync starts this exe through the `current` junction beside its version
/// folders; the shim is registered through that junction even if the exe was
/// started from its version folder. Outside that layout, the exe's own folder.
fn find_paths() -> Result<Paths, Box<dyn std::error::Error>> {
    paths_for(std::env::current_exe()?.parent().ok_or("no parent dir")?)
}

fn paths_for(launched: &std::path::Path) -> Result<Paths, Box<dyn std::error::Error>> {
    let real = real_path(launched)?;
    let stable = real
        .parent()
        .map(|root| root.join("current"))
        .filter(|current| real_path(current).is_ok_and(|target| target == real))
        .unwrap_or_else(|| launched.to_path_buf());
    let shim = stable.join("fzt_picker_shim.dll");
    let hook = real.join("fzt_picker_hook.dll");
    for file in [&shim, &hook] {
        if !file.exists() {
            return Err(format!("{} not found", file.display()).into());
        }
    }
    Ok(Paths { shim: shim.to_string_lossy().into_owned(), hook: hook.to_string_lossy().into_owned() })
}

/// Whether `entry` is the first byte of the module's first executable section, read
/// from the PE headers of the loaded image.
unsafe fn entry_is_first_code(module: HMODULE, entry: usize) -> bool {
    const IMAGE_SCN_MEM_EXECUTE: u32 = 0x2000_0000;
    let base = module.0 as *const u8;
    let read_u16 = |at: usize| (base.add(at) as *const u16).read_unaligned();
    let read_u32 = |at: usize| (base.add(at) as *const u32).read_unaligned();
    let nt = read_u32(0x3C) as usize;
    if read_u32(nt) != 0x0000_4550 {
        return false;
    }
    let sections = read_u16(nt + 6) as usize;
    let first_section = nt + 24 + read_u16(nt + 20) as usize;
    let first_code = (0..sections)
        .map(|i| first_section + i * 40)
        .filter(|&header| read_u32(header + 36) & IMAGE_SCN_MEM_EXECUTE != 0)
        .map(|header| read_u32(header + 12) as usize)
        .min();
    first_code == Some(entry - base as usize)
}

/// path with every junction and link resolved, without the `\\?\` prefix.
fn real_path(path: &std::path::Path) -> Result<std::path::PathBuf, Box<dyn std::error::Error>> {
    let resolved = std::fs::canonicalize(path)?;
    let text = resolved.to_string_lossy();
    Ok(match text.strip_prefix(r"\\?\UNC\") {
        Some(rest) => std::path::PathBuf::from(format!(r"\\{rest}")),
        None => std::path::PathBuf::from(text.strip_prefix(r"\\?\").unwrap_or(&text)),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The release build of the shim CI ships: build it first (TESTING.md).
    #[test]
    fn the_shims_hook_procedure_comes_first() {
        let target = option_env!("CARGO_TARGET_DIR")
            .map(std::path::PathBuf::from)
            .unwrap_or_else(|| std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../target"));
        let shim = target.join("release").join("fzt_picker_shim.dll");
        assert!(shim.exists(), "{} is missing: cargo build --release first", shim.display());
        let wide: Vec<u16> = shim.as_os_str().to_string_lossy().encode_utf16().chain(std::iter::once(0)).collect();
        unsafe {
            let module = LoadLibraryW(PCWSTR(wide.as_ptr())).unwrap();
            let entry = GetProcAddress(module, windows::core::s!("HookProc")).unwrap() as usize;
            assert!(entry_is_first_code(module, entry));
        }
    }

    #[test]
    fn the_shim_goes_through_current_and_the_hook_through_its_version() {
        let root = std::env::temp_dir().join(format!("fzt-picker-paths-{}", std::process::id()));
        let v1 = root.join("v1");
        let v2 = root.join("v2");
        let current = root.join("current");
        for version in [&v1, &v2] {
            std::fs::create_dir_all(version).unwrap();
            std::fs::write(version.join("fzt_picker_shim.dll"), b"").unwrap();
            std::fs::write(version.join("fzt_picker_hook.dll"), b"").unwrap();
        }
        let made = std::process::Command::new("cmd").args(["/c", "mklink", "/J"]).arg(&current).arg(&v2).output().unwrap();
        assert!(made.status.success(), "{made:?}");
        let real_v2 = real_path(&v2).unwrap();
        let through_current = paths_for(&current);
        let from_version = paths_for(&real_v2);
        let not_current = paths_for(&real_path(&v1).unwrap());
        let _ = std::fs::remove_dir(&current);
        let _ = std::fs::remove_dir_all(&root);
        for paths in [through_current.unwrap(), from_version.unwrap()] {
            assert!(paths.shim.ends_with(r"\current\fzt_picker_shim.dll"), "{}", paths.shim);
            assert_eq!(paths.hook, real_v2.join("fzt_picker_hook.dll").to_string_lossy());
        }
        assert!(not_current.unwrap().shim.ends_with(r"\v1\fzt_picker_shim.dll"));
    }

    #[test]
    fn real_path_follows_a_junction_and_drops_the_prefix() {
        let root = std::env::temp_dir().join(format!("fzt-picker-real-path-{}", std::process::id()));
        let version = root.join("v1");
        let current = root.join("current");
        std::fs::create_dir_all(&version).unwrap();
        let made = std::process::Command::new("cmd")
            .args(["/c", "mklink", "/J"])
            .arg(&current)
            .arg(&version)
            .output()
            .unwrap();
        assert!(made.status.success(), "{made:?}");
        std::fs::write(version.join("fzt-picker.exe"), b"").unwrap();
        let got = real_path(&current.join("fzt-picker.exe")).unwrap();
        let want = real_path(&version.join("fzt-picker.exe")).unwrap();
        let _ = std::fs::remove_dir(&current);
        let _ = std::fs::remove_dir_all(&root);
        assert_eq!(got, want);
        assert!(got.ends_with(r"v1\fzt-picker.exe"), "{}", got.display());
        assert!(!got.to_string_lossy().starts_with(r"\\?\"), "{}", got.display());
    }
}
