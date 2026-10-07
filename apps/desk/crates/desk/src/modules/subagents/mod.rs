mod board;
use super::chat::cassette;

use std::borrow::Cow;
use std::rc::Rc;

use desk_core::bridge::Event;
use desk_core::model::{Session, Store};
use desk_ui::components::agents::{AgentBoard, AgentScreen, AgentTile, summary};
use desk_ui::components::avatar::{Agent, AgentStatus};
use desk_ui::components::card::{Header, inner_card, shell};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::tabs::{Tab, TabEvent, TabMark, connected_tabs};
use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, Context, Entity, IntoElement, Render, SharedString, Window, div,
    prelude::*, px,
};

use cassette::{Replay, Step};

const BOARD: &str = "36-agents";
const IWY4: [usize; 4] = [12, 3, 3, 17];
const TILE_WIDTH: f32 = 513.0;
const TILE_INSET: f32 = 8.0;
const FONTS: [&[u8]; 6] = [
    include_bytes!("../../../../../assets/fonts/Geist-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-SemiBold.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-SemiBold.ttf"),
];

struct Subagents {
    store: Store,
    replay: Option<Replay>,
    board: Rc<AgentBoard>,
    tile: Entity<AgentTile>,
    screen: Entity<AgentScreen>,
    expanded: Option<Agent>,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if board.is_some_and(|board| board != BOARD) {
        return Err(format!(
            "the sub-agents module replays {BOARD}, not {board:?}"
        ));
    }
    cx.text_system()
        .add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("the sub-agents module cannot load the Geist fonts: {error}"))?;
    let replay = Replay::read()?;
    Ok(cx
        .new(|cx| {
            let board = Rc::new(AgentBoard {
                lines: Vec::new(),
                events: Vec::new(),
            });
            let (tile, screen) = Subagents::views(&board, cx);
            Subagents {
                store: Store::default(),
                replay: Some(replay),
                board,
                tile,
                screen,
                expanded: None,
            }
        })
        .into())
}

fn mention(agent: &Agent, _: &mut Window, _: &mut App) {
    eprintln!(
        "desk: {} mentioned, and this window has no chat",
        agent.name()
    );
}

impl Subagents {
    fn views(
        board: &Rc<AgentBoard>,
        cx: &mut Context<Self>,
    ) -> (Entity<AgentTile>, Entity<AgentScreen>) {
        let this = cx.weak_entity();
        let expand = move |agent: &Agent, _: &mut Window, cx: &mut App| {
            this.update(cx, |module, cx| module.expand(Some(*agent), cx))
                .ok();
        };
        let tile = cx.new(|cx| AgentTile::new(board.clone(), mention, expand, cx));
        let screen = cx.new(|_| AgentScreen::new(board.clone(), mention));
        (tile, screen)
    }

    fn session(&self) -> Option<&Session> {
        self.store.sessions.values().next()
    }

    fn rebuild(&mut self, cx: &mut Context<Self>) {
        self.board = Rc::new(self.session().map_or_else(
            || AgentBoard {
                lines: Vec::new(),
                events: Vec::new(),
            },
            board::board,
        ));
        (self.tile, self.screen) = Self::views(&self.board, cx);
        self.expand(self.expanded, cx);
    }

    fn expand(&mut self, agent: Option<Agent>, cx: &mut Context<Self>) {
        self.expanded = agent.and_then(|agent| {
            self.board
                .lines
                .iter()
                .map(|line| line.agent)
                .find(|now| now.kind == agent.kind && now.instance == agent.instance)
        });
        if let Some(agent) = self.expanded {
            self.screen.update(cx, |screen, cx| screen.show(agent, cx));
        }
        cx.notify();
    }

    fn feed(&mut self, event: &Event) {
        if let Err(error) = self.store.apply_batch(std::slice::from_ref(event)) {
            eprintln!("desk: the store refused an event from the cassette: {error}");
        }
    }

