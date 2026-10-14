use std::ffi::OsStr;
use std::fmt;
use std::fs;
use std::io;
use std::path::{Component, Path, PathBuf};

use crate::manifest::is_device;
use crate::{GRANT_FILE, Id, MANIFEST_FILE, PIN_FILE, WASM_FILE};

pub const MAX_FILE_BYTES: u64 = 16 << 20;
const KEPT: [&str; 4] = [MANIFEST_FILE, WASM_FILE, GRANT_FILE, PIN_FILE];
const FORBIDDEN: [char; 8] = ['<', '>', '"', '|', '?', '*', ':', '\\'];

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Op {
    Read,
    Write,
    List,
    Remove,
}

impl Op {
    pub fn name(self) -> &'static str {
        match self {
            Op::Read => "read",
            Op::Write => "write",
            Op::List => "list",
            Op::Remove => "remove",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PathRefusal {
    Absolute,
    Parent,
    Root,
    Name(String),
    Device(String),
    Kept(String),
    Outside,
}

impl fmt::Display for PathRefusal {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            PathRefusal::Absolute => f.write_str("an absolute path leaves the plugin folder"),
            PathRefusal::Parent => f.write_str(".. leaves the plugin folder"),
            PathRefusal::Root => f.write_str("the plugin folder itself is not a file"),
            PathRefusal::Name(name) => write!(f, "{name} is not a file name the desk accepts"),
            PathRefusal::Device(name) => write!(f, "{name} is a Windows device name"),
            PathRefusal::Kept(name) => write!(f, "{name} belongs to the desk, not the plugin"),
            PathRefusal::Outside => f.write_str("a link leads out of the plugin folder"),
        }
    }
}

#[derive(Debug)]
pub enum FsError {
    Denied(PathRefusal),
    Missing,
    TooLarge(u64),
    Io(io::Error),
}

impl fmt::Display for FsError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            FsError::Denied(refusal) => write!(f, "denied: {refusal}"),
            FsError::Missing => f.write_str("no such file"),
            FsError::TooLarge(bytes) => {
                write!(
                    f,
                    "{bytes} bytes, over the {MAX_FILE_BYTES} a plugin file may hold"
                )
            }
            FsError::Io(error) => write!(f, "{error}"),
        }
    }
}

impl std::error::Error for FsError {}

