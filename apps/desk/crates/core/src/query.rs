use std::collections::BTreeMap;
use std::fmt;
use std::time::{SystemTime, UNIX_EPOCH};

use serde::{Deserialize, Serialize};

use crate::protocol::{
    AccountStatus, AccountStatusState, Accounts, ContextBand, ContextOccupancy, CredentialReport,
    QuotaWindow, UsageReport, WindowReport, WindowStatus,
};

const SECONDS_PER_DAY: u64 = 86_400;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct QueryError {
    pub method: &'static str,
    pub reason: String,
}

impl fmt::Display for QueryError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{} got no answer: {}", self.method, self.reason)
    }
}

impl std::error::Error for QueryError {}

#[derive(Debug, Clone, PartialEq)]
pub struct Read<T> {
    pub value: T,
    pub at: String,
}

impl<T> Read<T> {
    pub fn now(value: T) -> Self {
        Read {
            value,
            at: stamp(SystemTime::now()),
        }
    }
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

pub const SERVING: &str = "serving";

impl ContextOccupancy {
    pub fn bands(&self) -> [(&'static str, &ContextBand); 4] {
        [
            ("identity", &self.identity),
            ("facts", &self.facts),
            ("working_set", &self.working_set),
            ("recent", &self.recent),
        ]
    }
}

impl UsageReport {
    pub fn live(&self, quota: &[QuotaWindow]) -> Vec<CredentialReport> {
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
                        Some(WindowReport {
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

pub fn is_email(text: &str) -> bool {
    text.split_once('@').is_some_and(|(local, domain)| {
        !local.is_empty()
            && domain.contains('.')
            && !domain.contains('@')
            && !text.contains(char::is_whitespace)
    })
}

pub fn display_name(source: &str) -> String {
    let words: Vec<String> = source
        .split('-')
        .map(|word| {
            let mut chars = word.chars();
            chars
                .next()
                .map(|first| first.to_uppercase().chain(chars).collect())
                .unwrap_or_default()
        })
        .collect();
    words.join(" ")
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(transparent)]
pub struct FilledEmails(BTreeMap<String, BTreeMap<i64, String>>);

impl FilledEmails {
    pub fn get(&self, source: &str, id: i64) -> Option<&str> {
        self.0.get(source)?.get(&id).map(String::as_str)
    }

    pub fn set(&mut self, source: &str, id: i64, email: Option<String>) {
        let accounts = self.0.entry(source.to_owned()).or_default();
        match email {
            Some(email) => {
                accounts.insert(id, email);
            }
            None => {
                accounts.remove(&id);
            }
        }
        self.0.retain(|_, accounts| !accounts.is_empty());
    }

    pub fn not_emails(&self) -> Vec<&str> {
        self.0
            .values()
            .flat_map(BTreeMap::values)
            .map(String::as_str)
            .filter(|email| !is_email(email))
            .collect()
    }

    pub fn filled(&self, mut accounts: Accounts) -> Accounts {
        for subscription in &mut accounts.subscriptions {
            for account in &mut subscription.accounts {
                if account.email().is_none()
                    && let Some(email) = self.get(&subscription.source, account.id)
                {
                    email.clone_into(&mut account.account);
                }
            }
        }
        accounts
    }
}

impl AccountStatusState {
    pub fn said(&self) -> &str {
        match self {
            AccountStatusState::InUse => "in use",
            AccountStatusState::Standby => "standby",
            AccountStatusState::SetAside => "set aside",
            AccountStatusState::Unread => "not read yet",
            AccountStatusState::Unchecked => "not checked",
            AccountStatusState::RefreshFailed => "refresh failed",
            AccountStatusState::Expired => "expired",
            AccountStatusState::Refused => "refused",
            AccountStatusState::Spent => "spent",
            AccountStatusState::RateLimited => "rate limited",
            AccountStatusState::Unknown(raw) => raw,
        }
    }

    pub fn serving(&self) -> bool {
        matches!(
            self,
            AccountStatusState::InUse | AccountStatusState::Standby
        )
    }
}

impl AccountStatus {
    pub fn email(&self) -> Option<&str> {
        is_email(&self.account).then_some(self.account.as_str())
    }

    pub fn live(&self, source: &str, quota: &[QuotaWindow]) -> Vec<WindowStatus> {
        let account = format!("#{}", self.id);
        let prefix = format!("{source} ");
        let live: Vec<WindowStatus> = quota
            .iter()
            .filter(|window| window.account == account && window.reported)
            .filter_map(|window| {
                Some(WindowStatus {
                    id: window.window.strip_prefix(&prefix)?.to_owned(),
                    only: Vec::new(),
                    resets_at: window.resets_at.clone(),
                    used: window.percent / 100.0,
                })
            })
            .collect();
        if live.is_empty() {
            self.windows.clone()
        } else {
            live
        }
    }
}

fn stamp(time: SystemTime) -> String {
    let seconds = time
        .duration_since(UNIX_EPOCH)
        .map_or(0, |since| since.as_secs());
    let clock = seconds % SECONDS_PER_DAY;
    let shifted = seconds / SECONDS_PER_DAY + 719_468;
    let era = shifted / 146_097;
    let of_era = shifted % 146_097;
    let year_of_era = (of_era - of_era / 1460 + of_era / 36_524 - of_era / 146_096) / 365;
    let of_year = of_era - (365 * year_of_era + year_of_era / 4 - year_of_era / 100);
    let month_index = (5 * of_year + 2) / 153;
    let day = of_year - (153 * month_index + 2) / 5 + 1;
    let month = if month_index < 10 {
        month_index + 3
    } else {
        month_index - 9
    };
    let year = year_of_era + era * 400 + u64::from(month <= 2);
    format!(
        "{year:04}-{month:02}-{day:02}T{:02}:{:02}:{:02}Z",
        clock / 3600,
        clock / 60 % 60,
        clock % 60
    )
}
