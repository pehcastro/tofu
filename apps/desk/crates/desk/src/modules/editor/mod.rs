mod blame;
mod kit;
mod listing;
mod watch;

use std::collections::BTreeSet;
use std::fs;
use std::path::{Component, Path, PathBuf};
use std::rc::Rc;
use std::sync::Arc;
use std::time::{Duration, Instant};

use desk_core::git::{Against, Git, GitBinary, GitError, Hunk};
use desk_core::home::Home;
use desk_core::symbols::{Index, Symbol, indexed};
use desk_core::syntax::Language;
use desk_ui::component::{control, icon};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{Header, inner_card, shell};
use desk_ui::components::code::{GutterMark, LineMarks, Marks};
use desk_ui::components::code_editor::{Asked, CodeEditor, Highlight, Jump, Way, open_phase};
use desk_ui::components::empty::empty_state;
use desk_ui::components::form::TextInput;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::history::inline_blame;
use desk_ui::components::keymap::{Keymap, host};
use desk_ui::components::overlay::{MenuItem, context_menu};
use desk_ui::components::sheet::Sheet;
use desk_ui::components::tabs::{Tab, TabEvent, TabMark, connected_tabs};
use desk_ui::components::title_bar::Github;
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::components::tree::{
    Draft, EditedFile, FileTree, IconPack, IconTheme, OpenProject, PickedPack, TreeEvent, TreeNode,
    TreeSpot, picked_pack,
};
use desk_ui::components::vim::Vim;
use desk_ui::icon::Icon;
use desk_ui::live::ActiveTheme;
use desk_ui::motion::reduced_motion;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, AnyView, App, AppContext, Bounds, ClickEvent, ClipboardItem, Context, CursorStyle,
    Entity, Focusable, KeyDownEvent, MouseButton, MouseDownEvent, MouseMoveEvent, Pixels, Point,
    Render, SharedString, Subscription, Task, Window, canvas, div, prelude::*, px, relative,
};
use std::cell::Cell;

use crate::modules::chat::Find;
use crate::title_bar::{ask_github, github};
use blame::{Blamed, Inline, Me};
use listing::{Listing, states};
use watch::Watched;

const BLAME_IDLE: Duration = Duration::from_millis(500);
const DOUBLE_CLICK: Duration = Duration::from_millis(500);
const SPLIT_KEY: &str = "\\";
const SIDE_SHARE: f32 = 0.26;
const SIDE_LEAST: f32 = 180.0;
const SIDE_MOST: f32 = 320.0;
const PATH_HEIGHT: f32 = 30.0;
const SPLIT_BUTTON: f32 = 22.0;
const SPLIT_GLYPH_W: f32 = 14.0;
const SPLIT_GLYPH_H: f32 = 12.0;
const SPLIT_GLYPH_RADIUS: f32 = 3.0;
const BAR_GAP: f32 = 8.0;
const CLOSE_ICON: f32 = 12.0;
const PANE_LEAST: f32 = 240.0;
const DIVIDER_HIT: f32 = 4.0;
const SPLIT_SHARE: f32 = 0.5;
const UNSAVED_BODY: &str = "Your changes are lost if you close without saving.";
const KEYMAP_FILE: &str = "keymap.json";
const SETTINGS_FILE: &str = "settings.json";
const GUIDES_KEY: &str = "editor.guides";
const CACHE_TAG: &str = "CACHEDIR.TAG";
const GIT_DIR: &str = ".git";
const MB: f64 = 1024.0 * 1024.0;
const CLOSED_PAGES: usize = 16;

pub fn open(board: Option<&str>, window: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    let root = match cx.try_global::<OpenProject>() {
        Some(OpenProject(folder)) => folder.clone(),
        None => std::env::current_dir()
            .map_err(|error| format!("the editor has no working folder: {error}"))?,
    };
    open_dir(&root, None, board, window, cx)
}

pub fn open_file(
    path: &Path,
    board: Option<&str>,
    window: &mut Window,
    cx: &mut App,
) -> Result<AnyView, String> {
    let full = absolute(path);
    let folder = full.parent().unwrap_or(&full).to_path_buf();
    open_dir(&folder, Some(&full), board, window, cx)
}

pub fn open_dir(
    dir: &Path,
    file: Option<&Path>,
    board: Option<&str>,
    window: &mut Window,
    cx: &mut App,
) -> Result<AnyView, String> {
    if let Some(board) = board {
        return Err(format!("the editor has no boards, so {board} is not drawn"));
    }
    kit::load_fonts(cx)?;
    let root = absolute(dir);
    if !root.is_dir() {
        return Err(format!("--dir {} is not a folder", root.display()));
    }
    let folder = Folder::new(root, cx)?;
    let file = file.map(absolute);
    Ok(cx
        .new(|cx| {
            let mut editor = Editor::new(folder, window, cx);
            if let Some(file) = file {
                editor.open_path(file, Open::Pinned, window, cx);
            }
            editor
        })
        .into())
}

type Opened = Result<Entity<CodeEditor>, SharedString>;
type Shown<'a> = (&'a [Page], usize, Option<&'a PathBuf>);

struct Repo {
    git: Arc<GitBinary>,
    path: String,
    wanted: Option<String>,
    blamed: Option<Blamed>,
    inline: Option<Inline>,
    absent_from_head: bool,
}

struct Page {
    path: PathBuf,
    name: SharedString,
    code: Opened,
    repo: Option<Repo>,
    dirty: bool,
    stale: bool,
    on_disk: Option<u64>,
    _changes: Option<Subscription>,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Open {
    Preview,
    Pinned,
}

impl Open {
    fn word(self) -> &'static str {
        match self {
            Open::Preview => "preview",
            Open::Pinned => "pinned",
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Side {
    Left,
    Right,
}

impl Side {
    fn other(self) -> Side {
        match self {
            Side::Left => Side::Right,
            Side::Right => Side::Left,
        }
    }

    fn tabs_id(self) -> &'static str {
        match self {
            Side::Left => "editor-file-tabs",
            Side::Right => "editor-file-tabs-right",
        }
    }

    fn tile(self) -> u32 {
        match self {
            Side::Left => 0,
            Side::Right => 1,
        }
    }
}

pub(crate) struct OpenTab {
    pub(crate) tile: u32,
    pub(crate) index: usize,
    pub(crate) path: PathBuf,
    pub(crate) active: bool,
    pub(crate) dirty: bool,
    pub(crate) stale: bool,
    pub(crate) code: Option<Entity<CodeEditor>>,
}

pub(crate) struct Hit {
    pub(crate) path: PathBuf,
    pub(crate) line: usize,
    pub(crate) col: usize,
    pub(crate) text: String,
}

struct Pane {
    pages: Vec<Page>,
    active: usize,
    preview: Option<PathBuf>,
}

struct Opening {
    path: PathBuf,
    how: Open,
    started: Instant,
}

enum Asking {
    Close {
        path: PathBuf,
        failed: Option<String>,
    },
    Delete {
        paths: Vec<String>,
    },
}

struct Spot {
    path: PathBuf,
    row: usize,
    col: usize,
}

struct Folder {
    listing: Arc<Listing>,
    icons: IconTheme,
    pack: IconPack,
    git: Option<Arc<GitBinary>>,
    tree: Entity<FileTree>,
    watched: Option<flume::Sender<Watched>>,
}

impl Folder {
    fn new(root: PathBuf, cx: &mut App) -> Result<Self, String> {
        let pack = picked_pack(cx);
        let icons = IconTheme::pack(pack).map_err(|error| error.to_string())?;
        let tree = FileTree::new("editor-files", Vec::new(), &icons)
            .badges()
            .focusable(cx);
        Ok(Folder {
            git: repository(&root),
            listing: Arc::new(Listing::new(root)),
            icons,
            pack,
            tree: cx.new(|_| tree),
            watched: None,
        })
    }

    fn relative(&self, path: &Path) -> Option<String> {
        self.listing.relative(path)
    }
}

pub struct Editor {
    folder: Folder,
    pages: Vec<Page>,
    active: usize,
    preview: Option<PathBuf>,
    side: Side,
    aside: Option<Pane>,
    share: f32,
    dividing: bool,
    span: Rc<Cell<Bounds<Pixels>>>,
    opening: Option<Opening>,
    peeked: Option<(PathBuf, Instant)>,
    asking: Option<Asking>,
    closed: Vec<PathBuf>,
    find_logged: Option<(String, usize, usize)>,
    github: Github,
    alerted: SharedString,
    menu: TreeMenu,
    keymap: Rc<Keymap>,
    vim: bool,
    guides: bool,
    symbols: Option<Arc<Index>>,
    back: Vec<Spot>,
    ahead: Vec<Spot>,
    choosing: Option<(Spot, Vec<Symbol>)>,
    choices_at: Option<Point<Pixels>>,
    _tree: Subscription,
    _pack: Subscription,
    _project: Subscription,
    _edited: Subscription,
    _watching: Option<Task<()>>,
    _refreshing: Option<Task<()>>,
    _unfolding: Vec<Task<()>>,
    _marking: Option<Task<()>>,
    _blaming: Option<Task<()>>,
    _indexing: Option<Task<()>>,
}

impl Editor {
    fn new(folder: Folder, window: &mut Window, cx: &mut Context<Self>) -> Self {
        let home = Home::find();
        let mut editor = Editor {
            _tree: cx.subscribe_in(&folder.tree, window, Self::tree_event),
            folder,
            pages: Vec::new(),
            active: 0,
            preview: None,
            side: Side::Left,
            aside: None,
            share: SPLIT_SHARE,
            dividing: false,
            span: Rc::default(),
            opening: None,
            peeked: None,
            asking: None,
            closed: Vec::new(),
            find_logged: None,
            github: Github::Asking,
            alerted: SharedString::default(),
            menu: TreeMenu {
                spot: TreeSpot::Empty,
                open_at: None,
                asked: None,
                clip: None,
                done: Vec::new(),
                undone: Vec::new(),
                naming: None,
            },
            keymap: Rc::new(match &home {
                Ok(home) => Keymap::load(&home.config.join(KEYMAP_FILE)),
                Err(error) => {
                    eprintln!("desk: keymap has no home, so the defaults stand: {error}");
                    Keymap::default()
                }
            }),
            vim: home
                .as_ref()
                .is_ok_and(|home| Vim::enabled(&home.config.join(SETTINGS_FILE))),
            guides: home
                .as_ref()
                .is_ok_and(|home| switched_on(&home.config.join(SETTINGS_FILE), GUIDES_KEY)),
            symbols: None,
            back: Vec::new(),
            ahead: Vec::new(),
            choosing: None,
            choices_at: None,
            _pack: cx.observe_global::<PickedPack>(Self::repack),
            _project: cx.observe_global_in::<OpenProject>(window, Self::reroot),
            _edited: cx.observe_global::<EditedFile>(Self::edited),
            _watching: None,
            _refreshing: None,
            _unfolding: Vec::new(),
            _marking: None,
            _blaming: None,
            _indexing: None,
        };
        editor.start_watching(cx);
        editor.refresh(cx);
        editor.index(cx);
        cx.spawn(async move |this, cx| {
            let answer = cx.background_executor().spawn(async { ask_github() }).await;
            let told = this.update(cx, |editor, cx| editor.signed_in(github(answer), cx));
            if let Err(error) = told {
                eprintln!("desk: gh answered after the editor closed: {error}");
            }
        })
        .detach();
        editor
    }

