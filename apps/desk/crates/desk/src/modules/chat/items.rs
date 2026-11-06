use std::cell::RefCell;
use std::collections::BTreeMap;
use std::env;
use std::path::PathBuf;
use std::rc::Rc;
use std::time::{SystemTime, UNIX_EPOCH};

use desk_core::model::{Agent as SubAgent, Role, Session, Tool};
use desk_core::protocol::{
    AgentState, ApprovalResolved, CardPiece, Family, Notification, OriginKind,
    ToolCard as WireCard, ToolCardTone, TurnCompletedStatus,
};
use desk_ui::components::board::{BoardCard, board_card};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::chat::{
    self, Agent, AgentMark, Block, Call, CallState, Ink, Marks, Object, OnLink, Phrase, Said,
    Streaming, ToolCard, ToolFold, Verdict, agent_row, calls, command, cron_row, fail, foot,
    fork_divider, linked_lead, name_tone, note, origin_row, tool_section, you,
};
use desk_ui::components::chip::{GitStatus, badge, file_chip, git_name};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::paint::{glyph, ink};
use desk_ui::components::size::CAPTION_TEXT;
use desk_ui::components::status_icon::RunState;
use desk_ui::metrics::ICON_SMALL;
use desk_ui::theme::Theme;
use gpui::{
    AnyElement, App, IntoElement, ParentElement, SharedString, StatefulInteractiveElement, Styled,
    Window, canvas, div, prelude::FluentBuilder,
};
use serde_json::Value;

use super::RevokeGrant;
use super::cards::{self, Kind, TicketView, View};
use super::mention::{Trace, mention, tool_glyph};

const OUTPUT: &str = "Result";
const COMMAND_SLOT: usize = 0;
const OUTPUT_SLOT: usize = 1;
const GROUP_SLOT: usize = 2;
const CARD_SLOT: usize = 3;
const ROW_SLOT: usize = 4;
const FRAME_LOG: &str = "DESK_FRAME_LOG";
const ARGUMENT_KEYS: [&str; 4] = ["command", "path", "pattern", "url"];
const QUEUED: &str = "queued";
const TOFU: &str = "tofu";
const QUOTED: &str = "> ";
const VERDICT: &str = "sub-agent ";
const VERDICT_IS: &str = " is ";
const VERDICT_END: &str = " after ";
const ACCEPTANCE: &str = "acceptance ";
const WROTE: &str = "wrote ";
const LINE: &str = "line ";
const MET: &str = "met:";
const UNMET: &str = "unmet:";
const DOT: &str = " \u{b7} ";
const FAILED_INK: Ink = Ink::Warn;
const FORKED: &str = "forked into ";
const FORK_KIND: &str = " as ";
const FORK_AT: &str = " at ";
const FORK_TOKENS: &str = " tokens, ";
const FORK_BEGAN: &str = "which began at ";
const THOUSAND: u64 = 1000;
const SPAWN: &str = "spawn";
const HIGHLIGHT_GAP: &str = "   ";

#[derive(Clone, PartialEq)]
pub enum Step {
    Tool(String),
    Agent(String),
}

#[derive(Clone, PartialEq)]
pub enum Entry {
    Message(usize),
    Task(String),
    Tool(String),
    Spawn(String),
    Report(String),
    Worked {
        steps: Vec<Step>,
        open: bool,
    },
    Done(String),
    Grant {
        tool: String,
        target: String,
        until: Option<String>,
        revoked: bool,
    },
    Resolved(String),
}

#[derive(Clone, PartialEq)]
pub struct Called {
    key: SharedString,
    card: ToolCard,
    opens: bool,
    view: View,
    trace: Option<Trace>,
}

#[derive(Clone, PartialEq)]
struct Sub {
    key: SharedString,
    mark: AgentMark,
    run: RunState,
    ticket: SharedString,
    model: SharedString,
    mission: SharedString,
    verdict: SharedString,
    said: SharedString,
    files: Vec<SharedString>,
    footer: Option<SharedString>,
    trace: Option<Trace>,
}

#[derive(Clone, PartialEq)]
enum Member {
    Call(Called),
    Shown(SharedString, View),
    Sub(Sub),
}

#[derive(Clone, PartialEq)]
pub struct Group {
    key: SharedString,
    head: ToolCard,
    members: Rc<Vec<Member>>,
}

#[derive(Clone)]
pub struct Lead {
    time: SharedString,
    text: SharedString,
    complete: bool,
    blocks: Rc<Vec<Block>>,
    streaming: Option<Rc<RefCell<Streaming>>>,
}

impl PartialEq for Lead {
    fn eq(&self, other: &Self) -> bool {
        self.time == other.time && self.text == other.text && self.complete == other.complete
    }
}

#[derive(Clone, PartialEq)]
pub enum Item {
    You {
        time: SharedString,
        text: SharedString,
        images: Vec<(u32, PathBuf)>,
    },
    Lead(Lead),
    Cron {
        job: SharedString,
        schedule: SharedString,
        prompt: SharedString,
    },
    Tool(Called),
    Worked {
        key: SharedString,
        head: ToolCard,
        groups: Rc<Vec<Group>>,
    },
    Stretch {
        key: SharedString,
        head: Option<ToolCard>,
        families: Rc<Vec<Group>>,
        alone: Rc<Vec<Called>>,
    },
    Note(SharedString),
    Told {
        head: SharedString,
        said: SharedString,
    },
    Fork {
        session: SharedString,
        tokens: u64,
    },
    Failure(SharedString),
    Foot(SharedString),
    Grant {
        target: SharedString,
        said: SharedString,
        revoked: bool,
    },
}

fn fork(text: &str) -> Option<Item> {
    let (session, rest) = text.strip_prefix(FORKED)?.split_once(FORK_KIND)?;
    let (_, rest) = rest.split_once(FORK_AT)?;
    let (tokens, began) = rest.split_once(FORK_TOKENS)?;
    began.strip_prefix(FORK_BEGAN)?.parse::<u64>().ok()?;
    Some(Item::Fork {
        session: session.to_owned().into(),
        tokens: tokens.parse().ok()?,
    })
}

fn fork_label(forks: Option<i64>, tokens: u64) -> String {
    let thousands = tokens.saturating_add(THOUSAND / 2) / THOUSAND;
    match forks {
        Some(count) => format!("Continued in fork {count} · at {thousands}k tokens"),
        None => format!("Continued in a new fork · at {thousands}k tokens"),
    }
}

