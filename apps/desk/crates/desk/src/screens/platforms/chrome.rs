use gpui::{Div, FontWeight, div, linear_color_stop, linear_gradient, prelude::*, px};

use super::fixture::{
    ACCOUNT_INITIAL, INACTIVE, INACTIVE_SESSIONS, PALETTE_KEY, PROJECT, PROJECT_INITIAL,
    PROJECT_LINE, RUNNING, SESSION, STATUS_BRANCH, STATUS_CONTEXT, STATUS_QUOTA, STATUS_RIGHT,
    TABS,
};
use super::glyph::{Glyph, glyph_at};
use super::paint::{FAINT, GREEN, SOFT, STRONG, WARN, at, ink, medium, rgb, strong, text};
use super::{BOARD_WIDTH, Platform, SIDE_WIDTH};

const TAB_TEXT: [f32; 3] = [265.0, 355.6, 451.3];
const STATUS_TOP: f32 = 23.0;
const BAR_WIDTH: f32 = 40.0;
pub const INACTIVE_TOP: f32 = 197.0;

const WORKSPACE_TAB: (f32, f32, f32, f32) = (235.0, 6.0, 87.6, 28.0);
const WINDOWS_CONTROLS: f32 = 118.0;
const LIGHTS: [(u8, u8, u8); 3] = [(255, 95, 87), (254, 188, 46), (40, 200, 64)];
const CONTROL_GLYPHS: [Glyph; 3] = [Glyph::Minimize, Glyph::Maximize, Glyph::Close];

fn from_right(width: f32, x: f32) -> f32 {
    width - BOARD_WIDTH + x
}

pub fn left(platform: Platform, side: bool, x: f32) -> f32 {
    x + platform.left_width(side) - SIDE_WIDTH
}

pub fn right(platform: Platform, width: f32, x: f32) -> f32 {
    from_right(width, x) + WINDOWS_CONTROLS - platform.controls_width()
}

pub fn toggle_x(platform: Platform) -> f32 {
    if platform == Platform::Macos {
        84.0
    } else {
        16.0
    }
}

pub fn control_rects(platform: Platform, width: f32) -> [(f32, f32, f32, f32); 3] {
    [0.0, 1.0, 2.0].map(|index: f32| match platform {
        Platform::Windows => (from_right(width, 1288.0 + 36.0 * index), 6.0, 34.0, 28.0),
        Platform::Linux => (from_right(width, 1312.0 + 28.0 * index), 10.0, 20.0, 20.0),
        Platform::Macos => (14.0 + 20.0 * index, 14.0, 12.0, 12.0),
    })
}

fn controls(platform: Platform, width: f32) -> Vec<Div> {
    control_rects(platform, width)
        .into_iter()
        .zip(CONTROL_GLYPHS.into_iter().zip(LIGHTS))
        .flat_map(|((x, y, w, h), (shape, (r, g, b)))| match platform {
            Platform::Windows => vec![glyph_at(shape, 13.0, ink(0.62), x + 10.5, y + 7.5)],
            Platform::Linux => vec![
                at(div().size(px(w)).rounded(px(h / 2.0)).bg(ink(0.08)), x, y),
                glyph_at(shape, 11.0, ink(0.6), x + 4.5, y + 4.5),
            ],
            Platform::Macos => vec![at(
                div().size(px(w)).rounded(px(h / 2.0)).bg(rgb(r, g, b, 1.0)),
                x,
                y,
            )],
        })
        .collect()
}

pub fn title_bar(width: f32, platform: Platform, side: bool) -> Vec<Div> {
    let right = |x| right(platform, width, x);
    let left = |x| left(platform, side, x);
    let (x, y, w, h) = WORKSPACE_TAB;
    let mut out = vec![
        glyph_at(Glyph::Sidebar, 16.0, ink(0.45), toggle_x(platform), 12.0),
        glyph_at(Glyph::Chat, 13.0, ink(1.0), left(245.0), 13.5),
        glyph_at(Glyph::Pin, 11.0, FAINT, left(305.1), 14.5),
        glyph_at(Glyph::Code, 13.0, SOFT, left(335.6), 13.5),
        glyph_at(Glyph::Data, 13.0, SOFT, left(431.3), 13.5),
        glyph_at(Glyph::Search, 13.0, SOFT, right(1156.3), 13.5),
        glyph_at(Glyph::Bell, 16.0, ink(0.45), right(1226.0), 12.0),
        at(
            div().w(px(w)).h(px(h)).rounded(px(8.0)).bg(ink(0.1)),
            left(x),
            y,
        ),
        at(medium("+", 13.0, SOFT), left(518.3), 11.5),
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
            .map(|(index, (name, x))| {
                at(
                    medium(*name, 13.0, if index == 0 { ink(1.0) } else { SOFT }),
                    left(x),
                    11.5,
                )
            }),
    );
    out.extend(side.then(|| {
        at(
            text("tofu", 13.0, ink(0.75)).font_weight(FontWeight::BOLD),
            toggle_x(platform) + 32.0,
            11.5,
        )
    }));
    out.extend(controls(platform, width));
    out
}

pub fn sidebar(inactive: bool) -> Vec<Div> {
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
        glyph_at(
            if inactive { Glyph::Down } else { Glyph::Right },
            13.0,
            FAINT,
            18.0,
            206.5,
        ),
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
    for (index, (name, age)) in INACTIVE_SESSIONS.iter().enumerate().filter(|_| inactive) {
        let top = INACTIVE_TOP + 32.0 * (index as f32 + 1.0);
        out.push(at(text(*name, 13.0, FAINT), 35.0, top + 7.0));
        out.push(at(
            div()
                .w(px(222.0))
                .flex()
                .justify_end()
                .child(text(*age, 11.5, FAINT)),
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
