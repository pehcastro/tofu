use std::collections::BTreeSet;
use std::process::Command;
use std::sync::Arc;
use std::time::{Instant, SystemTime, UNIX_EPOCH};

use desk_core::control::Control;
use desk_core::model::Session;
use desk_core::protocol::{AgentState, OriginKind, TurnCompletedStatus};
use desk_ui::components::agents::ago;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::title_bar::{
    Github, GithubUser, Notice, Title, TitleBar, TitlePick, WindowKeys,
};
use gpui::{AnyElement, Context, Image, ImageFormat, SharedString};

use crate::desk::Desk;

const SECONDS_PER_MINUTE: f32 = 60.0;
#[cfg(windows)]
const CREATE_NO_WINDOW: u32 = 0x0800_0000;
const AVATAR_FETCH_SECONDS: &str = "10";
const AVATAR_PIXELS: u32 = 96;

#[derive(Clone, Copy)]
enum Raise {
    Ask,
    Failed,
    Finished,
    Cron,
}

struct Raised {
    session: String,
    raise: Raise,
    text: String,
    code: Option<String>,
    at: Instant,
}

pub struct Bell {
    since: i64,
    seen: BTreeSet<String>,
    raised: Vec<Raised>,
    unread: bool,
    fresh_from: usize,
    read_to: usize,
}

const SECONDS_PER_DAY: i64 = 86_400;
const SECONDS_PER_HOUR: i64 = 3_600;

fn unix_seconds(stamp: &str) -> Option<i64> {
    let number = |text: &str, range: std::ops::Range<usize>| text.get(range)?.parse::<i64>().ok();
    let hours_minutes = |text: &str, at: usize| {
        Some(number(text, at..at + 2)? * SECONDS_PER_HOUR + number(text, at + 3..at + 5)? * 60)
    };
    let (year, month, day) = (
        number(stamp, 0..4)?,
        number(stamp, 5..7)?,
        number(stamp, 8..10)?,
    );
    let clock = hours_minutes(stamp, 11)? + number(stamp, 17..19)?;
    let zone = stamp
        .get(19..)?
        .trim_start_matches(|c: char| c == '.' || c.is_ascii_digit());
    let east = match zone.get(..1)? {
        "Z" => 0,
        "+" => hours_minutes(zone, 1)?,
        "-" => -hours_minutes(zone, 1)?,
        _ => return None,
    };
    let shifted_year = year - i64::from(month <= 2);
    let era = shifted_year.div_euclid(400);
    let year_of_era = shifted_year - era * 400;
    let day_of_year = (153 * (month + if month > 2 { -3 } else { 9 }) + 2) / 5 + day - 1;
    let day_of_era = year_of_era * 365 + year_of_era / 4 - year_of_era / 100 + day_of_year;
    let days = era * 146_097 + day_of_era - 719_468;
    Some(days * SECONDS_PER_DAY + clock - east)
}

impl Default for Bell {
    fn default() -> Self {
        Bell {
            since: SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .map_or(0, |since| {
                    i64::try_from(since.as_secs()).unwrap_or(i64::MAX)
                }),
            seen: BTreeSet::new(),
            raised: Vec::new(),
            unread: false,
            fresh_from: 0,
            read_to: 0,
        }
    }
}

impl Bell {
    fn live(&self, stamp: Option<&str>) -> bool {
        stamp
            .and_then(unix_seconds)
            .is_some_and(|at| at >= self.since)
    }

    pub fn gather(&mut self, id: &str, session: &Session) {
        let asks = session.approvals.values().map(|(_, asked)| {
            (
                format!("ask {}", asked.approval),
                Raise::Ask,
                format!("{} wants to run", asked.tool),
                Some(asked.target.clone()),
            )
        });
        let live_turns = session
            .turns
            .iter()
            .filter(|(_, at)| self.live(Some(&at.started_at)));
        let turns = live_turns.flat_map(|(turn, at)| {
            let failed = matches!(at.status, Some(TurnCompletedStatus::Failed)).then(|| {
                (
                    format!("failed {turn}"),
                    Raise::Failed,
                    "A turn failed".to_owned(),
                    Some(at.task.clone()),
                )
            });
            let cron = matches!(at.origin.kind, OriginKind::Cron).then(|| {
                (
                    format!("cron {turn}"),
                    Raise::Cron,
                    format!("Cron {} fired", at.origin.job.clone().unwrap_or_default()),
                    None,
                )
            });
            failed.into_iter().chain(cron)
        });
        let agents = session.agents.iter().filter_map(|(instance, agent)| {
            let ended = matches!(
                agent.state,
                AgentState::Finished | AgentState::Parked | AgentState::Errored
            ) && agent.report.is_some()
                && self.live(agent.ended_at.as_deref());
            ended.then(|| {
                (
                    format!("agent {instance}"),
                    Raise::Finished,
                    format!("{} finished", agent.kind),
                    Some(agent.task.clone()),
                )
            })
        });
        let found: Vec<_> = asks.chain(turns).chain(agents).collect();
        for (key, raise, text, code) in found {
            if self.seen.insert(format!("{id} {key}")) {
                eprintln!("desk: bell: {text} in {id}");
                self.unread = true;
                self.raised.push(Raised {
                    session: id.to_owned(),
                    raise,
                    text,
                    code: code.filter(|code| !code.is_empty()),
                    at: Instant::now(),
                });
            }
        }
    }