    fn signed_in(&mut self, github: Github, cx: &mut Context<Self>) {
        self.github = github;
        let Some(Page {
            code: Ok(code),
            repo: Some(repo),
            ..
        }) = self.pages.get_mut(self.active)
        else {
            return;
        };
        repo.inline = None;
        let code = code.clone();
        self.follow_blame(&code, cx);
    }

    fn reroot(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let OpenProject(root) = cx.global::<OpenProject>();
        let root = absolute(root);
        if root == self.folder.listing.root {
            return;
        }
        let folder = match Folder::new(root, cx) {
            Ok(folder) => folder,
            Err(error) => return eprintln!("desk: editor keeps its folder: {error}"),
        };
        eprintln!("desk: editor rooted at {}", folder.listing.root.display());
        self._tree = cx.subscribe_in(&folder.tree, window, Self::tree_event);
        self.folder = folder;
        let aside = self.aside.take().map(|pane| pane.pages).unwrap_or_default();
        self.pages.extend(aside);
        self.pages.retain(|page| page.dirty);
        self.active = 0;
        self.preview = None;
        self.side = Side::Left;
        self.asking = None;
        self.back.clear();
        self.ahead.clear();
        self.start_watching(cx);
        self.refresh(cx);
        self.index(cx);
        cx.notify();
    }

    fn start_watching(&mut self, cx: &mut Context<Self>) {
        let (watched, changes) = match watch::watch(self.folder.listing.root.clone()) {
            Ok(found) => found,
            Err(error) => {
                self._watching = None;
                return eprintln!("desk: editor does not watch files: {error}");
            }
        };
        self.folder.watched = Some(watched);
        self._watching = Some(cx.spawn(async move |this, cx| {
            while let Ok((seen, changed)) = changes.recv_async().await {
                if this
                    .update(cx, |editor, cx| editor.changed(&changed, seen, cx))
                    .is_err()
                {
                    return;
                }
            }
        }));
    }

    fn rewatch(&self, cx: &App) {
        let Some(watched) = &self.folder.watched else {
            return;
        };
        let listing = &self.folder.listing;
        let folders = std::iter::once(String::new())
            .chain(
                self.folder
                    .tree
                    .read(cx)
                    .open_folders()
                    .into_iter()
                    .map(String::from),
            )
            .filter(|folder| folder.is_empty() || !listing.ignore.ignored(folder))
            .collect();
        let files = self
            .all_pages()
            .filter_map(|page| listing.relative(&page.path))
            .collect();
        if watched.send(Watched { folders, files }).is_err() {
            eprintln!("desk: editor watcher is gone");
        }
    }

    fn edited(&mut self, cx: &mut Context<Self>) {
        let EditedFile(path) = cx.global::<EditedFile>();
        let full = match path.is_absolute() {
            true => path.clone(),
            false => self.folder.listing.root.join(path),
        };
        let Some(relative) = self.folder.relative(&full) else {
            return;
        };
        eprintln!("desk: editor heard tofu edit {relative}");
        self.changed(&BTreeSet::from([relative]), Instant::now(), cx);
    }

    fn changed(&mut self, changed: &BTreeSet<String>, seen: Instant, cx: &mut Context<Self>) {
        let shown: Vec<&str> = changed.iter().map(String::as_str).collect();
        eprintln!("desk: editor saw changes: {}", shown.join(", "));
        let touched: Vec<PathBuf> = self
            .all_pages()
            .filter(|page| {
                self.folder
                    .relative(&page.path)
                    .is_some_and(|relative| changed.contains(&relative))
            })
            .map(|page| page.path.clone())
            .collect();
        for path in touched {
            self.reread(path, seen, cx);
        }
        self.retag(changed, cx);
        self.refresh(cx);
    }

    fn index(&mut self, cx: &mut Context<Self>) {
        self.symbols = None;
        let listing = self.folder.listing.clone();
        let started = Instant::now();
        let built = cx
            .background_executor()
            .spawn(async move { Index::build(&files_under(&listing, indexed)) });
        self._indexing = Some(cx.spawn(async move |this, cx| {
            let index = match built.await {
                Ok(index) => index,
                Err(error) => return eprintln!("desk: symbols not indexed: {error}"),
            };
            eprintln!(
                "desk: symbols indexed {} files, {} symbols in {} ms, index {:.1} MB",
                index.files(),
                index.symbols(),
                started.elapsed().as_millis(),
                index.bytes() as f64 / MB
            );
            if let Err(error) = this.update(cx, |editor, cx| editor.indexed(index, cx)) {
                eprintln!("desk: editor is gone before its symbols: {error}");
            }
        }));
    }

    fn retag(&mut self, changed: &BTreeSet<String>, cx: &mut Context<Self>) {
        let Some(index) = self.symbols.clone() else {
            return;
        };
        let root = &self.folder.listing.root;
        let files: Vec<PathBuf> = changed
            .iter()
            .map(|path| root.join(path))
            .filter(|path| indexed(path))
            .collect();
        if files.is_empty() {
            return;
        }
        let retagged = cx.background_executor().spawn(async move {
            let mut next = (*index).clone();
            next.retag(&files).map(|()| (next, files.len()))
        });
        self._indexing = Some(cx.spawn(async move |this, cx| {
            let (index, count) = match retagged.await {
                Ok(found) => found,
                Err(error) => return eprintln!("desk: symbols kept, retag failed: {error}"),
            };
            eprintln!("desk: symbols retagged {count} files");
            if let Err(error) = this.update(cx, |editor, cx| editor.indexed(index, cx)) {
                eprintln!("desk: editor is gone before its symbols: {error}");
            }
        }));
    }

    fn indexed(&mut self, index: Index, cx: &mut Context<Self>) {
        let index = Arc::new(index);
        for page in self.all_pages() {
            if let Ok(code) = &page.code {
                code.update(cx, |code, _| code.set_symbols(Some(index.clone())));
            }
        }
        self.symbols = Some(index);
    }

    fn jumped(&mut self, jump: Jump, window: &mut Window, cx: &mut Context<Self>) {
        match jump {
            Jump::Definition(asked) => self.definition(asked, window, cx),
            Jump::Travel { way, row, col } => self.travel(way, (row, col), window, cx),
        }
    }

    fn shown(&self, path: &Path, row: usize) -> String {
        let path = self
            .folder
            .relative(path)
            .unwrap_or_else(|| path.display().to_string());
        format!("{path}:{}", row + 1)
    }

    fn definition(&mut self, asked: Asked, window: &mut Window, cx: &mut Context<Self>) {
        let Asked {
            name,
            at,
            row,
            col,
            here,
            since,
        } = asked;
        let Some(page) = self.pages.get(self.active) else {
            return;
        };
        let from = Spot {
            path: page.path.clone(),
            row,
            col,
        };
        let found: Vec<Symbol> = match (&self.symbols, Language::from_path(&page.path)) {
            _ if !here.is_empty() => here,
            (Some(index), Some(language)) => index.definitions(&name, language).cloned().collect(),
            _ => Vec::new(),
        };
        if found.len() > 1 {
            eprintln!("desk: editor definition {name} -> {} choices", found.len());
            self.choosing = Some((from, found));
            self.choices_at = Some(at);
            return cx.notify();
        }
        let Some(symbol) = found.into_iter().next() else {
            return eprintln!("desk: editor definition {name} -> none");
        };
        eprintln!(
            "desk: editor definition {name} -> {}",
            self.shown(&symbol.path, symbol.row)
        );
        self.go_to(from, &symbol, since, window, cx);
    }

    fn choose(&mut self, at: usize, window: &mut Window, cx: &mut Context<Self>) {
        let since = Instant::now();
        let Some((from, symbol)) = self
            .choosing
            .take()
            .and_then(|(from, found)| Some((from, found.into_iter().nth(at)?)))
        else {
            return;
        };
        eprintln!(
            "desk: editor definition {} chose {}",
            symbol.name,
            self.shown(&symbol.path, symbol.row)
        );
        self.go_to(from, &symbol, since, window, cx);
    }

    fn go_to(
        &mut self,
        from: Spot,
        symbol: &Symbol,
        since: Instant,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.back.push(from);
        self.ahead.clear();
        self.land(&symbol.path, symbol.row, symbol.col, window, cx);
        let ms = since.elapsed().as_secs_f64() * 1000.0;
        eprintln!("desk: editor goto: {ms:.2}ms");
    }

    fn travel(
        &mut self,
        way: Way,
        (row, col): (usize, usize),
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let Some(page) = self.pages.get(self.active) else {
            return;
        };
        let here = Spot {
            path: page.path.clone(),
            row,
            col,
        };
        let (word, from, to) = match way {
            Way::Back => ("back", &mut self.back, &mut self.ahead),
            Way::Forward => ("forward", &mut self.ahead, &mut self.back),
        };
        let Some(Spot { path, row, col }) = from.pop() else {
            return eprintln!("desk: editor {word} to nothing");
        };
        to.push(here);
        eprintln!("desk: editor {word} to {}", self.shown(&path, row));
        self.land(&path, row, col, window, cx);
    }

    fn all_pages(&self) -> impl Iterator<Item = &Page> {
        let aside = self.aside.iter().flat_map(|pane| &pane.pages);
        self.pages.iter().chain(aside)
    }

    fn swap_panes(&mut self) {
        let Some(aside) = &mut self.aside else {
            return;
        };
        std::mem::swap(&mut self.pages, &mut aside.pages);
        std::mem::swap(&mut self.active, &mut aside.active);
        std::mem::swap(&mut self.preview, &mut aside.preview);
        self.side = self.side.other();
    }

    fn focus_side(&mut self, side: Side, window: &mut Window, cx: &mut Context<Self>) {
        if side == self.side || self.aside.is_none() {
            return;
        }
        self.swap_panes();
        self.asking = None;
        self.focus_changed(cx);
        self.follow(cx);
        if let Some(Page { code: Ok(code), .. }) = self.pages.get(self.active) {
            window.focus(&code.focus_handle(cx), cx);
        }
        cx.notify();
    }

    fn split(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.aside.is_some() {
            return self.focus_side(self.side.other(), window, cx);
        }
        self.aside = Some(Pane {
            pages: Vec::new(),
            active: 0,
            preview: None,
        });
        self.swap_panes();
        eprintln!("desk: editor split, the right pane takes the next open");
        cx.notify();
    }

    fn unsplit(&mut self, cx: &mut Context<Self>) {
        if self.side == Side::Right {
            self.swap_panes();
        }
        let Some(right) = self.aside.take() else {
            return;
        };
        let edited: Vec<Page> = right
            .pages
            .into_iter()
            .filter(|page| {
                let dirty = page
                    .code
                    .as_ref()
                    .map_or(page.dirty, |code| code.read(cx).buffer().is_dirty());
                dirty && !self.pages.iter().any(|left| left.path == page.path)
            })
            .collect();
        eprintln!(
            "desk: editor split closed, {} edited pages move left",
            edited.len()
        );
        self.pages.extend(edited);
        self.side = Side::Left;
        self.dividing = false;
        self.asking = None;
        self.rewatch(cx);
        self.focus_changed(cx);
        self.follow(cx);
        cx.notify();
    }