pub fn entry(notification: &Notification) -> Option<(&str, Entry)> {
    match notification {
        Notification::TurnStarted(e) => Some((&e.session, Entry::Task(e.turn.clone()))),
        Notification::ToolStarted(e) => Some((&e.session, Entry::Tool(e.item.clone()))),
        Notification::AgentStarted(e) => Some((&e.session, Entry::Spawn(e.instance.clone()))),
        Notification::AgentEnded(e) => Some((&e.session, Entry::Report(e.instance.clone()))),
        Notification::TurnCompleted(e) => Some((&e.session, Entry::Done(e.turn.clone()))),
        _ => None,
    }
}

pub fn granted(session: &Session, resolved: &ApprovalResolved) -> Option<Entry> {
    if resolved.standing != Some(true) && resolved.until.is_none() {
        return None;
    }
    let (_, asked) = session.approvals.get(&resolved.approval)?;
    Some(Entry::Grant {
        tool: asked.tool.clone(),
        target: asked.target.clone(),
        until: resolved.until.clone(),
        revoked: false,
    })
}

pub fn resolved(session: &Session, resolved: &ApprovalResolved) -> Option<Entry> {
    let (_, asked) = session.approvals.get(&resolved.approval)?;
    let decision = String::from(resolved.decision.clone());
    let until = resolved.until.as_deref().unwrap_or_default();
    let read = match decision.as_str() {
        "allow_once" => "allowed once".to_owned(),
        "allow_always" => "allowed always".to_owned(),
        "allow_for" => format!("allowed for {}", until.get(11..16).unwrap_or(until)),
        "reject_once" => "rejected".to_owned(),
        "reject_always" => "rejected always".to_owned(),
        "remember_project"
        | "remember_global"
        | "remember_user_local"
        | "remember_project_local" => match resolved.standing {
            Some(false) => "remembered reject".to_owned(),
            _ => "remembered allow".to_owned(),
        },
        "cancelled" => "cancelled".to_owned(),
        _ => decision.clone(),
    };
    Some(Entry::Resolved(format!(
        "{} {}: {read} by {}",
        asked.tool, asked.target, resolved.by
    )))
}

pub fn revoke(order: &mut [Entry], target: &str) {
    for entry in order {
        if let Entry::Grant {
            target: granted,
            revoked,
            ..
        } = entry
            && granted == target
        {
            *revoked = true;
        }
    }
}

pub fn doing(session: &Session, since: &str) -> Option<String> {
    session
        .tools
        .values()
        .filter(|tool| {
            tool.output.is_none()
                && tool.agent.is_none()
                && tool.started_at.as_deref().is_some_and(|at| at >= since)
        })
        .max_by(|a, b| a.started_at.cmp(&b.started_at))
        .map(cards::doing)
}

pub fn sub_agents(session: &Session) -> usize {
    session
        .agents
        .values()
        .filter(|agent| agent.ended_at.is_none() && live(agent))
        .count()
}

fn live(agent: &SubAgent) -> bool {
    matches!(mark(&agent.state), AgentMark::Working | AgentMark::Asking)
}

pub fn settled(session: &Session, entry: &Entry) -> bool {
    match entry {
        Entry::Message(at) => session.messages.get(*at).is_none_or(|m| m.complete),
        Entry::Worked { steps, open } => {
            !open
                && steps.iter().all(|step| match step {
                    Step::Tool(id) => session.tools.get(id).is_none_or(|t| {
                        t.output.is_some()
                            && cards::away(t).is_none_or(|shell| {
                                session.shells.get(shell).is_none_or(|ended| ended.exited)
                            })
                    }),
                    Step::Agent(id) => session
                        .agents
                        .get(id)
                        .is_none_or(|agent| agent.ended_at.is_some() && !live(agent)),
                })
        }
        Entry::Task(turn) => session
            .turns
            .get(turn)
            .is_none_or(|started| started.status.is_some()),
        Entry::Tool(_) | Entry::Spawn(_) | Entry::Report(_) | Entry::Done(_) => true,
        Entry::Grant { revoked, .. } => *revoked,
        Entry::Resolved(_) => true,
    }
}

pub fn place(session: &Session, order: &mut Vec<Entry>, entry: Entry) {
    let step = match entry {
        Entry::Tool(id) => session
            .tools
            .get(&id)
            .filter(|tool| tool.agent.is_none())
            .map(|_| Step::Tool(id)),
        Entry::Spawn(id) => session
            .agents
            .get(&id)
            .filter(|agent| live(agent))
            .map(|_| Step::Agent(id)),
        Entry::Report(id) => seen(order, &id).then_some(Step::Agent(id)),
        Entry::Task(_) if order.contains(&entry) => return,
        entry @ (Entry::Message(_)
        | Entry::Task(_)
        | Entry::Worked { .. }
        | Entry::Done(_)
        | Entry::Grant { .. }
        | Entry::Resolved(_)) => {
            if !quiet(session, &entry) {
                close(order);
            }
            return order.push(entry);
        }
    };
    if let Some(step) = step {
        work(session, order, step);
    }
}

fn work(session: &Session, order: &mut Vec<Entry>, step: Step) {
    if let Some(Entry::Worked { steps, open: true }) = order
        .iter_mut()
        .rev()
        .find(|earlier| !quiet(session, earlier))
    {
        if !steps.contains(&step) {
            steps.push(step);
        }
        return;
    }
    close(order);
    order.push(Entry::Worked {
        steps: vec![step],
        open: true,
    });
}

fn close(order: &mut [Entry]) {
    if let Some(Entry::Worked { open, .. }) = order
        .iter_mut()
        .rev()
        .find(|earlier| matches!(earlier, Entry::Worked { .. }))
    {
        *open = false;
    }
}

fn seen(order: &[Entry], id: &str) -> bool {
    order.iter().any(|placed| match placed {
        Entry::Worked { steps, .. } => steps
            .iter()
            .any(|step| matches!(step, Step::Agent(agent) if agent == id)),
        _ => false,
    })
}

fn spoken_to(kind: &OriginKind) -> bool {
    matches!(kind, OriginKind::Person | OriginKind::Cron)
}

