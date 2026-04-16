use windows::Win32::System::Com::*;
use windows::Win32::UI::Shell::*;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let folders_only = std::env::args().any(|a| a == "--folders");

    unsafe {
        CoInitializeEx(None, COINIT_APARTMENTTHREADED).ok()?;
    }

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

    unsafe { CoUninitialize() };
    Ok(())
}
