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
        TicketStatus::ALL.into_iter().map(|status| {
            (
                status.path().to_owned(),
                Cow::Borrowed(status.svg().as_bytes()),
            )
        })
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
