use std::collections::{BTreeMap, BTreeSet};
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread;
use std::time::{Duration, SystemTime};

use desk_core::git::GitBinary;

const TICK: Duration = Duration::from_millis(150);
const TREE_EVERY: u32 = 3;
const GIT_DIR: &str = ".git";
const IGNORE_FILE: &str = ".gitignore";
const GIT_FILES: [&str; 5] = ["HEAD", "index", "FETCH_HEAD", "MERGE_HEAD", "packed-refs"];
const GIT_FOLDERS: [&str; 2] = ["refs", "rebase-merge"];

#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub enum Touched {
    Tree,
    Git,
}

type Stamps = BTreeMap<PathBuf, (SystemTime, u64)>;

fn stamp(path: &Path, into: &mut Stamps) {
    if let Ok(meta) = fs::metadata(path)
        && let Ok(modified) = meta.modified()
    {
        into.insert(path.to_owned(), (modified, meta.len()));
    }
}

fn walk(from: PathBuf, skip: &BTreeSet<PathBuf>, into: &mut Stamps) {
    let mut pending = vec![from];
    while let Some(dir) = pending.pop() {
        let Ok(entries) = fs::read_dir(&dir) else {
            continue;
        };
        for entry in entries.flatten() {
            let path = entry.path();
            if skip.contains(&path) {
                continue;
            }
            let Ok(meta) = entry.metadata() else {
                continue;
            };
            if meta.is_dir() {
                pending.push(path);
            } else if let Ok(modified) = meta.modified() {
                into.insert(path, (modified, meta.len()));
            }
        }
    }
}

fn git_stamps(git: &Path) -> Stamps {
    let mut found = Stamps::new();
    for file in GIT_FILES {
        stamp(&git.join(file), &mut found);
    }
    for folder in GIT_FOLDERS {
        walk(git.join(folder), &BTreeSet::new(), &mut found);
    }
    found
}

fn skipped(git: &GitBinary) -> BTreeSet<PathBuf> {
    let root = git.root();
    let ignored = git.ignored().unwrap_or_else(|error| {
        eprintln!("desk: git: the watcher reads every folder, ignored ones too: {error}");
        BTreeSet::new()
    });
    ignored
        .iter()
        .map(|path| root.join(path))
        .chain([root.join(GIT_DIR)])
        .collect()
}

fn tree_stamps(root: &Path, skip: &BTreeSet<PathBuf>) -> Stamps {
    let mut found = Stamps::new();
    walk(root.to_owned(), skip, &mut found);
    found
}

fn ignore_moved(was: &Stamps, now: &Stamps) -> bool {
    let ignore = |path: &&PathBuf| path.file_name().is_some_and(|name| name == IGNORE_FILE);
    now.iter()
        .filter(|(path, _)| ignore(path))
        .any(|(path, stamp)| was.get(path) != Some(stamp))
        || was
            .keys()
            .filter(ignore)
            .any(|path| !now.contains_key(path))
}

pub fn start(
    git: Arc<GitBinary>,
    paused: Arc<AtomicBool>,
) -> Result<flume::Receiver<Touched>, String> {
    let (sender, touched) = flume::unbounded();
    thread::Builder::new()
        .name("desk-git-screen-watch".into())
        .spawn(move || {
            let root = git.root().to_owned();
            let dot = root.join(GIT_DIR);
            let mut skip = skipped(&git);
            let mut gits = git_stamps(&dot);
            let mut tree = tree_stamps(&root, &skip);
            let mut tick: u32 = 0;
            while !sender.is_disconnected() {
                thread::sleep(TICK);
                tick = tick.wrapping_add(1);
                let now = git_stamps(&dot);
                let mut seen = (now != gits).then_some(Touched::Git);
                gits = now;
                if tick.is_multiple_of(TREE_EVERY) {
                    let now = tree_stamps(&root, &skip);
                    if now != tree {
                        if ignore_moved(&tree, &now) {
                            skip = skipped(&git);
                        }
                        seen = seen.or(Some(Touched::Tree));
                    }
                    tree = now;
                }
                let Some(seen) = seen else {
                    continue;
                };
                if !paused.load(Ordering::Acquire) && sender.send(seen).is_err() {
                    return;
                }
            }
        })
        .map_err(|error| format!("the git screen could not start its watcher: {error}"))?;
    Ok(touched)
}
