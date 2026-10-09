use std::collections::{BTreeSet, HashMap};
use std::ops::Range;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::{Duration, Instant};
use std::{fmt, fs, io};

use desk_core::syntax::Language;
use desk_motion::reduced_motion;
use desk_motion::tokens::{EASE_OUT, HOVER_MS, TOGGLE_MS};
use desk_tiling::{Store, StoreError};
use gpui::{
    AnyElement, App, ClickEvent, Context, ElementId, EventEmitter, Global, Image, ImageFormat,
    ListSizingBehavior, ScrollStrategy, ScrollWheelEvent, SharedString, UniformListScrollHandle,
    Window, div, img, prelude::*, px, uniform_list,
};
use serde_json::Value;

use crate::components::chip::{GitStatus, git_name};
use crate::components::paint::ink;
use crate::components::size::{
    FONT_TREE, HOVER, RADIUS_CHIP, ROW_ON, ROW_PAD_X, TREE_GAP, TREE_ROW,
};
use crate::live::ActiveTheme;
use crate::theme::Theme;

const INDENT: f32 = 20.0;
const GUIDE_X: f32 = 17.0;
const GUIDE: f32 = 0.06;
const IGNORED_FADE: f32 = 0.42;
const ICON_SIZE: f32 = 16.0;
const DOT: f32 = 6.0;
const LABEL_FONT: f32 = 11.0;
const UNHOVER_MS: Duration = Duration::from_millis(100);
const FRAME_LOG_VAR: &str = "DESK_TREE_FRAMES";
const FRAME_WATCH: Duration = Duration::from_millis(1000);
const FRAME_BUDGET_MS: f32 = 1000.0 / 60.0;

const PICK_FILE: &str = "editor-icons";

pub struct OpenProject(pub PathBuf);

impl Global for OpenProject {}

pub struct EditedFile(pub PathBuf);

impl Global for EditedFile {}

#[derive(Debug)]
pub enum IconThemeError {
    Json(serde_json::Error),
    Missing(&'static str),
    NoIcon(String),
    UnknownPack(String),
    Home(StoreError),
    Io(PathBuf, io::Error),
}

impl fmt::Display for IconThemeError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            IconThemeError::Json(error) => write!(f, "icon pack is not json: {error}"),
            IconThemeError::Missing(key) => write!(f, "icon pack has no {key}"),
            IconThemeError::NoIcon(id) => {
                write!(f, "icon pack names {id}, which it has no svg for")
            }
            IconThemeError::UnknownPack(key) => write!(f, "no icon pack is called {key}"),
            IconThemeError::Home(error) => write!(f, "the icon pick has nowhere to live: {error}"),
            IconThemeError::Io(path, error) => write!(f, "{}: {error}", path.display()),
        }
    }
}

impl std::error::Error for IconThemeError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            IconThemeError::Json(error) => Some(error),
            IconThemeError::Home(error) => Some(error),
            IconThemeError::Io(_, error) => Some(error),
            IconThemeError::Missing(_)
            | IconThemeError::NoIcon(_)
            | IconThemeError::UnknownPack(_) => None,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum IconPack {
    Material,
    Catppuccin,
    Github,
    Jetbrains,
    Makinda,
    Phosphor,
    Pierre,
    Symbols,
}

impl IconPack {
    pub const ALL: [IconPack; 8] = [
        IconPack::Material,
        IconPack::Catppuccin,
        IconPack::Github,
        IconPack::Jetbrains,
        IconPack::Makinda,
        IconPack::Phosphor,
        IconPack::Pierre,
        IconPack::Symbols,
    ];