fn quiet(session: &Session, entry: &Entry) -> bool {
    match entry {
        Entry::Message(at) => session.messages.get(*at).is_none_or(|message| {
            message.agent.is_some()
                || echoed(session, *at)
                || match message.role {
                    Role::Assistant => message.text.is_empty(),
                    Role::Harness(_) => message.told().is_none(),
                    Role::Inbox | Role::Thinking | Role::Taken => true,
                    Role::User
                    | Role::Steer
                    | Role::Queued(_)
                    | Role::Cron { .. }
                    | Role::Note
                    | Role::Failure => false,
                }
        }),
        Entry::Task(turn) => session.turns.get(turn).is_none_or(|started| {
            started.task.is_empty()
                || !spoken_to(&started.origin.kind)
                || session.messages.iter().any(|m| {
                    m.agent.is_none()
                        && &m.turn == turn
                        && m.text == started.task
                        && matches!(m.role, Role::User | Role::Cron { .. })
                })
        }),
        Entry::Done(turn) => session
            .turns
            .get(turn)
            .is_none_or(|ended| !spoken_to(&ended.origin.kind)),
        Entry::Tool(_) | Entry::Spawn(_) | Entry::Report(_) => true,
        Entry::Worked { .. } | Entry::Grant { .. } | Entry::Resolved(_) => false,
    }
}

fn echoed(session: &Session, at: usize) -> bool {
    let Some(steer) = session.messages.get(at) else {
        return false;
    };
    let steered = |role: &Role| matches!(role, Role::Steer | Role::Queued(_));
    if !steered(&steer.role) {
        return false;
    }
    let earlier = session
        .messages
        .iter()
        .take(at)
        .filter(|m| m.agent.is_none() && steered(&m.role) && m.text == steer.text)
        .count();
    let became = session
        .turns
        .iter()
        .filter(|(id, turn)| {
            **id != steer.turn && turn.origin.kind == OriginKind::Person && turn.task == steer.text
        })
        .count();
    earlier < became
}

fn sent(session: &Session, text: &str) -> Option<SharedString> {
    let at = session
        .messages
        .iter()
        .filter(|m| m.agent.is_none() && m.text == text)
        .find_map(|m| m.at.as_deref())?;
    local(at)
}

fn local(at: &str) -> Option<SharedString> {
    let at = chrono::DateTime::parse_from_rfc3339(at).ok()?;
    Some(
        at.with_timezone(&chrono::Local)
            .format("%H:%M")
            .to_string()
            .into(),
    )
}

pub type Parsed = BTreeMap<SharedString, Rc<Vec<Block>>>;

pub fn parsed_of(items: &[Item]) -> Parsed {
    items
        .iter()
        .filter_map(|item| match item {
            Item::Lead(lead) if lead.complete => Some((lead.text.clone(), lead.blocks.clone())),
            _ => None,
        })
        .collect()
}

fn said(
    time: SharedString,
    text: SharedString,
    complete: bool,
    (was, parsed): (Option<&Item>, &Parsed),
) -> Lead {
    if complete {
        return Lead {
            blocks: parsed
                .get(&text)
                .cloned()
                .unwrap_or_else(|| Rc::new(chat::markdown(&text))),
            complete,
            streaming: None,
            time,
            text,
        };
    }
    let streaming = match was {
        Some(Item::Lead(Lead {
            streaming: Some(streaming),
            ..
        })) => streaming.clone(),
        _ => Rc::default(),
    };
    let blocks = Rc::new(streaming.borrow_mut().blocks(&text));
    Lead {
        blocks,
        complete,
        streaming: Some(streaming),
        time,
        text,
    }
}

pub fn item(session: &Session, entry: &Entry, was: Option<&Item>, parsed: &Parsed) -> Option<Item> {
    if quiet(session, entry) {
        return None;
    }
    match entry {
        Entry::Message(at) => {
            let message = session.messages.get(*at)?;
            let text = SharedString::from(message.text.clone());
            let own = message.at.as_deref().and_then(local);
            let time = own.clone().unwrap_or_else(|| clock(session, &message.turn));
            let images = message.images.clone();
            match &message.role {
                Role::User | Role::Steer => Some(Item::You {
                    time: own.or_else(|| sent(session, &message.text)).unwrap_or(time),
                    text,
                    images,
                }),
                Role::Queued(_) => Some(Item::You {
                    time: QUEUED.into(),
                    text,
                    images,
                }),
                Role::Cron { job, schedule } => Some(cron(session, job, schedule, &message.text)),
                Role::Assistant => Some(Item::Lead(said(
                    time,
                    text,
                    message.complete,
                    (was, parsed),
                ))),
                Role::Harness(_) => message.told().map(|told| Item::Told {
                    head: told.head.into(),
                    said: told.said.into(),
                }),
                Role::Inbox | Role::Thinking | Role::Taken => None,
                Role::Note => fork(&message.text).or(Some(Item::Note(text))),
                Role::Failure => Some(Item::Failure(text)),
            }
        }
        Entry::Task(turn) => {
            let started = session.turns.get(turn)?;
            let origin = &started.origin;
            match &origin.kind {
                OriginKind::Person => Some(Item::You {
                    time: sent(session, &started.task).unwrap_or_else(|| clock(session, turn)),
                    text: started.task.clone().into(),
                    images: started.images.clone(),
                }),
                OriginKind::Cron => Some(cron(
                    session,
                    origin.job.as_deref().unwrap_or_default(),
                    origin.schedule.as_deref().unwrap_or_default(),
                    &started.task,
                )),
                OriginKind::Agent
                | OriginKind::Tofu
                | OriginKind::Hook
                | OriginKind::Memory
                | OriginKind::Skill
                | OriginKind::Library
                | OriginKind::Plugin
                | OriginKind::Unknown(_) => None,
            }
        }
        Entry::Worked { steps, .. } => worked(session, steps),
        Entry::Tool(_) | Entry::Spawn(_) | Entry::Report(_) => None,
        Entry::Done(turn) => {
            let ended = session.turns.get(turn)?;
            let seconds = ended.worked_for_ms / 1000;
            let took = match seconds {
                0..60 => format!("{seconds}s"),
                _ => format!("{}m {}s", seconds / 60, seconds % 60),
            };
            let said = match &ended.status {
                None | Some(TurnCompletedStatus::Finished) => "cooked for",
                Some(TurnCompletedStatus::Stopped) => "stopped after",
                Some(TurnCompletedStatus::Failed) => "failed after",
                Some(TurnCompletedStatus::Unknown(status)) => status,
            };
            Some(Item::Foot(format!("{said} {took}").into()))
        }
        Entry::Grant {
            tool,
            target,
            until,
            revoked,
        } => {
            let lasts = until.as_deref().map_or_else(
                || "always here".to_owned(),
                |until| format!("until {}", until.get(11..16).unwrap_or(until)),
            );
            let ended = if *revoked { " · revoked" } else { "" };
            Some(Item::Grant {
                target: target.clone().into(),
                said: format!("{tool} {target} runs without asking, {lasts}{ended}").into(),
                revoked: *revoked,
            })
        }
        Entry::Resolved(said) => Some(Item::Note(said.clone().into())),
    }
}

