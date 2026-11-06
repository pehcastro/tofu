mod cards;
pub(crate) mod cassette;
mod feed;
mod head;
mod items;
mod mention;
mod mirror;

use std::collections::{BTreeMap, VecDeque};
use std::io::ErrorKind;
use std::ops::Range;
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::rc::Rc;
use std::time::{Duration, Instant};
use std::{env, fs, iter, slice, thread};

use desk_core::bridge::{Bridge, BridgeError, Event, Exit, serve_command};
use desk_core::model::{ModelError, PersonAsk, PersonKind, Role, Statuses, Store};
use desk_core::protocol::turn::{PersonQuestion, PersonReply};
use desk_core::protocol::{
    Accounts, ApprovalAnswer, ApprovalRequest, ApprovalRequestDecisions, ApprovalRequestRisk,
    ApprovalRevokeParams, AttachedImage, ContextReport, CredentialReport, CronCommandParams,
    Decision, DecisionMade, InitializeResult, LedgerParams, ModelPick, ModelsQuery, NoParams,
    Notification, PROTOCOL, QuestionAnswer, QuestionOutcome, Request, RequestId, SessionAsking,
    SessionForked, SessionHistory, SessionHistoryParams, SessionInfo, SessionListParams,
    SessionOpenParams, SessionOpenResult, SessionParams, SessionRenameParams, SessionSetParams,
    SessionState, SessionTrace, ShellParams, StatusListParams, TurnCompleted, TurnParams,
    TurnSendParams, request, subagent,
};
use desk_core::query::{Answer, FilledEmails, QueryError, Read, session_ledger};
use desk_core::sessions::SessionRow;
use desk_ui::components::ask::{
    Act, Answer as Offer, Ask, Asking, Choice, Pick, Question, Risk, Shape, ask_bar,
};
use desk_ui::components::chat::{FIND_RESERVE, Hit, fail, find_hits};
use desk_ui::components::composer::{Composer, inline_picker, paperclip};
use desk_ui::components::find::FindBar;
use desk_ui::components::form::TextArea;
use desk_ui::components::logo::Brand;
use desk_ui::components::overlay::{Align, MenuButton, MenuIcon, MenuItem, Placement, Side};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::components::tree::EditedFile;
use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Entity, EventEmitter, FocusHandle, Focusable,
    IntoElement, KeyDownEvent, MouseDownEvent, PathPromptOptions, Render, SharedString, Task,
    Window, actions, div, prelude::*, px,
};

use cassette::{Replay, Step};
use feed::{Feed, Seat};
use head::SessionHead;
use items::{Entry, Item};
use mention::{List, Mention, Menu, Picture};
pub use mention::{Trace, mention};
pub use mirror::{ChatMirror, mirror};

const CLIENT: &str = "tofu-desk";
const TOFU_VARIABLE: &str = "DESK_TOFU";
const EMAILS_FILE: &str = "emails.json";
const SIGN_IN_POLL: Duration = Duration::from_millis(500);
const ANCHOR_HOLD: Duration = Duration::from_millis(1600);
#[cfg(windows)]
const CREATE_NO_WINDOW: u32 = 0x0800_0000;
const MODEL: &str = "Model";
const ATTACH: &str = "Attach a file";
const PLACEHOLDER: &str = "Ask tofu to build, inspect, or delegate";
const FIELD_LINES: usize = 8;
const ROW_INSET: f32 = 24.0;
const CONTENT_WIDTH: f32 = 672.0;
const COLUMN_TOP: f32 = 16.0;
const DOCK_GAP: f32 = 8.0;
const DOCK_INSET: f32 = 16.0;
const OTHER: &str = "Type your own answer";
const PAGE_LINES: i64 = 240;
const KEPT_PAGES_AROUND: usize = 2;
const STATUS_LEDGER_LAST: i64 = 150;
const WORKING: &str = "working";
const YES_NO: [&str; 2] = ["yes", "no"];
const HOUR_SECONDS: i64 = 3600;

type Opened = Result<(Bridge, InitializeResult), BridgeError>;

actions!(desk, [Find]);

#[derive(Clone, Debug, PartialEq, gpui::Action)]
#[action(namespace = chat, no_json)]
pub struct RevokeGrant {
    pub target: SharedString,
}

fn offer(decision: &ApprovalRequestDecisions) -> Option<Offer> {
    match decision {
        ApprovalRequestDecisions::AllowOnce => Some(Offer::Allow),
        ApprovalRequestDecisions::RejectOnce => Some(Offer::Deny),
        ApprovalRequestDecisions::AllowFor => Some(Offer::Hour),
        ApprovalRequestDecisions::AllowAlways => Some(Offer::Always),
        ApprovalRequestDecisions::RejectAlways
        | ApprovalRequestDecisions::Cancelled
        | ApprovalRequestDecisions::RememberProject
        | ApprovalRequestDecisions::RememberGlobal
        | ApprovalRequestDecisions::RememberUserLocal
        | ApprovalRequestDecisions::RememberProjectLocal
        | ApprovalRequestDecisions::Unknown(_) => None,
    }
}

fn decided(offer: Offer) -> ApprovalAnswer {
    let (decision, for_seconds) = match offer {
        Offer::Allow => (Decision::AllowOnce, None),
        Offer::Deny => (Decision::RejectOnce, None),
        Offer::Hour => (Decision::AllowFor, Some(HOUR_SECONDS)),
        Offer::Always => (Decision::AllowAlways, None),
    };
    ApprovalAnswer {
        decision,
        for_seconds,
    }
}

fn risk(risk: &ApprovalRequestRisk) -> Risk {
    match risk {
        ApprovalRequestRisk::Low => Risk::Low,
        ApprovalRequestRisk::Risky => Risk::Risky,
        ApprovalRequestRisk::Destructive => Risk::Destructive,
        ApprovalRequestRisk::Unjudged => Risk::Unjudged,
        ApprovalRequestRisk::Unknown(said) => Risk::Other(said.clone().into()),
    }
}

enum Link {
    Starting,
    Ready(Bridge),
    Gone,
    Recorded(Replay),
    Fed,
}

#[derive(Clone, Copy, Default)]
pub struct Decisions {
    pub count: usize,
    pub capped: bool,
}

#[derive(Default)]
struct Ledger {
    shown: Decisions,
    made: Vec<DecisionMade>,
}

impl std::fmt::Display for Decisions {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let more = if self.capped { "+" } else { "" };
        write!(f, "{}{more}", self.count)
    }
}

enum Opening {
    Closed,
    Pending,
    Open(String),
}

struct Slot {
    item: Option<Item>,
    settled: bool,
}

enum Asked {
    Approval,
    Question(Box<PersonAsk>),
}

struct Page {
    session: String,
    from: Option<String>,
    first: i64,
    end: i64,
    lines: Vec<Notification>,
    rows: usize,
}

impl Page {
    fn decode(
        page: SessionHistory,
        home: &str,
        (before, from): (Option<i64>, Option<i64>),
    ) -> (Page, i64) {
        let end = before.map_or(page.total, |before| before.max(0).min(page.total));
        let first = from
            .map_or(page.first, |from| from.max(page.first))
            .min(end);
        let skip = usize::try_from(first.saturating_sub(page.first)).unwrap_or(0);
        let from = page
            .lines
            .iter()
            .skip(skip)
            .find_map(|line| line.params.get("at")?.as_str().map(str::to_owned));
        let total = page.total;
        let lines = page
            .lines
            .into_iter()
            .skip(skip)
            .filter_map(|mut line| {
                if let Some(said) = line
                    .params
                    .get_mut("session")
                    .filter(|said| said.is_string())
                {
                    *said = home.into();
                }
                match Notification::parse(&line.method, line.params) {
                    Ok(notification) => Some(notification.unsequenced()),
                    Err(error) => {
                        eprintln!("desk: session.history {}: {error}", line.method);
                        None
                    }
                }
            })
            .collect();
        let page = Page {
            session: page.session,
            from,
            first,
            end,
            lines,
            rows: 0,
        };
        (page, total)
    }
}

#[derive(Clone, Copy, Debug)]
enum Paging {
    Older,
    Newer,
    Drop { older: usize, newer: usize },
}

#[derive(Clone, Copy)]
enum Reach {
    Chat,
    Shared,
}

enum Older {
    Page { session: String, before: i64 },
    Parent { session: String },
    Unknown { of: String },
    Root,
}

struct History {
    session: String,
    asked: Instant,
    pages: VecDeque<Page>,
    live: Vec<Notification>,
    total: i64,
    tail: bool,
    asking: bool,
    behind: bool,
    open_at_join: Vec<String>,
    parents: BTreeMap<String, Option<String>>,
}

impl History {
    fn new(session: String, asked: Instant) -> Self {
        History {
            session,
            asked,
            pages: VecDeque::new(),
            live: Vec::new(),
            total: 0,
            tail: false,
            asking: false,
            behind: false,
            open_at_join: Vec::new(),
            parents: BTreeMap::new(),
        }
    }

    fn older(&self) -> Older {
        let Some(front) = self.pages.front() else {
            return Older::Root;
        };
        if front.first > 0 {
            return Older::Page {
                session: front.session.clone(),
                before: front.first,
            };
        }
        match self.parents.get(&front.session) {
            Some(Some(parent)) => Older::Parent {
                session: parent.clone(),
            },
            Some(None) => Older::Root,
            None => Older::Unknown {
                of: front.session.clone(),
            },
        }
    }

    fn droppable_newer(&self, newer: usize) -> usize {
        let after_parents = self
            .pages
            .iter()
            .rposition(|page| page.session != self.session)
            .map_or(0, |at| at.saturating_add(2));
        let kept = self
            .pages
            .len()
            .saturating_sub(newer)
            .max(after_parents)
            .min(self.pages.len());
        self.pages.len().saturating_sub(kept)
    }

    fn continues_front(&self, page: &Page) -> bool {
        self.pages.front().is_some_and(|front| {
            (page.session == front.session && page.end == front.first)
                || (front.first == 0
                    && self.parents.get(&front.session) == Some(&Some(page.session.clone())))
        })
    }

    fn prompt_shown_by_history(&self, entry: &Entry) -> bool {
        matches!(entry, Entry::Task(turn) if self.open_at_join.contains(turn))
    }

    fn settle_live(&mut self, landed: &[Notification]) {
        let Some(cut) = self.live.iter().rposition(|line| landed.contains(line)) else {
            return;
        };
        let mut at = 0;
        self.live.retain(|line| {
            let covered = at <= cut
                && (landed.contains(line) || matches!(line, Notification::MessageDelta(_)));
            at += 1;
            !covered
        });
    }

    fn held(&self) -> Range<i64> {
        match (self.pages.front(), self.pages.back()) {
            (Some(oldest), Some(newest)) => oldest.first..newest.end,
            _ => 0..0,
        }
    }

    fn lines(&self) -> usize {
        self.pages.iter().map(|page| page.lines.len()).sum()
    }

    fn wanted(&self, visible: Range<usize>) -> Option<Paging> {
        let last = self.pages.len().checked_sub(1)?;
        if self.asking || visible.is_empty() {
            return None;
        }
        let page_of = |row: usize| {
            self.pages
                .iter()
                .position(|page| row < page.rows)
                .unwrap_or(last)
        };
        let top = page_of(visible.start);
        let bottom = page_of(visible.end.saturating_sub(1)).max(top);
        let older = top.saturating_sub(KEPT_PAGES_AROUND);
        let newer =
            self.droppable_newer(last.saturating_sub(bottom.saturating_add(KEPT_PAGES_AROUND)));
        if older > 0 || newer > 0 {
            return Some(Paging::Drop { older, newer });
        }
        let oldest_rows = self
            .pages
            .iter()
            .map(|page| page.rows)
            .find(|rows| *rows > 0)
            .unwrap_or(0);
        if visible.start < oldest_rows.max(1) && !matches!(self.older(), Older::Root) {
            return Some(Paging::Older);
        }
        (bottom == last && !self.tail).then_some(Paging::Newer)
    }
}

struct Card {
    key: String,
    id: RequestId,
    asking: Asking,
    asked: Asked,
}

