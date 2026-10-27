use chrono::{DateTime, NaiveDate, Utc};
use desk_core::protocol::{BoardyPriority, BoardyStatus as Status};
use desk_ui::components::priority_icon::Priority;
use desk_ui::components::status_icon::{RunState, TicketStatus};

const SECONDS_PER_MINUTE: i64 = 60;
const MINUTES_PER_HOUR: i64 = 60;
const HOURS_PER_DAY: i64 = 24;
const DAYS_PER_WEEK: i64 = 7;
const DAYS_PER_MONTH: i64 = 30;

pub fn kit(status: &Status) -> Option<TicketStatus> {
    Some(match status {
        Status::Triage => TicketStatus::Triage,
        Status::Backlog => TicketStatus::Backlog,
        Status::Todo => TicketStatus::Todo,
        Status::Doing => TicketStatus::Doing,
        Status::Review => TicketStatus::Review,
        Status::Done => TicketStatus::Done,
        Status::Dropped => TicketStatus::Dropped,
        Status::Blocked => TicketStatus::Blocked,
        Status::NeedsDecision => TicketStatus::NeedsDecision,
        Status::Unknown(_) => return None,
    })
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Lane {
    NeedsYou,
    Running,
    Done,
}

impl Lane {
    pub const ALL: [Lane; 3] = [Lane::NeedsYou, Lane::Running, Lane::Done];

    pub fn label(self) -> &'static str {
        match self {
            Lane::NeedsYou => "Needs you",
            Lane::Running => "Running",
            Lane::Done => "Done",
        }
    }

    pub fn status(self) -> TicketStatus {
        match self {
            Lane::NeedsYou => TicketStatus::NeedsDecision,
            Lane::Running => TicketStatus::Doing,
            Lane::Done => TicketStatus::Done,
        }
    }
}

pub fn inbox(status: &Status, asked: bool) -> Option<(Lane, RunState)> {
    Some(match status {
        Status::Blocked => (Lane::NeedsYou, RunState::Failed),
        _ if asked => (Lane::NeedsYou, RunState::Waiting),
        Status::NeedsDecision | Status::Review => (Lane::NeedsYou, RunState::Waiting),
        Status::Doing => (Lane::Running, RunState::Running),
        Status::Done => (Lane::Done, RunState::Done),
        Status::Triage | Status::Backlog | Status::Todo | Status::Dropped | Status::Unknown(_) => {
            return None;
        }
    })
}

pub fn label(status: &Status) -> &str {
    match status {
        Status::Unknown(raw) => raw,
        known => kit(known).map_or("", TicketStatus::label),
    }
}

pub fn priority(priority: &BoardyPriority) -> Option<Priority> {
    Some(match priority {
        BoardyPriority::P0 => Priority::P0,
        BoardyPriority::P1 => Priority::P1,
        BoardyPriority::P2 => Priority::P2,
        BoardyPriority::P3 => Priority::P3,
        BoardyPriority::P4 => Priority::P4,
        BoardyPriority::Unknown(_) => return None,
    })
}

pub fn parent(id: &str) -> Option<&str> {
    id.rsplit_once('-')
        .map(|(head, _)| head)
        .filter(|head| head.contains('-'))
}

pub fn age(at: &str, now: DateTime<Utc>) -> String {
    let Ok(then) = DateTime::parse_from_rfc3339(at) else {
        return String::new();
    };
    let minutes = now.signed_duration_since(then).num_seconds().max(0) / SECONDS_PER_MINUTE;
    let hours = minutes / MINUTES_PER_HOUR;
    let days = hours / HOURS_PER_DAY;
    match (minutes, hours, days) {
        (0, _, _) => "now".to_owned(),
        (m, 0, _) => format!("{m}m"),
        (_, h, 0) => format!("{h}h"),
        (_, _, d) if d < DAYS_PER_WEEK * 2 => format!("{d}d"),
        (_, _, d) if d < DAYS_PER_MONTH * 2 => format!("{}w", d / DAYS_PER_WEEK),
        (_, _, d) => format!("{}mo", d / DAYS_PER_MONTH),
    }
}

pub fn ago(at: &str, now: DateTime<Utc>) -> String {
    match age(at, now).as_str() {
        "" => String::new(),
        "now" => "now".to_owned(),
        aged => format!("{aged} ago"),
    }
}

pub fn stamp(at: &str) -> Option<DateTime<Utc>> {
    DateTime::parse_from_rfc3339(at)
        .ok()
        .map(|at| at.with_timezone(&Utc))
}

pub fn span(from: DateTime<Utc>, to: DateTime<Utc>) -> String {
    let seconds = to.signed_duration_since(from).num_seconds().max(0);
    let minutes = seconds / SECONDS_PER_MINUTE;
    let hours = minutes / MINUTES_PER_HOUR;
    let days = hours / HOURS_PER_DAY;
    let (s, m, h) = (
        seconds % SECONDS_PER_MINUTE,
        minutes % MINUTES_PER_HOUR,
        hours % HOURS_PER_DAY,
    );
    match (days, hours, minutes) {
        (0, 0, 0) => format!("{s}s"),
        (0, 0, _) => format!("{m}m {s:02}s"),
        (0, _, _) => format!("{h}h {m:02}m"),
        (d, _, _) => format!("{d}d {h}h"),
    }
}

fn date(at: &str) -> Option<NaiveDate> {
    DateTime::parse_from_rfc3339(at)
        .map(|at| at.date_naive())
        .or_else(|_| NaiveDate::parse_from_str(at, "%Y-%m-%d"))
        .ok()
}

pub fn day(at: &str) -> String {
    date(at).map_or_else(|| at.to_owned(), |date| date.format("%b %-d").to_string())
}

pub fn past(at: &str, now: DateTime<Utc>) -> bool {
    date(at).is_some_and(|date| date < now.date_naive())
}

pub fn noun(word: &str, plural: bool) -> String {
    let mut chars = word.chars();
    let head: String = chars
        .next()
        .map(|c| c.to_uppercase().collect())
        .unwrap_or_default();
    let tail: String = chars.collect();
    match plural {
        true => format!("{head}{tail}s"),
        false => format!("{head}{tail}"),
    }
}
