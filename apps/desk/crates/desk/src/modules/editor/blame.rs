use std::time::{SystemTime, UNIX_EPOCH};

use desk_core::git;
use gpui::SharedString;

const MINUTE: i64 = 60;
const HOUR: i64 = 60 * MINUTE;
const DAY: i64 = 24 * HOUR;
const WEEK: i64 = 7 * DAY;
const MONTH: i64 = 30 * DAY;
const YEAR: i64 = 365 * DAY;
const YOU: &str = "you";
const NOT_COMMITTED: &str = "not committed";
const NOREPLY: &str = "@users.noreply.github.com";

pub struct Blamed {
    pub text: String,
    pub lines: Vec<git::BlameLine>,
    pub own_email: Option<String>,
}

#[derive(Clone, PartialEq)]
pub struct Inline {
    pub row: usize,
    pub user: Option<SharedString>,
    pub author: SharedString,
    pub email: SharedString,
    pub when: SharedString,
    pub mine: bool,
}

impl Inline {
    pub fn name(&self) -> SharedString {
        let named = Some(self.author.clone()).filter(|author| !author.trim().is_empty());
        self.user
            .clone()
            .or(named)
            .unwrap_or_else(|| self.email.clone())
    }
}

pub struct Me<'a> {
    pub email: Option<&'a str>,
    pub login: Option<&'a str>,
}

fn noreply_login(email: &str) -> Option<&str> {
    let local = email.strip_suffix(NOREPLY)?;
    Some(local.rsplit('+').next().unwrap_or(local)).filter(|login| !login.is_empty())
}

impl Me<'_> {
    fn owns(&self, email: &str) -> bool {
        let noreply = self.login.is_some_and(|login| {
            noreply_login(email).is_some_and(|local| local.eq_ignore_ascii_case(login))
        });
        noreply || self.email == Some(email)
    }

    fn user(&self, email: &str) -> Option<SharedString> {
        self.login
            .filter(|_| self.owns(email))
            .or_else(|| noreply_login(email))
            .map(|login| login.to_owned().into())
    }
}

fn now() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .ok()
        .and_then(|since| i64::try_from(since.as_secs()).ok())
        .unwrap_or(0)
}

fn age(seconds: i64) -> String {
    let (count, unit) = match seconds.max(0) {
        seconds if seconds < MINUTE => return "just now".to_owned(),
        seconds if seconds < HOUR => (seconds / MINUTE, "minute"),
        seconds if seconds < DAY => (seconds / HOUR, "hour"),
        seconds if seconds < WEEK => (seconds / DAY, "day"),
        seconds if seconds < MONTH => (seconds / WEEK, "week"),
        seconds if seconds < YEAR => (seconds / MONTH, "month"),
        seconds => (seconds / YEAR, "year"),
    };
    let plural = if count == 1 { "" } else { "s" };
    format!("{count} {unit}{plural} ago")
}

pub fn inline(blamed: &Blamed, row: usize, me: &Me) -> Option<Inline> {
    let line = blamed.lines.get(row)?;
    let email = line.email.clone().into();
    Some(match line.commit {
        None => Inline {
            row,
            user: None,
            author: YOU.into(),
            email,
            when: NOT_COMMITTED.into(),
            mine: true,
        },
        Some(_) => Inline {
            row,
            user: me.user(&line.email),
            author: line.author.clone().into(),
            mine: me.owns(&line.email),
            email,
            when: age(now().saturating_sub(line.time)).into(),
        },
    })
}
