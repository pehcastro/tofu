use std::rc::Rc;

use desk_core::model::{Role, Session};
use desk_core::protocol::AgentState;
use desk_ui::components::chat::{
    self, Agent, AgentMark, Block, Marks, Span, Verdict, agent_row, command, fail, foot, lead,
    note, you,
};
use desk_ui::theme::Theme;
use gpui::{AnyElement, IntoElement, SharedString};
use serde_json::Value;

const ARGUMENT_KEYS: [&str; 4] = ["command", "path", "pattern", "url"];

#[derive(Clone, PartialEq)]
pub enum Entry {
    Message(usize),
    Task(String),
    Tool(String),
    Agent(String),
    Done(String),
}

#[derive(Clone)]
pub struct Lead {
    time: SharedString,
    text: SharedString,
    blocks: Rc<Vec<Block>>,
}

impl PartialEq for Lead {
    fn eq(&self, other: &Self) -> bool {
        self.time == other.time && self.text == other.text
    }
}

#[derive(Clone, PartialEq)]
pub enum Item {
    You {
        time: SharedString,
        text: SharedString,
    },
    Lead(Lead),
    Tool {
        busy: bool,
        text: SharedString,
        failed: bool,
        tail: Option<SharedString>,
    },
    Agent {
        mark: AgentMark,
        name: SharedString,
        summary: SharedString,
    },
    Note(SharedString),
    Failure(SharedString),
    Foot(SharedString),
}

pub fn settled(session: &Session, entry: &Entry) -> bool {
    match entry {
        Entry::Message(at) => session.messages.get(*at).is_none_or(|m| m.complete),
        Entry::Tool(id) => session.tools.get(id).is_none_or(|t| t.output.is_some()),
        Entry::Agent(_) => false,
        Entry::Task(_) | Entry::Done(_) => true,
    }
}

pub fn item(session: &Session, entry: &Entry) -> Option<Item> {
    match entry {
        Entry::Message(at) => {
            let message = session.messages.get(*at).filter(|m| m.agent.is_none())?;
            let text = SharedString::from(message.text.clone());
            let time = clock(session, &message.turn);
            match message.role {
                Role::User | Role::Steer => Some(Item::You { time, text }),
                Role::Assistant if !text.is_empty() => Some(Item::Lead(Lead {
                    blocks: Rc::new(blocks(&text)),
                    time,
                    text,
                })),
                Role::Assistant | Role::Thinking => None,
                Role::Note => Some(Item::Note(text)),
                Role::Failure => Some(Item::Failure(text)),
            }
        }
        Entry::Task(turn) => {
            let task = &session.turns.get(turn)?.task;
            (!task.is_empty()).then(|| Item::You {
                time: clock(session, turn),
                text: task.clone().into(),
            })
        }
        Entry::Tool(id) => {
            let tool = session.tools.get(id).filter(|tool| tool.agent.is_none())?;
            Some(Item::Tool {
                busy: tool.output.is_none(),
                text: format!("{} {}", tool.name, argument(&tool.args)).into(),
                failed: tool.failed,
                tail: tool
                    .output
                    .as_deref()
                    .map(|output| format!("{} lines", output.lines().count()).into()),
            })
        }
        Entry::Agent(id) => {
            let agent = session.agents.get(id)?;
            Some(Item::Agent {
                mark: mark(&agent.state),
                name: format!("{} {}", agent.kind, agent.number).into(),
                summary: agent
                    .report
                    .clone()
                    .unwrap_or_else(|| agent.task.clone())
                    .into(),
            })
        }
        Entry::Done(turn) => {
            let seconds = session.turns.get(turn)?.worked_for_ms / 1000;
            let took = match seconds {
                0..60 => format!("{seconds}s"),
                _ => format!("{}m {}s", seconds / 60, seconds % 60),
            };
            Some(Item::Foot(format!("cooked for {took}").into()))
        }
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

fn argument(args: &Value) -> String {
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

fn blocks(text: &str) -> Vec<Block> {
    text.split("\n\n")
        .map(str::trim)
        .filter(|part| !part.is_empty())
        .map(|part| {
            if let Some(heading) = part.strip_prefix('#') {
                return Block::Heading(heading.trim_start_matches('#').trim().to_owned().into());
            }
            let bullets: Option<Vec<SharedString>> = part
                .lines()
                .map(|line| {
                    line.trim_start()
                        .strip_prefix("- ")
                        .map(|item| item.to_owned().into())
                })
                .collect();
            bullets.map_or_else(|| Block::Para(spans(part)), Block::Bullets)
        })
        .collect()
}

fn spans(text: &str) -> Vec<Span> {
    text.split('`')
        .enumerate()
        .filter(|(_, piece)| !piece.is_empty())
        .map(|(at, piece)| match at % 2 {
            1 => Span::Code(piece.to_owned().into()),
            _ => Span::Plain(piece.to_owned().into()),
        })
        .collect()
}

pub fn pieces(item: &Item) -> Vec<String> {
    match item {
        Item::You { text, .. } => vec![text.to_string()],
        Item::Lead(said) => chat::pieces(&said.blocks),
        Item::Tool { .. }
        | Item::Agent { .. }
        | Item::Note(_)
        | Item::Failure(_)
        | Item::Foot(_) => Vec::new(),
    }
}

pub fn render(item: &Item, at: usize, marks: &[Marks], theme: &Theme) -> AnyElement {
    match item {
        Item::You { time, text } => you(
            time.clone(),
            text.clone(),
            false,
            None,
            at == 0,
            marks.first().map_or(&[][..], Vec::as_slice),
            theme,
        )
        .into_any_element(),
        Item::Lead(said) => lead(said.time.clone(), &said.blocks, marks, theme).into_any_element(),
        Item::Tool {
            busy,
            text,
            failed,
            tail,
        } => command(
            ("chat-tool", at),
            *busy,
            text.clone(),
            failed.then(|| Verdict::Exit("failed".into())),
            tail.clone(),
            theme,
        )
        .into_any_element(),
        Item::Agent {
            mark,
            name,
            summary,
        } => agent_row(
            ("chat-agent", at),
            &Agent {
                mark: *mark,
                name: name.clone(),
                summary: summary.clone(),
                link: None,
            },
            theme,
        )
        .into_any_element(),
        Item::Note(text) => note(text.clone(), theme).into_any_element(),
        Item::Failure(text) => fail("failed", text.clone(), "", theme).into_any_element(),
        Item::Foot(text) => foot(text.clone(), theme).into_any_element(),
    }
}
