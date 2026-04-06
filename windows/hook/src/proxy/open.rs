use std::sync::Mutex;

use windows::core::*;
use windows::Win32::Foundation::*;
use windows::Win32::System::Com::*;
use windows::Win32::UI::Shell::Common::COMDLG_FILTERSPEC;
use windows::Win32::UI::Shell::*;

use super::state::DialogState;

// FOS flag constants
const FOS_PICKFOLDERS: u32 = 0x20;
const FOS_ALLOWMULTISELECT: u32 = 0x200;

fn shell_item_to_path(si: &IShellItem) -> Option<String> {
    unsafe {
        let name = si.GetDisplayName(SIGDN_FILESYSPATH).ok()?;
        let path = name.to_string().ok()?;
        CoTaskMemFree(Some(name.0 as *const _));
        Some(path)
    }
}

fn path_to_shell_item(path: &str) -> Result<IShellItem> {
    let wide: Vec<u16> = path.encode_utf16().chain(std::iter::once(0)).collect();
    unsafe { SHCreateItemFromParsingName(PCWSTR(wide.as_ptr()), None) }
}

fn alloc_pwstr(s: &str) -> Result<PWSTR> {
    let wide: Vec<u16> = s.encode_utf16().chain(std::iter::once(0)).collect();
    unsafe {
        let size = wide.len() * 2;
        let ptr = CoTaskMemAlloc(size) as *mut u16;
        if ptr.is_null() {
            return Err(Error::from(E_OUTOFMEMORY));
        }
        std::ptr::copy_nonoverlapping(wide.as_ptr(), ptr, wide.len());
        Ok(PWSTR(ptr))
    }
}

#[implement(IFileOpenDialog, IFileDialog, IModalWindow)]
pub struct FileOpenDialogProxy {
    state: Mutex<DialogState>,
}

impl FileOpenDialogProxy {
    pub fn new() -> Self {
        Self {
            state: Mutex::new(DialogState::new()),
        }
    }
}

impl IModalWindow_Impl for FileOpenDialogProxy_Impl {
    fn Show(&self, hwndowner: HWND) -> Result<()> {
        let start_dir;
        let filter;
        let pick_folders;
        let multi_select;

        {
            let mut state = self.state.lock().unwrap();
            state.owner_hwnd = hwndowner;
            start_dir = state.start_directory();
            filter = state.active_filter().map(String::from);
            pick_folders = state.options & FOS_PICKFOLDERS != 0;
            multi_select = state.options & FOS_ALLOWMULTISELECT != 0;
        }

        crate::log(&format!(
            "picker: Show() dir={start_dir} filter={filter:?} folders={pick_folders}"
        ));

        let (yaml, count) = crate::walker::walk_yaml(&start_dir, filter.as_deref(), pick_folders);

        if count == 0 {
            crate::log("picker: no files found in directory");
            return Err(Error::from(HRESULT::from_win32(ERROR_CANCELLED.0)));
        }

        crate::log(&format!("picker: generated YAML tree with {count} items"));

        match crate::bridge::run_fzt(&yaml, multi_select) {
            Ok(paths) if !paths.is_empty() => {
                let mut state = self.state.lock().unwrap();
                state.result_path = Some(paths[0].clone());
                state.result_paths = paths;
                Ok(())
            }
            Ok(_) => Err(Error::from(HRESULT::from_win32(ERROR_CANCELLED.0))),
            Err(e) => {
                crate::log(&format!("picker: fzt failed: {e}"));
                Err(Error::from(HRESULT::from_win32(ERROR_CANCELLED.0)))
            }
        }
    }
}

impl IFileDialog_Impl for FileOpenDialogProxy_Impl {
    fn SetFileTypes(&self, cfiletypes: u32, rgfilterspec: *const COMDLG_FILTERSPEC) -> Result<()> {
        let mut state = self.state.lock().unwrap();
        state.file_types = unsafe {
            std::slice::from_raw_parts(rgfilterspec, cfiletypes as usize)
                .iter()
                .map(|fs| {
                    let name = fs.pszName.to_string().unwrap_or_default();
                    let spec = fs.pszSpec.to_string().unwrap_or_default();
                    (name, spec)
                })
                .collect()
        };
        Ok(())
    }

    fn SetFileTypeIndex(&self, ifiletype: u32) -> Result<()> {
        self.state.lock().unwrap().file_type_index = ifiletype;
        Ok(())
    }

