use std::collections::{BTreeMap, BTreeSet};
use std::fs;
use std::path::{Path, PathBuf};
use std::thread;
use std::time::{Duration, SystemTime};

use flume::RecvTimeoutError;

const POLL: Duration = Duration::from_millis(500);

#[derive(Default)]
pub struct Watched {
    pub folders: Vec<String>,
    pub files: Vec<String>,
}

type Stamp = Option<(SystemTime, u64)>;

fn stamp(path: &Path) -> Stamp {
    let found = fs::metadata(path).ok()?;
    Some((found.modified().ok()?, found.len()))
}

fn stamps(
    root: &Path,
    watched: Watched,
    mut known: BTreeMap<String, Stamp>,
) -> BTreeMap<String, Stamp> {
    watched
        .folders
        .into_iter()
        .chain(watched.files)
        .map(|path| {
            let was = known
                .remove(&path)
                .unwrap_or_else(|| stamp(&root.join(&path)));
            (path, was)
        })
        .collect()
}

pub fn watch(
    root: PathBuf,
) -> Result<(flume::Sender<Watched>, flume::Receiver<BTreeSet<String>>), String> {
    let (watch, wanted) = flume::unbounded::<Watched>();
    let (sender, changes) = flume::unbounded();
    thread::Builder::new()
        .name("desk-editor-watch".into())
        .spawn(move || {
            let mut known = BTreeMap::new();
            loop {
                match wanted.recv_timeout(POLL) {
                    Ok(watched) => known = stamps(&root, watched, known),
                    Err(RecvTimeoutError::Disconnected) => return,
                    Err(RecvTimeoutError::Timeout) => {
                        let changed: BTreeSet<String> = known
                            .iter_mut()
                            .filter_map(|(path, was)| {
                                let now = stamp(&root.join(path.as_str()));
                                (now != *was).then(|| {
                                    *was = now;
                                    path.clone()
                                })
                            })
                            .collect();
                        if !changed.is_empty() && sender.send(changed).is_err() {
                            return;
                        }
                    }
                }
            }
        })
        .map_err(|error| format!("the editor could not start its watcher: {error}"))?;
    Ok((watch, changes))
}
