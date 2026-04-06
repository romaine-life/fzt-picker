use std::collections::BTreeMap;
use std::path::Path;
use std::process::Command;

/// A tree node representing a file or directory.
/// Serializes to fzt's YAML format: [{name, description, children: [...]}]
struct Node {
    children: BTreeMap<String, Node>,
    files: Vec<(String, String)>, // (display_name, full_path)
}

impl Node {
    fn new() -> Self {
        Self {
            children: BTreeMap::new(),
            files: Vec::new(),
        }
    }

    fn has_content(&self) -> bool {
        !self.files.is_empty() || self.children.values().any(|c| c.has_content())
    }

    fn write_yaml(&self, buf: &mut String, indent: usize) {
        let pad = " ".repeat(indent);

        for (name, child) in &self.children {
            if !child.has_content() {
                continue;
            }
            buf.push_str(&format!("{pad}- name: {}\n", yaml_escape(name)));
            buf.push_str(&format!("{pad}  children:\n"));
            child.write_yaml(buf, indent + 4);
        }

        for (name, full_path) in &self.files {
            buf.push_str(&format!("{pad}- name: {}\n", yaml_escape(name)));
            buf.push_str(&format!("{pad}  description: {}\n", yaml_escape(full_path)));
        }
    }

    /// Insert a path into the tree. Components are the path parts (drive, folders, file).
    /// The last component is treated as a file leaf with the full_path as description.
    fn insert_file(&mut self, components: &[&str], full_path: &str) {
        if components.is_empty() {
            return;
        }
        if components.len() == 1 {
            // Leaf file
            self.files
                .push((components[0].to_string(), full_path.to_string()));
        } else {
            // Directory — descend
            let dir = self
                .children
                .entry(components[0].to_string())
                .or_insert_with(Node::new);
            dir.insert_file(&components[1..], full_path);
        }
    }

    /// Insert a directory path into the tree (no file leaf).
    fn insert_dir(&mut self, components: &[&str]) {
        if components.is_empty() {
            return;
        }
        let dir = self
            .children
            .entry(components[0].to_string())
            .or_insert_with(Node::new);
        if components.len() > 1 {
            dir.insert_dir(&components[1..]);
        }
    }
}

fn yaml_escape(s: &str) -> String {
    format!("'{}'", s.replace('\'', "''"))
}

/// Find the Everything CLI (es.exe).
fn find_es() -> Option<String> {
    // Check PATH
    if let Ok(output) = Command::new("where.exe").arg("es").output() {
        if output.status.success() {
            let path = String::from_utf8_lossy(&output.stdout);
            if let Some(first) = path.lines().next() {
                let first = first.trim();
                if !first.is_empty() && first.to_lowercase().contains("everything") {
                    return Some(first.to_string());
                }
            }
        }
    }

    // Check known winget location
    if let Ok(local) = std::env::var("LOCALAPPDATA") {
        let winget_path = format!(
            "{}\\Microsoft\\WinGet\\Packages\\voidtools.Everything.Cli_Microsoft.Winget.Source_8wekyb3d8bbwe\\es.exe",
            local
        );
        if Path::new(&winget_path).exists() {
            return Some(winget_path);
        }
    }

    // Check Program Files
    let candidates = [
        "C:\\Program Files\\Everything\\es.exe",
        "C:\\Program Files\\Everything 1.5a\\es.exe",
    ];
    for path in &candidates {
        if Path::new(path).exists() {
            return Some(path.to_string());
        }
    }

    None
}

