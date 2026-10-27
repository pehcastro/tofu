mod digest;
mod view;

use super::frame;

use std::collections::BTreeMap;
use std::sync::Arc;
use std::time::Instant;

use crate::modules::chat::Chat;
use desk_core::model::{Session, Store};
use desk_core::protocol::{
    Request, SessionDetail, SessionInfo, SessionParams, SessionTrace, request,
};
use desk_core::query::{Answer, QueryError, Read};
use desk_ui::components::composer::picker;
use desk_ui::components::overlay::{MenuButton, MenuItem};
use desk_ui::components::waterfall::Waterfall;
use gpui::{
    AnyView, App, AppContext, Context, Entity, EntityId, SharedString, Subscription, Task,
    UniformListScrollHandle, WeakEntity, Window,
};

use digest::{Digest, Live, Part, epoch};
use frame::{Gate, load_fonts};

const THIS_FORK: &str = "This fork";
const WHOLE: &str = "Whole session";
const ONWARD: &str = "From a fork onward";
const DAYS_FROM: usize = 12;

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if let Some(board) = board {
        return Err(format!(
            "the session screen draws what tofu session.info and session.trace report, not the board {board}"
        ));
    }
    load_fonts(cx)?;
    Ok(cx
        .new(|cx: &mut Context<SessionScreen>| {
            let scope_menu = MenuButton::new(WHOLE.into(), Vec::new(), cx);
            let screen = cx.weak_entity();
            scope_menu.update(cx, |button, _| {
                button.on_pick(move |at, _, cx| {
                    if let Err(error) = screen.update(cx, |screen, cx| screen.pick_scope(*at, cx)) {
                        eprintln!("desk: session: the scope picker outlived the screen: {error}");
                    }
                });
            });
            let mut screen = SessionScreen {
                trace: cx.new(|_| Waterfall::default()),
                scope_menu,
                picks: Vec::new(),
                scope: Scope::Whole,
                source: None,
                answer: Answer::default(),
                open: None,
                asked_for: None,
                kept: BTreeMap::new(),
                family: None,
                traces: BTreeMap::new(),
                live_trace: None,
                wanted: None,
                digest: None,
                building: None,
                picked: None,
                turns_scroll: UniformListScrollHandle::new(),
                said: String::new(),
                gate: Gate::default(),
            };
            screen.dress_picker(cx);
            screen
        })
        .into())
}

pub struct SessionScreen {
    trace: Entity<Waterfall>,
    scope_menu: Entity<MenuButton>,
    picks: Vec<Option<Scope>>,
    scope: Scope,
    source: Option<Source>,
    answer: Answer<SessionInfo>,
    open: Option<String>,
    asked_for: Option<String>,
    kept: BTreeMap<String, Read<SessionInfo>>,
    family: Option<(String, Loaded<Vec<Fork>>)>,
    traces: BTreeMap<String, Loaded<Arc<SessionTrace>>>,
    live_trace: Option<(String, Arc<SessionTrace>)>,
    wanted: Option<Want>,
    digest: Option<Digest>,
    building: Option<Task<()>>,
    picked: Option<String>,
    turns_scroll: UniformListScrollHandle,
    said: String,
    gate: Gate,
}

struct Source {
    chat: WeakEntity<Chat>,
    id: EntityId,
    _watch: Subscription,
}

#[derive(Clone, Debug, PartialEq, Eq)]
enum Scope {
    Fork,
    Whole,
    From(String),
}

enum Loaded<T> {
    Reading,
    Read(T),
    Failed(String),
}

struct Fork {
    id: String,
    generation: i64,
    at_ms: i64,
    at: String,
    day: String,
    turns: i64,
    cost_usd: f64,
}

#[derive(Clone, PartialEq)]
struct Want {
    open: String,
    scope: Scope,
    loaded: Vec<String>,
    seen: Seen,
}

#[derive(Clone, PartialEq)]
struct Seen {
    trace: Option<String>,
    turns: usize,
    ended: usize,
    messages: usize,
    reports: usize,
    agents: usize,
    agents_ended: usize,
}

