use super::replayed::{self, Replayed};

use std::path::{Component, Path};
use std::rc::Rc;
use std::time::Instant;

use desk_core::model::{Session, Store};
use desk_core::protocol::ShellReady;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::shells::{Runtime, Shell, ShellEvent, ShellState, Shells as ShellsTile};
use gpui::{
    AnyView, App, AppContext, Context, Entity, IntoElement, Render, SharedString, Subscription,
    Window,
};

const BOARD: &str = "36-agents";
const TILE_WIDTH: f32 = 640.0;
const DIR_TAIL: usize = 2;

pub type Kill = Rc<dyn Fn(SharedString, &mut App)>;

pub struct Shells {
    store: Entity<Store>,
    kill: Kill,
    shells: Vec<Shell>,
    active: usize,
    listing: bool,
    asking: Option<usize>,
    _watch: Subscription,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if board.is_some_and(|board| board != BOARD) {
        return Err(format!("the shells module replays {BOARD}, not {board:?}"));
    }
    replayed::fonts(cx)?;
    let store = cx.new(|_| Store::default());
    let kill: Kill = Rc::new(|shell, _| {
        eprintln!("desk: shells: kill {shell}: a replayed cassette has no tofu to ask");
    });
    let module = mount(store.clone(), kill, cx);
    let counted = module.clone();
    Replayed::open(
        store,
        module.into(),
        (Glyph::Terminal, "Shells", TILE_WIDTH),
        move |cx| {
            let line = counted.update(cx, |module, cx| module.counted(cx));
            eprintln!(
                "desk: shells from the store: {line}; cassette: {} shell.started events",
                cassette_started()
            );
        },
        cx,
    )
}

pub fn mount(store: Entity<Store>, kill: Kill, cx: &mut App) -> Entity<Shells> {
    cx.new(|cx: &mut Context<Shells>| Shells {
        _watch: cx.observe(&store, |module, _, cx| {
            module.rebuild(cx);
            cx.notify();
        }),
        store,
        kill,
        shells: Vec::new(),
        active: 0,
        listing: false,
        asking: None,
    })
}

fn moment(at: Option<&str>) -> Option<chrono::DateTime<chrono::FixedOffset>> {
    at.and_then(|at| chrono::DateTime::parse_from_rfc3339(at).ok())
}

fn runtime(state: ShellState, stored: &desk_core::model::Shell) -> Runtime {
    let started = moment(stored.started_at.as_deref());
    if state.endable() {
        return Runtime::Live(
            started
                .and_then(|at| (chrono::Utc::now() - at.to_utc()).to_std().ok())
                .and_then(|age| Instant::now().checked_sub(age))
                .unwrap_or(stored.first_seen),
        );
    }
    started
        .zip(moment(stored.ended_at.as_deref()))
        .and_then(|(started, ended)| (ended - started).to_std().ok())
        .map_or(Runtime::Unknown, Runtime::Ended)
}

fn short_dir(dir: &str) -> SharedString {
    let names: Vec<_> = Path::new(dir)
        .components()
        .filter_map(|part| match part {
            Component::Normal(name) => Some(name.to_string_lossy()),
            Component::Prefix(_)
            | Component::RootDir
            | Component::CurDir
            | Component::ParentDir => None,
        })
        .collect();
    match names.len().checked_sub(DIR_TAIL) {
        None | Some(0) => dir.to_owned().into(),
        Some(from) => format!(
            "\u{2026}{}{}",
            std::path::MAIN_SEPARATOR,
            names
                .get(from..)
                .unwrap_or_default()
                .join(std::path::MAIN_SEPARATOR_STR)
        )
        .into(),
    }
}

fn ready(ready: &ShellReady) -> SharedString {
    match ready {
        ShellReady::Port => "ready: its port opened".into(),
        ShellReady::Line => "ready: it printed a ready line".into(),
        ShellReady::Waited => "the wait ran out".into(),
        ShellReady::Stopped => "the turn stopped".into(),
        ShellReady::Unknown(raw) => format!("ready: {raw}").into(),
    }
}

