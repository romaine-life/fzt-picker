mod open;
mod state;

use std::ffi::c_void;

use windows::core::{Interface, GUID};
use windows_core::HRESULT;

/// Create a FileOpenDialog proxy and return it through the COM ppv out-pointer.
///
/// # Safety
/// `riid` and `ppv` must be valid pointers from the original CoCreateInstance call.
pub unsafe fn create_file_open_dialog(
    riid: *const GUID,
    ppv: *mut *mut c_void,
) -> Result<HRESULT, Box<dyn std::error::Error>> {
    crate::log(&format!(
        "picker: create_file_open_dialog riid={:?}",
        *riid
    ));
    let proxy = open::FileOpenDialogProxy::new();
    let unknown: windows::core::IUnknown = proxy.into();
    let hr = unknown.query(&*riid, ppv as *mut _);
    crate::log(&format!("picker: QueryInterface returned 0x{:08X}", hr.0));
    Ok(hr)
}
