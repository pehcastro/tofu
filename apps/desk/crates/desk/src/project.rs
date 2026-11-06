use std::cmp::Reverse;
use std::env;
use std::fs;
use std::io::{self, ErrorKind};
use std::iter;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Output, Stdio};
use std::time::{Duration, SystemTime};

use desk_core::git::{Git, GitBinary, State};
pub use desk_core::model::project_name as name;
use desk_core::model::{ProjectRow, StatusState, Statuses};
use desk_core::sessions::{SessionRow, age_changes_in};
use desk_tiling::Store as Layouts;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::palette::{
    Palette, PaletteDetail, PaletteEntry, PaletteItem, PaletteMeta,
};
use desk_ui::components::sidebar::{SessionColumns, SessionState};
use gpui::{App, Entity, Window};
use serde_json::{Value, json};

const RECENTS: &str = "recents.json";
const PROJECT_VARIABLE: &str = "DESK_PROJECT";
const STATE_VERSION: u64 = 1;
const MOST_RECENT: usize = 8;
pub const RECENT_ID: &str = "project.recent.";
pub const OPEN_FOLDER_ID: &str = "project.open";
const TOFU_VARIABLE: &str = "DESK_TOFU";
const SHORT_HASH: usize = 7;
const MISSING: &str = "missing";
#[cfg(windows)]
const CREATE_NO_WINDOW: u32 = 0x0800_0000;

pub struct Head {
    pub folder: PathBuf,
    pub branch: String,
    pub changed: usize,
    pub ahead: u32,
}

pub fn launch() -> Result<PathBuf, String> {
    launched().map(|(folder, _)| folder)
}

pub fn launched() -> Result<(PathBuf, &'static str), String> {
    match chosen()? {
        Some(found) => Ok(found),
        None => env::current_dir()
            .map(|folder| (folder, "the launch folder"))
            .map_err(|error| format!("the desk cannot read the folder it runs in: {error}")),
    }
}

pub fn chosen() -> Result<Option<(PathBuf, &'static str)>, String> {
    let named = env::args_os()
        .nth(1)
        .filter(|argument| !argument.to_string_lossy().starts_with("--"))
        .map(|folder| (folder, "the folder argument"))
        .or_else(|| {
            env::var_os(PROJECT_VARIABLE)
                .filter(|folder| !folder.is_empty())
                .map(|folder| (folder, PROJECT_VARIABLE))
        });
    if let Some((folder, from)) = named {
        let folder = std::path::absolute(&folder)
            .map_err(|error| format!("{from} {} is not a path: {error}", folder.display()))?;
        if !folder.is_dir() {
            return Err(format!("{from} {} is not a folder", folder.display()));
        }
        return Ok(Some((folder, from)));
    }
    let recent = recents()
        .unwrap_or_else(|error| {
            eprintln!("desk: recents: {error}");
            Vec::new()
        })
        .into_iter()
        .find(|folder| folder.is_dir());
    Ok(recent.map(|folder| (folder, "the most recent project")))
}

pub fn tofu() -> PathBuf {
    env::var_os(TOFU_VARIABLE).map_or_else(|| PathBuf::from("tofu"), PathBuf::from)
}

#[derive(Clone, PartialEq)]
pub struct Kept {
    pub name: String,
    pub pinned: bool,
    pub locked: bool,
}

#[derive(Clone, PartialEq)]
pub struct Remembered {
    pub session: Option<String>,
    pub family: Option<String>,
    pub screens: Vec<Kept>,
    pub focus: String,
}

fn state_file(folder: &Path) -> Result<PathBuf, String> {
    Layouts::state_file(folder).map_err(|error| error.to_string())
}

pub fn read_state(folder: &Path) -> Result<Option<Remembered>, String> {
    let path = state_file(folder)?;
    let text = match fs::read_to_string(&path) {
        Ok(text) => text,
        Err(error) if error.kind() == ErrorKind::NotFound => return Ok(None),
        Err(error) => return Err(format!("{}: {error}", path.display())),
    };
    parse_state(&text)
        .map(Some)
        .map_err(|reason| format!("{}: {reason}", path.display()))
}

