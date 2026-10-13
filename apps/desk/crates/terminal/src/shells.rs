use std::env;
use std::fs;
use std::io::ErrorKind;
use std::path::{Path, PathBuf};

const DESK_FOLDER: &str = "tofu-desk";
const PICK_FILE: &str = "terminal-shell";

pub(crate) fn desk_folder() -> Option<PathBuf> {
    let home = |name: &str| {
        env::var_os(name)
            .filter(|value| !value.is_empty())
            .map(PathBuf::from)
    };
    let config = if cfg!(windows) {
        home("APPDATA")
    } else {
        home("XDG_CONFIG_HOME").or_else(|| home("HOME").map(|home| home.join(".config")))
    };
    config.map(|config| config.join(DESK_FOLDER))
}

pub fn name(shell: &Path) -> String {
    shell
        .file_stem()
        .map(|stem| stem.to_string_lossy().to_lowercase())
        .unwrap_or_default()
}

#[cfg(windows)]
pub fn found() -> Vec<PathBuf> {
    let under = |variable: &str, parts: &[&str]| {
        env::var_os(variable).map(|root| {
            parts
                .iter()
                .fold(PathBuf::from(root), |at, part| at.join(part))
        })
    };
    let on_path = env::var_os("PATH").and_then(|paths| {
        env::split_paths(&paths)
            .map(|folder| folder.join("pwsh.exe"))
            .find(|candidate| candidate.is_file())
    });
    let candidates = [
        under("ProgramFiles", &["PowerShell", "7", "pwsh.exe"]),
        on_path,
        under(
            "SystemRoot",
            &["System32", "WindowsPowerShell", "v1.0", "powershell.exe"],
        ),
        env::var_os("ComSpec").map(PathBuf::from),
        under("ProgramFiles", &["Git", "bin", "bash.exe"]),
        under("SystemRoot", &["System32", "wsl.exe"]),
    ];
    let mut found: Vec<PathBuf> = Vec::new();
    for candidate in candidates.into_iter().flatten() {
        let seen = found.iter().any(|known| {
            known
                .as_os_str()
                .eq_ignore_ascii_case(candidate.as_os_str())
        });
        if !seen && candidate.is_file() {
            found.push(candidate);
        }
    }
    found
}

#[cfg(not(windows))]
pub fn found() -> Vec<PathBuf> {
    env::var_os("SHELL")
        .map(PathBuf::from)
        .into_iter()
        .filter(|shell| shell.is_file())
        .collect()
}

fn pick_file() -> Result<PathBuf, String> {
    desk_folder()
        .map(|folder| folder.join(PICK_FILE))
        .ok_or_else(|| "the desk has no config folder for its terminal shell".to_owned())
}

pub fn saved() -> Result<Option<PathBuf>, String> {
    let path = pick_file()?;
    match fs::read_to_string(&path) {
        Ok(text) => Ok(Some(text.trim())
            .filter(|text| !text.is_empty())
            .map(PathBuf::from)),
        Err(error) if error.kind() == ErrorKind::NotFound => Ok(None),
        Err(error) => Err(format!("could not read {}: {error}", path.display())),
    }
}

pub fn save(shell: &Path) -> Result<(), String> {
    let path = pick_file()?;
    path.parent()
        .map_or(Ok(()), fs::create_dir_all)
        .and_then(|()| fs::write(&path, format!("{}\n", shell.display())))
        .map_err(|error| format!("could not write {}: {error}", path.display()))
}

pub fn current() -> PathBuf {
    match saved() {
        Ok(Some(shell)) if shell.is_file() => return shell,
        Ok(Some(shell)) => eprintln!(
            "terminal: the picked shell {} is gone, so the first one found starts",
            shell.display()
        ),
        Ok(None) => {}
        Err(error) => eprintln!("terminal: {error}, so the first shell found starts"),
    }
    found()
        .into_iter()
        .next()
        .unwrap_or_else(|| PathBuf::from(if cfg!(windows) { "cmd.exe" } else { "/bin/sh" }))
}