pub struct Chat {
    link: Link,
    store: Entity<Store>,
    orders: BTreeMap<String, Vec<Entry>>,
    awaiting_delta: BTreeMap<String, Instant>,
    slots: Vec<Slot>,
    parsed: items::Parsed,
    transcript: Store,
    opening: Opening,
    items: Rc<Vec<Item>>,
    feed: Entity<Feed>,
    area: Entity<TextArea>,
    card: Option<Card>,
    own_answer: Entity<TextArea>,
    asking: SessionAsking,
    project: String,
    problem: Option<SharedString>,
    focus: FocusHandle,
    refocus: bool,
    find: Entity<FindBar>,
    query: String,
    hits: Rc<Vec<Hit>>,
    current: usize,
    finding: bool,
    aimed: bool,
    rows: Vec<SessionRow>,
    accounts: Answer<Accounts>,
    told_accounts: Option<Accounts>,
    pick: ModelPick,
    defaults: Vec<String>,
    choices: Vec<Option<(String, String)>>,
    models: Entity<MenuButton>,
    labelled: String,
    ledger: Option<(String, Option<Ledger>)>,
    emails: Result<FilledEmails, String>,
    signing: Option<SigningIn>,
    mentions: Vec<Mention>,
    pictures: Vec<Picture>,
    menu: Option<Menu>,
    dismissed: Option<usize>,
    files: List<String>,
    commands: List<desk_core::protocol::command::Command>,
    _files: Option<Task<()>>,
    _signed: Option<Task<()>>,
    reopening: Option<Instant>,
    history: Option<History>,
    heard: Option<i64>,
    painting: Option<(String, Instant)>,
    landed: Option<Instant>,
    head: Entity<SessionHead>,
    _drain: Option<Task<()>>,
    _flash: Option<Task<()>>,
}

struct SigningIn(Child);

impl Drop for SigningIn {
    fn drop(&mut self) {
        if let Err(error) = self.0.kill() {
            eprintln!("desk: sign in pid {} was not stopped: {error}", self.0.id());
        }
    }
}

pub struct Listed;

impl EventEmitter<Listed> for Chat {}

pub struct Touched;

impl EventEmitter<Touched> for Chat {}

pub struct SettingsMoved;

impl EventEmitter<SettingsMoved> for Chat {}

pub struct BoardyMoved(pub String);

impl EventEmitter<BoardyMoved> for Chat {}

#[derive(Clone)]
pub struct TofuStopped {
    pub exit: Exit,
    pub session: Option<String>,
}

impl EventEmitter<TofuStopped> for Chat {}

pub enum Spliced {
    Rows {
        old: Range<usize>,
        count: usize,
        after: usize,
    },
    Cleared,
}

impl EventEmitter<Spliced> for Chat {}

pub fn open(board: Option<&str>, window: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    let store = cx.new(|_| Store::default());
    match board {
        None => Ok(live(crate::project::launch()?, store, window, cx).into()),
        Some("36-agents") => {
            let replay = Replay::read()?;
            Ok(cx
                .new(|cx| Chat::new(Link::Recorded(replay), store, window, cx))
                .into())
        }
        Some(other) => Err(format!(
            "the chat runs a live session, or replays 36-agents, not {other}"
        )),
    }
}

pub fn fed(store: Entity<Store>, window: &mut Window, cx: &mut App) -> Entity<Chat> {
    let chat = cx.new(|cx| Chat::new(Link::Fed, store, window, cx));
    mention::aim(&chat, cx);
    chat
}

fn emails_at() -> Result<PathBuf, String> {
    desk_core::home::Home::find()
        .map(|home| home.state.join(EMAILS_FILE))
        .map_err(|error| error.to_string())
}

fn load_emails() -> Result<FilledEmails, String> {
    let path = emails_at()?;
    let text = match fs::read_to_string(&path) {
        Ok(text) => text,
        Err(error) if error.kind() == ErrorKind::NotFound => return Ok(FilledEmails::default()),
        Err(error) => return Err(format!("{}: {error}", path.display())),
    };
    let emails: FilledEmails = serde_json::from_str(&text)
        .map_err(|error| format!("{} is not filled emails: {error}", path.display()))?;
    let bad = emails.not_emails();
    if !bad.is_empty() {
        return Err(format!("{} holds {bad:?}, not emails", path.display()));
    }
    Ok(emails)
}

fn tofu() -> PathBuf {
    env::var_os(TOFU_VARIABLE).map_or_else(|| PathBuf::from("tofu"), PathBuf::from)
}

pub fn live(
    project: PathBuf,
    store: Entity<Store>,
    window: &mut Window,
    cx: &mut App,
) -> Entity<Chat> {
    let chat = cx.new(|cx| {
        let mut chat = Chat::new(Link::Starting, store, window, cx);
        chat.connect(project, None, cx);
        chat
    });
    mention::aim(&chat, cx);
    chat
}

fn person_question(asked: &PersonQuestion, kind: PersonKind) -> Question {
    let labels = match kind {
        PersonKind::YesNo => YES_NO
            .map(|label| (label.to_owned(), String::new()))
            .to_vec(),
        PersonKind::Choice | PersonKind::Multi | PersonKind::Text => asked
            .options
            .iter()
            .map(|option| (option.label.clone(), option.description.clone()))
            .collect(),
    };
    let pick = |many, other: Option<&str>| {
        Shape::Pick(Pick {
            choices: labels
                .into_iter()
                .map(|(label, about)| Choice {
                    label: label.into(),
                    about: (!about.is_empty()).then(|| about.into()),
                })
                .collect(),
            many,
            other: other.map(SharedString::from),
        })
    };
    Question {
        header: asked.header.clone().into(),
        text: asked.question.clone().into(),
        shape: match kind {
            PersonKind::Choice => pick(false, Some(OTHER)),
            PersonKind::Multi => pick(true, Some(OTHER)),
            PersonKind::YesNo => pick(false, None),
            PersonKind::Text => Shape::Typed(OTHER.into()),
        },
    }
}

fn person_reply(
    asked: &PersonQuestion,
    kind: PersonKind,
    picked: &[usize],
    typed: &str,
) -> PersonReply {
    let typed = (!typed.is_empty()).then(|| typed.to_owned());
    let (chosen, text) = match kind {
        PersonKind::YesNo => (
            Vec::new(),
            picked
                .first()
                .and_then(|at| YES_NO.get(*at))
                .map(|said| (*said).to_owned()),
        ),
        PersonKind::Choice | PersonKind::Multi | PersonKind::Text => (
            picked
                .iter()
                .filter_map(|at| asked.options.get(*at))
                .map(|option| option.label.clone())
                .collect(),
            typed,
        ),
    };
    PersonReply {
        id: asked.id.clone(),
        chosen,
        text,
    }
}

impl Card {
    fn question(ask: &PersonAsk) -> Self {
        Card {
            key: ask.request.question.clone(),
            id: ask.id.clone(),
            asking: Asking::new(
                ask.request
                    .questions
                    .iter()
                    .zip(&ask.kinds)
                    .map(|(asked, kind)| person_question(asked, *kind))
                    .collect(),
            ),
            asked: Asked::Question(Box::new(ask.clone())),
        }
    }

    fn approval(id: RequestId, asked: &ApprovalRequest, project: &str) -> Self {
        let hint = asked
            .judged
            .as_ref()
            .and_then(|judged| judged.reason.as_ref())
            .map_or_else(String::new, |reason| reason.question.clone());
        let command = asked
            .args
            .get("command")
            .and_then(|command| command.as_str())
            .unwrap_or(&asked.target);
        let offered: Vec<Offer> = asked.decisions.iter().filter_map(offer).collect();
        let answers = Offer::ALL
            .into_iter()
            .filter(|answer| offered.is_empty() || offered.contains(answer))
            .collect();
        Card {
            key: asked.approval.clone(),
            id,
            asked: Asked::Approval,
            asking: Asking::new(vec![Question {
                header: SharedString::default(),
                text: asked.reason.clone().unwrap_or_default().into(),
                shape: Shape::Approval(Ask {
                    tool: asked.tool.clone().into(),
                    command: command.to_owned().into(),
                    writes: project.to_owned().into(),
                    hint: hint.into(),
                    risk: risk(&asked.risk),
                    answers,
                }),
            }]),
        }
    }
}

impl Chat {
    fn new(link: Link, store: Entity<Store>, window: &mut Window, cx: &mut Context<Self>) -> Self {
        let submit = cx.listener(|chat, text: &str, _, cx| chat.send(text, cx));
        let area = cx.new(|cx| {
            TextArea::new(PLACEHOLDER.into(), window, cx)
                .bare()
                .max_lines(FIELD_LINES)
                .on_submit(submit)
        });
        let reply = cx.listener(|chat, _: &str, window, cx| chat.act(Act::Send, window, cx));
        let own_answer = cx.new(|cx| {
            TextArea::new(OTHER.into(), window, cx)
                .bare()
                .max_lines(FIELD_LINES)
                .on_submit(reply)
        });
        cx.observe_in(&area, window, |chat, _, window, cx| {
            chat.typed(window, cx);
            cx.notify();
        })
        .detach();
        let find = FindBar::new(window, cx);
        let changed = cx.listener(|chat, (query, current): &(String, usize), _, cx| {
            chat.found(query, *current, cx);
        });
        let closed = cx.listener(|chat, _: &(), _, cx| chat.closed(cx));
        let models = MenuButton::new(MODEL.into(), Vec::new(), cx);
        let chosen = cx.weak_entity();
        models.update(cx, |button, _| {
            button.trigger(|_, theme| inline_picker("chat-model", None, MODEL, theme));
            button.placement(Placement {
                side: Side::Top,
                align: Align::End,
                ..Placement::below()
            });
            button.on_pick(move |at, _, cx| {
                if let Err(error) = chosen.update(cx, |chat, cx| chat.choose(*at, cx)) {
                    eprintln!("desk: the model picker outlived the chat: {error}");
                }
            });
        });
        find.update(cx, |bar, _| {
            bar.on_change(move |query, current, window, cx| {
                changed(&(query.to_owned(), current), window, cx);
            });
            bar.on_close(move |window, cx| closed(&(), window, cx));
        });
        let head = cx.new(|cx| SessionHead::new(store.clone(), cx));
        let feed = Feed::new(
            "chat-transcript",
            Seat::Chat,
            cx.weak_entity(),
            (&store, 0),
            cx,
        );
        Chat {
            link,
            store,
            orders: BTreeMap::new(),
            awaiting_delta: BTreeMap::new(),
            slots: Vec::new(),
            parsed: items::Parsed::new(),
            transcript: Store::default(),
            opening: Opening::Closed,
            items: Rc::default(),
            feed,
            area,
            card: None,
            own_answer,
            asking: SessionAsking::Auto,
            project: String::new(),
            problem: None,
            focus: cx.focus_handle(),
            refocus: true,
            find,
            query: String::new(),
            hits: Rc::default(),
            current: 0,
            finding: false,
            aimed: true,
            rows: Vec::new(),
            accounts: Answer::default(),
            told_accounts: None,
            pick: ModelPick::default(),
            defaults: Vec::new(),
            choices: Vec::new(),
            models,
            labelled: MODEL.to_owned(),
            ledger: None,
            emails: load_emails(),
            signing: None,
            mentions: Vec::new(),
            pictures: Vec::new(),
            menu: None,
            dismissed: None,
            files: List::Unlisted,
            commands: List::Unlisted,
            _files: None,
            _signed: None,
            reopening: None,
            history: None,
            heard: None,
            painting: None,
            landed: None,
            head,
            _drain: None,
            _flash: None,
        }
    }

    pub fn store(&self) -> &Entity<Store> {
        &self.store
    }

    fn ask_cron(&mut self, session: String, cx: &mut Context<Self>) {
        self.call::<request::QueryCron>(&NoParams {}, cx, move |chat, state, cx| {
            eprintln!("desk: query.cron live {} goals {}", state.live, state.goals);
            chat.store.update(cx, |store, cx| {
                store.cron_answered(&session, state);
                cx.notify();
            });
        });
    }

    pub fn cron_command(&mut self, line: String, cx: &mut Context<Self>) {
        eprintln!("desk: cron.command {line}");
        let params = CronCommandParams { line };
        self.call::<request::CronCommand>(&params, cx, |_, said, _| {
            eprintln!("desk: cron.command answered: {}", said.note);
        });
    }

    pub fn rows(&self) -> &[SessionRow] {
        &self.rows
    }

    fn set_opening(&mut self, opening: Opening, cx: &mut Context<Self>) {
        let open = match &opening {
            Opening::Open(id) => Some(id.clone()),
            Opening::Closed | Opening::Pending => None,
        };
        let shown = open.is_some();
        self.store.update(cx, |store, cx| {
            store.open = open;
            cx.notify();
        });
        self.opening = opening;
        if shown {
            self.ask_due(cx);
        }
        self.tell_head(cx);
    }

    fn tell_head(&mut self, cx: &mut Context<Self>) {
        self.relabel(cx);
        let loading = self.loading();
        self.head.update(cx, |head, cx| head.loading(loading, cx));
    }

