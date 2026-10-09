use std::collections::{BTreeSet, HashMap};
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::{Duration, Instant};
use std::{fmt, fs, io};

use desk_core::syntax::Language;
use desk_motion::reduced_motion;
use desk_motion::tokens::{EASE_OUT, HOVER as SLIDE, HOVER_MS, PANEL_OUT_MS, TOGGLE_MS};
use desk_tiling::{Store, StoreError};
use gpui::{
    AnyElement, App, ClickEvent, Context, ElementId, EventEmitter, Global, Image, ImageFormat,
    SharedString, SpringState, Window, div, img, prelude::*, px,
};
use serde_json::Value;

use crate::components::chip::{GitStatus, git_name};
use crate::components::overlay::{actions, context_menu};
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
const SETTLED_PX: f32 = 0.05;
const UNHOVER_MS: Duration = Duration::from_millis(100);

const PICK_FILE: &str = "editor-icons";

pub const TREE_MENU: [&str; 10] = [
    "New file",
    "New folder",
    "Rename  F2",
    "Copy path",
    "Copy relative path",
    "Mention in chat",
    "Open in Terminal",
    "Reveal in Explorer",
    "Collapse all",
    "Delete",
];
const COLLAPSE_ALL: usize = 8;

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
        let icons = shards
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
                let icon = Icon {
                    id: id.clone().into(),
                    image: Arc::new(image),
                };
                Ok((id.as_str(), icon))
            })
            .collect::<Result<HashMap<_, _>, IconThemeError>>()?;
        let icon = |id: &Value| -> Result<Icon, IconThemeError> {
            id.as_str()
                .and_then(|id| icons.get(id))
                .cloned()
                .ok_or_else(|| IconThemeError::NoIcon(id.to_string()))
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

fn strength(git: GitStatus) -> u8 {
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

fn unread_paths(rows: &[Row]) -> impl Iterator<Item = SharedString> + '_ {
    rows.iter()
        .filter(|row| row.unread)
        .map(|row| row.path.clone())
}

fn fraction(elapsed: Duration, span: Duration) -> f32 {
    (elapsed.as_secs_f32() / span.as_secs_f32()).min(1.0)
}

fn fold_span(to: f32) -> Duration {
    if to > 0.0 { TOGGLE_MS } else { PANEL_OUT_MS }
}

struct Fold {
    from: f32,
    started: Instant,
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

#[derive(Clone, Copy)]
struct Slide {
    from: SpringState,
    moved: Instant,
}

impl Slide {
    fn offset(slide: Option<Slide>, now: Instant) -> SpringState {
        slide.map_or(
            SpringState {
                position: 0.0,
                velocity: 0.0,
            },
            |slide| {
                SLIDE.step(
                    slide.from,
                    0.0,
                    now.saturating_duration_since(slide.moved).as_secs_f32(),
                )
            },
        )
    }
}

struct Group {
    depth: usize,
    reveal: f32,
    start: f32,
    rows: Vec<AnyElement>,
}

impl Group {
    fn close(self, y: &mut f32, theme: &Theme) -> AnyElement {
        let content = *y - self.start;
        let shown = content * self.reveal;
        *y = self.start + shown;
        let guide = div()
            .absolute()
            .top_0()
            .bottom_0()
            .left(px(GUIDE_X + self.depth.saturating_sub(1) as f32 * INDENT))
            .w(px(1.0))
            .bg(ink(theme, GUIDE));
        div()
            .relative()
            .flex()
            .flex_col()
            .flex_none()
            .w_full()
            .when(self.reveal < 1.0, |group| {
                group.h(px(shown)).overflow_hidden().opacity(self.reveal)
            })
            .child(guide)
            .children(self.rows)
            .into_any_element()
    }
}

pub struct FileTree {
    id: SharedString,
    rows: Vec<Row>,
    closed: BTreeSet<SharedString>,
    folds: HashMap<SharedString, Fold>,
    hovers: Vec<Fade>,
    selected: Option<SharedString>,
    selected_y: Option<f32>,
    slide: Option<Slide>,
    slide_from: Option<SpringState>,
    badges: bool,
}

impl EventEmitter<TreeEvent> for FileTree {}

impl FileTree {
    pub fn new(id: impl Into<SharedString>, roots: Vec<TreeNode>, icons: &IconTheme) -> Self {
        let rows = flatten(&roots, icons, 0, "");
        FileTree {
            id: id.into(),
            closed: unread_paths(&rows).collect(),
            rows,
            folds: HashMap::new(),
            hovers: Vec::new(),
            selected: None,
            selected_y: None,
            slide: None,
            slide_from: None,
            badges: false,
        }
    }

    pub fn closed(mut self, path: &'static str) -> Self {
        self.closed.insert(path.into());
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
            .iter()
            .skip(start)
            .take_while(|row| row.depth > depth)
            .count();
        let rows = flatten(&children, icons, depth.saturating_add(1), folder);
        self.closed.extend(unread_paths(&rows));
        self.rows.splice(start..start.saturating_add(below), rows);
        true
    }

    pub fn entries(&self) -> impl Iterator<Item = (&str, &str, bool)> {
        self.rows
            .iter()
            .map(|row| (row.path.as_ref(), row.name.as_ref(), row.folder))
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
        let now = Instant::now();
        let at = Slide::offset(self.slide, now);
        self.slide_from = self
            .selected_y
            .filter(|_| !reduced_motion(cx))
            .map(|y| SpringState {
                position: y + at.position,
                velocity: at.velocity,
            });
        self.slide = None;
        self.hover(path.clone(), false, now);
        self.selected = Some(path);
        cx.notify();
    }

    fn openness(&self, path: &SharedString, now: Instant) -> f32 {
        let to = if self.closed.contains(path) { 0.0 } else { 1.0 };
        self.folds.get(path).map_or(to, |fold| {
            let elapsed = now.saturating_duration_since(fold.started);
            fold.from + (to - fold.from) * EASE_OUT(fraction(elapsed, fold_span(to)))
        })
    }

    fn toggle(&mut self, path: SharedString, reduced: bool, now: Instant) {
        let from = self.openness(&path, now);
        if !self.closed.remove(&path) {
            self.closed.insert(path.clone());
        }
        if reduced {
            self.folds.remove(&path);
        } else {
            self.folds.insert(path, Fold { from, started: now });
        }
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
        self.toggle(path.clone(), reduced_motion(cx), Instant::now());
        if unread {
            cx.emit(TreeEvent::Unfolded(path));
        }
        cx.notify();
    }

    fn picked(&mut self, at: &usize, _: &mut Window, cx: &mut Context<Self>) {
        if *at != COLLAPSE_ALL {
            return;
        }
        let now = Instant::now();
        let reduced = reduced_motion(cx);
        let open: Vec<SharedString> = self
            .rows
            .iter()
            .filter(|row| row.folder && !self.closed.contains(&row.path))
            .map(|row| row.path.clone())
            .collect();
        for path in open {
            self.toggle(path, reduced, now);
        }
        cx.notify();
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

    fn row(
        &self,
        row: &Row,
        open: f32,
        own_fill: bool,
        now: Instant,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> AnyElement {
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
            .filter(|fade| !picked || !fade.on)
            .map_or(0.0, |fade| fade.level(now));
        let fill = if own_fill { ROW_ON } else { HOVER * lit };
        let (folder, unread) = (row.folder, row.unread);
        let label = match (self.badges, row.git) {
            (true, Some(git)) if !folder => badge(git).map(|letter| (letter, git)),
            (false, Some(GitStatus::Conflict)) => Some(("conflict", GitStatus::Conflict)),
            _ => None,
        };
        let clicked = row.path.clone();
        let hovered = row.path.clone();
        div()
            .id(ElementId::Name(row.path.clone()))
            .relative()
            .flex()
            .flex_none()
            .items_center()
            .gap(px(TREE_GAP))
            .w_full()
            .min_w_0()
            .h(px(TREE_ROW))
            .pl(px(ROW_PAD_X + row.depth as f32 * INDENT))
            .pr(px(ROW_PAD_X))
            .rounded(px(RADIUS_CHIP))
            .text_size(px(FONT_TREE))
            .cursor_pointer()
            .when(fill > 0.0, |line| line.bg(ink(theme, fill)))
            .when(ignored, |line| line.opacity(IGNORED_FADE).italic())
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
            }))
            .on_hover(cx.listener(move |this, on: &bool, _, cx| {
                this.hover(hovered.clone(), *on, Instant::now());
                cx.notify();
            }))
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                this.click(clicked.clone(), folder, unread, cx);
            }))
            .into_any_element()
    }
}

