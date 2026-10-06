use std::path::{Component, Path, PathBuf};
use std::sync::Arc;
use std::time::SystemTime;
use std::{fs, thread};

use async_channel::Sender;
use gpui::{App, Global};

use crate::metrics::THEME_POLL;
use crate::theme::{Layer, Problem, Theme, ThemeError, build, parse_layer};

const BUILT_IN: &str = include_str!("../../../themes/tofu-glass.json");
const BUILT_IN_FILE: &str = "built-in tofu-glass.json";
pub const THEME_VARIABLE: &str = "TOFU_DESK_THEME";

pub struct ActiveTheme {
    theme: Arc<Theme>,
    problems: Arc<[ThemeError]>,
}

impl Global for ActiveTheme {}

impl ActiveTheme {
    pub fn theme(cx: &App) -> Arc<Theme> {
        cx.global::<Self>().theme.clone()
    }

    pub fn problems(cx: &App) -> Arc<[ThemeError]> {
        cx.global::<Self>().problems.clone()
    }
}

struct Loaded {
    theme: Option<Theme>,
    problems: Vec<ThemeError>,
    files: Vec<PathBuf>,
}

pub fn start(dir: PathBuf, name: String, cx: &mut App) -> Result<(), ThemeError> {
    let Loaded {
        theme,
        mut problems,
        files,
    } = load(&dir, &name);
    let theme = match theme {
        Some(theme) => theme,
        None => built_in()?,
    };
    let (sender, receiver) = async_channel::unbounded();
    let watcher = thread::Builder::new()
        .name("theme watch".into())
        .spawn(move || watch(&dir, &name, files, &sender));
    if let Err(error) = watcher {
        problems.push(ThemeError::new(
            THEME_VARIABLE,
            "",
            Problem::Watch(error.to_string()),
        ));
    }
    cx.set_global(ActiveTheme {
        theme: Arc::new(theme),
        problems: problems.into(),
    });
    cx.spawn(async move |cx| {
        while let Ok(loaded) = receiver.recv().await {
            cx.update(|cx| apply(loaded, cx));
        }
    })
    .detach();
    Ok(())
}

fn apply(loaded: Loaded, cx: &mut App) {
    let theme = match loaded.theme {
        Some(theme) => Arc::new(theme),
        None => ActiveTheme::theme(cx),
    };
    cx.set_global(ActiveTheme {
        theme,
        problems: loaded.problems.into(),
    });
    cx.refresh_windows();
}

fn watch(dir: &Path, name: &str, mut files: Vec<PathBuf>, sender: &Sender<Loaded>) {
    let mut seen = stamps(&files);
    loop {
        thread::sleep(THEME_POLL);
        let current = stamps(&files);
        if current == seen {
            continue;
        }
        let loaded = load(dir, name);
        seen = if loaded.files == files {
            current
        } else {
            stamps(&loaded.files)
        };
        files.clone_from(&loaded.files);
        if sender.send_blocking(loaded).is_err() {
            return;
        }
    }
}

fn stamps(files: &[PathBuf]) -> Vec<Option<SystemTime>> {
    files
        .iter()
        .map(|file| fs::metadata(file).and_then(|meta| meta.modified()).ok())
        .collect()
}

fn built_in() -> Result<Theme, ThemeError> {
    let mut problems = Vec::new();
    let parsed = parse_layer(BUILT_IN_FILE, BUILT_IN, &mut problems)?;
    let theme = build(&[parsed.layer], &mut problems)?;
    match problems.into_iter().next() {
        Some(problem) => Err(problem),
        None => Ok(theme),
    }
}

fn load(dir: &Path, name: &str) -> Loaded {
    let mut problems = Vec::new();
    let mut files = Vec::new();
    let theme = match chain(dir, name, &mut files, &mut problems)
        .and_then(|layers| build(&layers, &mut problems))
    {
        Ok(theme) => Some(theme),
        Err(error) => {
            problems.insert(0, error);
            None
        }
    };
    Loaded {
        theme,
        problems,
        files,
    }
}

fn chain(
    dir: &Path,
    name: &str,
    files: &mut Vec<PathBuf>,
    problems: &mut Vec<ThemeError>,
) -> Result<Vec<Layer>, ThemeError> {
    let mut layers = Vec::new();
    let mut trail: Vec<String> = Vec::new();
    let mut referrer = (THEME_VARIABLE.to_owned(), "");
    let mut next = Some(name.to_owned());
    while let Some(name) = next.take() {
        let fail = |problem| ThemeError::new(&referrer.0, referrer.1, problem);
        if trail.contains(&name) {
            trail.push(name);
            return Err(fail(Problem::ExtendsLoop(trail)));
        }
        let file = file_name(&name).ok_or_else(|| fail(Problem::NotAName(name.clone())))?;
        let path = dir.join(&file);
        files.push(path.clone());
        let text = fs::read_to_string(&path)
            .map_err(|error| fail(Problem::Unreadable(format!("{}: {error}", path.display()))))?;
        let parsed = parse_layer(&file, &text, problems)?;
        layers.push(parsed.layer);
        trail.push(name);
        next = parsed.extends;
        referrer = (file, "extends");
    }
    layers.push(parse_layer(BUILT_IN_FILE, BUILT_IN, problems)?.layer);
    Ok(layers)
}

fn file_name(name: &str) -> Option<String> {
    let mut components = Path::new(name).components();
    match (components.next(), components.next()) {
        (Some(Component::Normal(_)), None) => Some(format!("{name}.json")),
        _ => None,
    }
}
