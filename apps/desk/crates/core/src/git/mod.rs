mod binary;
mod fake;
mod parse;

use std::fmt;
use std::io;

pub use binary::GitBinary;
pub use fake::FakeGit;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Mark {
    Unmodified,
    Modified,
    TypeChanged,
    Added,
    Deleted,
    Renamed,
    Copied,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Conflict {
    BothDeleted,
    AddedByUs,
    DeletedByThem,
    AddedByThem,
    DeletedByUs,
    BothAdded,
    BothModified,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum State {
    Tracked {
        index: Mark,
        worktree: Mark,
    },
    Renamed {
        from: String,
        index: Mark,
        worktree: Mark,
    },
    Conflicted(Conflict),
    Untracked,
    Ignored,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Entry {
    pub path: String,
    pub state: State,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Against {
    Index,
    Head,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Lines {
    pub start: u32,
    pub count: u32,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Hunk {
    pub old: Lines,
    pub new: Lines,
    pub removed: Vec<String>,
    pub added: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BlameLine {
    pub commit: Option<String>,
    pub author: String,
    pub time: i64,
    pub text: String,
}

#[derive(Debug)]
pub enum GitError {
    Spawn(io::Error),
    Failed {
        args: Vec<String>,
        code: Option<i32>,
        stderr: String,
    },
    Parse {
        what: &'static str,
        text: String,
    },
}

impl fmt::Display for GitError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            GitError::Spawn(error) => write!(f, "git could not be run: {error}"),
            GitError::Failed { args, code, stderr } => {
                let code = code.map_or_else(|| "a signal".to_owned(), |c| c.to_string());
                write!(
                    f,
                    "git {} exited with {code}: {}",
                    args.join(" "),
                    stderr.trim_end()
                )
            }
            GitError::Parse { what, text } => write!(f, "git printed an unreadable {what}: {text}"),
        }
    }
}

impl std::error::Error for GitError {}

pub trait Git {
    fn status(&self) -> Result<Vec<Entry>, GitError>;
    fn diff(&self, path: &str, against: Against) -> Result<Vec<Hunk>, GitError>;
    fn blame(&self, path: &str, contents: Option<&str>) -> Result<Vec<BlameLine>, GitError>;
    fn stage(&self, paths: &[String]) -> Result<(), GitError>;
    fn unstage(&self, paths: &[String]) -> Result<(), GitError>;
    fn discard(&self, paths: &[String]) -> Result<(), GitError>;
    fn commit(&self, message: &str) -> Result<(), GitError>;
    fn branches(&self) -> Result<Vec<String>, GitError>;
    fn current_branch(&self) -> Result<Option<String>, GitError>;
    fn switch(&self, branch: &str) -> Result<(), GitError>;
}
