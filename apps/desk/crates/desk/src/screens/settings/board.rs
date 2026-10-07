use std::borrow::Cow;

use desk_core::control::TELL_BADGE;
use desk_ui::components::card::inner_card;
use desk_ui::components::overlay::toast;
use desk_ui::components::paint::{ink, tint};
use desk_ui::metrics::TOAST_BOTTOM;
use desk_ui::theme::{Theme, WordToken};
use gpui::{
    App, ClickEvent, Div, SharedString, Window, div, hsla, linear_color_stop, linear_gradient,
    prelude::*, px, rgb,
};

const BOARD_FRAME: [f32; 4] = [268.0, 60.0, 28.0, 48.0];
const UNDERLAY_TOP: (u32, f32) = (0x29292f, 0.18);
const UNDERLAY_BOTTOM: (u32, f32) = (0x1b1c20, 0.9);
const STRIP_TOP: f32 = 844.0;
const STRIP_LEFT: (u32, f32) = (0x1f2023, 0.25);
const STRIP_RIGHT: (u32, f32) = (0x111215, 0.58);
const VIGNETTE_WIDTH: f32 = 210.0;
const VIGNETTE_DEPTH: f32 = 0.62;
const TOP_SHADE_HEIGHT: f32 = 200.0;
const TOP_SHADE_DEPTH: f32 = 0.2;
const BODY_TRACKING: f32 = -0.12;
const INNER_TINT: u32 = 0xe6e0ff;
const INNER_FILL: (f32, f32) = (0.07, 0.05);
const SEGMENT_TEXT: f32 = 12.0;
const SEGMENT_ON: f32 = 0.12;
const SEGMENT_OFF: f32 = 0.5;
const BODY_TEXT: f32 = 14.0;
const BODY_LINE: f32 = 23.0;
const BODY_INK: f32 = 0.9;
const FONTS: [&[u8]; 6] = [
    include_bytes!("../../../../../assets/fonts/Geist-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-SemiBold.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-SemiBold.ttf"),
];

pub fn load_fonts(cx: &App) -> Result<(), String> {
    let text = cx.text_system();
    if text.all_font_names().iter().any(|name| name == "Geist") {
        return Ok(());
    }
    text.add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("the screen cannot load Geist: {error}"))
}

pub fn root(
    theme: &Theme,
    body: Div,
    told: Option<SharedString>,
    on_dismiss: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> Div {
    let [left, top, right, bottom] = BOARD_FRAME;
    div()
        .size_full()
        .relative()
        .flex()
        .flex_col()
        .pl(px(left))
        .pt(px(top))
        .pr(px(right))
        .pb(px(bottom))
        .bg(linear_gradient(
            180.0,
            linear_color_stop(rgb(UNDERLAY_TOP.0), UNDERLAY_TOP.1),
            linear_color_stop(rgb(UNDERLAY_BOTTOM.0), UNDERLAY_BOTTOM.1),
        ))
        .font_family(theme.word(WordToken::ShapeFont))
        .text_size(px(BODY_TEXT))
        .line_height(px(BODY_LINE))
        .letter_spacing(px(BODY_TRACKING))
        .text_color(ink(theme, BODY_INK))
        .child(
            div()
                .absolute()
                .top_0()
                .bottom_0()
                .right_0()
                .w(px(VIGNETTE_WIDTH))
                .bg(linear_gradient(
                    90.0,
                    linear_color_stop(hsla(0.0, 0.0, 0.0, 0.0), 0.0),
                    linear_color_stop(hsla(0.0, 0.0, 0.0, VIGNETTE_DEPTH), 1.0),
                )),
        )
        .child(
            div()
                .absolute()
                .top_0()
                .left_0()
                .right_0()
                .h(px(TOP_SHADE_HEIGHT))
                .bg(linear_gradient(
                    180.0,
                    linear_color_stop(hsla(0.0, 0.0, 0.0, TOP_SHADE_DEPTH), 0.0),
                    linear_color_stop(hsla(0.0, 0.0, 0.0, 0.0), 1.0),
                )),
        )
        .child(body)
        .child(
            div()
                .absolute()
                .left_0()
                .right_0()
                .top(px(STRIP_TOP))
                .bottom_0()
                .bg(linear_gradient(
                    90.0,
                    linear_color_stop(rgb(STRIP_LEFT.0), STRIP_LEFT.1),
                    linear_color_stop(rgb(STRIP_RIGHT.0), STRIP_RIGHT.1),
                )),
        )
        .children(told.map(|message| {
            div()
                .absolute()
                .left_0()
                .right_0()
                .bottom(px(TOAST_BOTTOM))
                .flex()
                .justify_center()
                .child(toast(message, TELL_BADGE, theme, on_dismiss))
        }))
}

pub fn segment(label: &'static str, on: bool, theme: &Theme) -> gpui::Stateful<Div> {
    div()
        .id(label)
        .py(px(3.0))
        .px(px(9.0))
        .rounded(px(6.0))
        .text_size(px(SEGMENT_TEXT))
        .cursor_pointer()
        .child(label)
        .when(on, |seg| {
            seg.bg(ink(theme, SEGMENT_ON)).text_color(ink(theme, 1.0))
        })
        .when(!on, |seg| seg.text_color(ink(theme, SEGMENT_OFF)))
}

pub fn inner(theme: &Theme) -> Div {
    let tinted = rgb(INNER_TINT);
    inner_card(theme).bg(linear_gradient(
        180.0,
        linear_color_stop(tint(tinted, INNER_FILL.0), 0.0),
        linear_color_stop(tint(tinted, INNER_FILL.1), 1.0),
    ))
}
