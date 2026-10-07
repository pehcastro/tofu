use super::replayed::{self, Replayed};

use std::rc::Rc;
use std::time::{Duration, Instant};

use desk_core::model::{Session, Store};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::shells::{Shell, ShellEvent, ShellState, Shells as ShellsTile};
use desk_ui::components::term::TermStatus;
use gpui::{
    AnyView, App, AppContext, Context, Entity, IntoElement, Render, SharedString, Subscription,
    Window,
};

const BOARD: &str = "36-agents";
const TILE_WIDTH: f32 = 640.0;

pub type Kill = Rc<dyn Fn(SharedString, &mut App)>;

pub struct Shells {
    store: Entity<Store>,
    stale: bool,
    kill: Kill,
    opened_at: Instant,
    shells: Vec<Shell>,
    active: usize,
    listing: bool,
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
            module.stale = true;
            cx.notify();
        }),
        store,
        stale: true,
        kill,
        opened_at: Instant::now(),
        shells: Vec::new(),
        active: 0,
        listing: false,
    })
}

fn shown(session: &Session, name: &str, stored: &desk_core::model::Shell, since: Instant) -> Shell {
    let (state, status) = match (stored.exited, stored.exit_code) {
        (false, _) => (ShellState::Running, TermStatus::Running { since }),
        (true, code) => {
            let code = code.and_then(|code| i32::try_from(code).ok()).unwrap_or(-1);
            let state = if code == 0 && !stored.killed {
                ShellState::Exited
            } else {
                ShellState::Failed
            };
            (
                state,
                TermStatus::Exited {
                    code,
                    took: Duration::ZERO,
                },
            )
        }
    };
    let starter = stored.agent.as_ref().map(|id| {
        session.agents.get(id).map_or_else(
            || SharedString::from(id.clone()),
            |agent| format!("{} {}", agent.kind, agent.number).into(),
        )
    });
    Shell {
        name: name.to_owned().into(),
        state,
        command: stored.command.clone().into(),
        pid: stored.pid,
        starter,
        port: stored.port,
        run_time: stored
            .started_at
            .as_deref()
            .and_then(|at| at.get(11..19))
            .map(|at| format!("since {at}").into()),
        lines: stored
            .output
            .lines()
            .map(|line| line.to_owned().into())
            .collect(),
        status,
    }
}

impl Shells {
    pub fn count(&self) -> usize {
        self.shells.len()
    }

    fn rebuild(&mut self, cx: &App) {
        self.stale = false;
        let since = self.opened_at;
        let session = self.store.read(cx).sessions.values().next();
        self.shells = session.map_or_else(Vec::new, |session| {
            session
                .shells
                .iter()
                .map(|(name, stored)| shown(session, name, stored, since))
                .collect()
        });
        self.active = self.active.min(self.shells.len().saturating_sub(1));
    }

    pub fn counted(&mut self, cx: &mut Context<Self>) -> String {
        self.rebuild(cx);
        let shells: Vec<String> = self
            .shells
            .iter()
            .map(|shell| {
                let pid = shell
                    .pid
                    .map_or_else(|| "no pid".to_owned(), |pid| format!("pid {pid}"));
                format!("{} {pid} {:?}", shell.name, shell.state)
            })
            .collect();
        format!("{} shells ({})", self.shells.len(), shells.join(", "))
    }

    fn apply(&mut self, event: ShellEvent) {
        match event {
            ShellEvent::Pick(at) => {
                self.active = at;
                self.listing = false;
            }
            ShellEvent::More => self.listing = true,
            ShellEvent::Dismiss => self.listing = false,
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
        if self.stale {
            self.rebuild(cx);
        }
        let names: Vec<SharedString> = self.shells.iter().map(|shell| shell.name.clone()).collect();
        let kill = self.kill.clone();
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
        .on_kill(move |picked: &usize, _, cx| match names.get(*picked) {
            Some(name) => kill(name.clone(), cx),
            None => eprintln!("desk: shells: kill names shell {picked}, which is not listed"),
        })
    }
}