    fn divide(&mut self, event: &MouseMoveEvent, cx: &mut Context<Self>) {
        if !self.dividing {
            return;
        }
        if event.pressed_button != Some(MouseButton::Left) {
            self.dividing = false;
            return;
        }
        let span = self.span.get();
        let width = span.size.width.as_f32();
        if width < 2.0 * PANE_LEAST + DIVIDER_HIT {
            return;
        }
        let least = PANE_LEAST / width;
        let share = ((event.position.x - span.origin.x).as_f32() / width).clamp(least, 1.0 - least);
        if share != self.share {
            self.share = share;
            cx.notify();
        }
    }

    fn preview_at(&self) -> Option<usize> {
        let preview = self.preview.as_ref()?;
        self.pages.iter().position(|page| page.path == *preview)
    }

    fn pin(&mut self, at: usize, cx: &mut Context<Self>) {
        let Some(page) = self.pages.get(at) else {
            return;
        };
        if self.preview.as_ref() != Some(&page.path) {
            return;
        }
        eprintln!("desk: editor pinned {}", page.path.display());
        self.preview = None;
        cx.notify();
    }

    pub(crate) fn active_code(&self) -> Option<(&Path, &Entity<CodeEditor>)> {
        let page = self.pages.get(self.active)?;
        page.code
            .as_ref()
            .ok()
            .map(|code| (page.path.as_path(), code))
    }

    pub(crate) fn land(
        &mut self,
        path: &Path,
        row: usize,
        col: usize,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.open_path(path.to_path_buf(), Open::Pinned, window, cx);
        if let Some(Page { code: Ok(code), .. }) = self.pages.get(self.active) {
            code.update(cx, |code, cx| code.jump(row, col, cx));
        }
    }

    pub(crate) fn focused(&self) -> (u32, usize) {
        (self.side.tile(), self.active)
    }

    pub(crate) fn open_tabs(&self, cx: &App) -> Vec<OpenTab> {
        let aside = self
            .aside
            .iter()
            .map(|pane| (self.side.other(), &pane.pages, pane.active));
        let mut tabs: Vec<OpenTab> = std::iter::once((self.side, &self.pages, self.active))
            .chain(aside)
            .flat_map(|(side, pages, active)| {
                pages.iter().enumerate().map(move |(index, page)| OpenTab {
                    tile: side.tile(),
                    index,
                    path: page.path.clone(),
                    active: index == active,
                    dirty: page
                        .code
                        .as_ref()
                        .map_or(page.dirty, |code| code.read(cx).buffer().is_dirty()),
                    stale: page.stale,
                    code: page.code.as_ref().ok().cloned(),
                })
            })
            .collect();
        tabs.sort_by_key(|tab| tab.tile);
        tabs
    }

    pub(crate) fn close_page(&mut self, path: &Path, cx: &mut Context<Self>) -> bool {
        let closed = self.on_pane_of(path, cx, Self::drop_page);
        self.focus_changed(cx);
        self.follow(cx);
        closed
    }

