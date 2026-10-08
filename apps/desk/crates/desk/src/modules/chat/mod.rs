pub(crate) mod cassette;
mod items;

use std::collections::BTreeMap;
use std::path::PathBuf;
use std::rc::Rc;
use std::time::Instant;
use std::{env, iter, slice, thread};

use desk_core::bridge::{Bridge, BridgeError, Event, serve_command};
use desk_core::model::{Role, Store};
use desk_core::protocol::{
    ApprovalAnswer, ApprovalDecision, ApprovalRequest, CronCommandParams, InitializeResult,
    NoParams, Notification, PROTOCOL, Request, RequestId, SessionOpenParams,
    SessionOpenParamsAsking, SessionRenameParams, ShellParams, TurnCompleted, TurnParams,
    TurnSendParams, TurnSteerParams, VerbResult, request,
};
use desk_core::query::{self, Answer, ContextReport, Definition, Provider, QueryError, Read};
use desk_core::sessions::{SessionRow, session_rows};
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
    asking: SessionOpenParamsAsking,
    project: String,
    tofu: String,
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
    _drain: Option<Task<()>>,
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

pub fn live(
    project: PathBuf,
    store: Entity<Store>,
    window: &mut Window,
    cx: &mut App,
) -> Entity<Chat> {
    cx.new(|cx| {
        let (sender, opened) = flume::bounded::<Opened>(1);
        thread::spawn(move || {
            let tofu =
                env::var_os(TOFU_VARIABLE).map_or_else(|| PathBuf::from("tofu"), PathBuf::from);
            let bridge = Bridge::open(serve_command(&tofu, &project), CLIENT, PROTOCOL);
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
            asking: SessionOpenParamsAsking::Auto,
            project: String::new(),
            tofu: String::new(),
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
            _drain: None,
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
        self.call::<request::SessionList>(&NoParams {}, cx, |chat, listed, cx| match session_rows(
            listed,
        ) {
            Ok(rows) => {
                eprintln!("desk: session list {} rows", rows.len());
                chat.rows = rows;
                cx.emit(Listed);
            }
            Err(error) => chat.fail(error.to_string(), cx),
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
        self.opening = Opening::Pending;
        let params = SessionOpenParams {
            asking: Some(self.asking.clone()),
            session,
            ..SessionOpenParams::default()
        };
        self.call::<request::SessionOpen>(&params, cx, |chat, opened, cx| {
            eprintln!("desk: session {} opened", opened.session);
            chat.ask_cron(opened.session.clone(), cx);
            chat.opening = Opening::Open(opened.session);
            chat.refresh(cx);
            chat.relist(cx);
        });
        cx.notify();
    }

    pub fn tofu_version(&self) -> &str {
        &self.tofu
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
        self.tofu = hello.tofu;
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
            && let Some(id) = self.store.read(cx).sessions.keys().next()
        {
            self.opening = Opening::Open(id.clone());
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
        self.opening = Opening::Closed;
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
                (&started.session, Entry::Agent(started.instance.clone()))
            }
            Event::Notification(Notification::TurnCompleted(completed)) => {
                self.said(completed, cx);
                cx.emit(Touched);
                if self.open_id() == Some(completed.session.as_str()) {
                    self.reread_context(cx);
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
            Event::Notification(
                notification @ (Notification::QuotaUpdated(_)
                | Notification::ContextUpdated(_)
                | Notification::Decision(_)
                | Notification::CronUpdated(_)),
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

    fn request<R: Request>(
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
                then(
                    chat,
                    reply.map_err(|error| format!("{}: {error}", R::METHOD)),
                    cx,
                )
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

    fn ask_due(&mut self, cx: &mut Context<Self>) {
        if !matches!(self.link, Link::Ready(_)) {
            return;
        }
        self.ask::<request::QueryContext, _>(
            |store| &mut store.context,
            query::context,
            context_said,
            cx,
        );
        self.ask::<request::QueryUsage, _>(
            |store| &mut store.usage,
            query::usage,
            |usage| windows_said(&usage.providers),
            cx,
        );
        self.ask::<request::QueryAgents, _>(
            |store| &mut store.agents,
            query::agents,
            |agents| agents_said(&agents.definitions),
            cx,
        );
        self.ask::<request::QueryRules, _>(
            |store| &mut store.rules,
            query::rules,
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

    fn ask<R, T: 'static>(
        &mut self,
        slot: fn(&mut Store) -> &mut Answer<T>,
        decode: fn(VerbResult) -> Result<Read<T>, QueryError>,
        said: fn(&T) -> String,
        cx: &mut Context<Self>,
    ) where
        R: Request<Params = NoParams, Result = VerbResult>,
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
        self.request::<R>(&NoParams {}, cx, move |chat, reply, cx| {
            let answer = reply
                .map_err(|reason| QueryError::Unanswered {
                    method: R::METHOD,
                    reason,
                })
                .and_then(decode);
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
                self.opening = Opening::Pending;
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
                    chat.opening = Opening::Open(opened.session);
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
        self.call::<request::TurnStop>(&TurnParams { session, turn }, cx, |_, _, _| {});
    }

    fn flip(&mut self, cx: &mut Context<Self>) {
        if !matches!(self.opening, Opening::Closed) {
            return self.fail(
                "asking is fixed once the session opens: tofu serve has no live switch".to_owned(),
                cx,
            );
        }
        self.asking = match self.asking {
            SessionOpenParamsAsking::Auto => SessionOpenParamsAsking::Ask,
            SessionOpenParamsAsking::Ask | SessionOpenParamsAsking::Unknown(_) => {
                SessionOpenParamsAsking::Auto
            }
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

pub fn windows_said(providers: &[Provider]) -> String {
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
        report.ceiling
    )
}

fn agents_said(definitions: &[Definition]) -> String {
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
