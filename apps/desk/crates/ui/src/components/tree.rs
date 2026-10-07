use std::collections::{BTreeSet, HashMap};
use std::fmt;
use std::sync::Arc;
use std::time::{Duration, Instant};

use desk_motion::reduced_motion;
use desk_motion::tokens::{EASE_OUT, HOVER as SLIDE, HOVER_MS, PANEL_OUT_MS, TOGGLE_MS};
use gpui::{
    AnyElement, ClickEvent, Context, ElementId, Image, ImageFormat, SharedString, SpringState,
    Window, div, img, prelude::*, px,
};
use serde_json::Value;

use crate::components::chip::{GitStatus, git_name};
use crate::components::overlay::context_menu;
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
const CONFLICT_FONT: f32 = 11.0;
const SETTLED_PX: f32 = 0.05;
const UNHOVER_MS: Duration = Duration::from_millis(100);

const MATERIAL: &str = include_str!("../../assets/files/material.json");

macro_rules! bundled {
    ($($name:literal),* $(,)?) => {
        [$(($name, include_bytes!(concat!("../../assets/files/", $name)).as_slice())),*]
    };
}

const MATERIAL_SVGS: [(&str, &[u8]); 25] = bundled![
    "document.svg",
    "folder.svg",
    "folder-open.svg",
    "folder-github.svg",
    "folder-github-open.svg",
    "folder-command.svg",
    "folder-command-open.svg",
    "folder-database.svg",
    "folder-database-open.svg",
    "folder-src.svg",
    "folder-src-open.svg",
    "folder-public.svg",
    "folder-public-open.svg",
    "go.svg",
    "go-mod.svg",
    "react_ts.svg",
    "typescript.svg",
    "tune.svg",
    "git.svg",
    "readme.svg",
    "license.svg",
    "database.svg",
    "yaml.svg",
    "json.svg",
    "nodejs.svg",
];

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
}

impl fmt::Display for IconThemeError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            IconThemeError::Json(error) => write!(f, "icon theme is not json: {error}"),
            IconThemeError::Missing(key) => write!(f, "icon theme has no {key}"),
            IconThemeError::NoIcon(id) => write!(f, "icon theme names {id}, which is not bundled"),
        }
    }
}

impl std::error::Error for IconThemeError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            IconThemeError::Json(error) => Some(error),
            IconThemeError::Missing(_) | IconThemeError::NoIcon(_) => None,
        }
    }
}

impl From<serde_json::Error> for IconThemeError {
    fn from(error: serde_json::Error) -> Self {
        IconThemeError::Json(error)
    }
}

type Icons = HashMap<String, Arc<Image>>;

pub struct IconTheme {
    file: Arc<Image>,
    folder: Arc<Image>,
    folder_open: Arc<Image>,
    file_names: Icons,
    file_extensions: Icons,
    folder_names: Icons,
    folder_names_open: Icons,
}

impl IconTheme {
    pub fn material() -> Result<Self, IconThemeError> {
        Self::from_vscode(MATERIAL, &MATERIAL_SVGS)
    }

    pub fn from_vscode(json: &str, svgs: &[(&str, &[u8])]) -> Result<Self, IconThemeError> {
        let theme: Value = serde_json::from_str(json)?;
        let field = |key: &'static str| theme.get(key).ok_or(IconThemeError::Missing(key));
        let images = field("iconDefinitions")?
            .as_object()
            .ok_or(IconThemeError::Missing("iconDefinitions"))?
            .iter()
            .map(|(id, definition)| {
                let path = definition
                    .get("iconPath")
                    .and_then(Value::as_str)
                    .ok_or(IconThemeError::Missing("iconPath"))?;
                let (_, bytes) = svgs
                    .iter()
                    .find(|(name, _)| *name == path)
                    .ok_or_else(|| IconThemeError::NoIcon(path.to_owned()))?;
                let image = Image::from_bytes(ImageFormat::Svg, bytes.to_vec());
                Ok((id.as_str(), Arc::new(image)))
            })
            .collect::<Result<HashMap<_, _>, IconThemeError>>()?;
        let image = |id: &Value| {
            id.as_str()
                .and_then(|id| images.get(id))
                .cloned()
                .ok_or_else(|| IconThemeError::NoIcon(id.to_string()))
        };
        let one = |key: &'static str| image(field(key)?);
        let table = |key: &'static str| {
            field(key)?
                .as_object()
                .ok_or(IconThemeError::Missing(key))?
                .iter()
                .map(|(name, id)| Ok((name.to_lowercase(), image(id)?)))
                .collect::<Result<Icons, IconThemeError>>()
        };
        Ok(IconTheme {
            file: one("file")?,
            folder: one("folder")?,
            folder_open: one("folderExpanded")?,
            file_names: table("fileNames")?,
            file_extensions: table("fileExtensions")?,
            folder_names: table("folderNames")?,
            folder_names_open: table("folderNamesExpanded")?,
        })
    }

    fn file(&self, name: &str) -> Arc<Image> {
        let name = name.to_lowercase();
        let by_extension = || {
            name.match_indices('.')
                .filter_map(|(at, _)| name.get(at + 1..))
                .find_map(|extension| self.file_extensions.get(extension))
        };
        self.file_names
            .get(&name)
            .or_else(by_extension)
            .unwrap_or(&self.file)
            .clone()
    }

    fn folder(&self, name: &str, open: bool) -> Arc<Image> {
        let (names, fallback) = if open {
            (&self.folder_names_open, &self.folder_open)
        } else {
            (&self.folder_names, &self.folder)
        };
        names.get(&name.to_lowercase()).unwrap_or(fallback).clone()
    }
}