impl Seen {
    fn of(trace: Option<&Read<SessionTrace>>, session: Option<&Session>) -> Self {
        Seen {
            trace: trace.map(|read| read.at.clone()),
            turns: session.map_or(0, |session| session.turns.len()),
            ended: session.map_or(0, |session| {
                session
                    .turns
                    .values()
                    .filter(|turn| turn.status.is_some())
                    .count()
            }),
            messages: session.map_or(0, |session| session.messages.len()),
            reports: session.map_or(0, |session| {
                session
                    .agents
                    .values()
                    .filter(|agent| agent.report.is_some())
                    .count()
            }),
            agents: session.map_or(0, |session| session.agents.len()),
            agents_ended: session.map_or(0, |session| {
                session
                    .agents
                    .values()
                    .filter(|agent| agent.ended_at.is_some())
                    .count()
            }),
        }
    }
}

enum Shown<'a> {
    Loading,
    Failed(&'a QueryError),
    Read(&'a Read<SessionInfo>),
}

impl SessionScreen {
    pub fn read_from(&mut self, chat: &Entity<Chat>, cx: &mut Context<Self>) {
        if self
            .source
            .as_ref()
            .is_some_and(|source| source.id == chat.entity_id())
        {
            return;
        }
        let store = chat.read(cx).store().clone();
        let watch = cx.observe(&store, |screen, store, cx| screen.saw(&store, cx));
        self.source = Some(Source {
            chat: chat.downgrade(),
            id: chat.entity_id(),
            _watch: watch,
        });
        self.saw(&store, cx);
        cx.notify();
    }

    fn chat(&self) -> Option<Entity<Chat>> {
        self.source
            .as_ref()
            .and_then(|source| source.chat.upgrade())
    }

    fn saw(&mut self, store: &Entity<Store>, cx: &mut Context<Self>) {
        let Some(chat) = self.chat() else {
            return;
        };
        let open = chat.read(cx).shown_id();
        if let Some(id) = open
            .clone()
            .filter(|id| self.asked_for.as_ref() != Some(id))
        {
            eprintln!("desk: session: asks session.info and session.trace for {id}");
            store.update(cx, |store, _| {
                store.info.again();
                store.trace.want();
                store.trace.again();
            });
            chat.update(cx, |chat, cx| chat.want_info(cx));
            self.asked_for = Some(id);
        }
        let told = {
            let store = store.read(cx);
            if let Some(read) = store
                .info
                .read
                .as_ref()
                .filter(|read| self.kept.get(&read.value.id) != Some(*read))
            {
                self.kept.insert(read.value.id.clone(), read.clone());
            }
            if let Some(read) = store
                .trace
                .read
                .as_ref()
                .filter(|read| Some(&read.value.session) == open.as_ref())
                .filter(|read| {
                    self.live_trace.as_ref().is_none_or(|(at, trace)| {
                        *at != read.at || trace.session != read.value.session
                    })
                })
            {
                self.live_trace = Some((read.at.clone(), Arc::new(read.value.clone())));
            }
            let told = store.info != self.answer;
            if told {
                if self.answer.asking != store.info.asking {
                    eprintln!(
                        "desk: session: {} session.info at {}",
                        if store.info.asking {
                            "asked"
                        } else {
                            "answer to"
                        },
                        frame::stamp()
                    );
                }
                self.answer = store.info.clone();
            }
            told
        };
        let moved = open != self.open;
        if moved {
            self.moved(open, cx);
        }
        if matches!(self.shown(), Shown::Loading) {
            self.gate.wait();
        }
        self.tell_state();
        self.refresh(cx);
        self.ask_traces(cx);
        if told || moved {
            cx.notify();
        }
    }

    fn moved(&mut self, open: Option<String>, cx: &mut Context<Self>) {
        self.open = open;
        self.picked = None;
        self.digest = None;
        self.wanted = None;
        self.building = None;
        if let Scope::From(_) = self.scope {
            self.scope = Scope::Whole;
        }
        let known = self
            .family
            .as_ref()
            .is_some_and(|(of, _)| Some(of) == self.open.as_ref());
        if let (false, Some(open)) = (known, self.open.clone()) {
            self.ask_family(open, cx);
        }
        self.dress_picker(cx);
    }

    fn tell_state(&mut self) {
        let said = match self.shown() {
            Shown::Loading => "loading".to_owned(),
            Shown::Failed(error) => format!("failed: {error}"),
            Shown::Read(read) => format!(
                "{} {}",
                if read.value.turns == 0 {
                    "empty"
                } else {
                    "filled"
                },
                read.value.id
            ),
        };
        if said != self.said {
            eprintln!(
                "desk: session state {said} for open {} asking {}",
                self.open.as_deref().unwrap_or("none"),
                self.answer.asking
            );
            self.said = said;
        }
    }

    fn shown(&self) -> Shown<'_> {
        let Some(open) = &self.open else {
            return Shown::Loading;
        };
        match (self.kept.get(open), &self.answer.failed) {
            (Some(read), _) => Shown::Read(read),
            (None, Some(error)) if !self.answer.asking && self.asked_for.as_ref() == Some(open) => {
                Shown::Failed(error)
            }
            (None, _) => Shown::Loading,
        }
    }

    fn forks(&self) -> &[Fork] {
        match &self.family {
            Some((of, Loaded::Read(forks))) if Some(of) == self.open.as_ref() => forks,
            Some(_) | None => &[],
        }
    }

    fn scoped(&self) -> Vec<(String, i64)> {
        let Some(open) = &self.open else {
            return Vec::new();
        };
        let forks = self.forks();
        let from = match &self.scope {
            Scope::Fork => None,
            Scope::Whole => Some(0),
            Scope::From(id) => forks.iter().position(|fork| fork.id == *id),
        };
        if let Some(forks) = from
            .and_then(|from| forks.get(from..))
            .filter(|forks| !forks.is_empty())
        {
            return forks
                .iter()
                .map(|fork| (fork.id.clone(), fork.at_ms))
                .collect();
        }
        let at = forks
            .iter()
            .find(|fork| fork.id == *open)
            .map(|fork| fork.at_ms)
            .or_else(|| self.kept.get(open).and_then(|read| epoch(&read.value.at)));
        at.map(|at| vec![(open.clone(), at)]).unwrap_or_default()
    }

    fn trace_of(&self, id: &str) -> Option<Arc<SessionTrace>> {
        let live = self
            .live_trace
            .as_ref()
            .filter(|(_, trace)| trace.session == id && self.open.as_deref() == Some(id));
        match (live, self.traces.get(id)) {
            (Some((_, trace)), _) | (None, Some(Loaded::Read(trace))) => Some(trace.clone()),
            (None, Some(Loaded::Reading | Loaded::Failed(_)) | None) => None,
        }
    }

    fn refresh(&mut self, cx: &mut Context<Self>) {
        let (Some(open), Some(chat)) = (self.open.clone(), self.chat()) else {
            return;
        };
        let scoped = self.scoped();
        let parts: Vec<(String, Part)> = scoped
            .iter()
            .filter_map(|(id, at_ms)| {
                let trace = self.trace_of(id)?;
                Some((
                    id.clone(),
                    Part {
                        at_ms: *at_ms,
                        trace,
                    },
                ))
            })
            .collect();
        if parts.is_empty() {
            return;
        }
        let store = chat.read(cx).store().read(cx);
        let session = store.sessions.get(&open);
        let trace = store
            .trace
            .read
            .as_ref()
            .filter(|read| read.value.session == open);
        let want = Want {
            seen: Seen::of(trace, session),
            open,
            scope: self.scope.clone(),
            loaded: parts.iter().map(|(id, _)| id.clone()).collect(),
        };
        if self.wanted.as_ref() == Some(&want) {
            return;
        }
        let since_ms = match self.scope {
            Scope::Whole => None,
            Scope::Fork | Scope::From(_) => scoped.iter().map(|(_, at)| *at).min(),
        };
        let live = scoped
            .iter()
            .any(|(id, _)| *id == want.open)
            .then(|| session.map(|session| Live::of(session, since_ms)))
            .flatten();
        self.wanted = Some(want.clone());
        let parts: Vec<Part> = parts.into_iter().map(|(_, part)| part).collect();
        let now_ms = chrono::Utc::now().timestamp_millis();
        let asked = Instant::now();
        self.building = Some(cx.spawn(async move |screen, cx| {
            let built = cx
                .background_executor()
                .spawn(async move { digest::digest(&parts, live.as_ref(), now_ms) })
                .await;
            let landed = screen.update(cx, |screen, cx| screen.landed(want, built, asked, cx));
            if let Err(error) = landed {
                eprintln!("desk: session closed before its digest landed: {error}");
            }
        }));
    }

    fn landed(&mut self, want: Want, mut digest: Digest, asked: Instant, cx: &mut Context<Self>) {
        if self.wanted.as_ref() != Some(&want) {
            return;
        }
        digest.partial = !digest.totals.agents_live && want.loaded.len() < self.scoped().len();
        eprintln!(
            "desk: session digest for {} {:?}: {} of {} forks read, {} lines, {} sub-agents, {} files, {} calls, {} tokens, {} trace spans over {} ms, built in {} ms",
            want.open,
            want.scope,
            want.loaded.len(),
            self.scoped().len(),
            digest.lines.len(),
            digest.totals.agents,
            digest.totals.files,
            digest.totals.calls,
            digest.totals.tokens,
            digest.trace.len(),
            digest.trace.end_ms(),
            asked.elapsed().as_millis()
        );
        let timeline = digest.trace.clone();
        self.trace.update(cx, |trace, cx| trace.show(timeline, cx));
        self.digest = Some(digest);
        cx.notify();
    }

    fn ask<R: Request, T: Send + 'static>(
        &mut self,
        params: &R::Params,
        shape: fn(R::Result) -> T,
        cx: &mut Context<Self>,
        land: impl FnOnce(&mut Self, Result<T, String>, &mut Context<Self>) + 'static,
    ) {
        let Some(chat) = self.chat() else {
            return land(
                self,
                Err("the work screen that ran tofu is closed".to_owned()),
                cx,
            );
        };
        let (sent, reply) = flume::bounded(1);
        chat.update(cx, |chat, cx| {
            chat.request::<R>(params, cx, move |_, reply, _| {
                if sent.send(reply).is_err() {
                    eprintln!("desk: session closed before {} answered", R::METHOD);
                }
            });
        });
        cx.spawn(async move |screen, cx| {
            let Ok(reply) = reply.recv_async().await else {
                return;
            };
            let shaped = cx
                .background_executor()
                .spawn(async move { reply.map(shape) })
                .await;
            let landed = screen.update(cx, |screen, cx| land(screen, shaped, cx));
            if let Err(error) = landed {
                eprintln!("desk: session closed before {} landed: {error}", R::METHOD);
            }
        })
        .detach();
    }

    fn ask_family(&mut self, open: String, cx: &mut Context<Self>) {
        self.family = Some((open.clone(), Loaded::Reading));
        let params = SessionParams {
            session: Some(open.clone()),
        };
        self.ask::<request::QuerySession, _>(&params, forks_of, cx, move |screen, forks, cx| {
            let landed = match forks {
                Ok(forks) => {
                    eprintln!(
                        "desk: session: query.session lists {} forks for {open}: {}",
                        forks.len(),
                        forks
                            .iter()
                            .map(|fork| format!(
                                "{} gen {} at {}",
                                fork.id, fork.generation, fork.at
                            ))
                            .collect::<Vec<_>>()
                            .join(", ")
                    );
                    Loaded::Read(forks)
                }
                Err(error) => {
                    eprintln!("desk: session: query.session for {open}: {error}");
                    Loaded::Failed(error)
                }
            };
            screen.family = Some((open, landed));
            screen.dress_picker(cx);
            screen.refresh(cx);
            screen.ask_traces(cx);
            cx.notify();
        });
    }

    fn ask_traces(&mut self, cx: &mut Context<Self>) {
        if self
            .traces
            .values()
            .any(|trace| matches!(trace, Loaded::Reading))
        {
            return;
        }
        let Some(open) = self.open.clone() else {
            return;
        };
        let Some(id) = self
            .scoped()
            .into_iter()
            .rev()
            .map(|(id, _)| id)
            .find(|id| *id != open && !self.traces.contains_key(id))
        else {
            return;
        };
        self.traces.insert(id.clone(), Loaded::Reading);
        let asked = Instant::now();
        let params = SessionParams {
            session: Some(id.clone()),
        };
        self.ask::<request::SessionTrace, _>(&params, Arc::new, cx, move |screen, trace, cx| {
            let landed = match trace {
                Ok(trace) => {
                    eprintln!(
                        "desk: session: session.trace for {id} landed in {} ms: {} calls, {} requests, {} sub-agent runs, {} messages",
                        asked.elapsed().as_millis(),
                        trace.calls.len(),
                        trace.requests.len(),
                        trace.agents.len(),
                        trace.messages.len()
                    );
                    Loaded::Read(trace)
                }
                Err(error) => {
                    eprintln!("desk: session: session.trace for {id}: {error}");
                    Loaded::Failed(error)
                }
            };
            screen.traces.insert(id, landed);
            screen.refresh(cx);
            screen.ask_traces(cx);
            cx.notify();
        });
    }

    fn reread(&mut self, cx: &mut Context<Self>) {
        let Some(chat) = self.chat() else {
            return eprintln!("desk: session: the chat is gone, so nothing was asked");
        };
        eprintln!("desk: session: read again");
        self.asked_for.clone_from(&self.open);
        self.traces
            .retain(|_, trace| matches!(trace, Loaded::Read(_)));
        chat.update(cx, |chat, cx| chat.reread_info(cx));
        if let Some(open) = self.open.clone() {
            self.ask_family(open, cx);
        }
    }

    fn pick_scope(&mut self, at: usize, cx: &mut Context<Self>) {
        let Some(Some(scope)) = self.picks.get(at).cloned() else {
            return eprintln!("desk: session: scope row {at} picks nothing");
        };
        eprintln!("desk: session: scope {scope:?}");
        self.scope = scope;
        self.dress_picker(cx);
        self.refresh(cx);
        self.ask_traces(cx);
        cx.notify();
    }

    fn dress_picker(&mut self, cx: &mut Context<Self>) {
        let (items, picks) = scope_items(self.forks());
        let label = SharedString::from(match &self.scope {
            Scope::Fork => THIS_FORK.to_owned(),
            Scope::Whole => WHOLE.to_owned(),
            Scope::From(id) => self.forks().iter().find(|fork| fork.id == *id).map_or_else(
                || ONWARD.to_owned(),
                |fork| format!("From generation {} onward", fork.generation),
            ),
        });
        self.picks = picks;
        self.scope_menu.update(cx, |button, cx| {
            button.items(items, cx);
            button.trigger(move |_, theme| picker("session-scope", label.clone(), theme));
            cx.notify();
        });
    }
}

