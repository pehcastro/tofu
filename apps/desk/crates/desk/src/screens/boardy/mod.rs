mod create;
mod look;
mod sidebar;
mod ticket;
mod views;

use std::collections::BTreeMap;
use std::rc::Rc;
use std::time::Instant;

use desk_core::boardy::{ALL_VIEW, BoardyStore, COLUMNS, Flow, Gate};
use desk_core::limits::TOAST_LIFETIME;
use desk_core::protocol::boardy::{
    AnswerRequest, AskedQuestion, BoardsAnswer, GetAnswer, GetRequest, InitRequest, ListRequest,
    LogRequest, MoveRequest, PlanAnswer, PlanRequest, QuestionsRequest, ReportAnswer,
    ReportRequest, Request as Asked, TriageRequest,
};
use desk_core::protocol::request::{
    BoardyAnswer, BoardyBoards, BoardyGet, BoardyInit, BoardyList, BoardyLog, BoardyMove,
    BoardyPlan, BoardyQuestions, BoardyReport, BoardyTriage,
};
use desk_core::protocol::{
    BoardyTicketStatus as Status, BoardyTo, BoardyTriageRequestAction, NoParams, Request,
};
use desk_motion::tokens::{PANEL_IN_MS, PANEL_OUT_MS};
use desk_motion::{FlipTracker, Presence};
use desk_ui::components::chat::{Block, markdown};
use desk_ui::components::form::{TextArea, TextInput};
use desk_ui::components::overlay::{MenuItem, context_menu, toast};
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::TOAST_BOTTOM;
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Entity, EntityId, FocusHandle, Focusable,
    KeyDownEvent, MouseButton, MouseDownEvent, Pixels, Point, Render, ScrollHandle,
    ScrollWheelEvent, SharedString, Task, UniformListScrollHandle, WeakEntity, Window, canvas, div,
    prelude::*, px,
};

use super::frame::{self, load_fonts, titled};
use crate::modules::chat::Chat;

const NEWEST: usize = 5;
const DROPPED: &str = "dropped from the desk";
const SCROLL_WINDOW_MS: u128 = 150;
const MS_PER_SECOND: f64 = 1000.0;

pub fn open(board: Option<&str>, window: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    load_fonts(cx)?;
    let asked = board.map(str::to_owned);
    Ok(cx.new(|cx| Boardy::new(asked, window, cx)).into())
}

