//! The DLL Windows registers for the global hook, from the one path that never
//! changes (fzt-picker-handoff says why). In each process it loads the hook the
//! injector last published, by that hook's version folder, and moves the detour to
//! it when a newer release publishes. Shims from older releases stay loaded in
//! long-running apps, so what this does with the handoff is a contract.

use std::ffi::c_void;
use std::sync::atomic::{AtomicBool, Ordering};

use fzt_picker_handoff::{log, Handoff, ATTACH, DETACH};
use windows::core::{PCSTR, PCWSTR};
use windows::Win32::Foundation::{BOOL, HINSTANCE, HMODULE, LPARAM, LRESULT, TRUE, WPARAM};
use windows::Win32::System::LibraryLoader::{GetModuleFileNameW, GetProcAddress, LoadLibraryW};
use windows::Win32::System::SystemServices::{DLL_PROCESS_ATTACH, DLL_PROCESS_DETACH};
use windows::Win32::UI::WindowsAndMessaging::{CallNextHookEx, HHOOK};

type Export = unsafe extern "system" fn();

struct Hook {
    path: String,
    attach: Export,
    detach: Export,
}

struct State {
    module: HMODULE,
    handoff: Option<Handoff>,
    /// The process can't read the handoff (a sandbox), so the shim does nothing.
    gave_up: bool,
    generation: u32,
    hook: Option<Hook>,
}

static mut STATE: State = State {
    module: HMODULE(std::ptr::null_mut()),
    handoff: None,
    gave_up: false,
    generation: 0,
    hook: None,
};

/// Held by the one thread looking at STATE; another thread's event skips the work.
static BUSY: AtomicBool = AtomicBool::new(false);

/// The CBT hook procedure. build.rs puts it first in the code section.
#[no_mangle]
pub unsafe extern "system" fn HookProc(code: i32, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
    if BUSY.compare_exchange(false, true, Ordering::Acquire, Ordering::Relaxed).is_ok() {
        follow_handoff(&mut *std::ptr::addr_of_mut!(STATE));
        BUSY.store(false, Ordering::Release);
    }
    CallNextHookEx(HHOOK::default(), code, wparam, lparam)
}

unsafe fn follow_handoff(state: &mut State) {
    if state.gave_up {
        return;
    }
    if state.handoff.is_none() {
        match own_path(state.module).and_then(|path| Handoff::open(&path).map_err(|e| e.to_string())) {
            Ok(handoff) => state.handoff = Some(handoff),
            Err(e) => {
                log(&format!("picker: shim can't read the handoff, so this process keeps the system dialog: {e}"));
                state.gave_up = true;
                return;
            }
        }
    }
    let handoff = state.handoff.as_ref().unwrap();
    if handoff.generation() != state.generation {
        if let Some((generation, path)) = handoff.read() {
            state.generation = generation;
            if !state.hook.as_ref().is_some_and(|hook| hook.path.eq_ignore_ascii_case(&path)) {
                match load(&path) {
                    Ok(hook) => {
                        if let Some(old) = state.hook.take() {
                            (old.detach)();
                        }
                        log(&format!("picker: shim loaded {path}"));
                        state.hook = Some(hook);
                    }
                    Err(e) => log(&format!("picker: shim can't load {path}: {e}")),
                }
            }
        }
    }
    // Every event, as the hook did before the shim: a copy from before the shim may
    // hold the detour until its process unloads it, and this takes over then.
    if let Some(hook) = &state.hook {
        (hook.attach)();
    }
}

unsafe fn load(path: &str) -> Result<Hook, String> {
    let wide: Vec<u16> = path.encode_utf16().chain(std::iter::once(0)).collect();
    let module = LoadLibraryW(PCWSTR(wide.as_ptr())).map_err(|e| e.to_string())?;
    let export = |name: &[u8]| -> Result<Export, String> {
        GetProcAddress(module, PCSTR(name.as_ptr()))
            .map(|f| std::mem::transmute::<_, Export>(f))
            .ok_or_else(|| format!("no {}", String::from_utf8_lossy(&name[..name.len() - 1])))
    };
    Ok(Hook { path: path.to_string(), attach: export(ATTACH)?, detach: export(DETACH)? })
}

unsafe fn own_path(module: HMODULE) -> Result<String, String> {
    let mut buffer = vec![0u16; 32768];
    let length = GetModuleFileNameW(module, &mut buffer) as usize;
    if length == 0 {
        return Err("can't read the shim's own path".into());
    }
    Ok(String::from_utf16_lossy(&buffer[..length]))
}

#[no_mangle]
unsafe extern "system" fn DllMain(module: HINSTANCE, reason: u32, reserved: *mut c_void) -> BOOL {
    let state = &mut *std::ptr::addr_of_mut!(STATE);
    match reason {
        DLL_PROCESS_ATTACH => state.module = HMODULE(module.0),
        // Unloaded because the injector stopped: dialogs go back to the system's.
        // At process exit (reserved set) nothing is left to restore.
        DLL_PROCESS_DETACH if reserved.is_null() => {
            if let Some(hook) = state.hook.take() {
                (hook.detach)();
            }
        }
        _ => {}
    }
    TRUE
}
