use windows::Win32::System::Com::*;
use windows::Win32::UI::Shell::*;
use windows::Win32::UI::WindowsAndMessaging::*;

/// `--repeat N --gap-ms M` opens N dialogs, M ms apart, and between them keeps
/// living like an app: it makes a window and pumps messages, so the hook can load,
/// unload and take over as it would in a long-running program (TESTING.md, the hook).
fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = std::env::args().collect();
    let folders_only = args.iter().any(|a| a == "--folders");
    let number = |flag: &str, default: u64| {
        args.iter()
            .position(|a| a == flag)
            .and_then(|i| args.get(i + 1))
            .and_then(|v| v.parse().ok())
            .unwrap_or(default)
    };
    let repeat = number("--repeat", 1);
    let gap = std::time::Duration::from_millis(number("--gap-ms", 0));

    unsafe {
        CoInitializeEx(None, COINIT_APARTMENTTHREADED).ok()?;
    }

    for round in 0..repeat {
        if round > 0 {
            live_for(gap);
        }
        open_dialog(folders_only)?;
    }

    unsafe { CoUninitialize() };
    Ok(())
}

/// Live like an app for `gap`: pump messages, and make and destroy a window every
/// 100 ms, as an app's windows come and go and take focus.
fn live_for(gap: std::time::Duration) {
    let end = std::time::Instant::now() + gap;
    unsafe {
        while std::time::Instant::now() < end {
            if let Ok(hwnd) = CreateWindowExW(
                WINDOW_EX_STYLE(0),
                windows::core::w!("STATIC"),
                windows::core::w!("test-trigger"),
                WINDOW_STYLE(0),
                0, 0, 10, 10,
                None, None, None, None,
            ) {
                let _ = DestroyWindow(hwnd);
            }
            let mut msg = MSG::default();
            while PeekMessageW(&mut msg, None, 0, 0, PM_REMOVE).as_bool() {
                let _ = TranslateMessage(&msg);
                DispatchMessageW(&msg);
            }
            std::thread::sleep(std::time::Duration::from_millis(100));
        }
    }
}

fn open_dialog(folders_only: bool) -> Result<(), Box<dyn std::error::Error>> {
    println!(
        "test-trigger: calling CoCreateInstance for FileOpenDialog (folders_only={folders_only})..."
    );

    let dialog: IFileOpenDialog = unsafe {
        CoCreateInstance(&FileOpenDialog, None, CLSCTX_INPROC_SERVER)?
    };

    if folders_only {
        unsafe {
            let mut opts = dialog.GetOptions()?;
            opts |= FILEOPENDIALOGOPTIONS(0x20); // FOS_PICKFOLDERS
            dialog.SetOptions(opts)?;
        }
        println!("test-trigger: set FOS_PICKFOLDERS");
    }

    println!("test-trigger: calling Show()...");

    let result = unsafe { dialog.Show(None) };

    match result {
        Ok(()) => {
            let item = unsafe { dialog.GetResult()? };
            let path = unsafe {
                let name = item.GetDisplayName(SIGDN_FILESYSPATH)?;
                let s = name.to_string()?;
                CoTaskMemFree(Some(name.0 as *const _));
                s
            };
            println!("test-trigger: selected file: {path}");
        }
        Err(e) => {
            println!("test-trigger: dialog cancelled or failed: {e}");
        }
    }
    Ok(())
}
