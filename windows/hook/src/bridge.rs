use std::ffi::{CStr, CString};
use std::path::PathBuf;

use windows::core::PCWSTR;
use windows::Win32::Foundation::{HMODULE, HWND};
use windows::Win32::System::LibraryLoader::{
    GetModuleFileNameW, GetModuleHandleExW, GetProcAddress, LoadLibraryW,
    GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS, GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT,
};

/// Call the Go picker frontend DLL in-process.
/// The DLL creates a Win32 window for modal state, spawns the frontend
/// process via ConPTY, and runs a proper modal message loop.
pub fn run_picker(
    filter: Option<&str>,
    folders_only: bool,
    start_dir: &str,
    owner_hwnd: HWND,
) -> Result<Vec<String>, Box<dyn std::error::Error>> {
    let dll_path = find_picker_dll()?;

    crate::log(&format!(
        "picker: loading frontend DLL ({dll_path}) filter={filter:?} folders={folders_only} dir={start_dir}"
    ));

    unsafe {
        let wide: Vec<u16> = dll_path.encode_utf16().chain(std::iter::once(0)).collect();
        let hmod = LoadLibraryW(windows::core::PCWSTR(wide.as_ptr()))
            .map_err(|e| format!("LoadLibrary failed: {e}"))?;

        // char* PickFile(char* filter, int foldersOnly, char* startDir, uintptr_t hwndOwner)
        let pick_file_addr = GetProcAddress(hmod, windows::core::s!("PickFile"))
            .ok_or("PickFile not found in fzt_picker_frontend.dll")?;
        let pick_file: unsafe extern "C" fn(*const i8, i32, *const i8, usize) -> *mut i8 =
            std::mem::transmute(pick_file_addr);

        let free_string_addr = GetProcAddress(hmod, windows::core::s!("FreeString"))
            .ok_or("FreeString not found in fzt_picker_frontend.dll")?;
        let free_string: unsafe extern "C" fn(*mut i8) = std::mem::transmute(free_string_addr);

        let filter_cstr = filter.map(|f| CString::new(f).unwrap());
        let filter_ptr = filter_cstr
            .as_ref()
            .map(|s| s.as_ptr())
            .unwrap_or(std::ptr::null());

        let start_dir_cstr = CString::new(start_dir).unwrap();
        let folders_flag = if folders_only { 1 } else { 0 };
        let hwnd_val = owner_hwnd.0 as usize;

        crate::log("picker: calling PickFile");
        let result_ptr = pick_file(filter_ptr, folders_flag, start_dir_cstr.as_ptr(), hwnd_val);

        if result_ptr.is_null() {
            crate::log("picker: PickFile returned null (cancelled)");
            return Ok(vec![]);
        }

        let result = CStr::from_ptr(result_ptr).to_string_lossy().to_string();
        free_string(result_ptr);

        crate::log(&format!("picker: PickFile returned: {result}"));

        let paths: Vec<String> = result
            .lines()
            .map(|l| l.trim())
            .filter(|l| !l.is_empty())
            .map(String::from)
            .collect();

        Ok(paths)
    }
}

/// The frontend DLL is installed beside this hook DLL: a release puts both in one
/// version folder, and nothing else is looked at.
fn find_picker_dll() -> Result<String, Box<dyn std::error::Error>> {
    let dir = own_folder()?;
    let candidate = dir.join("fzt_picker_frontend.dll");
    if !candidate.exists() {
        return Err(format!("fzt_picker_frontend.dll is not beside the hook in {}", dir.display()).into());
    }
    Ok(candidate.to_string_lossy().to_string())
}

/// The folder of the module this code was loaded from: the hook DLL in an app, the
/// test binary under cargo test.
fn own_folder() -> Result<PathBuf, Box<dyn std::error::Error>> {
    let mut module = HMODULE::default();
    let mut buffer = vec![0u16; 32768];
    let length = unsafe {
        GetModuleHandleExW(
            GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS | GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT,
            PCWSTR(own_folder as *const () as *const u16),
            &mut module,
        )?;
        GetModuleFileNameW(module, &mut buffer)
    };
    if length == 0 {
        return Err("couldn't read the hook DLL's own path".into());
    }
    let path = PathBuf::from(String::from_utf16_lossy(&buffer[..length as usize]));
    Ok(path.parent().ok_or("the hook DLL's path has no folder")?.to_path_buf())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn own_folder_is_the_loaded_module_folder() {
        let exe = std::env::current_exe().unwrap();
        assert_eq!(own_folder().unwrap(), exe.parent().unwrap());
    }

    #[test]
    fn a_missing_frontend_names_the_folder_it_looked_in() {
        let err = find_picker_dll().unwrap_err().to_string();
        let exe = std::env::current_exe().unwrap();
        assert!(err.contains(&exe.parent().unwrap().display().to_string()), "{err}");
    }
}