fn worked(session: &Session, steps: &[Step]) -> Option<Item> {
    let tools: Vec<(&str, &Tool)> = steps
        .iter()
        .filter_map(|step| match step {
            Step::Tool(id) => Some((id.as_str(), session.tools.get(id)?)),
            Step::Agent(_) => None,
        })
        .collect();
    let agents: Vec<(&str, &SubAgent)> = steps
        .iter()
        .filter_map(|step| match step {
            Step::Agent(id) => Some((id.as_str(), session.agents.get(id)?)),
            Step::Tool(_) => None,
        })
        .collect();
    let key: SharedString = match steps.first()? {
        Step::Tool(id) | Step::Agent(id) => id.clone().into(),
    };
    let lines: Vec<(&str, String)> = tools
        .iter()
        .filter_map(|(_, tool)| attached(tool, &agents))
        .collect();
    if let Some(stretch) = stretch(session, &key, &tools, &agents, &lines) {
        return Some(stretch);
    }
    let shown: Vec<(&str, &Tool)> = tools
        .iter()
        .copied()
        .filter(|(_, tool)| !cards::spawned(tool) && attached(tool, &agents).is_none())
        .collect();
    if let ([(id, tool)], []) = (shown.as_slice(), agents.as_slice())
        && cards::kind(tool) != Kind::Refused
        && cards::state(tool) != CallState::Failed
    {
        return Some(Item::Tool(called(id, tool)));
    }
    let mut kinds: BTreeMap<Kind, Vec<(&str, &Tool)>> = BTreeMap::new();
    for &(id, tool) in &shown {
        kinds.entry(cards::kind(tool)).or_default().push((id, tool));
    }
    if !agents.is_empty() {
        kinds.entry(Kind::Agent).or_default();
    }
    if kinds.is_empty() {
        return None;
    }
    let mut subs: Vec<Member> = agents
        .iter()
        .map(|(id, agent)| Member::Sub(sub(id, agent, &lines)))
        .collect();
    let (mut groups, mut counts, mut glyphs) = (Vec::new(), Vec::new(), Vec::new());
    for (kind, members) in kinds {
        let calls: Vec<&Tool> = members.iter().map(|(_, tool)| *tool).collect();
        let head = match kind {
            Kind::Agent => crew(&agents, &calls),
            kind => cards::group(kind, &calls),
        };
        counts.push(head.verb.to_string());
        if !glyphs.contains(&kind.glyph()) {
            glyphs.push(kind.glyph());
        }
        let mut drawn = match kind {
            Kind::Agent => std::mem::take(&mut subs),
            _ => Vec::new(),
        };
        drawn.extend(members.iter().map(|(id, tool)| member(id, tool)));
        groups.push(Group {
            key: format!("{key}:{kind:?}").into(),
            head,
            members: Rc::new(drawn),
        });
    }
    let running = tools
        .iter()
        .any(|(_, tool)| cards::state(tool) == CallState::Running)
        || agents.iter().any(|(_, agent)| live(agent));
    let failed = shown
        .iter()
        .filter(|(_, tool)| cards::state(tool) == CallState::Failed)
        .count();
    let all: Vec<&Tool> = tools.iter().map(|(_, tool)| *tool).collect();
    let meta = std::iter::once(match shown.len() + agents.len() + lines.len() {
        1 => "1 step".to_owned(),
        steps => format!("{steps} steps"),
    })
    .chain(counts)
    .chain(cards::wall(&all))
    .collect::<Vec<_>>()
    .join(DOT);
    Some(Item::Worked {
        key,
        head: ToolCard {
            glyphs,
            verb: if running { "Working" } else { "Worked" }.into(),
            objects: (failed > 0)
                .then(|| {
                    let failed = format!("{failed} failed");
                    Object::Phrase(Phrase::new([(failed.as_str(), FAILED_INK)]))
                })
                .into_iter()
                .collect(),
            meta: Some(meta.into()),
            state: match running {
                true => CallState::Running,
                false => CallState::Done,
            },
        },
        groups: Rc::new(groups),
    })
}

