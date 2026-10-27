use std::collections::btree_map::Entry;
use std::collections::{BTreeMap, BTreeSet};
use std::sync::Arc;

use desk_core::model::{Role, Session};
use desk_core::protocol::session::AgentRun;
use desk_core::protocol::{
    AgentState, OriginKind, SessionTrace, TraceCall, TraceMessage, TraceRequest,
    TurnCompletedStatus,
};
use desk_ui::components::waterfall::{Span, SpanKind, Timeline, duration};
use gpui::SharedString;

const LEAD: &str = "orchestrator";
const TARGET_MOST: usize = 80;
const ASKED_MOST: usize = 280;
const SAID_LINES: usize = 4;
const SAID_MOST: usize = 360;
const DID_MOST: usize = 160;
const TASK_FOLLOWS: &str = "\n\n[the task]\n";
const NOTES_AFTER: &str = "\n\n[tofu's notes on this task, not the person's words]\n";
const MEMORY_VIEW: &str = "remembered for you";
const PERSON_ORIGINS: [&str; 5] = [
    "task",
    "typed by the person",
    "steer",
    "question answer",
    "fork task",
];
const ORIGINS_JOINED: &str = " and ";

pub struct Part {
    pub at_ms: i64,
    pub trace: Arc<SessionTrace>,
}

pub struct Live {
    turns: Vec<LiveTurn>,
    told: BTreeSet<String>,
    replies: BTreeMap<String, String>,
    reports: BTreeMap<String, String>,
    agents: usize,
    running: usize,
}

struct LiveTurn {
    id: String,
    task: String,
    person: bool,
    started_at: String,
    state: TurnState,
    worked_ms: i64,
}

impl Live {
    pub fn of(session: &Session, since_ms: Option<i64>) -> Live {
        let agents: Vec<&AgentState> = session
            .agents
            .values()
            .filter(|agent| {
                let started = agent.started_at.as_deref().and_then(epoch);
                since_ms
                    .zip(started)
                    .is_none_or(|(since, started)| started >= since)
            })
            .map(|agent| &agent.state)
            .collect();
        let running = agents
            .iter()
            .filter(|state| match state {
                AgentState::Working
                | AgentState::WaitingAnswer
                | AgentState::InReview
                | AgentState::Reopened => true,
                AgentState::Parked
                | AgentState::Errored
                | AgentState::Finished
                | AgentState::Unknown(_) => false,
            })
            .count();
        let lead = session
            .messages
            .iter()
            .filter(|message| message.agent.is_none());
        let spoken: BTreeSet<&str> = lead
            .clone()
            .filter(|message| matches!(message.role, Role::User | Role::Steer))
            .map(|message| message.turn.as_str())
            .collect();
        let person = |id: &str| {
            session.turns.get(id).is_some_and(|turn| {
                matches!(turn.origin.kind, OriginKind::Person)
                    && (!turn.task.trim().is_empty() || spoken.contains(id))
            })
        };
        let told = lead
            .filter(|message| {
                matches!(
                    message.role,
                    Role::Harness(_) | Role::Inbox | Role::Cron { .. }
                )
            })
            .map(|message| message.turn.clone())
            .filter(|turn| !person(turn))
            .collect();
        let turns = session
            .turns
            .iter()
            .map(|(id, turn)| LiveTurn {
                id: id.clone(),
                task: turn.task.clone(),
                person: person(id),
                started_at: turn.started_at.clone(),
                state: match &turn.status {
                    None => TurnState::Running,
                    Some(TurnCompletedStatus::Finished) => TurnState::Finished,
                    Some(TurnCompletedStatus::Stopped) => TurnState::Stopped,
                    Some(TurnCompletedStatus::Failed) => TurnState::Failed,
                    Some(TurnCompletedStatus::Unknown(_)) => TurnState::Unreported,
                },
                worked_ms: turn.worked_for_ms,
            })
            .collect();
        let replies = session
            .messages
            .iter()
            .filter(|message| matches!(message.role, Role::Assistant) && message.agent.is_none())
            .filter(|message| !message.text.trim().is_empty())
            .map(|message| (message.turn.clone(), message.text.clone()))
            .collect();
        let reports = session
            .agents
            .iter()
            .map(|(id, member)| {
                let did = member.report.as_deref().unwrap_or(&member.task);
                (id.clone(), first_line(did, DID_MOST))
            })
            .filter(|(_, did)| !did.is_empty())
            .collect();
        Live {
            turns,
            told,
            replies,
            reports,
            agents: agents.len(),
            running,
        }
    }
}