#[derive(Clone, Debug, PartialEq, Eq)]
enum Place {
    CatchUp,
    Attention,
    Board(String, Section),
    Ticket(String),
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Section {
    Tickets,
    Milestones,
    Epics,
    Sprints,
    Triage,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Layout {
    List,
    Board,
}

struct Opened {
    answer: GetAnswer,
    body: Vec<Block>,
    acceptance: Vec<Block>,
    said: Vec<Option<Vec<Block>>>,
}

pub struct Boardy {
    tofu: Option<(WeakEntity<Chat>, EntityId)>,
    project: String,
    flow: Flow,
    asked: Option<String>,
    store: BoardyStore,
    reports: BTreeMap<String, ReportAnswer>,
    triage: BTreeMap<String, Vec<Asked>>,
    plans: BTreeMap<String, PlanAnswer>,
    questions: Vec<AskedQuestion>,
    naming: Option<Entity<TextInput>>,
    no_boards: bool,
    place: Place,
    back: Place,
    layout: Layout,
    trail: Vec<String>,
    at: usize,
    opened: Option<Opened>,
    folded: Vec<Status>,
    lines: Rc<Vec<views::Line>>,
    aimed: Option<String>,
    menu_at: Option<Point<Pixels>>,
    flips: Entity<FlipTracker>,
    composer: Entity<TextArea>,
    wanted: Option<(String, Status)>,
    draft: Option<create::Draft>,
    shown: Presence,
    panel: Entity<ticket::Width>,
    toast: Option<SharedString>,
    failed: Option<SharedString>,
    asking: usize,
    focus: FocusHandle,
    focused: bool,
    follow: bool,
    list: UniformListScrollHandle,
    board_scroll: ScrollHandle,
    started: Instant,
    painted: bool,
    wheel: Option<Instant>,
    _toast: Option<Task<()>>,
}

impl Boardy {
    fn new(asked: Option<String>, window: &mut Window, cx: &mut Context<Self>) -> Self {
        let weak = cx.weak_entity();
        let composer = cx.new(|cx| {
            TextArea::new("Leave a comment".into(), window, cx).on_submit(move |text, _, cx| {
                let text = text.trim().to_owned();
                if let Err(error) = weak.update(cx, |boardy, cx| boardy.comment(text, cx)) {
                    eprintln!("desk: boardy closed before the comment was sent: {error}");
                }
            })
        });
        Boardy {
            tofu: None,
            project: String::new(),
            flow: Flow::Jira,
            asked,
            store: BoardyStore::default(),
            reports: BTreeMap::new(),
            triage: BTreeMap::new(),
            plans: BTreeMap::new(),
            questions: Vec::new(),
            naming: None,
            no_boards: false,
            place: Place::CatchUp,
            back: Place::CatchUp,
            layout: Layout::List,
            trail: Vec::new(),
            at: 0,
            opened: None,
            folded: Vec::new(),
            lines: Rc::default(),
            aimed: None,
            menu_at: None,
            flips: cx.new(|_| FlipTracker::default()),
            composer,
            wanted: None,
            draft: None,
            shown: Presence::new(PANEL_IN_MS, PANEL_OUT_MS),
            panel: cx.new(|_| ticket::Width::default()),
            toast: None,
            failed: None,
            asking: 0,
            focus: cx.focus_handle(),
            focused: false,
            follow: false,
            list: UniformListScrollHandle::new(),
            board_scroll: ScrollHandle::new(),
            started: Instant::now(),
            painted: false,
            wheel: None,
            _toast: None,
        }
    }

    pub fn read_from(&mut self, chat: &Entity<Chat>, gate: Gate, cx: &mut Context<Self>) {
        let same = self
            .tofu
            .as_ref()
            .is_some_and(|(_, id)| *id == chat.entity_id());
        if same && gate.flow == self.flow {
            return;
        }
        self.flow = gate.flow;
        self.tofu = Some((chat.downgrade(), chat.entity_id()));
        chat.read(cx).project().clone_into(&mut self.project);
        eprintln!(
            "desk: boardy reads from tofu in {}, flow {:?}",
            self.project, self.flow
        );
        self.refetch(cx);
    }

    pub fn changed(&mut self, board: &str, cx: &mut Context<Self>) {
        if self.tofu.is_none() {
            return;
        }
        self.fetch_board(board.to_owned(), cx);
        self.ask_questions(cx);
        if let Place::Ticket(id) = &self.place
            && id.split_once('-').is_some_and(|(held, _)| held == board)
        {
            self.get(id.clone(), cx);
        }
    }

    fn refetch(&mut self, cx: &mut Context<Self>) {
        self.failed = None;
        self.send::<BoardyBoards>(NoParams {}, cx, |boardy, reply, cx| {
            boardy.boards_landed(reply, None, cx)
        });
        self.ask_questions(cx);
    }

    fn boards_landed(
        &mut self,
        reply: Result<BoardsAnswer, String>,
        open: Option<String>,
        cx: &mut Context<Self>,
    ) {
        let answer = match reply {
            Ok(answer) => answer,
            Err(error) => return self.fail(format!("boardy.boards in {}: {error}", self.project)),
        };
        let first_read = self.store.boards.is_empty();
        self.store.boards_landed(answer);
        let keys: Vec<String> = self.store.boards.iter().map(|b| b.key.clone()).collect();
        eprintln!(
            "desk: boardy boards in {}: {} [{}]",
            self.project,
            keys.len(),
            keys.join(" ")
        );
        self.no_boards = keys.is_empty();
        match self.no_boards {
            true => self.fail(format!(
                "tofu has no {}s for {}",
                self.store.words.board, self.project
            )),
            false => self.failed = None,
        }
        let landing = first_read.then(|| {
            self.asked
                .clone()
                .filter(|asked| keys.contains(asked))
                .or_else(|| keys.first().cloned())
        });
        let first = open.or(landing.flatten());
        if let Some(first) = first {
            self.place = Place::Board(first, Section::Tickets);
        }
        for key in keys {
            self.fetch_board(key, cx);
        }
    }

    fn ask_questions(&mut self, cx: &mut Context<Self>) {
        self.send::<BoardyQuestions>(QuestionsRequest { board: None }, cx, |boardy, reply, _| {
            match reply {
                Ok(answer) => {
                    eprintln!("desk: boardy questions {}", answer.questions.len());
                    boardy.questions = answer.questions;
                }
                Err(error) => boardy.fail(error),
            }
        });
    }

    fn answer_question(&mut self, ticket: String, id: i64, answer: String, cx: &mut Context<Self>) {
        eprintln!("desk: boardy answer {ticket} Q{id} with {answer}");
        let params = AnswerRequest { answer, id, ticket };
        self.send::<BoardyAnswer>(params, cx, |boardy, reply, cx| {
            boardy.landed(reply, cx);
            boardy.ask_questions(cx);
        });
    }

    fn start_naming(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let input = TextInput::new(
            format!(
                "{} key, such as WEB, Enter to create",
                look::noun(&self.store.words.board, false)
            )
            .into(),
            window,
            cx,
        );
        input.focus_handle(cx).focus(window, cx);
        self.naming = Some(input);
        cx.notify();
    }

    fn create_board(&mut self, cx: &mut Context<Self>) {
        let Some(input) = self.naming.take() else {
            return;
        };
        let key = input.read(cx).text().trim().to_uppercase();
        if key.is_empty() {
            return;
        }
        eprintln!("desk: boardy init {key} in {}", self.project);
        let params = InitRequest {
            key: key.clone(),
            name: key.clone(),
        };
        self.send::<BoardyInit>(params, cx, move |boardy, reply, cx| match reply {
            Ok(answer) => boardy.boards_landed(Ok(answer), Some(key), cx),
            Err(error) => boardy.say(error, cx),
        });
    }

    fn fetch_board(&mut self, key: String, cx: &mut Context<Self>) {
        let list = ListRequest {
            board: Some(key.clone()),
            view: Some(ALL_VIEW.to_owned()),
            filter: None,
        };
        let listed = key.clone();
        self.send::<BoardyList>(list, cx, move |boardy, reply, cx| {
            let first = !boardy.store.read(&listed);
            match reply.map(|answer| boardy.store.list_landed(answer)) {
                Ok(Ok(())) if first => boardy.land_selection(),
                Ok(Ok(())) => eprintln!(
                    "desk: boardy {listed} refetched, board scroll x {:.1}, list scroll y {:.1}",
                    f32::from(boardy.board_scroll.offset().x),
                    f32::from(boardy.list.0.borrow().base_handle.offset().y)
                ),
                Ok(Err(error)) => boardy.fail(error.to_string()),
                Err(error) => boardy.fail(error),
            }
            cx.notify();
        });
        let report = ReportRequest {
            board: Some(key.clone()),
            since: None,
        };
        self.send::<BoardyReport>(report, cx, |boardy, reply, _| match reply {
            Ok(answer) => {
                boardy.reports.insert(answer.board.clone(), answer);
            }
            Err(error) => boardy.fail(error),
        });
        let plan = PlanRequest { board: key.clone() };
        let planned = key.clone();
        self.send::<BoardyPlan>(plan, cx, move |boardy, reply, _| match reply {
            Ok(answer) => {
                boardy.plans.insert(planned, answer);
            }
            Err(error) => boardy.fail(error),
        });
        let triage = TriageRequest {
            action: BoardyTriageRequestAction::List,
            board: Some(key),
            reason: None,
            request: None,
        };
        self.send::<BoardyTriage>(triage, cx, |boardy, reply, _| match reply {
            Ok(answer) => {
                boardy.triage.insert(answer.board, answer.requests);
            }
            Err(error) => boardy.fail(error),
        });
    }

    fn triage_act(&mut self, board: String, request: String, accept: bool, cx: &mut Context<Self>) {
        eprintln!("desk: boardy triage {request} accept {accept}");
        let params = TriageRequest {
            action: match accept {
                true => BoardyTriageRequestAction::Accept,
                false => BoardyTriageRequestAction::Drop,
            },
            board: Some(board.clone()),
            reason: (!accept).then(|| DROPPED.to_owned()),
            request: Some(request),
        };
        self.send::<BoardyTriage>(params, cx, move |boardy, reply, cx| match reply {
            Ok(_) => boardy.fetch_board(board, cx),
            Err(error) => boardy.say(error, cx),
        });
    }

    fn land_selection(&mut self) {
        if self.at != 0 || matches!(self.place, Place::Ticket(_)) {
            return;
        }
        let landing = self.flow.landing();
        let order = self.order();
        let found = order.iter().position(|id| {
            self.store
                .find(id)
                .is_some_and(|ticket| ticket.status == landing)
        });
        if let Some(at) = found {
            self.at = at;
            self.follow = true;
        }
    }

    fn fail(&mut self, error: impl Into<SharedString>) {
        let error = error.into();
        eprintln!("desk: boardy: {error}");
        self.failed = Some(error);
    }

    fn send<R: Request>(
        &mut self,
        params: R::Params,
        cx: &mut Context<Self>,
        then: impl FnOnce(&mut Self, Result<R::Result, String>, &mut Context<Self>) + 'static,
    ) where
        R::Result: 'static,
    {
        let chat = self.tofu.as_ref().and_then(|(chat, _)| chat.upgrade());
        let (sent, reply) = flume::bounded(1);
        match chat {
            Some(chat) => chat.update(cx, |chat, cx| {
                chat.request::<R>(&params, cx, move |_, reply, _| {
                    sent.send(reply).ok();
                });
            }),
            None => {
                sent.send(Err("the work screen that ran tofu is closed".to_owned()))
                    .ok();
            }
        }
        self.asking = self.asking.saturating_add(1);
        cx.spawn(async move |boardy, cx| {
            let Ok(reply) = reply.recv_async().await else {
                return;
            };
            let landed = boardy.update(cx, |boardy, cx| {
                boardy.asking = boardy.asking.saturating_sub(1);
                then(boardy, reply, cx);
                cx.notify();
            });
            if let Err(error) = landed {
                eprintln!("desk: boardy closed before {} answered: {error}", R::METHOD);
            }
        })
        .detach();
    }

    fn go(&mut self, place: Place, cx: &mut Context<Self>) {
        eprintln!("desk: boardy shows {place:?}");
        self.place = place;
        self.at = 0;
        cx.notify();
    }

    fn open_ticket(&mut self, id: String, cx: &mut Context<Self>) {
        if !matches!(self.place, Place::Ticket(_)) {
            self.back = self.place.clone();
            self.trail = self.order();
        }
        if let Some(at) = self.trail.iter().position(|held| *held == id) {
            self.at = at;
        }
        self.place = Place::Ticket(id.clone());
        self.get(id, cx);
    }

    fn get(&mut self, id: String, cx: &mut Context<Self>) {
        self.send::<BoardyGet>(GetRequest { id }, cx, |boardy, reply, _| match reply {
            Ok(answer) => {
                eprintln!(
                    "desk: boardy got {} with {} events",
                    answer.ticket.id,
                    answer.events.len()
                );
                boardy.opened = Some(Opened {
                    body: markdown(&format!(
                        "{}\n\n{}",
                        answer.ticket.problem, answer.ticket.scope
                    )),
                    acceptance: markdown(
                        answer.ticket.acceptance.last().map_or("", String::as_str),
                    ),
                    said: answer
                        .events
                        .iter()
                        .map(|event| {
                            desk_core::boardy::spoken(event)
                                .then(|| markdown(event.text.as_deref().unwrap_or_default()))
                        })
                        .collect(),
                    answer,
                });
            }
            Err(error) => boardy.fail(error),
        });
    }

    fn move_to(&mut self, id: String, to: Status, cx: &mut Context<Self>) {
        eprintln!("desk: boardy move {id} to {}", String::from(to.clone()));
        let params = MoveRequest {
            id,
            reason: None,
            to: BoardyTo::from(String::from(to)),
        };
        self.send::<BoardyMove>(params, cx, |boardy, reply, cx| boardy.landed(reply, cx));
    }

    fn comment(&mut self, text: String, cx: &mut Context<Self>) {
        let Place::Ticket(id) = &self.place else {
            return;
        };
        if text.is_empty() {
            return;
        }
        self.composer.update(cx, |area, cx| area.clear(cx));
        let params = LogRequest {
            id: id.clone(),
            text,
        };
        self.send::<BoardyLog>(params, cx, |boardy, reply, cx| boardy.landed(reply, cx));
    }

    fn landed(
        &mut self,
        reply: Result<desk_core::protocol::boardy::TicketAnswer, String>,
        cx: &mut Context<Self>,
    ) {
        match reply {
            Ok(answer) => {
                let id = answer.ticket.id.clone();
                eprintln!(
                    "desk: boardy {id} is {}",
                    String::from(answer.ticket.status.clone())
                );
                let fresh = self.store.find(&id).is_none();
                self.store.ticket_landed(answer.ticket);
                if fresh {
                    let board = id.split_once('-').map(|(board, _)| board.to_owned());
                    if let Some(board) = board {
                        self.fetch_board(board, cx);
                    }
                }
                if matches!(&self.place, Place::Ticket(open) if *open == id) {
                    self.get(id, cx);
                }
            }
            Err(error) => self.say(error, cx),
        }
    }

    fn say(&mut self, said: String, cx: &mut Context<Self>) {
        eprintln!("desk: boardy refused: {said}");
        self.toast = Some(said.into());
        self._toast = Some(cx.spawn(async move |boardy, cx| {
            cx.background_executor().timer(TOAST_LIFETIME).await;
            if let Err(error) = boardy.update(cx, |boardy, cx| {
                boardy.toast = None;
                cx.notify();
            }) {
                eprintln!("desk: boardy closed before its toast ended: {error}");
            }
        }));
        cx.notify();
    }

    pub fn open_create(&mut self, status: Option<String>, cx: &mut Context<Self>) {
        let status = status.map_or_else(|| self.flow.landing(), Status::from);
        if let Status::Unknown(raw) = &status {
            return self.say(format!("boardy has no status {raw}"), cx);
        }
        let board = match &self.place {
            Place::Board(board, _) => Some(board.clone()),
            Place::Ticket(id) => id.split_once('-').map(|(board, _)| board.to_owned()),
            Place::CatchUp | Place::Attention => None,
        }
        .or_else(|| self.store.boards.first().map(|board| board.key.clone()));
        let words = &self.store.words;
        let Some(board) = board else {
            return self.say(
                format!("tofu has no {} to hold a new {}", words.board, words.ticket),
                cx,
            );
        };
        eprintln!(
            "desk: boardy: new {} in {board}, {}",
            words.ticket,
            look::label(&status)
        );
        self.wanted = Some((board, status));
        cx.notify();
    }

    fn order(&self) -> Vec<String> {
        views::groups(self)
            .into_iter()
            .filter(|group| !group.folded)
            .flat_map(|group| group.tickets.into_iter().map(|ticket| ticket.id.clone()))
            .collect()
    }

    fn step(&mut self, by: isize, cx: &mut Context<Self>) {
        let ticket = matches!(self.place, Place::Ticket(_));
        let count = if ticket {
            self.trail.len()
        } else {
            self.order().len()
        };
        let Some(last) = count.checked_sub(1) else {
            return;
        };
        self.at = self.at.saturating_add_signed(by).min(last);
        if ticket {
            if let Some(id) = self.trail.get(self.at).cloned() {
                self.open_ticket(id, cx);
            }
        } else {
            self.follow = true;
        }
        cx.notify();
    }

    fn shift(&mut self, by: isize, cx: &mut Context<Self>) {
        let id = match &self.place {
            Place::Ticket(id) => Some(id.clone()),
            _ => self.order().get(self.at).cloned(),
        };
        let Some(ticket) = id.and_then(|id| self.store.find(&id)) else {
            return;
        };
        let Some(from) = COLUMNS.iter().position(|status| *status == ticket.status) else {
            return;
        };
        let to = from.saturating_add_signed(by);
        if let Some(to) = COLUMNS.get(to).filter(|_| to != from).cloned() {
            self.move_to(ticket.id.clone(), to, cx);
        }
    }

    fn key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let key = event.keystroke.key.as_str();
        if self.naming.is_some() {
            match key {
                "enter" => self.create_board(cx),
                "escape" => self.naming = None,
                _ => return,
            }
            cx.stop_propagation();
            return cx.notify();
        }
        if self.drafting() {
            match key {
                "enter" if event.keystroke.modifiers.secondary() => self.submit(window, cx),
                "escape" => self.close_draft(window, cx),
                _ => return,
            }
            cx.stop_propagation();
            return cx.notify();
        }
        if !self.focus.is_focused(window) {
            return;
        }
        match key {
            "j" | "down" => self.step(1, cx),
            "k" | "up" => self.step(-1, cx),
            "]" => self.shift(1, cx),
            "[" => self.shift(-1, cx),
            "c" => self.open_create(None, cx),
            "enter" => match self.order().get(self.at).cloned() {
                Some(id) if !matches!(self.place, Place::Ticket(_)) => self.open_ticket(id, cx),
                _ => return,
            },
            "escape" if matches!(self.place, Place::Ticket(_)) => {
                self.place = self.back.clone();
                self.opened = None;
            }
            _ => return,
        }
        cx.stop_propagation();
        cx.notify();
    }

    fn aim(&mut self, id: String, cx: &mut Context<Self>) {
        self.aimed = Some(id);
        cx.notify();
    }

    fn menu(&self) -> Vec<MenuItem> {
        let current = self
            .aimed
            .as_deref()
            .and_then(|id| self.store.find(id))
            .map(|ticket| ticket.status.clone());
        COLUMNS
            .iter()
            .map(|status| {
                MenuItem::action(format!("Move to {}", look::label(status)))
                    .enabled(Some(status) != current.as_ref())
            })
            .collect()
    }

    fn picked(&mut self, at: usize, cx: &mut Context<Self>) {
        let (Some(id), Some(to)) = (self.aimed.clone(), COLUMNS.get(at).cloned()) else {
            return;
        };
        self.move_to(id, to, cx);
    }

    fn timed(&mut self, started: Instant) -> impl IntoElement + use<> {
        let first = !self.painted
            && match &self.place {
                Place::Board(board, _) => self.store.read(board),
                Place::CatchUp | Place::Attention | Place::Ticket(_) => false,
            };
        if first {
            self.painted = true;
        }
        let opened = self.started;
        let scrolling = self
            .wheel
            .is_some_and(|wheel| wheel.elapsed().as_millis() < SCROLL_WINDOW_MS)
            && self.layout == Layout::Board;
        canvas(
            |_, _, _| {},
            move |_, (), _, _| {
                if first {
                    eprintln!(
                        "desk: boardy: open {:.2}ms",
                        opened.elapsed().as_secs_f64() * MS_PER_SECOND
                    );
                }
                if scrolling {
                    eprintln!(
                        "desk: boardy: frame {:.2}ms",
                        started.elapsed().as_secs_f64() * MS_PER_SECOND
                    );
                }
            },
        )
        .absolute()
        .size_0()
    }
}

impl Render for Boardy {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let started = Instant::now();
        let theme = ActiveTheme::theme(cx);
        if !self.focused {
            self.focused = true;
            self.focus.focus(window, cx);
        }
        self.begin_draft(window, cx);
        if !self.shown.mounted(started) {
            self.draft = None;
        }
        let body = match &self.place {
            Place::Ticket(_) => ticket::page(self, &theme, cx).into_any_element(),
            Place::CatchUp => views::catch_up(self, &theme, cx).into_any_element(),
            Place::Attention | Place::Board(..) => views::main(self, &theme, cx).into_any_element(),
        };
        let items = self.menu();
        let timed = self.timed(started);
        let drafted = create::modal(self, &theme, window, cx);
        frame::tile(
            titled("boardy", None),
            &theme,
            div()
                .id("boardy")
                .track_focus(&self.focus)
                .on_key_down(cx.listener(Self::key))
                .on_mouse_down(
                    MouseButton::Left,
                    cx.listener(|boardy, _: &MouseDownEvent, window, cx| {
                        if !boardy.focus.contains_focused(window, cx) {
                            boardy.focus.focus(window, cx);
                        }
                    }),
                )
                .on_scroll_wheel(cx.listener(|boardy, _: &ScrollWheelEvent, _, _| {
                    boardy.wheel = Some(Instant::now());
                }))
                .relative()
                .size_full()
                .flex()
                .min_h_0()
                .child(sidebar::sidebar(self, cx))
                .child(
                    context_menu(items)
                        .id("boardy-move")
                        .open_at(self.menu_at.take())
                        .on_pick(cx.listener(|boardy, at: &usize, _, cx| boardy.picked(*at, cx)))
                        .flex_1()
                        .min_w_0()
                        .h_full()
                        .flex()
                        .flex_col()
                        .child(body),
                )
                .children(drafted)
                .children(self.toast.clone().map(|said| {
                    div()
                        .absolute()
                        .right(px(TOAST_BOTTOM))
                        .bottom(px(TOAST_BOTTOM))
                        .child(toast(
                            said,
                            None,
                            &theme,
                            cx.listener(|boardy, _: &ClickEvent, _, cx| {
                                boardy.toast = None;
                                cx.notify();
                            }),
                        ))
                }))
                .child(timed),
        )
    }
}