    pub(crate) fn reopen_page(
        &mut self,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Option<PathBuf> {
        let path = self.closed.pop()?;
        self.open_path(path.clone(), Open::Pinned, window, cx);
        Some(path)
    }

    pub(crate) fn reveal_in_tree(&mut self, path: &Path, cx: &mut Context<Self>) -> bool {
        let Some(relative) = self.folder.relative(path) else {
            return false;
        };
        let folders: Vec<SharedString> = relative
            .match_indices('/')
            .filter_map(|(at, _)| relative.get(..at))
            .map(|folder| SharedString::from(folder.to_owned()))
            .collect();
        eprintln!("desk: editor tree reveals /{relative}");
        self.folder.tree.update(cx, |tree, cx| {
            tree.expand(folders, cx);
            tree.select(relative.into(), cx);
        });
        self.refresh(cx);
        true
    }

    pub(crate) fn find_text(
        &self,
        pattern: &str,
        only: Option<&Path>,
        most: usize,
        cx: &App,
    ) -> Vec<Hit> {
        let mut paths = match only {
            Some(path) => vec![path.to_path_buf()],
            None => files_under(&self.folder.listing, |_| true),
        };
        paths.sort();
        let mut hits = Vec::new();
        for path in paths {
            let open = self.all_pages().find(|page| page.path == path);
            let text = match open.map(|page| &page.code) {
                Some(Ok(code)) => code.read(cx).buffer().text(),
                Some(Err(_)) | None => match fs::read_to_string(&path) {
                    Ok(text) => text,
                    Err(_) => continue,
                },
            };
            for (line, row) in text.lines().enumerate() {
                for (at, _) in row.match_indices(pattern) {
                    let col = row.get(..at).map_or(0, |before| before.chars().count());
                    hits.push(Hit {
                        path: path.clone(),
                        line,
                        col,
                        text: row.to_owned(),
                    });
                    if hits.len() >= most {
                        return hits;
                    }
                }
            }
        }
        hits
    }

    fn reread(&mut self, path: PathBuf, seen: Instant, cx: &mut Context<Self>) {
        let read = path.clone();
        let text = cx
            .background_executor()
            .spawn(async move { fs::read_to_string(&read) });
        cx.spawn(async move |this, cx| {
            let text = text.await;
            let done = this.update(cx, |editor, cx| editor.compare(&path, text, seen, cx));
            if let Err(error) = done {
                eprintln!("desk: editor is gone before a reread: {error}");
            }
        })
        .detach();
    }

    fn compare(
        &mut self,
        path: &Path,
        text: std::io::Result<String>,
        seen: Instant,
        cx: &mut Context<Self>,
    ) {
        self.on_pane_of(path, cx, |editor, at, cx| {
            editor.compared(path, at, text, seen, cx);
        });
    }

    fn on_pane_of(
        &mut self,
        path: &Path,
        cx: &mut Context<Self>,
        act: impl FnOnce(&mut Self, usize, &mut Context<Self>),
    ) -> bool {
        let aside_only = !self.pages.iter().any(|page| page.path == path)
            && self
                .aside
                .as_ref()
                .is_some_and(|aside| aside.pages.iter().any(|page| page.path == path));
        if aside_only {
            self.swap_panes();
        }
        let found = self.pages.iter().position(|page| page.path == path);
        if let Some(at) = found {
            act(self, at, cx);
        }
        if aside_only {
            self.swap_panes();
        }
        found.is_some()
    }

    fn compared(
        &mut self,
        path: &Path,
        at: usize,
        text: std::io::Result<String>,
        seen: Instant,
        cx: &mut Context<Self>,
    ) {
        let Some(page) = self.pages.get_mut(at) else {
            return;
        };
        let same = match (&page.code, &text) {
            (Ok(code), Ok(text)) => *code.read(cx).buffer().rope() == text.as_str(),
            _ => false,
        };
        let ours = text
            .as_ref()
            .is_ok_and(|text| Some(content_hash(text)) == page.on_disk);
        if same || ours {
            page.stale = false;
            return cx.notify();
        }
        if page.dirty {
            eprintln!(
                "desk: editor {} changed on disk under edits",
                path.display()
            );
            page.stale = true;
            return cx.notify();
        }
        eprintln!("desk: editor reloads {}", path.display());
        self.reload(at, seen, cx);
    }

    fn reload(&mut self, at: usize, seen: Instant, cx: &mut Context<Self>) {
        let git = self.folder.git.clone();
        let Some(page) = self.pages.get(at) else {
            return;
        };
        let before = page.code.as_ref().ok().cloned();
        let fresh = self.page(page.path.clone(), git, cx);
        if let (Some(before), Ok(code)) = (before, &fresh.code) {
            let path = fresh.path.display().to_string();
            code.update(cx, |code, cx| code.follow(&before, path, seen, cx));
        }
        if let Some(page) = self.pages.get_mut(at) {
            *page = fresh;
        }
        if at == self.active {
            self.focus_changed(cx);
        }
        cx.notify();
    }

    fn refresh(&mut self, cx: &mut Context<Self>) {
        let root = self.folder.listing.root.clone();
        let git = self.folder.git.clone();
        let folders: Vec<String> = self
            .folder
            .tree
            .read(cx)
            .open_folders()
            .into_iter()
            .map(String::from)
            .collect();
        let started = Instant::now();
        let listed = cx.background_executor().spawn(async move {
            let mut listing = Listing::new(root);
            if let Some(git) = git {
                match states(&git, &listing.root) {
                    Ok(found) => listing.states = found,
                    Err(error) => eprintln!("desk: editor shows no git badges: {error}"),
                }
            }
            let top = listing.children("");
            let inside: Vec<(String, Vec<TreeNode>)> = folders
                .into_iter()
                .map(|folder| {
                    let children = listing.children(&folder);
                    (folder, children)
                })
                .collect();
            (listing, top, inside)
        });
        self._refreshing = Some(cx.spawn(async move |this, cx| {
            let (listing, top, inside) = listed.await;
            let done = this.update(cx, |editor, cx| {
                let icons = &editor.folder.icons;
                let count = top.len();
                editor.folder.tree.update(cx, |tree, cx| {
                    tree.reset(top, icons);
                    for (folder, children) in inside {
                        tree.load(&folder, children, icons);
                    }
                    cx.notify();
                });
                eprintln!(
                    "desk: editor listed {} with {count} entries and {} git states in {} ms",
                    listing.root.display(),
                    listing.states.len(),
                    started.elapsed().as_millis()
                );
                editor.folder.listing = Arc::new(listing);
                editor.rewatch(cx);
                editor.follow(cx);
            });
            if let Err(error) = done {
                eprintln!("desk: editor is gone before its listing: {error}");
            }
        }));
    }

    fn repack(&mut self, cx: &mut Context<Self>) {
        let PickedPack(pack) = *cx.global::<PickedPack>();
        if pack == self.folder.pack {
            return;
        }
        let started = Instant::now();
        let icons = match IconTheme::pack(pack) {
            Ok(icons) => icons,
            Err(error) => {
                return eprintln!(
                    "desk: editor keeps {} icons, {} failed: {error}",
                    self.folder.pack.key(),
                    pack.key()
                );
            }
        };
        self.folder
            .tree
            .update(cx, |tree, cx| tree.reicon(&icons, cx));
        eprintln!(
            "desk: editor icons {} -> {} in {} ms",
            self.folder.pack.key(),
            pack.key(),
            started.elapsed().as_millis()
        );
        self.folder.icons = icons;
        self.folder.pack = pack;
        cx.notify();
    }

    fn follow(&self, cx: &mut Context<Self>) {
        let Some(path) = self
            .pages
            .get(self.active)
            .and_then(|page| self.folder.relative(&page.path))
        else {
            return;
        };
        self.folder
            .tree
            .update(cx, |tree, cx| tree.select(path.into(), cx));
    }

    fn tree_event(
        &mut self,
        _: &Entity<FileTree>,
        event: &TreeEvent,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        match event {
            TreeEvent::Unfolded(path) => self.unfold(path.to_string(), cx),
            TreeEvent::Opened(path) => {
                let target = self.folder.listing.root.join(path.as_str());
                let twice = self
                    .peeked
                    .take()
                    .is_some_and(|(peeked, at)| peeked == target && at.elapsed() < DOUBLE_CLICK);
                let how = match twice {
                    true => Open::Pinned,
                    false => {
                        self.peeked = Some((target.clone(), Instant::now()));
                        Open::Preview
                    }
                };
                self.open_path(target, how, window, cx);
            }
            TreeEvent::Delete => {
                let paths = self.folder.tree.read(cx).picked().iter();
                let paths = paths.map(SharedString::to_string).collect();
                self.ask_delete(paths, cx);
            }
            TreeEvent::Menu(spot, at) => {
                eprintln!("desk: editor tree menu on /{}", spot_path(spot));
                self.menu.spot = spot.clone();
                self.menu.open_at = Some(*at);
                self.menu.asked = Some(Instant::now());
                cx.notify();
            }
        }
    }

    fn tree_items(&self) -> Vec<MenuItem> {
        let picked = !matches!(self.menu.spot, TreeSpot::Empty);
        TREE_MENU
            .iter()
            .map(|slot| match slot {
                None => MenuItem::Separator,
                Some(action) => MenuItem::action(action.label())
                    .icon(action.glyph())
                    .danger(*action == TreeAction::Delete)
                    .enabled(match action {
                        TreeAction::Cut
                        | TreeAction::Copy
                        | TreeAction::Duplicate
                        | TreeAction::Delete => picked,
                        TreeAction::Paste => self.menu.clip.is_some(),
                        TreeAction::Undo => !self.menu.done.is_empty(),
                        TreeAction::Redo => !self.menu.undone.is_empty(),
                        TreeAction::NewFile
                        | TreeAction::NewFolder
                        | TreeAction::Reveal
                        | TreeAction::CopyPath
                        | TreeAction::CopyRelative
                        | TreeAction::ExpandAll
                        | TreeAction::CollapseAll => true,
                    }),
            })
            .collect()
    }

    fn full(&self, relative: &str) -> PathBuf {
        relative
            .split('/')
            .filter(|part| !part.is_empty())
            .fold(self.folder.listing.root.clone(), |full, part| {
                full.join(part)
            })
    }

    fn tree_pick(&mut self, at: usize, window: &mut Window, cx: &mut Context<Self>) {
        let Some(Some(action)) = TREE_MENU.get(at) else {
            return eprintln!("desk: editor tree menu has no item {at}");
        };
        let path = spot_path(&self.menu.spot).to_owned();
        let folder = match &self.menu.spot {
            TreeSpot::Folder(folder) => folder.to_string(),
            TreeSpot::File(file) => parent_of(file).to_owned(),
            TreeSpot::Empty => String::new(),
        };
        let picked = self.folder.tree.read(cx).picked();
        let targets: Vec<String> = match path.as_str() {
            "" => Vec::new(),
            spot if picked.iter().any(|picked| picked == spot) => {
                picked.iter().map(SharedString::to_string).collect()
            }
            spot => vec![spot.to_owned()],
        };
        eprintln!(
            "desk: editor tree: {} on /{path}, {} selected",
            action.label(),
            targets.len()
        );
        let done = match action {
            TreeAction::NewFile | TreeAction::NewFolder => {
                let makes_folder = *action == TreeAction::NewFolder;
                self.name_new(folder, makes_folder, window, cx);
                Ok(())
            }
            TreeAction::Reveal => reveal(&self.full(&path)),
            TreeAction::Cut | TreeAction::Copy => {
                let held = match action {
                    TreeAction::Cut => Held::Cut,
                    _ => Held::Copied,
                };
                eprintln!("desk: editor tree: holds {targets:?} as {held:?}");
                self.menu.clip = Some(Clip {
                    held,
                    paths: targets,
                });
                Ok(())
            }
            TreeAction::Duplicate => targets.iter().try_for_each(|path| {
                let from = self.full(path);
                let to = free_name(&self.full(parent_of(path)), name_of(path));
                self.run(FileOp::Copied { from, to })
            }),
            TreeAction::Delete => {
                self.ask_delete(targets, cx);
                Ok(())
            }
            TreeAction::Paste => self.paste(&folder),
            TreeAction::Undo => self.step_back(),
            TreeAction::Redo => self.step_forward(),
            TreeAction::CopyPath | TreeAction::CopyRelative => {
                let copied = targets
                    .iter()
                    .map(|path| match action {
                        TreeAction::CopyPath => self.full(path).display().to_string(),
                        _ => path.clone(),
                    })
                    .collect::<Vec<_>>()
                    .join("\n");
                eprintln!("desk: editor tree: clipboard holds {copied}");
                cx.write_to_clipboard(ClipboardItem::new_string(copied));
                Ok(())
            }
            TreeAction::ExpandAll => {
                self.expand_all(folder, cx);
                Ok(())
            }
            TreeAction::CollapseAll => {
                self.folder
                    .tree
                    .update(cx, |tree, cx| tree.collapse_all(&folder, cx));
                eprintln!("desk: editor tree: collapsed /{folder}");
                Ok(())
            }
        };
        if let Err(error) = done {
            eprintln!("desk: editor tree: {} failed: {error}", action.label());
        }
        self.refresh(cx);
        cx.notify();
    }

    fn run(&mut self, op: FileOp) -> std::io::Result<()> {
        op.apply()?;
        eprintln!("desk: editor tree: did {}", op.said());
        self.menu.done.push(op);
        self.menu.undone.clear();
        Ok(())
    }

    fn step_back(&mut self) -> std::io::Result<()> {
        let Some(op) = self.menu.done.pop() else {
            return Ok(());
        };
        if let Err(error) = op.revert() {
            self.menu.done.push(op);
            return Err(error);
        }
        eprintln!("desk: editor tree: undid {}", op.said());
        self.menu.undone.push(op);
        Ok(())
    }

    fn step_forward(&mut self) -> std::io::Result<()> {
        let Some(op) = self.menu.undone.pop() else {
            return Ok(());
        };
        if let Err(error) = op.apply() {
            self.menu.undone.push(op);
            return Err(error);
        }
        eprintln!("desk: editor tree: redid {}", op.said());
        self.menu.done.push(op);
        Ok(())
    }

    fn paste(&mut self, folder: &str) -> std::io::Result<()> {
        let Some(clip) = self.menu.clip.clone() else {
            return Ok(());
        };
        let into = self.full(folder);
        for path in &clip.paths {
            let from = self.full(path);
            let to = free_name(&into, name_of(path));
            match clip.held {
                Held::Copied => self.run(FileOp::Copied { from, to })?,
                Held::Cut if from.parent() == Some(into.as_path()) => {}
                Held::Cut => self.run(FileOp::Moved { from, to })?,
            }
        }
        if clip.held == Held::Cut {
            self.menu.clip = None;
        }
        Ok(())
    }

    fn name_new(
        &mut self,
        folder: String,
        makes_folder: bool,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let input = TextInput::new(
            match makes_folder {
                true => "Folder name",
                false => "File name",
            }
            .into(),
            window,
            cx,
        );
        let focus = input.focus_handle(cx);
        window.focus(&focus, cx);
        let blurred = cx.on_blur(&focus, window, |editor, _, cx| {
            editor.drop_naming("left", cx);
        });
        let draft = Draft {
            folder: folder.clone().into(),
            makes_folder,
            input: input.clone(),
            error: None,
        };
        let icons = &self.folder.icons;
        self.folder
            .tree
            .update(cx, |tree, cx| tree.draft(draft, icons, cx));
        self.menu.naming = Some(Naming {
            folder,
            makes_folder,
            input,
            _blurred: blurred,
        });
    }

    fn drop_naming(&mut self, why: &str, cx: &mut Context<Self>) {
        let Some(naming) = self.menu.naming.take() else {
            return;
        };
        eprintln!("desk: editor tree: new entry in /{} {why}", naming.folder);
        self.folder.tree.update(cx, |tree, cx| tree.end_draft(cx));
        cx.notify();
    }

    fn name_key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let Some(naming) = &self.menu.naming else {
            return;
        };
        match event.keystroke.key.as_str() {
            "enter" => {}
            "escape" => {
                cx.stop_propagation();
                return self.drop_naming("cancelled", cx);
            }
            _ => return,
        }
        cx.stop_propagation();
        let name = naming.input.read(cx).text().trim().to_owned();
        let path = self.full(&naming.folder).join(&name);
        let single = Path::new(&name).components().count() == 1
            && Path::new(&name)
                .components()
                .all(|part| matches!(part, Component::Normal(_)));
        let refused = match (single, path.exists()) {
            (false, _) => Some(format!("{name:?} is not a name")),
            (true, true) => Some(format!("{name} already exists")),
            (true, false) => None,
        };
        if let Some(refused) = refused {
            eprintln!("desk: editor tree: new entry refused: {refused}");
            return self
                .folder
                .tree
                .update(cx, |tree, cx| tree.draft_error(Some(refused.into()), cx));
        }
        let makes_folder = naming.makes_folder;
        self.menu.naming = None;
        self.folder.tree.update(cx, |tree, cx| tree.end_draft(cx));
        let made = self.run(FileOp::Made {
            path: path.clone(),
            folder: makes_folder,
        });
        match made {
            Ok(()) if !makes_folder => self.open_path(path, Open::Pinned, window, cx),
            Ok(()) => {}
            Err(error) => eprintln!("desk: editor tree: new {name} failed: {error}"),
        }
        self.refresh(cx);
        cx.notify();
    }

    fn ask_delete(&mut self, paths: Vec<String>, cx: &mut Context<Self>) {
        self.alerted = match paths.as_slice() {
            [] => return,
            [one] => format!("Delete {}?", name_of(one)),
            many => format!("Delete {} items?", many.len()),
        }
        .into();
        eprintln!("desk: editor tree: asks before deleting {paths:?}");
        self.asking = Some(Asking::Delete { paths });
        cx.notify();
    }

    fn recycle(&mut self, cx: &mut Context<Self>) {
        let Some(Asking::Delete { paths }) = self.asking.take() else {
            return;
        };
        cx.notify();
        let full: Vec<PathBuf> = paths.iter().map(|path| self.full(path)).collect();
        let started = Instant::now();
        let gone = |page: &Page| full.iter().any(|path| page.path.starts_with(path));
        for _ in 0..2 {
            while let Some(at) = self.pages.iter().position(gone) {
                self.drop_page(at, cx);
            }
            self.swap_panes();
        }
        let shown: Vec<SharedString> = paths.iter().cloned().map(SharedString::from).collect();
        self.folder
            .tree
            .update(cx, |tree, cx| tree.forget(&shown, cx));
        eprintln!(
            "desk: editor deleted {} entries in {} ms",
            full.len(),
            started.elapsed().as_millis()
        );
        let moved = cx
            .background_executor()
            .spawn(async move { send_to_recycle_bin(&full).map(|()| full.len()) });
        cx.spawn(async move |this, cx| {
            let moved = moved.await;
            let done = this.update(cx, |editor, cx| {
                match moved {
                    Ok(count) => eprintln!(
                        "desk: editor tree: recycled {count} items in {} ms: {paths:?}",
                        started.elapsed().as_millis()
                    ),
                    Err(error) => eprintln!("desk: editor tree: delete failed: {error}"),
                }
                editor.refresh(cx);
            });
            if let Err(error) = done {
                eprintln!("desk: editor is gone before its delete finished: {error}");
            }
        })
        .detach();
    }

