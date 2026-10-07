mod cassette;
mod items;

use std::collections::BTreeMap;
use std::path::PathBuf;
use std::rc::Rc;
use std::time::Instant;
use std::{env, iter, slice, thread};

use desk_core::bridge::{Bridge, BridgeError, Event, serve_command};
use desk_core::model::{Role, Store};
use desk_core::protocol::{
    ApprovalAnswer, ApprovalDecision, ApprovalRequest, InitializeResult, Notification, PROTOCOL,
    Request, RequestId, SessionOpenParams, SessionOpenParamsAsking, TurnCompleted, TurnParams,
    TurnSendParams, TurnSteerParams, request,
};
use desk_ui::components::ask::{Act, Ask, Asking, Question, Shape, ask_bar};
use desk_ui::components::chat::fail;
use desk_ui::components::composer::{Composer, picker};
use desk_ui::components::form::TextArea;
use desk_ui::components::transcript::{Transcript, transcript};
use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Entity, FocusHandle, Focusable, IntoElement,
    KeyDownEvent, Render, SharedString, Task, Window, div, prelude::*, px,
};

use cassette::{Replay, Step};
use items::{Entry, Item};

const CLIENT: &str = "tofu-desk";
const TOFU_VARIABLE: &str = "DESK_TOFU";
const PLACEHOLDER: &str = "Ask tofu to build, inspect, or delegate";
const FIELD_LINES: usize = 8;
const ROW_INSET: f32 = 24.0;
const COLUMN_TOP: f32 = 16.0;
const DOCK_GAP: f32 = 8.0;
const DOCK_INSET: f32 = 16.0;
const DECISIONS: [ApprovalDecision; 3] = [
    ApprovalDecision::AllowOnce,
    ApprovalDecision::RejectOnce,
    ApprovalDecision::AllowAlways,
];

type Opened = Result<(Bridge, InitializeResult), BridgeError>;

enum Link {
    Starting,
    Ready(Bridge),
    Gone,
    Recorded(Replay),
}

enum Opening {
    Closed,
    Pending,
    Open(String),
}

struct Approval {
    key: String,
    id: RequestId,
    asking: Asking,
}

pub struct Chat {
    link: Link,
    store: Store,
    orders: BTreeMap<String, Vec<Entry>>,
    opening: Opening,
    items: Rc<Vec<Item>>,
    transcript: Transcript,
    area: Entity<TextArea>,
    approval: Option<Approval>,
    asking: SessionOpenParamsAsking,
    project: String,
    problem: Option<SharedString>,
    focus: FocusHandle,
    refocus: bool,
    _drain: Option<Task<()>>,
}

pub fn open(board: Option<&str>, window: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    match board {
        None => Ok(live(window, cx).into()),
        Some("36-agents") => {
            let replay = Replay::read()?;
            Ok(cx
                .new(|cx| Chat::new(Link::Recorded(replay), window, cx))
                .into())
        }
        Some(other) => Err(format!(
            "the chat runs a live session, or replays 36-agents, not {other}"
        )),
    }
}