fn shown(session: &Session, name: &str, stored: &desk_core::model::Shell) -> Option<Shell> {
    if stored.kept.is_none() && !stored.left_over {
        return None;
    }
    let state = match (stored.exited, stored.killed, stored.left_over) {
        (true, true, _) => ShellState::Killed,
        (true, false, _) => {
            ShellState::Exited(stored.exit_code.and_then(|code| i32::try_from(code).ok()))
        }
        (false, _, true) => ShellState::LeftOver,
        (false, _, false) => ShellState::Running,
    };
    let starter = stored.agent.as_ref().map(|id| {
        session.agents.get(id).map_or_else(
            || SharedString::from(id.clone()),
            |agent| format!("{} {}", agent.kind, agent.number).into(),
        )
    });
    Some(Shell {
        name: name.to_owned().into(),
        state,
        command: stored.command.clone().into(),
        dir: short_dir(&stored.dir),
        pid: stored.pid,
        starter,
        port: stored.port,
        ready: stored.ready.as_ref().map(ready),
        runtime: runtime(state, stored),
        lines: stored
            .output
            .lines()
            .map(|line| line.to_owned().into())
            .collect(),
    })
}

impl Shells {
    pub fn count(&self) -> usize {
        self.shells
            .iter()
            .filter(|shell| shell.state == ShellState::Running)
            .count()
    }

    fn rebuild(&mut self, cx: &mut Context<Self>) {
        let session = self.store.read(cx).open_session();
        let mut shells: Vec<(Option<&str>, Shell)> = session.map_or_else(Vec::new, |session| {
            session
                .shells
                .iter()
                .filter_map(|(name, stored)| {
                    shown(session, name, stored).map(|shell| (stored.started_at.as_deref(), shell))
                })
                .collect()
        });
        shells.sort_by_key(|(started, _)| *started);
        self.shells = shells.into_iter().map(|(_, shell)| shell).collect();
        self.active = self.active.min(self.shells.len().saturating_sub(1));
        self.asking = self.asking.filter(|at| {
            self.shells
                .get(*at)
                .is_some_and(|shell| shell.state.endable())
        });
        eprintln!("desk: tile shells rebuilt {}", self.listed());
    }

    fn listed(&self) -> String {
        let shells: Vec<String> = self
            .shells
            .iter()
            .map(|shell| {
                let pid = shell
                    .pid
                    .map_or_else(|| "no pid".to_owned(), |pid| format!("pid {pid}"));
                format!("{} {pid} {}", shell.name, shell.state.word())
            })
            .collect();
        format!(
            "{} kept shells, {} running ({})",
            self.shells.len(),
            self.count(),
            shells.join(", ")
        )
    }

    pub fn counted(&mut self, cx: &mut Context<Self>) -> String {
        self.rebuild(cx);
        self.listed()
    }

    fn apply(&mut self, event: ShellEvent) {
        match event {
            ShellEvent::Pick(at) => {
                self.active = at;
                self.listing = false;
                self.asking = None;
            }
            ShellEvent::More => self.listing = true,
            ShellEvent::Dismiss => self.listing = false,
            ShellEvent::AskKill(at) => self.asking = Some(at),
            ShellEvent::Keep => self.asking = None,
            ShellEvent::Close(at) | ShellEvent::Open(at) | ShellEvent::Trace(at) => {
                eprintln!("desk: shells: {event:?} on shell {at} is not wired");
            }
        }
    }
}

fn cassette_started() -> usize {
    include_str!("../../../../../cassettes/36-agents.cassette")
        .lines()
        .filter(|line| line.contains("\"method\": \"shell.started\""))
        .count()
}

impl Render for Shells {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        ShellsTile::new(
            "shells",
            self.shells.clone(),
            self.active,
            self.listing,
            cx.listener(|module, event: &ShellEvent, _, cx| {
                module.apply(*event);
                cx.notify();
            }),
        )
        .asking(self.asking)
        .on_kill(cx.listener(|module, picked: &usize, _, cx| {
            module.asking = None;
            match module.shells.get(*picked) {
                Some(shell) => (module.kill)(shell.name.clone(), cx),
                None => eprintln!("desk: shells: kill names shell {picked}, which is not listed"),
            }
            cx.notify();
        }))
    }
}