    pub const fn key(self) -> &'static str {
        match self {
            IconPack::Material => "material",
            IconPack::Catppuccin => "catppuccin",
            IconPack::Github => "github",
            IconPack::Jetbrains => "jetbrains",
            IconPack::Makinda => "makinda",
            IconPack::Phosphor => "phosphor",
            IconPack::Pierre => "pierre",
            IconPack::Symbols => "symbols",
        }
    }

    pub const fn label(self) -> &'static str {
        match self {
            IconPack::Material => "Material",
            IconPack::Catppuccin => "Catppuccin",
            IconPack::Github => "GitHub",
            IconPack::Jetbrains => "JetBrains",
            IconPack::Makinda => "Makinda",
            IconPack::Phosphor => "Phosphor",
            IconPack::Pierre => "Pierre",
            IconPack::Symbols => "Symbols",
        }
    }

    fn shards(self) -> &'static [&'static str] {
        macro_rules! pack {
            ($($file:literal),+) => {
                &[$(include_str!(concat!("../../assets/editor-icons/", $file))),+]
            };
        }
        match self {
            IconPack::Material => pack!("material.json"),
            IconPack::Catppuccin => pack!("catppuccin.json", "catppuccin-2.json"),
            IconPack::Github => pack!("github.json"),
            IconPack::Jetbrains => pack!("jetbrains.json"),
            IconPack::Makinda => pack!("makinda.json"),
            IconPack::Phosphor => pack!("phosphor.json"),
            IconPack::Pierre => pack!("pierre.json"),
            IconPack::Symbols => pack!("symbols.json", "symbols-2.json", "symbols-3.json"),
        }
    }

    fn pick_file() -> Result<PathBuf, IconThemeError> {
        Ok(Store::home().map_err(IconThemeError::Home)?.join(PICK_FILE))
    }

    pub fn saved() -> Result<IconPack, IconThemeError> {
        let path = Self::pick_file()?;
        let key = match fs::read_to_string(&path) {
            Ok(key) => key,
            Err(error) if error.kind() == io::ErrorKind::NotFound => {
                return Ok(IconPack::Material);
            }
            Err(error) => return Err(IconThemeError::Io(path, error)),
        };
        let key = key.trim();
        IconPack::ALL
            .into_iter()
            .find(|pack| pack.key() == key)
            .ok_or_else(|| IconThemeError::UnknownPack(key.to_owned()))
    }

    pub fn save(self) -> Result<(), IconThemeError> {
        let path = Self::pick_file()?;
        let written = path
            .parent()
            .map_or(Ok(()), fs::create_dir_all)
            .and_then(|()| fs::write(&path, format!("{}\n", self.key())));
        written.map_err(|error| IconThemeError::Io(path, error))
    }
}

pub struct PickedPack(pub IconPack);

impl Global for PickedPack {}

pub fn picked_pack(cx: &mut App) -> IconPack {
    if let Some(PickedPack(pack)) = cx.try_global::<PickedPack>() {
        return *pack;
    }
    let pack = IconPack::saved().unwrap_or_else(|error| {
        eprintln!("desk: the editor shows Material icons: {error}");
        IconPack::Material
    });
    cx.set_global(PickedPack(pack));
    pack
}

pub fn pick_pack(pack: IconPack, cx: &mut App) -> Result<(), IconThemeError> {
    cx.set_global(PickedPack(pack));
    pack.save()
}

fn vscode_language(language: Language) -> &'static str {
    match language {
        Language::Rust => "rust",
        Language::Go => "go",
        Language::TypeScript => "typescript",
        Language::Tsx => "typescriptreact",
        Language::JavaScript => "javascript",
        Language::Python => "python",
        Language::Json => "json",
        Language::Toml => "toml",
        Language::Markdown => "markdown",
        Language::Bash => "shellscript",
    }
}

impl From<serde_json::Error> for IconThemeError {
    fn from(error: serde_json::Error) -> Self {
        IconThemeError::Json(error)
    }
}

#[derive(Clone)]
struct Icon {
    id: SharedString,
    image: Arc<Image>,
}

type Icons = HashMap<String, Icon>;

pub struct IconTheme {
    file: Icon,
    folder: Icon,
    folder_open: Option<Icon>,
    file_names: Icons,
    file_extensions: Icons,
    language_ids: Icons,
    folder_names: Icons,
    folder_names_open: Icons,
}