    fn expand_all(&mut self, folder: String, cx: &mut Context<Self>) {
        let listing = self.folder.listing.clone();
        let found = cx
            .background_executor()
            .spawn(async move { listing.folders_under(&folder) });
        self._unfolding.retain(|task| !task.is_ready());
        self._unfolding.push(cx.spawn(async move |this, cx| {
            let folders = found.await;
            let done = this.update(cx, |editor, cx| {
                eprintln!(
                    "desk: editor tree: expand all opens {} folders",
                    folders.len()
                );
                editor.folder.tree.update(cx, |tree, cx| {
                    tree.expand(folders.into_iter().map(SharedString::from), cx)
                });
                editor.refresh(cx);
            });
            if let Err(error) = done {
                eprintln!("desk: editor is gone before expand all: {error}");
            }
        }));
    }

    fn unfold(&mut self, path: String, cx: &mut Context<Self>) {
        let listing = self.folder.listing.clone();
        let started = Instant::now();
        let listed = cx.background_executor().spawn(async move {
            let children = listing.children(&path);
            (path, children)
        });
        self._unfolding.retain(|task| !task.is_ready());
        self._unfolding.push(cx.spawn(async move |this, cx| {
            let (path, children) = listed.await;
            let done = this.update(cx, |editor, cx| {
                let count = children.len();
                let icons = &editor.folder.icons;
                let loaded = editor.folder.tree.update(cx, |tree, cx| {
                    cx.notify();
                    tree.load(&path, children, icons)
                });
                match loaded {
                    true => eprintln!(
                        "desk: editor listed /{path}: {count} entries in {} ms",
                        started.elapsed().as_millis()
                    ),
                    false => eprintln!("desk: editor tree has no folder {path}"),
                }
                editor.rewatch(cx);
            });
            if let Err(error) = done {
                eprintln!("desk: editor is gone before its folder listed: {error}");
            }
        }));
    }

    fn page(&self, full: PathBuf, git: Option<Arc<GitBinary>>, cx: &mut Context<Self>) -> Page {
        let code = file_editor(&full, self, cx);
        let changes = code
            .as_ref()
            .ok()
            .map(|code| cx.observe(code, Self::code_changed));
        let on_disk = code
            .as_ref()
            .ok()
            .map(|code| content_hash(&code.read(cx).buffer().text()));
        Page {
            name: full
                .file_name()
                .map(|name| name.to_string_lossy().into_owned().into())
                .unwrap_or_default(),
            repo: git.and_then(|git| repo(&full, git)),
            path: full,
            code,
            dirty: false,
            stale: false,
            on_disk,
            _changes: changes,
        }
    }

    fn open_path(&mut self, full: PathBuf, how: Open, window: &mut Window, cx: &mut Context<Self>) {
        let started = Instant::now();
        match self.pages.iter().position(|page| page.path == full) {
            Some(at) => self.active = at,
            None => {
                let page = self.page(full.clone(), self.folder.git.clone(), cx);
                open_phase("page", started);
                let slot = match how {
                    Open::Preview => self.preview_at(),
                    Open::Pinned => None,
                };
                match slot.and_then(|at| Some((at, self.pages.get_mut(at)?))) {
                    Some((at, old)) => {
                        *old = page;
                        self.active = at;
                    }
                    None => {
                        self.pages.push(page);
                        self.active = self.pages.len().saturating_sub(1);
                    }
                }
                if how == Open::Preview {
                    self.preview = Some(full.clone());
                }
                self.rewatch(cx);
            }
        }
        if how == Open::Pinned && self.preview.as_ref() == Some(&full) {
            self.preview = None;
        }
        let how = match self.preview.as_ref() == Some(&full) {
            true => Open::Preview,
            false => Open::Pinned,
        };
        self.opening = Some(Opening {
            path: full,
            how,
            started,
        });
        let blamed = Instant::now();
        self.focus_changed(cx);
        open_phase("marks and blame", blamed);
        let followed = Instant::now();
        self.follow(cx);
        open_phase("tree follow", followed);
        if let Some(Page { code: Ok(code), .. }) = self.pages.get(self.active) {
            window.focus(&code.focus_handle(cx), cx);
        }
        cx.notify();
    }

    fn select_page(&mut self, at: usize, window: &mut Window, cx: &mut Context<Self>) {
        self.active = at;
        self.asking = None;
        self.focus_changed(cx);
        self.follow(cx);
        if let Some(Page { code: Ok(code), .. }) = self.pages.get(at) {
            window.focus(&code.focus_handle(cx), cx);
        }
        cx.notify();
    }

    pub(crate) fn step_page(&mut self, forward: bool, window: &mut Window, cx: &mut Context<Self>) {
        let count = self.pages.len();
        let step = if forward { 1 } else { count.saturating_sub(1) };
        if let Some(at) = (self.active + step).checked_rem(count) {
            self.select_page(at, window, cx);
        }
    }

    fn focus_changed(&mut self, cx: &mut Context<Self>) {
        self.remark(cx);
        if let Some(Page { code: Ok(code), .. }) = self.pages.get(self.active) {
            let code = code.clone();
            self.follow_blame(&code, cx);
        }
    }

    fn close(&mut self, at: usize, cx: &mut Context<Self>) {
        let Some(page) = self.pages.get(at) else {
            return;
        };
        if page.dirty {
            eprintln!("desk: editor asks before closing {}", page.path.display());
            self.alerted = format!("Save changes to {}?", page.name).into();
            self.asking = Some(Asking::Close {
                path: page.path.clone(),
                failed: None,
            });
            return cx.notify();
        }
        self.drop_page(at, cx);
    }

    fn keep_open(&mut self, cx: &mut Context<Self>) {
        match self.asking.take() {
            Some(Asking::Close { path, .. }) => eprintln!("desk: editor kept {}", path.display()),
            Some(Asking::Delete { paths }) => eprintln!("desk: editor tree: kept {paths:?}"),
            None => {}
        }
        cx.notify();
    }

    fn close_unsaved(&mut self, cx: &mut Context<Self>) {
        let Some(Asking::Close { path, .. }) = &self.asking else {
            return;
        };
        if let Some(at) = self.pages.iter().position(|page| page.path == *path) {
            eprintln!("desk: editor closes {} without saving", path.display());
            self.drop_page(at, cx);
        }
    }

    fn save_and_close(&mut self, cx: &mut Context<Self>) {
        let Some(Asking::Close { path, failed }) = &mut self.asking else {
            return;
        };
        let Some((at, Page { code: Ok(code), .. })) = self
            .pages
            .iter()
            .enumerate()
            .find(|(_, page)| page.path == *path)
        else {
            return;
        };
        match fs::write(&*path, code.read(cx).buffer().text()) {
            Ok(()) => {
                eprintln!("desk: editor saved {} before closing", path.display());
                self.drop_page(at, cx);
            }
            Err(error) => {
                eprintln!("desk: editor could not save {}: {error}", path.display());
                *failed = Some(format!("Could not save: {error}"));
                cx.notify();
            }
        }
    }

    fn alerts(&self, cx: &mut Context<Self>) -> [AnyElement; 2] {
        let theme = ActiveTheme::theme(cx);
        let (closing, deleting, failed) = match &self.asking {
            Some(Asking::Close { failed, .. }) => (true, None, failed.clone()),
            Some(Asking::Delete { paths }) => (false, Some(paths.len()), None),
            None => (false, None, None),
        };
        let recycled = match deleting {
            Some(1) => "It goes to the Recycle Bin.",
            _ => "They go to the Recycle Bin.",
        };
        let delete = Sheet::new("editor-delete", self.alerted.clone())
            .modal()
            .open(deleting.is_some())
            .confirm("Delete")
            .on_confirm(answered(cx, Self::recycle))
            .on_cancel(answered(cx, Self::keep_open))
            .child(div().truncate().child(recycled));
        let unsaved = Sheet::new("editor-unsaved", self.alerted.clone())
            .modal()
            .open(closing)
            .confirm("Save")
            .on_confirm(answered(cx, Self::save_and_close))
            .aside("Close without saving", answered(cx, Self::close_unsaved))
            .on_cancel(answered(cx, Self::keep_open))
            .child(div().truncate().child(UNSAVED_BODY))
            .children(failed.map(|failed| {
                div()
                    .truncate()
                    .text_color(theme.color(ColorToken::StatusDanger))
                    .child(failed)
            }));
        [unsaved.into_any_element(), delete.into_any_element()]
    }

    fn drop_page(&mut self, at: usize, cx: &mut Context<Self>) {
        if at >= self.pages.len() {
            return;
        }
        let page = self.pages.remove(at);
        eprintln!("desk: editor closed {}", page.path.display());
        if self.preview.as_ref() == Some(&page.path) {
            self.preview = None;
        }
        self.closed.push(page.path);
        let excess = self.closed.len().saturating_sub(CLOSED_PAGES);
        self.closed.drain(..excess);
        if at < self.active || self.active >= self.pages.len() {
            self.active = self.active.saturating_sub(1);
        }
        if self.pages.is_empty() && self.aside.is_some() {
            self.swap_panes();
            self.aside = None;
            self.side = Side::Left;
            eprintln!("desk: editor pane closed, one pane again");
        }
        self.asking = None;
        self.rewatch(cx);
        self.focus_changed(cx);
        self.follow(cx);
        cx.notify();
    }

    fn code_changed(&mut self, code: Entity<CodeEditor>, cx: &mut Context<Self>) {
        let finding = code
            .read(cx)
            .finding()
            .map(|(query, at, total)| (query.to_owned(), at, total));
        if finding != self.find_logged {
            match &finding {
                Some((query, at, total)) => eprintln!("desk: find editor {query} {at} of {total}"),
                None => eprintln!("desk: find editor cleared"),
            }
            self.find_logged = finding;
        }
        let dirty = code.read(cx).buffer().is_dirty();
        let Some(at) = self
            .pages
            .iter()
            .position(|page| page.code.as_ref().is_ok_and(|own| *own == code))
        else {
            return;
        };
        if at == self.active {
            self.follow_blame(&code, cx);
        }
        let Some(page) = self.pages.get_mut(at) else {
            return;
        };
        if page.dirty == dirty {
            return;
        }
        page.dirty = dirty;
        if dirty && self.preview.as_ref() == Some(&page.path) {
            eprintln!("desk: editor pinned {} by an edit", page.path.display());
            self.preview = None;
        }
        eprintln!(
            "desk: editor {} {}",
            page.name,
            if dirty { "changed" } else { "saved" }
        );
        if !dirty {
            page.stale = false;
            page.on_disk = Some(content_hash(&code.read(cx).buffer().text()));
            if let Some(repo) = &mut page.repo {
                repo.absent_from_head = false;
                repo.wanted = None;
            }
            if at == self.active {
                self.remark(cx);
                self.follow_blame(&code, cx);
            }
            self.refresh(cx);
        }
        cx.notify();
    }

    fn remark(&mut self, cx: &mut Context<Self>) {
        let Some(Page {
            code: Ok(code),
            repo: Some(repo),
            ..
        }) = self.pages.get(self.active)
        else {
            self._marking = None;
            return;
        };
        let (code, git, path) = (code.downgrade(), repo.git.clone(), repo.path.clone());
        let diff = cx
            .background_executor()
            .spawn(async move { git.diff(&path, Against::Index).map(|hunks| (path, hunks)) });
        self._marking = Some(cx.spawn(async move |_, cx| {
            let (path, hunks) = match diff.await {
                Ok(found) => found,
                Err(error) => {
                    return eprintln!("desk: editor could not read git marks: {error}");
                }
            };
            let marks = git_marks(&hunks);
            let shown = described(&marks);
            let applied = code.update(cx, |code, cx| {
                let clean = !code.buffer().is_dirty();
                if clean {
                    code.set_marks(marks);
                    cx.notify();
                }
                clean
            });
            match applied {
                Ok(true) => eprintln!(
                    "desk: editor git marks for {path}, {} hunks: {shown}",
                    hunks.len()
                ),
                Ok(false) => eprintln!("desk: editor dropped git marks for {path}, edited since"),
                Err(error) => eprintln!("desk: editor is gone before its git marks: {error}"),
            }
        }));
    }

