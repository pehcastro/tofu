use std::env;
use std::fmt;
use std::fs;
use std::io::{self, ErrorKind};
use std::path::{Path, PathBuf};

const LEGACY_FOLDER: &str = "tofu-desk";
const LEGACY_TO_CONFIG: [&str; 2] = ["theme", "terminal-shell"];
const LEGACY_TO_STATE: [&str; 5] = [
    "layouts",
    "projects",
    "editor-icons",
    "recents.json",
    "emails.json",
];

#[derive(Debug)]
pub enum HomeError {
    NoHome(&'static str),
    Io { path: PathBuf, error: io::Error },
}

impl fmt::Display for HomeError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            HomeError::NoHome(name) => write!(f, "{name} is not set, so the desk has no home"),
            HomeError::Io { path, error } => write!(f, "{}: {error}", path.display()),
        }
    }
}

impl std::error::Error for HomeError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            HomeError::Io { error, .. } => Some(error),
            HomeError::NoHome(_) => None,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Home {
    pub config: PathBuf,
    pub state: PathBuf,
    pub setup: PathBuf,
    pub runtime: PathBuf,
    pub cache: PathBuf,
    pub logs: PathBuf,
}

fn variable(name: &str) -> Option<PathBuf> {
    env::var_os(name)
        .filter(|value| !value.is_empty())
        .map(PathBuf::from)
}

fn user_home() -> Result<PathBuf, HomeError> {
    let name = if cfg!(windows) { "USERPROFILE" } else { "HOME" };
    variable(name).ok_or(HomeError::NoHome(name))
}

fn legacy_folder() -> Option<PathBuf> {
    let config = if cfg!(windows) {
        variable("APPDATA")
    } else {
        variable("XDG_CONFIG_HOME").or_else(|| variable("HOME").map(|home| home.join(".config")))
    };
    config.map(|config| config.join(LEGACY_FOLDER))
}

impl Home {
    pub fn find() -> Result<Home, HomeError> {
        Ok(Home::at(&user_home()?.join(".tofu").join("desk")))
    }

    pub fn at(root: &Path) -> Home {
        Home {
            config: root.join("config"),
            state: root.join("state"),
            setup: root.join("setup"),
            runtime: root.join("runtime"),
            cache: root.join("cache"),
            logs: root.join("logs"),
        }
    }

    pub fn create(&self) -> Result<(), HomeError> {
        for folder in [
            &self.config,
            &self.state,
            &self.setup,
            &self.runtime,
            &self.cache,
            &self.logs,
        ] {
            fs::create_dir_all(folder).map_err(|error| HomeError::Io {
                path: folder.clone(),
                error,
            })?;
        }
        Ok(())
    }

    pub fn migrate(&self) -> Result<(), HomeError> {
        self.create()?;
        let Some(legacy) = legacy_folder() else {
            return Ok(());
        };
        let targets = LEGACY_TO_CONFIG
            .iter()
            .map(|name| (*name, &self.config))
            .chain(LEGACY_TO_STATE.iter().map(|name| (*name, &self.state)));
        for (name, target) in targets {
            move_tree(&legacy.join(name), &target.join(name))?;
        }
        Ok(())
    }
}

fn move_tree(from: &Path, to: &Path) -> Result<(), HomeError> {
    let failed = |path: &Path| {
        let path = path.to_owned();
        move |error| HomeError::Io { path, error }
    };
    let mut pending = vec![(from.to_owned(), to.to_owned())];
    while let Some((from, to)) = pending.pop() {
        let meta = match fs::symlink_metadata(&from) {
            Ok(meta) => meta,
            Err(error) if error.kind() == ErrorKind::NotFound => continue,
            Err(error) => return Err(failed(&from)(error)),
        };
        if meta.is_dir() {
            fs::create_dir_all(&to).map_err(failed(&to))?;
            for entry in fs::read_dir(&from).map_err(failed(&from))? {
                let name = entry.map_err(failed(&from))?.file_name();
                pending.push((from.join(&name), to.join(&name)));
            }
            continue;
        }
        if fs::symlink_metadata(&to).is_ok() {
            continue;
        }
        fs::rename(&from, &to).map_err(failed(&from))?;
        eprintln!("desk: home moved {} -> {}", from.display(), to.display());
    }
    Ok(())
}