impl IconTheme {
    pub fn material() -> Result<Self, IconThemeError> {
        Self::pack(IconPack::Material)
    }

    pub fn pack(pack: IconPack) -> Result<Self, IconThemeError> {
        let shards = pack
            .shards()
            .iter()
            .map(|shard| serde_json::from_str::<Value>(shard))
            .collect::<Result<Vec<_>, _>>()?;
        let [pack, ..] = shards.as_slice() else {
            return Err(IconThemeError::Missing("a pack file"));
        };
        let field = |key: &'static str| pack.get(key).ok_or(IconThemeError::Missing(key));
        let aliases = field("aliases")?
            .as_object()
            .ok_or(IconThemeError::Missing("aliases"))?;
        let images = shards
            .iter()
            .map(|shard| {
                shard
                    .get("icons")
                    .and_then(Value::as_object)
                    .ok_or(IconThemeError::Missing("icons"))
            })
            .collect::<Result<Vec<_>, _>>()?
            .into_iter()
            .flatten()
            .map(|(id, svg)| {
                let svg = svg
                    .as_str()
                    .ok_or_else(|| IconThemeError::NoIcon(id.clone()))?;
                let image = Image::from_bytes(ImageFormat::Svg, svg.as_bytes().to_vec());
                Ok((id.as_str(), Arc::new(image)))
            })
            .collect::<Result<HashMap<_, _>, IconThemeError>>()?;
        let icon = |id: &Value| -> Result<Icon, IconThemeError> {
            let named = id
                .as_str()
                .ok_or_else(|| IconThemeError::NoIcon(id.to_string()))?;
            let drawn = aliases.get(named).and_then(Value::as_str).unwrap_or(named);
            let image = images
                .get(drawn)
                .ok_or_else(|| IconThemeError::NoIcon(named.to_owned()))?;
            Ok(Icon {
                id: named.to_owned().into(),
                image: image.clone(),
            })
        };
        let table = |key: &'static str| -> Result<Icons, IconThemeError> {
            field(key)?
                .as_object()
                .ok_or(IconThemeError::Missing(key))?
                .iter()
                .map(|(name, id)| Ok((name.to_lowercase(), icon(id)?)))
                .collect()
        };
        Ok(IconTheme {
            file: icon(field("file")?)?,
            folder: icon(field("folder")?)?,
            folder_open: pack.get("folderOpen").map(icon).transpose()?,
            file_names: table("fileNames")?,
            file_extensions: table("fileExtensions")?,
            language_ids: table("languageIds")?,
            folder_names: table("folderNames")?,
            folder_names_open: table("folderNamesOpen")?,
        })
    }

    fn file(&self, name: &str) -> &Icon {
        let name = name.to_lowercase();
        let by_extension = || {
            name.match_indices('.')
                .filter_map(|(at, _)| name.get(at.saturating_add(1)..))
                .find_map(|extension| self.file_extensions.get(extension))
        };
        let by_language = || {
            Language::from_path(Path::new(&name))
                .and_then(|language| self.language_ids.get(vscode_language(language)))
        };
        self.file_names
            .get(&name)
            .or_else(by_extension)
            .or_else(by_language)
            .unwrap_or(&self.file)
    }

    fn folder(&self, name: &str, open: bool) -> &Icon {
        let name = name.to_lowercase();
        let shut = self.folder_names.get(&name);
        if !open {
            return shut.unwrap_or(&self.folder);
        }
        self.folder_names_open
            .get(&name)
            .or(shut)
            .or(self.folder_open.as_ref())
            .unwrap_or(&self.folder)
    }

    pub fn file_icon(&self, name: &str) -> &str {
        &self.file(name).id
    }

    pub fn file_image(&self, name: &str) -> Arc<Image> {
        self.file(name).image.clone()
    }

    pub fn folder_icon(&self, name: &str, open: bool) -> &str {
        &self.folder(name, open).id
    }

    pub fn folder_image(&self, name: &str, open: bool) -> Arc<Image> {
        self.folder(name, open).image.clone()
    }
}