    fn counts(&self) {
        let Some(session) = self.session() else {
            return eprintln!("desk: sub-agents: the store holds no session");
        };
        let count = |status| {
            session
                .agents
                .values()
                .filter(|agent| board::status(&agent.state) == status)
                .count()
        };
        let [working, asking, failed, finished] = AgentStatus::ALL.map(count);
        let unshown = session
            .agents
            .values()
            .filter(|agent| board::shown(agent).is_none())
            .count();
        eprintln!(
            "desk: sub-agents from the store: Working {working} Asking {asking} Failed {failed} Finished {finished} of {}, {unshown} with no desk_ui kind; IWY-4: Working {} Asking {} Failed {} Finished {} of {}",
            session.agents.len(),
            IWY4[0],
            IWY4[1],
            IWY4[2],
            IWY4[3],
            IWY4.iter().sum::<usize>()
        );
    }

    fn settle(&mut self, cx: &mut Context<Self>) -> Result<(), String> {
        self.replay = None;
        self.store = Store::default();
        let mut whole = Replay::read()?;
        while let Step::Feed(event) = whole.step()? {
            self.feed(&event);
        }
        self.counts();
        self.rebuild(cx);
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
                self.rebuild(cx);
                Ok(())
            }
            Ok(Step::Restart) => {
                self.store = Store::default();
                Ok(())
            }
            Ok(Step::Report) => self.settle(cx),
            Err(error) => Err(error),
        };
        if let Err(error) = stepped {
            eprintln!("desk: the cassette has a bad line: {error}");
            cx.quit();
        }
    }

    fn tabs(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let session = self.session();
        let count = |n: usize| u32::try_from(n).ok();
        let tab = |label: SharedString, icon, count| Tab {
            label,
            icon: Some(icon),
            count,
            mark: TabMark::Close,
        };
        let mut tabs = vec![
            tab("Sub-agents".into(), Glyph::Agents, count(self.board.live())),
            tab(
                "File edits".into(),
                Glyph::File,
                session.and_then(|s| count(s.files.len())),
            ),
            tab(
                "Shells".into(),
                Glyph::Terminal,
                session.and_then(|s| count(s.shells.len())),
            ),
        ];
        tabs.extend(
            self.expanded
                .map(|agent| tab(agent.name(), Glyph::Agents, None)),
        );
        let active = if self.expanded.is_some() { 3 } else { 0 };
        let this = cx.weak_entity();
        connected_tabs(
            "subagents-tabs",
            &tabs,
            active,
            usize::MAX,
            &ActiveTheme::theme(cx),
            move |event, _, cx| {
                let shown = match event {
                    TabEvent::Select(0) | TabEvent::Close(3) => None,
                    TabEvent::Select(_) | TabEvent::Close(_) | TabEvent::More | TabEvent::New => {
                        return;
                    }
                };
                this.update(cx, |module, cx| module.expand(shown, cx)).ok();
            },
        )
    }
}

impl Render for Subagents {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        self.frame(window, cx);
        let theme = ActiveTheme::theme(cx);
        let tabs = self.tabs(cx).into_any_element();
        let card = match self.expanded {
            None => shell(Header::Tabs(tabs, None), &theme)
                .w(px(TILE_WIDTH))
                .child(
                    inner_card(&theme)
                        .flex_1()
                        .min_h_0()
                        .child(self.tile.clone()),
                ),
            Some(_) => shell(
                Header::Tabs(tabs, Some(summary(&self.board, &theme).into_any_element())),
                &theme,
            )
            .flex_1()
            .child(
                inner_card(&theme)
                    .flex_1()
                    .min_h_0()
                    .child(self.screen.clone()),
            ),
        };
        let meter = self.replay.as_ref().map(Replay::meter);
        div()
            .size_full()
            .flex()
            .justify_center()
            .p(px(TILE_INSET))
            .child(card.h_full())
            .children(meter)
    }
}
