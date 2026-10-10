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
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{Header, inner_card, shell};
use desk_ui::components::code::{GutterMark, LineMarks, Marks};
use desk_ui::components::code_editor::{CodeEditor, Highlight};
use desk_ui::components::empty::empty_state;
use desk_ui::components::form::TextInput;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::history::inline_blame;
use desk_ui::components::overlay::{MenuItem, context_menu};
use desk_ui::components::sheet::Sheet;
use desk_ui::components::tabs::{Tab, TabEvent, TabMark, connected_tabs};
use desk_ui::components::title_bar::Github;
use desk_ui::components::tree::{
    EditedFile, FileTree, IconPack, IconTheme, OpenProject, PickedPack, TreeEvent, TreeNode,
    TreeSpot, picked_pack,
};
use desk_ui::live::ActiveTheme;
use desk_ui::motion::reduced_motion;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, AnyView, App, AppContext, ClipboardItem, Context, Entity, Focusable, KeyDownEvent,
    Pixels, Point, Render, SharedString, Subscription, Task, Window, canvas, div, prelude::*, px,
    relative,
};

use crate::modules::chat::Find;
use crate::title_bar::{ask_github, github};
use blame::{Blamed, Inline, Me};
use listing::{Listing, states};
use watch::Watched;

const BLAME_IDLE: Duration = Duration::from_millis(500);
const SIDE_SHARE: f32 = 0.26;
const SIDE_LEAST: f32 = 180.0;
const SIDE_MOST: f32 = 320.0;
const PATH_HEIGHT: f32 = 30.0;
const BAR_GAP: f32 = 8.0;
const NAMING_TEXT: f32 = 12.0;
const UNSAVED_BODY: &str = "Your changes are lost if you close without saving.";

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
                editor.open_path(file, window, cx);
            }
            editor
        })
        .into())
}

type Opened = Result<Entity<CodeEditor>, SharedString>;

struct Repo {
    git: Arc<GitBinary>,
    path: String,
    wanted: Option<String>,
    blamed: Option<Blamed>,
    inline: Option<Inline>,
}

struct Page {
    path: PathBuf,
    name: SharedString,
    code: Opened,
    repo: Option<Repo>,
    dirty: bool,
    stale: bool,
    _changes: Option<Subscription>,
}

enum Asking {
    Close {
        path: PathBuf,
        failed: Option<String>,
    },
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
        let tree = FileTree::new("editor-files", Vec::new(), &icons).badges();
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
    asking: Option<Asking>,
    find_logged: Option<(String, usize, usize)>,
    github: Github,
    alerted: SharedString,
    menu: TreeMenu,
    _tree: Subscription,
    _pack: Subscription,
    _project: Subscription,
    _edited: Subscription,
    _watching: Option<Task<()>>,
    _refreshing: Option<Task<()>>,
    _unfolding: Vec<Task<()>>,
    _marking: Option<Task<()>>,
    _blaming: Option<Task<()>>,
}

