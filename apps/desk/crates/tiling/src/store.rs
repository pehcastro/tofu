use std::collections::HashSet;
use std::env;
use std::fmt;
use std::fs;
use std::io::{self, ErrorKind};
use std::path::{Component, Path, PathBuf};

use crate::{Axis, MAX_TILES, Module, Node, Part, Preset, Size, Stack, TileId, Workspace};

const HEADER_V1: &str = "tofu-desk-layout 1";
const HEADER_V2: &str = "tofu-desk-layout 2";
const WORKSPACE: &str = "workspace";
const EMPTY: &str = "(empty)";
const NO_FOCUS: &str = "-";
const PLUGIN_PREFIX: &str = "plugin:";
const FOLDER: &str = "tofu-desk";
const LAYOUTS: &str = "layouts";
const EXTENSION: &str = "layout";
const MAX_DEPTH: usize = MAX_TILES;
const V1_NAME: &str = "work";

#[derive(Debug)]
pub enum StoreError {
    NoHome(&'static str),
    BadProject(String),
    Io { path: PathBuf, error: io::Error },
    Parse { path: PathBuf, reason: String },
}

impl fmt::Display for StoreError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            StoreError::NoHome(names) => {
                write!(f, "{names} is not set, so the layout has nowhere to live")
            }
            StoreError::BadProject(project) => {
                write!(f, "project name {project} is not a plain folder name")
            }
            StoreError::Io { path, error } => write!(f, "{}: {error}", path.display()),
            StoreError::Parse { path, reason } => write!(f, "{}: {reason}", path.display()),
        }
    }
}

impl std::error::Error for StoreError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            StoreError::Io { error, .. } => Some(error),
            StoreError::NoHome(_) | StoreError::BadProject(_) | StoreError::Parse { .. } => None,
        }
    }
}

pub struct Store {
    path: PathBuf,
}

fn config_home() -> Result<PathBuf, StoreError> {
    let from = |name: &str| {
        env::var_os(name)
            .filter(|value| !value.is_empty())
            .map(PathBuf::from)
    };
    if cfg!(windows) {
        from("APPDATA").ok_or(StoreError::NoHome("APPDATA"))
    } else {
        from("XDG_CONFIG_HOME")
            .or_else(|| from("HOME").map(|home| home.join(".config")))
            .ok_or(StoreError::NoHome("neither XDG_CONFIG_HOME nor HOME"))
    }
}

impl Store {
    pub fn for_project(project: &str) -> Result<Store, StoreError> {
        let name = Path::new(project);
        if !name
            .components()
            .all(|part| matches!(part, Component::Normal(_)))
        {
            return Err(StoreError::BadProject(project.to_owned()));
        }
        let path = config_home()?
            .join(FOLDER)
            .join(LAYOUTS)
            .join(name)
            .with_extension(EXTENSION);
        Ok(Store { path })
    }

    pub fn at(path: PathBuf) -> Store {
        Store { path }
    }

    pub fn load(&self) -> Result<Option<Vec<Workspace>>, StoreError> {
        let text = match fs::read_to_string(&self.path) {
            Ok(text) => text,
            Err(error) if error.kind() == ErrorKind::NotFound => return Ok(None),
            Err(error) => {
                return Err(StoreError::Io {
                    path: self.path.clone(),
                    error,
                });
            }
        };
        parse(&text).map(Some).map_err(|reason| StoreError::Parse {
            path: self.path.clone(),
            reason,
        })
    }

    pub fn save(&self, workspaces: &[Workspace]) -> Result<(), StoreError> {
        let failed = |error: io::Error| StoreError::Io {
            path: self.path.clone(),
            error,
        };
        if let Some(parent) = self.path.parent() {
            fs::create_dir_all(parent).map_err(failed)?;
        }
        let mut text = format!("{HEADER_V2}\n");
        for workspace in workspaces {
            let focus = workspace
                .focus()
                .map_or(NO_FOCUS.to_owned(), |focus| focus.0.to_string());
            text.push_str(&format!(
                "{WORKSPACE} {} {} {focus} {} {}\n",
                workspace.preset.name(),
                u8::from(workspace.locked),
                workspace.next_id(),
                workspace.name.replace(['\n', '\r'], " "),
            ));
            match workspace.tree() {
                Some(tree) => write(tree, &mut text),
                None => text.push_str(EMPTY),
            }
            text.push('\n');
        }
        fs::write(&self.path, text).map_err(failed)
    }
}

