use super::frame;

use std::collections::BTreeMap;

use crate::modules::chat::Chat;
use desk_core::model::{Role, Session, Store};
use desk_core::protocol::{HunkLineKind, SessionInfo, SessionTrace, TurnCompletedStatus};
use desk_core::query::{Answer, QueryError, Read};
use desk_ui::components::avatar::spinner;
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{caption, inner_card, outer_card};
use desk_ui::components::chip::mono;
use desk_ui::components::empty::{EmptyAction, empty_state};
use desk_ui::components::list::row;
use desk_ui::components::paint::ink;
use desk_ui::components::size::{CHIP_FILL, T2, T3};
use desk_ui::components::skeleton::{pulse, skeleton_bar, skeleton_lines};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, AnyView, App, AppContext, ClickEvent, Context, Div, Entity, EntityId, FontWeight,
    Rgba, SharedString, Subscription, WeakEntity, Window, div, prelude::*, px, relative,
};

use frame::{ellipsis, fraction, load_fonts, note, panel, panes, title, window};

const NO_TOFU: &str = "The session reads session.info and session.trace from the tofu the work screen runs, and no work screen is open here.";
const COUNT_LEAST: f32 = 150.0;
const TURNS_LEAST: f32 = 380.0;
const DETAIL_LEAST: f32 = 320.0;
const FIGURE: f32 = 24.0;
const TIME_WIDTH: f32 = 44.0;
const SAID_MOST: usize = 420;
const SKELETON_FIGURE: f32 = 22.0;

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if let Some(board) = board {
        return Err(format!(
            "the session screen draws what tofu session.info and session.trace report, not the board {board}"
        ));
    }
    load_fonts(cx)?;
    Ok(cx
        .new(|_| SessionScreen {
            source: None,
            answer: Answer::default(),
            open: None,
            switching: false,
            kept: BTreeMap::new(),
            digests: BTreeMap::new(),
            picked: None,
            said: String::new(),
        })
        .into())
}

pub struct SessionScreen {
    source: Option<Source>,
    answer: Answer<SessionInfo>,
    open: Option<String>,
    switching: bool,
    kept: BTreeMap<String, Read<SessionInfo>>,
    digests: BTreeMap<String, Digest>,
    picked: Option<String>,
    said: String,
}

struct Source {
    chat: WeakEntity<Chat>,
    id: EntityId,
    _watch: Subscription,
}