pub enum NodeKind {
    File,
    Folder(Vec<TreeNode>),
    Unread(Vec<GitStatus>),
}

pub struct TreeNode {
    pub name: SharedString,
    pub git: Option<GitStatus>,
    pub kind: NodeKind,
}

pub enum TreeEvent {
    Opened(SharedString),
    Unfolded(SharedString),
}

impl TreeNode {
    pub fn file(name: impl Into<SharedString>, git: Option<GitStatus>) -> Self {
        TreeNode {
            name: name.into(),
            git,
            kind: NodeKind::File,
        }
    }

    pub fn folder(
        name: impl Into<SharedString>,
        git: Option<GitStatus>,
        children: Vec<TreeNode>,
    ) -> Self {
        TreeNode {
            name: name.into(),
            git,
            kind: NodeKind::Folder(children),
        }
    }

    pub fn unread(
        name: impl Into<SharedString>,
        git: Option<GitStatus>,
        inside: Vec<GitStatus>,
    ) -> Self {
        TreeNode {
            name: name.into(),
            git,
            kind: NodeKind::Unread(inside),
        }
    }

    fn rolled_up(&self) -> Option<GitStatus> {
        let mut found = Vec::new();
        let mut pending = vec![self];
        while let Some(node) = pending.pop() {
            found.extend(node.git);
            match &node.kind {
                NodeKind::File => {}
                NodeKind::Folder(children) => pending.extend(children),
                NodeKind::Unread(inside) => found.extend(inside),
            }
        }
        found
            .into_iter()
            .filter(|git| *git != GitStatus::Ignored)
            .max_by_key(|git| strength(*git))
    }
}

fn badge(git: GitStatus) -> Option<&'static str> {
    match git {
        GitStatus::Modified => Some("M"),
        GitStatus::Added => Some("A"),
        GitStatus::Untracked => Some("U"),
        GitStatus::Deleted => Some("D"),
        GitStatus::Conflict => Some("!"),
        GitStatus::Ignored => None,
    }
}

pub fn strength(git: GitStatus) -> u8 {
    match git {
        GitStatus::Ignored => 0,
        GitStatus::Untracked => 1,
        GitStatus::Added => 2,
        GitStatus::Modified => 3,
        GitStatus::Deleted => 4,
        GitStatus::Conflict => 5,
    }
}

struct Row {
    depth: usize,
    path: SharedString,
    name: SharedString,
    git: Option<GitStatus>,
    folder: bool,
    unread: bool,
    rolled: Option<GitStatus>,
    shut_icon: Arc<Image>,
    open_icon: Arc<Image>,
}

fn row_icons(icons: &IconTheme, name: &str, folder: bool) -> (Arc<Image>, Arc<Image>) {
    match folder {
        true => (
            icons.folder(name, false).image.clone(),
            icons.folder(name, true).image.clone(),
        ),
        false => {
            let icon = &icons.file(name).image;
            (icon.clone(), icon.clone())
        }
    }
}

fn flatten(roots: &[TreeNode], icons: &IconTheme, depth: usize, parent: &str) -> Vec<Row> {
    let mut rows = Vec::new();
    let mut pending: Vec<(usize, SharedString, &TreeNode)> = roots
        .iter()
        .rev()
        .map(|node| match parent {
            "" => (depth, node.name.clone(), node),
            parent => (depth, format!("{parent}/{}", node.name).into(), node),
        })
        .collect();
    while let Some((depth, path, node)) = pending.pop() {
        if let NodeKind::Folder(children) = &node.kind {
            pending.extend(children.iter().rev().map(|child| {
                (
                    depth.saturating_add(1),
                    SharedString::from(format!("{path}/{}", child.name)),
                    child,
                )
            }));
        }
        let folder = !matches!(node.kind, NodeKind::File);
        let (shut_icon, open_icon) = row_icons(icons, &node.name, folder);
        rows.push(Row {
            depth,
            path,
            name: node.name.clone(),
            git: node.git,
            folder,
            unread: matches!(node.kind, NodeKind::Unread(_)),
            rolled: if folder { node.rolled_up() } else { None },
            shut_icon,
            open_icon,
        });
    }
    rows
}

