use std::fs;
use std::path::{Path, PathBuf};

use desk_core::git::{Git, GitBinary, Mark, State};
use desk_ui::components::chip::GitStatus;
use desk_ui::components::tree::TreeNode;

const SKIPPED: &str = ".git";
const ALWAYS_IGNORED: [&str; 14] = [
    "node_modules",
    "target",
    "dist",
    "build",
    "out",
    "coverage",
    ".next",
    ".turbo",
    ".cache",
    ".venv",
    "__pycache__",
    ".idea",
    ".vs",
    ".svelte-kit",
];

enum Rule {
    Name(String),
    Anchored(String),
}

pub struct Ignore {
    rules: Vec<Rule>,
}

fn glob(pattern: &[u8], text: &[u8]) -> bool {
    let (mut at, mut seen) = (0, 0);
    let mut star: Option<(usize, usize)> = None;
    while seen < text.len() {
        match pattern.get(at) {
            Some(b'*') => {
                star = Some((at, seen));
                at += 1;
            }
            Some(&wanted) if wanted == b'?' || text.get(seen) == Some(&wanted) => {
                at += 1;
                seen += 1;
            }
            _ => match star {
                Some((star_at, star_seen)) => {
                    at = star_at + 1;
                    seen = star_seen + 1;
                    star = Some((star_at, star_seen + 1));
                }
                None => return false,
            },
        }
    }
    pattern
        .get(at..)
        .unwrap_or_default()
        .iter()
        .all(|byte| *byte == b'*')
}

impl Ignore {
    pub fn read(root: &Path) -> Self {
        let mut rules: Vec<Rule> = ALWAYS_IGNORED
            .iter()
            .map(|name| Rule::Name((*name).to_owned()))
            .collect();
        let written = match fs::read_to_string(root.join(".gitignore")) {
            Ok(text) => text,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => String::new(),
            Err(error) => {
                eprintln!(
                    "desk: editor reads no .gitignore in {}: {error}",
                    root.display()
                );
                String::new()
            }
        };
        for line in written.lines().map(str::trim) {
            if line.is_empty() || line.starts_with('#') || line.starts_with('!') {
                continue;
            }
            let anchored = line.starts_with('/') || line.trim_end_matches('/').contains('/');
            let pattern = line.trim_matches('/').to_owned();
            rules.push(match anchored {
                true => Rule::Anchored(pattern),
                false => Rule::Name(pattern),
            });
        }
        Ignore { rules }
    }

    pub fn ignored(&self, relative: &str) -> bool {
        let names: Vec<&str> = relative.split('/').collect();
        self.rules.iter().any(|rule| match rule {
            Rule::Name(pattern) => names
                .iter()
                .any(|name| glob(pattern.as_bytes(), name.as_bytes())),
            Rule::Anchored(pattern) => {
                let depth = pattern.split('/').count();
                names.len() >= depth
                    && glob(pattern.as_bytes(), names[..depth].join("/").as_bytes())
            }
        })
    }
}

pub struct Listing {
    pub root: PathBuf,
    pub ignore: Ignore,
    pub states: Vec<(String, GitStatus)>,
}

impl Listing {
    pub fn new(root: PathBuf) -> Self {
        Listing {
            ignore: Ignore::read(&root),
            root,
            states: Vec::new(),
        }
    }

    pub fn children(&self, folder: &str) -> Vec<TreeNode> {
        let dir = self.root.join(folder);
        let read = match fs::read_dir(&dir) {
            Ok(read) => read,
            Err(error) => {
                eprintln!("desk: editor could not list {}: {error}", dir.display());
                return Vec::new();
            }
        };
        let mut found: Vec<(bool, String)> = read
            .filter_map(|entry| match entry {
                Ok(entry) => Some((
                    entry.file_type().is_ok_and(|kind| kind.is_dir()),
                    entry.file_name().to_string_lossy().into_owned(),
                )),
                Err(error) => {
                    eprintln!(
                        "desk: editor skipped an entry of {}: {error}",
                        dir.display()
                    );
                    None
                }
            })
            .filter(|(_, name)| name != SKIPPED)
            .collect();
        found.sort_by_cached_key(|(is_folder, name)| (!is_folder, name.to_lowercase()));
        found
            .into_iter()
            .map(|(is_folder, name)| {
                let path = match folder {
                    "" => name.clone(),
                    folder => format!("{folder}/{name}"),
                };
                let git = match self.ignore.ignored(&path) {
                    true => Some(GitStatus::Ignored),
                    false => self.own(&path, is_folder),
                };
                match is_folder {
                    true => TreeNode::unread(name, git, self.inside(&path)),
                    false => TreeNode::file(name, git),
                }
            })
            .collect()
    }

