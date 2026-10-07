use std::env;
use std::fs;
use std::io::ErrorKind;
use std::path::{Path, PathBuf};
use std::time::SystemTime;

use desk_core::git::{Git, GitBinary, State};
use desk_core::sessions::SessionRow;
use desk_tiling::Store as Layouts;
use desk_ui::components::sidebar::{Project, Session};

const RECENTS: &str = "recents.json";
const MOST_RECENT: usize = 8;
const RUNNING: &str = "Running";

#[derive(Clone)]
pub struct Head {
    pub folder: PathBuf,
    pub branch: String,
    pub changed: usize,
}

pub fn launch() -> Result<PathBuf, String> {
    env::current_dir()
        .map_err(|error| format!("the desk cannot read the folder it runs in: {error}"))
}

pub fn name(folder: &Path) -> String {
    folder.file_name().map_or_else(
        || folder.display().to_string(),
        |name| name.to_string_lossy().into_owned(),
    )
}

fn recents_file() -> Result<PathBuf, String> {
    Layouts::home()
        .map(|home| home.join(RECENTS))
        .map_err(|error| error.to_string())
}

pub fn recents() -> Result<Vec<PathBuf>, String> {
    let path = recents_file()?;
    let folders: Vec<PathBuf> = match fs::read_to_string(&path) {
        Ok(text) => {
            serde_json::from_str(&text).map_err(|error| format!("{}: {error}", path.display()))?
        }
        Err(error) if error.kind() == ErrorKind::NotFound => Vec::new(),
        Err(error) => return Err(format!("{}: {error}", path.display())),
    };
    Ok(folders
        .into_iter()
        .filter(|folder| folder.is_dir())
        .collect())
}

pub fn remember(folder: &Path) -> Result<Vec<PathBuf>, String> {
    let mut folders = recents()?;
    folders.retain(|known| known != folder);
    folders.insert(0, folder.to_owned());
    folders.truncate(MOST_RECENT);
    let path = recents_file()?;
    let failed = |error: std::io::Error| format!("{}: {error}", path.display());
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).map_err(failed)?;
    }
    let text = serde_json::to_string_pretty(&folders).map_err(|error| error.to_string())?;
    fs::write(&path, text).map_err(failed)?;
    Ok(folders)
}

pub fn head(folder: PathBuf) -> Head {
    let read = GitBinary::open(&folder).and_then(|git| {
        let branch = git.current_branch()?.unwrap_or_default();
        let changed = git
            .status()?
            .iter()
            .filter(|entry| entry.state != State::Ignored)
            .count();
        Ok((branch, changed))
    });
    let (branch, changed) = read.unwrap_or_else(|error| {
        eprintln!("desk: {} has no git state: {error}", folder.display());
        (String::new(), 0)
    });
    Head {
        folder,
        branch,
        changed,
    }
}

pub fn sidebar(head: &Head, rows: &[SessionRow], open: Option<&str>) -> Project {
    let now = SystemTime::now();
    let session = |row: &SessionRow, state: &str| Session {
        name: row.title().to_owned().into(),
        state: state.to_owned().into(),
        age: row.age(now).unwrap_or_default().into(),
    };
    let running: Vec<Session> = match open {
        None => Vec::new(),
        Some(id) => vec![rows.iter().find(|row| row.id == id).map_or_else(
            || Session {
                name: id.to_owned().into(),
                state: RUNNING.into(),
                age: "".into(),
            },
            |row| session(row, RUNNING),
        )],
    };
    Project {
        name: name(&head.folder).into(),
        branch: head.branch.clone().into(),
        changed: head.changed,
        shown: (!running.is_empty()).then_some(0),
        running,
        inactive: inactive(rows, open)
            .map(|row| session(row, row.state()))
            .collect(),
    }
}

pub fn inactive<'a>(
    rows: &'a [SessionRow],
    open: Option<&'a str>,
) -> impl Iterator<Item = &'a SessionRow> {
    rows.iter().filter(move |row| Some(row.id.as_str()) != open)
}
