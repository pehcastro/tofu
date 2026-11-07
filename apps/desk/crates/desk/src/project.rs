use std::cmp::Reverse;
use std::env;
use std::ffi::OsString;
use std::fs;
use std::io::{self, ErrorKind};
use std::path::{Path, PathBuf};
#[cfg(any(feature = "screen-work", feature = "screen-onboarding"))]
use std::process::{Child, Command, Stdio};
use std::time::{Duration, SystemTime};

use desk_core::git::{Git, GitBinary, State};
pub use desk_core::model::project_name as name;
use desk_core::model::{StatusState, Statuses};
use desk_core::sessions::{SessionRow, age_changes_in};
use desk_tiling::Store as Layouts;
use desk_ui::components::sidebar::{SessionColumns, SessionState};
#[cfg(any(feature = "screen-work", feature = "screen-onboarding"))]
use serde::Deserialize;
use serde_json::{Value, json};

const RECENTS: &str = "recents.json";
const PROJECT_VARIABLE: &str = "DESK_PROJECT";
const STATE_VERSION: u64 = 1;
const MOST_RECENT: usize = 8;
const TOFU_VARIABLE: &str = "DESK_TOFU";
#[cfg(feature = "screen-onboarding")]
const SHORT_HASH: usize = 7;
#[cfg(all(windows, any(feature = "screen-work", feature = "screen-onboarding")))]
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

fn folder_arguments() -> Vec<OsString> {
    env::args_os()
        .skip(1)
        .take_while(|argument| !argument.to_string_lossy().starts_with("--"))
        .collect()
}

pub fn opened_beside() -> Vec<PathBuf> {
    let mut folders = folder_arguments();
    folders.pop();
    folders
        .into_iter()
        .filter_map(|folder| match std::path::absolute(&folder) {
            Ok(folder) if folder.is_dir() => Some(folder),
            _ => {
                eprintln!(
                    "desk: the folder argument {} is not a folder",
                    folder.display()
                );
                None
            }
        })
        .collect()
}

pub fn chosen() -> Result<Option<(PathBuf, &'static str)>, String> {
    let named = folder_arguments()
        .pop()
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
    let mut sessions = SessionColumns {
        listed: rows.len(),
        ..SessionColumns::default()
    };
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
    let listed = u32::try_from(sessions.listed).unwrap_or(u32::MAX);
    let mut fresh: Vec<u32> = (0..listed)
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

#[cfg(feature = "screen-onboarding")]
pub struct Listed {
    pub folder: PathBuf,
    pub branch: Option<String>,
    pub missing: bool,
    pub sessions: Option<usize>,
}

#[cfg(feature = "screen-onboarding")]
pub fn listed(folders: Vec<PathBuf>) -> Vec<Listed> {
    let mut table = desk_core::model::Projects::default();
    for folder in folders {
        table.open(folder);
    }
    let read = known();
    let mut sessions = vec![read.is_ok().then_some(0); table.rows().len()];
    match &read {
        Ok(known) => {
            for project in known {
                if let Some(count) = table
                    .find(&project.folder)
                    .and_then(|at| sessions.get_mut(at))
                {
                    *count = Some(project.sessions.len());
                }
            }
        }
        Err(error) => eprintln!("desk: sessions: {error}"),
    }
    table
        .rows()
        .iter()
        .zip(sessions)
        .map(|(row, sessions)| Listed {
            branch: branch(&row.folder),
            missing: !row.folder.is_dir(),
            sessions,
            folder: row.folder.clone(),
        })
        .collect()
}

#[cfg(any(feature = "screen-work", feature = "screen-onboarding"))]
pub struct Known {
    pub folder: PathBuf,
    pub sessions: Vec<SessionRow>,
}

#[cfg(any(feature = "screen-work", feature = "screen-onboarding"))]
#[derive(Deserialize)]
struct KnownReport {
    data: KnownProjects,
}

#[cfg(any(feature = "screen-work", feature = "screen-onboarding"))]
#[derive(Deserialize)]
struct KnownProjects {
    projects: Vec<KnownProject>,
}

#[cfg(any(feature = "screen-work", feature = "screen-onboarding"))]
#[derive(Deserialize)]
struct KnownProject {
    path: PathBuf,
    sessions: Vec<KnownSession>,
}

#[cfg(any(feature = "screen-work", feature = "screen-onboarding"))]
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct KnownSession {
    id: String,
    #[serde(default)]
    name: Option<String>,
    handle: String,
    last_at: String,
    working: bool,
}

#[cfg(any(feature = "screen-work", feature = "screen-onboarding"))]
pub fn known() -> Result<Vec<Known>, String> {
    let mut command = Command::new(tofu());
    command
        .args(["projects", "--json"])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    #[cfg(windows)]
    std::os::windows::process::CommandExt::creation_flags(&mut command, CREATE_NO_WINDOW);
    eprintln!("desk: tofu projects --json");
    let output = command
        .spawn()
        .and_then(Child::wait_with_output)
        .map_err(|error| format!("tofu projects: {error}"))?;
    if !output.status.success() {
        return Err(format!(
            "tofu projects: {}",
            String::from_utf8_lossy(&output.stderr).trim()
        ));
    }
    let report: KnownReport = serde_json::from_slice(&output.stdout)
        .map_err(|error| format!("tofu projects printed no data.projects: {error}"))?;
    Ok(report
        .data
        .projects
        .into_iter()
        .map(|project| Known {
            folder: project.path,
            sessions: project
                .sessions
                .into_iter()
                .map(|session| SessionRow {
                    id: session.id,
                    name: session.name,
                    handle: session.handle,
                    at: session.last_at.clone(),
                    last_at: Some(session.last_at),
                    running: session.working,
                    ..SessionRow::default()
                })
                .collect(),
        })
        .collect())
}

#[cfg(feature = "screen-onboarding")]
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
