use super::chat::cassette;

use std::borrow::Cow;
use std::time::{Duration, Instant};

use desk_core::bridge::Event;
use desk_core::model::{Session, Store};
use desk_ui::components::card::{Header, inner_card, shell};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::shells::{Shell, ShellEvent, ShellState, Shells as ShellsTile};
use desk_ui::components::term::TermStatus;
use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, Context, IntoElement, Render, SharedString, Window, div, prelude::*,
    px,
};

use cassette::{Replay, Step};

const BOARD: &str = "36-agents";
const TILE_WIDTH: f32 = 640.0;
const INSET: f32 = 8.0;
const FONTS: [&[u8]; 6] = [
    include_bytes!("../../../../../assets/fonts/Geist-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-SemiBold.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-SemiBold.ttf"),
];

struct Shells {
    store: Store,
    replay: Option<Replay>,
    opened_at: Instant,
    shells: Vec<Shell>,
    active: usize,
    listing: bool,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if board.is_some_and(|board| board != BOARD) {
        return Err(format!("the shells module replays {BOARD}, not {board:?}"));
    }
    cx.text_system()
        .add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("the shells module cannot load the Geist fonts: {error}"))?;
    let replay = Replay::read()?;
    Ok(cx
        .new(|_| Shells {
            store: Store::default(),
            replay: Some(replay),
            opened_at: Instant::now(),
            shells: Vec::new(),
            active: 0,
            listing: false,
        })
        .into())
}

fn shown(session: &Session, name: &str, stored: &desk_core::model::Shell, since: Instant) -> Shell {
    let (state, status) = match (stored.exited, stored.exit_code) {
        (false, _) => (ShellState::Running, TermStatus::Running { since }),
        (true, code) => {
            let code = code.and_then(|code| i32::try_from(code).ok()).unwrap_or(-1);
            let state = if code == 0 && !stored.killed {
                ShellState::Running
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
    fn session(&self) -> Option<&Session> {
        self.store.sessions.values().next()
    }

    fn rebuild(&mut self) {
        let since = self.opened_at;
        self.shells = self.session().map_or_else(Vec::new, |session| {
            session
                .shells
                .iter()
                .map(|(name, stored)| shown(session, name, stored, since))
                .collect()
        });
        self.active = self.active.min(self.shells.len().saturating_sub(1));
    }

    fn feed(&mut self, event: &Event) {
        if let Err(error) = self.store.apply_batch(std::slice::from_ref(event)) {
            eprintln!("desk: the store refused an event from the cassette: {error}");
        }
    }

    fn settle(&mut self) -> Result<(), String> {
        self.replay = None;
        self.store = Store::default();
        let mut whole = Replay::read()?;
        while let Step::Feed(event) = whole.step()? {
            self.feed(&event);
        }
        self.rebuild();
        let pids: Vec<String> = self
            .shells
            .iter()
            .map(|shell| {
                shell.pid.map_or_else(
                    || format!("{} no pid", shell.name),
                    |pid| format!("{} pid {pid}", shell.name),
                )
            })
            .collect();
        eprintln!(
            "desk: shells from the store: {} shells ({}); cassette: {} shell.started events",
            self.shells.len(),
            pids.join(", "),
            cassette_started()
        );
        Ok(())
    }

    fn frame(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(replay) = &mut self.replay else {
            return;
        };
        window.request_animation_frame();
        let stepped = match replay.step() {
            Ok(Step::Feed(event)) => {
                self.feed(&event);
                self.rebuild();
                Ok(())
            }
            Ok(Step::Restart) => {
                self.store = Store::default();
                Ok(())
            }
            Ok(Step::Report) => self.settle(),
            Err(error) => Err(error),
        };
        if let Err(error) = stepped {
            eprintln!("desk: the cassette has a bad line: {error}");
            cx.quit();
        }
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
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        self.frame(window, cx);
        let theme = ActiveTheme::theme(cx);
        let tile = ShellsTile::new(
            "shells",
            self.shells.clone(),
            self.active,
            self.listing,
            cx.listener(|module, event: &ShellEvent, _, cx| {
                module.apply(*event);
                cx.notify();
            }),
        )
        .on_kill(|picked: &usize, _, _| {
            eprintln!("desk: shells: kill shell {picked}: desk_core's bridge has no kill request");
        });
        let meter = self.replay.as_ref().map(Replay::meter);
        div()
            .size_full()
            .flex()
            .items_start()
            .justify_center()
            .p(px(INSET))
            .child(
                shell(
                    Header::Title(Some(Glyph::Terminal), "Shells".into(), None),
                    &theme,
                )
                .w(px(TILE_WIDTH))
                .child(inner_card(&theme).child(tile)),
            )
            .children(meter)
    }
}