fn forks_of(detail: SessionDetail) -> Vec<Fork> {
    let mut forks: Vec<Fork> = detail
        .generations
        .into_iter()
        .filter_map(|generation| {
            let at = chrono::DateTime::parse_from_rfc3339(&generation.at).ok()?;
            let local = at.with_timezone(&chrono::Local);
            Some(Fork {
                id: generation.session,
                generation: generation.generation,
                at_ms: at.timestamp_millis(),
                at: local.format("%m-%d %H:%M").to_string(),
                day: local.format("%a %d %b").to_string(),
                turns: generation.turns,
                cost_usd: generation.cost_usd,
            })
        })
        .collect();
    forks.sort_by_key(|fork| fork.generation);
    forks
}

fn scope_items(forks: &[Fork]) -> (Vec<MenuItem>, Vec<Option<Scope>>) {
    let mut items = vec![MenuItem::action(THIS_FORK), MenuItem::action(WHOLE)];
    let mut picks = vec![Some(Scope::Fork), Some(Scope::Whole), None];
    if forks.len() < 2 {
        items.push(MenuItem::action(ONWARD).enabled(false));
        return (items, picks);
    }
    let named =
        |fork: &Fork, at: &str| MenuItem::action(format!("Generation {} · {at}", fork.generation));
    let mut onward = Vec::new();
    match forks.len() > DAYS_FROM {
        true => {
            let mut days: Vec<(&str, Vec<&Fork>)> = Vec::new();
            for fork in forks.iter().rev() {
                match days.last_mut() {
                    Some((day, list)) if *day == fork.day => list.push(fork),
                    Some(_) | None => days.push((fork.day.as_str(), vec![fork])),
                }
            }
            for (day, list) in days {
                picks.push(None);
                picks.extend(list.iter().map(|fork| Some(Scope::From(fork.id.clone()))));
                onward.push(MenuItem::Submenu {
                    label: day.to_owned().into(),
                    icon: None,
                    items: list
                        .iter()
                        .map(|fork| named(fork, fork.at.rsplit(' ').next().unwrap_or(&fork.at)))
                        .collect(),
                });
            }
        }
        false => {
            for fork in forks.iter().rev() {
                picks.push(Some(Scope::From(fork.id.clone())));
                onward.push(named(fork, &fork.at));
            }
        }
    }
    items.push(MenuItem::Submenu {
        label: ONWARD.into(),
        icon: None,
        items: onward,
    });
    (items, picks)
}