    fn opened(&mut self, opened: SessionOpenResult, asked: Instant, cx: &mut Context<Self>) {
        self.store.update(cx, |store, _| {
            store.trace = Answer::default();
            store.info = Answer::default();
            store.info.want();
            store
                .sessions
                .entry(opened.session.clone())
                .or_default()
                .members
                .clone_from(&opened.members);
        });
        self.painting.get_or_insert((opened.session.clone(), asked));
        if opened.fresh {
            eprintln!("desk: chat open asks cron for fresh {}", opened.session);
            self.ledger = Some((opened.session.clone(), Some(Ledger::default())));
        } else {
            eprintln!(
                "desk: chat open asks query.ledger session {} last {STATUS_LEDGER_LAST}, session.trace, session.history, cron",
                opened.session
            );
            self.ask_ledger(opened.session.clone(), cx);
            self.want_trace(cx);
            if self
                .history
                .as_ref()
                .is_none_or(|history| history.session != opened.session)
            {
                self.history = Some(History::new(opened.session.clone(), asked));
                self.ask_page(Paging::Older, cx);
            }
        }
        self.ask_cron(opened.session.clone(), cx);
        self.set_opening(Opening::Open(opened.session), cx);
    }

    fn page(&mut self, paging: Paging, cx: &mut Context<Self>) {
        let Paging::Drop { older, newer } = paging else {
            return self.ask_page(paging, cx);
        };
        let Some(history) = self.history.as_mut() else {
            return;
        };
        let newer = history.droppable_newer(newer);
        history.asking = false;
        if older == 0 && newer == 0 {
            return;
        }
        let kept = history.pages.len().saturating_sub(newer);
        history.pages.truncate(kept);
        history.pages.drain(..older.min(kept));
        if newer > 0 {
            history.tail = false;
            history.live = Vec::new();
        }
        self.replay(Reach::Chat, cx);
        self.told_pages(&format!("dropped {older} older and {newer} newer"));
    }

    fn ask_page(&mut self, paging: Paging, cx: &mut Context<Self>) {
        let Some(history) = self.history.as_mut() else {
            return;
        };
        let end = history.pages.back().map_or(0, |back| back.end);
        let (target, before, from) = match (history.pages.is_empty(), paging) {
            (true, _) => (history.session.clone(), None, None),
            (false, Paging::Newer) => (
                history.session.clone(),
                Some(end.saturating_add(PAGE_LINES)),
                Some(end),
            ),
            (false, Paging::Older | Paging::Drop { .. }) => match history.older() {
                Older::Page { session, before } => (session, Some(before), None),
                Older::Parent { session } => (session, None, None),
                Older::Unknown { of } => {
                    history.asking = true;
                    return self.ask_parent(of, cx);
                }
                Older::Root => {
                    history.asking = false;
                    return eprintln!("desk: chat history reached the first session of its line");
                }
            },
        };
        history.asking = true;
        let session = history.session.clone();
        let params = SessionHistoryParams {
            session: target.clone(),
            limit: Some(PAGE_LINES),
            before,
        };
        let asked = Instant::now();
        self.request::<request::SessionHistory>(&params, cx, move |chat, page, cx| {
            eprintln!(
                "desk: session.history {target} for {session} page of {PAGE_LINES} before {} answered in {} ms",
                before.map_or_else(|| "the end".to_owned(), |at| at.to_string()),
                asked.elapsed().as_millis()
            );
            let page = match page {
                Ok(page) => page,
                Err(error) => {
                    if chat.history.as_ref().is_some_and(|h| h.session == session) {
                        chat.history = None;
                        chat.tell_head(cx);
                        chat.fail(format!("session.history {session}: {error}"), cx);
                    }
                    return;
                }
            };
            let home = session.clone();
            let decoded =
                cx.background_spawn(async move { Page::decode(page, &home, (before, from)) });
            cx.spawn(async move |chat, cx| {
                let landed = decoded.await;
                if let Err(error) = chat.update(cx, |chat, cx| chat.paged(&session, landed, cx)) {
                    eprintln!("desk: a history page came back to a closed chat: {error}");
                }
            })
            .detach();
        });
    }

    fn ask_parent(&mut self, of: String, cx: &mut Context<Self>) {
        let params = SessionParams {
            session: Some(of.clone()),
        };
        self.request::<request::SessionInfo>(&params, cx, move |chat, info, cx| {
            let Some(history) = chat.history.as_mut() else {
                return;
            };
            history.asking = false;
            let parent = match info {
                Ok(info) => info
                    .parent
                    .or_else(|| info.carried_from.map(|carried| carried.session))
                    .filter(|parent| *parent != of),
                Err(error) => {
                    return eprintln!("desk: session.info {of} for its parent failed: {error}");
                }
            };
            eprintln!(
                "desk: chat history of {of} continues from {}",
                parent
                    .as_deref()
                    .unwrap_or("nothing, it is the first session")
            );
            history.parents.insert(of, parent);
            chat.ask_page(Paging::Older, cx);
        });
    }

    fn paged(&mut self, session: &str, (page, total): (Page, i64), cx: &mut Context<Self>) {
        let Some(history) = self.history.as_mut().filter(|h| h.session == session) else {
            return;
        };
        history.asking = false;
        let held = history.held();
        let first = history.pages.is_empty();
        let newest = first
            || (page.session == history.session
                && history
                    .pages
                    .back()
                    .is_some_and(|back| back.session == page.session && back.end == page.first));
        let older = !newest && history.continues_front(&page);
        eprintln!(
            "desk: chat page {} from {}",
            page.session,
            page.from.as_deref().unwrap_or("no timestamp")
        );
        let caught_up = !first && newest && history.tail;
        if page.session == history.session {
            history.total = total;
        }
        if newest {
            history.tail = page.end >= total;
        }
        let reached = newest && history.tail;
        if page.first == page.end && newest && !first {
            history.tail |= page.end >= total;
            history.behind &= !history.tail;
            return self.told_pages("nothing newer");
        }
        let (mut calls, mut carded, mut grouped, mut summed) = (0, 0, 0, 0);
        for line in &page.lines {
            match line {
                Notification::ToolStarted(e) if e.agent.is_none() => {
                    calls += 1;
                    carded += usize::from(e.card.is_some());
                    grouped += usize::from(e.group.is_some());
                }
                Notification::ToolCompleted(e) if e.agent.is_none() => {
                    summed += usize::from(e.group_summary.is_some());
                }
                _ => {}
            }
        }
        eprintln!(
            "desk: session.history {session} page lead calls {calls}, with card {carded}, with group {grouped}, completed with groupSummary {summed}"
        );
        match (newest, older) {
            (true, _) => {
                history.settle_live(&page.lines);
                match history.pages.back_mut().filter(|_| caught_up) {
                    Some(back) => {
                        back.end = page.end;
                        back.lines.extend(page.lines);
                    }
                    None => history.pages.push_back(page),
                }
            }
            (false, true) => history.pages.push_front(page),
            (false, false) => {
                return eprintln!(
                    "desk: session.history page {} {}..{} does not touch the held {}..{}, dropped",
                    page.session, page.first, page.end, held.start, held.end
                );
            }
        }
        let replaying = Instant::now();
        let rows = self.items.len();
        let refused = self.replay(if older { Reach::Chat } else { Reach::Shared }, cx);
        if older && self.items.len() == rows {
            eprintln!("desk: chat page added no rows, the next older page is asked at once");
            self.ask_page(Paging::Older, cx);
        }
        let (stretches, calls, worked) = items::shapes(&self.items);
        eprintln!(
            "desk: paint replayed {} lines into {} rows in {} ms, {stretches} stretch rows, {calls} single call rows, {worked} worked rows, {refused} refused",
            self.history.as_ref().map_or(0, History::lines),
            self.items.len(),
            replaying.elapsed().as_millis()
        );
        self.told_pages("page landed");
        if first {
            if let Some(history) = &self.history {
                eprintln!(
                    "desk: session {session} first history page landed in {} ms, pages of {PAGE_LINES}",
                    history.asked.elapsed().as_millis()
                );
            }
            self.landed = Some(Instant::now());
            self.ask_shells(cx);
        }
        if let Some(history) = self.history.as_mut().filter(|history| history.behind) {
            history.behind = !reached;
            if history.behind {
                self.ask_page(Paging::Newer, cx);
            }
        }
    }

    fn catch_up(&mut self, why: &str, cx: &mut Context<Self>) {
        let Some(history) = self.history.as_mut() else {
            return;
        };
        if history.pages.is_empty() {
            return;
        }
        if !history.tail {
            return eprintln!(
                "desk: chat feed gap ({why}) while the transcript is scrolled back, paging reads it"
            );
        }
        eprintln!(
            "desk: chat feed gap ({why}), session.history {} catches up from line {}",
            history.session,
            history.held().end
        );
        history.behind = true;
        if !history.asking {
            self.ask_page(Paging::Newer, cx);
        }
    }

    fn follow_fork(&mut self, fork: &SessionForked, cx: &mut Context<Self>) {
        if fork.agent.is_some() || self.open_id() != Some(fork.from.as_str()) {
            return;
        }
        eprintln!(
            "desk: session {} continues as {} ({}), the chat follows",
            fork.from, fork.to, fork.kind
        );
        self.history = Some(History::new(fork.to.clone(), Instant::now()));
        self.set_opening(Opening::Open(fork.to.clone()), cx);
        self.ask_page(Paging::Older, cx);
        self.want_trace(cx);
        self.reread_info(cx);
    }

    fn told_pages(&self, why: &str) {
        if let Some(history) = &self.history {
            let held = history.held();
            eprintln!(
                "desk: chat history {why}, holds {} pages, lines {}..{} of {}, {} lines, {} rows",
                history.pages.len(),
                held.start,
                held.end,
                history.total,
                history.lines(),
                self.items.len()
            );
        }
    }

    fn replay(&mut self, reach: Reach, cx: &mut Context<Self>) -> usize {
        let Some(history) = &self.history else {
            return 0;
        };
        let id = history.session.clone();
        let newest = history.pages.back().map(|page| &page.lines);
        let live = history
            .live
            .iter()
            .filter(|live| newest.is_none_or(|lines| !lines.contains(live)));
        let mut pages: Vec<Vec<Event>> = history
            .pages
            .iter()
            .map(|page| {
                page.lines
                    .iter()
                    .cloned()
                    .map(Event::Notification)
                    .collect()
            })
            .collect();
        if let Some(newest) = pages.last_mut() {
            newest.extend(live.cloned().map(Event::Notification));
        }
        self.transcript
            .sessions
            .entry(id.clone())
            .or_default()
            .reset_transcript();
        self.orders.remove(&id);
        let mut refused = 0;
        let mut entries = Vec::with_capacity(pages.len());
        for events in &pages {
            refused += events
                .iter()
                .filter(|event| self.absorb(event).is_err())
                .count();
            entries.push(self.orders.get(&id).map_or(0, Vec::len));
        }
        if let (Reach::Shared, Some(newest)) = (reach, pages.last()) {
            self.store.update(cx, |store, cx| {
                store
                    .sessions
                    .entry(id.clone())
                    .or_default()
                    .reset_transcript();
                let refused = newest
                    .iter()
                    .filter(|event| store.apply_batch(slice::from_ref(event)).is_err())
                    .count();
                eprintln!("desk: chat newest page given to the shared store, {refused} refused");
                cx.notify();
            });
        }
        drop(pages);
        self.rebuild(cx);
        if let Some(history) = self.history.as_mut() {
            for (page, entries) in history.pages.iter_mut().zip(entries) {
                page.rows = self
                    .slots
                    .iter()
                    .take(entries)
                    .filter(|slot| slot.item.is_some())
                    .count();
            }
        }
        refused
    }

    fn rebuild(&mut self, cx: &mut Context<Self>) {
        let shown = std::mem::take(&mut self.items);
        self.parsed = items::parsed_of(&shown);
        self.slots.clear();
        self.restack();
        self.parsed.clear();
        let head = shown
            .iter()
            .zip(self.items.iter())
            .take_while(|(was, now)| was == now)
            .count();
        let tail = shown
            .iter()
            .skip(head)
            .rev()
            .zip(self.items.iter().skip(head).rev())
            .take_while(|(was, now)| was == now)
            .count();
        let (old, count) = (head..shown.len() - tail, self.items.len() - head - tail);
        self.feed
            .update(cx, |feed, cx| feed.splice(old.clone(), count, cx));
        cx.emit(Spliced::Rows {
            old,
            count,
            after: self.items.len(),
        });
        self.refresh(cx);
        self.tell_head(cx);
    }

    fn loading(&self) -> Option<String> {
        match (&self.history, &self.opening) {
            (Some(history), _) if history.pages.is_empty() => Some("loading history".to_owned()),
            (_, Opening::Pending) => Some("opening session".to_owned()),
            _ => None,
        }
    }

    fn want_trace(&mut self, cx: &mut Context<Self>) {
        self.store.update(cx, |store, _| {
            store.trace.want();
            store.trace.again();
        });
    }

    pub fn head(&self) -> AnyView {
        self.head.clone().into()
    }

    pub fn open_id(&self) -> Option<&str> {
        match &self.opening {
            Opening::Open(id) => Some(id),
            Opening::Closed | Opening::Pending => None,
        }
    }

