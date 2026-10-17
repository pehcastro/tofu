use std::collections::BTreeMap;
use std::fmt;

use crate::protocol::boardy::{BoardSummary, BoardsAnswer, Event, Row, RowsAnswer, Ticket, Words};
use crate::protocol::{BoardyEventKind, BoardyStatus as Status};
use crate::settings::{Settings, Value};

pub const MANAGEMENT: &str = "projectManagement";
pub const FLOW: &str = "boardFlow";
pub const ALL_VIEW: &str = "all";
const BOARDY: &str = "boardy";
const OFF: &str = "off";
const PERSON: &str = "person";
const REPORT_ECHO: &str = "report:";
const SESSION_ID_LEN: usize = 36;
const SESSION_ID_DASHES: [usize; 4] = [8, 13, 18, 23];

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
    Unknown,
    Lead,
    Agent,
    Named,
}

impl Speaker {
    pub fn of(actor: &str) -> Speaker {
        if actor.is_empty() || actor == PERSON {
            Speaker::Unknown
        } else if actor.contains('/') {
            Speaker::Agent
        } else if session_id(actor) {
            Speaker::Lead
        } else {
            Speaker::Named
        }
    }
}

fn session_id(actor: &str) -> bool {
    actor.len() == SESSION_ID_LEN
        && actor
            .char_indices()
            .all(|(at, c)| match SESSION_ID_DASHES.contains(&at) {
                true => c == '-',
                false => c.is_ascii_hexdigit(),
            })
}

pub fn spoken(event: &Event) -> bool {
    match event.kind {
        BoardyEventKind::Commented
        | BoardyEventKind::Asked
        | BoardyEventKind::Answered
        | BoardyEventKind::Spawned
        | BoardyEventKind::Reported
        | BoardyEventKind::Messaged => !event.text.as_deref().unwrap_or_default().is_empty(),
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
        | BoardyEventKind::Hook
        | BoardyEventKind::HookFailed
        | BoardyEventKind::Unknown(_) => false,
    }
}

pub fn report_echoes(events: &[Event]) -> Vec<bool> {
    let mut reported: Option<&str> = None;
    events
        .iter()
        .map(|event| {
            let text = event.text.as_deref().map(str::trim);
            match event.kind {
                BoardyEventKind::Reported => {
                    reported = text;
                    false
                }
                BoardyEventKind::Commented => {
                    let echo = text
                        .and_then(|text| text.strip_prefix(REPORT_ECHO))
                        .is_some_and(|rest| reported == Some(rest.trim()));
                    if echo {
                        reported = None;
                    }
                    echo
                }
                _ => false,
            }
        })
        .collect()
}

pub fn row(ticket: Ticket) -> Row {
    Row {
        id: ticket.id,
        title: ticket.title,
        status: ticket.status,
        priority: ticket.priority,
        epic: ticket.epic,
        sprint: ticket.sprint,
        assignee: ticket.assignee,
        labels: ticket.labels,
        questions: i64::try_from(ticket.questions.len()).ok(),
        created: ticket.created,
        updated: ticket.updated,
    }
}

pub fn wants_attention(ticket: &Row) -> bool {
    ticket.questions.unwrap_or_default() > 0
        || matches!(
            ticket.status,
            Status::NeedsDecision | Status::Blocked | Status::Review | Status::Triage
        )
}

pub fn running(ticket: &Row) -> bool {
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
    tickets: BTreeMap<String, Vec<Row>>,
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

    pub fn rows_landed(&mut self, answer: RowsAnswer) -> Result<(), BoardyError> {
        let mut tickets = answer.rows;
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
            *held = row(ticket);
        }
    }

    pub fn read(&self, board: &str) -> bool {
        self.tickets.contains_key(board)
    }

    pub fn tickets(&self, board: &str) -> &[Row] {
        self.tickets.get(board).map_or(&[], Vec::as_slice)
    }

    pub fn find(&self, id: &str) -> Option<&Row> {
        self.tickets
            .values()
            .flatten()
            .find(|ticket| ticket.id == id)
    }

    pub fn columns(&self, board: &str) -> Vec<(Status, Vec<&Row>)> {
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

    pub fn attention(&self) -> Vec<&Row> {
        self.tickets
            .values()
            .flatten()
            .filter(|ticket| wants_attention(ticket))
            .collect()
    }

    pub fn newest(&self, count: usize) -> Vec<&Row> {
        let mut all: Vec<&Row> = self.tickets.values().flatten().collect();
        all.sort_by(|a, b| b.created.cmp(&a.created));
        all.truncate(count);
        all
    }
}
