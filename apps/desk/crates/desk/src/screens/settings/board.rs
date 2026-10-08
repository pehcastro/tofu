use std::borrow::Cow;

use desk_core::control::TELL_BADGE;
use desk_ui::components::card::inner_card;
use desk_ui::components::overlay::toast;
use desk_ui::components::paint::{ink, tint};
use desk_ui::metrics::TOAST_BOTTOM;
use desk_ui::theme::{Theme, WordToken};
use gpui::{
    App, ClickEvent, Div, SharedString, Window, div, linear_color_stop, linear_gradient,
    prelude::*, px, rgb,
};

const FRAME_PAD: f32 = 20.0;
const UNDERLAY_TOP: (u32, f32) = (0x29292f, 0.18);
const UNDERLAY_BOTTOM: (u32, f32) = (0x1b1c20, 0.9);
const BODY_TRACKING: f32 = -0.12;
const INNER_TINT: u32 = 0xe6e0ff;
const INNER_FILL: (f32, f32) = (0.07, 0.05);
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
    div()
        .size_full()
        .min_w_0()
        .relative()
        .flex()
        .flex_col()
        .p(px(FRAME_PAD))
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
        .child(body)
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

pub fn inner(theme: &Theme) -> Div {
    let tinted = rgb(INNER_TINT);
    inner_card(theme).bg(linear_gradient(
        180.0,
        linear_color_stop(tint(tinted, INNER_FILL.0), 0.0),
        linear_color_stop(tint(tinted, INNER_FILL.1), 1.0),
    ))
}
