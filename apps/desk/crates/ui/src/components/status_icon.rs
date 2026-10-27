use std::borrow::Cow;

use gpui::{App, Pixels, Rgba, Window, prelude::*, svg};

use crate::components::paint::ink;
use crate::components::size::{CAPTION_TEXT, T2};
use crate::live::ActiveTheme;
use crate::theme::{ColorToken, Theme};

macro_rules! ringed {
    ($body:literal) => {
        concat!(
            r##"<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="#000" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">"##,
            $body,
            "</svg>"
        )
    };
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum TicketStatus {
    Triage,
    Backlog,
    Todo,
    Doing,
    Review,
    Done,
    Blocked,
    NeedsDecision,
    Dropped,
}

impl TicketStatus {
    pub const ALL: [TicketStatus; 9] = [
        TicketStatus::Triage,
        TicketStatus::Backlog,
        TicketStatus::Todo,
        TicketStatus::Doing,
        TicketStatus::Review,
        TicketStatus::Done,
        TicketStatus::Blocked,
        TicketStatus::NeedsDecision,
        TicketStatus::Dropped,
    ];

    pub fn label(self) -> &'static str {
        match self {
            TicketStatus::Triage => "Triage",
            TicketStatus::Backlog => "Backlog",
            TicketStatus::Todo => "Todo",
            TicketStatus::Doing => "Doing",
            TicketStatus::Review => "Review",
            TicketStatus::Done => "Done",
            TicketStatus::Blocked => "Blocked",
            TicketStatus::NeedsDecision => "Needs decision",
            TicketStatus::Dropped => "Dropped",
        }
    }

    pub fn color(self, theme: &Theme) -> Rgba {
        match self {
            TicketStatus::Triage => theme.color(ColorToken::AnsiMagenta),
            TicketStatus::Backlog | TicketStatus::Dropped => ink(theme, CAPTION_TEXT),
            TicketStatus::Todo => ink(theme, T2),
            TicketStatus::Doing => theme.color(ColorToken::AnsiYellow),
            TicketStatus::Review => theme.color(ColorToken::GitAdded),
            TicketStatus::Done => theme.color(ColorToken::StatusAccent),
            TicketStatus::Blocked => theme.color(ColorToken::StatusDanger),
            TicketStatus::NeedsDecision => theme.color(ColorToken::StatusWarn),
        }
    }

    fn path(self) -> &'static str {
        match self {
            TicketStatus::Triage => "status/triage.svg",
            TicketStatus::Backlog => "status/backlog.svg",
            TicketStatus::Todo => "status/todo.svg",
            TicketStatus::Doing => "status/doing.svg",
            TicketStatus::Review => "status/review.svg",
            TicketStatus::Done => "status/done.svg",
            TicketStatus::Blocked => "status/blocked.svg",
            TicketStatus::NeedsDecision => "status/needs-decision.svg",
            TicketStatus::Dropped => "status/dropped.svg",
        }
    }

    fn svg(self) -> &'static str {
        match self {
            TicketStatus::Triage => ringed!(
                r##"<circle cx="8" cy="8" r="6.25"/><circle cx="8" cy="8" r="2" fill="#000" stroke="none"/>"##
            ),
            TicketStatus::Backlog => ringed!(
                r##"<circle cx="8" cy="8" r="6.25" stroke-linecap="butt" stroke-dasharray="1.6 1.205"/>"##
            ),
            TicketStatus::Todo => ringed!(r##"<circle cx="8" cy="8" r="6.25"/>"##),
            TicketStatus::Doing => ringed!(
                r##"<circle cx="8" cy="8" r="6.25"/><path d="M8 4.25A3.75 3.75 0 0 1 8 11.75z" fill="#000" stroke="none"/>"##
            ),
            TicketStatus::Review => ringed!(
                r##"<circle cx="8" cy="8" r="6.25"/><path d="M8 4.25A3.75 3.75 0 1 1 4.25 8H8z" fill="#000" stroke="none"/>"##
            ),
            TicketStatus::Done => ringed!(
                r##"<defs><mask id="tick"><rect width="16" height="16" fill="#fff" stroke="none"/><path d="M5.2 8.2 7.1 10.1 10.8 6.3" stroke-width="1.6"/></mask></defs><circle cx="8" cy="8" r="7" fill="#000" stroke="none" mask="url(#tick)"/>"##
            ),
            TicketStatus::Blocked => {
                ringed!(r##"<circle cx="8" cy="8" r="6.25"/><path d="M5.25 8h5.5"/>"##)
            }
            TicketStatus::NeedsDecision => ringed!(
                r##"<circle cx="8" cy="8" r="6.25"/><path d="M6.5 6.6a1.6 1.6 0 1 1 2.2 1.5c-.45.2-.7.55-.7 1v.3"/><circle cx="8" cy="11.1" r=".6" fill="#000" stroke="none"/>"##
            ),
            TicketStatus::Dropped => {
                ringed!(r##"<circle cx="8" cy="8" r="6.25"/><path d="m6 6 4 4m0-4-4 4"/>"##)
            }
        }
    }

    pub(crate) fn entries() -> impl Iterator<Item = (String, Cow<'static, [u8]>)> {
        let statuses = TicketStatus::ALL
            .into_iter()
            .map(|status| (status.path(), status.svg()));
        let runs = RunState::ALL
            .into_iter()
            .map(|state| (state.path(), state.svg()));
        statuses
            .chain(runs)
            .map(|(path, svg)| (path.to_owned(), Cow::Borrowed(svg.as_bytes())))
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum RunState {
    Waiting,
    Failed,
    Running,
    Done,
}

impl RunState {
    pub const ALL: [RunState; 4] = [
        RunState::Waiting,
        RunState::Failed,
        RunState::Running,
        RunState::Done,
    ];

    pub fn color(self, theme: &Theme) -> Rgba {
        theme.color(match self {
            RunState::Waiting => ColorToken::StatusWarn,
            RunState::Failed => ColorToken::StatusDanger,
            RunState::Running => ColorToken::StatusAccent,
            RunState::Done => ColorToken::StatusLive,
        })
    }

    fn path(self) -> &'static str {
        match self {
            RunState::Waiting => "run/waiting.svg",
            RunState::Failed => "run/failed.svg",
            RunState::Running => "run/running.svg",
            RunState::Done => "run/done.svg",
        }
    }

    fn svg(self) -> &'static str {
        match self {
            RunState::Waiting => ringed!(
                r##"<defs><mask id="warn"><rect width="16" height="16" fill="#fff" stroke="none"/><path d="M8 6.4v2.9" stroke-width="1.7"/><circle cx="8" cy="11.6" r=".95" fill="#000" stroke="none"/></mask></defs><path d="M8 1.6c.42 0 .8.22 1 .58l5.75 10.1c.42.74-.1 1.66-.95 1.66H2.2c-.85 0-1.37-.92-.95-1.66L7 2.18c.2-.36.58-.58 1-.58z" fill="#000" stroke="none" mask="url(#warn)"/>"##
            ),
            RunState::Failed => ringed!(
                r##"<defs><mask id="fail"><rect width="16" height="16" fill="#fff" stroke="none"/><path d="M8 4.6v4" stroke-width="1.7"/><circle cx="8" cy="11.3" r=".95" fill="#000" stroke="none"/></mask></defs><circle cx="8" cy="8" r="7" fill="#000" stroke="none" mask="url(#fail)"/>"##
            ),
            RunState::Running => ringed!(
                r##"<circle cx="8" cy="8" r="6.25" stroke-opacity=".3"/><path d="M8 1.75A6.25 6.25 0 0 1 14.25 8"/>"##
            ),
            RunState::Done => {
                ringed!(r##"<circle cx="8" cy="8" r="5" fill="#000" stroke="none"/>"##)
            }
        }
    }
}

#[derive(IntoElement)]
pub struct RunMark {
    state: RunState,
    size: Pixels,
}

pub fn run_mark(state: RunState, size: Pixels) -> impl IntoElement {
    RunMark { state, size }
}

impl RenderOnce for RunMark {
    fn render(self, _: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        svg()
            .path(self.state.path())
            .size(self.size)
            .flex_none()
            .text_color(self.state.color(&theme))
    }
}

#[derive(IntoElement)]
pub struct StatusIcon {
    status: TicketStatus,
    size: Pixels,
}

pub fn status_icon(status: TicketStatus, size: Pixels) -> impl IntoElement {
    StatusIcon { status, size }
}

impl RenderOnce for StatusIcon {
    fn render(self, _: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        svg()
            .path(self.status.path())
            .size(self.size)
            .flex_none()
            .text_color(self.status.color(&theme))
    }
}