/// Query Everything for files, build a YAML tree with drives as top-level nodes.
pub fn walk_yaml(
    focused_dir: &str,
    filter_pattern: Option<&str>,
    folders_only: bool,
) -> (String, usize) {
    let es = match find_es() {
        Some(path) => path,
        None => {
            crate::log("picker: es.exe not found, falling back to walkdir");
            return walk_yaml_fallback(focused_dir, filter_pattern, folders_only);
        }
    };

    // Build the Everything query — no path restriction so results span all drives.
    // File extension filter (e.g. "*.txt;*.md" → "ext:txt;md")
    let mut search_terms = String::new();
    if let Some(pattern) = filter_pattern {
        let exts: Vec<&str> = pattern
            .split(';')
            .filter_map(|p| p.trim().strip_prefix("*."))
            .filter(|e| *e != "*" && !e.is_empty())
            .collect();
        if !exts.is_empty() {
            search_terms = format!("ext:{}", exts.join(";"));
        }
    }

    let mut args: Vec<String> = Vec::new();
    args.push("-n".to_string());
    args.push("10000".to_string());

    if folders_only {
        args.push("/ad".to_string());
    } else {
        args.push("/a-d".to_string());
    }

    crate::log(&format!("picker: querying Everything: es {} {}", args.join(" "), search_terms));

    let mut cmd = Command::new(&es);
    for arg in &args {
        cmd.arg(arg);
    }
    if !search_terms.is_empty() {
        cmd.arg(&search_terms);
    }

    let output = match cmd.output() {
        Ok(o) => o,
        Err(e) => {
            crate::log(&format!("picker: es.exe failed: {e}"));
            return walk_yaml_fallback(focused_dir, filter_pattern, folders_only);
        }
    };

    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        crate::log(&format!("picker: es.exe error: {stderr}"));
        return walk_yaml_fallback(focused_dir, filter_pattern, folders_only);
    }

    let stdout = String::from_utf8_lossy(&output.stdout);
    let paths: Vec<&str> = stdout.lines().filter(|l| !l.is_empty()).collect();

    crate::log(&format!("picker: Everything returned {} results", paths.len()));

    // Build tree from paths
    let mut root = Node::new();
    let mut count = 0;

    for path_str in &paths {
        let path = Path::new(path_str);
        let components: Vec<&str> = path
            .components()
            .map(|c| c.as_os_str().to_str().unwrap_or("?"))
            .collect();

        if components.is_empty() {
            continue;
        }

        // First component is the drive prefix (e.g. "C:"), second is root "\"
        // Skip the root dir separator — merge drive + contents directly
        let drive = components[0].trim_end_matches('\\');
        let mut parts = vec![drive];
        for c in &components[1..] {
            if *c != "\\" && *c != "/" {
                parts.push(c);
            }
        }

        if folders_only {
            root.insert_dir(&parts);
        } else {
            root.insert_file(&parts, path_str);
        }
        count += 1;
    }

    let mut yaml = String::new();
    root.write_yaml(&mut yaml, 0);
    (yaml, count)
}

/// Fallback walker using walkdir when Everything isn't available.
fn walk_yaml_fallback(
    root: &str,
    filter_pattern: Option<&str>,
    folders_only: bool,
) -> (String, usize) {
    use walkdir::WalkDir;

    let root_path = Path::new(root);
    let mut tree = Node::new();
    let mut count = 0;

    for entry in WalkDir::new(root)
        .follow_links(true)
        .max_depth(5)
        .into_iter()
        .filter_map(|e| e.ok())
    {
        if entry.depth() == 0 {
            continue;
        }

        let is_dir = entry.file_type().is_dir();
        let path = entry.path();

        if !is_dir && !folders_only {
            if let Some(pattern) = filter_pattern {
                if !matches_filter(path, pattern) {
                    continue;
                }
            }
        }

        if folders_only && !is_dir {
            continue;
        }

        let rel = match path.strip_prefix(root_path) {
            Ok(r) => r,
            Err(_) => continue,
        };

        let components: Vec<&str> = rel
            .components()
            .map(|c| c.as_os_str().to_str().unwrap_or("?"))
            .collect();

        if components.is_empty() {
            continue;
        }

        if is_dir {
            tree.insert_dir(&components);
            if folders_only {
                count += 1;
            }
        } else {
            let full_path = path.to_string_lossy().to_string();
            tree.insert_file(&components, &full_path);
            count += 1;
        }
    }

    let mut yaml = String::new();
    tree.write_yaml(&mut yaml, 0);
    (yaml, count)
}

fn matches_filter(path: &Path, pattern: &str) -> bool {
    let ext = path
        .extension()
        .and_then(|e| e.to_str())
        .unwrap_or("")
        .to_lowercase();

    for part in pattern.split(';') {
        let part = part.trim();
        if part == "*.*" || part == "*" {
            return true;
        }
        if let Some(expected_ext) = part.strip_prefix("*.") {
            if ext == expected_ext.to_lowercase() {
                return true;
            }
        }
    }
    false
}
