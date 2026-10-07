use std::fs;
use std::path::PathBuf;

use desk_ui::live::{self, ActiveTheme};
use desk_ui::theme::Mode;
use gpui::{App, SharedString};

pub const DEFAULT_THEME: &str = "tofu-glass";

pub struct Choice {
    pub name: SharedString,
    dir: PathBuf,
}

pub fn discover() -> Result<Vec<Choice>, String> {
    let dir = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../themes");
    let unlisted = |error| format!("the book cannot list {}: {error}", dir.display());
    let mut found = Vec::new();
    for entry in fs::read_dir(&dir).map_err(unlisted)? {
        let path = entry.map_err(unlisted)?.path();
        let stem = path
            .extension()
            .filter(|extension| *extension == "json")
            .and_then(|_| path.file_stem())
            .and_then(|stem| stem.to_str());
        if let Some(stem) = stem {
            found.push(Choice {
                name: stem.to_owned().into(),
                dir: dir.clone(),
            });
        }
    }
    found.sort_by(|left, right| left.name.cmp(&right.name));
    Ok(found)
}

pub fn parse_mode(name: &str) -> Result<Mode, String> {
    Mode::ALL
        .iter()
        .copied()
        .find(|mode| mode.label() == name)
        .ok_or_else(|| {
            let known: Vec<&str> = Mode::ALL.iter().map(|mode| mode.label()).collect();
            format!("unknown mode {name}; modes: {}", known.join(" "))
        })
}

pub fn set_mode(name: &SharedString, mode: Mode, cx: &mut App) -> Result<(), String> {
    live::set_mode(mode, cx)
        .map_err(|error| format!("theme {name} has no {} set: {error}", mode.label()))
}

pub fn apply(choice: &Choice, mode: Option<Mode>, cx: &mut App) -> Result<(), String> {
    live::start(choice.dir.clone(), choice.name.to_string(), cx)
        .map_err(|error| format!("theme {} does not load: {error}", choice.name))?;
    if let Some(mode) = mode.filter(|mode| *mode != ActiveTheme::mode(cx)) {
        set_mode(&choice.name, mode, cx)?;
    }
    for problem in ActiveTheme::problems(cx).iter() {
        eprintln!("theme {}: {problem}", choice.name);
    }
    cx.refresh_windows();
    Ok(())
}
