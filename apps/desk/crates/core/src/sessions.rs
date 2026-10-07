use std::fmt;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use serde::Deserialize;

use crate::protocol::VerbResult;

const MINUTE: u64 = 60;
const HOUR: u64 = 60 * MINUTE;
const DAY: u64 = 24 * HOUR;

#[derive(Clone, Debug, PartialEq, Eq, Deserialize)]
pub struct SessionRow {
    pub id: String,
    #[serde(default)]
    pub name: String,
    pub at: String,
    #[serde(default)]
    pub outcome: String,
    #[serde(default)]
    pub ended_at: Option<String>,
    #[serde(default)]
    pub end_reason: String,
    #[serde(default)]
    pub expired: bool,
}

#[derive(Deserialize)]
struct Listing {
    sessions: Option<Vec<SessionRow>>,
}

#[derive(Debug)]
pub enum SessionListError {
    NotOk(String),
    Shape(serde_json::Error),
}

impl fmt::Display for SessionListError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            SessionListError::NotOk(problems) => write!(f, "session list failed: {problems}"),
            SessionListError::Shape(error) => write!(f, "session list is unreadable: {error}"),
        }
    }
}

impl std::error::Error for SessionListError {}

pub fn session_rows(listed: VerbResult) -> Result<Vec<SessionRow>, SessionListError> {
    if !listed.ok {
        let problems: Vec<String> = listed.problems.into_iter().map(|p| p.what).collect();
        return Err(SessionListError::NotOk(problems.join("; ")));
    }
    let listing: Listing = serde_json::from_value(listed.data).map_err(SessionListError::Shape)?;
    Ok(listing.sessions.unwrap_or_default())
}

impl SessionRow {
    pub fn title(&self) -> &str {
        if self.name.is_empty() {
            &self.id
        } else {
            &self.name
        }
    }

    pub fn state(&self) -> &str {
        match (self.outcome.as_str(), self.end_reason.as_str()) {
            ("", "") if self.ended_at.is_some() => "ended",
            ("", "") => "idle",
            ("", reason) => reason,
            (outcome, _) => outcome,
        }
    }

    pub fn age(&self, now: SystemTime) -> Option<String> {
        let since = now.duration_since(instant(&self.at)?).ok()?.as_secs();
        Some(match since {
            0..MINUTE => "just now".to_owned(),
            MINUTE..HOUR => format!("{}m ago", since / MINUTE),
            HOUR..DAY => format!("{}h ago", since / HOUR),
            _ => format!("{}d ago", since / DAY),
        })
    }
}

fn number(text: &str, from: usize, to: usize) -> Option<i64> {
    text.get(from..to)?.parse().ok()
}

fn instant(stamp: &str) -> Option<SystemTime> {
    let (year, month, day) = (
        number(stamp, 0, 4)?,
        number(stamp, 5, 7)?,
        number(stamp, 8, 10)?,
    );
    let (hour, minute, second) = (
        number(stamp, 11, 13)?,
        number(stamp, 14, 16)?,
        number(stamp, 17, 19)?,
    );
    let zone = stamp
        .get(19..)?
        .trim_start_matches(|c: char| c == '.' || c.is_ascii_digit());
    let offset = match zone {
        "Z" => 0,
        _ => {
            let sign = match zone.get(..1)? {
                "+" => 1,
                "-" => -1,
                _ => return None,
            };
            sign * (number(zone, 1, 3)? * 3600 + number(zone, 4, 6)? * 60)
        }
    };
    let seconds = days(year, month, day)? * 86_400 + hour * 3600 + minute * 60 + second - offset;
    Some(UNIX_EPOCH + Duration::from_secs(u64::try_from(seconds).ok()?))
}

fn days(year: i64, month: i64, day: i64) -> Option<i64> {
    if !(1..=12).contains(&month) || !(1..=31).contains(&day) {
        return None;
    }
    let year = if month <= 2 { year - 1 } else { year };
    let era = year.div_euclid(400);
    let of_era = year.rem_euclid(400);
    let of_year = (153 * ((month + 9) % 12) + 2) / 5 + day - 1;
    let of_cycle = of_era * 365 + of_era / 4 - of_era / 100 + of_year;
    Some(era * 146_097 + of_cycle - 719_468)
}
