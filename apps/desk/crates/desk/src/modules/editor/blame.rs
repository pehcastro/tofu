use std::time::{SystemTime, UNIX_EPOCH};

use desk_core::git;
use desk_ui::components::history::{Blame, BlameLine};
use gpui::SharedString;

const MINUTE: i64 = 60;
const HOUR: i64 = 60 * MINUTE;
const DAY: i64 = 24 * HOUR;
const WEEK: i64 = 7 * DAY;
const MONTH: i64 = 30 * DAY;
const YEAR: i64 = 365 * DAY;
const SHORT_SHA: usize = 7;
const YOU: &str = "you";
const NOT_COMMITTED: &str = "not committed";

pub struct Blamed {
    pub text: String,
    pub lines: Vec<git::BlameLine>,
}

pub type Inline = (usize, SharedString, SharedString);

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

pub fn inline(blamed: &Blamed, row: usize) -> Option<Inline> {
    let line = blamed.lines.get(row)?;
    Some(match line.commit {
        None => (row, YOU.into(), NOT_COMMITTED.into()),
        Some(_) => (
            row,
            line.author.clone().into(),
            age(now().saturating_sub(line.time)).into(),
        ),
    })
}

pub fn gutter(blamed: &Blamed) -> Vec<BlameLine> {
    blamed
        .lines
        .iter()
        .zip(1..)
        .map(|(line, number)| BlameLine {
            number,
            text: line.text.clone().into(),
            blame: match &line.commit {
                None => Blame::Person {
                    name: YOU.into(),
                    commit: NOT_COMMITTED.into(),
                },
                Some(commit) => Blame::Person {
                    name: line.author.clone().into(),
                    commit: commit.get(..SHORT_SHA).unwrap_or(commit).to_owned().into(),
                },
            },
        })
        .collect()
}
