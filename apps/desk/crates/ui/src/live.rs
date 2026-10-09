use std::path::{Component, Path, PathBuf};
use std::sync::Arc;
use std::time::SystemTime;
use std::{fs, thread};

use flume::{Receiver, RecvTimeoutError, Sender};
use gpui::{App, Global};

use crate::metrics::THEME_POLL;
use crate::theme::{Layer, Mode, Problem, Theme, ThemeError, build, build_mode, parse_layer};

const BUILT_IN: &str = include_str!("../../../themes/tofu-glass.json");
const BUILT_IN_FILE: &str = "built-in tofu-glass.json";
pub const THEME_VARIABLE: &str = "TOFU_DESK_THEME";

pub struct ActiveTheme {
    theme: Arc<Theme>,
    problems: Arc<[ThemeError]>,
    layers: Arc<[Layer]>,
}

impl Global for ActiveTheme {}

struct ThemeWatch {
    _stops_on_drop: Sender<()>,
}

impl Global for ThemeWatch {}

impl ActiveTheme {
    pub fn theme(cx: &App) -> Arc<Theme> {
        cx.global::<Self>().theme.clone()
    }

    pub fn problems(cx: &App) -> Arc<[ThemeError]> {
        cx.global::<Self>().problems.clone()
    }

    pub fn mode(cx: &App) -> Mode {
        cx.global::<Self>().theme.mode()
    }
}

struct Loaded {
    layers: Option<Vec<Layer>>,
    problems: Vec<ThemeError>,
    files: Vec<PathBuf>,
}

pub fn start(dir: PathBuf, name: String, cx: &mut App) -> Result<(), ThemeError> {
    let Loaded {
        layers,
        mut problems,
        files,
    } = load(&dir, &name);
    let built = layers.and_then(|layers| match build(&layers, &mut problems) {
        Ok(theme) => Some((layers, theme)),
        Err(error) => {
            problems.insert(0, error);
            None
        }
    });
    let (layers, theme) = match built {
        Some(built) => built,
        None => built_in()?,
    };
    let (sender, receiver) = flume::unbounded();
    let (stop, stopped) = flume::unbounded();
    cx.set_global(ThemeWatch {
        _stops_on_drop: stop,
    });
    let watcher = thread::Builder::new()
        .name("theme watch".into())
        .spawn(move || watch(&dir, &name, files, &sender, &stopped));
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
        layers: layers.into(),
    });
    cx.spawn(async move |cx| {
        while let Ok(loaded) = receiver.recv_async().await {
            cx.update(|cx| apply(loaded, cx));
        }
    })
    .detach();
    Ok(())
}

pub fn set_mode(mode: Mode, cx: &mut App) -> Result<(), ThemeError> {
    let current = cx.global::<ActiveTheme>();
    let layers = current.layers.clone();
    let mut problems = current.problems.to_vec();
    let mut fresh = Vec::new();
    let theme = build_mode(&layers, mode, &mut fresh)?;
    fresh.retain(|problem| !problems.contains(problem));
    problems.append(&mut fresh);
    cx.set_global(ActiveTheme {
        theme: Arc::new(theme),
        problems: problems.into(),
        layers,
    });
    cx.refresh_windows();
    Ok(())
}

fn apply(loaded: Loaded, cx: &mut App) {
    let Loaded {
        layers,
        mut problems,
        ..
    } = loaded;
    let current = cx.global::<ActiveTheme>();
    let (theme, layers) = match layers.map(|layers| {
        let built = build_mode(&layers, current.theme.mode(), &mut problems);
        (built, layers)
    }) {
        Some((Ok(theme), layers)) => (Arc::new(theme), layers.into()),
        Some((Err(error), _)) => {
            problems.insert(0, error);
            (current.theme.clone(), current.layers.clone())
        }
        None => (current.theme.clone(), current.layers.clone()),
    };
    cx.set_global(ActiveTheme {
        theme,
        problems: problems.into(),
        layers,
    });
    cx.refresh_windows();
}

fn watch(
    dir: &Path,
    name: &str,
    mut files: Vec<PathBuf>,
    sender: &Sender<Loaded>,
    stopped: &Receiver<()>,
) {
    let mut seen = stamps(&files);
    while let Err(RecvTimeoutError::Timeout) = stopped.recv_timeout(THEME_POLL) {
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
        if sender.send(loaded).is_err() {
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

fn built_in() -> Result<(Vec<Layer>, Theme), ThemeError> {
    let mut problems = Vec::new();
    let layers = vec![parse_layer(BUILT_IN_FILE, BUILT_IN, &mut problems)?.layer];
    let theme = build(&layers, &mut problems)?;
    match problems.into_iter().next() {
        Some(problem) => Err(problem),
        None => Ok((layers, theme)),
    }
}

fn load(dir: &Path, name: &str) -> Loaded {
    let mut problems = Vec::new();
    let mut files = Vec::new();
    let layers = match chain(dir, name, &mut files, &mut problems) {
        Ok(layers) => Some(layers),
        Err(error) => {
            problems.insert(0, error);
            None
        }
    };
    Loaded {
        layers,
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