fn stretch(
    session: &Session,
    key: &SharedString,
    tools: &[(&str, &Tool)],
    agents: &[(&str, &SubAgent)],
    lines: &[(&str, String)],
) -> Option<Item> {
    let carded = tools
        .iter()
        .map(|&(id, tool)| {
            tool.group.as_ref()?;
            Some((id, tool, tool.card.as_ref()?))
        })
        .collect::<Option<Vec<_>>>()?;
    let (alone, together): (Vec<_>, Vec<_>) = carded
        .iter()
        .copied()
        .partition(|(_, _, card)| card.family == Family::Asked);
    let alone = alone
        .iter()
        .map(|&(id, tool, card)| lone(id, tool, card))
        .collect::<Option<Vec<_>>>()?;
    let mut seen: Vec<&str> = Vec::new();
    for (_, tool, _) in &carded {
        if let Some(group) = tool.group.as_deref().filter(|group| !seen.contains(group)) {
            seen.push(group);
        }
    }
    let mut details: Vec<(&Family, &[CardPiece])> = Vec::new();
    for line in seen
        .iter()
        .filter_map(|group| session.stretches.get(*group))
        .flat_map(|summary| &summary.open)
        .filter(|line| line.family != Family::Asked)
    {
        match details
            .iter_mut()
            .find(|(family, _)| **family == line.family)
        {
            Some(slot) => slot.1 = &line.detail,
            None => details.push((&line.family, &line.detail)),
        }
    }
    let spawns = together
        .iter()
        .filter(|(_, tool, card)| card.family == Family::Agents && tool.name == SPAWN)
        .count();
    let started = match spawns {
        1 => "1 agent started".to_owned(),
        many => format!("{many} agents started"),
    };
    let outcomes: Vec<(&Family, String, Ink, bool)> = together
        .iter()
        .filter_map(|(_, tool, card)| {
            let shell = cards::away(tool)?;
            let command = card
                .object
                .as_deref()
                .filter(|object| !object.is_empty())
                .unwrap_or_else(|| cards::text(&tool.args, "command"));
            let ended = session.shells.get(shell).filter(|ended| ended.exited);
            let (said, ink) = match ended {
                Some(ended) if ended.killed => (format!("stopped in {shell}"), Ink::Muted),
                Some(ended) => match ended.exit_code {
                    Some(0) => (format!("exited 0 in {shell}"), Ink::Plain),
                    Some(code) => (format!("exited {code} in {shell}"), FAILED_INK),
                    None => (format!("ended in {shell}"), Ink::Muted),
                },
                None => (format!("running in {shell}"), Ink::Muted),
            };
            Some((
                &card.family,
                format!("{command} {said}"),
                ink,
                ended.is_none(),
            ))
        })
        .collect();
    let failing = |card: &WireCard| card.tone == ToolCardTone::Fail;
    let mut families = Vec::new();
    let mut said: Vec<(&str, Ink)> = Vec::new();
    for &(family, detail) in &details {
        let glyph = cards::family_glyph(family)?;
        let held: Vec<(&str, &Tool, &WireCard)> = together
            .iter()
            .copied()
            .filter(|(_, _, card)| card.family == *family)
            .collect();
        let away: Vec<(&str, Ink)> = outcomes
            .iter()
            .filter(|(of, _, _, _)| *of == family)
            .flat_map(|(_, said, ink, _)| [(DOT, Ink::Muted), (said.as_str(), *ink)])
            .skip(1)
            .collect();
        let detail = match (family, spawns) {
            (Family::Agents, 1..) => vec![(started.as_str(), Ink::Plain)],
            _ if !away.is_empty() => away,
            _ => cards::inked(detail)?,
        };
        said.push((DOT, Ink::Muted));
        said.extend(detail.iter().copied());
        let subs = agents
            .iter()
            .filter(|_| *family == Family::Agents && spawns > 0)
            .map(|(id, agent)| Member::Sub(sub(id, agent, lines)));
        let shown = held
            .iter()
            .filter_map(|&(id, tool, card)| match failing(card) {
                true => lone(id, tool, card).map(Member::Call),
                false => own(id, tool),
            })
            .collect::<Vec<_>>();
        let pending = outcomes
            .iter()
            .any(|(of, _, _, running)| *of == family && *running);
        let state = match held.is_empty() || pending {
            true => CallState::Skipped,
            false => stage(
                &held
                    .iter()
                    .map(|&(id, tool, _)| (id, tool))
                    .collect::<Vec<_>>(),
            ),
        };
        families.push(Group {
            key: format!("{key}:{family:?}").into(),
            head: phrased(vec![glyph], Phrase::new(detail), None, state),
            members: Rc::new(subs.chain(shown).collect()),
        });
    }
    let stray: Vec<Member> = together
        .iter()
        .filter(|(_, _, card)| failing(card) && !details.iter().any(|(f, _)| **f == card.family))
        .filter_map(|&(id, tool, card)| lone(id, tool, card).map(Member::Call))
        .collect();
    let failed = together.iter().filter(|(_, _, card)| failing(card)).count();
    let failed = format!("{failed} failed");
    if !stray.is_empty() {
        families.push(Group {
            key: format!("{key}:failed").into(),
            head: phrased(
                vec![Glyph::Terminal],
                Phrase::new([(failed.as_str(), FAILED_INK)]),
                None,
                CallState::Done,
            ),
            members: Rc::new(stray),
        });
    }
    if together.iter().any(|(_, _, card)| failing(card)) {
        said.extend([(DOT, Ink::Muted), (failed.as_str(), FAILED_INK)]);
    }
    let mut glyphs = Vec::new();
    for (_, _, card) in &together {
        let glyph = cards::family_glyph(&card.family)?;
        if !glyphs.contains(&glyph) {
            glyphs.push(glyph);
        }
    }
    let all: Vec<&Tool> = carded.iter().map(|(_, tool, _)| *tool).collect();
    let calls = match carded.len() {
        1 => "1 call".to_owned(),
        many => format!("{many} calls"),
    };
    let running = together
        .iter()
        .rev()
        .find(|(_, tool, _)| cards::state(tool) == CallState::Running);
    let head = match running {
        Some((_, _, card)) => {
            let doing = [Some(card.doing.as_str()), card.object.as_deref()]
                .into_iter()
                .flatten()
                .filter(|said| !said.is_empty())
                .collect::<Vec<_>>()
                .join(" ");
            Phrase::new([
                (calls.as_str(), Ink::Muted),
                (HIGHLIGHT_GAP, Ink::Plain),
                (doing.as_str(), Ink::Plain),
            ])
        }
        None => Phrase::new(std::iter::once((calls.as_str(), Ink::Muted)).chain(said)),
    };
    let together: Vec<(&str, &Tool)> = together.iter().map(|&(id, tool, _)| (id, tool)).collect();
    Some(Item::Stretch {
        key: key.clone(),
        head: (!together.is_empty()).then(|| {
            let state = match outcomes.iter().any(|(_, _, _, running)| *running) {
                true => CallState::Skipped,
                false => stage(&together),
            };
            phrased(glyphs, head, cards::wall(&all), state)
        }),
        families: Rc::new(families),
        alone: Rc::new(alone),
    })
}

fn stage(tools: &[(&str, &Tool)]) -> CallState {
    match tools
        .iter()
        .any(|(_, tool)| cards::state(tool) == CallState::Running)
    {
        true => CallState::Running,
        false => CallState::Done,
    }
}

fn phrased(glyphs: Vec<Glyph>, phrase: Phrase, meta: Option<String>, state: CallState) -> ToolCard {
    ToolCard {
        glyphs,
        verb: SharedString::default(),
        objects: vec![Object::Phrase(phrase)],
        meta: meta.map(Into::into),
        state,
    }
}

fn own(id: &str, tool: &Tool) -> Option<Member> {
    match cards::view(tool) {
        view @ (View::Ticket(_) | View::Files(_) | View::Commits(_)) => {
            Some(Member::Shown(id.to_owned().into(), view))
        }
        View::Output(_) | View::Command { .. } => None,
    }
}

fn lone(id: &str, tool: &Tool, card: &WireCard) -> Option<Called> {
    let state = match card.tone {
        ToolCardTone::Fail => CallState::Failed,
        ToolCardTone::Ok | ToolCardTone::Warn => cards::state(tool),
        ToolCardTone::Unknown(_) => return None,
    };
    let verb = match state {
        CallState::Running => &card.doing,
        CallState::Done | CallState::Failed | CallState::Skipped | CallState::Warned => &card.verb,
    };
    let object = card.object.as_deref().filter(|object| !object.is_empty());
    Some(Called {
        key: id.to_owned().into(),
        card: ToolCard {
            glyphs: vec![cards::family_glyph(&card.family)?],
            verb: verb.clone().into(),
            objects: object
                .map(|object| Object::Phrase(Phrase::new([(object, Ink::Plain)])))
                .into_iter()
                .collect(),
            meta: card
                .meta
                .clone()
                .filter(|meta| !meta.is_empty())
                .map(Into::into),
            state,
        },
        opens: false,
        view: View::Output(None),
        trace: traced(tool, [verb.as_str(), object.unwrap_or_default()].join(" ")),
    })
}

