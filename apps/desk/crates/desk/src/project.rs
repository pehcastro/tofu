use std::cmp::Reverse;
use std::env;
use std::fs;
use std::io::ErrorKind;
use std::path::{Path, PathBuf};
use std::time::{Duration, SystemTime};

use desk_core::git::{Git, GitBinary, State};
use desk_core::sessions::SessionRow;
use desk_tiling::Store as Layouts;
use desk_ui::components::sidebar::{Project, Session, SessionAt, SessionState};

const RECENTS: &str = "recents.json";
const MOST_RECENT: usize = 8;

#[derive(Clone)]
pub struct Head {
    pub folder: PathBuf,
    pub branch: String,
    pub changed: usize,
    pub ahead: u32,
}

impl Head {
    pub fn empty(folder: PathBuf) -> Self {
        Head {
            folder,
            branch: String::new(),
            changed: 0,
            ahead: 0,
        }
    }
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
        let ahead = git.drift()?.map_or(0, |drift| drift.ahead);
        Ok(Head {
            folder: folder.clone(),
            branch,
            changed,
            ahead,
        })
    });
    read.unwrap_or_else(|error| {
        eprintln!("desk: {} has no git state: {error}", folder.display());
        Head::empty(folder)
    })
}

pub const ACTIVE_FOR: Duration = Duration::from_secs(3600);

pub struct Opened<'a> {
    pub active: &'a [String],
    pub open: Option<&'a str>,
    pub waiting: bool,
    pub working: bool,
}

pub fn sidebar(head: &Head, rows: &[SessionRow], opened: &Opened) -> Project {
    let now = SystemTime::now();
    let active = opened.active.iter().map(|id| {
        let row = rows.iter().find(|row| row.id == *id);
        let shown = opened.open == Some(id.as_str());
        Session {
            name: row.map_or(id.as_str(), SessionRow::title).to_owned().into(),
            state: match (shown && opened.waiting, shown && opened.working) {
                (true, _) => SessionState::Waiting,
                (false, true) => SessionState::Running,
                (false, false) => SessionState::Idle,
            },
            age: row.and_then(|row| row.age(now)).unwrap_or_default().into(),
        }
    });
    let shown = opened.open.and_then(|open| {
        opened
            .active
            .iter()
            .position(|id| id == open)
            .map(SessionAt::Active)
            .or_else(|| {
                inactive(rows, opened.active)
                    .position(|row| row.id == open)
                    .map(SessionAt::Inactive)
            })
    });
    Project {
        name: name(&head.folder).into(),
        branch: head.branch.clone().into(),
        changed: head.changed,
        shown,
        active: active.collect(),
        inactive: inactive(rows, opened.active)
            .map(|row| Session {
                name: row.title().to_owned().into(),
                state: SessionState::Stopped,
                age: row.age(now).unwrap_or_default().into(),
            })
            .collect(),
    }
}

pub fn keep_active(active: &mut Vec<String>, rows: &[SessionRow], busy: Option<&str>) -> bool {
    let now = SystemTime::now();
    let idle = |id: &str| {
        if busy == Some(id) {
            return Some(Duration::ZERO);
        }
        rows.iter()
            .find(|row| row.id == id)
            .and_then(|row| row.since(now))
    };
    let recent = |id: &str| idle(id).is_some_and(|since| since < ACTIVE_FOR);
    let before = active.clone();
    active.retain(|id| recent(id));
    let mut fresh: Vec<&str> = rows
        .iter()
        .map(|row| row.id.as_str())
        .chain(busy)
        .filter(|id| recent(id) && !active.iter().any(|known| known == id))
        .collect();
    fresh.sort_by_key(|id| Reverse(idle(id)));
    for id in fresh {
        if !active.iter().any(|known| known == id) {
            active.push(id.to_owned());
        }
    }
    *active != before
}

pub fn inactive<'a>(
    rows: &'a [SessionRow],
    active: &'a [String],
) -> impl Iterator<Item = &'a SessionRow> {
    rows.iter().filter(move |row| !active.contains(&row.id))
}
