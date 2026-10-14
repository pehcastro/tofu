use std::io::ErrorKind;
use std::path::PathBuf;
use std::{env, fs};

use desk_core::home::Home;
use desk_ui::live::{self, ActiveTheme, THEME_VARIABLE};
use desk_ui::theme::Mode;
use gpui::App;

const DEFAULT_THEME: &str = "tofu-glass";
const FILE: &str = "theme";

pub struct Pick {
    pub theme: String,
    pub mode: Option<Mode>,
}

pub fn folder() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../themes")
}

fn saved_at() -> Result<PathBuf, String> {
    Home::find()
        .map(|home| home.config.join(FILE))
        .map_err(|error| error.to_string())
}

pub fn chosen() -> Result<Pick, String> {
    if let Some(name) = env::var_os(THEME_VARIABLE) {
        return Ok(Pick {
            theme: name.to_string_lossy().into_owned(),
            mode: None,
        });
    }
    let path = saved_at()?;
    let text = match fs::read_to_string(&path) {
        Ok(text) => text,
        Err(error) if error.kind() == ErrorKind::NotFound => {
            return Ok(Pick {
                theme: DEFAULT_THEME.to_owned(),
                mode: None,
            });
        }
        Err(error) => return Err(format!("{}: {error}", path.display())),
    };
    let parsed = text.trim().split_once(' ').and_then(|(theme, mode)| {
        let mode = Mode::ALL
            .iter()
            .copied()
            .find(|known| known.label() == mode)?;
        Some(Pick {
            theme: theme.to_owned(),
            mode: Some(mode),
        })
    });
    parsed.ok_or_else(|| format!("{} holds {text:?}, not a theme and a mode", path.display()))
}

#[cfg(feature = "screen-theme")]
pub fn save(theme: &str, mode: Mode) -> Result<(), String> {
    let path = saved_at()?;
    let failed = |error: std::io::Error| {
        format!(
            "the theme pick cannot be saved to {}: {error}",
            path.display()
        )
    };
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).map_err(failed)?;
    }
    fs::write(&path, format!("{theme} {}\n", mode.label())).map_err(failed)
}

pub fn start(theme: &str, cx: &mut App) -> Result<(), String> {
    live::start(folder(), theme.to_owned(), cx)
        .map_err(|error| format!("tofu desk has no theme {theme} to draw with: {error}"))?;
    for problem in ActiveTheme::problems(cx).iter() {
        eprintln!("theme {theme}: {problem}");
    }
    Ok(())
}

pub fn set_mode(theme: &str, mode: Option<Mode>, cx: &mut App) -> Result<(), String> {
    if let Some(mode) = mode.filter(|mode| *mode != ActiveTheme::mode(cx)) {
        live::set_mode(mode, cx)
            .map_err(|error| format!("theme {theme} has no {} set: {error}", mode.label()))?;
    }
    eprintln!("desk theme {theme} {}", ActiveTheme::mode(cx).label());
    cx.refresh_windows();
    Ok(())
}
