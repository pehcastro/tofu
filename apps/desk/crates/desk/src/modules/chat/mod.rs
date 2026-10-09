pub(crate) mod cassette;
mod items;

use std::collections::BTreeMap;
use std::io::ErrorKind;
use std::ops::Range;
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::rc::Rc;
use std::time::{Duration, Instant};
use std::{env, fs, iter, slice, thread};

use desk_core::bridge::{Bridge, BridgeError, Event, serve_command};
use desk_core::model::{Role, Store};
use desk_core::protocol::{
    Accounts, ApprovalAnswer, ApprovalDecision, ApprovalRequest, ContextReport, CredentialReport,
    CronCommandParams, InitializeResult, NoParams, Notification, PROTOCOL, Request, RequestId,
    SessionAsking, SessionInfo, SessionListParams, SessionOpenParams, SessionParams,
    SessionRenameParams, SessionTrace, ShellParams, TurnCompleted, TurnParams, TurnSendParams,
    TurnSteerParams, request, subagent,
};
use desk_core::query::{Answer, FilledEmails, QueryError, Read};
use desk_core::sessions::SessionRow;
use desk_ui::components::ask::{Act, Ask, Asking, Question, Shape, ask_bar};
use desk_ui::components::chat::{FIND_RESERVE, Hit, fail, find_hits, hit_marks};
use desk_ui::components::composer::{Composer, picker};
use desk_ui::components::find::FindBar;
use desk_ui::components::form::TextArea;
use desk_ui::components::transcript::{Transcript, transcript};
use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Entity, EventEmitter, FocusHandle, Focusable,
    IntoElement, KeyDownEvent, MouseDownEvent, Render, SharedString, Task, Window, actions, div,
    prelude::*, px,
};

use cassette::{Replay, Step};
use items::{Entry, Item};

const CLIENT: &str = "tofu-desk";
const TOFU_VARIABLE: &str = "DESK_TOFU";
const DESK_FOLDER: &str = "tofu-desk";
const EMAILS_FILE: &str = "emails.json";
const SIGN_IN_POLL: Duration = Duration::from_millis(500);
const ANCHOR_HOLD: Duration = Duration::from_millis(1600);
#[cfg(windows)]
const CREATE_NO_WINDOW: u32 = 0x0800_0000;
const PLACEHOLDER: &str = "Ask tofu to build, inspect, or delegate";
const FIELD_LINES: usize = 8;
const ROW_INSET: f32 = 24.0;
const CONTENT_WIDTH: f32 = 672.0;
const COLUMN_TOP: f32 = 16.0;
const DOCK_GAP: f32 = 8.0;
const DOCK_INSET: f32 = 16.0;
const DECISIONS: [ApprovalDecision; 3] = [
    ApprovalDecision::AllowOnce,
    ApprovalDecision::RejectOnce,
    ApprovalDecision::AllowAlways,
];

type Opened = Result<(Bridge, InitializeResult), BridgeError>;

actions!(desk, [Find]);

