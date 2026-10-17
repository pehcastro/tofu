use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::sync::atomic::AtomicBool;
use std::time::Instant;

use desk_core::git::{
    Branch, Commit, CommitDetail, DiffOf, Entry, GitBinary, Operation, PullHow, Side, Stash,
    UndoStep,
};
use desk_core::limits::TOAST_LIFETIME;
use desk_ui::components::overlay::MenuButton;
use gpui::{Context, Entity, Task, UniformListScrollHandle, Window};

use super::history::History;
use super::sidebar::Sidebar;
use super::watch::Touched;

pub const LEFT_WIDTH: f32 = 240.0;
pub const RIGHT_WIDTH: f32 = 380.0;

#[derive(Clone)]
pub enum Panel {
    Changes,
    Commit(String),
    Diff {
        path: String,
        of: DiffOf,
        back: Box<Panel>,
    },
}

#[derive(Default)]
pub struct Snapshot {
    pub branch: Option<Branch>,
    pub branches: Vec<Branch>,
    pub remotes: Vec<(String, String)>,
    pub tags: Vec<String>,
    pub stashes: Vec<Stash>,
    pub changes: Vec<Entry>,
    pub operation: Option<Operation>,
}

#[derive(Clone)]
pub enum Action {
    Stage(Vec<String>),
    Unstage(Vec<String>),
    Discard(Vec<String>),
    StagePatch(String),
    DiscardPatch(String),
    Commit {
        message: String,
        amend: bool,
        push: bool,
    },
    Switch(String),
    CreateBranch {
        name: String,
        at: String,
    },
    Fetch,
    Pull(PullHow),
    Push {
        lease: bool,
    },
    StashPush(String),
    StashPop(u32),
    StashDrop(u32),
    Restore {
        sha: String,
        keep: bool,
    },
    Take {
        path: String,
        side: Side,
    },
    Resolve(String),
    Continue,
    Abort,
}

#[derive(Clone)]
pub enum Told {
    Done(String),
    Failed { said: String, fix: Option<Action> },
}

pub enum Loaded<T> {
    Waiting,
    Ready(T),
    Failed(String),
}

#[derive(Clone, Copy)]
pub(super) enum Edge {
    Left,
    Right,
}

#[derive(Clone, Copy)]
pub(super) struct Drag {
    pub(super) edge: Edge,
    pub(super) from_x: f32,
    pub(super) from_width: f32,
}

pub struct GitScreen {
    pub(super) root: PathBuf,
    pub(super) git: Loaded<Arc<GitBinary>>,
    pub(super) snapshot: Snapshot,
    pub(super) read: bool,
    pub(super) problem: Option<String>,
    pub(super) history: History,
    pub(super) panel: Panel,
    pub(super) detail: Option<(String, Loaded<CommitDetail>)>,
    pub(super) diff: Option<(String, Loaded<String>)>,
    pub(super) undo: Loaded<Vec<UndoStep>>,
    pub(super) outgoing: Loaded<Vec<Commit>>,
    pub(super) told: Option<Told>,
    pub(super) running: bool,
    pub(super) reloading: bool,
    pub(super) wanted: Option<Touched>,
    pub(super) paused: Arc<AtomicBool>,
    pub(super) sidebar: Sidebar,
    pub(super) switcher: Entity<MenuButton>,
    pub(super) left: f32,
    pub(super) right: f32,
    pub(super) drag: Option<Drag>,
    pub(super) list: UniformListScrollHandle,
    pub(super) opened: Instant,
    pub(super) _watching: Option<Task<()>>,
    pub(super) _toast: Option<Task<()>>,
}

impl GitScreen {
    pub fn new(root: PathBuf, window: &mut Window, cx: &mut Context<Self>) -> Self {
        let mut screen = GitScreen {
            root,
            git: Loaded::Waiting,
            snapshot: Snapshot::default(),
            read: false,
            problem: None,
            history: History::default(),
            panel: Panel::Changes,
            detail: None,
            diff: None,
            undo: Loaded::Waiting,
            outgoing: Loaded::Waiting,
            told: None,
            running: false,
            reloading: false,
            wanted: None,
            paused: Arc::new(AtomicBool::new(false)),
            sidebar: Sidebar::new(window, cx),
            switcher: MenuButton::new("Switch branch".into(), Vec::new(), cx),
            left: LEFT_WIDTH,
            right: RIGHT_WIDTH,
            drag: None,
            list: UniformListScrollHandle::new(),
            opened: Instant::now(),
            _watching: None,
            _toast: None,
        };
        screen.open_repo(cx);
        screen
    }

    pub fn snapshot(&self) -> &Snapshot {
        &self.snapshot
    }

    pub fn root(&self) -> &Path {
        &self.root
    }

    pub fn panel(&self) -> &Panel {
        &self.panel
    }

    pub fn detail(&self) -> Option<&Loaded<CommitDetail>> {
        match (&self.panel, &self.detail) {
            (Panel::Commit(sha), Some((loaded, detail))) if sha == loaded => Some(detail),
            _ => None,
        }
    }

    pub fn diff(&self) -> Option<&Loaded<String>> {
        match (&self.panel, &self.diff) {
            (Panel::Diff { path, .. }, Some((loaded, diff))) if path == loaded => Some(diff),
            _ => None,
        }
    }

    pub fn undo_steps(&self) -> &Loaded<Vec<UndoStep>> {
        &self.undo
    }

    pub fn outgoing(&self) -> &Loaded<Vec<Commit>> {
        &self.outgoing
    }

    pub fn told(&self) -> Option<&Told> {
        self.told.as_ref()
    }

    pub fn running(&self) -> bool {
        self.running
    }

    pub fn show(&mut self, panel: Panel, cx: &mut Context<Self>) {
        match &panel {
            Panel::Changes => {}
            Panel::Commit(sha) => self.load_detail(sha.clone(), cx),
            Panel::Diff { path, of, .. } => self.load_diff(path.clone(), of.clone(), cx),
        }
        self.panel = panel;
        cx.notify();
    }

    pub fn tell(&mut self, told: Told, cx: &mut Context<Self>) {
        let (Told::Done(said) | Told::Failed { said, .. }) = &told;
        eprintln!("desk: git: {said}");
        self.told = Some(told);
        self._toast = Some(cx.spawn(async move |screen, cx| {
            cx.background_executor().timer(TOAST_LIFETIME).await;
            let cleared = screen.update(cx, |screen, cx| {
                screen.told = None;
                cx.notify();
            });
            if let Err(error) = cleared {
                eprintln!("desk: git: the toast ended on a closed screen: {error}");
            }
        }));
        cx.notify();
    }

    pub(super) fn binary(&self) -> Option<Arc<GitBinary>> {
        match &self.git {
            Loaded::Ready(git) => Some(Arc::clone(git)),
            Loaded::Waiting | Loaded::Failed(_) => None,
        }
    }
}