#[derive(Default)]
pub struct Digest {
    pub lines: Vec<Line>,
    pub totals: Totals,
    pub trace: Timeline,
    pub dated: bool,
    pub partial: bool,
}

#[derive(Default)]
pub struct Totals {
    pub agents: usize,
    pub agents_live: bool,
    pub running: usize,
    pub files: usize,
    pub edits: usize,
    pub calls: usize,
    pub requests: usize,
    pub tokens: i64,
}

pub enum Line {
    Turn(TurnRow),
    Quiet {
        count: usize,
        from: String,
        to: String,
    },
}

pub struct TurnRow {
    pub id: String,
    pub at: String,
    at_ms: i64,
    pub asked: String,
    pub said: Option<String>,
    pub state: TurnState,
    pub worked_ms: i64,
    pub agents: Vec<AgentRow>,
    pub files: Vec<String>,
    pub calls: usize,
    pub tokens: i64,
    quiet: bool,
}

#[derive(Clone, Copy, PartialEq)]
pub enum TurnState {
    Running,
    Finished,
    Stopped,
    Failed,
    Unreported,
}

pub struct AgentRow {
    pub name: String,
    pub outcome: String,
    pub did: Option<String>,
}

struct Merged<'a> {
    calls: Vec<(i64, &'a TraceCall)>,
    requests: Vec<(i64, &'a TraceRequest)>,
    agents: Vec<Run<'a>>,
    messages: Vec<&'a TraceMessage>,
    asked_by: BTreeMap<&'a str, Option<&'a str>>,
}

struct Run<'a> {
    start_ms: i64,
    end_ms: Option<i64>,
    run: &'a AgentRun,
}

pub fn digest(parts: &[Part], live: Option<&Live>, now_ms: i64) -> Digest {
    let merged = merged(parts);
    let origin_ms = merged
        .calls
        .iter()
        .map(|(at, _)| *at)
        .chain(merged.requests.iter().map(|(at, _)| *at))
        .chain(merged.agents.iter().map(|run| run.start_ms))
        .filter(|at| *at >= 0)
        .min()
        .unwrap_or(now_ms);
    let clock_ms = chrono::DateTime::from_timestamp_millis(origin_ms).map(|at| {
        let offset = at.with_timezone(&chrono::Local).offset().local_minus_utc();
        origin_ms.saturating_add(i64::from(offset).saturating_mul(1_000))
    });
    let edited = edited(&merged.calls);
    let (agents, running) = match live {
        Some(live) => (live.agents, live.running),
        None => (
            merged.agents.len(),
            merged
                .agents
                .iter()
                .filter(|run| run.end_ms.is_none())
                .count(),
        ),
    };
    let totals = Totals {
        agents,
        agents_live: live.is_some(),
        running,
        files: edited
            .iter()
            .map(|(_, path)| path)
            .collect::<BTreeSet<_>>()
            .len(),
        edits: edited
            .iter()
            .map(|(call, _)| call)
            .collect::<BTreeSet<_>>()
            .len(),
        calls: merged.calls.len(),
        requests: merged.requests.len(),
        tokens: merged.requests.iter().fold(0, |sum: i64, (_, request)| {
            sum.saturating_add(request.usage.input_tokens)
                .saturating_add(request.usage.output_tokens)
        }),
    };
    let (lines, dated) = lines(&merged, live, &edited);
    Digest {
        lines,
        totals,
        trace: waterfall(&merged, origin_ms, now_ms, clock_ms),
        dated,
        partial: false,
    }
}