fn traced(tool: &Tool, label: String) -> Option<Trace> {
    tool.mention.clone().map(|token| Trace {
        token,
        glyph: tool_glyph(&tool.name),
        label,
        detail: argument(&tool.args),
    })
}

fn attached<'a>(tool: &'a Tool, agents: &[(&str, &SubAgent)]) -> Option<(&'a str, String)> {
    cards::addressed(tool).filter(|(to, _)| agents.iter().any(|(id, _)| id == to))
}

fn crew(agents: &[(&str, &SubAgent)], calls: &[&Tool]) -> ToolCard {
    let running = agents.iter().any(|(_, agent)| live(agent))
        || calls
            .iter()
            .any(|tool| cards::state(tool) == CallState::Running);
    let count = match agents.len() {
        0 => calls.len(),
        named => named,
    };
    ToolCard {
        glyphs: vec![Kind::Agent.glyph()],
        verb: Kind::Agent.counted(count).into(),
        objects: cards::capped(
            agents
                .iter()
                .map(|(id, _)| Object::Agent((*id).to_owned().into())),
        ),
        meta: None,
        state: match running {
            true => CallState::Running,
            false => CallState::Done,
        },
    }
}

fn member(id: &str, tool: &Tool) -> Member {
    let call = called(id, tool);
    match &call.view {
        View::Ticket(_) => Member::Shown(call.key, call.view),
        View::Output(_) | View::Command { .. } | View::Files(_) | View::Commits(_) => {
            Member::Call(call)
        }
    }
}

fn called(id: &str, tool: &Tool) -> Called {
    let card = cards::card(tool);
    let label = card
        .objects
        .iter()
        .map(Object::text)
        .fold(card.verb.to_string(), |line, object| {
            format!("{line} {object}")
        });
    Called {
        key: id.to_owned().into(),
        card,
        opens: cards::opens(&[tool]),
        view: cards::view(tool),
        trace: traced(tool, label),
    }
}

fn sub(id: &str, agent: &SubAgent, lines: &[(&str, String)]) -> Sub {
    let report = agent.report.as_deref().unwrap_or_default();
    let mark = mark(&agent.state);
    let verdict = verdict(report);
    let (met, unmet) = report
        .lines()
        .filter_map(|line| Some(line.strip_prefix(LINE)?.split_once(' ')?.1))
        .fold((0usize, 0usize), |(met, unmet), rest| {
            match (rest.starts_with(MET), rest.starts_with(UNMET)) {
                (true, _) => (met.saturating_add(1), unmet),
                (_, true) => (met, unmet.saturating_add(1)),
                _ => (met, unmet),
            }
        });
    let files: Vec<SharedString> = match report.is_empty() {
        true => agent
            .owns
            .iter()
            .map(|path| cards::name(path).to_owned().into())
            .collect(),
        false => report
            .lines()
            .find_map(|line| line.strip_prefix(WROTE))
            .into_iter()
            .flat_map(|wrote| wrote.split(", "))
            .map(|path| cards::name(path).to_owned().into())
            .collect(),
    };
    let footer: Vec<String> = (met.saturating_add(unmet) > 0)
        .then(|| format!("acceptance {met} met{DOT}{unmet} unmet"))
        .into_iter()
        .chain(
            lines
                .iter()
                .filter(|(to, _)| *to == id)
                .map(|(_, line)| line.clone()),
        )
        .collect();
    Sub {
        key: id.to_owned().into(),
        mark,
        run: match (mark, verdict) {
            (AgentMark::Working, _) => RunState::Running,
            (AgentMark::Asking, _) => RunState::Waiting,
            (AgentMark::Failed, _) | (AgentMark::Done, Some("blocked")) => RunState::Failed,
            (AgentMark::Done, Some("needs you" | "needs context" | "done with concerns")) => {
                RunState::Waiting
            }
            (AgentMark::Done, _) => RunState::Done,
        },
        ticket: agent.ticket.clone().unwrap_or_default().into(),
        model: agent.model.clone().into(),
        mission: agent
            .task
            .lines()
            .map(str::trim)
            .find(|line| !line.is_empty())
            .unwrap_or_default()
            .to_owned()
            .into(),
        verdict: format!(
            "{}{DOT}{} steps",
            verdict.unwrap_or(state_word(&agent.state)),
            agent.steps
        )
        .into(),
        said: brief(report).to_owned().into(),
        files,
        footer: (!footer.is_empty()).then(|| footer.join("\n").into()),
        trace: agent.mention.clone().map(|token| Trace {
            token,
            glyph: Glyph::Agents,
            label: id.to_owned(),
            detail: agent.task.clone(),
        }),
    }
}

fn verdict(report: &str) -> Option<&str> {
    report
        .lines()
        .find_map(|line| {
            line.strip_prefix(ACCEPTANCE)?
                .split_once(", ")?
                .1
                .strip_suffix(':')
        })
        .or_else(|| {
            report.lines().find_map(|line| {
                let (_, rest) = line.strip_prefix(VERDICT)?.split_once(VERDICT_IS)?;
                rest.split(VERDICT_END).next()?.split(", ").nth(1)
            })
        })
}

fn state_word(state: &AgentState) -> &str {
    match state {
        AgentState::Working => "working",
        AgentState::InReview => "in review",
        AgentState::Reopened => "reopened",
        AgentState::WaitingAnswer => "waiting on an answer",
        AgentState::Parked => "parked",
        AgentState::Finished => "finished",
        AgentState::Errored => "errored",
        AgentState::Unknown(state) => state.as_str(),
    }
}

pub(super) fn brief(report: &str) -> &str {
    let said = report
        .lines()
        .find_map(|line| Some(line.strip_prefix(QUOTED)?.trim()).filter(|said| !said.is_empty()));
    let verdict = || {
        report.lines().find_map(|line| {
            let (_, rest) = line.strip_prefix(VERDICT)?.split_once(VERDICT_IS)?;
            Some(rest.split(VERDICT_END).next().unwrap_or(rest))
        })
    };
    said.or_else(verdict).unwrap_or_default()
}

fn cron(session: &Session, job: &str, schedule: &str, said: &str) -> Item {
    let prompt = session
        .cron
        .as_ref()
        .and_then(|cron| cron.jobs.iter().find(|known| known.id == job))
        .map_or(said, |known| known.prompt.as_str());
    Item::Cron {
        job: job.to_owned().into(),
        schedule: schedule.to_owned().into(),
        prompt: prompt.to_owned().into(),
    }
}