enum Shown<'a> {
    Loading,
    Failed(&'a QueryError),
    Read(&'a Read<SessionInfo>),
}

#[derive(Default, PartialEq)]
struct Digest {
    turns: Vec<TurnRow>,
    tokens: i64,
    files: usize,
    added: usize,
    removed: usize,
}

#[derive(PartialEq)]
struct TurnRow {
    id: String,
    at: String,
    asked: String,
    said: Option<String>,
    state: TurnState,
    worked_ms: i64,
    agents: Vec<AgentRow>,
    files: Vec<String>,
    calls: usize,
    tokens: i64,
}

#[derive(Clone, Copy, PartialEq)]
enum TurnState {
    Running,
    Finished,
    Stopped,
    Failed,
    Unreported,
}

#[derive(PartialEq)]
struct AgentRow {
    name: String,
    outcome: String,
    did: Option<String>,
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
        chat.update(cx, |chat, cx| chat.want_info(cx));
        self.saw(&store, cx);
        cx.notify();
    }

    fn saw(&mut self, store: &Entity<Store>, cx: &mut Context<Self>) {
        let open = self
            .source
            .as_ref()
            .and_then(|source| source.chat.upgrade())
            .and_then(|chat| chat.read(cx).open_id().map(str::to_owned));
        let store = store.read(cx);
        let info = &store.info;
        if let Some(read) = &info.read {
            self.kept.insert(read.value.id.clone(), read.clone());
        }
        let traced = &store.trace;
        let gave_up = traced.failed.is_some() && !traced.asking;
        let digest = open.as_deref().and_then(|id| {
            let trace = traced
                .read
                .as_ref()
                .map(|read| &read.value)
                .filter(|trace| trace.session == id);
            (trace.is_some() || gave_up).then(|| digest(store.sessions.get(id), trace))
        });
        let fresh = digest.is_some()
            && open
                .as_deref()
                .is_some_and(|id| self.digests.get(id) != digest.as_ref());
        if *info == self.answer && open == self.open && !fresh {
            return;
        }
        if let (Some(id), Some(digest)) = (&open, digest) {
            eprintln!(
                "desk: session digest for {id}: {} turns, {} files, {} tokens",
                digest.turns.len(),
                digest.files,
                digest.tokens
            );
            self.digests.insert(id.clone(), digest);
        }
        if open != self.open {
            self.picked = None;
        }
        self.switching = match open {
            Some(_) => false,
            None => self.switching || self.open.is_some(),
        };
        self.answer = info.clone();
        self.open = open;
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
        cx.notify();
    }

    fn shown(&self) -> Shown<'_> {
        let kept = match &self.open {
            Some(open) => self.kept.get(open),
            None if self.switching => None,
            None => self.answer.read.as_ref(),
        };
        match (kept, &self.answer.failed) {
            (Some(read), _) => Shown::Read(read),
            (None, Some(error)) if !self.answer.asking && !self.switching => Shown::Failed(error),
            (None, _) => Shown::Loading,
        }
    }

    fn reread(&mut self, cx: &mut Context<Self>) {
        let Some(chat) = self
            .source
            .as_ref()
            .and_then(|source| source.chat.upgrade())
        else {
            return eprintln!("desk: session: the chat is gone, so nothing was asked");
        };
        eprintln!("desk: session: read again");
        chat.update(cx, |chat, cx| chat.reread_info(cx));
    }

    fn header(&self, info: Option<&SessionInfo>, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let about = info.map(|info| {
            let mut said = vec![format!("started {}", clock(&info.at))];
            said.extend(info.model.clone());
            said.extend(info.outcome.clone());
            if let (Some(at), Some(of)) = (info.generation, info.generations) {
                said.push(format!("generation {at} of {of}"));
            }
            said.join(" · ")
        });
        div()
            .flex()
            .flex_none()
            .flex_wrap()
            .items_center()
            .gap(px(10.0))
            .px(px(4.0))
            .child(title("Session"))
            .children(info.map(|info| {
                div()
                    .font_weight(FontWeight::SEMIBOLD)
                    .child(info.name.clone().unwrap_or_else(|| info.handle.clone()))
            }))
            .children(about.map(|about| ellipsis(note(about, theme))))
            .child(div().flex_1())
            .when(self.answer.asking, |header| {
                header.child(spinner("session-asking", theme))
            })
            .child(
                button(
                    "session-reread",
                    "Read again",
                    None,
                    ButtonKind::Plain,
                    theme,
                )
                .on_click(cx.listener(|screen, _: &ClickEvent, _, cx| screen.reread(cx))),
            )
    }

    fn failed(&self, error: &QueryError, theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
        let again = cx.listener(|screen, _: &(), _, cx| screen.reread(cx));
        empty_state(
            "session-failed",
            "The session could not be read",
            Some(error.to_string().into()),
            &[EmptyAction {
                label: "Try again".into(),
                glyph: None,
                keys: None,
            }],
            &[],
            theme,
            move |_, window, cx| again(&(), window, cx),
        )
        .into_any_element()
    }

    fn body(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let read = match self.shown() {
            Shown::Read(read) => read,
            Shown::Loading => {
                return div()
                    .flex()
                    .flex_col()
                    .gap(px(8.0))
                    .child(self.header(None, theme, cx))
                    .child(loading(theme, cx));
            }
            Shown::Failed(error) => {
                return div()
                    .flex()
                    .flex_col()
                    .gap(px(8.0))
                    .child(self.header(None, theme, cx))
                    .child(self.failed(error, theme, cx));
            }
        };
        let info = &read.value;
        let digest = self.digests.get(&info.id);
        let page = div()
            .flex()
            .flex_col()
            .gap(px(8.0))
            .child(self.header(Some(info), theme, cx))
            .children(
                self.answer
                    .failed
                    .as_ref()
                    .filter(|_| !self.answer.asking)
                    .map(|error| warn(format!("the last read failed: {error}"), theme)),
            )
            .child(
                panes().flex_none().children(
                    counts(info, digest)
                        .into_iter()
                        .map(|(label, value, aside)| count(label, value, aside, theme)),
                ),
            );
        let Some(digest) = digest else {
            return page.child(loading_turns(theme, cx));
        };
        if digest.turns.is_empty() {
            return page.child(empty_state(
                "session-no-turns",
                "No turns yet",
                Some("session.trace reported no turn for this session".into()),
                &[],
                &[],
                theme,
                |_, _, _| {},
            ));
        }
        let picked = self
            .picked
            .as_deref()
            .and_then(|id| digest.turns.iter().find(|turn| turn.id == id))
            .or_else(|| digest.turns.first());
        page.child(
            panes()
                .flex_1()
                .child(self.turns(digest, picked.map(|turn| turn.id.as_str()), theme, cx))
                .children(picked.map(|turn| detail(turn, theme))),
        )
    }

    fn turns(
        &self,
        digest: &Digest,
        picked: Option<&str>,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Div {
        panel(
            "Turns",
            Some(note("newest first, click one", theme).into_any_element()),
            theme,
        )
        .map(|panel| fraction(panel, 1.4, TURNS_LEAST))
        .child(
            inner_card(theme)
                .p(px(6.0))
                .gap(px(2.0))
                .text_size(px(13.5))
                .children(digest.turns.iter().enumerate().map(|(at, turn)| {
                    let id = turn.id.clone();
                    row(
                        ("session-turn", at),
                        picked == Some(turn.id.as_str()),
                        false,
                        theme,
                    )
                    .flex()
                    .items_center()
                    .gap(px(12.0))
                    .px(px(10.0))
                    .py(px(7.0))
                    .child(
                        div()
                            .flex_none()
                            .w(px(TIME_WIDTH))
                            .font_family(mono(theme))
                            .text_color(ink(theme, T3))
                            .child(turn.at.clone()),
                    )
                    .child(ellipsis(div().flex_1()).child(turn.asked.clone()))
                    .child(
                        div()
                            .flex_none()
                            .text_size(px(12.0))
                            .text_color(ink(theme, T3))
                            .child(did(turn)),
                    )
                    .child(
                        div()
                            .flex_none()
                            .text_size(px(12.0))
                            .text_color(state_ink(turn.state, theme))
                            .child(state_said(turn.state)),
                    )
                    .on_click(cx.listener(
                        move |screen, _: &ClickEvent, _, cx| {
                            screen.picked = Some(id.clone());
                            cx.notify();
                        },
                    ))
                })),
        )
    }
}