fn parse_state(text: &str) -> Result<Remembered, String> {
    let state: Value = serde_json::from_str(text).map_err(|error| error.to_string())?;
    match state.get("version").and_then(Value::as_u64) {
        Some(STATE_VERSION) => {}
        Some(other) => return Err(format!("version {other} is not {STATE_VERSION}")),
        None => return Err("it has no version".to_owned()),
    }
    let text_at =
        |value: &Value, key: &str| value.get(key).and_then(Value::as_str).map(str::to_owned);
    let flag_at =
        |value: &Value, key: &str| value.get(key).and_then(Value::as_bool).unwrap_or(false);
    let screens = state
        .get("screens")
        .and_then(Value::as_array)
        .ok_or("it has no screens list")?
        .iter()
        .map(|screen| {
            Ok(Kept {
                name: text_at(screen, "name").ok_or("a screen has no name")?,
                pinned: flag_at(screen, "pinned"),
                locked: flag_at(screen, "locked"),
            })
        })
        .collect::<Result<Vec<Kept>, String>>()?;
    Ok(Remembered {
        session: text_at(&state, "session"),
        family: text_at(&state, "family"),
        screens,
        focus: text_at(&state, "focus").ok_or("it has no focus")?,
    })
}

pub fn write_state(folder: &Path, state: &Remembered) -> Result<(), String> {
    let path = state_file(folder)?;
    let failed = |error: io::Error| format!("{}: {error}", path.display());
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).map_err(failed)?;
    }
    let screens: Vec<Value> = state
        .screens
        .iter()
        .map(|kept| json!({"name": kept.name, "pinned": kept.pinned, "locked": kept.locked}))
        .collect();
    let text = serde_json::to_string_pretty(&json!({
        "version": STATE_VERSION,
        "session": state.session,
        "family": state.family,
        "screens": screens,
        "focus": state.focus,
    }))
    .map_err(|error| error.to_string())?;
    fs::write(&path, text).map_err(failed)
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
        Ok((branch, changed, ahead))
    });
    let (branch, changed, ahead) = read.unwrap_or_else(|error| {
        eprintln!("desk: {} has no git state: {error}", folder.display());
        (String::new(), 0, 0)
    });
    Head {
        folder,
        branch,
        changed,
        ahead,
    }
}

#[cfg(feature = "screen-intro")]
pub fn branches(folder: &Path) -> Result<Vec<String>, String> {
    GitBinary::open(folder)
        .and_then(|git| git.branches())
        .map_err(|error| error.to_string())
}

#[cfg(feature = "screen-intro")]
pub fn switch(folder: &Path, branch: &str) -> Result<(), String> {
    GitBinary::open(folder)
        .and_then(|git| git.switch(branch))
        .map_err(|error| error.to_string())
}

pub const ACTIVE_FOR: Duration = Duration::from_secs(3600);

pub fn columns(rows: &[SessionRow], was: &SessionColumns) -> SessionColumns {
    let mut sessions = SessionColumns::default();
    for row in rows {
        sessions.push(
            row.id.clone().into(),
            row.title().to_owned().into(),
            row.last(),
            SessionState::Stopped,
        );
    }
    for column in &was.active {
        let Some(id) = usize::try_from(*column).ok().and_then(|at| was.id.get(at)) else {
            continue;
        };
        let at = match sessions.find(id) {
            Some(at) => at,
            None => sessions.push(id.clone(), id.clone(), None, SessionState::Unreported),
        };
        sessions.active.push(at);
    }
    sessions
}

pub struct Live<'a> {
    pub listed: usize,
    pub busy: Option<&'a str>,
    pub running: &'a [&'a str],
    pub statuses: &'a Statuses,
}