pub enum NodeKind {
    File,
    Folder(Vec<TreeNode>),
}

pub struct TreeNode {
    pub name: SharedString,
    pub git: Option<GitStatus>,
    pub kind: NodeKind,
}

impl TreeNode {
    pub fn file(name: &'static str, git: Option<GitStatus>) -> Self {
        TreeNode {
            name: name.into(),
            git,
            kind: NodeKind::File,
        }
    }

    pub fn folder(name: &'static str, git: Option<GitStatus>, children: Vec<TreeNode>) -> Self {
        TreeNode {
            name: name.into(),
            git,
            kind: NodeKind::Folder(children),
        }
    }

    fn rolled_up(&self) -> Option<GitStatus> {
        let mut strongest = None;
        let mut pending = vec![self];
        while let Some(node) = pending.pop() {
            if let Some(git) = node.git.filter(|git| *git != GitStatus::Ignored)
                && strongest.is_none_or(|known| strength(git) > strength(known))
            {
                strongest = Some(git);
            }
            if let NodeKind::Folder(children) = &node.kind {
                pending.extend(children);
            }
        }
        strongest
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
    rolled: Option<GitStatus>,
    shut_icon: Arc<Image>,
    open_icon: Arc<Image>,
}

fn flatten(roots: &[TreeNode], icons: &IconTheme) -> Vec<Row> {
    let mut rows = Vec::new();
    let mut pending: Vec<(usize, SharedString, &TreeNode)> = roots
        .iter()
        .rev()
        .map(|node| (0, node.name.clone(), node))
        .collect();
    while let Some((depth, path, node)) = pending.pop() {
        let (folder, shut_icon, open_icon) = match &node.kind {
            NodeKind::File => {
                let icon = icons.file(&node.name);
                (false, icon.clone(), icon)
            }
            NodeKind::Folder(children) => {
                pending.extend(children.iter().rev().map(|child| {
                    (
                        depth.saturating_add(1),
                        SharedString::from(format!("{path}/{}", child.name)),
                        child,
                    )
                }));
                (
                    true,
                    icons.folder(&node.name, false),
                    icons.folder(&node.name, true),
                )
            }
        };
        rows.push(Row {
            depth,
            path,
            name: node.name.clone(),
            git: node.git,
            folder,
            rolled: if folder { node.rolled_up() } else { None },
            shut_icon,
            open_icon,
        });
    }
    rows
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
}

impl FileTree {
    pub fn new(id: impl Into<SharedString>, roots: Vec<TreeNode>, icons: &IconTheme) -> Self {
        FileTree {
            id: id.into(),
            rows: flatten(&roots, icons),
            closed: BTreeSet::new(),
            folds: HashMap::new(),
            hovers: Vec::new(),
            selected: None,
            selected_y: None,
            slide: None,
            slide_from: None,
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

    fn click(&mut self, path: SharedString, folder: bool, cx: &mut Context<Self>) {
        let now = Instant::now();
        let reduced = reduced_motion(cx);
        if folder {
            self.toggle(path, reduced, now);
        } else if self.selected.as_ref() != Some(&path) {
            let at = Slide::offset(self.slide, now);
            self.slide_from = self.selected_y.filter(|_| !reduced).map(|y| SpringState {
                position: y + at.position,
                velocity: at.velocity,
            });
            self.slide = None;
            self.hover(path.clone(), false, now);
            self.selected = Some(path);
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
        let folder = row.folder;
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
            .children((row.git == Some(GitStatus::Conflict)).then(|| {
                div()
                    .flex_none()
                    .text_size(px(CONFLICT_FONT))
                    .text_color(GitStatus::Conflict.color(theme))
                    .child("conflict")
            }))
            .on_hover(cx.listener(move |this, on: &bool, _, cx| {
                this.hover(hovered.clone(), *on, Instant::now());
                cx.notify();
            }))
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                this.click(clicked.clone(), folder, cx);
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
        context_menu(TREE_MENU.map(SharedString::from).to_vec())
            .id(ElementId::Name(format!("{}-menu", self.id).into()))
            .on_pick(cx.listener(Self::picked))
            .child(list)
    }
}
