use std::borrow::Cow;

use gpui::{App, Div, Window, div, prelude::*, px, svg};

use crate::components::paint::ink;
use crate::components::size::{CAPTION_TEXT, T2};
use crate::live::ActiveTheme;
use crate::metrics::ICON;
use crate::theme::{ColorToken, Theme};

const URGENT_PATH: &str = "priority/urgent.svg";
const URGENT_SVG: &str = r##"<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16"><defs><mask id="bang"><rect width="16" height="16" fill="#fff"/><path d="M8 4.6v4.1" stroke="#000" stroke-width="1.8" stroke-linecap="round"/><circle cx="8" cy="11.3" r="1.05" fill="#000"/></mask></defs><rect x="1.5" y="1.5" width="13" height="13" rx="3.25" fill="#000" mask="url(#bang)"/></svg>"##;
const BAR_WIDTH: f32 = 3.0;
const BAR_GAP: f32 = 1.5;
const BAR_HEIGHTS: [f32; 3] = [5.0, 8.5, 12.0];
const BAR_BOTTOM: f32 = 2.0;
const BAR_TRACK: f32 = 0.18;
const DASH_WIDTH: f32 = 2.5;
const DASH_HEIGHT: f32 = 1.5;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum Priority {
    P0,
    P1,
    P2,
    P3,
    P4,
}

impl Priority {
    pub const ALL: [Priority; 5] = [
        Priority::P0,
        Priority::P1,
        Priority::P2,
        Priority::P3,
        Priority::P4,
    ];

    pub fn label(self) -> &'static str {
        match self {
            Priority::P0 => "Urgent",
            Priority::P1 => "High",
            Priority::P2 => "Medium",
            Priority::P3 => "Low",
            Priority::P4 => "No priority",
        }
    }

    pub(crate) fn entries() -> impl Iterator<Item = (String, Cow<'static, [u8]>)> {
        std::iter::once((URGENT_PATH.to_owned(), Cow::Borrowed(URGENT_SVG.as_bytes())))
    }
}

#[derive(IntoElement)]
pub struct PriorityIcon(Priority);

pub fn priority_icon(p: Priority) -> impl IntoElement {
    PriorityIcon(p)
}

impl RenderOnce for PriorityIcon {
    fn render(self, _: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let frame = div()
            .flex()
            .flex_none()
            .size(px(ICON))
            .justify_center()
            .gap(px(BAR_GAP));
        match self.0 {
            Priority::P0 => frame.items_center().child(
                svg()
                    .path(URGENT_PATH)
                    .size(px(ICON))
                    .text_color(theme.color(ColorToken::StatusDanger)),
            ),
            Priority::P1 => bars(frame, 3, &theme),
            Priority::P2 => bars(frame, 2, &theme),
            Priority::P3 => bars(frame, 1, &theme),
            Priority::P4 => frame.items_center().children((0..3).map(|_| {
                div()
                    .w(px(DASH_WIDTH))
                    .h(px(DASH_HEIGHT))
                    .rounded_full()
                    .bg(ink(&theme, CAPTION_TEXT))
            })),
        }
    }
}

fn bars(frame: Div, lit: usize, theme: &Theme) -> Div {
    frame
        .items_end()
        .pb(px(BAR_BOTTOM))
        .children(BAR_HEIGHTS.iter().enumerate().map(|(at, height)| {
            div()
                .w(px(BAR_WIDTH))
                .h(px(*height))
                .rounded(px(BAR_WIDTH / 3.0))
                .bg(ink(theme, if at < lit { T2 } else { BAR_TRACK }))
        }))
}