fn clock(session: &Session, turn: &str) -> SharedString {
    session
        .turns
        .get(turn)
        .and_then(|turn| turn.started_at.get(11..16))
        .unwrap_or_default()
        .to_owned()
        .into()
}

pub(super) fn argument(args: &Value) -> String {
    ARGUMENT_KEYS
        .iter()
        .find_map(|key| args.get(key)?.as_str())
        .map_or_else(|| args.to_string(), str::to_owned)
}

fn mark(state: &AgentState) -> AgentMark {
    match state {
        AgentState::Working | AgentState::InReview | AgentState::Reopened => AgentMark::Working,
        AgentState::WaitingAnswer => AgentMark::Asking,
        AgentState::Parked | AgentState::Finished => AgentMark::Done,
        AgentState::Errored | AgentState::Unknown(_) => AgentMark::Failed,
    }
}

pub fn shapes(items: &[Item]) -> (usize, usize, usize) {
    items
        .iter()
        .fold((0, 0, 0), |(stretches, calls, worked), item| match item {
            Item::Stretch { .. } => (stretches + 1, calls, worked),
            Item::Tool(_) => (stretches, calls + 1, worked),
            Item::Worked { .. } => (stretches, calls, worked + 1),
            Item::You { .. }
            | Item::Lead(_)
            | Item::Cron { .. }
            | Item::Note(_)
            | Item::Told { .. }
            | Item::Fork { .. }
            | Item::Failure(_)
            | Item::Foot(_)
            | Item::Grant { .. } => (stretches, calls, worked),
        })
}

pub fn pieces(item: &Item) -> Vec<String> {
    match item {
        Item::You { text, .. } => vec![text.to_string()],
        Item::Lead(said) => chat::pieces(&said.blocks),
        Item::Grant { said, .. } => vec![said.to_string()],
        Item::Told { head, said } => vec![format!("{head} {said}")],
        Item::Cron { .. }
        | Item::Tool(_)
        | Item::Worked { .. }
        | Item::Stretch { .. }
        | Item::Note(_)
        | Item::Fork { .. }
        | Item::Failure(_)
        | Item::Foot(_) => Vec::new(),
    }
}

