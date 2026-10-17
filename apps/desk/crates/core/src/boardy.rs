use std::collections::BTreeMap;
use std::fmt;

use crate::protocol::boardy::{BoardSummary, BoardsAnswer, Event, ListAnswer, Ticket, Words};
use crate::protocol::{BoardyEventKind, BoardyTicketStatus as Status};
use crate::settings::{Settings, Value};

pub const MANAGEMENT: &str = "projectManagement";
pub const FLOW: &str = "boardFlow";
pub const ALL_VIEW: &str = "all";
const BOARDY: &str = "boardy";
const OFF: &str = "off";
const PERSON: &str = "person";

pub const COLUMNS: [Status; 9] = [
    Status::Triage,
    Status::Backlog,
    Status::Todo,
    Status::Doing,
    Status::Review,
    Status::NeedsDecision,
    Status::Blocked,
    Status::Done,
    Status::Dropped,
];

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Flow {
    Jira,
    Linear,
}

impl Flow {
    pub fn landing(self) -> Status {
        match self {
            Flow::Jira => Status::Backlog,
            Flow::Linear => Status::Triage,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Gate {
    pub on: bool,
    pub flow: Flow,
}

impl Gate {
    pub fn read(settings: &Settings) -> Result<Gate, BoardyError> {
        let text = |key: &'static str| match settings.get(key).map(|setting| &setting.value) {
            None => Ok(String::new()),
            Some(Value::Text(text) | Value::Choice(text, _)) => Ok(text.clone()),
            Some(other) => Err(BoardyError::Setting {
                key,
                value: other.wire(),
            }),
        };
        let on = match text(MANAGEMENT)?.as_str() {
            "" | OFF => false,
            BOARDY => true,
            other => {
                return Err(BoardyError::Setting {
                    key: MANAGEMENT,
                    value: other.to_owned(),
                });
            }
        };
        let flow = match text(FLOW)?.as_str() {
            "" | "jira" => Flow::Jira,
            "linear" => Flow::Linear,
            other => {
                return Err(BoardyError::Setting {
                    key: FLOW,
                    value: other.to_owned(),
                });
            }
        };
        Ok(Gate { on, flow })
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum BoardyError {
    Setting { key: &'static str, value: String },
    Status(String),
}

impl fmt::Display for BoardyError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            BoardyError::Setting { key, value } => {
                write!(f, "tofu sent {value} for {key}, which boardy does not know")
            }
            BoardyError::Status(status) => {
                write!(
                    f,
                    "tofu sent the status {status}, which boardy does not know"
                )
            }
        }
    }
}

impl std::error::Error for BoardyError {}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Speaker {
    You,
    Lead,
    Agent,
}

impl Speaker {
    pub fn of(actor: &str) -> Speaker {
        if actor == PERSON {
            Speaker::You
        } else if actor.contains('/') {
            Speaker::Agent
        } else {
            Speaker::Lead
        }
    }
}

pub fn spoken(event: &Event) -> bool {
    match event.kind {
        BoardyEventKind::Commented
        | BoardyEventKind::Asked
        | BoardyEventKind::Answered
        | BoardyEventKind::Spawned
        | BoardyEventKind::Reported => !event.text.as_deref().unwrap_or_default().is_empty(),
        BoardyEventKind::Created
        | BoardyEventKind::Moved
        | BoardyEventKind::Rule
        | BoardyEventKind::Waiting
        | BoardyEventKind::Requested
        | BoardyEventKind::Accepted
        | BoardyEventKind::RequestDropped
        | BoardyEventKind::Handed
        | BoardyEventKind::Widened
        | BoardyEventKind::Managers
        | BoardyEventKind::Assigned
        | BoardyEventKind::Unknown(_) => false,
    }
}

pub fn wants_attention(ticket: &Ticket) -> bool {
    !ticket.questions.is_empty()
        || matches!(
            ticket.status,
            Status::NeedsDecision | Status::Blocked | Status::Review | Status::Triage
        )
}

pub fn running(ticket: &Ticket) -> bool {
    ticket.status == Status::Doing
        && ticket
            .assignee
            .as_deref()
            .is_some_and(|who| who.contains('/'))
}

#[derive(Clone, Debug, Default)]
pub struct BoardyStore {
    pub boards: Vec<BoardSummary>,
    pub words: Words,
    tickets: BTreeMap<String, Vec<Ticket>>,
}

impl BoardyStore {
    pub fn boards_landed(&mut self, answer: BoardsAnswer) {
        let mut boards = answer.boards;
        boards.sort_by(|a, b| a.key.cmp(&b.key));
        self.tickets
            .retain(|key, _| boards.iter().any(|board| board.key == *key));
        self.boards = boards;
        self.words = answer.words;
    }

    pub fn list_landed(&mut self, answer: ListAnswer) -> Result<(), BoardyError> {
        let mut tickets: Vec<Ticket> = answer
            .groups
            .into_iter()
            .flat_map(|group| group.tickets)
            .collect();
        if let Some(unknown) = tickets.iter().find_map(|ticket| match &ticket.status {
            Status::Unknown(raw) => Some(raw.clone()),
            _ => None,
        }) {
            return Err(BoardyError::Status(unknown));
        }
        tickets.sort_by(|a, b| b.created.cmp(&a.created));
        self.words = answer.words;
        self.tickets.insert(answer.board, tickets);
        Ok(())
    }

    pub fn ticket_landed(&mut self, ticket: Ticket) {
        let found = self
            .tickets
            .values_mut()
            .flat_map(|tickets| tickets.iter_mut())
            .find(|held| held.id == ticket.id);
        if let Some(held) = found {
            *held = ticket;
        }
    }

    pub fn read(&self, board: &str) -> bool {
        self.tickets.contains_key(board)
    }

    pub fn tickets(&self, board: &str) -> &[Ticket] {
        self.tickets.get(board).map_or(&[], Vec::as_slice)
    }

    pub fn find(&self, id: &str) -> Option<&Ticket> {
        self.tickets
            .values()
            .flatten()
            .find(|ticket| ticket.id == id)
    }

    pub fn columns(&self, board: &str) -> Vec<(Status, Vec<&Ticket>)> {
        let tickets = self.tickets(board);
        COLUMNS
            .iter()
            .map(|status| {
                let held = tickets
                    .iter()
                    .filter(|ticket| ticket.status == *status)
                    .collect();
                (status.clone(), held)
            })
            .collect()
    }

    pub fn attention(&self) -> Vec<&Ticket> {
        self.tickets
            .values()
            .flatten()
            .filter(|ticket| wants_attention(ticket))
            .collect()
    }

    pub fn newest(&self, count: usize) -> Vec<&Ticket> {
        let mut all: Vec<&Ticket> = self.tickets.values().flatten().collect();
        all.sort_by(|a, b| b.created.cmp(&a.created));
        all.truncate(count);
        all
    }
}