fn digest(session: Option<&Session>, trace: Option<&SessionTrace>) -> Digest {
    let mut turns: BTreeMap<&str, TurnRow> = BTreeMap::new();
    let blank = |id: &str| TurnRow {
        id: id.to_owned(),
        at: String::new(),
        asked: String::new(),
        said: None,
        state: TurnState::Unreported,
        worked_ms: 0,
        agents: Vec::new(),
        files: Vec::new(),
        calls: 0,
        tokens: 0,
    };
    for (id, turn) in session.iter().flat_map(|session| &session.turns) {
        let row = turns.entry(id).or_insert_with(|| blank(id));
        row.at.clone_from(&turn.started_at);
        row.asked.clone_from(&turn.task);
        row.worked_ms = turn.worked_for_ms;
        row.state = match &turn.status {
            None => TurnState::Running,
            Some(TurnCompletedStatus::Finished) => TurnState::Finished,
            Some(TurnCompletedStatus::Stopped) => TurnState::Stopped,
            Some(TurnCompletedStatus::Failed) => TurnState::Failed,
            Some(TurnCompletedStatus::Unknown(_)) => TurnState::Unreported,
        };
    }
    for message in session.iter().flat_map(|session| &session.messages) {
        if let (Role::Assistant, true) = (&message.role, message.agent.is_none()) {
            let row = turns
                .entry(&message.turn)
                .or_insert_with(|| blank(&message.turn));
            row.said = Some(message.text.clone()).filter(|text| !text.trim().is_empty());
        }
    }
    let mut added = 0;
    let mut removed = 0;
    let mut files = 0;
    for (path, edits) in session.iter().flat_map(|session| &session.files) {
        files += 1;
        for edit in edits {
            let row = turns.entry(&edit.turn).or_insert_with(|| blank(&edit.turn));
            if !row.files.contains(path) {
                row.files.push(path.clone());
            }
            for line in edit.hunks.iter().flat_map(|hunk| &hunk.lines) {
                match line.kind {
                    HunkLineKind::Added => added += 1,
                    HunkLineKind::Removed => removed += 1,
                    HunkLineKind::Context | HunkLineKind::Unknown(_) => {}
                }
            }
        }
    }
    let mut tokens: i64 = 0;
    if let Some(trace) = trace {
        for message in &trace.messages {
            let row = turns
                .entry(&message.turn)
                .or_insert_with(|| blank(&message.turn));
            if row.at.is_empty() {
                row.at.clone_from(&message.at);
            }
            match message.role.as_str() {
                "user" if row.asked.is_empty() => row.asked.clone_from(&message.text),
                "assistant" if !message.text.trim().is_empty() => {
                    row.said = Some(message.text.clone());
                }
                _ => {}
            }
        }
        for call in &trace.calls {
            let row = turns.entry(&call.turn).or_insert_with(|| blank(&call.turn));
            row.calls += 1;
        }
        for request in &trace.requests {
            let used = request
                .usage
                .input_tokens
                .saturating_add(request.usage.output_tokens);
            tokens = tokens.saturating_add(used);
            let row = turns
                .entry(&request.turn)
                .or_insert_with(|| blank(&request.turn));
            row.tokens = row.tokens.saturating_add(used);
        }
        for run in &trace.agents {
            let member = session.and_then(|session| session.agents.get(&run.agent));
            let did = member
                .and_then(|member| member.report.clone().or_else(|| Some(member.task.clone())))
                .filter(|did| !did.trim().is_empty());
            let lasted = run
                .ended_at
                .as_deref()
                .and_then(|ended| between(&run.started_at, ended))
                .map_or_else(String::new, |lasted| format!(" in {lasted}"));
            let row = turns
                .entry(&run.spawn_turn)
                .or_insert_with(|| blank(&run.spawn_turn));
            row.agents.push(AgentRow {
                name: run.definition.clone().unwrap_or_else(|| run.agent.clone()),
                outcome: format!("{}{lasted}", run.status),
                did,
            });
        }
    }
    let mut turns: Vec<TurnRow> = turns
        .into_values()
        .filter(|turn| !turn.asked.is_empty() || turn.said.is_some())
        .collect();
    turns.sort_by(|a, b| b.at.cmp(&a.at));
    for turn in &mut turns {
        turn.at = clock(&turn.at);
        turn.asked = turn.asked.split_whitespace().collect::<Vec<_>>().join(" ");
    }
    Digest {
        turns,
        tokens,
        files,
        added,
        removed,
    }
}

