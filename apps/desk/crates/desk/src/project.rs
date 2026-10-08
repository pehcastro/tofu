use std::cmp::Reverse;
use std::env;
use std::fs;
use std::io::{self, ErrorKind};
use std::iter;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Output, Stdio};
use std::time::{Duration, SystemTime};

use desk_core::git::{Git, GitBinary, State};
use desk_core::sessions::SessionRow;
use desk_tiling::Store as Layouts;
use desk_ui::components::palette::{
    Palette, PaletteDetail, PaletteEntry, PaletteItem, PaletteMeta,
};
use desk_ui::components::sidebar::{Project, Session, SessionAt, SessionState};
use gpui::{App, Entity, Window};
use serde_json::Value;

const RECENTS: &str = "recents.json";
const MOST_RECENT: usize = 8;
pub const RECENT_ID: &str = "project.recent.";
pub const OPEN_FOLDER_ID: &str = "project.open";
const TOFU_VARIABLE: &str = "DESK_TOFU";
const SHORT_HASH: usize = 7;
const MISSING: &str = "missing";
#[cfg(windows)]
const CREATE_NO_WINDOW: u32 = 0x0800_0000;

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
    Ok(folders)
}

fn store(folders: Vec<PathBuf>) -> Result<Vec<PathBuf>, String> {
    let path = recents_file()?;
    let failed = |error: std::io::Error| format!("{}: {error}", path.display());
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).map_err(failed)?;
    }
    let text = serde_json::to_string_pretty(&folders).map_err(|error| error.to_string())?;
    fs::write(&path, text).map_err(failed)?;
    Ok(folders)
}

pub fn remember(folder: &Path) -> Result<Vec<PathBuf>, String> {
    let mut folders = recents()?;
    folders.retain(|known| known != folder);
    folders.insert(0, folder.to_owned());
    folders.truncate(MOST_RECENT);
    store(folders)
}

