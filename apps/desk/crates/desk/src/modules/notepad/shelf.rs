use std::collections::BTreeMap;
use std::fs;
use std::io::ErrorKind;
use std::path::{Component, Path, PathBuf};

use desk_ui::theme::ColorToken;
use serde::{Deserialize, Serialize};

const COLOURS: &str = ".colours.json";

#[derive(Clone, Copy, Debug, PartialEq, Eq, Deserialize, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Colour {
    Red,
    Yellow,
    Green,
    Cyan,
    Blue,
    Magenta,
}

impl Colour {
    pub const ALL: [Colour; 6] = [
        Colour::Red,
        Colour::Yellow,
        Colour::Green,
        Colour::Cyan,
        Colour::Blue,
        Colour::Magenta,
    ];

    pub fn label(self) -> &'static str {
        match self {
            Colour::Red => "Red",
            Colour::Yellow => "Yellow",
            Colour::Green => "Green",
            Colour::Cyan => "Cyan",
            Colour::Blue => "Blue",
            Colour::Magenta => "Magenta",
        }
    }

    pub fn token(self) -> ColorToken {
        match self {
            Colour::Red => ColorToken::AnsiRed,
            Colour::Yellow => ColorToken::AnsiYellow,
            Colour::Green => ColorToken::AnsiGreen,
            Colour::Cyan => ColorToken::AnsiCyan,
            Colour::Blue => ColorToken::AnsiBlue,
            Colour::Magenta => ColorToken::AnsiMagenta,
        }
    }
}

pub struct Entry {
    pub path: PathBuf,
    pub depth: usize,
    pub folder: bool,
}

pub struct Shelf {
    root: PathBuf,
    entries: Vec<Entry>,
    colours: BTreeMap<String, Colour>,
}

pub type Sidecar = (PathBuf, String);

impl Shelf {
    pub fn new(root: PathBuf) -> Self {
        Shelf {
            root,
            entries: Vec::new(),
            colours: BTreeMap::new(),
        }
    }

    pub fn root(&self) -> &Path {
        &self.root
    }

    pub fn entries(&self) -> &[Entry] {
        &self.entries
    }

    pub fn reload(&mut self) -> Result<(), String> {
        self.entries = walk(&self.root)?;
        let sidecar = self.root.join(COLOURS);
        self.colours = match fs::read_to_string(&sidecar) {
            Ok(text) => serde_json::from_str(&text)
                .map_err(|error| format!("{}: {error}", sidecar.display()))?,
            Err(error) if error.kind() == ErrorKind::NotFound => BTreeMap::new(),
            Err(error) => return Err(format!("{}: {error}", sidecar.display())),
        };
        Ok(())
    }

    pub fn colour(&self, path: &Path) -> Option<Colour> {
        self.key(path)
            .and_then(|key| self.colours.get(&key).copied())
    }

    pub fn paint(&mut self, path: &Path, colour: Option<Colour>) -> Result<Sidecar, String> {
        let key = self
            .key(path)
            .ok_or_else(|| format!("{} is outside {}", path.display(), self.root.display()))?;
        match colour {
            Some(colour) => self.colours.insert(key, colour),
            None => self.colours.remove(&key),
        };
        self.sidecar()
    }

    pub fn carry(&mut self, from: &Path, to: Option<&Path>) -> Result<Option<Sidecar>, String> {
        let Some(from) = self.key(from) else {
            return Ok(None);
        };
        let under = |key: &str| {
            key.strip_prefix(from.as_str())
                .is_some_and(|rest| rest.is_empty() || rest.starts_with('/'))
        };
        let carried: Vec<(String, Colour)> = self
            .colours
            .iter()
            .filter(|(key, _)| under(key))
            .map(|(key, colour)| (key.clone(), *colour))
            .collect();
        if carried.is_empty() {
            return Ok(None);
        }
        self.colours.retain(|key, _| !under(key));
        if let Some(to) = to.and_then(|to| self.key(to)) {
            for (key, colour) in carried {
                let rest = key.get(from.len()..).unwrap_or_default();
                self.colours.insert(format!("{to}{rest}"), colour);
            }
        }
        self.sidecar().map(Some)
    }

    fn sidecar(&self) -> Result<Sidecar, String> {
        let path = self.root.join(COLOURS);
        serde_json::to_string_pretty(&self.colours)
            .map(|text| (path, text))
            .map_err(|error| error.to_string())
    }

    fn key(&self, path: &Path) -> Option<String> {
        let parts: Vec<String> = path
            .strip_prefix(&self.root)
            .ok()?
            .components()
            .map(|part| match part {
                Component::Normal(name) => Some(name.to_string_lossy().into_owned()),
                _ => None,
            })
            .collect::<Option<_>>()?;
        (!parts.is_empty()).then(|| parts.join("/"))
    }
}

fn walk(root: &Path) -> Result<Vec<Entry>, String> {
    let mut entries = Vec::new();
    let mut pending = children(root, 0)?;
    while let Some(entry) = pending.pop() {
        if entry.folder {
            pending.extend(children(&entry.path, entry.depth.saturating_add(1))?);
        }
        entries.push(entry);
    }
    Ok(entries)
}

fn children(folder: &Path, depth: usize) -> Result<Vec<Entry>, String> {
    let read = fs::read_dir(folder).map_err(|error| format!("{}: {error}", folder.display()))?;
    let mut found: Vec<Entry> = read
        .filter_map(Result::ok)
        .filter(|entry| !entry.file_name().to_string_lossy().starts_with('.'))
        .filter_map(|entry| {
            let kind = entry.file_type().ok()?;
            (kind.is_dir() || kind.is_file()).then(|| Entry {
                path: entry.path(),
                depth,
                folder: kind.is_dir(),
            })
        })
        .collect();
    found.sort_by_key(|entry| {
        let name = entry
            .path
            .file_name()
            .map(|name| name.to_string_lossy().to_lowercase());
        (!entry.folder, name)
    });
    found.reverse();
    Ok(found)
}
