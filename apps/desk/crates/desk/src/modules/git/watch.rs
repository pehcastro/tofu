use std::collections::{BTreeMap, BTreeSet};
use std::fs;
use std::path::{Path, PathBuf};
use std::thread;
use std::time::{Duration, SystemTime};

use flume::RecvTimeoutError;

const POLL: Duration = Duration::from_millis(500);
const GIT_DIR: &str = ".git";
const GIT_STATE: [&str; 2] = [".git/index", ".git/HEAD"];

type Stamps = BTreeMap<String, (SystemTime, u64)>;

fn hidden(path: &str, skip: &BTreeSet<String>) -> bool {
    skip.iter().any(|skipped| {
        path.strip_prefix(skipped.as_str())
            .is_some_and(|rest| rest.is_empty() || rest.starts_with('/'))
    })
}

fn stamps(root: &Path, skip: &BTreeSet<String>) -> Stamps {
    let mut found = Stamps::new();
    let mut pending = vec![String::new()];
    while let Some(dir) = pending.pop() {
        let Ok(entries) = fs::read_dir(root.join(&dir)) else {
            continue;
        };
        for entry in entries.flatten() {
            let name = entry.file_name().to_string_lossy().into_owned();
            let path = if dir.is_empty() {
                name
            } else {
                format!("{dir}/{name}")
            };
            let Ok(meta) = entry.metadata() else {
                continue;
            };
            if path == GIT_DIR || hidden(&path, skip) {
                continue;
            }
            if meta.is_dir() {
                pending.push(path);
            } else if let Ok(modified) = meta.modified() {
                found.insert(path, (modified, meta.len()));
            }
        }
    }
    for state in GIT_STATE {
        if let Ok(meta) = fs::metadata(root.join(state))
            && let Ok(modified) = meta.modified()
        {
            found.insert(state.to_owned(), (modified, meta.len()));
        }
    }
    found
}

pub fn watch(
    root: PathBuf,
    skip: BTreeSet<String>,
) -> Result<(flume::Sender<BTreeSet<String>>, flume::Receiver<()>), String> {
    let (skips, skipped) = flume::unbounded::<BTreeSet<String>>();
    let (sender, changes) = flume::unbounded();
    thread::Builder::new()
        .name("desk-git-watch".into())
        .spawn(move || {
            let mut skip = skip;
            let mut known = stamps(&root, &skip);
            if sender.send(()).is_err() {
                return;
            }
            loop {
                match skipped.recv_timeout(POLL) {
                    Ok(next) => {
                        skip = next;
                        known.retain(|path, _| !hidden(path, &skip));
                    }
                    Err(RecvTimeoutError::Disconnected) => return,
                    Err(RecvTimeoutError::Timeout) => {
                        let now = stamps(&root, &skip);
                        if now != known {
                            known = now;
                            if sender.send(()).is_err() {
                                return;
                            }
                        }
                    }
                }
            }
        })
        .map_err(|error| format!("the git module could not start its watcher: {error}"))?;
    Ok((skips, changes))
}
