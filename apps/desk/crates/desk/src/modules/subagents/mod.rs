mod board;
use super::replayed::{self, Replayed};

use std::rc::Rc;

use desk_core::model::{Session, Store};
use desk_ui::components::agents::{AgentBoard, AgentScreen, AgentTile};
use desk_ui::components::avatar::{Agent, AgentStatus};
use desk_ui::components::glyph::Glyph;
use gpui::{
    AnyView, App, AppContext, Context, Entity, IntoElement, KeyDownEvent, Render, Subscription,
    Window, div, prelude::*,
};

const BOARD: &str = "36-agents";
const IWY4: [usize; 4] = [12, 3, 3, 17];
const TILE_WIDTH: f32 = 513.0;

pub struct Subagents {
    store: Entity<Store>,
    stale: bool,
    board: Rc<AgentBoard>,
    tile: Entity<AgentTile>,
    screen: Entity<AgentScreen>,
    expanded: Option<Agent>,
    _watch: Subscription,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if board.is_some_and(|board| board != BOARD) {
        return Err(format!(
            "the sub-agents module replays {BOARD}, not {board:?}"
        ));
    }
    replayed::fonts(cx)?;
    let store = cx.new(|_| Store::default());
    let module = mount(store.clone(), cx);
    let counted = module.clone();
    Replayed::open(
        store,
        module.into(),
        (Glyph::Agents, "Sub-agents", TILE_WIDTH),
        move |cx| {
            let line = counted.update(cx, |module, cx| module.counted(cx));
            eprintln!(
                "desk: sub-agents from the store: {line}; IWY-4: Working {} Asking {} Failed {} Finished {} of {}",
                IWY4[0],
                IWY4[1],
                IWY4[2],
                IWY4[3],
                IWY4.iter().sum::<usize>()
            );
        },
        cx,
    )
}

pub fn mount(store: Entity<Store>, cx: &mut App) -> Entity<Subagents> {
    cx.new(|cx: &mut Context<Subagents>| {
        let board = Rc::new(empty());
        let this = cx.weak_entity();
        let expand = move |agent: &Agent, _: &mut Window, cx: &mut App| {
            this.update(cx, |module, cx| module.expand(Some(*agent), cx))
                .ok();
        };
        let tile = cx.new(|cx| AgentTile::new(board.clone(), mention, expand, cx));
        let screen = cx.new(|_| AgentScreen::new(board.clone(), mention));
        Subagents {
            _watch: cx.observe(&store, |module, _, cx| {
                module.stale = true;
                cx.notify();
            }),
            store,
            stale: true,
            board,
            tile,
            screen,
            expanded: None,
        }
    })
}

fn empty() -> AgentBoard {
    AgentBoard {
        lines: Vec::new(),
        events: Vec::new(),
    }
}

fn mention(agent: &Agent, _: &mut Window, _: &mut App) {
    eprintln!(
        "desk: {} mentioned, and the composer takes no mention yet",
        agent.name()
    );
}

fn session(store: &Store) -> Option<&Session> {
    store.sessions.values().next()
}

impl Subagents {
    pub fn live(&self) -> usize {
        self.board.live()
    }

    fn rebuild(&mut self, cx: &mut Context<Self>) {
        self.stale = false;
        self.board = Rc::new(session(self.store.read(cx)).map_or_else(empty, board::board));
        let board = self.board.clone();
        self.tile
            .update(cx, |tile, cx| tile.set_board(board.clone(), cx));
        self.screen
            .update(cx, |screen, cx| screen.set_board(board, cx));
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

    pub fn counted(&mut self, cx: &mut Context<Self>) -> String {
        self.rebuild(cx);
        let Some(session) = session(self.store.read(cx)) else {
            return "no session".to_owned();
        };
        let [working, asking, failed, finished] = AgentStatus::ALL.map(|status| {
            self.board
                .lines
                .iter()
                .filter(|line| line.agent.status == status)
                .count()
        });
        let unshown = session
            .agents
            .values()
            .filter(|agent| board::shown(agent).is_none())
            .count();
        format!(
            "Working {working} Asking {asking} Failed {failed} Finished {finished} of {}, {unshown} with no desk_ui kind",
            session.agents.len()
        )
    }
}

impl Render for Subagents {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if self.stale {
            self.rebuild(cx);
        }
        let shown: AnyView = match self.expanded {
            None => self.tile.clone().into(),
            Some(_) => self.screen.clone().into(),
        };
        div()
            .size_full()
            .on_key_down(cx.listener(|module, event: &KeyDownEvent, _, cx| {
                if event.keystroke.key == "escape" && module.expanded.is_some() {
                    module.expand(None, cx);
                }
            }))
            .child(shown)
    }
}