fn counts(
    info: &SessionInfo,
    digest: Option<&Digest>,
) -> Vec<(&'static str, String, Option<String>)> {
    let mut counts = vec![
        (
            "Turns",
            grouped(info.turns),
            Some(format!("{} steps", grouped(info.steps))),
        ),
        ("Sub-agents", grouped(info.sub_agents), None),
    ];
    if let Some(digest) = digest {
        counts.push((
            "Files changed",
            grouped(digest.files as i64),
            Some(format!("+{} -{}", digest.added, digest.removed)),
        ));
        counts.push((
            "Tokens",
            compact(digest.tokens),
            info.cost_usd.map(|cost| format!("${cost:.2}")),
        ));
    }
    counts
}

fn count(label: &'static str, value: String, aside: Option<String>, theme: &Theme) -> Div {
    fraction(outer_card(theme), 1.0, COUNT_LEAST)
        .flex_col()
        .px(px(3.0))
        .pb(px(3.0))
        .child(
            div()
                .h(px(30.0))
                .flex()
                .items_center()
                .pl(px(9.0))
                .child(caption(label, theme)),
        )
        .child(
            inner_card(theme)
                .flex_row()
                .items_baseline()
                .gap(px(8.0))
                .px(px(14.0))
                .py(px(10.0))
                .child(
                    div()
                        .text_size(px(FIGURE))
                        .line_height(relative(1.0))
                        .font_weight(FontWeight::SEMIBOLD)
                        .child(value),
                )
                .children(aside.map(|aside| ellipsis(note(aside, theme)))),
        )
}

