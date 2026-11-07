#![windows_subsystem = "windows"]

use windows::core::PCWSTR;
use windows::Win32::Foundation::{HINSTANCE, HWND, LPARAM, LRESULT, WPARAM};
use windows::Win32::System::LibraryLoader::{GetModuleHandleW, GetProcAddress, LoadLibraryW};
use windows::Win32::UI::Shell::{
    Shell_NotifyIconW, NIF_ICON, NIF_MESSAGE, NIF_TIP, NIM_ADD, NIM_DELETE, NOTIFYICONDATAW,
};
use windows::Win32::UI::WindowsAndMessaging::*;

type FnSetHookHandle = unsafe extern "system" fn(HHOOK);

const WM_TRAYICON: u32 = 0x8001;
const IDM_EXIT: u16 = 1;

static mut HOOK_HANDLE: HHOOK = HHOOK(std::ptr::null_mut());

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let dll_path = find_hook_dll()?;

    unsafe {
        let wide: Vec<u16> = dll_path.encode_utf16().chain(std::iter::once(0)).collect();
        let hmod = LoadLibraryW(PCWSTR(wide.as_ptr()))?;
        let hinstance: HINSTANCE = std::mem::transmute(hmod);

        let hook_proc_addr = GetProcAddress(hmod, windows::core::s!("HookProc"))
            .ok_or("HookProc not found")?;
        let hook_proc: HOOKPROC = Some(std::mem::transmute(hook_proc_addr));

        let hook = SetWindowsHookExW(WH_CBT, hook_proc, hinstance, 0)?;
        HOOK_HANDLE = hook;

        let set_handle_addr = GetProcAddress(hmod, windows::core::s!("SetHookHandle"))
            .ok_or("SetHookHandle not found")?;
        let set_handle: FnSetHookHandle = std::mem::transmute(set_handle_addr);
        set_handle(hook);

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

/// The hook DLL beside this exe, by its real path. The exe starts through the
/// `current` junction, and a DLL loaded through it has the same name in every
/// release: a process that already holds an older copy is handed that copy again,
/// so it never takes the new hook (FP-6). By its version folder, each release's
/// copy is its own module, and the older one unloads and hands over (hook::install).
fn find_hook_dll() -> Result<String, Box<dyn std::error::Error>> {
    let exe_dir = real_path(&std::env::current_exe()?)?
        .parent()
        .ok_or("no parent dir")?
        .to_path_buf();

    let candidate = exe_dir.join("fzt_picker_hook.dll");
    if candidate.exists() {
        return Ok(candidate.to_string_lossy().to_string());
    }

    Err(format!("fzt_picker_hook.dll not found (looked in {})", exe_dir.display()).into())
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