    fn follow_blame(&mut self, code: &Entity<CodeEditor>, cx: &mut Context<Self>) {
        let Some(Page {
            repo: Some(repo), ..
        }) = self.pages.get_mut(self.active)
        else {
            return;
        };
        let editor = code.read(cx);
        let rope = editor.buffer().rope().clone();
        let row = editor
            .carets()
            .last()
            .and_then(|(_, head)| editor.buffer().char_to_line(head).ok());
        let shown = editor
            .marks()
            .iter()
            .find_map(|(row, mark)| mark.trailing.as_ref().map(|_| *row));
        if !repo.absent_from_head && repo.wanted.as_deref().is_none_or(|wanted| rope != wanted) {
            let text = rope.to_string();
            let wait = match repo.wanted {
                Some(_) => BLAME_IDLE,
                None => Duration::ZERO,
            };
            repo.wanted = Some(text.clone());
            let (git, path) = (repo.git.clone(), repo.path.clone());
            self._blaming = Some(cx.spawn(async move |this, cx| {
                cx.background_executor().timer(wait).await;
                let started = Instant::now();
                let blamed = cx
                    .background_executor()
                    .spawn(async move {
                        let lines = git.blame(&path, Some(&text))?;
                        let own_email = git.own_email()?.map(str::to_owned);
                        Ok::<_, GitError>(Blamed {
                            text,
                            lines,
                            own_email,
                        })
                    })
                    .await;
                let applied = this.update(cx, |editor, cx| editor.blamed(blamed, started, cx));
                if let Err(error) = applied {
                    eprintln!("desk: editor is gone before its blame: {error}");
                }
            }));
        }
        let user = match &self.github {
            Github::SignedIn(user) => Some(user),
            Github::Asking | Github::SignedOut => None,
        };
        let wanted = row
            .zip(
                repo.blamed
                    .as_ref()
                    .filter(|blamed| rope == blamed.text.as_str()),
            )
            .and_then(|(row, blamed)| {
                let me = Me {
                    email: blamed.own_email.as_deref(),
                    login: user.map(|user| user.login.as_ref()),
                };
                blame::inline(blamed, row, &me)
            });
        if wanted == repo.inline && shown == wanted.as_ref().map(|inline| inline.row) {
            return;
        }
        repo.inline.clone_from(&wanted);
        let picture = wanted
            .as_ref()
            .filter(|inline| inline.mine)
            .and(user)
            .and_then(|user| user.picture.clone());
        if let Some(inline) = &wanted {
            eprintln!(
                "desk: editor blame {} line {}: shows {}, user {}, name {}, email <{}>, {}, {}",
                repo.path,
                inline.row + 1,
                inline.name(),
                inline.user.as_deref().unwrap_or("-"),
                inline.author,
                inline.email,
                inline.when,
                match &picture {
                    Some(_) => "your github picture",
                    None => "no icon",
                }
            );
        }
        let reduced = reduced_motion(cx);
        code.update(cx, |code, cx| {
            let mut marks = code.marks().clone();
            marks.retain(|_, mark| {
                mark.trailing = None;
                mark.gutter.is_some() || mark.edge.is_some() || mark.background.is_some()
            });
            if let Some(inline) = wanted {
                let (row, name, when) = (inline.row, inline.name(), inline.when);
                marks.entry(row).or_default().trailing = Some(Rc::new(move |theme: &Theme| {
                    inline_blame(
                        ("inline-blame", row),
                        &name,
                        &when,
                        picture.clone(),
                        reduced,
                        theme,
                    )
                }));
            }
            code.set_marks(marks);
            cx.notify();
        });
    }

    fn blamed(
        &mut self,
        blamed: Result<Blamed, GitError>,
        started: Instant,
        cx: &mut Context<Self>,
    ) {
        let Some(Page {
            code: Ok(code),
            repo: Some(repo),
            ..
        }) = self.pages.get_mut(self.active)
        else {
            return;
        };
        match blamed {
            Ok(blamed) => {
                eprintln!(
                    "desk: editor blamed {}: {} lines in {} ms",
                    repo.path,
                    blamed.lines.len(),
                    started.elapsed().as_millis()
                );
                repo.blamed = Some(blamed);
            }
            Err(error) => {
                repo.absent_from_head = matches!(&error, GitError::Failed { stderr, .. } if stderr.contains("no such path") && stderr.contains("in HEAD"));
                eprintln!("desk: editor shows no blame for {}: {error}", repo.path);
            }
        }
        let code = code.clone();
        self.follow_blame(&code, cx);
        cx.notify();
    }

    fn keep_mine(&mut self, cx: &mut Context<Self>) {
        if let Some(page) = self.pages.get_mut(self.active) {
            page.stale = false;
        }
        cx.notify();
    }

    fn path_header(&self, theme: &Theme) -> AnyElement {
        div()
            .flex()
            .flex_none()
            .items_center()
            .h(px(PATH_HEIGHT))
            .px_3()
            .text_size(px(12.0))
            .text_color(theme.color(ColorToken::TextMuted))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .overflow_hidden()
                    .whitespace_nowrap()
                    .text_ellipsis_start()
                    .child(self.folder.listing.root.display().to_string()),
            )
            .into_any_element()
    }

    fn pane_button(
        &self,
        (id, label, tip): (&'static str, &'static str, &'static str),
        face: AnyElement,
        act: fn(&mut Self, &mut Window, &mut Context<Self>),
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let theme = ActiveTheme::theme(cx);
        let target = control(id, label, &theme)
            .size(px(SPLIT_BUTTON))
            .rounded(px(SPLIT_GLYPH_RADIUS))
            .child(face)
            .on_click(cx.listener(move |editor, _: &ClickEvent, window, cx| {
                eprintln!("desk: editor {id} button");
                act(editor, window, cx);
            }));
        div()
            .flex()
            .flex_none()
            .items_center()
            .px_1()
            .child(tooltip(
                format!("{id}-tip"),
                target,
                Edge::Frame,
                tip,
                &theme,
                window,
                cx,
            ))
            .into_any_element()
    }

    fn split_button(&self, window: &mut Window, cx: &mut Context<Self>) -> AnyElement {
        let ink = ActiveTheme::theme(cx).color(ColorToken::TextIcon);
        let glyph = div()
            .flex()
            .justify_center()
            .w(px(SPLIT_GLYPH_W))
            .h(px(SPLIT_GLYPH_H))
            .border_1()
            .border_color(ink)
            .rounded(px(SPLIT_GLYPH_RADIUS))
            .child(div().w(px(1.0)).h_full().bg(ink));
        self.pane_button(
            ("editor-split", "Split right", "Split right (Ctrl+\\)"),
            glyph.into_any_element(),
            Self::split,
            window,
            cx,
        )
    }

    fn unsplit_button(&self, window: &mut Window, cx: &mut Context<Self>) -> AnyElement {
        let ink = ActiveTheme::theme(cx).color(ColorToken::TextIcon);
        self.pane_button(
            ("editor-unsplit", "Close split", "Close split"),
            icon(Icon::Close, CLOSE_ICON, ink).into_any_element(),
            |editor, _, cx| editor.unsplit(cx),
            window,
            cx,
        )
    }

    fn tabs(
        &self,
        pane: Shown,
        side: Side,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let (pages, active, preview) = pane;
        let theme = ActiveTheme::theme(cx);
        let tabs: Vec<Tab> = pages
            .iter()
            .map(|page| Tab {
                label: page.name.clone(),
                icon: Some(Glyph::File),
                count: None,
                mark: match page.dirty {
                    true => TabMark::Dirty,
                    false => TabMark::Close,
                },
                flag: None,
            })
            .collect();
        let images = pages
            .iter()
            .map(|page| Some(self.folder.icons.file_image(&page.name)))
            .collect();
        let preview_at =
            preview.and_then(|preview| pages.iter().position(|page| page.path == *preview));
        let (editor, pinner) = (cx.weak_entity(), cx.weak_entity());
        let button = match (self.aside.is_some(), side) {
            (false, _) => Some(self.split_button(window, cx)),
            (true, Side::Right) => Some(self.unsplit_button(window, cx)),
            (true, Side::Left) => None,
        };
        div()
            .flex()
            .flex_none()
            .items_center()
            .min_w_0()
            .child(
                div().flex_1().min_w_0().child(
                    connected_tabs(
                        side.tabs_id(),
                        &tabs,
                        active,
                        tabs.len().max(1),
                        &theme,
                        move |event, window, cx| {
                            let done = editor.update(cx, |editor, cx| {
                                editor.focus_side(side, window, cx);
                                match event {
                                    TabEvent::Select(at) => editor.select_page(*at, window, cx),
                                    TabEvent::Close(at) => editor.close(*at, cx),
                                    TabEvent::New => {}
                                }
                            });
                            if let Err(error) = done {
                                eprintln!("desk: editor is gone before its tab event: {error}");
                            }
                        },
                    )
                    .images(images)
                    .preview(preview_at, move |at, window, cx| {
                        let done = pinner.update(cx, |editor, cx| {
                            editor.focus_side(side, window, cx);
                            editor.pin(at, cx);
                        });
                        if let Err(error) = done {
                            eprintln!("desk: editor is gone before its tab pinned: {error}");
                        }
                    }),
                ),
            )
            .children(button)
            .into_any_element()
    }

    fn bar(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let theme = ActiveTheme::theme(cx);
        let line = |text: String| {
            div()
                .flex()
                .flex_none()
                .items_center()
                .gap(px(BAR_GAP))
                .px_3()
                .py_1p5()
                .bg(theme.color(ColorToken::TabsFill))
                .text_size(px(12.5))
                .text_color(theme.color(ColorToken::TextBase))
                .child(div().flex_1().min_w_0().truncate().child(text))
        };
        let page = self.pages.get(self.active).filter(|page| page.stale)?;
        Some(
            line(format!("{} changed on disk.", page.name))
                .child(
                    button("editor-reload", "Reload", None, ButtonKind::Text, &theme).on_click(
                        cx.listener(|editor, _, _, cx| {
                            let at = editor.active;
                            editor.reload(at, Instant::now(), cx);
                        }),
                    ),
                )
                .child(
                    button(
                        "editor-keep-mine",
                        "Keep mine",
                        None,
                        ButtonKind::Plain,
                        &theme,
                    )
                    .on_click(cx.listener(|editor, _, _, cx| editor.keep_mine(cx))),
                )
                .into_any_element(),
        )
    }

    fn choices_menu(&mut self, cx: &mut Context<Self>) -> impl IntoElement {
        let items = self
            .choosing
            .iter()
            .flat_map(|(_, found)| found)
            .map(|symbol| MenuItem::action(self.shown(&symbol.path, symbol.row)))
            .collect();
        context_menu(items)
            .id("editor-definitions")
            .absolute()
            .size_0()
            .open_at(self.choices_at.take())
            .on_pick(cx.listener(|editor, at: &usize, window, cx| editor.choose(*at, window, cx)))
    }

    fn divided(
        &self,
        panes: Vec<AnyElement>,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Vec<AnyElement> {
        let [left, right] = match <[AnyElement; 2]>::try_from(panes) {
            Ok(pair) => pair,
            Err(one) => return one,
        };
        let line = theme.color(ColorToken::Separator);
        let lit = theme.color(ColorToken::FocusRing);
        let divider = div()
            .id("editor-divider")
            .group("editor-divider")
            .flex()
            .flex_none()
            .justify_center()
            .w(px(DIVIDER_HIT))
            .h_full()
            .cursor(CursorStyle::ResizeLeftRight)
            .child(
                div()
                    .w(px(1.0))
                    .h_full()
                    .bg(line)
                    .group_hover("editor-divider", move |mark| mark.bg(lit)),
            )
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(|editor, _: &MouseDownEvent, _, cx| {
                    cx.stop_propagation();
                    editor.dividing = true;
                }),
            );
        let wrap = |pane: AnyElement| div().flex().min_w(px(PANE_LEAST)).min_h_0().child(pane);
        vec![
            wrap(left)
                .flex_none()
                .w(relative(self.share))
                .into_any_element(),
            divider.into_any_element(),
            wrap(right).flex_1().into_any_element(),
        ]
    }

    fn pane(
        &self,
        pane: Shown,
        side: Side,
        opening: Option<Opening>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let theme = ActiveTheme::theme(cx);
        let (pages, active, _) = pane;
        let painted = opening.map(|Opening { path, how, started }| {
            canvas(
                |_, _, _| {},
                move |_, (), _, _| {
                    eprintln!(
                        "desk: editor opened {} {} in {} ms",
                        path.display(),
                        how.word(),
                        started.elapsed().as_millis()
                    )
                },
            )
            .absolute()
            .size_0()
        });
        div()
            .flex()
            .flex_col()
            .flex_1()
            .min_w_0()
            .min_h_0()
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(move |editor, _, window, cx| editor.focus_side(side, window, cx)),
            )
            .when(!pages.is_empty() || side == Side::Right, |main| {
                main.child(self.tabs(pane, side, window, cx))
            })
            .when(side == self.side, |main| main.children(self.bar(cx)))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .flex_1()
                    .min_h_0()
                    .overflow_hidden()
                    .child(code_area(pages.get(active), &theme))
                    .children(painted),
            )
            .into_any_element()
    }
}