    pub fn asked_id(&self) -> Option<&str> {
        match &self.opening {
            Opening::Open(id) => Some(id),
            Opening::Pending => self
                .history
                .as_ref()
                .map(|history| history.session.as_str()),
            Opening::Closed => None,
        }
    }

    pub fn project(&self) -> &str {
        &self.project
    }

    pub fn busy(&self, cx: &App) -> bool {
        self.running(cx).is_some()
    }

    fn relist(&mut self, cx: &mut Context<Self>) {
        self.call::<request::SessionList>(&SessionListParams::default(), cx, |chat, listed, cx| {
            eprintln!("desk: session list {} rows", listed.sessions.len());
            chat.rows = listed.sessions;
            cx.emit(Listed);
        });
    }

    pub fn open_session(&mut self, session: Option<String>, cx: &mut Context<Self>) {
        if session.is_some() && session.as_deref() == self.open_id() {
            return;
        }
        match self.running(cx) {
            Some((busy, _)) => self.step_aside(&busy, cx),
            None => self.restart(cx),
        }
        self.problem = None;
        if let Some(row) = self
            .rows
            .iter()
            .find(|row| Some(&row.id) == session.as_ref())
        {
            let row = row.clone();
            self.head.update(cx, |head, cx| head.show(row, cx));
        }
        self.set_opening(Opening::Pending, cx);
        let asked = Instant::now();
        self.painting = session.clone().map(|id| (id, asked));
        self.history = session.clone().map(|id| History::new(id, asked));
        self.tell_head(cx);
        self.ask_page(Paging::Older, cx);
        let wanted = session.clone().unwrap_or_default();
        eprintln!("desk: session.open asked for {wanted}");
        let params = SessionOpenParams {
            asking: Some(self.asking.clone()),
            session,
            replay: Some(0),
            ..SessionOpenParams::default()
        };
        self.request_typed::<request::SessionOpen>(&params, cx, move |chat, reply, cx| {
            let opened = match reply {
                Ok(opened) => opened,
                Err(error) if error.turn_running() => {
                    return eprintln!("desk: session.open waits, a turn is running: {error}");
                }
                Err(error) => {
                    eprintln!("desk: session.open {wanted} failed: {error:?}");
                    return chat.fail(error.to_string(), cx);
                }
            };
            eprintln!(
                "desk: session attach {} with {} members, fresh {}, in {} ms",
                opened.session,
                opened.members.len(),
                opened.fresh,
                asked.elapsed().as_millis()
            );
            if let Some(at) = chat.reopening.take() {
                eprintln!(
                    "desk: tofu restarted, session {} back in {} ms",
                    opened.session,
                    at.elapsed().as_millis()
                );
            }
            chat.ask_shells(cx);
            chat.opened(opened, asked, cx);
            chat.refresh(cx);
            chat.relist(cx);
            chat.reread_context(cx);
            chat.reread_info(cx);
        });
        cx.notify();
    }

    pub fn problem(&self) -> Option<&SharedString> {
        self.problem.as_ref()
    }

    pub fn rename(&mut self, session: String, name: String, cx: &mut Context<Self>) {
        let params = SessionRenameParams { session, name };
        self.call::<request::SessionRename>(&params, cx, |_, _, _| {});
    }

    fn revoke(&mut self, grant: &RevokeGrant, _: &mut Window, cx: &mut Context<Self>) {
        let target = grant.target.to_string();
        eprintln!("desk: approval.revoke asked {target}");
        let params = ApprovalRevokeParams {
            target: target.clone(),
        };
        self.request::<request::ApprovalRevoke>(&params, cx, move |chat, reply, cx| match reply {
            Ok(standing) => eprintln!("desk: approval.revoke {} answered", standing.target),
            Err(error) => chat.fail(format!("approval.revoke {target}: {error}"), cx),
        });
    }

    fn open_find(&mut self, _: &Find, window: &mut Window, cx: &mut Context<Self>) {
        if !self.aimed {
            cx.propagate();
            return;
        }
        eprintln!("desk: find chat open");
        self.finding = true;
        self.find.update(cx, |bar, cx| bar.open(window, cx));
    }

    fn found(&mut self, query: &str, current: usize, cx: &mut Context<Self>) {
        query.clone_into(&mut self.query);
        self.current = current;
        self.search(cx);
        match (query.is_empty(), self.hits.get(self.current)) {
            (true, _) => eprintln!("desk: find chat cleared"),
            (false, None) => eprintln!("desk: find chat {query} no matches"),
            (false, Some(hit)) => {
                let item = hit.item;
                self.feed.update(cx, |feed, cx| feed.reveal(item, cx));
                eprintln!(
                    "desk: find chat {query} {} of {}",
                    self.current + 1,
                    self.hits.len()
                );
            }
        }
        cx.notify();
    }

    pub fn search_hits(&self, query: &str) -> Vec<(String, Range<usize>)> {
        let pieces: Vec<Vec<String>> = self.items.iter().map(items::pieces).collect();
        find_hits(pieces.iter().cloned(), query)
            .into_iter()
            .filter_map(|hit| Some((pieces.get(hit.item)?.get(hit.piece)?.clone(), hit.range)))
            .collect()
    }

    pub fn anchor(&mut self, query: &str, at: usize, cx: &mut Context<Self>) {
        eprintln!("desk: find chat anchored on hit {} of {query:?}", at + 1);
        self.found(query, at, cx);
        self._flash = Some(cx.spawn(async move |this, cx| {
            cx.background_executor().timer(ANCHOR_HOLD).await;
            if let Err(error) = this.update(cx, |chat, cx| {
                if !chat.finding {
                    chat.closed(cx);
                }
            }) {
                eprintln!("desk: find chat: the anchor outlived the chat: {error}");
            }
        }));
    }

    fn search(&mut self, cx: &mut Context<Self>) {
        self.hits = Rc::new(find_hits(self.items.iter().map(items::pieces), &self.query));
        let total = self.hits.len();
        if self.current >= total {
            self.current = 0;
        }
        self.find.update(cx, |bar, cx| bar.set_total(total, cx));
        self.feed.update(cx, |_, cx| cx.notify());
    }

    fn closed(&mut self, cx: &mut Context<Self>) {
        eprintln!("desk: find closed");
        self.finding = false;
        self.query.clear();
        self.hits = Rc::default();
        self.feed.update(cx, |_, cx| cx.notify());
        cx.notify();
    }

    fn started(
        &mut self,
        opened: Opened,
        cx: &mut Context<Self>,
    ) -> Option<flume::Receiver<Event>> {
        let (bridge, hello) = match opened {
            Ok(opened) => opened,
            Err(error) => {
                self.link = Link::Gone;
                self.fail(error.to_string(), cx);
                return None;
            }
        };
        eprintln!(
            "desk: tofu {} pid {} speaks {} in {}",
            hello.tofu,
            bridge.pid(),
            hello.protocol,
            hello.project
        );
        let events = bridge.events().clone();
        self.project = hello.project;
        self.link = Link::Ready(bridge);
        self.heard = None;
        self.relist(cx);
        self.ask_models(cx);
        self.ask_due(cx);
        cx.emit(SettingsMoved);
        cx.notify();
        Some(events)
    }

    fn connect(&mut self, project: PathBuf, reopen: Option<String>, cx: &mut Context<Self>) {
        let (sender, opened) = flume::bounded::<Opened>(1);
        thread::spawn(move || {
            let bridge = Bridge::open(serve_command(&tofu(), &project), CLIENT, PROTOCOL);
            if let Err(flume::SendError(Ok((unwanted, _)))) = sender.send(bridge)
                && let Err(error) = unwanted.stop()
            {
                eprintln!("desk: an unwanted tofu did not stop: {error}");
            }
        });
        self.link = Link::Starting;
        self._drain = Some(cx.spawn(async move |this, cx| {
            let Ok(opened) = opened.recv_async().await else {
                return;
            };
            let events = this
                .update(cx, move |chat, cx| {
                    let events = chat.started(opened, cx)?;
                    if let Some(session) = reopen {
                        chat.restart(cx);
                        chat.open_session(Some(session), cx);
                    }
                    Some(events)
                })
                .ok()
                .flatten();
            if let Some(events) = events {
                drain(this, events, cx).await;
            }
        }));
    }

    pub fn drop_tofu(&self) -> Result<(), String> {
        let Link::Ready(bridge) = &self.link else {
            return Err("tofu is not connected".to_owned());
        };
        eprintln!("desk: drop-tofu: killing tofu serve pid {}", bridge.pid());
        bridge.crash().map_err(|error| error.to_string())
    }

    #[cfg(all(feature = "screen-intro", feature = "screen-work"))]
    pub fn take_tofu(&mut self, from: &mut Chat, cx: &mut Context<Self>) -> bool {
        let Link::Ready(bridge) = std::mem::replace(&mut from.link, Link::Gone) else {
            return false;
        };
        from._drain = None;
        eprintln!(
            "desk: tofu pid {} handed over in {}",
            bridge.pid(),
            from.project
        );
        let events = bridge.events().clone();
        self.project = from.project.clone();
        self.link = Link::Ready(bridge);
        self.heard = from.heard.take();
        self._drain = Some(cx.spawn(async move |this, cx| drain(this, events, cx).await));
        self.relist(cx);
        self.ask_models(cx);
        self.ask_due(cx);
        cx.emit(SettingsMoved);
        cx.notify();
        true
    }

    pub fn reconnect(&mut self, session: Option<String>, cx: &mut Context<Self>) {
        eprintln!(
            "desk: restarting tofu, session {}",
            session.as_deref().unwrap_or("none")
        );
        self.problem = None;
        self.reopening = session.as_ref().map(|_| Instant::now());
        self.connect(PathBuf::from(&self.project), session, cx);
    }

    fn exited(&mut self, exit: &Exit, cx: &mut Context<Self>) {
        self.link = Link::Gone;
        cx.emit(TofuStopped {
            exit: exit.clone(),
            session: self.open_id().map(str::to_owned),
        });
        cx.notify();
    }

    fn fail(&mut self, problem: String, cx: &mut Context<Self>) {
        eprintln!("desk: {problem}");
        self.problem = Some(problem.into());
        cx.notify();
    }

    pub fn take(&mut self, batch: &[Event], cx: &mut Context<Self>) {
        let mut gap = None;
        for event in batch {
            let seq = match event {
                Event::Notification(notification) => notification.seq(),
                Event::Request { request, .. } => request.seq(),
                Event::Unreadable { .. } | Event::Exited(_) => None,
            };
            if let Some(seq) = seq
                && let Some(last) = self.heard.replace(seq)
                && seq > last.saturating_add(1)
            {
                gap = Some(format!("tofu sent line {seq} after {last}"));
            }
            self.apply(event, cx);
        }
        if let Some(gap) = gap {
            self.catch_up(&gap, cx);
        }
        self.store.update(cx, |_, cx| cx.notify());
        if let (Link::Recorded(_) | Link::Fed, Opening::Closed) = (&self.link, &self.opening)
            && let Some(id) = self.store.read(cx).sessions.keys().next().cloned()
        {
            self.want_trace(cx);
            self.set_opening(Opening::Open(id), cx);
        }
        self.refresh(cx);
        self.ask_due(cx);
    }

    fn step_aside(&mut self, busy: &str, cx: &mut Context<Self>) {
        self.store.update(cx, |store, cx| {
            store.sessions.retain(|id, _| id == busy);
            cx.notify();
        });
        self.orders.retain(|id, _| id == busy);
        self.transcript.sessions.retain(|id, _| id == busy);
        self.forget_view(cx);
    }

    pub fn restart(&mut self, cx: &mut Context<Self>) {
        self.store.update(cx, |store, cx| {
            store.sessions.clear();
            store.statuses = Statuses::default();
            cx.notify();
        });
        self.orders.clear();
        self.transcript = Store::default();
        self.forget_view(cx);
    }

    fn forget_view(&mut self, cx: &mut Context<Self>) {
        self.history = None;
        self.set_opening(Opening::Closed, cx);
        self.clear_view(cx);
    }

    fn clear_view(&mut self, cx: &mut Context<Self>) {
        self.slots.clear();
        self.items = Rc::default();
        self.feed.update(cx, |feed, cx| feed.reset(0, cx));
        cx.emit(Spliced::Cleared);
    }

    pub fn focus_composer(&self, window: &mut Window, cx: &mut App) -> bool {
        let field = self.area.focus_handle(cx);
        field.focus(window, cx);
        field.is_focused(window)
    }

    pub fn kill(&mut self, shell: SharedString, cx: &mut Context<Self>) {
        eprintln!("desk: kill {shell}: asking tofu shell.kill");
        let params = ShellParams {
            shell: shell.to_string(),
            offset: None,
        };
        self.call::<request::ShellKill>(&params, cx, move |_, ack, _| {
            eprintln!("desk: shell.kill {shell} answered ok {}", ack.ok);
        });
    }