    fn GetFileTypeIndex(&self) -> Result<u32> {
        Ok(self.state.lock().unwrap().file_type_index)
    }

    fn Advise(&self, _pfde: Option<&IFileDialogEvents>) -> Result<u32> {
        Ok(0)
    }

    fn Unadvise(&self, _dwcookie: u32) -> Result<()> {
        Ok(())
    }

    fn SetOptions(&self, fos: FILEOPENDIALOGOPTIONS) -> Result<()> {
        self.state.lock().unwrap().options = fos.0;
        Ok(())
    }

    fn GetOptions(&self) -> Result<FILEOPENDIALOGOPTIONS> {
        Ok(FILEOPENDIALOGOPTIONS(self.state.lock().unwrap().options))
    }

    fn SetDefaultFolder(&self, psi: Option<&IShellItem>) -> Result<()> {
        if let Some(si) = psi {
            if let Some(path) = shell_item_to_path(si) {
                self.state.lock().unwrap().default_folder = Some(path);
            }
        }
        Ok(())
    }

    fn SetFolder(&self, psi: Option<&IShellItem>) -> Result<()> {
        if let Some(si) = psi {
            if let Some(path) = shell_item_to_path(si) {
                self.state.lock().unwrap().folder = Some(path);
            }
        }
        Ok(())
    }

    fn GetFolder(&self) -> Result<IShellItem> {
        let state = self.state.lock().unwrap();
        let dir = state.start_directory();
        drop(state);
        path_to_shell_item(&dir)
    }

    fn GetCurrentSelection(&self) -> Result<IShellItem> {
        self.GetResult()
    }

    fn SetFileName(&self, pszname: &PCWSTR) -> Result<()> {
        let name = unsafe { pszname.to_string().unwrap_or_default() };
        self.state.lock().unwrap().file_name = Some(name);
        Ok(())
    }

    fn GetFileName(&self) -> Result<PWSTR> {
        let state = self.state.lock().unwrap();
        let name = state.file_name.as_deref().unwrap_or("");
        alloc_pwstr(name)
    }

    fn SetTitle(&self, psztitle: &PCWSTR) -> Result<()> {
        let title = unsafe { psztitle.to_string().unwrap_or_default() };
        self.state.lock().unwrap().title = Some(title);
        Ok(())
    }

    fn SetOkButtonLabel(&self, psztext: &PCWSTR) -> Result<()> {
        let label = unsafe { psztext.to_string().unwrap_or_default() };
        self.state.lock().unwrap().ok_label = Some(label);
        Ok(())
    }

    fn SetFileNameLabel(&self, _pszlabel: &PCWSTR) -> Result<()> {
        Ok(())
    }

    fn GetResult(&self) -> Result<IShellItem> {
        let state = self.state.lock().unwrap();
        let path = state
            .result_path
            .as_ref()
            .ok_or_else(|| Error::from(E_FAIL))?
            .clone();
        drop(state);
        path_to_shell_item(&path)
    }

    fn AddPlace(&self, _psi: Option<&IShellItem>, _fdap: FDAP) -> Result<()> {
        Ok(())
    }

    fn SetDefaultExtension(&self, pszdefaultextension: &PCWSTR) -> Result<()> {
        let ext = unsafe { pszdefaultextension.to_string().unwrap_or_default() };
        self.state.lock().unwrap().default_extension = Some(ext);
        Ok(())
    }

    fn Close(&self, _hr: HRESULT) -> Result<()> {
        Ok(())
    }

    fn SetClientGuid(&self, _guid: *const GUID) -> Result<()> {
        Ok(())
    }

    fn ClearClientData(&self) -> Result<()> {
        Ok(())
    }

    fn SetFilter(&self, _pfilter: Option<&IShellItemFilter>) -> Result<()> {
        Ok(())
    }
}

impl IFileOpenDialog_Impl for FileOpenDialogProxy_Impl {
    fn GetResults(&self) -> Result<IShellItemArray> {
        let state = self.state.lock().unwrap();
        if state.result_paths.is_empty() {
            return Err(Error::from(E_FAIL));
        }
        // Use the first result for now
        let path = state.result_paths[0].clone();
        drop(state);

        let item = path_to_shell_item(&path)?;
        unsafe { SHCreateShellItemArrayFromShellItem(&item) }
    }

    fn GetSelectedItems(&self) -> Result<IShellItemArray> {
        self.GetResults()
    }
}
