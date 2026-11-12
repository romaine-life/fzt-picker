//! What the injector, the shim and the hook agree on. Windows registers a global
//! hook's DLL by its path in a small fixed table and keeps the entry while any
//! process holds that DLL, so a path per release filled the table and Windows
//! refused new releases (RLFP-1). Windows registers only the shim, from one path
//! that never changes; the injector publishes the hook of its own release here, and
//! the shim in each process loads that hook by its version folder. Changing this
//! contract means new names: shims from older releases stay loaded and read it.

use std::sync::atomic::{fence, AtomicU32, Ordering};

use windows::core::PCWSTR;
use windows::Win32::Foundation::{CloseHandle, HANDLE, INVALID_HANDLE_VALUE};
use windows::Win32::System::Memory::{
    CreateFileMappingW, MapViewOfFile, OpenFileMappingW, FILE_MAP_ALL_ACCESS, FILE_MAP_READ,
    MEMORY_MAPPED_VIEW_ADDRESS, PAGE_READWRITE,
};

/// The hook's exports the shim calls: claim the CoCreateInstance detour if no
/// other copy holds it, and give it up.
pub const ATTACH: &[u8] = b"FztPickerAttach\0";
pub const DETACH: &[u8] = b"FztPickerDetach\0";

const PATH_UNITS: usize = 1024;

#[repr(C)]
struct Shared {
    /// Odd while the injector writes. Each publish moves it on, so it is also the
    /// generation the shim compares.
    seq: AtomicU32,
    len: u32,
    path: [u16; PATH_UNITS],
}

/// The mapping's name: one per shim path, so a test layout elsewhere on the disk
/// never reaches the installed one.
pub fn mapping_name(shim_path: &str) -> String {
    let mut hash: u64 = 0xcbf29ce484222325;
    for byte in shim_path.to_lowercase().bytes() {
        hash ^= u64::from(byte);
        hash = hash.wrapping_mul(0x100000001b3);
    }
    format!("Local\\fzt-picker-handoff-{hash:016x}")
}

fn wide(text: &str) -> Vec<u16> {
    text.encode_utf16().chain(std::iter::once(0)).collect()
}

struct Owned(HANDLE);

impl Drop for Owned {
    fn drop(&mut self) {
        unsafe {
            let _ = CloseHandle(self.0);
        }
    }
}

/// A view of the mapping, held by the injector for its life and by a shim for the
/// life of its process.
pub struct Handoff {
    view: *mut Shared,
    _mapping: Owned,
}

unsafe impl Send for Handoff {}
unsafe impl Sync for Handoff {}

impl Handoff {
    /// The injector's side: create the mapping, or open the one shims still hold.
    pub fn create(shim_path: &str) -> windows::core::Result<Self> {
        let name = wide(&mapping_name(shim_path));
        unsafe {
            let size = std::mem::size_of::<Shared>() as u32;
            let mapping = Owned(CreateFileMappingW(
                INVALID_HANDLE_VALUE,
                None,
                PAGE_READWRITE,
                0,
                size,
                PCWSTR(name.as_ptr()),
            )?);
            let view = MapViewOfFile(mapping.0, FILE_MAP_ALL_ACCESS, 0, 0, 0);
            Self::from_view(view, mapping)
        }
    }

    /// The shim's side. Fails before the injector has created it, and in a
    /// sandboxed process that can't open it.
    pub fn open(shim_path: &str) -> windows::core::Result<Self> {
        let name = wide(&mapping_name(shim_path));
        unsafe {
            let mapping = Owned(OpenFileMappingW(FILE_MAP_READ.0, false, PCWSTR(name.as_ptr()))?);
            let view = MapViewOfFile(mapping.0, FILE_MAP_READ, 0, 0, 0);
            Self::from_view(view, mapping)
        }
    }

    fn from_view(view: MEMORY_MAPPED_VIEW_ADDRESS, mapping: Owned) -> windows::core::Result<Self> {
        if view.Value.is_null() {
            return Err(windows::core::Error::from_win32());
        }
        Ok(Self { view: view.Value.cast(), _mapping: mapping })
    }