    fn absorb(&mut self, event: &Event) -> Result<(), ModelError> {
        let granted = match event {
            Event::Notification(Notification::ApprovalResolved(resolved)) => self
                .transcript
                .sessions
                .get(&resolved.session)
                .and_then(|session| items::granted(session, resolved))
                .map(|entry| (resolved.session.clone(), entry)),
            _ => None,
        };
        let before: Vec<(String, usize)> = self
            .transcript
            .sessions
            .iter()
            .map(|(id, session)| (id.clone(), session.messages.len()))
            .collect();
        self.transcript.apply_batch(slice::from_ref(event))?;
        for (id, session) in &self.transcript.sessions {
            let was = before
                .iter()
                .find(|(seen, _)| seen == id)
                .map_or(0, |(_, length)| *length);
            let order = self.orders.entry(id.clone()).or_default();
            order.extend((was..session.messages.len()).map(Entry::Message));
        }
        if let Event::Notification(notification) = event
            && let Some((session, entry)) = items::entry(notification)
            && !self
                .history
                .as_ref()
                .is_some_and(|history| history.prompt_shown_by_history(&entry))
        {
            let order = self.orders.entry(session.to_owned()).or_default();
            match self.transcript.sessions.get(session) {
                Some(found) => items::place(found, order, entry),
                None => order.push(entry),
            }
        }
        if let Some((session, entry)) = granted
            && let Some(found) = self.transcript.sessions.get(&session)
        {
            items::place(found, self.orders.entry(session).or_default(), entry);
        }
        if let Event::Notification(Notification::ApprovalRevoked(revoked)) = event
            && let Some(order) = self.orders.get_mut(&revoked.session)
        {
            items::revoke(order, &revoked.target);
        }
        Ok(())
    }

    fn apply(&mut self, event: &Event, cx: &mut Context<Self>) {
        if let (Some(history), Event::Notification(notification)) = (&mut self.history, event) {
            if let Notification::TurnStarted(started) = notification
                && history.pages.is_empty()
                && history.session == started.session
            {
                eprintln!(
                    "desk: turn {} was open before this desk joined, its prompt draws from history only",
                    started.turn
                );
                history.open_at_join.push(started.turn.clone());
            }
            if history.tail || history.pages.is_empty() {
                history.live.push(notification.unsequenced());
            }
        }
        let shared = self
            .store
            .update(cx, |store, _| store.apply_batch(slice::from_ref(event)));
        match shared.and(self.absorb(event)) {
            Ok(()) => {}
            Err(error) if matches!(event, Event::Notification(Notification::Status(_))) => {
                return self.fail(
                    format!("tofu sent a status the desk cannot read: {error}"),
                    cx,
                );
            }
            Err(error) => eprintln!("desk: the store refused an event from tofu: {error}"),
        }
        self.react(event, cx);
    }

    fn react(&mut self, event: &Event, cx: &mut Context<Self>) {
        match event {
            Event::Notification(Notification::TurnStarted(started)) => {
                eprintln!("desk: turn {} started", started.turn);
                self.awaiting_delta
                    .insert(started.turn.clone(), Instant::now());
                if let Link::Ready(_) = self.link {
                    self.relist(cx);
                }
            }
            Event::Notification(Notification::SessionUpdated(_)) => {
                if let Link::Ready(_) = self.link {
                    self.relist(cx);
                }
            }
            Event::Notification(Notification::AgentStarted(started)) => {
                eprintln!("desk: tofu sent AgentStarted({started:?})");
            }
            Event::Notification(Notification::TurnCompleted(completed)) => {
                self.said(completed, cx);
                cx.emit(Touched);
                if self.open_id() == Some(completed.session.as_str()) {
                    self.reread_context(cx);
                    self.reread_info(cx);
                    if self.store.read(cx).trace.read.is_none() {
                        self.want_trace(cx);
                    }
                }
                if let Link::Ready(_) = self.link {
                    self.relist(cx);
                }
            }
            Event::Notification(Notification::TurnSteerQueued(queued))
                if queued.agent.is_none() =>
            {
                eprintln!("desk: chat steer queued {}", queued.item);
            }
            Event::Notification(Notification::TurnSteered(read)) if read.agent.is_none() => {
                eprintln!("desk: chat steer read {}", read.item);
            }
            Event::Notification(Notification::ShellStarted(started)) => eprintln!(
                "desk: session {} shell {} started pid {}: {}",
                started.session, started.shell, started.pid, started.command
            ),
            Event::Notification(Notification::ShellExited(exited)) => eprintln!(
                "desk: session {} shell {} exited code {:?} killed {}",
                exited.session, exited.shell, exited.exit_code, exited.killed
            ),
            Event::Notification(Notification::ShellReady(ready)) => eprintln!(
                "desk: session {} shell {} ready port {}",
                ready.session, ready.shell, ready.port
            ),
            Event::Notification(Notification::TurnAccount(account)) => {
                eprintln!(
                    "desk: session {} turn {} account {} {} {}",
                    account.session,
                    account.turn,
                    account.source,
                    account.account_id,
                    account.model
                );
                if self.open_id() == Some(account.session.as_str()) {
                    self.reread_info(cx);
                }
                self.accounts.notified();
                self.ask_accounts(cx);
                self.fill_models(cx);
                cx.emit(SettingsMoved);
            }
            Event::Notification(
                notification @ (Notification::AccountState(_) | Notification::QuotaUpdated(_)),
            ) => {
                self.accounts.notified();
                eprintln!("desk: tofu sent {notification:?}");
            }
            Event::Notification(Notification::FileEdit(edit)) => {
                cx.set_global(EditedFile(PathBuf::from(&edit.path)));
                cx.emit(Touched);
            }
            Event::Notification(Notification::SettingsChanged(changed)) => {
                eprintln!(
                    "desk: settings.changed {:?} {} = {}",
                    changed.scope, changed.key, changed.value
                );
                cx.emit(SettingsMoved);
            }
            Event::Notification(Notification::BoardyChanged(changed)) => {
                eprintln!("desk: boardy.changed {} {:?}", changed.board, changed.paths);
                cx.emit(BoardyMoved(changed.board.clone()));
            }
            Event::Notification(Notification::SessionSettings(settings)) => {
                eprintln!(
                    "desk: session.settings pick wire {} model {}",
                    settings.pick.wire.as_deref().unwrap_or("none"),
                    settings.pick.model.as_deref().unwrap_or("none")
                );
                self.pick = settings.pick.clone();
                if let Some(asking) = &settings.asking {
                    self.asking = asking.clone();
                }
                self.fill_models(cx);
            }
            Event::Notification(
                notification @ (Notification::ContextUpdated(_)
                | Notification::Decision(_)
                | Notification::CronUpdated(_)
                | Notification::AgentEnded(_)
                | Notification::UsageUpdated(_)),
            ) => eprintln!("desk: tofu sent {notification:?}"),
            Event::Notification(Notification::Status(report)) => eprintln!(
                "desk: status {} {}",
                report.item,
                String::from(report.state.clone())
            ),
            Event::Request { request, .. } => eprintln!("desk: tofu asks {request:?}"),
            Event::Notification(Notification::MessageDelta(delta)) => {
                if let Some(at) = self.awaiting_delta.remove(&delta.turn) {
                    eprintln!(
                        "desk: turn {} first delta after {} ms",
                        delta.turn,
                        at.elapsed().as_millis()
                    );
                }
            }
            Event::Notification(Notification::Resync(resync)) => self.catch_up(
                &format!(
                    "tofu dropped {} lines of session {}",
                    resync.dropped, resync.session
                ),
                cx,
            ),
            Event::Notification(Notification::SessionForked(fork)) => self.follow_fork(fork, cx),
            Event::Exited(exit) => self.exited(exit, cx),
            Event::Notification(_) | Event::Unreadable { .. } => {}
        }
    }

    fn said(&self, completed: &TurnCompleted, cx: &App) {
        let answer = self
            .store
            .read(cx)
            .sessions
            .get(&completed.session)
            .and_then(|session| {
                session.messages.iter().rev().find(|message| {
                    message.turn == completed.turn
                        && message.agent.is_none()
                        && message.role == Role::Assistant
                })
            });
        eprintln!(
            "desk: session {} turn {} {:?} answer {:?}",
            completed.session,
            completed.turn,
            completed.status,
            answer
                .map(|message| message.text.as_str())
                .unwrap_or_default()
        );
    }

    fn running(&self, cx: &App) -> Option<(String, String)> {
        let Opening::Open(id) = &self.opening else {
            return None;
        };
        let session = self.store.read(cx).sessions.get(id)?;
        let (turn, _) = session
            .turns
            .iter()
            .find(|(_, turn)| turn.status.is_none())?;
        Some((id.clone(), turn.clone()))
    }

    fn status(&self, cx: &App) -> (bool, bool, String) {
        let waiting = self
            .card
            .as_ref()
            .is_some_and(|shown| shown.asking.waiting());
        let open = self.running(cx).is_some();
        let session = self.store.read(cx).open_session();
        let sub_agents = session.map_or(0, items::sub_agents);
        let lead_busy = open
            && !session.is_some_and(|session| {
                session
                    .turns
                    .values()
                    .any(|turn| turn.status.is_none() && turn.lead_idle)
            });
        let phase = match (waiting, lead_busy) {
            (true, _) => "waiting on you".to_owned(),
            (false, true) => self.doing(cx),
            (false, false) => format!(
                "lead idle, free to talk · {sub_agents} sub-agent{} running",
                if sub_agents == 1 { "" } else { "s" }
            ),
        };
        (waiting || lead_busy || sub_agents > 0, !lead_busy, phase)
    }

    fn ask_card(
        &self,
        id: &'static str,
        owns_field: bool,
        answer: impl Fn(Act, &mut Window, &mut App) + 'static,
        theme: &desk_ui::theme::Theme,
    ) -> Option<gpui::Div> {
        self.card.as_ref().map(|shown| {
            div()
                .w_full()
                .max_w(px(CONTENT_WIDTH))
                .mx_auto()
                .child(ask_bar(
                    id,
                    &shown.asking,
                    (owns_field && shown.asking.typing())
                        .then(|| self.own_answer.clone().into_any_element()),
                    1.0,
                    owns_field,
                    answer,
                    theme,
                ))
        })
    }

    fn doing(&self, cx: &App) -> String {
        let store = self.store.read(cx);
        store
            .open_session()
            .and_then(|session| {
                let (_, turn) = session.turns.iter().find(|(_, t)| t.status.is_none())?;
                items::doing(session, &turn.started_at)
            })
            .map_or_else(|| WORKING.to_owned(), |doing| format!("{WORKING}: {doing}"))
    }

    pub fn shown_id(&self) -> Option<String> {
        match &self.opening {
            Opening::Open(id) => Some(id.clone()),
            Opening::Pending => self.history.as_ref().map(|history| history.session.clone()),
            Opening::Closed => None,
        }
    }

    fn refresh(&mut self, cx: &mut Context<Self>) {
        let spliced = self.restack();
        let Some(id) = self.shown_id() else {
            return;
        };
        let Some(session) = self.store.read(cx).sessions.get(&id) else {
            return;
        };
        let kept = self.card.as_ref().is_some_and(|shown| {
            session.approvals.contains_key(&shown.key) || session.questions.contains_key(&shown.key)
        });
        if !kept {
            let shown = self.card.take().is_some();
            self.card = session
                .approvals
                .values()
                .next()
                .map(|(asked_id, asked)| Card::approval(asked_id.clone(), asked, &self.project))
                .or_else(|| session.questions.values().next().map(Card::question));
            self.refocus |= shown || self.card.is_some();
        }
        let changed = !spliced.is_empty();
        for rows in spliced {
            if let Spliced::Rows { old, count, .. } = &rows {
                self.feed
                    .update(cx, |feed, cx| feed.splice(old.clone(), *count, cx));
            }
            cx.emit(rows);
        }
        if changed && !self.query.is_empty() {
            self.search(cx);
        }
        cx.notify();
    }

