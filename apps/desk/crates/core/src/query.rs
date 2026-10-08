use std::fmt;

use serde::Deserialize;
use serde::de::DeserializeOwned;

use crate::protocol::{QuotaWindow, VerbResult};

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum QueryError {
    Unanswered {
        method: &'static str,
        reason: String,
    },
    WrongVerb {
        asked: &'static str,
        answered: String,
    },
    Refused {
        verb: &'static str,
        problems: Vec<String>,
    },
    Shape {
        verb: &'static str,
        reason: String,
    },
}

impl fmt::Display for QueryError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Unanswered { method, reason } => write!(f, "{method} got no answer: {reason}"),
            Self::WrongVerb { asked, answered } => {
                write!(f, "asked tofu {asked} and it answered {answered}")
            }
            Self::Refused { verb, problems } => {
                write!(f, "tofu {verb} refused: {}", problems.join("; "))
            }
            Self::Shape { verb, reason } => {
                write!(
                    f,
                    "tofu {verb} answered a shape the desk does not know: {reason}"
                )
            }
        }
    }
}

impl std::error::Error for QueryError {}

#[derive(Debug, Clone, PartialEq)]
pub struct Read<T> {
    pub value: T,
    pub at: String,
}

#[derive(Debug, Clone, PartialEq)]
pub struct Answer<T> {
    pub read: Option<Read<T>>,
    pub failed: Option<QueryError>,
    pub asking: bool,
    pub stale: bool,
    wanted: bool,
    due: bool,
}

impl<T> Default for Answer<T> {
    fn default() -> Self {
        Answer {
            read: None,
            failed: None,
            asking: false,
            stale: false,
            wanted: false,
            due: true,
        }
    }
}

impl<T> Answer<T> {
    pub fn want(&mut self) {
        self.wanted = true;
    }

    pub fn again(&mut self) {
        self.due = true;
    }

    pub fn notified(&mut self) {
        self.stale = true;
    }

    pub fn take_due(&mut self) -> bool {
        let due = self.wanted && self.due && !self.asking;
        if due {
            self.due = false;
            self.asking = true;
        }
        due
    }

    pub fn answered(&mut self, answer: Result<Read<T>, QueryError>) {
        self.asking = false;
        match answer {
            Ok(read) => {
                self.read = Some(read);
                self.failed = None;
                self.stale = false;
            }
            Err(error) => self.failed = Some(error),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize)]
pub enum UsageState {
    #[serde(rename = "serving")]
    Serving,
    #[serde(rename = "needs attention")]
    NeedsAttention,
    #[serde(rename = "none")]
    Nothing,
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Usage {
    pub state: UsageState,
    pub fullest_window: Option<String>,
    pub providers: Vec<Provider>,
    pub spend_limit: String,
    #[serde(default)]
    pub missing: Vec<Missing>,
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Provider {
    pub provider: String,
    pub plan: Option<String>,
    pub state: String,
    #[serde(default)]
    pub windows: Vec<Window>,
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Window {
    pub id: String,
    pub used_fraction: f64,
    pub used_reported: bool,
    pub resets_at: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Missing {
    pub label: String,
    pub what: String,
    pub command: String,
}

pub const SERVING: &str = "serving";

pub fn usage(result: VerbResult) -> Result<Read<Usage>, QueryError> {
    enveloped(result, "usage")
}

fn enveloped<T: DeserializeOwned>(
    result: VerbResult,
    verb: &'static str,
) -> Result<Read<T>, QueryError> {
    if result.verb != verb {
        return Err(QueryError::WrongVerb {
            asked: verb,
            answered: result.verb,
        });
    }
    if !result.ok {
        return Err(QueryError::Refused {
            verb,
            problems: result
                .problems
                .into_iter()
                .map(|problem| match problem.hint {
                    Some(hint) => format!("{} ({hint})", problem.what),
                    None => problem.what,
                })
                .collect(),
        });
    }
    let value = serde_json::from_value(result.data).map_err(|error| QueryError::Shape {
        verb,
        reason: error.to_string(),
    })?;
    Ok(Read {
        value,
        at: result.at,
    })
}

impl Usage {
    pub fn live(&self, quota: &[QuotaWindow]) -> Vec<Provider> {
        let mut accounts: Vec<(&str, &str)> = Vec::new();
        for window in quota {
            let provider = window.window.split(' ').next().unwrap_or_default();
            if !accounts
                .iter()
                .any(|(account, _)| *account == window.account)
            {
                accounts.push((&window.account, provider));
            }
        }
        self.providers
            .iter()
            .map(|provider| {
                let mut provider = provider.clone();
                let Some(at) = accounts
                    .iter()
                    .position(|(_, name)| *name == provider.provider)
                else {
                    return provider;
                };
                let (account, _) = accounts.remove(at);
                let prefix = format!("{} ", provider.provider);
                provider.windows = quota
                    .iter()
                    .filter(|live| live.account == account)
                    .filter_map(|live| {
                        Some(Window {
                            id: live.window.strip_prefix(&prefix)?.to_owned(),
                            used_fraction: live.percent / 100.0,
                            used_reported: live.reported,
                            resets_at: live.resets_at.clone(),
                        })
                    })
                    .collect();
                provider
            })
            .collect()
    }
}
