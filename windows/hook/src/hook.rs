use std::ffi::c_void;

use retour::static_detour;
use windows::core::GUID;
use windows::Win32::System::LibraryLoader::{GetModuleHandleA, GetProcAddress};
use windows_core::HRESULT;

use crate::proxy;

/// CLSID_FileOpenDialog {DC1C5A9C-E88A-4DDE-A5A1-60F82A20AEF7}
const CLSID_FILE_OPEN_DIALOG: GUID = GUID {
    data1: 0xDC1C5A9C,
    data2: 0xE88A,
    data3: 0x4DDE,
    data4: [0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7],
};

/// CLSID_FileSaveDialog {C0B4E2F3-BA21-4773-8DBA-335EC946EB8B}
const CLSID_FILE_SAVE_DIALOG: GUID = GUID {
    data1: 0xC0B4E2F3,
    data2: 0xBA21,
    data3: 0x4773,
    data4: [0x8D, 0xBA, 0x33, 0x5E, 0xC9, 0x46, 0xEB, 0x8B],
};

// Raw CoCreateInstance ABI: (REFCLSID, LPUNKNOWN, DWORD, REFIID, LPVOID*) -> HRESULT
type FnCoCreateInstance = unsafe extern "system" fn(
    rclsid: *const GUID,
    punk_outer: *mut c_void,
    cls_context: u32,
    riid: *const GUID,
    ppv: *mut *mut c_void,
) -> HRESULT;

static_detour! {
    static CoCreateInstanceDetour: unsafe extern "system" fn(
        *const GUID, *mut c_void, u32, *const GUID, *mut *mut c_void
    ) -> HRESULT;
}

/// Install the CoCreateInstance detour. Called from DllMain on DLL_PROCESS_ATTACH.
///
/// # Safety
/// Must be called under the loader lock (DllMain context). Only does memory patching
/// via retour — no LoadLibrary, no thread creation, no COM calls.
pub unsafe fn install() -> Result<(), Box<dyn std::error::Error>> {
    let module = GetModuleHandleA(windows::core::s!("combase.dll"))?;
    let proc = GetProcAddress(module, windows::core::s!("CoCreateInstance"))
        .ok_or("CoCreateInstance not found in combase.dll")?;
    let original: FnCoCreateInstance = std::mem::transmute(proc);

    CoCreateInstanceDetour
        .initialize(original, |rclsid, punk_outer, cls_context, riid, ppv| unsafe {
            hooked_co_create_instance(rclsid, punk_outer, cls_context, riid, ppv)
        })?
        .enable()?;

    crate::log("picker: CoCreateInstance hook installed");
    Ok(())
}

pub fn uninstall() {
    unsafe {
        let _ = CoCreateInstanceDetour.disable();
    }
}

unsafe fn hooked_co_create_instance(
    rclsid: *const GUID,
    punk_outer: *mut c_void,
    cls_context: u32,
    riid: *const GUID,
    ppv: *mut *mut c_void,
) -> HRESULT {
    let clsid = &*rclsid;

    if *clsid == CLSID_FILE_OPEN_DIALOG {
        crate::log("picker: intercepted FileOpenDialog");
        match proxy::create_file_open_dialog(riid, ppv) {
            Ok(hr) => return hr,
            Err(e) => {
                crate::log(&format!("picker: proxy creation failed, falling back: {e}"));
            }
        }
    }

    if *clsid == CLSID_FILE_SAVE_DIALOG {
        crate::log("picker: intercepted FileSaveDialog (pass-through for now)");
        // Phase 3: return proxy::create_file_save_dialog(riid, ppv)
    }

    // Pass through to original
    CoCreateInstanceDetour.call(rclsid, punk_outer, cls_context, riid, ppv)
}
