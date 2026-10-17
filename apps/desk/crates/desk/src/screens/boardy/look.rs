use chrono::{DateTime, Utc};
use desk_core::protocol::{BoardyPriority, BoardyStatus as Status};
use desk_ui::components::priority_icon::Priority;
use desk_ui::components::status_icon::TicketStatus;
use gpui::{Div, FontWeight, Rgba, div, prelude::*, px};

const DISC: f32 = 22.0;
const DISC_TEXT: f32 = 10.5;
const DISC_FILL: f32 = 0.18;
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

pub fn disc(letter: &str, color: Rgba) -> Div {
    div()
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .size(px(DISC))
        .rounded_full()
        .bg(desk_ui::components::paint::tint(color, DISC_FILL))
        .text_color(color)
        .text_size(px(DISC_TEXT))
        .font_weight(FontWeight::SEMIBOLD)
        .child(letter.to_owned())
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

pub fn day(at: &str) -> String {
    DateTime::parse_from_rfc3339(at)
        .map_or_else(|_| at.to_owned(), |at| at.format("%b %-d").to_string())
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
