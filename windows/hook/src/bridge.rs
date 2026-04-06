use std::io::Write;
use std::os::windows::process::CommandExt;
use std::process::{Command, Stdio};

const CREATE_NEW_CONSOLE: u32 = 0x00000010;

/// Spawn fzt with a YAML tree file and capture the selection.
/// Uses CREATE_NEW_CONSOLE so fzt gets a visible window for its TUI.
/// stdout is piped to capture the result; fzt renders via CONOUT$.
pub fn run_fzt(
    yaml_content: &str,
    _multi_select: bool,
) -> Result<Vec<String>, Box<dyn std::error::Error>> {
    let fzt_path = find_fzt()?;
    let temp_dir = std::env::var("TEMP").unwrap_or_else(|_| ".".to_string());
    let yaml_path = std::path::Path::new(&temp_dir).join("picker-tree.yaml");

    // Write YAML tree
    {
        let mut f = std::fs::File::create(&yaml_path)?;
        f.write_all(yaml_content.as_bytes())?;
    }

    crate::log(&format!(
        "picker: spawning fzt ({fzt_path}) with yaml at {}",
        yaml_path.display()
    ));

    let mut cmd = Command::new(&fzt_path);

    // CREATE_NEW_CONSOLE gives fzt a visible window for its TUI.
    // fzt renders via CONOUT$, result goes to piped stdout.
    cmd.creation_flags(CREATE_NEW_CONSOLE);
    cmd.stdin(Stdio::null());
    cmd.stdout(Stdio::piped());

    cmd.arg("--yaml").arg(&yaml_path);
    cmd.arg("--border");
    cmd.arg("--accept-nth=2");

    let child = cmd.spawn()?;
    let output = child.wait_with_output()?;

    crate::log(&format!("picker: fzt exited with status={}", output.status));

    // Clean up
    let _ = std::fs::remove_file(&yaml_path);

    if output.status.success() {
        let stdout = String::from_utf8_lossy(&output.stdout);
        let paths: Vec<String> = stdout
            .lines()
            .map(|l| l.trim())
            .filter(|l| !l.is_empty())
            .map(String::from)
            .collect();
        Ok(paths)
    } else {
        Ok(vec![])
    }
}

fn find_fzt() -> Result<String, Box<dyn std::error::Error>> {
    if let Ok(output) = Command::new("where.exe").arg("fzt").output() {
        if output.status.success() {
            let path = String::from_utf8_lossy(&output.stdout);
            if let Some(first) = path.lines().next() {
                let first = first.trim();
                if !first.is_empty() {
                    return Ok(first.to_string());
                }
            }
        }
    }

    let candidates = [
        "D:\\repos\\fzt\\fzt.exe",
        "C:\\Program Files\\fzt\\fzt.exe",
    ];
    for path in &candidates {
        if std::path::Path::new(path).exists() {
            return Ok(path.to_string());
        }
    }

    Err("fzt.exe not found on PATH or in known locations".into())
}
