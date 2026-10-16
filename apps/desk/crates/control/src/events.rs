use crate::replies::{Event, EventPage};
use crate::shapes::EventKind;
use std::collections::{HashMap, VecDeque};
use std::fmt;
use std::str::FromStr;
use std::sync::{Arc, Condvar, Mutex, MutexGuard, PoisonError};
use std::time::{SystemTime, UNIX_EPOCH};

pub const EVENT_RING: usize = 1024;

const SCHEME: &str = "desk://";

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Uri {
    Workspace(usize),
    Tile { workspace: Option<usize>, id: u32 },
    Editor(String),
    Draw(String),
}

impl fmt::Display for Uri {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Uri::Workspace(at) => write!(f, "{SCHEME}workspace/{at}"),
            Uri::Tile {
                workspace: Some(at),
                id,
            } => write!(f, "{SCHEME}workspace/{at}/tile/{id}"),
            Uri::Tile {
                workspace: None,
                id,
            } => write!(f, "{SCHEME}tile/{id}"),
            Uri::Editor(path) => write!(f, "{SCHEME}editor/{path}"),
            Uri::Draw(board) => write!(f, "{SCHEME}draw/{board}"),
        }
    }
}

impl FromStr for Uri {
    type Err = String;

    fn from_str(text: &str) -> Result<Self, Self::Err> {
        let unknown = || {
            format!(
                "{text} is not a desk uri: desk://workspace/N, desk://workspace/N/tile/M, desk://tile/M, desk://editor/<path> or desk://draw/<board>"
            )
        };
        let (kind, name) = text
            .strip_prefix(SCHEME)
            .and_then(|rest| rest.split_once('/'))
            .filter(|(_, name)| !name.is_empty())
            .ok_or_else(unknown)?;
        let parsed = match (kind, name.split_once("/tile/")) {
            ("workspace", Some((at, id))) => {
                at.parse()
                    .ok()
                    .zip(id.parse().ok())
                    .map(|(at, id)| Uri::Tile {
                        workspace: Some(at),
                        id,
                    })
            }
            ("workspace", None) => name.parse().ok().map(Uri::Workspace),
            ("tile", _) => name.parse().ok().map(|id| Uri::Tile {
                workspace: None,
                id,
            }),
            ("editor", _) => Some(Uri::Editor(name.to_owned())),
            ("draw", _) => Some(Uri::Draw(name.to_owned())),
            _ => None,
        };
        parsed.ok_or_else(unknown)
    }
}

#[derive(Default)]
struct Log {
    ring: VecDeque<Event>,
    head: u64,
    revisions: HashMap<String, u64>,
}

impl Log {
    fn page(&self, cursor: u64) -> EventPage {
        let oldest = self
            .ring
            .front()
            .map_or(self.head, |event| event.cursor.saturating_sub(1));
        let resync = cursor > self.head || cursor < oldest;
        let events = match resync {
            true => Vec::new(),
            false => self
                .ring
                .iter()
                .filter(|event| event.cursor > cursor)
                .cloned()
                .collect(),
        };
        EventPage {
            events,
            resync,
            head: self.head,
        }
    }
}

#[derive(Clone, Default)]
pub struct Feed(Arc<(Mutex<Log>, Condvar)>);

impl Feed {
    pub fn emit(&self, uri: &Uri, kind: EventKind) -> Event {
        let (log, ready) = &*self.0;
        let event = {
            let mut held = locked(log);
            held.head += 1;
            let uri = uri.to_string();
            let revision = held.revisions.entry(uri.clone()).or_default();
            *revision += 1;
            let revision = *revision;
            let event = Event {
                cursor: held.head,
                uri,
                revision,
                kind,
                at: SystemTime::now()
                    .duration_since(UNIX_EPOCH)
                    .map_or(0, |since| {
                        u64::try_from(since.as_millis()).unwrap_or(u64::MAX)
                    }),
            };
            if held.ring.len() == EVENT_RING {
                held.ring.pop_front();
            }
            held.ring.push_back(event.clone());
            event
        };
        ready.notify_all();
        event
    }

    pub fn revision(&self, uri: &Uri) -> u64 {
        locked(&self.0.0)
            .revisions
            .get(&uri.to_string())
            .copied()
            .unwrap_or_default()
    }

    pub fn head(&self) -> u64 {
        locked(&self.0.0).head
    }

    pub fn since(&self, cursor: u64) -> EventPage {
        locked(&self.0.0).page(cursor)
    }

    pub(crate) fn after(&self, cursor: u64) -> EventPage {
        let (log, ready) = &*self.0;
        let mut held = locked(log);
        while held.head <= cursor {
            held = ready.wait(held).unwrap_or_else(PoisonError::into_inner);
        }
        held.page(cursor)
    }
}

fn locked(log: &Mutex<Log>) -> MutexGuard<'_, Log> {
    log.lock().unwrap_or_else(PoisonError::into_inner)
}
