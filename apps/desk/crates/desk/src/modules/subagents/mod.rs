mod board;
use super::replayed::{self, Replayed};

use std::rc::Rc;

use desk_core::model::{Session, Store};
use desk_ui::components::agents::{AgentBoard, AgentScreen, AgentTile};
use desk_ui::components::avatar::{Agent, AgentStatus};
use desk_ui::components::glyph::Glyph;

use super::chat::{Trace, mention};
use gpui::{
    AnyView, App, AppContext, Context, Entity, EventEmitter, IntoElement, Render, Subscription,
    Window,
};

const BOARD: &str = "36-agents";
const IWY4: [usize; 4] = [12, 3, 3, 17];
const TILE_WIDTH: f32 = 513.0;

pub struct Subagents {
    store: Entity<Store>,
    board: Rc<AgentBoard>,
    tile: Entity<AgentTile>,
    screen: Entity<AgentScreen>,
    _watch: Subscription,
}

pub struct ExpandAgent(pub Agent);

impl EventEmitter<ExpandAgent> for Subagents {}

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
            this.update(cx, |_, cx| cx.emit(ExpandAgent(*agent)))
                .unwrap_or_else(|_| eprintln!("desk: sub-agents: the module is gone"));
        };
        let tile = cx.new(|cx| AgentTile::new(board.clone(), mentioner(&store), expand, cx));
        let screen = cx.new(|_| AgentScreen::new(board.clone(), mentioner(&store)));
        Subagents {
            _watch: cx.observe(&store, |module, _, cx| {
                module.rebuild(cx);
                eprintln!(
                    "desk: tile sub-agents rebuilt {} rows, {} ends from session.trace",
                    module.board.lines.len(),
                    module.store.read(cx).agent_runs().len()
                );
                cx.notify();
            }),
            store,
            board,
            tile,
            screen,
        }
    })
}

fn empty() -> AgentBoard {
    AgentBoard {
        lines: Vec::new(),
        events: Vec::new(),
        attributed: false,
    }
}

fn mentioner(store: &Entity<Store>) -> impl Fn(&Agent, &mut Window, &mut App) + 'static {
    let store = store.downgrade();
    move |agent, window, cx| {
        let found = store.upgrade().and_then(|store| {
            let session = store.read(cx).open_session()?;
            let member = session.agents.values().find(|member| {
                board::shown(member).is_some_and(|shown| {
                    shown.kind == agent.kind && shown.instance == agent.instance
                })
            })?;
            Some(Trace {
                token: member.mention.clone()?,
                glyph: Glyph::Agents,
                label: agent.name().to_string(),
                detail: member.task.lines().next().unwrap_or_default().to_owned(),
            })
        });
        match found {
            Some(trace) => mention(trace, window, cx),
            None => eprintln!("desk: {} has no ref on the wire to mention", agent.name()),
        }
    }
}

fn session(store: &Store) -> Option<&Session> {
    store.open_session()
}

impl Subagents {
    pub fn live(&self) -> usize {
        self.board.live()
    }

    pub fn screen(&self, agent: Option<Agent>, cx: &mut Context<Self>) -> Entity<AgentScreen> {
        self.screen.update(cx, |screen, cx| screen.show(agent, cx));
        eprintln!(
            "desk: sub-agents screen on {}",
            agent.map_or_else(|| "All activity".into(), |agent| agent.name())
        );
        self.screen.clone()
    }

    fn rebuild(&mut self, cx: &mut Context<Self>) {
        let store = self.store.read(cx);
        self.board = Rc::new(
            session(store).map_or_else(empty, |open| board::board(open, store.agent_runs())),
        );
        let board = self.board.clone();
        self.tile
            .update(cx, |tile, cx| tile.set_board(board.clone(), cx));
        self.screen
            .update(cx, |screen, cx| screen.set_board(board, cx));
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
    fn render(&mut self, _: &mut Window, _: &mut Context<Self>) -> impl IntoElement {
        self.tile.clone()
    }
}