    fn restack(&mut self) -> Vec<Spliced> {
        let Some(id) = self.shown_id() else {
            return Vec::new();
        };
        let Some(session) = self.transcript.sessions.get(&id) else {
            return Vec::new();
        };
        let order = self.orders.get(&id).map_or(&[][..], Vec::as_slice);
        let shown = Rc::make_mut(&mut self.items);
        let (mut at, mut spliced) = (0, Vec::new());
        for (index, entry) in order.iter().enumerate() {
            if let Some(slot) = self.slots.get(index)
                && slot.settled
            {
                at += usize::from(slot.item.is_some());
                continue;
            }
            let now = items::item(
                session,
                entry,
                self.slots.get(index).and_then(|slot| slot.item.as_ref()),
                &self.parsed,
            );
            let settled = items::settled(session, entry);
            let was = match self.slots.get_mut(index) {
                Some(slot) => {
                    slot.settled = settled;
                    std::mem::replace(&mut slot.item, now.clone())
                }
                None => {
                    self.slots.push(Slot {
                        item: now.clone(),
                        settled,
                    });
                    None
                }
            };
            let (old, count) = match (was, now) {
                (None, None) => continue,
                (Some(was), Some(now)) if was == now => {
                    at += 1;
                    continue;
                }
                (Some(_), Some(now)) => {
                    shown[at] = now;
                    (at..at + 1, 1)
                }
                (None, Some(now)) => {
                    shown.insert(at, now);
                    (at..at, 1)
                }
                (Some(_), None) => {
                    shown.remove(at);
                    (at..at + 1, 0)
                }
            };
            spliced.push(Spliced::Rows {
                old,
                count,
                after: shown.len(),
            });
            at += count;
        }
        spliced
    }

    fn call<R: Request>(
        &mut self,
        params: &R::Params,
        cx: &mut Context<Self>,
        then: impl FnOnce(&mut Self, R::Result, &mut Context<Self>) + 'static,
    ) where
        R::Result: 'static,
    {
        self.request_typed::<R>(params, cx, |chat, reply, cx| {
            chat.landed::<R>(reply, then, cx)
        });
    }

    fn landed<R: Request>(
        &mut self,
        reply: Result<R::Result, BridgeError>,
        then: impl FnOnce(&mut Self, R::Result, &mut Context<Self>),
        cx: &mut Context<Self>,
    ) {
        match reply {
            Ok(result) => then(self, result, cx),
            Err(error) if error.turn_running() => {
                eprintln!("desk: {} waits, a turn is running: {error}", R::METHOD);
            }
            Err(error) => {
                eprintln!("desk: {} failed: {error:?}", R::METHOD);
                self.fail(error.to_string(), cx);
            }
        }
    }

    fn moved(&self, method: &str, sent: &str) -> bool {
        let now = self.shown_id();
        if now.as_deref() == Some(sent) {
            return false;
        }
        eprintln!(
            "desk: dropped {method} for {sent}, now {}",
            now.as_deref().unwrap_or("none")
        );
        true
    }

    pub(crate) fn request<R: Request>(
        &mut self,
        params: &R::Params,
        cx: &mut Context<Self>,
        then: impl FnOnce(&mut Self, Result<R::Result, String>, &mut Context<Self>) + 'static,
    ) where
        R::Result: 'static,
    {
        let Link::Ready(_) = &self.link else {
            return then(
                self,
                Err("tofu is not running, so nothing was sent".to_owned()),
                cx,
            );
        };
        self.request_typed::<R>(params, cx, |chat, reply, cx| {
            let reply = reply.map_err(|error| {
                eprintln!("desk: {} failed: {error:?}", R::METHOD);
                error.to_string()
            });
            then(chat, reply, cx);
        });
    }

    fn request_typed<R: Request>(
        &mut self,
        params: &R::Params,
        cx: &mut Context<Self>,
        then: impl FnOnce(&mut Self, Result<R::Result, BridgeError>, &mut Context<Self>) + 'static,
    ) where
        R::Result: 'static,
    {
        let Link::Ready(bridge) = &self.link else {
            return then(self, Err(BridgeError::Closed), cx);
        };
        let pending = match bridge.request::<R>(params) {
            Ok(pending) => pending,
            Err(error) => return then(self, Err(error), cx),
        };
        cx.spawn(async move |this, cx| {
            let reply = pending.reply().await;
            this.update(cx, |chat, cx| then(chat, reply, cx))
        })
        .detach();
    }

    fn ask_models(&mut self, cx: &mut Context<Self>) {
        self.call::<request::QueryModels>(&NoParams {}, cx, |chat, models, cx| {
            chat.call::<request::SessionState>(&NoParams {}, cx, move |chat, state, cx| {
                eprintln!(
                    "desk: session.state pick wire {} model {}, query.models {} rows",
                    state.pick.wire.as_deref().unwrap_or("none"),
                    state.pick.model.as_deref().unwrap_or("none"),
                    models.models.len()
                );
                chat.state_now(&state, cx);
                chat.pick = state.pick;
                chat.listed(&models, cx);
            });
        });
    }

    fn ask_shells(&mut self, cx: &mut Context<Self>) {
        self.call::<request::SessionState>(&NoParams {}, cx, |chat, state, cx| {
            eprintln!(
                "desk: session.state for {} lists {} shells",
                state.session,
                state.shells.len()
            );
            chat.state_now(&state, cx);
        });
    }

    fn state_now(&mut self, state: &SessionState, cx: &mut Context<Self>) {
        if let Err(error) = self.store.update(cx, |store, cx| {
            cx.notify();
            store.state_now(state)
        }) {
            eprintln!("desk: the store refused session.state from tofu: {error}");
        }
    }

    fn listed(&mut self, models: &ModelsQuery, cx: &mut Context<Self>) {
        self.defaults.clone_from(&models.defaults);
        let usable: Vec<(&str, &str)> = models
            .models
            .iter()
            .filter(|model| model.kind == "llm" && model.r#use != "excluded")
            .filter_map(|model| Some((model.slug.split_once('/')?.0, model.id.as_str())))
            .collect();
        let mut sources: Vec<&str> = Vec::new();
        for source in models
            .subscriptions
            .iter()
            .map(String::as_str)
            .chain(usable.iter().map(|(source, _)| *source))
        {
            if !sources.contains(&source) {
                sources.push(source);
            }
        }
        let mut items = Vec::new();
        self.choices.clear();
        for source in sources {
            let inside: Vec<&str> = usable
                .iter()
                .filter(|(owner, _)| *owner == source)
                .map(|(_, model)| *model)
                .collect();
            if inside.is_empty() {
                continue;
            }
            self.choices.push(None);
            self.choices.extend(
                inside
                    .iter()
                    .map(|model| Some((source.to_owned(), (*model).to_owned()))),
            );
            let submenu = MenuItem::Submenu {
                label: source.to_owned().into(),
                icon: Brand::for_source(source).map(MenuIcon::from),
                items: inside.into_iter().map(MenuItem::action).collect(),
            };
            items.push(submenu);
        }
        self.models.update(cx, |button, cx| button.items(items, cx));
        self.fill_models(cx);
    }

    fn model_name(&self, cx: &App) -> String {
        let session = self
            .shown_id()
            .and_then(|id| self.store.read(cx).sessions.get(&id)?.pick.clone());
        let slug = |pick: &ModelPick| {
            let model = pick.model.as_deref()?;
            Some(match pick.wire.as_deref() {
                Some(wire) if !model.contains('/') => format!("{wire}/{model}"),
                _ => model.to_owned(),
            })
        };
        slug(&self.pick)
            .or_else(|| session.as_ref().and_then(slug))
            .or_else(|| self.defaults.first().cloned())
            .unwrap_or_else(|| MODEL.to_owned())
    }

    fn relabel(&mut self, cx: &mut Context<Self>) {
        let label = self.model_name(cx);
        if label == self.labelled {
            return;
        }
        eprintln!("desk: the model picker reads {label}");
        self.labelled.clone_from(&label);
        let brand = label
            .split_once('/')
            .and_then(|(source, _)| Brand::for_source(source));
        self.models.update(cx, |button, cx| {
            button
                .trigger(move |_, theme| inline_picker("chat-model", brand, label.clone(), theme));
            cx.notify();
        });
    }

    fn fill_models(&mut self, cx: &mut Context<Self>) {
        self.relabel(cx);
        self.store.update(cx, |_, cx| cx.notify());
        cx.notify();
    }

    fn choose(&mut self, at: usize, cx: &mut Context<Self>) {
        let Some(Some((wire, model))) = self.choices.get(at).cloned() else {
            return;
        };
        let chosen = ModelPick {
            wire: Some(wire.clone()),
            model: Some(model.clone()),
            effort: self.pick.effort.clone(),
        };
        let was = std::mem::replace(&mut self.pick, chosen);
        self.fill_models(cx);
        eprintln!("desk: session.set asked wire {wire} model {model}");
        let params = SessionSetParams {
            wire: Some(wire.clone()),
            model: Some(model.clone()),
            ..SessionSetParams::default()
        };
        self.request::<request::SessionSet>(&params, cx, move |chat, reply, cx| match reply {
            Ok(ack) => eprintln!(
                "desk: session.set wire {wire} model {model} answered ok {}",
                ack.ok
            ),
            Err(error) => {
                chat.pick = was;
                chat.fill_models(cx);
                chat.fail(format!("session.set {wire}/{model}: {error}"), cx);
            }
        });
    }

    pub fn source(&self, cx: &App) -> Option<String> {
        let open = self.open_id().and_then(|id| {
            self.store
                .read(cx)
                .sessions
                .get(id)?
                .pick
                .as_ref()?
                .wire
                .clone()
        });
        open.or_else(|| self.pick.wire.clone()).or_else(|| {
            self.defaults
                .first()
                .and_then(|slug| slug.split('/').next())
                .map(str::to_owned)
        })
    }

    pub fn decisions_shown(&self, cx: &App) -> Decisions {
        let Opening::Open(open) = &self.opening else {
            return Decisions::default();
        };
        let live = self
            .store
            .read(cx)
            .sessions
            .get(open)
            .map_or(0, |session| session.decisions.len());
        let past = match &self.ledger {
            Some((session, Some(past))) if session == open => past.shown,
            Some(_) | None => Decisions::default(),
        };
        Decisions {
            count: past.count.saturating_add(live),
            capped: past.capped,
        }
    }

    pub fn past_decisions(&self) -> &[DecisionMade] {
        let Opening::Open(open) = &self.opening else {
            return &[];
        };
        match &self.ledger {
            Some((session, Some(past))) if session == open => &past.made,
            Some(_) | None => &[],
        }
    }

    fn ask_ledger(&mut self, session: String, cx: &mut Context<Self>) {
        self.ledger = Some((session.clone(), None));
        let params = LedgerParams {
            last: Some(STATUS_LEDGER_LAST),
            session: Some(session.clone()),
            ..LedgerParams::default()
        };
        self.call::<request::QueryLedger>(&params, cx, move |chat, ledger, cx| {
            let mut turns: Vec<&str> = ledger
                .rows
                .iter()
                .filter_map(|row| row.turn_id.as_deref())
                .collect();
            turns.sort_unstable();
            turns.dedup();
            let made: Vec<DecisionMade> = session_ledger(&ledger.rows, &session, &turns)
                .into_iter()
                .map(|(_, made)| made)
                .collect();
            let capped =
                i64::try_from(ledger.rows.len()).is_ok_and(|read| read >= STATUS_LEDGER_LAST);
            let shown = Decisions {
                count: made.len(),
                capped,
            };
            eprintln!(
                "desk: status bar classifier reads {shown} of {} ledger rows, last {STATUS_LEDGER_LAST}, for {session}",
                ledger.rows.len()
            );
            if chat
                .ledger
                .as_ref()
                .is_some_and(|(asked, _)| *asked == session)
            {
                chat.ledger = Some((session, Some(Ledger { shown, made })));
                chat.store.update(cx, |_, cx| cx.notify());
            }
        });
    }

    pub fn want_usage(&mut self, cx: &mut Context<Self>) {
        self.store.update(cx, |store, _| store.usage.want());
        self.ask_due(cx);
    }

    pub fn reread_usage(&mut self, cx: &mut Context<Self>) {
        self.store.update(cx, |store, _| store.usage.again());
        self.ask_due(cx);
    }

    pub fn accounts(&self) -> &Answer<Accounts> {
        &self.accounts
    }

    pub fn want_accounts(&mut self, cx: &mut Context<Self>) {
        self.accounts.want();
        self.ask_due(cx);
    }

    fn ask_accounts(&mut self, cx: &mut Context<Self>) {
        if !self.accounts.take_due() {
            return;
        }
        eprintln!("desk: query.accounts asked");
        self.request::<request::QueryAccounts>(&NoParams {}, cx, |chat, reply, cx| {
            let answer = reply.map(Read::now).map_err(|reason| QueryError {
                method: request::QueryAccounts::METHOD,
                reason,
            });
            match &answer {
                Ok(read) => eprintln!(
                    "desk: query.accounts read at {}: {}",
                    read.at,
                    accounts_said(&read.value)
                ),
                Err(error) => eprintln!("desk: {error}"),
            }
            chat.told_accounts = answer.as_ref().ok().map(|read| read.value.clone());
            chat.accounts.answered(answer);
            chat.fill_accounts(cx);
            chat.ask_due(cx);
        });
    }

    #[cfg(feature = "screen-settings")]
    pub fn signed_subscriptions(&self) -> usize {
        self.accounts.read.as_ref().map_or(0, |read| {
            read.value
                .subscriptions
                .iter()
                .filter(|source| !source.accounts.is_empty())
                .count()
        })
    }

    fn fill_accounts(&mut self, cx: &mut Context<Self>) {
        if let (Some(told), Some(read), Ok(emails)) =
            (&self.told_accounts, &mut self.accounts.read, &self.emails)
        {
            read.value = emails.filled(told.clone());
        }
        self.store.update(cx, |_, cx| cx.notify());
    }

    pub fn sign_in(&mut self, source: &str, cx: &mut Context<Self>) {
        if self.signing.is_some() {
            return eprintln!("desk: sign in {source}: one sign in already runs");
        }
        let tofu = tofu();
        eprintln!(
            "desk: sign in spawns {} login llm {source} in {}",
            tofu.display(),
            self.project
        );
        let mut command = Command::new(tofu);
        command
            .args(["login", "llm", source])
            .current_dir(&self.project)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null());
        #[cfg(windows)]
        std::os::windows::process::CommandExt::creation_flags(&mut command, CREATE_NO_WINDOW);
        let child = match command.spawn() {
            Ok(child) => child,
            Err(error) => {
                return self.fail(
                    format!("tofu login llm {source} did not start: {error}"),
                    cx,
                );
            }
        };
        eprintln!("desk: sign in {source} pid {}", child.id());
        self.signing = Some(SigningIn(child));
        self._signed = Some(cx.spawn(async move |this, cx| {
            loop {
                cx.background_executor().timer(SIGN_IN_POLL).await;
                match this.update(cx, |chat, cx| chat.signed(cx)) {
                    Ok(false) => {}
                    Ok(true) | Err(_) => return,
                }
            }
        }));
    }