fn read_folders(rows: &[Row]) -> impl Iterator<Item = SharedString> + '_ {
    rows.iter()
        .filter(|row| row.folder && !row.unread)
        .map(|row| row.path.clone())
}

fn inside(path: &str, folder: &str) -> bool {
    path.strip_prefix(folder)
        .is_some_and(|rest| rest.starts_with('/'))
}

fn fraction(elapsed: Duration, span: Duration) -> f32 {
    (elapsed.as_secs_f32() / span.as_secs_f32()).min(1.0)
}

struct Fold {
    started: Instant,
    opening: bool,
}

impl Fold {
    fn eased(&self, now: Instant) -> f32 {
        EASE_OUT(fraction(
            now.saturating_duration_since(self.started),
            TOGGLE_MS,
        ))
    }
}

struct Fade {
    path: SharedString,
    from: f32,
    started: Instant,
    on: bool,
}

impl Fade {
    fn span(&self) -> Duration {
        if self.on { HOVER_MS } else { UNHOVER_MS }
    }

    fn level(&self, now: Instant) -> f32 {
        let to = if self.on { 1.0 } else { 0.0 };
        let eased = EASE_OUT(fraction(
            now.saturating_duration_since(self.started),
            self.span(),
        ));
        self.from + (to - self.from) * eased
    }

    fn done(&self, now: Instant) -> bool {
        now.saturating_duration_since(self.started) >= self.span()
    }

    fn turn(&mut self, on: bool, now: Instant) {
        self.from = self.level(now);
        self.started = now;
        self.on = on;
    }
}

enum FrameLog {
    Off,
    Idle,
    Watching(FrameWatch),
}

impl FrameLog {
    fn from_env() -> Self {
        match std::env::var_os(FRAME_LOG_VAR) {
            Some(_) => FrameLog::Idle,
            None => FrameLog::Off,
        }
    }

    fn watch(&mut self, what: SharedString) {
        let now = Instant::now();
        match std::mem::replace(self, FrameLog::Off) {
            FrameLog::Off => {}
            FrameLog::Watching(watch) => {
                let what = match watch.what == what {
                    true => what,
                    false => format!("{}, then {what}", watch.what).into(),
                };
                *self = FrameLog::Watching(FrameWatch {
                    what,
                    until: now + FRAME_WATCH,
                    ..watch
                });
            }
            FrameLog::Idle => *self = FrameLog::Watching(FrameWatch::new(what, now)),
        }
    }

    fn tick(&mut self, now: Instant) -> bool {
        let FrameLog::Watching(watch) = self else {
            return false;
        };
        watch
            .intervals
            .push(now.saturating_duration_since(watch.last).as_secs_f32() * 1000.0);
        watch.last = now;
        if now < watch.until {
            return true;
        }
        if let FrameLog::Watching(watch) = std::mem::replace(self, FrameLog::Idle) {
            watch.report();
        }
        false
    }
}

struct FrameWatch {
    what: SharedString,
    until: Instant,
    last: Instant,
    intervals: Vec<f32>,
    slowest_rows: f32,
}

impl FrameWatch {
    fn new(what: SharedString, now: Instant) -> Self {
        FrameWatch {
            what,
            until: now + FRAME_WATCH,
            last: now,
            intervals: Vec::new(),
            slowest_rows: 0.0,
        }
    }