fn merged(parts: &[Part]) -> Merged<'_> {
    let mut calls: BTreeMap<&str, (i64, &TraceCall)> = BTreeMap::new();
    let mut requests: BTreeMap<&str, (i64, &TraceRequest)> = BTreeMap::new();
    let mut agents: BTreeMap<&str, Run<'_>> = BTreeMap::new();
    let mut messages: BTreeMap<&str, &TraceMessage> = BTreeMap::new();
    let mut asked_by: BTreeMap<&str, Option<&str>> = BTreeMap::new();
    for part in parts {
        let trace = part.trace.as_ref();
        for insert in trace
            .inserted
            .iter()
            .filter(|insert| insert.agent.is_none() && insert.role == "user")
        {
            let Some(turn) = insert.turn.as_deref() else {
                continue;
            };
            let person = insert
                .origin
                .split(ORIGINS_JOINED)
                .any(|origin| PERSON_ORIGINS.contains(&origin));
            let asked = asked_by.entry(turn).or_insert(None);
            if person && asked.is_none() {
                *asked = Some(insert.text.as_str());
            }
        }
        for call in &trace.calls {
            let found = (part.at_ms.saturating_add(call.start_ms), call);
            let ended = |call: &TraceCall| call.duration_ms.is_some() || call.outcome.is_some();
            match calls.entry(call.call.as_str()) {
                Entry::Vacant(slot) => {
                    slot.insert(found);
                }
                Entry::Occupied(mut slot) if !ended(slot.get().1) && ended(call) => {
                    slot.insert(found);
                }
                Entry::Occupied(_) => {}
            }
        }
        for request in &trace.requests {
            let found = (part.at_ms.saturating_add(request.start_ms), request);
            match requests.entry(request.request.as_str()) {
                Entry::Vacant(slot) => {
                    slot.insert(found);
                }
                Entry::Occupied(mut slot)
                    if slot.get().1.duration_ms.is_none() && request.duration_ms.is_some() =>
                {
                    slot.insert(found);
                }
                Entry::Occupied(_) => {}
            }
        }
        for run in &trace.agents {
            let found = Run {
                start_ms: epoch(&run.started_at)
                    .unwrap_or_else(|| part.at_ms.saturating_add(run.start_ms.unwrap_or_default())),
                end_ms: run.ended_at.as_deref().and_then(epoch),
                run,
            };
            match agents.entry(run.agent.as_str()) {
                Entry::Vacant(slot) => {
                    slot.insert(found);
                }
                Entry::Occupied(mut slot)
                    if slot.get().end_ms.is_none() && found.end_ms.is_some() =>
                {
                    slot.insert(found);
                }
                Entry::Occupied(_) => {}
            }
        }
        for message in &trace.messages {
            messages.entry(message.id.as_str()).or_insert(message);
        }
    }
    Merged {
        calls: calls.into_values().collect(),
        requests: requests.into_values().collect(),
        agents: agents.into_values().collect(),
        messages: messages.into_values().collect(),
        asked_by,
    }
}

fn edited<'a>(calls: &[(i64, &'a TraceCall)]) -> Vec<(&'a str, String)> {
    calls
        .iter()
        .map(|(_, call)| *call)
        .filter(|call| matches!(call.tool.as_str(), "edit" | "write"))
        .filter(|call| call.refused != Some(true) && call.outcome.as_deref() != Some("failed"))
        .flat_map(|call| {
            let args = call.args.as_ref();
            let one = args
                .and_then(|args| args.get("path"))
                .and_then(serde_json::Value::as_str);
            let many = args
                .and_then(|args| args.get("edits"))
                .and_then(serde_json::Value::as_array)
                .into_iter()
                .flatten()
                .filter_map(|change| change.get("path")?.as_str());
            one.into_iter()
                .chain(many)
                .filter(|path| !path.is_empty())
                .map(|path| (call.call.as_str(), path.replace('\\', "/")))
                .collect::<Vec<_>>()
        })
        .collect()
}