    fn signed(&mut self, cx: &mut Context<Self>) -> bool {
        let Some(SigningIn(child)) = &mut self.signing else {
            return true;
        };
        let exited = match child.try_wait() {
            Ok(None) => return false,
            Ok(Some(status)) => status.to_string(),
            Err(error) => error.to_string(),
        };
        eprintln!("desk: sign in pid {} ended: {exited}", child.id());
        self.signing = None;
        self.accounts.again();
        self.reread_usage(cx);
        true
    }

    pub fn want_library(&mut self, cx: &mut Context<Self>) {
        self.store.update(cx, |store, _| {
            store.agents.want();
            store.rules.want();
        });
        self.ask_due(cx);
    }

    pub fn reread_library(&mut self, cx: &mut Context<Self>) {
        self.store.update(cx, |store, _| {
            store.agents.again();
            store.rules.again();
        });
        self.ask_due(cx);
    }

    pub fn want_context(&mut self, cx: &mut Context<Self>) {
        self.store.update(cx, |store, _| store.context.want());
        self.ask_due(cx);
    }

    pub fn reread_context(&mut self, cx: &mut Context<Self>) {
        self.store.update(cx, |store, _| store.context.again());
        self.ask_due(cx);
    }

    pub fn want_info(&mut self, cx: &mut Context<Self>) {
        self.store.update(cx, |store, _| store.info.want());
        self.ask_due(cx);
    }

    pub fn reread_info(&mut self, cx: &mut Context<Self>) {
        self.store.update(cx, |store, _| store.info.again());
        self.ask_due(cx);
    }

    pub fn want_lineage(&mut self, session: &str, cx: &mut Context<Self>) {
        self.store.update(cx, |store, _| {
            store.lineage.entry(session.to_owned()).or_default().want();
        });
        self.ask_due(cx);
    }

    pub fn forget_lineage(&mut self, cx: &mut Context<Self>) {
        self.store.update(cx, |store, _| store.lineage.clear());
    }

    pub fn reread_lineage(&mut self, cx: &mut Context<Self>) {
        self.store.update(cx, |store, _| {
            store.lineage.values_mut().for_each(Answer::again);
        });
        self.ask_due(cx);
    }

    fn ask_due(&mut self, cx: &mut Context<Self>) {
        if !matches!(self.link, Link::Ready(_)) {
            return;
        }
        if let Some(open) = self.open_id().map(str::to_owned)
            && self
                .store
                .update(cx, |store, _| store.statuses.take_due(&open))
        {
            let params = StatusListParams {
                session: Some(open.clone()),
            };
            self.request_typed::<request::StatusList>(&params, cx, move |chat, reply, cx| {
                if chat.moved(request::StatusList::METHOD, &open) {
                    return;
                }
                chat.landed::<request::StatusList>(
                    reply,
                    |chat, list, cx| {
                        let listed = chat.store.update(cx, |store, cx| {
                            cx.notify();
                            store.statuses.listed(&open, &list.records)
                        });
                        match listed {
                            Ok(changes) => changes.iter().for_each(|(id, state)| {
                                eprintln!("desk: status {id} {}", state.word())
                            }),
                            Err(error) => chat.fail(format!("status.list: {error}"), cx),
                        }
                    },
                    cx,
                );
            });
        }
        let open = SessionParams {
            session: self.shown_id(),
        };
        let sent = open.session.clone();
        self.ask::<request::QueryContext>(
            &open,
            sent.clone(),
            |store| &mut store.context,
            context_said,
            cx,
        );
        self.ask::<request::SessionInfo>(
            &open,
            sent.clone(),
            |store| &mut store.info,
            info_said,
            cx,
        );
        if sent.is_some() {
            self.ask::<request::SessionTrace>(
                &open,
                sent,
                |store| &mut store.trace,
                trace_said,
                cx,
            );
        }
        let lineage: Vec<String> = self.store.read(cx).lineage.keys().cloned().collect();
        for session in lineage {
            let params = SessionParams {
                session: Some(session.clone()),
            };
            self.ask::<request::SessionInfo>(
                &params,
                None,
                move |store| store.lineage.entry(session.clone()).or_default(),
                info_said,
                cx,
            );
        }
        self.ask_accounts(cx);
        self.ask::<request::QueryUsage>(
            &NoParams {},
            None,
            |store| &mut store.usage,
            |usage| windows_said(&usage.providers),
            cx,
        );
        self.ask::<request::QueryAgents>(
            &NoParams {},
            None,
            |store| &mut store.agents,
            |agents| agents_said(&agents.definitions),
            cx,
        );
        self.ask::<request::QueryRules>(
            &NoParams {},
            None,
            |store| &mut store.rules,
            |rules| {
                let ids: Vec<&str> = rules.rules.iter().map(|rule| rule.id.as_str()).collect();
                format!(
                    "{} rules from {} [{}]",
                    ids.len(),
                    rules.origin,
                    ids.join(", ")
                )
            },
            cx,
        );
    }

    fn ask<R: Request>(
        &mut self,
        params: &R::Params,
        sent: Option<String>,
        slot: impl Fn(&mut Store) -> &mut Answer<R::Result> + 'static,
        said: fn(&R::Result) -> String,
        cx: &mut Context<Self>,
    ) where
        R::Result: 'static,
    {
        let (asked, stale) = self.store.update(cx, |store, cx| {
            let answer = slot(store);
            let asked = answer.take_due();
            let stale = answer.stale;
            if asked {
                cx.notify();
            }
            (asked, stale)
        });
        if !asked {
            return;
        }
        eprintln!("desk: {} asked, stale {stale}", R::METHOD);
        self.request_typed::<R>(params, cx, move |chat, reply, cx| {
            if sent.is_some_and(|sent| chat.moved(R::METHOD, &sent)) {
                chat.store.update(cx, |store, _| {
                    let answer = slot(store);
                    answer.asking = false;
                    answer.again();
                });
                return chat.ask_due(cx);
            }
            let answer = reply.map(Read::now).map_err(|reason| QueryError {
                method: R::METHOD,
                reason: reason.to_string(),
            });
            match &answer {
                Ok(read) => eprintln!(
                    "desk: {} read at {}: {}",
                    R::METHOD,
                    read.at,
                    said(&read.value)
                ),
                Err(error) => eprintln!("desk: {error}"),
            }
            chat.store.update(cx, |store, cx| {
                slot(store).answered(answer);
                cx.notify();
            });
            chat.ask_due(cx);
        });
    }

    fn send(&mut self, text: &str, cx: &mut Context<Self>) {
        if let Some(held) = self.held() {
            return eprintln!("desk: send held: {held}");
        }
        let (typed, images) = self.numbered(text.trim());
        let mut words = self.take_tokens();
        words.push(typed);
        let text = words.join(" ").trim().to_owned();
        if text.is_empty() {
            return;
        }
        eprintln!("desk: chat sends {} images", images.len());
        self.pictures.clear();
        self.area.update(cx, |area, cx| area.clear(cx));
        self.post_with(text, images, cx);
    }

    fn post(&mut self, text: String, cx: &mut Context<Self>) {
        self.post_with(text, Vec::new(), cx);
    }

    fn post_with(&mut self, text: String, images: Vec<AttachedImage>, cx: &mut Context<Self>) {
        self.problem = None;
        match &self.opening {
            Opening::Open(session) => self.deliver(session.clone(), text, images, cx),
            Opening::Pending => {
                self.fail("the session is still opening".to_owned(), cx);
            }
            Opening::Closed => {
                self.set_opening(Opening::Pending, cx);
                let params = SessionOpenParams {
                    asking: Some(self.asking.clone()),
                    ..SessionOpenParams::default()
                };
                self.call::<request::SessionOpen>(&params, cx, move |chat, opened, cx| {
                    eprintln!(
                        "desk: session {} opened, fresh {}, asking {}",
                        opened.session,
                        opened.fresh,
                        String::from(chat.asking.clone())
                    );
                    chat.opened(opened, Instant::now(), cx);
                    chat.relist(cx);
                    chat.post_with(text, images, cx);
                });
            }
        }
        cx.notify();
    }

    fn deliver(
        &mut self,
        session: String,
        text: String,
        images: Vec<AttachedImage>,
        cx: &mut Context<Self>,
    ) {
        let params = TurnSendParams {
            session,
            text,
            images,
            ..TurnSendParams::default()
        };
        self.request_typed::<request::TurnSend>(&params, cx, |chat, sent, cx| match sent {
            Ok(sent) => match sent.steered {
                Some(steered) => eprintln!("desk: steer {steered} sent on turn {}", sent.turn),
                None => eprintln!("desk: turn {} sent", sent.turn),
            },
            Err(error) => chat.fail(error.to_string(), cx),
        });
    }

    fn stop(&mut self, cx: &mut Context<Self>) {
        let Some((session, turn)) = self.running(cx) else {
            return;
        };
        self.call::<request::TurnStop>(
            &TurnParams {
                session,
                turn,
                lead: None,
                everything: None,
            },
            cx,
            |_, _, _| {},
        );
    }

    fn attach(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let asked = cx.prompt_for_paths(PathPromptOptions {
            files: true,
            directories: false,
            multiple: true,
            prompt: Some(ATTACH.into()),
        });
        cx.spawn_in(window, async move |chat, cx| {
            let picked = match asked.await {
                Ok(Ok(Some(picked))) => picked,
                Ok(Ok(None)) => return eprintln!("desk: chat attach: no file was picked"),
                Ok(Err(error)) => {
                    return eprintln!("desk: chat attach: the file dialog failed: {error}");
                }
                Err(_) => {
                    return eprintln!(
                        "desk: chat attach: the file dialog closed without an answer"
                    );
                }
            };
            let landed = chat.update_in(cx, |chat, window, cx| {
                chat.attach_paths(&picked, window, cx);
                chat.area.focus_handle(cx).focus(window, cx);
            });
            if landed.is_err() {
                eprintln!("desk: the chat closed before the attached file landed");
            }
        })
        .detach();
    }

    fn act(&mut self, act: Act, window: &mut Window, cx: &mut Context<Self>) {
        let typed = self.own_answer.read(cx).text();
        let Some(shown) = &mut self.card else {
            return;
        };
        let was_typing = shown.asking.typing();
        let answered = shown.asking.act(act, &typed, Instant::now());
        let typing = shown.asking.typing();
        if answered || !typing {
            self.own_answer.update(cx, TextArea::clear);
        }
        if typing && !was_typing {
            self.own_answer.focus_handle(cx).focus(window, cx);
        }
        cx.notify();
        let Some(shown) = self
            .card
            .as_ref()
            .filter(|shown| answered && !shown.asking.waiting())
        else {
            return;
        };
        let Link::Ready(bridge) = &self.link else {
            return self.fail("tofu is not running, so the answer was lost".to_owned(), cx);
        };
        let sent = match &shown.asked {
            Asked::Approval => {
                let Some(answer) = shown.asking.approved().map(decided) else {
                    return;
                };
                let said = String::from(answer.decision.clone());
                bridge
                    .answer(&shown.id, &answer)
                    .map(|()| format!("approval {} answered {said}", shown.key))
            }
            Asked::Question(ask) => {
                let answers = ask
                    .request
                    .questions
                    .iter()
                    .zip(&ask.kinds)
                    .zip(shown.asking.said())
                    .map(|((asked, kind), (picked, typed))| {
                        person_reply(asked, *kind, picked, typed)
                    })
                    .collect();
                bridge
                    .answer(
                        &shown.id,
                        &QuestionAnswer {
                            outcome: QuestionOutcome::Submitted,
                            answers,
                        },
                    )
                    .map(|()| format!("question {} answered", shown.key))
            }
        };
        match sent {
            Ok(said) => eprintln!("desk: {said}"),
            Err(error) => self.fail(error.to_string(), cx),
        }
        self.refocus = true;
    }