pub fn live(window: &mut Window, cx: &mut App) -> Entity<Chat> {
    cx.new(|cx| {
        let (sender, opened) = flume::bounded::<Opened>(1);
        thread::spawn(move || {
            let tofu =
                env::var_os(TOFU_VARIABLE).map_or_else(|| PathBuf::from("tofu"), PathBuf::from);
            let bridge = env::current_dir()
                .map_err(BridgeError::Io)
                .and_then(|project| Bridge::open(serve_command(&tofu, &project), CLIENT, PROTOCOL));
            sender.send(bridge).ok();
        });
        let mut chat = Chat::new(Link::Starting, window, cx);
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
    fn new(link: Link, window: &mut Window, cx: &mut Context<Self>) -> Self {
        let submit = cx.listener(|chat, text: &str, _, cx| chat.send(text, cx));
        let area = cx.new(|cx| {
            TextArea::new(PLACEHOLDER.into(), window, cx)
                .bare()
                .max_lines(FIELD_LINES)
                .on_submit(submit)
        });
        cx.observe(&area, |_, _, cx| cx.notify()).detach();
        Chat {
            link,
            store: Store::default(),
            orders: BTreeMap::new(),
            opening: Opening::Closed,
            items: Rc::default(),
            transcript: Transcript::new(0),
            area,
            approval: None,
            asking: SessionOpenParamsAsking::Auto,
            project: String::new(),
            problem: None,
            focus: cx.focus_handle(),
            refocus: true,
            _drain: None,
        }
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
        cx.notify();
        Some(events)
    }

    fn stopped(&mut self, cx: &mut Context<Self>) {
        let said = match &self.link {
            Link::Ready(bridge) => bridge.log().pop().unwrap_or_default(),
            Link::Starting | Link::Gone | Link::Recorded(_) => String::new(),
        };
        self.link = Link::Gone;
        self.fail(format!("tofu stopped: {said}"), cx);
    }

    fn fail(&mut self, problem: String, cx: &mut Context<Self>) {
        eprintln!("desk: {problem}");
        self.problem = Some(problem.into());
        cx.notify();
    }

    fn take(&mut self, batch: &[Event], cx: &mut Context<Self>) {
        for event in batch {
            self.apply(event);
        }
        if let (Link::Recorded(_), Opening::Closed) = (&self.link, &self.opening)
            && let Some(id) = self.store.sessions.keys().next()
        {
            self.opening = Opening::Open(id.clone());
        }
        self.refresh(cx);
    }

    fn apply(&mut self, event: &Event) {
        let before: Vec<(String, usize)> = self
            .store
            .sessions
            .iter()
            .map(|(id, session)| (id.clone(), session.messages.len()))
            .collect();
        if let Err(error) = self.store.apply_batch(slice::from_ref(event)) {
            eprintln!("desk: the store refused an event from tofu: {error}");
            return;
        }
        for (id, session) in &self.store.sessions {
            let was = before
                .iter()
                .find(|(seen, _)| seen == id)
                .map_or(0, |(_, length)| *length);
            let order = self.orders.entry(id.clone()).or_default();
            order.extend((was..session.messages.len()).map(Entry::Message));
        }
        let (session, entry) = match event {
            Event::Notification(Notification::ToolStarted(started)) => {
                (&started.session, Entry::Tool(started.item.clone()))
            }
            Event::Notification(Notification::AgentStarted(started)) => {
                (&started.session, Entry::Agent(started.instance.clone()))
            }
            Event::Notification(Notification::TurnCompleted(completed)) => {
                self.said(completed);
                (&completed.session, Entry::Done(completed.turn.clone()))
            }
            Event::Request { request, .. } => {
                eprintln!("desk: tofu asks {request:?}");
                return;
            }
            Event::Notification(_) | Event::Unreadable { .. } => return,
        };
        self.orders.entry(session.clone()).or_default().push(entry);
    }

    fn said(&self, completed: &TurnCompleted) {
        let answer = self
            .store
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

    fn running(&self) -> Option<(String, String)> {
        let Opening::Open(id) = &self.opening else {
            return None;
        };
        let session = self.store.sessions.get(id)?;
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
        let Some(session) = self.store.sessions.get(id) else {
            return;
        };
        let next = items::items(session, self.orders.get(id).map_or(&[], Vec::as_slice));
        let same = self
            .items
            .iter()
            .zip(&next)
            .take_while(|(was, now)| was == now)
            .count();
        if let Some(Item::Lead { text, .. }) = next.get(same) {
            eprintln!("desk: lead row streams {} chars", text.len());
        }
        if same < self.items.len() || next.len() > same {
            self.transcript
                .splice(same..self.items.len(), next.len() - same);
        }
        self.items = Rc::new(next);
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
        let Link::Ready(bridge) = &self.link else {
            self.fail("tofu is not running, so nothing was sent".to_owned(), cx);
            return;
        };
        let pending = match bridge.request::<R>(params) {
            Ok(pending) => pending,
            Err(error) => return self.fail(error.to_string(), cx),
        };
        cx.spawn(async move |this, cx| {
            let reply = pending.reply().await;
            this.update(cx, |chat, cx| match reply {
                Ok(result) => then(chat, result, cx),
                Err(error) => chat.fail(format!("{}: {error}", R::METHOD), cx),
            })
        })
        .detach();
    }

    fn send(&mut self, text: &str, cx: &mut Context<Self>) {
        let text = text.trim().to_owned();
        if text.is_empty() {
            return;
        }
        self.area.update(cx, |area, cx| area.clear(cx));
        self.problem = None;
        match (&self.opening, self.running()) {
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
                    chat.opening = Opening::Open(opened.session);
                    chat.send(&text, cx);
                });
            }
        }
        cx.notify();
    }

    fn stop(&mut self, cx: &mut Context<Self>) {
        let Some((session, turn)) = self.running() else {
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
            Ok(Step::Restart) => {
                self.store = Store::default();
                self.orders.clear();
                self.opening = Opening::Closed;
                self.items = Rc::default();
                self.transcript.reset(0);
            }
            Ok(Step::Report) => cx.quit(),
            Err(error) => {
                eprintln!("desk: the cassette has a bad line: {error}");
                cx.quit();
            }
        }
    }
}

impl Render for Chat {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        self.frame(window, cx);
        if std::mem::take(&mut self.refocus) {
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
        let items = self.items.clone();
        let rows = transcript(
            "chat-transcript",
            &self.transcript,
            &theme,
            move |at, _, cx| {
                let theme = ActiveTheme::theme(cx);
                items.get(at).map_or_else(
                    || div().into_any_element(),
                    |item| {
                        div()
                            .px(px(ROW_INSET))
                            .child(items::render(item, at, &theme))
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
            .busy(self.running().is_some())
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
            Link::Starting | Link::Ready(_) | Link::Gone => None,
        };
        div()
            .track_focus(&self.focus)
            .on_key_down(cx.listener(|chat, event: &KeyDownEvent, _, cx| chat.key(event, cx)))
            .relative()
            .size_full()
            .flex()
            .flex_col()
            .pt(px(COLUMN_TOP))
            .child(div().flex_1().min_h_0().child(rows))
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