fn module_key(module: &Module) -> String {
    match module {
        Module::Plugin(id) => format!("{PLUGIN_PREFIX}{id}"),
        other => other.key().to_owned(),
    }
}

fn write(node: &Node, out: &mut String) {
    match node {
        Node::Tile(stack) => {
            out.push_str(&format!("(tile {} {}", stack.id.0, stack.active));
            for module in &stack.modules {
                out.push(' ');
                out.push_str(&module_key(module));
            }
            out.push(')');
        }
        Node::Split { axis, parts } => {
            out.push_str(match axis {
                Axis::Row => "(row",
                Axis::Column => "(column",
            });
            for part in parts {
                match part.size {
                    Size::Share(share) => out.push_str(&format!(" s{share} ")),
                    Size::Fixed(pixels) => out.push_str(&format!(" f{pixels} ")),
                }
                write(&part.node, out);
            }
            out.push(')');
        }
    }
}

fn parse(text: &str) -> Result<Vec<Workspace>, String> {
    let mut lines = text.lines();
    match lines.next().map(str::trim) {
        Some(HEADER_V1) => {
            let body: Vec<&str> = lines.collect();
            let (tree, next) = tree(&body.join(" "), Version::One)?;
            Ok(vec![Workspace::restore(
                V1_NAME.to_owned(),
                Preset::Work,
                false,
                tree,
                None,
                next,
            )])
        }
        Some(HEADER_V2) => {
            let mut workspaces = Vec::new();
            while let Some(line) = lines.next() {
                if line.trim().is_empty() {
                    continue;
                }
                let body = lines
                    .next()
                    .ok_or("a workspace line has no layout after it")?;
                workspaces.push(workspace(line, body)?);
            }
            Ok(workspaces)
        }
        _ => Err(format!(
            "the first line is neither {HEADER_V1} nor {HEADER_V2}"
        )),
    }
}

fn workspace(line: &str, body: &str) -> Result<Workspace, String> {
    let mut words = line.splitn(6, ' ');
    if words.next() != Some(WORKSPACE) {
        return Err(format!("expected a {WORKSPACE} line, found {line}"));
    }
    let preset = words.next().ok_or("a workspace has no preset")?;
    let preset = Preset::ALL
        .into_iter()
        .find(|known| known.name() == preset)
        .ok_or_else(|| format!("unknown preset {preset}"))?;
    let locked = match words.next() {
        Some("0") => false,
        Some("1") => true,
        other => {
            return Err(format!(
                "locked is 0 or 1, not {}",
                other.unwrap_or("nothing")
            ));
        }
    };
    let focus = match words.next().ok_or("a workspace has no focus")? {
        NO_FOCUS => None,
        id => Some(TileId(
            id.parse().map_err(|error| format!("focus {id}: {error}"))?,
        )),
    };
    let saved: u32 = words
        .next()
        .ok_or("a workspace has no next tile id")?
        .parse()
        .map_err(|error| format!("next tile id: {error}"))?;
    let name = words.next().map(str::trim).filter(|name| !name.is_empty());
    let name = name.ok_or("a workspace has no name")?.to_owned();
    let (tree, next) = tree(body, Version::Two)?;
    if next > saved {
        return Err(format!(
            "workspace {name} uses tile id {} but its next id is {saved}",
            next - 1
        ));
    }
    Ok(Workspace::restore(name, preset, locked, tree, focus, saved))
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Version {
    One,
    Two,
}

fn tree(body: &str, version: Version) -> Result<(Option<Node>, u32), String> {
    if body.trim() == EMPTY {
        return Ok((None, 0));
    }
    let spaced = body.replace('(', " ( ").replace(')', " ) ");
    let mut parser = Parser {
        tokens: spaced.split_whitespace().collect(),
        at: 0,
        version,
        seen: HashSet::new(),
        next: 0,
    };
    let node = parser.node(0)?;
    match parser.next() {
        None => Ok((Some(node), parser.next)),
        Some(extra) => Err(format!("unexpected {extra} after the layout")),
    }
}

struct Parser<'a> {
    tokens: Vec<&'a str>,
    at: usize,
    version: Version,
    seen: HashSet<TileId>,
    next: u32,
}