    fn want_page(&mut self, visible: Range<usize>, window: &mut Window, cx: &mut Context<Self>) {
        if let Some(history) = self.history.as_mut()
            && let Some(paging) = history.wanted(visible)
        {
            history.asking = true;
            cx.defer_in(window, move |chat, _, cx| chat.page(paging, cx));
        }
    }

    fn key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let act = self
            .card
            .as_ref()
            .and_then(|shown| shown.asking.key(&event.keystroke.key));
        if let Some(act) = act {
            self.act(act, window, cx);
        }
    }

    fn frame(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Link::Recorded(replay) = &mut self.link else {
            return;
        };
        window.request_animation_frame();
        match replay.step() {
            Ok(Step::Feed(event)) => self.take(&[event], cx),
            Ok(Step::Restart) => self.restart(cx),
            Ok(Step::Report) => cx.quit(),
            Err(error) => eprintln!("desk: the cassette has a bad line, skipped: {error}"),
        }
    }
}

pub fn windows_said(providers: &[CredentialReport]) -> String {
    let said: Vec<String> = providers
        .iter()
        .map(|provider| {
            let windows: Vec<String> = provider
                .windows
                .iter()
                .map(|window| format!("{} {:.0}%", window.id, window.used_fraction * 100.0))
                .collect();
            format!("{} [{}]", provider.provider, windows.join(", "))
        })
        .collect();
    said.join("; ")
}

fn trace_said(trace: &SessionTrace) -> String {
    let agents: Vec<String> = trace
        .agents
        .iter()
        .map(|run| {
            format!(
                "{} {}..{}",
                run.agent,
                run.started_at,
                run.ended_at.as_deref().unwrap_or("running")
            )
        })
        .collect();
    format!("{} agents [{}]", agents.len(), agents.join(", "))
}

fn accounts_said(accounts: &Accounts) -> String {
    let said: Vec<String> = accounts
        .subscriptions
        .iter()
        .flat_map(|source| {
            source.accounts.iter().map(|account| {
                let windows: Vec<String> = account
                    .windows
                    .iter()
                    .map(|window| format!("{} {:.0}%", window.id, window.used * 100.0))
                    .collect();
                format!(
                    "{} #{} {} {} [{}]",
                    source.source,
                    account.id,
                    account.account,
                    String::from(account.state.clone()),
                    windows.join(", ")
                )
            })
        })
        .collect();
    said.join("; ")
}

fn info_said(info: &SessionInfo) -> String {
    format!(
        "session {} {}: {} turns, {} steps, {} sub-agents, {} reads, {} carried, outcome {}, forked into {}",
        info.name.as_deref().unwrap_or("unnamed"),
        info.id,
        info.turns,
        info.steps,
        info.sub_agents,
        info.reads,
        info.carried_messages,
        info.outcome.as_deref().unwrap_or("none"),
        info.forked_into.as_deref().unwrap_or("none")
    )
}

fn context_said(report: &ContextReport) -> String {
    let bands = report.occupancy.as_ref().map_or_else(
        || "no occupancy".to_owned(),
        |occupancy| {
            let bands: Vec<String> = occupancy
                .bands()
                .iter()
                .map(|(name, band)| format!("{name} {}/{}", band.tokens, band.cap))
                .collect();
            format!(
                "total {} mark {} [{}]",
                occupancy.total,
                occupancy.mark,
                bands.join(", ")
            )
        },
    );
    format!(
        "session {} {}: {bands} ceiling {}",
        report.name.as_deref().unwrap_or("unnamed"),
        report.session.as_deref().unwrap_or("none"),
        report.ceiling.unwrap_or_default()
    )
}

fn agents_said(definitions: &[subagent::Definition]) -> String {
    let names: Vec<&str> = definitions
        .iter()
        .map(|definition| definition.name.as_str())
        .collect();
    format!("{} agents [{}]", names.len(), names.join(", "))
}

async fn drain(
    this: gpui::WeakEntity<Chat>,
    events: flume::Receiver<Event>,
    cx: &mut gpui::AsyncApp,
) {
    while let Ok(first) = events.recv_async().await {
        let batch: Vec<Event> = iter::once(first).chain(events.try_iter()).collect();
        if this.update(cx, |chat, cx| chat.take(&batch, cx)).is_err() {
            return;
        }
    }
}

impl Drop for Chat {
    fn drop(&mut self) {
        let Link::Ready(bridge) = std::mem::replace(&mut self.link, Link::Gone) else {
            return;
        };
        let pid = bridge.pid();
        thread::spawn(move || match bridge.stop() {
            Ok(status) => eprintln!("desk: tofu pid {pid} stopped: {status}"),
            Err(error) => eprintln!("desk: tofu pid {pid} did not stop: {error}"),
        });
    }
}

impl Render for Chat {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        desk_perf::span!("chat");
        self.frame(window, cx);
        if !self.finding && std::mem::take(&mut self.refocus) {
            match self.card.as_ref().filter(|shown| shown.asking.waiting()) {
                Some(shown) if shown.asking.typing() => {
                    self.own_answer.focus_handle(cx).focus(window, cx);
                }
                Some(_) => self.focus.focus(window, cx),
                None => self.area.focus_handle(cx).focus(window, cx),
            }
        }
        if !self.items.is_empty()
            && let Some((session, asked)) = self.painting.take()
        {
            eprintln!(
                "desk: session {session} first message painted in {} ms",
                asked.elapsed().as_millis()
            );
        }
        if !self.items.is_empty()
            && let Some(landed) = self.landed.take()
        {
            eprintln!(
                "desk: chat history painted {} items {} ms after landing",
                self.items.len(),
                landed.elapsed().as_millis()
            );
        }
        let theme = ActiveTheme::theme(cx);
        let clip = tooltip(
            "chat-attach-tip",
            paperclip("chat-attach", &theme)
                .on_click(cx.listener(|chat, _: &ClickEvent, window, cx| chat.attach(window, cx))),
            Edge::FrameStart,
            ATTACH,
            &theme,
            window,
            cx,
        );
        let dropped = cx.weak_entity();
        let stop = cx.listener(|chat, _: &(), _, cx| chat.stop(cx));
        let pick = cx.listener(|chat, at: &usize, window, cx| chat.pick(*at, window, cx));
        let unmention = cx.listener(|chat, at: &usize, _, cx| chat.remove_mention(*at, cx));
        let (busy, quiet, phase) = self.status(cx);
        let composer = Composer::new("chat-composer", self.area.clone())
            .content_width(px(CONTENT_WIDTH))
            .busy(busy)
            .quiet(quiet)
            .phase(phase)
            .below_start(clip)
            .below_end(self.models.clone())
            .traces(self.chips())
            .held(self.held().is_some())
            .menu(self.mention_menu(), move |at, window, cx| {
                pick(&at, window, cx)
            })
            .on_send(cx.listener(|chat, text: &str, _, cx| chat.send(text, cx)))
            .on_stop(move |window, cx| stop(&(), window, cx))
            .on_drop(move |paths, window, cx| {
                let landed = dropped.update(cx, |chat, cx| {
                    chat.attach_paths(paths, window, cx);
                    chat.area.focus_handle(cx).focus(window, cx);
                });
                if let Err(error) = landed {
                    eprintln!("desk: a drop outlived the chat: {error}");
                }
            })
            .on_remove_trace(move |at, window, cx| unmention(&at, window, cx));
        let answer = cx.listener(|chat, act: &Act, window, cx| chat.act(*act, window, cx));
        let ask = self.ask_card(
            "chat-ask",
            true,
            move |act, window, cx| answer(&act, window, cx),
            &theme,
        );
        let problem = self
            .problem
            .clone()
            .map(|problem| fail("tofu", problem, "", &theme));
        let meter = match &self.link {
            Link::Recorded(replay) => Some(replay.meter()),
            Link::Starting | Link::Ready(_) | Link::Gone | Link::Fed => None,
        };
        div()
            .track_focus(&self.focus)
            .capture_key_down(cx.listener(|chat, event: &KeyDownEvent, window, cx| {
                let up = match event.keystroke.key.as_str() {
                    "pageup" => true,
                    "pagedown" => false,
                    _ => return chat.mention_key(event, window, cx),
                };
                cx.stop_propagation();
                let rows = chat.items.len();
                chat.feed.update(cx, |feed, cx| feed.page(up, rows, cx));
            }))
            .on_key_down(
                cx.listener(|chat, event: &KeyDownEvent, window, cx| chat.key(event, window, cx)),
            )
            .on_action(cx.listener(Self::open_find))
            .on_action(cx.listener(Self::revoke))
            .capture_any_mouse_down(cx.listener(|chat, _: &MouseDownEvent, _, _| chat.aimed = true))
            .on_mouse_down_out(cx.listener(|chat, _: &MouseDownEvent, _, _| chat.aimed = false))
            .relative()
            .size_full()
            .flex()
            .flex_col()
            .pt(px(COLUMN_TOP))
            .child(
                div()
                    .relative()
                    .flex_1()
                    .min_h_0()
                    .when(self.finding, |area| area.pt(px(FIND_RESERVE)))
                    .child(Feed::cached(&self.feed))
                    .child(self.find.clone()),
            )
            .child(
                div()
                    .flex_none()
                    .flex()
                    .flex_col()
                    .gap(px(DOCK_GAP))
                    .p(px(DOCK_INSET))
                    .children(problem)
                    .children(ask)
                    .child(composer),
            )
            .children(meter)
    }
}

#[cfg(feature = "screen-accounts")]
mod keys {
    use std::fs;

    use desk_core::protocol::{LoginKeyParams, LoginNote, LogoutParams, request};
    use desk_core::query::{FilledEmails, is_email};
    use gpui::Context;

    use super::{Chat, emails_at};

    impl Chat {
        pub fn filled_email(&self, source: &str, id: i64) -> Option<&str> {
            self.emails.as_ref().ok()?.get(source, id)
        }

        pub fn fill_email(
            &mut self,
            source: &str,
            id: i64,
            typed: &str,
            cx: &mut Context<Self>,
        ) -> Result<(), String> {
            let typed = typed.trim();
            if !typed.is_empty() && !is_email(typed) {
                return Err(format!("{typed} is not an email address"));
            }
            let emails = self.emails.as_mut().map_err(|error| error.clone())?;
            let mut next = emails.clone();
            next.set(source, id, (!typed.is_empty()).then(|| typed.to_owned()));
            save_emails(&next)?;
            *emails = next;
            eprintln!("desk: email for {source} #{id} filled as {typed:?}");
            self.fill_accounts(cx);
            Ok(())
        }

        pub fn add_key(&mut self, provider: &str, key: &str, cx: &mut Context<Self>) {
            let params = LoginKeyParams {
                key: key.trim().to_owned(),
                provider: provider.to_owned(),
            };
            eprintln!("desk: login.key {provider} asked");
            self.request::<request::LoginKey>(&params, cx, Self::keyed);
        }

        pub fn remove_key(&mut self, provider: &str, role: &str, cx: &mut Context<Self>) {
            let params = LogoutParams {
                number: None,
                provider: provider.to_owned(),
                role: role.to_owned(),
            };
            eprintln!("desk: login.logout {provider} {role} asked");
            self.request::<request::LoginLogout>(&params, cx, Self::keyed);
        }

        fn keyed(&mut self, reply: Result<LoginNote, String>, cx: &mut Context<Self>) {
            match reply {
                Ok(note) => eprintln!("desk: tofu said {}", note.note),
                Err(error) => return self.fail(error, cx),
            }
            self.accounts.again();
            self.ask_due(cx);
        }
    }

    fn save_emails(emails: &FilledEmails) -> Result<(), String> {
        let path = emails_at()?;
        let failed = |error: &dyn std::fmt::Display| {
            format!(
                "filled emails cannot be saved to {}: {error}",
                path.display()
            )
        };
        let text = serde_json::to_string_pretty(emails).map_err(|error| failed(&error))?;
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent).map_err(|error| failed(&error))?;
        }
        fs::write(&path, text).map_err(|error| failed(&error))
    }
}