    /// Point every shim at this hook DLL.
    pub fn publish(&self, hook_path: &str) -> Result<(), String> {
        let units: Vec<u16> = hook_path.encode_utf16().collect();
        if units.len() > PATH_UNITS {
            return Err(format!("the hook's path is longer than {PATH_UNITS} characters: {hook_path}"));
        }
        unsafe {
            let shared = &*self.view;
            let writing = shared.seq.load(Ordering::Relaxed) | 1;
            shared.seq.store(writing, Ordering::Relaxed);
            fence(Ordering::Release);
            let path = std::ptr::addr_of_mut!((*self.view).path).cast::<u16>();
            for (i, unit) in units.iter().enumerate() {
                path.add(i).write_volatile(*unit);
            }
            std::ptr::addr_of_mut!((*self.view).len).write_volatile(units.len() as u32);
            shared.seq.store(writing.wrapping_add(1), Ordering::Release);
        }
        Ok(())
    }

    /// The generation and hook path last published; None before the first
    /// publish and while one is being written.
    pub fn read(&self) -> Option<(u32, String)> {
        unsafe {
            let shared = &*self.view;
            let before = shared.seq.load(Ordering::Acquire);
            if before == 0 || before & 1 == 1 {
                return None;
            }
            let len = (std::ptr::addr_of!((*self.view).len).read_volatile() as usize).min(PATH_UNITS);
            let path = std::ptr::addr_of!((*self.view).path).cast::<u16>();
            let units: Vec<u16> = (0..len).map(|i| path.add(i).read_volatile()).collect();
            fence(Ordering::Acquire);
            if shared.seq.load(Ordering::Relaxed) != before {
                return None;
            }
            Some((before, String::from_utf16_lossy(&units)))
        }
    }

    /// The generation alone: what the shim checks on every hook event.
    pub fn generation(&self) -> u32 {
        unsafe { (*self.view).seq.load(Ordering::Acquire) }
    }
}

/// Append a line to %TEMP%\picker.log, prefixed with the process ID. Opened and
/// closed each time, since every hooked process writes to it.
pub fn log(msg: &str) {
    use std::io::Write;
    let Ok(temp) = std::env::var("TEMP") else {
        return;
    };
    let path = std::path::Path::new(&temp).join("picker.log");
    let Ok(mut f) = std::fs::OpenOptions::new().create(true).append(true).open(&path) else {
        return;
    };
    let _ = writeln!(f, "[{}] {msg}", std::process::id());
}

#[cfg(test)]
mod tests {
    use super::*;

    fn shim(tag: &str) -> String {
        format!(r"D:\handoff-test\{tag}-{}\fzt_picker_shim.dll", std::process::id())
    }

    #[test]
    fn a_shim_reads_what_the_injector_published() {
        let shim = shim("read");
        let injector = Handoff::create(&shim).unwrap();
        let reader = Handoff::open(&shim).unwrap();
        assert_eq!(reader.read(), None);
        injector.publish(r"C:\fzt-picker\v1\fzt_picker_hook.dll").unwrap();
        let (first, path) = reader.read().unwrap();
        assert_eq!(path, r"C:\fzt-picker\v1\fzt_picker_hook.dll");
        injector.publish(r"C:\fzt-picker\v2\fzt_picker_hook.dll").unwrap();
        let (second, path) = reader.read().unwrap();
        assert_eq!(path, r"C:\fzt-picker\v2\fzt_picker_hook.dll");
        assert_ne!(first, second);
        assert_eq!(reader.generation(), second);
    }

    #[test]
    fn a_new_injector_reaches_shims_holding_the_old_mapping() {
        let shim = shim("restart");
        let reader = {
            let old = Handoff::create(&shim).unwrap();
            old.publish(r"C:\v1\fzt_picker_hook.dll").unwrap();
            Handoff::open(&shim).unwrap()
        };
        Handoff::create(&shim).unwrap().publish(r"C:\v2\fzt_picker_hook.dll").unwrap();
        assert_eq!(reader.read().unwrap().1, r"C:\v2\fzt_picker_hook.dll");
    }

    #[test]
    fn the_name_ignores_case_and_differs_by_path() {
        assert_eq!(mapping_name(r"C:\A\fzt_picker_shim.dll"), mapping_name(r"c:\a\FZT_PICKER_SHIM.DLL"));
        assert_ne!(mapping_name(r"C:\a\fzt_picker_shim.dll"), mapping_name(r"C:\b\fzt_picker_shim.dll"));
    }

    #[test]
    fn a_path_too_long_is_refused() {
        let injector = Handoff::create(&shim("long")).unwrap();
        assert!(injector.publish(&"x".repeat(PATH_UNITS + 1)).is_err());
    }
}