fn code_area(page: Option<&Page>, theme: &Theme) -> AnyElement {
    match page.map(|page| &page.code) {
        None => empty_state(
            "editor-shut",
            "No file open",
            Some("Click a file in the tree to open it.".into()),
            &[],
            &[],
            theme,
            |_, _, _| {},
        )
        .into_any_element(),
        Some(Ok(code)) => {
            let finder = code.clone();
            div()
                .size_full()
                .on_action(move |_: &Find, window, cx| {
                    eprintln!("desk: find editor open");
                    finder.update(cx, |editor, cx| editor.find(window, cx));
                })
                .child(code.clone())
                .into_any_element()
        }
        Some(Err(error)) => empty_state(
            "editor-failure",
            "Could not open the file",
            Some(error.clone()),
            &[],
            &[],
            theme,
            |_, _, _| {},
        )
        .into_any_element(),
    }
}

impl Render for Editor {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let asked = self.menu.asked.take();
        let painted = canvas(
            |_, _, _| {},
            move |_, (), _, _| {
                if let Some(asked) = asked {
                    eprintln!(
                        "desk: editor tree menu painted {:.2} ms after the right click",
                        asked.elapsed().as_secs_f32() * 1000.0
                    );
                }
            },
        )
        .absolute()
        .size_0();
        let tree = context_menu(self.tree_items())
            .id("editor-tree-menu")
            .flex()
            .flex_col()
            .flex_1()
            .min_h_0()
            .open_at(self.menu.open_at.take())
            .on_pick(
                cx.listener(|editor, at: &usize, window, cx| editor.tree_pick(*at, window, cx)),
            )
            .child(painted)
            .child(self.folder.tree.clone());
        let side = div()
            .flex()
            .flex_col()
            .flex_none()
            .w(relative(SIDE_SHARE))
            .min_w(px(SIDE_LEAST))
            .max_w(px(SIDE_MOST))
            .min_h_0()
            .border_r_1()
            .border_color(theme.color(ColorToken::Separator))
            .child(self.path_header(&theme))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .flex_1()
                    .min_h_0()
                    .px_1()
                    .py_1()
                    .on_key_down(cx.listener(Self::name_key))
                    .child(tree),
            );
        let opening = self.opening.take().filter(|opening| {
            self.pages
                .get(self.active)
                .is_some_and(|page| page.path == opening.path)
        });
        let focused = (self.pages.as_slice(), self.active, self.preview.as_ref());
        let focused = self.pane(focused, self.side, opening, window, cx);
        let aside = self.aside.as_ref().map(|aside| {
            let shown = (aside.pages.as_slice(), aside.active, aside.preview.as_ref());
            self.pane(shown, self.side.other(), None, window, cx)
        });
        let panes = match (self.side, aside) {
            (_, None) => vec![focused],
            (Side::Left, Some(aside)) => vec![focused, aside],
            (Side::Right, Some(aside)) => vec![aside, focused],
        };
        let span = self.span.clone();
        let main = div()
            .flex()
            .flex_row()
            .flex_1()
            .min_w_0()
            .min_h_0()
            .relative()
            .on_mouse_move(
                cx.listener(|editor, event: &MouseMoveEvent, _, cx| editor.divide(event, cx)),
            )
            .on_mouse_up(
                MouseButton::Left,
                cx.listener(|editor, _, _, _| editor.dividing = false),
            )
            .child(
                canvas(move |bounds, _, _| span.set(bounds), |_, (), _, _| {})
                    .absolute()
                    .size_full(),
            )
            .children(self.divided(panes, &theme, cx))
            .child(self.choices_menu(cx));
        let asking = self.asking.is_some();
        shell(
            Header::Title(Some(Glyph::Code), "Editor".into(), None),
            &theme,
        )
        .size_full()
        .relative()
        .capture_key_down(cx.listener(move |editor, event: &KeyDownEvent, _, cx| {
            if asking && event.keystroke.key == "escape" {
                cx.stop_propagation();
                editor.keep_open(cx);
            }
        }))
        .capture_key_down(cx.listener(|editor, event: &KeyDownEvent, window, cx| {
            if event.keystroke.modifiers.secondary() && event.keystroke.key == SPLIT_KEY {
                cx.stop_propagation();
                editor.split(window, cx);
            }
        }))
        .on_action(cx.listener(|editor, _: &host::CloseTab, _, cx| editor.close(editor.active, cx)))
        .on_action(
            cx.listener(|editor, _: &host::NextTab, window, cx| editor.step_page(true, window, cx)),
        )
        .on_action(
            cx.listener(|editor, _: &host::PrevTab, window, cx| {
                editor.step_page(false, window, cx)
            }),
        )
        .on_action(|_: &host::GoToLine, _, _| no_surface("go_to_line"))
        .on_action(|_: &host::QuickOpen, _, _| no_surface("quick_open"))
        .on_action(|_: &host::FindInFiles, _, _| no_surface("find_in_files"))
        .child(
            inner_card(&theme)
                .flex_1()
                .min_h_0()
                .flex_row()
                .child(side)
                .child(main),
        )
        .children(self.alerts(cx))
    }
}