fn module(key: &str) -> Result<Module, String> {
    if let Some(id) = key.strip_prefix(PLUGIN_PREFIX) {
        let plain = id
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || matches!(c, '-' | '_' | '.'));
        return (plain && !id.is_empty())
            .then(|| Module::Plugin(id.to_owned()))
            .ok_or_else(|| format!("plugin id {id} is not a plain name"));
    }
    Module::BUILT_IN
        .into_iter()
        .find(|module| module.key() == key)
        .ok_or_else(|| format!("unknown module {key}"))
}

impl<'a> Parser<'a> {
    fn next(&mut self) -> Option<&'a str> {
        let token = self.tokens.get(self.at).copied();
        self.at += 1;
        token
    }

    fn expect(&mut self, want: &str) -> Result<(), String> {
        match self.next() {
            Some(token) if token == want => Ok(()),
            Some(token) => Err(format!("expected {want}, found {token}")),
            None => Err(format!("expected {want}, found the end")),
        }
    }

    fn node(&mut self, depth: usize) -> Result<Node, String> {
        if depth > MAX_DEPTH {
            return Err(format!("the layout nests deeper than {MAX_DEPTH}"));
        }
        self.expect("(")?;
        match self.next() {
            Some("tile") => self.tile(),
            Some("row") => self.split(Axis::Row, depth),
            Some("column") => self.split(Axis::Column, depth),
            Some(other) => Err(format!("unknown node {other}")),
            None => Err("the layout ends inside a node".to_owned()),
        }
    }

    fn number<T: std::str::FromStr>(&mut self, what: &str) -> Result<T, String>
    where
        T::Err: fmt::Display,
    {
        let token = self.next().ok_or_else(|| format!("a tile has no {what}"))?;
        token
            .parse()
            .map_err(|error| format!("a tile's {what} {token}: {error}"))
    }

    fn tile(&mut self) -> Result<Node, String> {
        let id = match self.version {
            Version::One => TileId(self.next),
            Version::Two => TileId(self.number("id")?),
        };
        let active: usize = self.number("active index")?;
        let mut modules = Vec::new();
        loop {
            match self.next() {
                Some(")") => break,
                Some(key) => modules.push(module(key)?),
                None => return Err("the layout ends inside a tile".to_owned()),
            }
        }
        if active >= modules.len().max(1) {
            return Err(format!(
                "a tile with {} modules has active index {active}",
                modules.len()
            ));
        }
        if self.seen.len() >= MAX_TILES {
            return Err(format!("the layout has more than {MAX_TILES} tiles"));
        }
        if !self.seen.insert(id) {
            return Err(format!("tile id {} appears twice", id.0));
        }
        self.next = self.next.max(id.0.saturating_add(1));
        Ok(Node::Tile(Stack {
            id,
            modules,
            active,
        }))
    }

    fn size(&self, token: &str) -> Result<Size, String> {
        let positive = |text: &str| {
            text.parse::<f32>()
                .ok()
                .filter(|value| value.is_finite() && *value > 0.0)
                .ok_or_else(|| format!("size {token} is not a positive number"))
        };
        match self.version {
            Version::One => positive(token).map(Size::Share),
            Version::Two => match token.split_at_checked(1) {
                Some(("s", share)) => positive(share).map(Size::Share),
                Some(("f", pixels)) => positive(pixels).map(Size::Fixed),
                _ => Err(format!("size {token} is neither s<share> nor f<pixels>")),
            },
        }
    }

    fn split(&mut self, axis: Axis, depth: usize) -> Result<Node, String> {
        let mut parts = Vec::new();
        loop {
            let size = match self.next() {
                Some(")") => break,
                Some(token) => self.size(token)?,
                None => return Err("the layout ends inside a split".to_owned()),
            };
            parts.push(Part {
                size,
                node: self.node(depth + 1)?,
            });
        }
        if parts.len() < 2 {
            return Err("a split holds fewer than two parts".to_owned());
        }
        Ok(Node::Split { axis, parts })
    }
}