    pub fn folders_under(&self, folder: &str) -> Vec<String> {
        let mut found = Vec::new();
        let mut pending = vec![folder.to_owned()];
        while let Some(at) = pending.pop() {
            let read = match fs::read_dir(self.root.join(&at)) {
                Ok(read) => read,
                Err(error) => {
                    eprintln!("desk: editor could not expand {at}: {error}");
                    continue;
                }
            };
            for entry in read.flatten() {
                let name = entry.file_name().to_string_lossy().into_owned();
                let path = match at.as_str() {
                    "" => name.clone(),
                    at => format!("{at}/{name}"),
                };
                let folder = entry.file_type().is_ok_and(|kind| kind.is_dir());
                if folder && name != SKIPPED && !self.ignore.ignored(&path) {
                    pending.push(path);
                }
            }
            found.push(at);
        }
        found.retain(|path| !path.is_empty());
        found.sort();
        found
    }

    fn own(&self, path: &str, folder: bool) -> Option<GitStatus> {
        let key = match folder {
            true => format!("{path}/"),
            false => path.to_owned(),
        };
        self.states
            .iter()
            .find(|(state, _)| {
                *state == key || (state.ends_with('/') && key.starts_with(state.as_str()))
            })
            .map(|(_, git)| *git)
    }

    fn inside(&self, folder: &str) -> Vec<GitStatus> {
        let prefix = format!("{folder}/");
        self.states
            .iter()
            .filter(|(path, _)| path.len() > prefix.len() && path.starts_with(&prefix))
            .map(|(_, git)| *git)
            .collect()
    }

    pub fn relative(&self, path: &Path) -> Option<String> {
        let inside = path.strip_prefix(&self.root).ok()?;
        let parts: Vec<String> = inside
            .iter()
            .map(|part| part.to_string_lossy().into_owned())
            .collect();
        Some(parts.join("/"))
    }
}

fn git_status(state: &State) -> Option<GitStatus> {
    match state {
        State::Untracked => Some(GitStatus::Untracked),
        State::Ignored => Some(GitStatus::Ignored),
        State::Conflicted(_) => Some(GitStatus::Conflict),
        State::Renamed { .. } => Some(GitStatus::Modified),
        State::Tracked {
            index: Mark::Deleted,
            ..
        }
        | State::Tracked {
            worktree: Mark::Deleted,
            ..
        } => Some(GitStatus::Deleted),
        State::Tracked {
            index: Mark::Added, ..
        } => Some(GitStatus::Added),
        State::Tracked {
            index: Mark::Unmodified,
            worktree: Mark::Unmodified,
        } => None,
        State::Tracked { .. } => Some(GitStatus::Modified),
    }
}

pub fn states(git: &GitBinary, root: &Path) -> Result<Vec<(String, GitStatus)>, String> {
    let inside = root.strip_prefix(git.root()).map_err(|_| {
        format!(
            "{} is outside its repository {}",
            root.display(),
            git.root().display()
        )
    })?;
    let prefix: String = inside
        .iter()
        .map(|part| format!("{}/", part.to_string_lossy()))
        .collect();
    let entries = git.status().map_err(|error| error.to_string())?;
    Ok(entries
        .into_iter()
        .filter_map(|entry| {
            let path = entry.path.strip_prefix(prefix.as_str())?.to_owned();
            Some((path, git_status(&entry.state)?))
        })
        .collect())
}