impl From<io::Error> for FsError {
    fn from(error: io::Error) -> Self {
        match error.kind() {
            io::ErrorKind::NotFound => FsError::Missing,
            _ => FsError::Io(error),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Outcome {
    Ok,
    Denied,
    Failed,
}

impl Outcome {
    pub fn of<T>(done: &Result<T, FsError>) -> Self {
        match done {
            Ok(_) => Outcome::Ok,
            Err(FsError::Denied(_)) => Outcome::Denied,
            Err(_) => Outcome::Failed,
        }
    }

    pub fn name(self) -> &'static str {
        match self {
            Outcome::Ok => "ok",
            Outcome::Denied => "denied",
            Outcome::Failed => "failed",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Entry {
    pub name: String,
    pub folder: bool,
    pub bytes: u64,
}

#[derive(Debug, Clone)]
pub struct Folder {
    id: Id,
    root: PathBuf,
}

impl Folder {
    pub fn new(id: Id, root: PathBuf) -> Self {
        Self { id, root }
    }

    pub fn path(&self, op: Op, asked: &str) -> Result<PathBuf, FsError> {
        self.logged(op, asked, |path| Ok(path.to_owned()))
    }

    pub fn read(&self, asked: &str) -> Result<Vec<u8>, FsError> {
        self.logged(Op::Read, asked, |path| {
            let bytes = fs::metadata(path)?.len();
            if bytes > MAX_FILE_BYTES {
                return Err(FsError::TooLarge(bytes));
            }
            Ok(fs::read(path)?)
        })
    }

    pub fn write(&self, asked: &str, data: &[u8]) -> Result<(), FsError> {
        self.logged(Op::Write, asked, |path| {
            let bytes = u64::try_from(data.len()).unwrap_or(u64::MAX);
            if bytes > MAX_FILE_BYTES {
                return Err(FsError::TooLarge(bytes));
            }
            if let Some(parent) = path.parent() {
                fs::create_dir_all(parent)?;
            }
            Ok(fs::write(path, data)?)
        })
    }

    pub fn list(&self, asked: &str) -> Result<Vec<Entry>, FsError> {
        self.logged(Op::List, asked, |path| {
            let mut entries = Vec::new();
            for entry in fs::read_dir(path)? {
                let entry = entry?;
                let meta = entry.metadata()?;
                entries.push(Entry {
                    name: entry.file_name().to_string_lossy().into_owned(),
                    folder: meta.is_dir(),
                    bytes: meta.len(),
                });
            }
            entries.sort_by(|a, b| a.name.cmp(&b.name));
            Ok(entries)
        })
    }

    pub fn remove(&self, asked: &str) -> Result<(), FsError> {
        self.logged(Op::Remove, asked, |path| {
            match fs::symlink_metadata(path)?.is_dir() {
                true => fs::remove_dir_all(path)?,
                false => fs::remove_file(path)?,
            }
            Ok(())
        })
    }

    fn logged<T>(
        &self,
        op: Op,
        asked: &str,
        act: impl FnOnce(&Path) -> Result<T, FsError>,
    ) -> Result<T, FsError> {
        let done = self.resolve(op, asked).and_then(|path| act(&path));
        let outcome = Outcome::of(&done);
        let why = match &done {
            Ok(_) => String::new(),
            Err(error) => format!(" ({error})"),
        };
        eprintln!(
            "desk: plugin {} fs {} {} {}{why}",
            self.id,
            op.name(),
            asked.escape_debug(),
            outcome.name()
        );
        done
    }

    fn resolve(&self, op: Op, asked: &str) -> Result<PathBuf, FsError> {
        let denied = |refusal| Err(FsError::Denied(refusal));
        let mut names = Vec::new();
        for part in Path::new(asked).components() {
            match part {
                Component::Normal(name) => names.push(checked(name)?),
                Component::CurDir => {}
                Component::ParentDir => return denied(PathRefusal::Parent),
                Component::RootDir | Component::Prefix(_) => return denied(PathRefusal::Absolute),
            }
        }
        match names.as_slice() {
            [] if op != Op::List => return denied(PathRefusal::Root),
            [only] if KEPT.iter().any(|kept| kept.eq_ignore_ascii_case(only)) => {
                return denied(PathRefusal::Kept((*only).to_owned()));
            }
            _ => {}
        }
        let root = fs::canonicalize(&self.root)?;
        let path = names
            .iter()
            .fold(root.clone(), |path, name| path.join(name));
        let existing = path
            .ancestors()
            .find(|at| fs::symlink_metadata(at).is_ok())
            .unwrap_or(&root);
        match fs::canonicalize(existing) {
            Ok(real) if real.starts_with(&root) => Ok(path),
            _ => denied(PathRefusal::Outside),
        }
    }
}

fn checked(name: &OsStr) -> Result<&str, FsError> {
    let refuse = |refusal| Err(FsError::Denied(refusal));
    let Some(text) = name.to_str() else {
        return refuse(PathRefusal::Name(name.to_string_lossy().into_owned()));
    };
    let odd = text
        .chars()
        .any(|c| c.is_control() || FORBIDDEN.contains(&c))
        || text.ends_with(['.', ' ']);
    if odd {
        return refuse(PathRefusal::Name(text.to_owned()));
    }
    let stem = text
        .split('.')
        .next()
        .unwrap_or_default()
        .trim_end()
        .to_lowercase();
    let numbered = ["com", "lpt"].iter().any(|port| {
        stem.strip_prefix(port)
            .is_some_and(|n| matches!(n, "0" | "\u{b9}" | "\u{b2}" | "\u{b3}"))
    });
    if is_device(&stem) || numbered || matches!(stem.as_str(), "conin$" | "conout$") {
        return refuse(PathRefusal::Device(text.to_owned()));
    }
    Ok(text)
}