impl Editor {
    fn new(folder: Folder, window: &mut Window, cx: &mut Context<Self>) -> Self {
        let mut editor = Editor {
            _tree: cx.subscribe_in(&folder.tree, window, Self::tree_event),
            folder,
            pages: Vec::new(),
            active: 0,
            asking: None,
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
            _pack: cx.observe_global::<PickedPack>(Self::repack),
            _project: cx.observe_global_in::<OpenProject>(window, Self::reroot),
            _edited: cx.observe_global::<EditedFile>(Self::edited),
            _watching: None,
            _refreshing: None,
            _unfolding: Vec::new(),
            _marking: None,
            _blaming: None,
        };
        editor.start_watching(cx);
        editor.refresh(cx);
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
        self.pages.retain(|page| page.dirty);
        self.active = 0;
        self.asking = None;
        self.start_watching(cx);
        self.refresh(cx);
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
            while let Ok(changed) = changes.recv_async().await {
                if this
                    .update(cx, |editor, cx| editor.changed(&changed, cx))
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
            .pages
            .iter()
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
        self.changed(&BTreeSet::from([relative]), cx);
    }

    fn changed(&mut self, changed: &BTreeSet<String>, cx: &mut Context<Self>) {
        let shown: Vec<&str> = changed.iter().map(String::as_str).collect();
        eprintln!("desk: editor saw changes: {}", shown.join(", "));
        let touched: Vec<PathBuf> = self
            .pages
            .iter()
            .filter(|page| {
                self.folder
                    .relative(&page.path)
                    .is_some_and(|relative| changed.contains(&relative))
            })
            .map(|page| page.path.clone())
            .collect();
        for path in touched {
            self.reread(path, cx);
        }
        self.refresh(cx);
    }

    fn reread(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        let read = path.clone();
        let text = cx
            .background_executor()
            .spawn(async move { fs::read_to_string(&read) });
        cx.spawn(async move |this, cx| {
            let text = text.await;
            let done = this.update(cx, |editor, cx| editor.compare(&path, text, cx));
            if let Err(error) = done {
                eprintln!("desk: editor is gone before a reread: {error}");
            }
        })
        .detach();
    }

    fn compare(&mut self, path: &Path, text: std::io::Result<String>, cx: &mut Context<Self>) {
        let Some(at) = self.pages.iter().position(|page| page.path == path) else {
            return;
        };
        let Some(page) = self.pages.get_mut(at) else {
            return;
        };
        let same = match (&page.code, &text) {
            (Ok(code), Ok(text)) => *code.read(cx).buffer().rope() == text.as_str(),
            _ => false,
        };
        if same {
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
        self.reload(at, cx);
    }

    fn reload(&mut self, at: usize, cx: &mut Context<Self>) {
        let git = self.folder.git.clone();
        let Some(page) = self.pages.get(at) else {
            return;
        };
        let fresh = self.page(page.path.clone(), git, cx);
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
                self.open_path(target, window, cx);
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
                    .enabled(match action {
                        TreeAction::Cut | TreeAction::Copy | TreeAction::Duplicate => picked,
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
        eprintln!("desk: editor tree: {} on /{path}", action.label());
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
                eprintln!("desk: editor tree: holds /{path} as {held:?}");
                self.menu.clip = Some(Clip { held, path });
                Ok(())
            }
            TreeAction::Duplicate => {
                let from = self.full(&path);
                let to = free_name(&self.full(parent_of(&path)), name_of(&path));
                self.run(FileOp::Copied { from, to })
            }
            TreeAction::Paste => self.paste(&folder),
            TreeAction::Undo => self.step_back(),
            TreeAction::Redo => self.step_forward(),
            TreeAction::CopyPath | TreeAction::CopyRelative => {
                let copied = match action {
                    TreeAction::CopyPath => self.full(&path).display().to_string(),
                    _ => path,
                };
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
        let from = self.full(&clip.path);
        let into = self.full(folder);
        if clip.held == Held::Cut && from.parent() == Some(into.as_path()) {
            self.menu.clip = None;
            return Ok(());
        }
        let to = free_name(&into, name_of(&clip.path));
        match clip.held {
            Held::Copied => self.run(FileOp::Copied { from, to }),
            Held::Cut => {
                self.run(FileOp::Moved { from, to })?;
                self.menu.clip = None;
                Ok(())
            }
        }
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
        window.focus(&input.focus_handle(cx), cx);
        if !folder.is_empty() {
            let opened = SharedString::from(folder.clone());
            self.folder
                .tree
                .update(cx, |tree, cx| tree.expand([opened], cx));
        }
        self.menu.naming = Some(Naming {
            folder,
            makes_folder,
            input,
        });
    }

    fn name_key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let entered = match event.keystroke.key.as_str() {
            "enter" => true,
            "escape" => false,
            _ => return,
        };
        cx.stop_propagation();
        let Some(naming) = self.menu.naming.take() else {
            return;
        };
        cx.notify();
        let name = naming.input.read(cx).text().trim().to_owned();
        let single = Path::new(&name).components().count() == 1
            && Path::new(&name)
                .components()
                .all(|part| matches!(part, Component::Normal(_)));
        if !entered || !single {
            return eprintln!(
                "desk: editor tree: new entry {name:?} in /{} dropped",
                naming.folder
            );
        }
        let path = self.full(&naming.folder).join(&name);
        let made = self.run(FileOp::Made {
            path: path.clone(),
            folder: naming.makes_folder,
        });
        match made {
            Ok(()) if !naming.makes_folder => self.open_path(path, window, cx),
            Ok(()) => {}
            Err(error) => eprintln!("desk: editor tree: new {name} failed: {error}"),
        }
        self.refresh(cx);
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
        let code = file_editor(&full, cx);
        let changes = code
            .as_ref()
            .ok()
            .map(|code| cx.observe(code, Self::code_changed));
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
            _changes: changes,
        }
    }

    fn open_path(&mut self, full: PathBuf, window: &mut Window, cx: &mut Context<Self>) {
        match self.pages.iter().position(|page| page.path == full) {
            Some(at) => self.active = at,
            None => {
                let page = self.page(full, self.folder.git.clone(), cx);
                self.pages.push(page);
                self.active = self.pages.len().saturating_sub(1);
                self.rewatch(cx);
            }
        }
        self.focus_changed(cx);
        self.follow(cx);
        if let Some(Page { code: Ok(code), .. }) = self.pages.get(self.active) {
            window.focus(&code.focus_handle(cx), cx);
        }
        cx.notify();
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
        if let Some(Asking::Close { path, .. }) = self.asking.take() {
            eprintln!("desk: editor kept {}", path.display());
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

    fn alert(&self, cx: &mut Context<Self>) -> AnyElement {
        let theme = ActiveTheme::theme(cx);
        let failed = match &self.asking {
            Some(Asking::Close { failed, .. }) => failed.clone(),
            None => None,
        };
        Sheet::new("editor-unsaved", self.alerted.clone())
            .modal()
            .open(self.asking.is_some())
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
            }))
            .into_any_element()
    }

    fn drop_page(&mut self, at: usize, cx: &mut Context<Self>) {
        if at >= self.pages.len() {
            return;
        }
        let page = self.pages.remove(at);
        eprintln!("desk: editor closed {}", page.path.display());
        if at < self.active || self.active >= self.pages.len() {
            self.active = self.active.saturating_sub(1);
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
        eprintln!(
            "desk: editor {} {}",
            page.name,
            if dirty { "changed" } else { "saved" }
        );
        if !dirty {
            page.stale = false;
            if at == self.active {
                self.remark(cx);
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
        if repo.wanted.as_deref().is_none_or(|wanted| rope != wanted) {
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
            Err(error) => eprintln!("desk: editor shows no blame for {}: {error}", repo.path),
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

    fn tabs(&self, cx: &mut Context<Self>) -> AnyElement {
        let theme = ActiveTheme::theme(cx);
        let tabs: Vec<Tab> = self
            .pages
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
        let images = self
            .pages
            .iter()
            .map(|page| Some(self.folder.icons.file_image(&page.name)))
            .collect();
        let editor = cx.weak_entity();
        div()
            .flex_none()
            .min_w_0()
            .child(
                connected_tabs(
                    "editor-file-tabs",
                    &tabs,
                    self.active,
                    tabs.len().max(1),
                    &theme,
                    move |event, window, cx| {
                        let done = editor.update(cx, |editor, cx| match event {
                            TabEvent::Select(at) => {
                                editor.active = *at;
                                editor.asking = None;
                                editor.focus_changed(cx);
                                editor.follow(cx);
                                if let Some(Page { code: Ok(code), .. }) = editor.pages.get(*at) {
                                    window.focus(&code.focus_handle(cx), cx);
                                }
                                cx.notify();
                            }
                            TabEvent::Close(at) => editor.close(*at, cx),
                            TabEvent::New => {}
                        });
                        if let Err(error) = done {
                            eprintln!("desk: editor is gone before its tab event: {error}");
                        }
                    },
                )
                .images(images),
            )
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
                            editor.reload(at, cx);
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

    fn code_area(&self, theme: &Theme) -> AnyElement {
        match self.pages.get(self.active).map(|page| &page.code) {
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
}

impl Render for Editor {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
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
        let naming = self.menu.naming.as_ref().map(|naming| {
            let place = match naming.folder.as_str() {
                "" => "the root".to_owned(),
                folder => format!("/{folder}"),
            };
            let kind = if naming.makes_folder {
                "folder"
            } else {
                "file"
            };
            div()
                .flex()
                .flex_col()
                .gap_1()
                .px_2()
                .py_1()
                .text_size(px(NAMING_TEXT))
                .text_color(theme.color(ColorToken::TextBase))
                .on_key_down(cx.listener(Self::name_key))
                .child(format!("New {kind} in {place}"))
                .child(naming.input.clone())
        });
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
                    .children(naming)
                    .child(tree),
            );
        let main = div()
            .flex()
            .flex_col()
            .flex_1()
            .min_w_0()
            .min_h_0()
            .when(!self.pages.is_empty(), |main| main.child(self.tabs(cx)))
            .children(self.bar(cx))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .flex_1()
                    .min_h_0()
                    .overflow_hidden()
                    .child(self.code_area(&theme)),
            );
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
        .child(
            inner_card(&theme)
                .flex_1()
                .min_h_0()
                .flex_row()
                .child(side)
                .child(main),
        )
        .child(self.alert(cx))
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
}

const TREE_MENU: [Option<TreeAction>; 17] = [
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
    path: String,
}

struct Naming {
    folder: String,
    makes_folder: bool,
    input: Entity<TextInput>,
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

fn file_editor(path: &Path, cx: &mut App) -> Opened {
    let shown = path.display();
    let editor = match CodeEditor::open(path, cx) {
        Ok(editor) => editor,
        Err(error) => {
            eprintln!("desk: editor could not open {shown}: {error}");
            return Err(error.into());
        }
    };
    let language = match editor.syntax() {
        Highlight::Tree(syntax) => syntax.report(),
        Highlight::Plain => "plain".to_owned(),
    };
    let language = language.split(' ').next().unwrap_or("plain");
    eprintln!(
        "desk: editor opened {shown} {language} {} lines",
        editor.buffer().line_count()
    );
    let path = PathBuf::from(path);
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
    Ok(cx.new(|_| editor))
}
