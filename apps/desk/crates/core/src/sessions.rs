use std::time::{Duration, SystemTime, UNIX_EPOCH};

pub use crate::protocol::SessionRow;

const MINUTE: u64 = 60;
const HOUR: u64 = 60 * MINUTE;
const DAY: u64 = 24 * HOUR;

impl SessionRow {
    pub fn title(&self) -> &str {
        self.name
            .as_deref()
            .filter(|name| !name.is_empty())
            .unwrap_or(&self.id)
    }

    pub fn since(&self, now: SystemTime) -> Option<Duration> {
        now.duration_since(instant(self.last_at.as_deref().unwrap_or(&self.at))?)
            .ok()
    }

    pub fn age(&self, now: SystemTime) -> Option<String> {
        let since = self.since(now)?.as_secs();
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
