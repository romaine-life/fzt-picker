use windows::Win32::Foundation::HINSTANCE;
use windows::Win32::System::LibraryLoader::{GetProcAddress, LoadLibraryW};
use windows::Win32::UI::WindowsAndMessaging::*;

/// Type for the SetHookHandle export from picker-hook.dll
type FnSetHookHandle = unsafe extern "system" fn(HHOOK);

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let dll_path = find_hook_dll()?;
    println!("picker: loading hook DLL from {dll_path}");

    unsafe {
        let wide: Vec<u16> = dll_path.encode_utf16().chain(std::iter::once(0)).collect();
        let hmod = LoadLibraryW(windows::core::PCWSTR(wide.as_ptr()))?;
        let hinstance: HINSTANCE = std::mem::transmute(hmod);

        // Get the HookProc export
        let hook_proc_addr = GetProcAddress(hmod, windows::core::s!("HookProc"))
            .ok_or("HookProc not found in picker-hook.dll")?;
        let hook_proc: HOOKPROC = Some(std::mem::transmute(hook_proc_addr));

        // Install global CBT hook — Windows will load picker-hook.dll into every
        // process that receives window messages
        let hook = SetWindowsHookExW(WH_CBT, hook_proc, hinstance, 0)?;

        println!("picker: global CBT hook installed (handle: {hook:?})");

        // Communicate the hook handle to the DLL so HookProc can call CallNextHookEx
        let set_handle_addr = GetProcAddress(hmod, windows::core::s!("SetHookHandle"))
            .ok_or("SetHookHandle not found in picker-hook.dll")?;
        let set_handle: FnSetHookHandle = std::mem::transmute(set_handle_addr);
        set_handle(hook);

        println!("picker: ready. Press Ctrl+C to exit.");

        // Message loop — required to keep the global hook alive
        let mut msg = MSG::default();
        while GetMessageW(&mut msg, None, 0, 0).as_bool() {
            let _ = TranslateMessage(&msg);
            DispatchMessageW(&msg);
        }

        let _ = UnhookWindowsHookEx(hook);
        println!("picker: hook removed, exiting.");
    }

    Ok(())
}

fn find_hook_dll() -> Result<String, Box<dyn std::error::Error>> {
    // Look next to the injector executable first
    let exe_dir = std::env::current_exe()?
        .parent()
        .ok_or("no parent dir")?
        .to_path_buf();

    let candidate = exe_dir.join("picker_hook.dll");
    if candidate.exists() {
        return Ok(candidate.to_string_lossy().to_string());
    }

    Err(format!(
        "picker_hook.dll not found (looked in {})",
        exe_dir.display()
    )
    .into())
}
