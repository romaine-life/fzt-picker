mod bridge;
mod hook;
mod proxy;

use std::ffi::c_void;
use windows::Win32::Foundation::{BOOL, HINSTANCE, TRUE};
use windows::Win32::System::SystemServices::DLL_PROCESS_DETACH;

pub(crate) use fzt_picker_handoff::log;

/// Called by the shim on each hook event: install the detour unless another copy
/// of this DLL owns it (hook::install).
///
/// # Safety
/// Called by the shim from its hook procedure, on the thread of the event.
#[no_mangle]
pub unsafe extern "system" fn FztPickerAttach() {
    if let Err(e) = hook::install() {
        log(&format!("picker: hook install failed: {e}"));
    }
}

/// Called by the shim when a newer release takes over, or when the injector stops.
///
/// # Safety
/// Called by the shim, which holds this DLL loaded until the call returns.
#[no_mangle]
pub unsafe extern "system" fn FztPickerDetach() {
    hook::uninstall();
}

#[no_mangle]
unsafe extern "system" fn DllMain(_hinstance: HINSTANCE, reason: u32, _reserved: *mut c_void) -> BOOL {
    if reason == DLL_PROCESS_DETACH {
        hook::uninstall();
    }
    TRUE
}
