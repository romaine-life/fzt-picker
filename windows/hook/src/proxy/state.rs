use windows::Win32::Foundation::HWND;

/// Internal state shared across IFileDialog method calls.
pub struct DialogState {
    pub folder: Option<String>,
    pub default_folder: Option<String>,
    pub file_types: Vec<(String, String)>, // (display name, pattern e.g. "*.txt;*.md")
    pub file_type_index: u32,
    pub options: u32, // FILEOPENDIALOGOPTIONS bits
    pub title: Option<String>,
    pub file_name: Option<String>,
    pub ok_label: Option<String>,
    pub default_extension: Option<String>,
    pub result_path: Option<String>,
    pub result_paths: Vec<String>,
    pub owner_hwnd: HWND,
}

impl DialogState {
    pub fn new() -> Self {
        Self {
            folder: None,
            default_folder: None,
            file_types: Vec::new(),
            file_type_index: 0,
            options: 0,
            title: None,
            file_name: None,
            ok_label: None,
            default_extension: None,
            result_path: None,
            result_paths: Vec::new(),
            owner_hwnd: HWND::default(),
        }
    }

    /// Resolve the starting directory for file listing.
    /// Priority: SetFolder > SetDefaultFolder > user home
    pub fn start_directory(&self) -> String {
        if let Some(ref f) = self.folder {
            return f.clone();
        }
        if let Some(ref f) = self.default_folder {
            return f.clone();
        }
        std::env::var("USERPROFILE").unwrap_or_else(|_| "C:\\".to_string())
    }

    /// Get the active file type filter pattern (e.g. "*.txt;*.md"), if any.
    pub fn active_filter(&self) -> Option<&str> {
        if self.file_types.is_empty() {
            return None;
        }
        let idx = if self.file_type_index > 0 {
            (self.file_type_index - 1) as usize // 1-based index
        } else {
            0
        };
        self.file_types
            .get(idx)
            .map(|(_, pattern)| pattern.as_str())
    }
}