    pub fn read(&mut self) {
        self.unread = false;
        self.fresh_from = self.read_to;
        self.read_to = self.raised.len();
    }

    pub fn session(&self, ix: usize) -> Option<&str> {
        let at = self.raised.len().checked_sub(ix.checked_add(1)?)?;
        self.raised.get(at).map(|raised| raised.session.as_str())
    }

    fn notices(&self) -> Vec<Notice> {
        self.raised
            .iter()
            .enumerate()
            .rev()
            .map(|(ix, raised)| Notice {
                glyph: match raised.raise {
                    Raise::Ask => Glyph::Lock,
                    Raise::Failed => Glyph::Trace,
                    Raise::Finished => Glyph::Agents,
                    Raise::Cron => Glyph::Cron,
                },
                text: raised.text.clone().into(),
                code: raised.code.clone().map(SharedString::from),
                detail: ago(raised.at.elapsed().as_secs_f32() / SECONDS_PER_MINUTE),
                needs_you: matches!(raised.raise, Raise::Ask),
                fresh: ix >= self.fresh_from,
            })
            .collect()
    }
}

pub struct GhAnswer {
    login: String,
    name: Option<String>,
    picture: Option<(ImageFormat, Vec<u8>)>,
}

pub fn github(answer: Option<GhAnswer>) -> Github {
    let Some(answer) = answer else {
        return Github::SignedOut;
    };
    Github::SignedIn(GithubUser {
        login: answer.login.into(),
        name: answer.name.map(Into::into),
        picture: answer
            .picture
            .map(|(format, bytes)| Arc::new(Image::from_bytes(format, bytes)).into()),
    })
}

fn hidden(program: &str) -> Command {
    let mut command = Command::new(program);
    #[cfg(windows)]
    std::os::windows::process::CommandExt::creation_flags(&mut command, CREATE_NO_WINDOW);
    command
}

fn picture_format(bytes: &[u8]) -> Option<ImageFormat> {
    match bytes {
        [0x89, b'P', b'N', b'G', ..] => Some(ImageFormat::Png),
        [0xFF, 0xD8, ..] => Some(ImageFormat::Jpeg),
        [b'G', b'I', b'F', b'8', ..] => Some(ImageFormat::Gif),
        _ => None,
    }
}

pub fn ask_github() -> Option<GhAnswer> {
    let asked = hidden("gh").args(["api", "user"]).output();
    let user = match asked {
        Ok(output) if output.status.success() => output.stdout,
        Ok(output) => {
            eprintln!(
                "desk: gh api user: {}",
                String::from_utf8_lossy(&output.stderr).trim()
            );
            return None;
        }
        Err(error) => {
            eprintln!("desk: gh api user did not run: {error}");
            return None;
        }
    };
    let user: serde_json::Value = match serde_json::from_slice(&user) {
        Ok(user) => user,
        Err(error) => {
            eprintln!("desk: gh api user answered something that is not json: {error}");
            return None;
        }
    };
    let text = |key: &str| {
        user.get(key)
            .and_then(serde_json::Value::as_str)
            .filter(|text| !text.is_empty())
            .map(str::to_owned)
    };
    let Some(login) = text("login") else {
        eprintln!("desk: gh api user has no login");
        return None;
    };
    let picture = text("avatar_url").and_then(|url| {
        let fetched = hidden("curl")
            .args(["-sfL", "--max-time", AVATAR_FETCH_SECONDS])
            .arg(format!(
                "{url}{}s={AVATAR_PIXELS}",
                if url.contains('?') { '&' } else { '?' }
            ))
            .output();
        match fetched {
            Ok(output) if output.status.success() => {
                let format = picture_format(&output.stdout);
                if format.is_none() {
                    eprintln!("desk: the avatar at {url} is not an image desk reads");
                }
                format.map(|format| (format, output.stdout))
            }
            Ok(output) => {
                eprintln!("desk: curl {url} exited {}", output.status);
                None
            }
            Err(error) => {
                eprintln!("desk: curl did not run: {error}");
                None
            }
        }
    });
    eprintln!(
        "desk: gh user {login}, picture {} bytes",
        picture.as_ref().map_or(0, |(_, bytes)| bytes.len())
    );
    Some(GhAnswer {
        login,
        name: text("name"),
        picture,
    })
}

pub fn title_bar(
    sidebar_open: bool,
    tabs_x: f32,
    github: Github,
    bell: &Bell,
    tabs: Option<AnyElement>,
    cx: &mut Context<Desk>,
) -> TitleBar {
    let title = Title {
        sidebar_open,
        palette_keys: Control::Palette.label().into(),
        notices: bell.notices(),
        unread: bell.unread,
        github,
        keys: WindowKeys::Live,
        open: None,
        tabs_x,
    };
    TitleBar::new(
        "title-bar",
        title,
        tabs,
        cx.listener(|desk, pick: &TitlePick, window, cx| match pick {
            TitlePick::Sidebar => desk.toggle_sidebar(cx),
            TitlePick::Palette => desk.open_palette(window, cx),
            TitlePick::NewWorkspace => desk.tell(Control::NewWorkspace, cx),
            TitlePick::NoticesRead => desk.read_notices(cx),
            TitlePick::Notice(ix) => desk.open_notice(*ix, cx),
        }),
    )
}