enum Link {
    Starting,
    Ready(Bridge),
    Gone,
    Recorded(Replay),
    Fed,
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

struct Approval {
    key: String,
    id: RequestId,
    asking: Asking,
}

pub struct Chat {
    link: Link,
    store: Entity<Store>,
    orders: BTreeMap<String, Vec<Entry>>,
    slots: Vec<Slot>,
    opening: Opening,
    items: Rc<Vec<Item>>,
    transcript: Transcript,
    area: Entity<TextArea>,
    approval: Option<Approval>,
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
    emails: Result<FilledEmails, String>,
    signing: Option<SigningIn>,
    _signed: Option<Task<()>>,
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
    cx.new(|cx| Chat::new(Link::Fed, store, window, cx))
}

fn emails_at() -> Result<PathBuf, String> {
    let home = |name: &str| {
        env::var_os(name)
            .filter(|value| !value.is_empty())
            .map(PathBuf::from)
    };
    let config = if cfg!(windows) {
        home("APPDATA")
    } else {
        home("XDG_CONFIG_HOME").or_else(|| home("HOME").map(|home| home.join(".config")))
    };
    config
        .map(|config| config.join(DESK_FOLDER).join(EMAILS_FILE))
        .ok_or_else(|| "the desk has no app data folder for filled emails".to_owned())
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
    cx.new(|cx| {
        let (sender, opened) = flume::bounded::<Opened>(1);
        thread::spawn(move || {
            let bridge = Bridge::open(serve_command(&tofu(), &project), CLIENT, PROTOCOL);
            sender.send(bridge).ok();
        });
        let mut chat = Chat::new(Link::Starting, store, window, cx);
        chat._drain = Some(cx.spawn(async move |this, cx| {
            let Ok(opened) = opened.recv_async().await else {
                return;
            };
            let events = this
                .update(cx, |chat, cx| chat.started(opened, cx))
                .ok()
                .flatten();
            let Some(events) = events else {
                return;
            };
            while let Ok(first) = events.recv_async().await {
                let batch: Vec<Event> = iter::once(first).chain(events.try_iter()).collect();
                if this.update(cx, |chat, cx| chat.take(&batch, cx)).is_err() {
                    return;
                }
            }
            this.update(cx, |chat, cx| chat.stopped(cx)).ok();
        }));
        chat
    })
}

impl Approval {
    fn new(id: RequestId, asked: &ApprovalRequest, project: &str) -> Self {
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
        Approval {
            key: asked.approval.clone(),
            id,
            asking: Asking::new(vec![Question {
                header: SharedString::default(),
                text: SharedString::default(),
                shape: Shape::Approval(Ask {
                    tool: asked.tool.clone().into(),
                    command: command.to_owned().into(),
                    writes: project.to_owned().into(),
                    hint: hint.into(),
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
        cx.observe(&area, |_, _, cx| cx.notify()).detach();
        let find = FindBar::new(window, cx);
        let changed = cx.listener(|chat, (query, current): &(String, usize), _, cx| {
            chat.found(query, *current, cx);
        });
        let closed = cx.listener(|chat, _: &(), _, cx| chat.closed(cx));
        find.update(cx, |bar, _| {
            bar.on_change(move |query, current, window, cx| {
                changed(&(query.to_owned(), current), window, cx);
            });
            bar.on_close(move |window, cx| closed(&(), window, cx));
        });
        Chat {
            link,
            store,
            orders: BTreeMap::new(),
            slots: Vec::new(),
            opening: Opening::Closed,
            items: Rc::default(),
            transcript: Transcript::new(0).content_width(px(CONTENT_WIDTH)),
            area,
            approval: None,
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
            emails: load_emails(),
            signing: None,
            _signed: None,
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
        let opened = open.is_some();
        self.store.update(cx, |store, cx| {
            store.open = open;
            if opened {
                store.trace.want();
                store.trace.again();
            }
            cx.notify();
        });
        self.opening = opening;
        if opened {
            self.ask_due(cx);
        }
    }

    pub fn open_id(&self) -> Option<&str> {
        match &self.opening {
            Opening::Open(id) => Some(id),
            Opening::Closed | Opening::Pending => None,
        }
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
        if self.busy(cx) {
            return self.fail(
                "a turn is running: stop it before opening another session".to_owned(),
                cx,
            );
        }
        self.restart(cx);
        self.problem = None;
        self.set_opening(Opening::Pending, cx);
        let params = SessionOpenParams {
            asking: Some(self.asking.clone()),
            session,
            ..SessionOpenParams::default()
        };
        self.call::<request::SessionOpen>(&params, cx, |chat, opened, cx| {
            eprintln!("desk: session {} opened", opened.session);
            chat.ask_cron(opened.session.clone(), cx);
            chat.set_opening(Opening::Open(opened.session), cx);
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
                self.transcript.reveal(hit.item);
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
    }

    fn closed(&mut self, cx: &mut Context<Self>) {
        eprintln!("desk: find closed");
        self.finding = false;
        self.query.clear();
        self.hits = Rc::default();
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
        self.relist(cx);
        self.ask_due(cx);
        cx.notify();
        Some(events)
    }

    fn stopped(&mut self, cx: &mut Context<Self>) {
        let said = match &self.link {
            Link::Ready(bridge) => bridge.log().pop().unwrap_or_default(),
            Link::Starting | Link::Gone | Link::Recorded(_) | Link::Fed => String::new(),
        };
        self.link = Link::Gone;
        self.fail(format!("tofu stopped: {said}"), cx);
    }

    fn fail(&mut self, problem: String, cx: &mut Context<Self>) {
        eprintln!("desk: {problem}");
        self.problem = Some(problem.into());
        cx.notify();
    }

    pub fn take(&mut self, batch: &[Event], cx: &mut Context<Self>) {
        for event in batch {
            self.apply(event, cx);
        }
        self.store.update(cx, |_, cx| cx.notify());
        if let (Link::Recorded(_) | Link::Fed, Opening::Closed) = (&self.link, &self.opening)
            && let Some(id) = self.store.read(cx).sessions.keys().next().cloned()
        {
            self.set_opening(Opening::Open(id), cx);
        }
        self.refresh(cx);
        self.ask_due(cx);
    }

    pub fn restart(&mut self, cx: &mut Context<Self>) {
        self.store.update(cx, |store, cx| {
            store.sessions.clear();
            cx.notify();
        });
        self.orders.clear();
        self.slots.clear();
        self.set_opening(Opening::Closed, cx);
        self.items = Rc::default();
        self.transcript.reset(0);
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

    fn apply(&mut self, event: &Event, cx: &mut Context<Self>) {
        let applied = self.store.update(cx, |store, _| {
            let before: Vec<(String, usize)> = store
                .sessions
                .iter()
                .map(|(id, session)| (id.clone(), session.messages.len()))
                .collect();
            store.apply_batch(slice::from_ref(event)).map(|()| before)
        });
        let before = match applied {
            Ok(before) => before,
            Err(error) => return eprintln!("desk: the store refused an event from tofu: {error}"),
        };
        for (id, session) in &self.store.read(cx).sessions {
            let was = before
                .iter()
                .find(|(seen, _)| seen == id)
                .map_or(0, |(_, length)| *length);
            let order = self.orders.entry(id.clone()).or_default();
            order.extend((was..session.messages.len()).map(Entry::Message));
        }
        let (session, entry) = match event {
            Event::Notification(Notification::TurnStarted(started)) => {
                if let Link::Ready(_) = self.link {
                    self.relist(cx);
                }
                (&started.session, Entry::Task(started.turn.clone()))
            }
            Event::Notification(Notification::SessionUpdated(_)) => {
                if let Link::Ready(_) = self.link {
                    self.relist(cx);
                }
                return;
            }
            Event::Notification(Notification::ToolStarted(started)) => {
                (&started.session, Entry::Tool(started.item.clone()))
            }
            Event::Notification(Notification::AgentStarted(started)) => {
                eprintln!("desk: tofu sent AgentStarted({started:?})");
                (&started.session, Entry::Agent(started.instance.clone()))
            }
            Event::Notification(Notification::TurnCompleted(completed)) => {
                self.said(completed, cx);
                cx.emit(Touched);
                if self.open_id() == Some(completed.session.as_str()) {
                    self.reread_context(cx);
                    self.reread_info(cx);
                }
                if let Link::Ready(_) = self.link {
                    self.relist(cx);
                }
                (&completed.session, Entry::Done(completed.turn.clone()))
            }
            Event::Notification(Notification::ShellStarted(started)) => {
                return eprintln!(
                    "desk: session {} shell {} started pid {}: {}",
                    started.session, started.shell, started.pid, started.command
                );
            }
            Event::Notification(Notification::ShellExited(exited)) => {
                return eprintln!(
                    "desk: session {} shell {} exited code {:?} killed {}",
                    exited.session, exited.shell, exited.exit_code, exited.killed
                );
            }
            Event::Notification(Notification::FileEdit(_)) => return cx.emit(Touched),
            Event::Notification(notification @ Notification::QuotaUpdated(_)) => {
                self.accounts.notified();
                return eprintln!("desk: tofu sent {notification:?}");
            }
            Event::Notification(
                notification @ (Notification::ContextUpdated(_)
                | Notification::Decision(_)
                | Notification::CronUpdated(_)
                | Notification::AgentEnded(_)
                | Notification::UsageUpdated(_)),
            ) => return eprintln!("desk: tofu sent {notification:?}"),
            Event::Request { request, .. } => {
                eprintln!("desk: tofu asks {request:?}");
                return;
            }
            Event::Notification(_) | Event::Unreadable { .. } => return,
        };
        self.orders.entry(session.clone()).or_default().push(entry);
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

    fn refresh(&mut self, cx: &mut Context<Self>) {
        let Opening::Open(id) = &self.opening else {
            return;
        };
        let Some(session) = self.store.read(cx).sessions.get(id) else {
            return;
        };
        let order = self.orders.get(id).map_or(&[][..], Vec::as_slice);
        let shown = Rc::make_mut(&mut self.items);
        let (mut at, mut changed) = (0, false);
        for (index, entry) in order.iter().enumerate() {
            if let Some(slot) = self.slots.get(index)
                && slot.settled
            {
                at += usize::from(slot.item.is_some());
                continue;
            }
            let now = items::item(session, entry);
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
            match (was, now) {
                (None, None) => {}
                (Some(was), Some(now)) if was == now => at += 1,
                (Some(_), Some(now)) => {
                    shown[at] = now;
                    self.transcript.splice(at..at + 1, 1);
                    (at, changed) = (at + 1, true);
                }
                (None, Some(now)) => {
                    shown.insert(at, now);
                    self.transcript.splice(at..at, 1);
                    (at, changed) = (at + 1, true);
                }
                (Some(_), None) => {
                    shown.remove(at);
                    self.transcript.splice(at..at + 1, 0);
                    changed = true;
                }
            }
        }
        let kept = self
            .approval
            .as_ref()
            .is_some_and(|shown| session.approvals.contains_key(&shown.key));
        if !kept {
            let shown = self.approval.take().is_some();
            self.approval = session
                .approvals
                .values()
                .next()
                .map(|(asked_id, asked)| Approval::new(asked_id.clone(), asked, &self.project));
            self.refocus |= shown || self.approval.is_some();
        }
        if changed && !self.query.is_empty() {
            self.search(cx);
        }
        cx.notify();
    }

    fn call<R: Request>(
        &mut self,
        params: &R::Params,
        cx: &mut Context<Self>,
        then: impl FnOnce(&mut Self, R::Result, &mut Context<Self>) + 'static,
    ) where
        R::Result: 'static,
    {
        self.request::<R>(params, cx, |chat, reply, cx| match reply {
            Ok(result) => then(chat, result, cx),
            Err(error) => chat.fail(error, cx),
        });
    }

    pub(crate) fn request<R: Request>(
        &mut self,
        params: &R::Params,
        cx: &mut Context<Self>,
        then: impl FnOnce(&mut Self, Result<R::Result, String>, &mut Context<Self>) + 'static,
    ) where
        R::Result: 'static,
    {
        let Link::Ready(bridge) = &self.link else {
            return then(
                self,
                Err("tofu is not running, so nothing was sent".to_owned()),
                cx,
            );
        };
        let pending = match bridge.request::<R>(params) {
            Ok(pending) => pending,
            Err(error) => return then(self, Err(error.to_string()), cx),
        };
        cx.spawn(async move |this, cx| {
            let reply = pending.reply().await;
            this.update(cx, |chat, cx| {
                let reply = reply.map_err(|error| {
                    eprintln!("desk: {} failed: {error:?}", R::METHOD);
                    error.to_string()
                });
                then(chat, reply, cx)
            })
        })
        .detach();
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
        let open = SessionParams {
            session: self.open_id().map(str::to_owned),
        };
        self.ask::<request::QueryContext>(&open, |store| &mut store.context, context_said, cx);
        self.ask::<request::SessionInfo>(&open, |store| &mut store.info, info_said, cx);
        if open.session.is_some() {
            self.ask::<request::SessionTrace>(&open, |store| &mut store.trace, trace_said, cx);
        }
        let lineage: Vec<String> = self.store.read(cx).lineage.keys().cloned().collect();
        for session in lineage {
            let params = SessionParams {
                session: Some(session.clone()),
            };
            self.ask::<request::SessionInfo>(
                &params,
                move |store| store.lineage.entry(session.clone()).or_default(),
                info_said,
                cx,
            );
        }
        self.ask_accounts(cx);
        self.ask::<request::QueryUsage>(
            &NoParams {},
            |store| &mut store.usage,
            |usage| windows_said(&usage.providers),
            cx,
        );
        self.ask::<request::QueryAgents>(
            &NoParams {},
            |store| &mut store.agents,
            |agents| agents_said(&agents.definitions),
            cx,
        );
        self.ask::<request::QueryRules>(
            &NoParams {},
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
        self.request::<R>(params, cx, move |chat, reply, cx| {
            let answer = reply.map(Read::now).map_err(|reason| QueryError {
                method: R::METHOD,
                reason,
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
        let text = text.trim().to_owned();
        if text.is_empty() {
            return;
        }
        self.area.update(cx, |area, cx| area.clear(cx));
        self.problem = None;
        match (&self.opening, self.running(cx)) {
            (_, Some((session, turn))) => {
                let params = TurnSteerParams {
                    expected_turn_id: turn,
                    session,
                    text,
                };
                self.call::<request::TurnSteer>(&params, cx, |_, _, _| {});
            }
            (Opening::Open(session), None) => {
                let params = TurnSendParams {
                    session: session.clone(),
                    text,
                    ..TurnSendParams::default()
                };
                self.call::<request::TurnSend>(&params, cx, |_, sent, _| {
                    eprintln!("desk: turn {} sent", sent.turn);
                });
            }
            (Opening::Pending, None) => {
                self.fail("the session is still opening".to_owned(), cx);
            }
            (Opening::Closed, None) => {
                self.set_opening(Opening::Pending, cx);
                let params = SessionOpenParams {
                    asking: Some(self.asking.clone()),
                    ..SessionOpenParams::default()
                };
                self.call::<request::SessionOpen>(&params, cx, move |chat, opened, cx| {
                    eprintln!(
                        "desk: session {} opened, asking {}",
                        opened.session,
                        String::from(chat.asking.clone())
                    );
                    chat.ask_cron(opened.session.clone(), cx);
                    chat.set_opening(Opening::Open(opened.session), cx);
                    chat.relist(cx);
                    chat.send(&text, cx);
                });
            }
        }
        cx.notify();
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
            },
            cx,
            |_, _, _| {},
        );
    }

    fn flip(&mut self, cx: &mut Context<Self>) {
        if !matches!(self.opening, Opening::Closed) {
            return self.fail(
                "asking is fixed once the session opens: tofu serve has no live switch".to_owned(),
                cx,
            );
        }
        self.asking = match self.asking {
            SessionAsking::Auto => SessionAsking::Ask,
            SessionAsking::Ask | SessionAsking::Unknown(_) => SessionAsking::Auto,
        };
        eprintln!("desk: asking {}", String::from(self.asking.clone()));
        cx.notify();
    }

    fn act(&mut self, act: Act, cx: &mut Context<Self>) {
        let Some(shown) = &mut self.approval else {
            return;
        };
        let answered = shown.asking.act(act, "", Instant::now());
        cx.notify();
        let (true, Act::Pick(at)) = (answered, act) else {
            return;
        };
        let Some(decision) = DECISIONS.get(at).cloned() else {
            return;
        };
        let Link::Ready(bridge) = &self.link else {
            return self.fail("tofu is not running, so the answer was lost".to_owned(), cx);
        };
        let said = String::from(decision.clone());
        match bridge.answer(&shown.id, &ApprovalAnswer { decision }) {
            Ok(()) => eprintln!("desk: approval {} answered {said}", shown.key),
            Err(error) => self.fail(error.to_string(), cx),
        }
        self.refocus = true;
    }

    fn key(&mut self, event: &KeyDownEvent, cx: &mut Context<Self>) {
        let act = self
            .approval
            .as_ref()
            .and_then(|shown| shown.asking.key(&event.keystroke.key));
        if let Some(act) = act {
            self.act(act, cx);
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
            Err(error) => {
                eprintln!("desk: the cassette has a bad line: {error}");
                cx.quit();
            }
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
        self.frame(window, cx);
        if !self.finding && std::mem::take(&mut self.refocus) {
            match self
                .approval
                .as_ref()
                .is_some_and(|shown| shown.asking.waiting())
            {
                true => self.focus.focus(window, cx),
                false => self.area.focus_handle(cx).focus(window, cx),
            }
        }
        let theme = ActiveTheme::theme(cx);
        let (items, hits, current) = (self.items.clone(), self.hits.clone(), self.current);
        let rows = transcript(
            "chat-transcript",
            &self.transcript,
            &theme,
            move |at, _, cx| {
                let theme = ActiveTheme::theme(cx);
                items.get(at).map_or_else(
                    || div().into_any_element(),
                    |item| {
                        let marks = hit_marks(&hits, current, at, &theme);
                        div()
                            .px(px(ROW_INSET))
                            .child(items::render(item, at, &marks, &theme))
                            .into_any_element()
                    },
                )
            },
        );
        let waiting = self
            .approval
            .as_ref()
            .is_some_and(|shown| shown.asking.waiting());
        let mode = picker("chat-asking", String::from(self.asking.clone()), &theme)
            .on_click(cx.listener(|chat, _: &ClickEvent, _, cx| chat.flip(cx)));
        let stop = cx.listener(|chat, _: &(), _, cx| chat.stop(cx));
        let composer = Composer::new("chat-composer", self.area.clone())
            .content_width(px(CONTENT_WIDTH))
            .busy(self.running(cx).is_some())
            .phase(if waiting { "waiting on you" } else { "working" })
            .effort(mode)
            .on_send(cx.listener(|chat, text: &str, _, cx| chat.send(text, cx)))
            .on_stop(move |window, cx| stop(&(), window, cx));
        let answer = cx.listener(|chat, act: &Act, _, cx| chat.act(*act, cx));
        let ask = self.approval.as_ref().map(|shown| {
            ask_bar(
                "chat-ask",
                &shown.asking,
                None,
                1.0,
                true,
                move |act, window, cx| answer(&act, window, cx),
                &theme,
            )
        });
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
            .on_key_down(cx.listener(|chat, event: &KeyDownEvent, _, cx| chat.key(event, cx)))
            .on_action(cx.listener(Self::open_find))
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
                    .child(rows)
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