    fn report(mut self) {
        let worst_at = self
            .intervals
            .iter()
            .enumerate()
            .max_by(|a, b| a.1.total_cmp(b.1))
            .map_or(0, |(at, _)| at.saturating_add(1));
        self.intervals.sort_by(f32::total_cmp);
        let count = self.intervals.len();
        let at = |share: f32| {
            let index = ((count as f32 - 1.0) * share).round() as usize;
            self.intervals.get(index).copied().unwrap_or_default()
        };
        let over = self
            .intervals
            .iter()
            .filter(|ms| **ms > FRAME_BUDGET_MS)
            .count();
        eprintln!(
            "desk: tree frames {}: {count} frames, median {:.2} ms, p95 {:.2} ms, worst {:.2} ms at frame {worst_at}, {over} over {FRAME_BUDGET_MS:.1} ms, rows built in at most {:.2} ms",
            self.what,
            at(0.5),
            at(0.95),
            at(1.0),
            self.slowest_rows,
        );
    }
}

pub struct FileTree {
    id: SharedString,
    frames: FrameLog,
    rows: Vec<Row>,
    shown: Vec<usize>,
    expanded: BTreeSet<SharedString>,
    folds: HashMap<SharedString, Fold>,
    hovers: Vec<Fade>,
    selected: Option<SharedString>,
    scroll: UniformListScrollHandle,
    badges: bool,
}

impl EventEmitter<TreeEvent> for FileTree {}

impl FileTree {
    pub fn new(id: impl Into<SharedString>, roots: Vec<TreeNode>, icons: &IconTheme) -> Self {
        let rows = flatten(&roots, icons, 0, "");
        let mut tree = FileTree {
            id: id.into(),
            frames: FrameLog::from_env(),
            expanded: read_folders(&rows).collect(),
            rows,
            shown: Vec::new(),
            folds: HashMap::new(),
            hovers: Vec::new(),
            selected: None,
            scroll: UniformListScrollHandle::new(),
            badges: false,
        };
        tree.reshow();
        tree
    }

    pub fn closed(mut self, path: &'static str) -> Self {
        self.expanded.remove(path);
        self.reshow();
        self
    }

    pub fn selected(mut self, path: &'static str) -> Self {
        self.selected = Some(path.into());
        self
    }

    pub fn badges(mut self) -> Self {
        self.badges = true;
        self
    }

    fn reshow(&mut self) {
        let mut hidden_below = None;
        self.shown.clear();
        for (at, row) in self.rows.iter().enumerate() {
            if hidden_below.is_some_and(|depth| row.depth > depth) {
                continue;
            }
            hidden_below = (row.folder && !self.expanded.contains(&row.path)).then_some(row.depth);
            self.shown.push(at);
        }
    }

    pub fn load(&mut self, folder: &str, children: Vec<TreeNode>, icons: &IconTheme) -> bool {
        let Some((at, row)) = self
            .rows
            .iter_mut()
            .enumerate()
            .find(|(_, row)| row.folder && row.path == folder)
        else {
            return false;
        };
        row.unread = false;
        let depth = row.depth;
        let start = at.saturating_add(1);
        let below = self
            .rows
            .get(start..)
            .unwrap_or_default()
            .iter()
            .take_while(|row| row.depth > depth)
            .count();
        self.frames
            .watch(format!("load /{folder}, {} entries", children.len()).into());
        let rows = flatten(&children, icons, depth.saturating_add(1), folder);
        self.rows.splice(start..start.saturating_add(below), rows);
        self.reshow();
        true
    }

    pub fn reset(&mut self, roots: Vec<TreeNode>, icons: &IconTheme) {
        self.rows = flatten(&roots, icons, 0, "");
        self.reshow();
    }

    pub fn open_folders(&self) -> Vec<SharedString> {
        self.expanded.iter().cloned().collect()
    }

    pub fn reicon(&mut self, icons: &IconTheme, cx: &mut Context<Self>) {
        for row in &mut self.rows {
            (row.shut_icon, row.open_icon) = row_icons(icons, &row.name, row.folder);
        }
        cx.notify();
    }