fn answered(
    cx: &mut Context<Editor>,
    act: fn(&mut Editor, &mut Context<Editor>),
) -> impl Fn(&mut Window, &mut App) + 'static {
    let editor = cx.weak_entity();
    move |_, cx| {
        if let Err(error) = editor.update(cx, act) {
            eprintln!("desk: editor is gone before its alert answered: {error}");
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum TreeAction {
    NewFile,
    NewFolder,
    Reveal,
    Cut,
    Copy,
    Duplicate,
    Paste,
    Undo,
    Redo,
    CopyPath,
    CopyRelative,
    ExpandAll,
    CollapseAll,
    Delete,
}

const TREE_MENU: [Option<TreeAction>; 19] = [
    Some(TreeAction::NewFile),
    Some(TreeAction::NewFolder),
    None,
    Some(TreeAction::Reveal),
    None,
    Some(TreeAction::Cut),
    Some(TreeAction::Copy),
    Some(TreeAction::Duplicate),
    Some(TreeAction::Paste),
    Some(TreeAction::Undo),
    Some(TreeAction::Redo),
    None,
    Some(TreeAction::CopyPath),
    Some(TreeAction::CopyRelative),
    None,
    Some(TreeAction::ExpandAll),
    Some(TreeAction::CollapseAll),
    None,
    Some(TreeAction::Delete),
];

impl TreeAction {
    fn label(self) -> &'static str {
        match self {
            TreeAction::NewFile => "New File",
            TreeAction::NewFolder => "New Folder",
            TreeAction::Reveal => "Reveal in File Explorer",
            TreeAction::Cut => "Cut",
            TreeAction::Copy => "Copy",
            TreeAction::Duplicate => "Duplicate",
            TreeAction::Paste => "Paste",
            TreeAction::Undo => "Undo",
            TreeAction::Redo => "Redo",
            TreeAction::CopyPath => "Copy Path",
            TreeAction::CopyRelative => "Copy Relative Path",
            TreeAction::ExpandAll => "Expand All",
            TreeAction::CollapseAll => "Collapse All",
            TreeAction::Delete => "Delete",
        }
    }

    fn glyph(self) -> Glyph {
        match self {
            TreeAction::NewFile => Glyph::FileAdd,
            TreeAction::NewFolder => Glyph::FolderAdd,
            TreeAction::Reveal => Glyph::Reveal,
            TreeAction::Cut => Glyph::Cut,
            TreeAction::Copy => Glyph::Copy,
            TreeAction::Duplicate => Glyph::Duplicate,
            TreeAction::Paste => Glyph::Paste,
            TreeAction::Undo => Glyph::Undo,
            TreeAction::Redo => Glyph::Redo,
            TreeAction::CopyPath => Glyph::Link,
            TreeAction::CopyRelative => Glyph::Relative,
            TreeAction::ExpandAll => Glyph::ExpandAll,
            TreeAction::CollapseAll => Glyph::CollapseAll,
            TreeAction::Delete => Glyph::Trash,
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
enum Held {
    Cut,
    Copied,
}

#[derive(Clone)]
struct Clip {
    held: Held,
    paths: Vec<String>,
}

struct Naming {
    folder: String,
    makes_folder: bool,
    input: Entity<TextInput>,
    _blurred: Subscription,
}

enum FileOp {
    Made { path: PathBuf, folder: bool },
    Copied { from: PathBuf, to: PathBuf },
    Moved { from: PathBuf, to: PathBuf },
}

impl FileOp {
    fn apply(&self) -> std::io::Result<()> {
        match self {
            FileOp::Made { path, folder: true } => fs::create_dir(path),
            FileOp::Made {
                path,
                folder: false,
            } => fs::File::create_new(path).map(drop),
            FileOp::Copied { from, to } if to.starts_with(from) => Err(std::io::Error::other(
                format!("{} is inside {}", to.display(), from.display()),
            )),
            FileOp::Copied { from, to } => copy_tree(from, to),
            FileOp::Moved { from, to } => fs::rename(from, to),
        }
    }

    fn revert(&self) -> std::io::Result<()> {
        match self {
            FileOp::Made { path, .. } | FileOp::Copied { to: path, .. } if path.is_dir() => {
                fs::remove_dir_all(path)
            }
            FileOp::Made { path, .. } | FileOp::Copied { to: path, .. } => fs::remove_file(path),
            FileOp::Moved { from, to } => fs::rename(to, from),
        }
    }

    fn said(&self) -> String {
        match self {
            FileOp::Made { path, folder } => {
                let kind = if *folder { "folder" } else { "file" };
                format!("new {kind} {}", path.display())
            }
            FileOp::Copied { from, to } => format!("copy {} -> {}", from.display(), to.display()),
            FileOp::Moved { from, to } => format!("move {} -> {}", from.display(), to.display()),
        }
    }
}

struct TreeMenu {
    spot: TreeSpot,
    open_at: Option<Point<Pixels>>,
    asked: Option<Instant>,
    clip: Option<Clip>,
    done: Vec<FileOp>,
    undone: Vec<FileOp>,
    naming: Option<Naming>,
}

fn spot_path(spot: &TreeSpot) -> &str {
    match spot {
        TreeSpot::Empty => "",
        TreeSpot::File(path) | TreeSpot::Folder(path) => path,
    }
}

fn parent_of(path: &str) -> &str {
    path.rsplit_once('/').map_or("", |(parent, _)| parent)
}

fn name_of(path: &str) -> &str {
    path.rsplit_once('/').map_or(path, |(_, name)| name)
}

fn free_name(dir: &Path, name: &str) -> PathBuf {
    let (stem, ext) = match name.rsplit_once('.') {
        Some((stem, ext)) if !stem.is_empty() => (stem, format!(".{ext}")),
        _ => (name, String::new()),
    };
    let mut candidate = dir.join(name);
    let mut copies = 0_u32;
    while candidate.exists() {
        copies = copies.saturating_add(1);
        let tail = match copies {
            1 => " copy".to_owned(),
            count => format!(" copy {count}"),
        };
        candidate = dir.join(format!("{stem}{tail}{ext}"));
    }
    candidate
}

fn copy_tree(from: &Path, to: &Path) -> std::io::Result<()> {
    let mut pending = vec![(from.to_path_buf(), to.to_path_buf())];
    while let Some((from, to)) = pending.pop() {
        if !from.is_dir() {
            fs::copy(&from, &to)?;
            continue;
        }
        fs::create_dir(&to)?;
        for entry in fs::read_dir(&from)? {
            let entry = entry?;
            pending.push((entry.path(), to.join(entry.file_name())));
        }
    }
    Ok(())
}

fn reveal(path: &Path) -> std::io::Result<()> {
    let mut command = std::process::Command::new(REVEALER);
    #[cfg(windows)]
    std::os::windows::process::CommandExt::raw_arg(
        &mut command,
        format!("/select,\"{}\"", path.display()),
    );
    #[cfg(not(windows))]
    command.arg(path.parent().unwrap_or(path));
    let child = command.spawn()?;
    eprintln!(
        "desk: editor tree: revealed {} with {REVEALER}, pid {}",
        path.display(),
        child.id()
    );
    Ok(())
}

fn send_to_recycle_bin(paths: &[PathBuf]) -> std::io::Result<()> {
    let listed = paths
        .iter()
        .map(|path| path.display().to_string())
        .collect::<Vec<_>>()
        .join("\n");
    let mut command = std::process::Command::new("powershell");
    command
        .args(["-NoProfile", "-NonInteractive", "-Command", RECYCLE_SCRIPT])
        .env(RECYCLE_VAR, listed);
    #[cfg(windows)]
    std::os::windows::process::CommandExt::creation_flags(&mut command, CREATE_NO_WINDOW);
    let output = command.output()?;
    match output.status.success() {
        true => Ok(()),
        false => Err(std::io::Error::other(
            String::from_utf8_lossy(&output.stderr).trim().to_owned(),
        )),
    }
}

const RECYCLE_VAR: &str = "DESK_RECYCLE";
const RECYCLE_SCRIPT: &str = "$ErrorActionPreference = 'Stop'; Add-Type -AssemblyName Microsoft.VisualBasic; foreach ($p in $env:DESK_RECYCLE -split \"`n\") { if (Test-Path -LiteralPath $p -PathType Container) { [Microsoft.VisualBasic.FileIO.FileSystem]::DeleteDirectory($p, 'OnlyErrorDialogs', 'SendToRecycleBin') } else { [Microsoft.VisualBasic.FileIO.FileSystem]::DeleteFile($p, 'OnlyErrorDialogs', 'SendToRecycleBin') } }";
#[cfg(windows)]
const CREATE_NO_WINDOW: u32 = 0x0800_0000;

#[cfg(windows)]
const REVEALER: &str = "explorer";
#[cfg(target_os = "macos")]
const REVEALER: &str = "open";
#[cfg(not(any(windows, target_os = "macos")))]
const REVEALER: &str = "xdg-open";

fn absolute(path: &Path) -> PathBuf {
    std::path::absolute(path).unwrap_or_else(|_| path.to_path_buf())
}

fn repository(dir: &Path) -> Option<Arc<GitBinary>> {
    match GitBinary::open(dir) {
        Ok(git) => Some(Arc::new(git)),
        Err(error) => {
            eprintln!("desk: editor shows no git marks: {error}");
            None
        }
    }
}

fn repo(full: &Path, git: Arc<GitBinary>) -> Option<Repo> {
    let inside = full.strip_prefix(git.root()).ok()?;
    let parts: Vec<String> = inside
        .iter()
        .map(|part| part.to_string_lossy().into_owned())
        .collect();
    Some(Repo {
        git,
        path: parts.join("/"),
        wanted: None,
        blamed: None,
        inline: None,
        absent_from_head: false,
    })
}

fn git_marks(hunks: &[Hunk]) -> Marks {
    hunks
        .iter()
        .flat_map(|hunk| {
            let (start, count) = (hunk.new.start as usize, hunk.new.count as usize);
            let (lines, gutter) = match (hunk.old.count, count) {
                (_, 0) => (
                    start.max(1)..start.max(1) + 1,
                    GutterMark::Removed(ColorToken::GitDeleted),
                ),
                (0, _) => (
                    start..start + count,
                    GutterMark::Changed(ColorToken::GitAdded),
                ),
                _ => (
                    start..start + count,
                    GutterMark::Changed(ColorToken::GitModified),
                ),
            };
            lines.map(move |line| {
                let mark = LineMarks {
                    gutter: Some(gutter),
                    ..LineMarks::default()
                };
                (line.saturating_sub(1), mark)
            })
        })
        .collect()
}

fn described(marks: &Marks) -> String {
    let lines: Vec<String> = marks
        .iter()
        .map(|(row, mark)| match mark.gutter {
            Some(GutterMark::Changed(ColorToken::GitAdded)) => format!("{} added", row + 1),
            Some(GutterMark::Removed(_)) => format!("{} removed below", row + 1),
            _ => format!("{} modified", row + 1),
        })
        .collect();
    lines.join(", ")
}

fn no_surface(action: &str) {
    eprintln!("desk: editor action {action} no surface yet");
}

pub(crate) fn language(code: &CodeEditor) -> String {
    match code.syntax() {
        Highlight::Tree(syntax) => syntax
            .report()
            .split(' ')
            .next()
            .unwrap_or("plain")
            .to_owned(),
        Highlight::Plain => "plain".to_owned(),
    }
}

fn files_under(listing: &Listing, wanted: impl Fn(&Path) -> bool) -> Vec<PathBuf> {
    let mut found = Vec::new();
    let mut pending = vec![String::new()];
    while let Some(folder) = pending.pop() {
        let dir = listing.root.join(&folder);
        if dir.join(CACHE_TAG).is_file() {
            continue;
        }
        let read = match fs::read_dir(&dir) {
            Ok(read) => read,
            Err(error) => {
                eprintln!("desk: editor could not walk {}: {error}", dir.display());
                continue;
            }
        };
        for entry in read.flatten() {
            let name = entry.file_name().to_string_lossy().into_owned();
            let relative = match folder.as_str() {
                "" => name.clone(),
                folder => format!("{folder}/{name}"),
            };
            if name == GIT_DIR || listing.ignore.ignored(&relative) {
                continue;
            }
            match entry.file_type() {
                Ok(kind) if kind.is_dir() => pending.push(relative),
                Ok(kind) if kind.is_file() && wanted(&entry.path()) => found.push(entry.path()),
                Ok(_) => {}
                Err(error) => eprintln!("desk: editor walk skipped {relative}: {error}"),
            }
        }
    }
    found
}

fn file_editor(path: &Path, owner: &Editor, cx: &mut Context<Editor>) -> Opened {
    let shown = path.display();
    let editor = match CodeEditor::open(path, cx) {
        Ok(editor) => editor,
        Err(error) => {
            eprintln!("desk: editor could not open {shown}: {error}");
            return Err(error.into());
        }
    };
    eprintln!(
        "desk: editor opened {shown} {} {} lines",
        language(&editor),
        editor.buffer().line_count()
    );
    let path = PathBuf::from(path);
    let desk = cx.weak_entity();
    let on_jump = move |jump: Jump, window: &mut Window, cx: &mut App| {
        if let Err(error) = desk.update(cx, |desk, cx| desk.jumped(jump, window, cx)) {
            eprintln!("desk: editor is gone before its jump: {error}");
        }
    };
    let mut editor = editor
        .keymap(owner.keymap.clone())
        .vim(owner.vim)
        .guides(owner.guides)
        .on_jump(on_jump);
    editor.set_symbols(owner.symbols.clone());
    let editor = editor.on_save(move |buffer, _, _| {
        let shown = path.display();
        match buffer.save(&path) {
            Ok(()) => {
                eprintln!("desk: editor saved {shown} {} lines", buffer.line_count());
                Ok(())
            }
            Err(error) => {
                eprintln!("desk: editor could not save {shown}: {error}");
                Err(format!("{shown}: {error}"))
            }
        }
    });
    Ok(cx.new(|cx| {
        let mut editor = editor;
        editor.colour_rest(cx);
        editor
    }))
}

fn switched_on(file: &Path, key: &str) -> bool {
    let shown = file.display();
    let read = match fs::read_to_string(file) {
        Ok(text) => serde_json::from_str::<serde_json::Map<String, serde_json::Value>>(&text)
            .map(|settings| settings.get(key).cloned())
            .map_err(|error| error.to_string()),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(error) => Err(error.to_string()),
    };
    let on = match read {
        Ok(None) => false,
        Ok(Some(serde_json::Value::Bool(on))) => on,
        Ok(Some(other)) => {
            eprintln!("desk: settings {shown} refused {key} {other}, not true or false");
            false
        }
        Err(error) => {
            eprintln!("desk: settings {shown} could not be read, {key} stays off: {error}");
            false
        }
    };
    eprintln!("desk: {key} {}", if on { "on" } else { "off" });
    on
}

fn content_hash(text: &str) -> u64 {
    let mut hasher = std::hash::DefaultHasher::new();
    std::hash::Hash::hash(text, &mut hasher);
    std::hash::Hasher::finish(&hasher)
}