impl Render for FileTree {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let now = Instant::now();
        let closed = &self.closed;
        self.folds.retain(|path, fold| {
            let to = if closed.contains(path) { 0.0 } else { 1.0 };
            now.saturating_duration_since(fold.started) < fold_span(to)
        });
        self.hovers.retain(|fade| fade.on || !fade.done(now));

        let mut stack = vec![Group {
            depth: 0,
            reveal: 1.0,
            start: 0.0,
            rows: Vec::new(),
        }];
        let mut y = 0.0;
        let mut hidden_below = None;
        let mut selected_at = None;
        for row in &self.rows {
            if hidden_below.is_some_and(|depth| row.depth > depth) {
                continue;
            }
            hidden_below = None;
            while let Some(group) = stack.pop_if(|group| group.depth > row.depth) {
                let closed = group.close(&mut y, &theme);
                if let Some(parent) = stack.last_mut() {
                    parent.rows.push(closed);
                }
            }
            let open = if row.folder {
                self.openness(&row.path, now)
            } else {
                0.0
            };
            let shown = stack.iter().all(|group| group.reveal >= 1.0);
            let picked = self.selected.as_ref() == Some(&row.path);
            if picked && shown {
                selected_at = Some(y);
            }
            let element = self.row(row, open, picked && !shown, now, &theme, cx);
            if let Some(top) = stack.last_mut() {
                top.rows.push(element);
            }
            y += TREE_ROW;
            match (row.folder, open > 0.0) {
                (true, true) => stack.push(Group {
                    depth: row.depth.saturating_add(1),
                    reveal: open,
                    start: y,
                    rows: Vec::new(),
                }),
                (true, false) => hidden_below = Some(row.depth),
                (false, _) => {}
            }
        }
        while let Some(group) = stack.pop_if(|group| group.depth > 0) {
            let closed = group.close(&mut y, &theme);
            if let Some(parent) = stack.last_mut() {
                parent.rows.push(closed);
            }
        }
        let rows = stack.pop().map(|root| root.rows).unwrap_or_default();

        if let (Some(from), Some(at)) = (self.slide_from.take(), selected_at) {
            self.slide = Some(Slide {
                from: SpringState {
                    position: from.position - at,
                    velocity: from.velocity,
                },
                moved: now,
            });
        }
        let offset = Slide::offset(self.slide, now);
        if SLIDE.is_settled(offset, 0.0, SETTLED_PX) {
            self.slide = None;
        }
        self.selected_y = selected_at;

        let moving = !self.folds.is_empty()
            || self.slide.is_some()
            || self.hovers.iter().any(|fade| !fade.done(now));
        if moving {
            window.request_animation_frame();
        }

        let highlight = selected_at.map(|at| {
            div()
                .absolute()
                .left_0()
                .right_0()
                .top(px(at + offset.position))
                .h(px(TREE_ROW))
                .rounded(px(RADIUS_CHIP))
                .bg(ink(&theme, ROW_ON))
        });
        let list = div()
            .relative()
            .flex()
            .flex_col()
            .w_full()
            .min_w_0()
            .children(highlight)
            .children(rows);
        context_menu(actions(TREE_MENU))
            .id(ElementId::Name(format!("{}-menu", self.id).into()))
            .on_pick(cx.listener(Self::picked))
            .child(list)
    }
}
