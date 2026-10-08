use std::collections::BTreeSet;
use std::time::{Instant, SystemTime, UNIX_EPOCH};

use desk_core::control::Control;
use desk_core::model::Session;
use desk_core::protocol::{AgentState, OriginKind, TurnCompletedStatus};
use desk_ui::components::agents::ago;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::status_bar::Quota;
use desk_ui::components::title_bar::{Account, Notice, Title, TitleBar, TitlePick, WindowKeys};
use gpui::{AnyElement, Context, SharedString};

use crate::desk::Desk;

const SECONDS_PER_MINUTE: f32 = 60.0;

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
    since: String,
    seen: BTreeSet<String>,
    raised: Vec<Raised>,
    unread: bool,
}

const SECONDS_PER_DAY: u64 = 86_400;
const STAMP_LENGTH: usize = 19;

fn utc_stamp(now: SystemTime) -> String {
    let seconds = now
        .duration_since(UNIX_EPOCH)
        .map_or(0, |since| since.as_secs());
    let (days, clock) = (seconds / SECONDS_PER_DAY, seconds % SECONDS_PER_DAY);
    let shifted = i64::try_from(days).unwrap_or(0) + 719_468;
    let era = shifted.div_euclid(146_097);
    let day_of_era = shifted.rem_euclid(146_097);
    let year_of_era =
        (day_of_era - day_of_era / 1460 + day_of_era / 36_524 - day_of_era / 146_096) / 365;
    let day_of_year = day_of_era - (365 * year_of_era + year_of_era / 4 - year_of_era / 100);
    let month_index = (5 * day_of_year + 2) / 153;
    let day = day_of_year - (153 * month_index + 2) / 5 + 1;
    let month = if month_index < 10 {
        month_index + 3
    } else {
        month_index - 9
    };
    let year = year_of_era + era * 400 + i64::from(month <= 2);
    format!(
        "{year:04}-{month:02}-{day:02}T{:02}:{:02}:{:02}",
        clock / 3600,
        clock / 60 % 60,
        clock % 60
    )
}

impl Default for Bell {
    fn default() -> Self {
        Bell {
            since: utc_stamp(SystemTime::now()),
            seen: BTreeSet::new(),
            raised: Vec::new(),
            unread: false,
        }
    }
}

impl Bell {
    fn live(&self, stamp: Option<&str>) -> bool {
        stamp
            .and_then(|stamp| stamp.get(..STAMP_LENGTH))
            .is_some_and(|stamp| stamp >= self.since.as_str())
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
    }

    pub fn session(&self, ix: usize) -> Option<&str> {
        let at = self.raised.len().checked_sub(ix.checked_add(1)?)?;
        self.raised.get(at).map(|raised| raised.session.as_str())
    }

    fn notices(&self) -> Vec<Notice> {
        self.raised
            .iter()
            .rev()
            .map(|raised| Notice {
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
            })
            .collect()
    }
}

pub fn title_bar(
    sidebar_open: bool,
    tabs_x: f32,
    quota: Option<&Quota>,
    bell: &Bell,
    version: Option<SharedString>,
    tabs: Option<AnyElement>,
    cx: &mut Context<Desk>,
) -> TitleBar {
    let title = Title {
        sidebar_open,
        palette_keys: Control::Palette.label().into(),
        notices: bell.notices(),
        unread: bell.unread,
        letter: quota.and_then(|quota| {
            let first = quota.account.chars().find(|c| c.is_alphanumeric())?;
            Some(first.to_lowercase().collect::<String>().into())
        }),
        account: quota.map(|quota| Account {
            name: quota.account.clone(),
            found: format!("{} window at {}%", quota.window, quota.percent).into(),
        }),
        version,
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
