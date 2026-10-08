mod session;

use std::collections::BTreeMap;
use std::fmt;

use crate::bridge::Event;
use crate::protocol::{CronState, Notification, QuotaWindow, ServerRequest};
use crate::query::{Answer, Usage};

pub use session::{Agent, Message, Role, Session, Shell, Tool, Turn};

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ModelError {
    UnknownEvent(String),
    UnknownValue { field: &'static str, value: String },
    Unreadable { line: String, reason: String },
    Orphan { event: &'static str, id: String },
}

impl fmt::Display for ModelError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::UnknownEvent(kind) => write!(f, "no reducer knows the event kind {kind:?}"),
            Self::UnknownValue { field, value } => {
                write!(f, "{field} has the unknown value {value:?}")
            }
            Self::Unreadable { line, reason } => write!(f, "an unreadable line ({reason}): {line}"),
            Self::Orphan { event, id } => {
                write!(f, "{event} names {id}, which no earlier event started")
            }
        }
    }
}

impl std::error::Error for ModelError {}

#[derive(Debug, Default)]
pub struct Store {
    pub sessions: BTreeMap<String, Session>,
    pub usage: Answer<Usage>,
    pub quota: Vec<QuotaWindow>,
}

impl Store {
    pub fn apply_batch(&mut self, batch: &[Event]) -> Result<(), ModelError> {
        batch.iter().try_for_each(|event| self.apply(event))
    }

    pub fn cron_answered(&mut self, session: &str, state: CronState) {
        self.session(session).cron = Some(state);
    }

    fn apply(&mut self, event: &Event) -> Result<(), ModelError> {
        match event {
            Event::Notification(Notification::SessionForked(fork)) => {
                self.session(&fork.session).forks.push((**fork).clone());
                self.session(&fork.to).forked_from = Some(fork.from.clone());
                Ok(())
            }
            Event::Notification(Notification::Unknown { method, .. }) => {
                Err(ModelError::UnknownEvent(method.clone()))
            }
            Event::Notification(notification) => {
                if let Notification::QuotaUpdated(updated) = notification {
                    self.quota.clone_from(&updated.windows);
                    self.usage.notified();
                }
                let id = session_of(notification)?;
                self.session(id).apply(notification)
            }
            Event::Request {
                id,
                request: ServerRequest::TofuRequestApproval(asked),
            } => {
                self.session(&asked.session)
                    .ask(id.clone(), (**asked).clone());
                Ok(())
            }
            Event::Request {
                request: ServerRequest::Unknown { method, .. },
                ..
            } => Err(ModelError::UnknownEvent(method.clone())),
            Event::Unreadable { line, reason } => Err(ModelError::Unreadable {
                line: line.clone(),
                reason: reason.clone(),
            }),
        }
    }

    fn session(&mut self, id: &str) -> &mut Session {
        self.sessions.entry(id.to_owned()).or_default()
    }
}

fn session_of(notification: &Notification) -> Result<&str, ModelError> {
    use Notification as N;
    Ok(match notification {
        N::AgentEnded(e) => &e.session,
        N::AgentStarted(e) => &e.session,
        N::AgentUpdated(e) => &e.session,
        N::ApprovalResolved(e) => &e.session,
        N::ContextUpdated(e) => &e.session,
        N::CronUpdated(e) => &e.session,
        N::Decision(e) => &e.session,
        N::Failure(e) | N::Note(e) => &e.session,
        N::FileEdit(e) => &e.session,
        N::ItemPersisted(e) => &e.session,
        N::MessageCompleted(e) | N::MessageDelta(e) | N::ThinkingDelta(e) | N::TurnSteered(e) => {
            &e.session
        }
        N::MessageUser(e) => &e.session,
        N::MessageReset(e) | N::MessageStarted(e) => &e.session,
        N::PlanUpdated(e) => &e.session,
        N::QuotaUpdated(e) => &e.session,
        N::Resync(e) => &e.session,
        N::SessionForked(e) => &e.session,
        N::SessionUpdated(e) => &e.session,
        N::ShellExited(e) => &e.session,
        N::ShellOutput(e) => &e.session,
        N::ShellStarted(e) => &e.session,
        N::ToolCompleted(e) => &e.session,
        N::ToolStarted(e) => &e.session,
        N::TurnCompleted(e) => &e.session,
        N::TurnStarted(e) => &e.session,
        N::UsageUpdated(e) => &e.session,
        N::Unknown { method, .. } => return Err(ModelError::UnknownEvent(method.clone())),
    })
}