fn detail(turn: &TurnRow, theme: &Theme) -> Div {
    let mut about = vec![turn.at.clone()];
    if turn.worked_ms > 0 {
        about.push(format!("worked {}", lasted(turn.worked_ms)));
    }
    if turn.tokens > 0 {
        about.push(format!("{} tokens", compact(turn.tokens)));
    }
    let said = turn.said.as_deref().map(|said| {
        let said = said.trim();
        match said.char_indices().nth(SAID_MOST) {
            Some((cut, _)) => format!("{}…", said.get(..cut).unwrap_or(said)),
            None => said.to_owned(),
        }
    });
    let section = |name: &'static str| div().pt(px(6.0)).child(caption(name, theme));
    panel(
        turn.asked.clone(),
        Some(note(about.join(" · "), theme).into_any_element()),
        theme,
    )
    .map(|panel| fraction(panel, 1.0, DETAIL_LEAST))
    .child(
        inner_card(theme)
            .flex_1()
            .px(px(16.0))
            .py(px(14.0))
            .gap(px(8.0))
            .text_size(px(13.5))
            .child(match said {
                Some(said) => div().text_color(ink(theme, T2)).child(said),
                None => note("the lead has not answered in this turn yet", theme),
            })
            .when(!turn.agents.is_empty(), |card| {
                card.child(section("Sub-agents"))
                    .children(turn.agents.iter().map(|agent| {
                        div()
                            .flex()
                            .flex_col()
                            .gap(px(2.0))
                            .child(
                                div()
                                    .flex()
                                    .gap(px(8.0))
                                    .child(
                                        div()
                                            .font_weight(FontWeight::SEMIBOLD)
                                            .child(agent.name.clone()),
                                    )
                                    .child(note(agent.outcome.clone(), theme)),
                            )
                            .children(agent.did.clone().map(|did| ellipsis(note(did, theme))))
                    }))
            })
            .when(!turn.files.is_empty(), |card| {
                card.child(section("Files"))
                    .child(
                        div()
                            .flex()
                            .flex_wrap()
                            .gap(px(6.0))
                            .children(turn.files.iter().map(|path| {
                                div()
                                    .px(px(8.0))
                                    .py(px(3.0))
                                    .rounded(px(6.0))
                                    .bg(ink(theme, CHIP_FILL))
                                    .font_family(mono(theme))
                                    .text_size(px(12.0))
                                    .child(file_name(path))
                            })),
                    )
            })
            .when(
                turn.agents.is_empty() && turn.files.is_empty() && turn.calls > 0,
                |card| {
                    card.child(note(
                        format!("{} tool calls, no sub-agent, no file edit", turn.calls),
                        theme,
                    ))
                },
            ),
    )
}

fn did(turn: &TurnRow) -> String {
    let mut said = Vec::new();
    match turn.agents.len() {
        0 => {}
        1 => said.push("1 sub-agent".to_owned()),
        many => said.push(format!("{many} sub-agents")),
    }
    match turn.files.len() {
        0 => {}
        1 => said.push("1 file".to_owned()),
        many => said.push(format!("{many} files")),
    }
    if said.is_empty() && turn.calls > 0 {
        said.push(format!("{} calls", turn.calls));
    }
    if said.is_empty() {
        return "no edits".to_owned();
    }
    said.join(" · ")
}