pub fn keep_active(sessions: &mut SessionColumns, live: &Live) -> bool {
    let now = SystemTime::now();
    let mut column_of = |id: &str| match sessions.find(id) {
        Some(at) => at,
        None => sessions.push(id.into(), id.into(), None, SessionState::Unreported),
    };
    let busy = live.busy.map(&mut column_of);
    let running: Vec<u32> = live.running.iter().map(|id| column_of(id)).collect();
    let idle = |column: u32| match Some(column) == busy {
        true => Some(Duration::ZERO),
        false => usize::try_from(column)
            .ok()
            .and_then(|at| *sessions.last_at.get(at)?)
            .and_then(|last| now.duration_since(last).ok()),
    };
    let recent = |column: u32| idle(column).is_some_and(|since| since < ACTIVE_FOR);
    let mut active = std::mem::take(&mut sessions.active);
    let before = active.clone();
    active.retain(|column| recent(*column));
    let mut fresh: Vec<u32> = (0..u32::try_from(live.listed).unwrap_or(u32::MAX))
        .chain(busy)
        .filter(|column| recent(*column) && !active.contains(column))
        .collect();
    fresh.sort_by_key(|column| Reverse(idle(*column)));
    for column in fresh.into_iter().chain(running.iter().copied()) {
        if !active.contains(&column) {
            active.push(column);
        }
    }
    for (column, state) in sessions.state.iter_mut().enumerate() {
        let id = u32::try_from(column).unwrap_or(u32::MAX);
        *state = match active.contains(&id) {
            false => SessionState::Stopped,
            true => {
                let reported = sessions
                    .id
                    .get(column)
                    .and_then(|id| live.statuses.activity(id));
                match reported {
                    Some(StatusState::Blocked) => SessionState::Waiting,
                    _ if running.contains(&id) => SessionState::Running,
                    Some(StatusState::Working) => SessionState::Running,
                    Some(
                        StatusState::Idle
                        | StatusState::Done
                        | StatusState::Error
                        | StatusState::Clear,
                    ) => SessionState::Idle,
                    None => SessionState::Unreported,
                }
            }
        };
    }
    let listed = u32::try_from(live.listed).unwrap_or(u32::MAX);
    sessions.inactive = (0..listed)
        .filter(|column| !active.contains(column))
        .collect();
    let moved = active != before;
    sessions.active = active;
    moved
}

pub fn next_age(sessions: &SessionColumns, now: SystemTime) -> Option<Duration> {
    sessions
        .last_at
        .iter()
        .flatten()
        .filter_map(|last| now.duration_since(*last).ok())
        .map(age_changes_in)
        .min()
}

pub enum Picked {
    OpenFolder,
    Switch(PathBuf),
    Forget(PathBuf),
    Unknown,
}

pub fn picked(id: &str, projects: &[ProjectRow]) -> Picked {
    if id == OPEN_FOLDER_ID {
        return Picked::OpenFolder;
    }
    let folder = id
        .strip_prefix(RECENT_ID)
        .and_then(|at| at.parse::<usize>().ok())
        .and_then(|at| projects.get(at))
        .map(|row| &row.folder);
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
    let marks = shown
        .iter()
        .map(|recent| (format!("{RECENT_ID}{}", recent.at).into(), Glyph::Code))
        .chain(iter::once((OPEN_FOLDER_ID.into(), Glyph::Folder)))
        .collect();
    menu.update(cx, |menu, _| menu.marks(marks));
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

#[cfg(feature = "screen-onboarding")]
pub struct Listed {
    pub folder: PathBuf,
    pub branch: Option<String>,
    pub missing: bool,
    pub sessions: Option<usize>,
}

#[cfg(feature = "screen-onboarding")]
pub fn listed(folder: PathBuf) -> Listed {
    let missing = !folder.is_dir();
    let read = match missing {
        true => Err("the folder is missing".to_owned()),
        false => list_sessions(&tofu(), &folder)
            .and_then(Child::wait_with_output)
            .map_err(|error| error.to_string())
            .and_then(activity),
    };
    let sessions = match read {
        Ok(activity) => Some(activity.sessions),
        Err(error) => {
            eprintln!("desk: sessions in {}: {error}", folder.display());
            None
        }
    };
    Listed {
        branch: branch(&folder),
        missing,
        sessions,
        folder,
    }
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
    let tofu = tofu();
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
