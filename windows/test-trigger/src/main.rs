use windows::Win32::System::Com::*;
use windows::Win32::UI::Shell::*;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    unsafe {
        CoInitializeEx(None, COINIT_APARTMENTTHREADED).ok()?;
    }

    println!("test-trigger: calling CoCreateInstance for FileOpenDialog...");

    let dialog: IFileOpenDialog = unsafe {
        CoCreateInstance(&FileOpenDialog, None, CLSCTX_INPROC_SERVER)?
    };

    println!("test-trigger: got IFileOpenDialog, calling Show()...");

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

    unsafe { CoUninitialize() };
    Ok(())
}