fn state_said(state: TurnState) -> &'static str {
    match state {
        TurnState::Running => "running",
        TurnState::Finished => "finished",
        TurnState::Stopped => "stopped",
        TurnState::Failed => "failed",
        TurnState::Unreported => "",
    }
}

fn state_ink(state: TurnState, theme: &Theme) -> Rgba {
    match state {
        TurnState::Running => theme.color(ColorToken::StatusLive),
        TurnState::Failed => theme.color(ColorToken::StatusDanger),
        TurnState::Finished | TurnState::Stopped | TurnState::Unreported => ink(theme, T3),
    }
}

fn file_name(path: &str) -> String {
    path.rsplit(['/', '\\']).next().unwrap_or(path).to_owned()
}

fn clock(at: &str) -> String {
    chrono::DateTime::parse_from_rfc3339(at).map_or_else(
        |_| at.to_owned(),
        |at| at.with_timezone(&chrono::Local).format("%H:%M").to_string(),
    )
}

fn between(from: &str, to: &str) -> Option<String> {
    let from = chrono::DateTime::parse_from_rfc3339(from).ok()?;
    let to = chrono::DateTime::parse_from_rfc3339(to).ok()?;
    Some(lasted(to.signed_duration_since(from).num_milliseconds()))
}

fn lasted(ms: i64) -> String {
    let seconds = ms.max(0) / 1000;
    match seconds {
        0..60 => format!("{seconds}s"),
        60..3600 => format!("{}m", seconds / 60),
        _ => format!("{}h {}m", seconds / 3600, seconds % 3600 / 60),
    }
}

fn compact(count: i64) -> String {
    match count {
        ..1_000 => count.to_string(),
        1_000..1_000_000 => format!("{:.1}k", count as f64 / 1e3),
        _ => format!("{:.1}M", count as f64 / 1e6),
    }
}

fn grouped(count: i64) -> String {
    let digits = count.unsigned_abs().to_string();
    let mut said = String::with_capacity(digits.len() + digits.len() / 3 + 1);
    if count < 0 {
        said.push('-');
    }
    for (at, digit) in digits.chars().enumerate() {
        if at > 0 && (digits.len() - at).is_multiple_of(3) {
            said.push(',');
        }
        said.push(digit);
    }
    said
}

fn loading(theme: &Theme, cx: &App) -> Div {
    div()
        .flex()
        .flex_col()
        .gap(px(8.0))
        .child(panes().flex_none().children((0..4).map(|at| {
            fraction(inner_card(theme), 1.0, COUNT_LEAST)
                .px(px(14.0))
                .py(px(12.0))
                .gap(px(10.0))
                .child(pulse(
                    "session-count-label",
                    at,
                    skeleton_bar(0.4, 8.0, theme),
                    cx,
                ))
                .child(pulse(
                    "session-count",
                    at,
                    skeleton_bar(0.3, SKELETON_FIGURE, theme),
                    cx,
                ))
        })))
        .child(loading_turns(theme, cx))
}

fn loading_turns(theme: &Theme, cx: &App) -> Div {
    panes()
        .child(
            fraction(inner_card(theme), 1.4, TURNS_LEAST)
                .px(px(16.0))
                .py(px(14.0))
                .child(skeleton_lines("session-turns", 6, theme, cx)),
        )
        .child(
            fraction(inner_card(theme), 1.0, DETAIL_LEAST)
                .px(px(16.0))
                .py(px(14.0))
                .child(skeleton_lines("session-detail", 5, theme, cx)),
        )
}

fn warn(text: impl Into<SharedString>, theme: &Theme) -> Div {
    note(text, theme).text_color(theme.color(ColorToken::StatusWarn))
}

impl Render for SessionScreen {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let body = match self.source {
            None => empty_state(
                "session-no-tofu",
                "No tofu to ask",
                Some(NO_TOFU.into()),
                &[],
                &[],
                &theme,
                |_, _, _| {},
            )
            .into_any_element(),
            Some(_) => self.body(&theme, cx).into_any_element(),
        };
        window(&theme, div().flex_1().flex().flex_col().child(body))
    }
}