fn lines(merged: &Merged, live: Option<&Live>, edited: &[(&str, String)]) -> (Vec<Line>, bool) {
    let mut turns: BTreeMap<&str, TurnRow> = BTreeMap::new();
    let blank = |id: &str| TurnRow {
        id: id.to_owned(),
        at: String::new(),
        at_ms: 0,
        asked: String::new(),
        said: None,
        state: TurnState::Unreported,
        worked_ms: 0,
        agents: Vec::new(),
        files: Vec::new(),
        calls: 0,
        tokens: 0,
        quiet: false,
    };
    for message in &merged.messages {
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
    for turn in live.iter().flat_map(|live| &live.turns) {
        let row = turns.entry(&turn.id).or_insert_with(|| blank(&turn.id));
        row.at.clone_from(&turn.started_at);
        if !turn.task.trim().is_empty() {
            row.asked.clone_from(&turn.task);
        }
        row.quiet = !turn.person;
        row.state = turn.state;
        row.worked_ms = turn.worked_ms;
    }
    for turn in live.iter().flat_map(|live| &live.told) {
        if let Some(row) = turns.get_mut(turn.as_str()) {
            row.quiet = true;
        }
    }
    for (turn, person) in &merged.asked_by {
        if let Some(row) = turns.get_mut(turn) {
            row.quiet = person.is_none();
            if let Some(text) = person {
                (*text).clone_into(&mut row.asked);
            }
        }
    }
    for (turn, said) in live.iter().flat_map(|live| &live.replies) {
        if let Some(row) = turns.get_mut(turn.as_str()) {
            row.said = Some(said.clone());
        }
    }
    let turn_of: BTreeMap<&str, &str> = merged
        .calls
        .iter()
        .map(|(_, call)| (call.call.as_str(), call.turn.as_str()))
        .collect();
    for (_, call) in &merged.calls {
        if let Some(row) = turns.get_mut(call.turn.as_str()) {
            row.calls = row.calls.saturating_add(1);
        }
    }
    for (call, path) in edited {
        if let Some(row) = turn_of
            .get(call)
            .and_then(|turn| turns.get_mut(*turn))
            .filter(|row| !row.files.contains(path))
        {
            row.files.push(path.clone());
        }
    }
    for (_, request) in &merged.requests {
        if let Some(row) = turns.get_mut(request.turn.as_str()) {
            row.tokens = row
                .tokens
                .saturating_add(request.usage.input_tokens)
                .saturating_add(request.usage.output_tokens);
        }
    }
    let reports = live.map(|live| &live.reports);
    for run in &merged.agents {
        let Some(row) = turns.get_mut(run.run.spawn_turn.as_str()) else {
            continue;
        };
        let lasted = run
            .end_ms
            .map(|end| format!(" in {}", duration(end.saturating_sub(run.start_ms))))
            .unwrap_or_default();
        row.agents.push(AgentRow {
            name: run
                .run
                .definition
                .clone()
                .unwrap_or_else(|| run.run.agent.clone()),
            outcome: format!("{}{lasted}", run.run.status),
            did: reports.and_then(|reports| reports.get(&run.run.agent).cloned()),
        });
    }
    let today = chrono::Local::now().date_naive();
    let mut rows: Vec<TurnRow> = turns
        .into_values()
        .filter(|row| !row.asked.trim().is_empty() || row.said.is_some())
        .map(|mut row| {
            let local = chrono::DateTime::parse_from_rfc3339(&row.at)
                .ok()
                .map(|at| at.with_timezone(&chrono::Local));
            row.at_ms = local.map_or(0, |at| at.timestamp_millis());
            let typed = typed(&row.asked).split_whitespace().collect::<Vec<_>>();
            row.quiet = row.quiet || typed.is_empty();
            row.asked = cut(&typed.join(" "), ASKED_MOST);
            row.said = row.said.as_deref().map(excerpt);
            row.at = local.map_or_else(String::new, |at| {
                at.format(match at.date_naive() == today {
                    true => "%H:%M",
                    false => "%m-%d %H:%M",
                })
                .to_string()
            });
            row
        })
        .collect();
    rows.sort_by_key(|row| std::cmp::Reverse(row.at_ms));
    let dated = rows.iter().any(|row| row.at.len() > "00:00".len());
    let mut lines = Vec::new();
    let mut quiet: Vec<TurnRow> = Vec::new();
    for row in rows {
        if row.quiet {
            quiet.push(row);
            continue;
        }
        lines.extend(fold(std::mem::take(&mut quiet)));
        lines.push(Line::Turn(row));
    }
    lines.extend(fold(quiet));
    (lines, dated)
}

fn fold(quiet: Vec<TurnRow>) -> Option<Line> {
    let newest = quiet.first()?;
    let oldest = quiet.last()?;
    Some(Line::Quiet {
        count: quiet.len(),
        from: oldest.at.clone(),
        to: newest.at.clone(),
    })
}

fn typed(text: &str) -> &str {
    let text = text.split_once(TASK_FOLLOWS).map_or(text, |(_, task)| task);
    let text = text.split_once(NOTES_AFTER).map_or(text, |(task, _)| task);
    let mut rest = text.trim_start();
    while let Some(after) = injected(rest) {
        rest = after.trim_start();
    }
    rest.trim_end()
}

fn injected(text: &str) -> Option<&str> {
    if let Some(tag) = text.strip_prefix('<') {
        let name = tag
            .split(|c: char| c == '>' || c.is_whitespace())
            .next()
            .filter(|name| {
                !name.is_empty()
                    && name
                        .chars()
                        .all(|c| c.is_ascii_alphanumeric() || c == '_' || c == '-')
            })?;
        let close = format!("</{name}>");
        let at = text.find(&close)?;
        return text.get(at.saturating_add(close.len())..);
    }
    let first = text.lines().next().unwrap_or_default().trim();
    let header = first.starts_with('[') && first.ends_with(']');
    (header || text.starts_with(MEMORY_VIEW))
        .then(|| text.split_once("\n\n").map_or("", |(_, after)| after))
}

fn excerpt(said: &str) -> String {
    let lines: Vec<&str> = said
        .trim()
        .lines()
        .map(str::trim_end)
        .filter(|line| !line.trim().is_empty())
        .take(SAID_LINES)
        .collect();
    cut(&lines.join("\n"), SAID_MOST)
}

fn first_line(text: &str, most: usize) -> String {
    cut(text.lines().next().unwrap_or_default().trim(), most)
}

fn cut(text: &str, most: usize) -> String {
    match text.char_indices().nth(most) {
        Some((at, _)) => format!("{}…", text.get(..at).unwrap_or(text)),
        None => text.to_owned(),
    }
}

pub fn epoch(stamp: &str) -> Option<i64> {
    chrono::DateTime::parse_from_rfc3339(stamp)
        .ok()
        .map(|at| at.timestamp_millis())
        .filter(|ms| *ms >= 0)
}

fn waterfall(merged: &Merged, origin_ms: i64, now_ms: i64, clock_ms: Option<i64>) -> Timeline {
    let now = now_ms.saturating_sub(origin_ms);
    let placed = |at: i64| (at >= 0).then(|| at.saturating_sub(origin_ms));
    let running = |start_ms: i64| now.saturating_sub(start_ms).max(0);
    let mut lanes: BTreeMap<&str, Vec<Span>> = BTreeMap::new();
    for (at, request) in &merged.requests {
        let Some(start_ms) = placed(*at) else {
            continue;
        };
        let length_ms = request.duration_ms.unwrap_or(0);
        lanes
            .entry(lane(request.agent.as_deref()))
            .or_default()
            .push(Span {
                name: "LLM call".into(),
                detail: request.model.clone().unwrap_or_default().into(),
                depth: 0,
                start_ms,
                length_ms,
                kind: SpanKind::Model,
                failed: request.error.is_some(),
                live: false,
                label: priced(length_ms, request.cost_usd).into(),
            });
    }
    for (at, call) in &merged.calls {
        let Some(start_ms) = placed(*at) else {
            continue;
        };
        let live = call.outcome.is_none() && call.duration_ms.is_none();
        let length_ms = match (call.duration_ms, live) {
            (Some(ms), _) => ms,
            (None, true) => running(start_ms),
            (None, false) => 0,
        };
        lanes
            .entry(lane(call.agent.as_deref()))
            .or_default()
            .push(Span {
                name: call.tool.clone().into(),
                detail: call.args.as_ref().map(target).unwrap_or_default().into(),
                depth: 0,
                start_ms,
                length_ms,
                kind: SpanKind::Tool,
                failed: call.refused == Some(true) || call.outcome.as_deref() == Some("failed"),
                live,
                label: match live {
                    true => format!("{} running", duration(length_ms)),
                    false => duration(length_ms),
                }
                .into(),
            });
    }
    let spawned_at: BTreeMap<&str, i64> = merged
        .calls
        .iter()
        .filter_map(|(at, call)| Some((call.call.as_str(), placed(*at)?)))
        .collect();
    let lead = lanes.remove(LEAD).unwrap_or_default();
    let head = (!lead.is_empty()).then(|| covering(LEAD.into(), "lead".into(), 0, &lead));
    let mut blocks: Vec<(i64, Vec<Span>)> = lead
        .into_iter()
        .map(|span| (span.start_ms, vec![Span { depth: 1, ..span }]))
        .collect();
    for run in &merged.agents {
        let Some(start_ms) = placed(run.start_ms) else {
            continue;
        };
        let live = run.end_ms.is_none();
        let length_ms = run
            .end_ms
            .map_or_else(|| running(start_ms), |end| end.saturating_sub(run.start_ms));
        let own = Span {
            name: run
                .run
                .definition
                .clone()
                .unwrap_or_else(|| run.run.agent.clone())
                .into(),
            detail: run
                .run
                .model
                .clone()
                .unwrap_or_else(|| run.run.agent.clone())
                .into(),
            depth: usize::try_from(run.run.depth).unwrap_or(0).max(1),
            start_ms,
            length_ms,
            kind: SpanKind::Group,
            failed: run.run.status == "failed",
            live,
            label: match live {
                true => format!("{} running", duration(length_ms)),
                false => priced(length_ms, run.run.cost_usd),
            }
            .into(),
        };
        let at = run
            .run
            .spawn_call
            .as_deref()
            .and_then(|call| spawned_at.get(call))
            .copied()
            .unwrap_or(start_ms);
        let children = lanes.remove(run.run.agent.as_str()).unwrap_or_default();
        blocks.push((at, group(own, children).collect()));
    }
    blocks.sort_by_key(|(at, _)| *at);
    let mut spans: Vec<Span> = head
        .into_iter()
        .chain(blocks.into_iter().flat_map(|(_, block)| block))
        .collect();
    for (agent, children) in lanes {
        let head = covering(agent.to_owned().into(), "no agent run".into(), 1, &children);
        spans.extend(group(head, children));
    }
    let live = spans.iter().any(|span| span.live);
    Timeline::new(spans, live.then_some(now), clock_ms)
}

fn covering(name: SharedString, detail: SharedString, depth: usize, children: &[Span]) -> Span {
    let start_ms = children.iter().map(|span| span.start_ms).min().unwrap_or(0);
    let end_ms = children
        .iter()
        .map(|span| span.start_ms.saturating_add(span.length_ms.max(0)))
        .max()
        .unwrap_or(start_ms);
    let length_ms = end_ms.saturating_sub(start_ms);
    Span {
        name,
        detail,
        depth,
        start_ms,
        length_ms,
        kind: SpanKind::Group,
        failed: false,
        live: children.iter().any(|span| span.live),
        label: duration(length_ms).into(),
    }
}

fn group(head: Span, mut children: Vec<Span>) -> impl Iterator<Item = Span> {
    children.sort_by_key(|span| span.start_ms);
    let below = head.depth.saturating_add(1);
    std::iter::once(head).chain(children.into_iter().map(move |span| Span {
        depth: below,
        ..span
    }))
}

fn lane(agent: Option<&str>) -> &str {
    agent.filter(|agent| !agent.is_empty()).unwrap_or(LEAD)
}

fn priced(length_ms: i64, cost_usd: f64) -> String {
    match cost_usd > 0.0 {
        true => format!("{} · ${cost_usd:.2}", duration(length_ms)),
        false => duration(length_ms),
    }
}

fn target(args: &serde_json::Value) -> String {
    let said = args
        .as_object()
        .and_then(|args| args.values().find_map(serde_json::Value::as_str))
        .or_else(|| args.as_str())
        .unwrap_or_default();
    first_line(said, TARGET_MOST)
}
