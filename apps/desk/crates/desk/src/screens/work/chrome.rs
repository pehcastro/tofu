use gpui::{Div, FontWeight, div, linear_color_stop, linear_gradient, prelude::*, px};

use super::BOARD_WIDTH;
use super::fixture::{
    ACCOUNT_INITIAL, INACTIVE, PALETTE_KEY, PROJECT, PROJECT_INITIAL, PROJECT_LINE, RUNNING,
    SESSION, STATUS_BRANCH, STATUS_CONTEXT, STATUS_QUOTA, STATUS_RIGHT, TABS,
};
use super::glyph::{Glyph, glyph_at};
use super::paint::{FAINT, GREEN, SOFT, STRONG, WARN, at, ink, medium, rgb, strong, text};

const TAB_TEXT: [f32; 3] = [265.0, 355.6, 451.3];
const STATUS_TOP: f32 = 23.0;
const BAR_WIDTH: f32 = 40.0;

pub const WORKSPACE_TAB: (f32, f32, f32, f32) = (235.0, 6.0, 87.6, 28.0);
pub const TAB_MENU: (f32, f32, f32, f32) = (301.6, 6.0, 21.0, 28.0);

fn from_right(width: f32, x: f32) -> f32 {
    width - BOARD_WIDTH + x
}

pub fn title_bar(width: f32) -> Vec<Div> {
    let right = |x| from_right(width, x);
    let (x, y, w, h) = WORKSPACE_TAB;
    let mut out = vec![
        glyph_at(Glyph::Sidebar, 16.0, ink(0.45), 16.0, 12.0),
        glyph_at(Glyph::Chat, 13.0, ink(1.0), 245.0, 13.5),
        glyph_at(Glyph::Pin, 11.0, FAINT, 305.1, 14.5),
        glyph_at(Glyph::Code, 13.0, SOFT, 335.6, 13.5),
        glyph_at(Glyph::Data, 13.0, SOFT, 431.3, 13.5),
        glyph_at(Glyph::Search, 13.0, SOFT, right(1156.3), 13.5),
        glyph_at(Glyph::Bell, 16.0, ink(0.45), right(1226.0), 12.0),
        glyph_at(Glyph::Minimize, 13.0, ink(0.62), right(1298.5), 13.5),
        glyph_at(Glyph::Maximize, 13.0, ink(0.62), right(1334.5), 13.5),
        glyph_at(Glyph::Close, 13.0, ink(0.62), right(1370.5), 13.5),
        at(
            text("tofu", 13.0, ink(0.75)).font_weight(FontWeight::BOLD),
            48.0,
            11.5,
        ),
        at(div().w(px(w)).h(px(h)).rounded(px(8.0)).bg(ink(0.1)), x, y),
        at(medium("+", 13.0, SOFT), 518.3, 11.5),
        at(medium(PALETTE_KEY, 13.0, FAINT), right(1177.3), 11.5),
        at(
            div().size(px(6.0)).rounded(px(3.0)).bg(WARN),
            right(1236.0),
            12.0,
        ),
        at(
            div().size(px(22.0)).rounded(px(11.0)).bg(linear_gradient(
                135.0,
                linear_color_stop(rgb(107, 122, 143, 1.0), 0.0),
                linear_color_stop(rgb(58, 66, 80, 1.0), 1.0),
            )),
            right(1254.0),
            9.0,
        ),
        at(strong(ACCOUNT_INITIAL, 10.0, STRONG), right(1261.9), 13.0),
    ];
    out.extend(
        TABS.iter()
            .zip(TAB_TEXT)
            .enumerate()
            .map(|(index, (name, left))| {
                at(
                    medium(*name, 13.0, if index == 0 { ink(1.0) } else { SOFT }),
                    left,
                    11.5,
                )
            }),
    );
    out
}

pub fn sidebar() -> Vec<Div> {
    let mut out = vec![
        at(
            div().size(px(26.0)).rounded(px(8.0)).bg(ink(0.1)),
            18.0,
            53.5,
        ),
        at(strong(PROJECT_INITIAL, 12.0, STRONG), 27.4, 58.5),
        at(strong(PROJECT, 14.0, STRONG), 54.0, 49.0),
        at(text(PROJECT_LINE, 12.0, FAINT), 54.0, 67.0),
        glyph_at(Glyph::Down, 13.0, FAINT, 217.0, 60.0),
        glyph_at(Glyph::Right, 13.0, FAINT, 18.0, 206.5),
        at(medium("Running", 11.5, FAINT), 18.0, 107.0),
        at(medium("+", 11.5, FAINT), 223.5, 107.0),
        at(text("Inactive", 13.0, FAINT), 40.0, 204.0),
        at(text(INACTIVE, 11.5, FAINT), 222.9, 205.0),
    ];
    for (index, session) in RUNNING.iter().enumerate() {
        let top = 127.0 + 32.0 * index as f32;
        if session.on {
            out.push(at(
                div()
                    .w(px(232.0))
                    .h(px(32.0))
                    .rounded(px(9.0))
                    .bg(ink(0.08)),
                8.0,
                top,
            ));
        }
        out.push(at(text("●", 8.0, GREEN), 18.0, top + 11.0));
        out.push(at(
            text(
                session.name,
                13.0,
                if session.on { STRONG } else { ink(0.6) },
            ),
            31.8,
            top + 7.0,
        ));
        out.push(at(
            div()
                .w(px(222.0))
                .flex()
                .justify_end()
                .child(text(session.state, 11.5, FAINT)),
            8.0,
            top + 8.0,
        ));
    }
    out
}

pub fn status_bar(width: f32, height: f32) -> Vec<Div> {
    let top = height - STATUS_TOP;
    let right = |x| from_right(width, x);
    let (label, used, share) = STATUS_CONTEXT;
    let (quota, window, percent) = STATUS_QUOTA;
    let mut out = vec![
        glyph_at(Glyph::Branch, 13.0, ink(0.5), 18.0, top + 1.5),
        at(medium(STATUS_BRANCH.0, 12.0, ink(0.5)), 37.0, top),
        at(medium(STATUS_BRANCH.1, 12.0, FAINT), 70.4, top),
        at(medium(SESSION, 12.0, ink(0.5)), 104.5, top),
        at(medium(label, 12.0, ink(0.5)), right(1026.1), top),
        at(
            div()
                .w(px(BAR_WIDTH))
                .h(px(5.0))
                .rounded(px(3.0))
                .bg(ink(0.08))
                .child(
                    div()
                        .h_full()
                        .w(px(BAR_WIDTH * share))
                        .rounded(px(3.0))
                        .bg(ink(0.6)),
                ),
            right(1050.3),
            top + 5.5,
        ),
        at(medium(used, 12.0, ink(0.5)), right(1096.3), top),
        at(medium(quota, 12.0, ink(0.5)), right(1137.3), top),
        at(medium(window, 12.0, FAINT), right(1206.5), top),
        at(medium(percent, 12.0, ink(0.5)), right(1227.3), top),
    ];
    out.extend(
        STATUS_RIGHT
            .iter()
            .zip([1269.9, 1349.3])
            .map(|(item, x)| at(medium(*item, 12.0, ink(0.5)), right(x), top)),
    );
    out
}