pub fn forget(folder: &Path) -> Result<Vec<PathBuf>, String> {
    let mut folders = recents()?;
    folders.retain(|known| known != folder);
    store(folders)
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

pub enum Picked {
    OpenFolder,
    Switch(PathBuf),
    Forget(PathBuf),
    Unknown,
}

pub fn picked(id: &str, recents: &[PathBuf]) -> Picked {
    if id == OPEN_FOLDER_ID {
        return Picked::OpenFolder;
    }
    let folder = id
        .strip_prefix(RECENT_ID)
        .and_then(|at| at.parse::<usize>().ok())
        .and_then(|at| recents.get(at));
    match folder {
        Some(folder) if folder.is_dir() => Picked::Switch(folder.clone()),
        Some(folder) => Picked::Forget(folder.clone()),
        None => Picked::Unknown,
    }
}

struct Recent {
    at: usize,
    folder: PathBuf,
    branch: Option<String>,
    current: bool,
    missing: bool,
    activity: Option<Activity>,
}

struct Activity {
    sessions: usize,
    last: Option<String>,
}

pub fn picker(
    recents: &[PathBuf],
    current: Option<&Path>,
    window: &mut Window,
    cx: &mut App,
) -> Entity<Palette> {
    let mut shown: Vec<Recent> = recents
        .iter()
        .enumerate()
        .map(|(at, folder)| Recent {
            at,
            folder: folder.clone(),
            branch: branch(folder),
            current: current == Some(folder.as_path()),
            missing: !folder.is_dir(),
            activity: None,
        })
        .collect();
    shown.sort_by_key(|recent| !recent.current);
    let menu = Palette::detailed(entries(&shown), window, cx);
    let filling = menu.downgrade();
    cx.spawn(async move |cx| {
        let shown = cx
            .background_executor()
            .spawn(async move { with_activity(shown) })
            .await;
        if let Err(error) = filling.update(cx, |menu, cx| menu.replace(entries(&shown), cx)) {
            eprintln!("desk: projects menu closed before its sessions were read: {error}");
        }
    })
    .detach();
    menu
}

fn entries(shown: &[Recent]) -> Vec<PaletteEntry> {
    let recent = shown.iter().map(|recent| {
        let mut meta: Vec<PaletteMeta> = Vec::new();
        if recent.missing {
            meta.push(PaletteMeta::Text(MISSING.into()));
        }
        meta.extend(
            recent
                .branch
                .clone()
                .map(|branch| PaletteMeta::Branch(branch.into())),
        );
        if let Some(activity) = &recent.activity {
            meta.push(PaletteMeta::Text(
                match activity.sessions {
                    0 => "no sessions".to_owned(),
                    1 => "1 session".to_owned(),
                    count => format!("{count} sessions"),
                }
                .into(),
            ));
            meta.extend(
                activity
                    .last
                    .clone()
                    .map(|last| PaletteMeta::Text(last.into())),
            );
        }
        let item = PaletteItem {
            id: format!("{RECENT_ID}{}", recent.at).into(),
            label: name(&recent.folder).into(),
            group: "Recent projects".into(),
            keys: None,
        };
        let detail = PaletteDetail {
            below: recent.folder.display().to_string().into(),
            meta,
            checked: recent.current,
            dim: recent.missing,
        };
        (item, Some(detail))
    });
    let open = PaletteItem {
        id: OPEN_FOLDER_ID.into(),
        label: "Open a folder".into(),
        group: "Open".into(),
        keys: None,
    };
    recent.chain(iter::once((open, None))).collect()
}

fn branch(folder: &Path) -> Option<String> {
    let dot = folder.join(".git");
    let git_dir = match fs::read_to_string(&dot) {
        Ok(link) => folder.join(link.trim().strip_prefix("gitdir:")?.trim()),
        Err(_) => dot,
    };
    let head = match fs::read_to_string(git_dir.join("HEAD")) {
        Ok(head) => head,
        Err(error) if error.kind() == ErrorKind::NotFound => return None,
        Err(error) => {
            eprintln!("desk: {} has no readable HEAD: {error}", folder.display());
            return None;
        }
    };
    let head = head.trim();
    match head.strip_prefix("ref: ") {
        Some(reference) => Some(
            reference
                .strip_prefix("refs/heads/")
                .unwrap_or(reference)
                .to_owned(),
        ),
        None => head.get(..SHORT_HASH).map(str::to_owned),
    }
}

fn with_activity(mut shown: Vec<Recent>) -> Vec<Recent> {
    let tofu = env::var_os(TOFU_VARIABLE).map_or_else(|| PathBuf::from("tofu"), PathBuf::from);
    let listing: Vec<Option<io::Result<Child>>> = shown
        .iter()
        .map(|recent| (!recent.missing).then(|| list_sessions(&tofu, &recent.folder)))
        .collect();
    for (recent, child) in shown.iter_mut().zip(listing) {
        let Some(child) = child else {
            continue;
        };
        match child
            .and_then(Child::wait_with_output)
            .map_err(|error| error.to_string())
            .and_then(activity)
        {
            Ok(activity) => recent.activity = Some(activity),
            Err(error) => eprintln!("desk: sessions in {}: {error}", recent.folder.display()),
        }
    }
    shown
}

fn list_sessions(tofu: &Path, folder: &Path) -> io::Result<Child> {
    let mut command = Command::new(tofu);
    command
        .args(["session", "list", "--json"])
        .current_dir(folder)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    #[cfg(windows)]
    std::os::windows::process::CommandExt::creation_flags(&mut command, CREATE_NO_WINDOW);
    command.spawn()
}

fn activity(output: Output) -> Result<Activity, String> {
    if !output.status.success() {
        return Err(String::from_utf8_lossy(&output.stderr).trim().to_owned());
    }
    let listing: Value =
        serde_json::from_slice(&output.stdout).map_err(|error| error.to_string())?;
    let sessions = listing
        .pointer("/data/sessions")
        .and_then(Value::as_array)
        .ok_or("tofu session list printed no data.sessions")?;
    let now = SystemTime::now();
    let last = sessions
        .iter()
        .filter_map(|row| {
            let row = SessionRow {
                at: row.get("at")?.as_str()?.to_owned(),
                last_at: row.get("lastAt").and_then(Value::as_str).map(str::to_owned),
                ..SessionRow::default()
            };
            Some((row.since(now)?, row.age(now)?))
        })
        .min_by_key(|(since, _)| *since)
        .map(|(_, age)| age);
    Ok(Activity {
        sessions: sessions.len(),
        last,
    })
}