pub fn render(
    item: &Item,
    at: usize,
    marks: &[Marks],
    forks: Option<i64>,
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> AnyElement {
    match item {
        Item::You { time, text, images } => you(
            time.clone(),
            Said {
                text: text.clone(),
                images,
            },
            false,
            None,
            at == 0,
            marks.first().map_or(&[][..], Vec::as_slice),
            theme,
        )
        .into_any_element(),
        Item::Lead(said) => linked_lead(
            ("chat-lead", at),
            said.time.clone(),
            &said.blocks,
            marks,
            open_cited(),
            open_flow(),
            Some((&*window, &*cx)),
            theme,
        )
        .when(
            !said.complete && env::var_os(FRAME_LOG).is_some(),
            |drawn| drawn.child(stream_meter()),
        )
        .into_any_element(),
        Item::Cron {
            job,
            schedule,
            prompt,
        } => cron_row(job.clone(), schedule.clone(), prompt.clone(), theme).into_any_element(),
        Item::Tool(call) => drawn(call),
        Item::Worked { key, head, groups } => {
            let groups = groups.clone();
            ToolFold::new(
                (key.clone(), GROUP_SLOT),
                head.clone(),
                false,
                move |theme| match groups.as_slice() {
                    [one] => one
                        .members
                        .iter()
                        .flat_map(|member| drawn_member(member, theme))
                        .collect(),
                    many => many.iter().map(group_fold).collect(),
                },
            )
            .into_any_element()
        }
        Item::Stretch {
            key,
            head,
            families,
            alone,
        } => {
            let families = families.clone();
            let head = head.clone().map(|head| match families.is_empty() {
                true => ToolFold::line((key.clone(), GROUP_SLOT), head),
                false => ToolFold::new((key.clone(), GROUP_SLOT), head, false, move |_| {
                    families.iter().map(group_fold).collect()
                }),
            });
            div()
                .flex()
                .flex_col()
                .min_w_0()
                .children(head)
                .children(alone.iter().map(drawn))
                .into_any_element()
        }
        Item::Note(text) => note(text.clone(), theme).into_any_element(),
        Item::Told { head, said } => {
            origin_row(Glyph::Talk, TOFU, head.clone(), said.clone(), theme).into_any_element()
        }
        Item::Fork { session, tokens } => fork_divider(
            ("chat-fork", at),
            fork_label(forks, *tokens).into(),
            session.clone(),
            theme,
            window,
            cx,
        )
        .into_any_element(),
        Item::Failure(text) => fail("failed", text.clone(), "", theme).into_any_element(),
        Item::Foot(text) => foot(text.clone(), theme).into_any_element(),
        Item::Grant {
            target,
            said,
            revoked,
        } => {
            let target = target.clone();
            note(said.clone(), theme)
                .flex()
                .items_center()
                .gap_2()
                .when(!revoked, |row| {
                    row.child(
                        button(("chat-grant", at), "Revoke", None, ButtonKind::Text, theme)
                            .on_click(move |_, window, cx| {
                                window.dispatch_action(
                                    Box::new(RevokeGrant {
                                        target: target.clone(),
                                    }),
                                    cx,
                                );
                            }),
                    )
                })
                .into_any_element()
        }
    }
}

#[cfg(feature = "plugin-draw")]
fn open_flow() -> Option<OnLink> {
    Some(Rc::new(|source, window, cx| {
        if let Err(error) = crate::modules::draw::open_flow(source, window, cx) {
            eprintln!("desk: chat flow did not open in draw: {error}");
        }
    }))
}

#[cfg(not(feature = "plugin-draw"))]
fn open_flow() -> Option<OnLink> {
    None
}

#[cfg(all(feature = "screen-work", feature = "module-editor"))]
fn open_cited() -> Option<OnLink> {
    use desk_core::limits::TOAST_LIFETIME;

    use crate::desk::Desk;

    Some(Rc::new(|cited, window, cx| {
        let Some(Some(desk)) = window.root::<Desk>() else {
            return eprintln!("desk: chat cite {cited}: the window's root is not the desk");
        };
        desk.update(cx, |desk, cx| {
            let Err(why) = crate::control::editor::open_cited(cited, desk, window, cx) else {
                return;
            };
            eprintln!("desk: chat cite {cited} did not open: {why}");
            let id = desk.notify("File did not open".into(), why.into(), cx);
            cx.spawn(async move |desk, cx| {
                cx.background_executor().timer(TOAST_LIFETIME).await;
                if let Err(error) = desk.update(cx, |desk, cx| desk.dismiss_toast(Some(id), cx)) {
                    eprintln!("desk: chat cite toast outlived the desk: {error}");
                }
            })
            .detach();
        });
    }))
}

#[cfg(not(all(feature = "screen-work", feature = "module-editor")))]
fn open_cited() -> Option<OnLink> {
    None
}

#[cfg(feature = "screen-boardy")]
fn opens_ticket(card: BoardCard, id: SharedString) -> BoardCard {
    if id.is_empty() {
        return card;
    }
    card.on_click(move |_, window, cx| {
        window.dispatch_action(
            Box::new(crate::screens::boardy::OpenTicket { id: id.clone() }),
            cx,
        );
    })
}

#[cfg(not(feature = "screen-boardy"))]
fn opens_ticket(card: BoardCard, _: SharedString) -> BoardCard {
    card
}

fn stream_meter() -> impl IntoElement {
    canvas(
        |_, _, _| (),
        |_, (), window, _| {
            if let Ok(now) = SystemTime::now().duration_since(UNIX_EPOCH) {
                eprintln!("desk: chat stream frame {}", now.as_micros());
            }
            window.request_animation_frame();
        },
    )
    .absolute()
    .size_0()
}

fn group_fold(group: &Group) -> AnyElement {
    let key = (group.key.clone(), GROUP_SLOT);
    if group.members.is_empty() {
        return ToolFold::line(key, group.head.clone()).into_any_element();
    }
    let members = group.members.clone();
    ToolFold::new(key, group.head.clone(), false, move |theme| {
        members
            .iter()
            .flat_map(|member| drawn_member(member, theme))
            .collect()
    })
    .into_any_element()
}

fn drawn_member(member: &Member, theme: &Theme) -> Vec<AnyElement> {
    match member {
        Member::Call(call) => vec![drawn(call)],
        Member::Shown(key, view) => viewed(key, view, theme),
        Member::Sub(sub) => vec![sub_drawn(sub, theme)],
    }
}

fn drawn(call: &Called) -> AnyElement {
    let (key, view) = (call.key.clone(), call.view.clone());
    let fold = match view {
        View::Output(None) => ToolFold::line(key, call.card.clone()),
        view => ToolFold::new(key.clone(), call.card.clone(), call.opens, move |theme| {
            viewed(&key, &view, theme)
        }),
    };
    match call.trace.clone() {
        Some(trace) => fold
            .on_trace(move |window, cx| mention(trace.clone(), window, cx))
            .into_any_element(),
        None => fold.into_any_element(),
    }
}

fn viewed(key: &SharedString, view: &View, theme: &Theme) -> Vec<AnyElement> {
    let section = |output: &SharedString| {
        tool_section((key.clone(), OUTPUT_SLOT), OUTPUT, output.clone(), theme).into_any_element()
    };
    match view {
        View::Output(output) => output.iter().map(section).collect(),
        View::Command {
            line,
            exit,
            said,
            output,
        } => [
            Some(
                command(
                    (key.clone(), COMMAND_SLOT),
                    false,
                    line.clone(),
                    exit.clone().map(Verdict::Exit),
                    None,
                    theme,
                )
                .into_any_element(),
            ),
            said.clone().map(|said| {
                div()
                    .min_w_0()
                    .truncate()
                    .text_color(ink(theme, CAPTION_TEXT))
                    .child(said)
                    .into_any_element()
            }),
            output.as_ref().map(section),
        ]
        .into_iter()
        .flatten()
        .collect(),
        View::Files(files) => vec![
            div()
                .mt_1()
                .flex()
                .flex_col()
                .gap_1()
                .min_w_0()
                .children(files.iter().map(|(status, path)| {
                    div()
                        .flex()
                        .items_center()
                        .gap_2()
                        .min_w_0()
                        .child(badge(letter(*status), theme))
                        .child(git_name(path.clone(), Some(*status), theme))
                }))
                .into_any_element(),
        ],
        View::Commits(commits) => {
            let rows: Vec<Call> = commits
                .iter()
                .map(|(sha, subject)| Call {
                    tool: sha.clone(),
                    arg: subject.clone(),
                    out: SharedString::default(),
                })
                .collect();
            vec![calls(&rows, None, theme).into_any_element()]
        }
        View::Ticket(ticket) => vec![ticket_drawn(key, ticket, theme)],
    }
}

fn letter(status: GitStatus) -> &'static str {
    match status {
        GitStatus::Modified => "M",
        GitStatus::Added => "A",
        GitStatus::Deleted => "D",
        GitStatus::Untracked => "U",
        GitStatus::Ignored => "I",
        GitStatus::Conflict => "C",
    }
}

fn ticket_drawn(key: &SharedString, ticket: &TicketView, theme: &Theme) -> AnyElement {
    let card = board_card((key.clone(), CARD_SLOT))
        .id_line(ticket.id.clone())
        .title(ticket.title.clone())
        .when_some(ticket.status, BoardCard::status)
        .when_some(ticket.said.clone(), BoardCard::summary)
        .when(ticket.acceptance > 0, |card| {
            card.chips([badge(format!("{} acceptance", ticket.acceptance), theme)])
        });
    div()
        .pt_1()
        .child(opens_ticket(card, ticket.id.clone()))
        .into_any_element()
}

fn sub_drawn(sub: &Sub, theme: &Theme) -> AnyElement {
    let row = agent_row(
        (sub.key.clone(), ROW_SLOT),
        &Agent {
            mark: sub.mark,
            name: sub.key.clone(),
            summary: sub.ticket.clone(),
            link: Some(sub.model.clone()),
            tone: Some(name_tone(&sub.key, theme)),
        },
        theme,
    )
    .mt_0();
    let files = sub.files.iter().map(|file| {
        file_chip(
            glyph(Glyph::File, ICON_SMALL, ink(theme, CAPTION_TEXT)),
            file.clone(),
            theme,
        )
    });
    let card = board_card((sub.key.clone(), CARD_SLOT))
        .id_line(row)
        .title(sub.mission.clone())
        .run(sub.run, sub.verdict.clone())
        .chips(files)
        .when(!sub.said.is_empty(), |card| card.summary(sub.said.clone()))
        .when_some(sub.footer.clone(), BoardCard::footer)
        .when_some(sub.trace.clone(), |card, trace| {
            card.on_click(move |_, window, cx| mention(trace.clone(), window, cx))
        });
    div().pt_1().child(card).into_any_element()
}