    pub fn select(&mut self, path: SharedString, cx: &mut Context<Self>) {
        if self.selected.as_ref() == Some(&path) {
            return;
        }
        self.hover(path.clone(), false, Instant::now());
        if let Some(at) = self
            .shown
            .iter()
            .position(|row| self.rows.get(*row).is_some_and(|row| row.path == path))
        {
            self.scroll.scroll_to_item(at, ScrollStrategy::Nearest);
        }
        self.selected = Some(path);
        cx.notify();
    }

    pub fn collapse_all(&mut self, cx: &mut Context<Self>) {
        self.expanded.clear();
        self.folds.clear();
        self.reshow();
        cx.notify();
    }

    fn toggle(&mut self, path: SharedString, reduced: bool) {
        let opening = !self.expanded.remove(&path);
        if opening {
            self.expanded.insert(path.clone());
        }
        if !reduced {
            let started = Instant::now();
            self.folds.insert(path, Fold { started, opening });
        }
        self.reshow();
    }

    fn hover(&mut self, path: SharedString, on: bool, now: Instant) {
        for fade in &mut self.hovers {
            if fade.on && (fade.path == path) != on {
                fade.turn(false, now);
            }
        }
        if !on {
            return;
        }
        match self.hovers.iter_mut().find(|fade| fade.path == path) {
            Some(fade) => fade.turn(true, now),
            None => self.hovers.push(Fade {
                path,
                from: 0.0,
                started: now,
                on: true,
            }),
        }
    }

    fn click(&mut self, path: SharedString, folder: bool, unread: bool, cx: &mut Context<Self>) {
        if !folder {
            self.select(path.clone(), cx);
            cx.emit(TreeEvent::Opened(path));
            return;
        }
        self.frames.watch(format!("toggle /{path}").into());
        self.toggle(path.clone(), reduced_motion(cx));
        if unread && self.expanded.contains(&path) {
            cx.emit(TreeEvent::Unfolded(path));
        }
        cx.notify();
    }

    fn openness(&self, row: &Row, now: Instant) -> f32 {
        let open = self.expanded.contains(&row.path);
        match self.folds.get(&row.path) {
            Some(fold) if fold.opening => fold.eased(now),
            Some(fold) => 1.0 - fold.eased(now),
            None if open => 1.0,
            None => 0.0,
        }
    }

    fn reveal(&self, row: &Row, now: Instant) -> f32 {
        self.folds
            .iter()
            .filter(|(folder, fold)| fold.opening && inside(&row.path, folder))
            .map(|(_, fold)| fold.eased(now))
            .fold(1.0, f32::min)
    }

    fn icon(row: &Row, open: f32) -> AnyElement {
        let sized = |image: &Arc<Image>| img(image.clone()).flex_none().size(px(ICON_SIZE));
        if open <= 0.0 {
            return sized(&row.shut_icon).into_any_element();
        }
        if open >= 1.0 {
            return sized(&row.open_icon).into_any_element();
        }
        div()
            .relative()
            .flex_none()
            .size(px(ICON_SIZE))
            .child(sized(&row.shut_icon).absolute().opacity(1.0 - open))
            .child(sized(&row.open_icon).absolute().opacity(open))
            .into_any_element()
    }

