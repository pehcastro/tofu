use std::collections::BTreeMap;

use super::ModelError;
use crate::protocol::{StatusReport, status::Record};

const LEAD: &str = "tofu";
const AGENTS: &str = "agents/";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum StatusState {
    Idle,
    Working,
    Done,
    Blocked,
    Error,
    Clear,
}

impl StatusState {
    pub fn read(word: &str) -> Result<Self, ModelError> {
        match word {
            "idle" => Ok(Self::Idle),
            "working" => Ok(Self::Working),
            "done" => Ok(Self::Done),
            "blocked" => Ok(Self::Blocked),
            "error" => Ok(Self::Error),
            "clear" => Ok(Self::Clear),
            other => Err(ModelError::UnknownValue {
                field: "status state",
                value: other.to_owned(),
            }),
        }
    }

    pub fn word(self) -> &'static str {
        match self {
            Self::Idle => "idle",
            Self::Working => "working",
            Self::Done => "done",
            Self::Blocked => "blocked",
            Self::Error => "error",
            Self::Clear => "clear",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Status {
    pub state: StatusState,
    pub kind: Option<String>,
    pub msg: Option<String>,
}

pub type Change = (String, StatusState);

#[derive(Debug, Default)]
pub struct Statuses {
    sessions: BTreeMap<String, BTreeMap<String, Status>>,
    listed_for: Option<String>,
}

impl Statuses {
    pub fn of(&self, session: &str) -> Option<&BTreeMap<String, Status>> {
        self.sessions.get(session)
    }

    pub fn activity(&self, session: &str) -> Option<StatusState> {
        self.of(session)?
            .iter()
            .filter(|(id, _)| *id == LEAD || id.starts_with(AGENTS))
            .map(|(_, status)| match status.state {
                StatusState::Blocked => (2, StatusState::Blocked),
                StatusState::Working => (1, StatusState::Working),
                StatusState::Idle | StatusState::Done | StatusState::Error | StatusState::Clear => {
                    (0, StatusState::Idle)
                }
            })
            .max_by_key(|(urgency, _)| *urgency)
            .map(|(_, state)| state)
    }

    pub fn take_due(&mut self, open: &str) -> bool {
        if self.listed_for.as_deref() == Some(open) {
            return false;
        }
        self.listed_for = Some(open.to_owned());
        true
    }

    pub(super) fn again(&mut self) {
        self.listed_for = None;
    }

    pub(super) fn report(&mut self, report: &StatusReport) -> Result<(), ModelError> {
        let status = Status {
            state: StatusState::read(&report.state)?,
            kind: report.kind.clone(),
            msg: report.msg.clone(),
        };
        let records = self.sessions.entry(report.session.clone()).or_default();
        if status.state == StatusState::Clear {
            records.remove(&report.item);
        } else {
            records.insert(report.item.clone(), status);
        }
        Ok(())
    }

    pub fn listed(&mut self, session: &str, list: &[Record]) -> Result<Vec<Change>, ModelError> {
        let mut now = BTreeMap::new();
        for record in list {
            let id = record.id.clone().ok_or_else(|| ModelError::UnknownValue {
                field: "status id",
                value: String::new(),
            })?;
            let state = StatusState::read(&record.state)?;
            if state != StatusState::Clear {
                let status = Status {
                    state,
                    kind: record.kind.clone(),
                    msg: record.msg.clone(),
                };
                now.insert(id, status);
            }
        }
        let was = self.sessions.remove(session).unwrap_or_default();
        let gone = was
            .keys()
            .filter(|id| !now.contains_key(*id))
            .map(|id| (id.clone(), StatusState::Clear));
        let changes = now
            .iter()
            .filter(|(id, status)| was.get(*id) != Some(*status))
            .map(|(id, status)| (id.clone(), status.state))
            .chain(gone)
            .collect();
        self.sessions.insert(session.to_owned(), now);
        Ok(changes)
    }
}