    fn row(&self, row: &Row, now: Instant, theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
        let ignored = row.git == Some(GitStatus::Ignored);
        let named = match (ignored, row.folder) {
            (true, _) => None,
            (false, true) => row.rolled.map(|_| GitStatus::Modified),
            (false, false) => row.git,
        };
        let picked = self.selected.as_ref() == Some(&row.path);
        let lit = self
            .hovers
            .iter()
            .find(|fade| fade.path == row.path)
            .filter(|_| !picked)
            .map_or(0.0, |fade| fade.level(now));
        let fill = if picked { ROW_ON } else { HOVER * lit };
        let (folder, unread) = (row.folder, row.unread);
        let label = match (self.badges, row.git) {
            (true, Some(git)) if !folder => badge(git).map(|letter| (letter, git)),
            (false, Some(GitStatus::Conflict)) => Some(("conflict", GitStatus::Conflict)),
            _ => None,
        };
        let open = if folder { self.openness(row, now) } else { 0.0 };
        let reveal = self.reveal(row, now);
        let guides = (1..=row.depth).map(|level| {
            div()
                .absolute()
                .top_0()
                .bottom_0()
                .left(px(GUIDE_X + level.saturating_sub(1) as f32 * INDENT))
                .w(px(1.0))
                .bg(ink(theme, GUIDE))
        });
        let clicked = row.path.clone();
        let hovered = row.path.clone();
        let body = div()
            .flex()
            .flex_1()
            .min_w_0()
            .h_full()
            .items_center()
            .gap(px(TREE_GAP))
            .pl(px(ROW_PAD_X + row.depth as f32 * INDENT))
            .pr(px(ROW_PAD_X))
            .rounded(px(RADIUS_CHIP))
            .when(fill > 0.0, |line| line.bg(ink(theme, fill)))
            .when(ignored, |line| line.opacity(IGNORED_FADE).italic())
            .when(reveal < 1.0, |line| line.opacity(reveal))
            .child(Self::icon(row, open))
            .child(git_name(row.name.clone(), named, theme).flex_1())
            .children(row.rolled.filter(|_| folder).map(|git| {
                div()
                    .flex_none()
                    .size(px(DOT))
                    .rounded(px(DOT / 2.0))
                    .bg(git.color(theme))
            }))
            .children(label.map(|(text, git)| {
                div()
                    .flex_none()
                    .text_size(px(LABEL_FONT))
                    .text_color(git.color(theme))
                    .child(text)
            }));
        div()
            .id(ElementId::Name(row.path.clone()))
            .relative()
            .flex()
            .w_full()
            .min_w_0()
            .h(px(TREE_ROW))
            .text_size(px(FONT_TREE))
            .cursor_pointer()
            .children(guides)
            .child(body)
            .on_hover(cx.listener(move |this, on: &bool, _, cx| {
                this.hover(hovered.clone(), *on, Instant::now());
                cx.notify();
            }))
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                this.click(clicked.clone(), folder, unread, cx);
            }))
            .into_any_element()
    }

    fn visible(&mut self, range: Range<usize>, cx: &mut Context<Self>) -> Vec<AnyElement> {
        let theme = ActiveTheme::theme(cx);
        let now = Instant::now();
        let picked: Vec<usize> = self.shown.get(range).unwrap_or_default().to_vec();
        let rows = picked
            .into_iter()
            .filter_map(|at| self.rows.get(at))
            .map(|row| self.row(row, now, &theme, cx))
            .collect();
        if let FrameLog::Watching(watch) = &mut self.frames {
            let ms = now.elapsed().as_secs_f32() * 1000.0;
            watch.slowest_rows = watch.slowest_rows.max(ms);
        }
        rows
    }
}

impl Render for FileTree {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let now = Instant::now();
        self.folds
            .retain(|_, fold| now.saturating_duration_since(fold.started) < TOGGLE_MS);
        self.hovers.retain(|fade| fade.on || !fade.done(now));
        let watching = self.frames.tick(now);
        if watching || !self.folds.is_empty() || self.hovers.iter().any(|fade| !fade.done(now)) {
            window.request_animation_frame();
        }
        uniform_list(
            self.id.clone(),
            self.shown.len(),
            cx.processor(|tree, range, _, cx| tree.visible(range, cx)),
        )
        .with_sizing_behavior(ListSizingBehavior::Infer)
        .track_scroll(&self.scroll)
        .w_full()
        .flex_1()
        .min_h_0()
        .on_scroll_wheel(cx.listener(|tree, _: &ScrollWheelEvent, _, cx| {
            if !matches!(tree.frames, FrameLog::Off) {
                tree.frames.watch("scroll".into());
                cx.notify();
            }
        }))
    }
}
