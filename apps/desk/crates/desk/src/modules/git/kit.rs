use std::borrow::Cow;

use desk_core::control::TELL_BADGE;
use desk_ui::components::overlay::toast;
use desk_ui::components::paint::ink;

use super::fixture::Kind;
use desk_ui::theme::{Theme, WordToken};
use gpui::{
    App, BoxShadow, ClickEvent, Div, FontWeight, Rgba, SharedString, Window, div,
    linear_color_stop, linear_gradient, point, prelude::*, px, relative, rgb, rgba,
};

const UNDERLAY: u32 = 0x232329;
const EDGE: u32 = 0x111015;
const FADE: u32 = 0x0f0e13;
const FADE_LEFT: f32 = 1200.0;
const FADE_HEIGHT: f32 = 108.0;
const SHADE: u32 = 0x15171b;
const SHADE_FLOOR: u32 = 0x101015;
const SHADE_LEFT: f32 = 700.0;
const SHADE_RIGHT_LEFT: f32 = 1000.0;
const SHADE_TOP: f32 = 270.0;
const SHADE_RAMP: f32 = 150.0;
const FRAME: [f32; 4] = [268.0, 60.0, 28.0, 56.0];
const TOAST_BOTTOM: f32 = 44.0;
const SHELL: u32 = 0x18171e;

pub const BASE: f32 = 0.9;
pub const T2: f32 = 0.6;
pub const T3: f32 = 0.38;
pub const ADD: u32 = 0x7fd6a6;
pub const DEL: u32 = 0xee8a8f;
pub const AGENT: u32 = 0xb9a6ea;
pub const AGENT_TEXT: u32 = 0xcfc2f2;
pub const DANGER: u32 = 0xf1737d;
pub const WARN: u32 = 0xe8c98a;
pub const YOU: [u32; 2] = [0x6b7a8f, 0x3a4250];
pub const ANA: [u32; 2] = [0x8f7a6b, 0x50423a];

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
    let names = text.all_font_names();
    if ["Geist", "Geist Mono"]
        .iter()
        .all(|family| names.iter().any(|name| name == family))
    {
        return Ok(());
    }
    text.add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("the git module cannot load Geist: {error}"))
}

fn layer(top: f32, height: Option<f32>, left: f32, angle: f32, from: u32, to: u32) -> Div {
    let placed = div()
        .absolute()
        .top(px(top))
        .left(px(left))
        .right(px(FRAME[2]));
    match height {
        Some(height) => placed.h(px(height)),
        None => placed.bottom_0(),
    }
    .bg(linear_gradient(
        angle,
        linear_color_stop(rgb(from), 0.0),
        linear_color_stop(rgb(to), 1.0),
    ))
}

pub fn frame(
    theme: &Theme,
    body: Div,
    told: Option<SharedString>,
    on_dismiss: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> Div {
    let [left, top, right, bottom] = FRAME;
    div()
        .size_full()
        .relative()
        .flex()
        .pl(px(left))
        .pt(px(top))
        .pr(px(right))
        .pb(px(bottom))
        .bg(rgb(UNDERLAY))
        .font_family(theme.word(WordToken::ShapeFont))
        .text_size(px(14.0))
        .line_height(px(23.0))
        .text_color(ink(theme, BASE))
        .child(
            div()
                .absolute()
                .top_0()
                .bottom_0()
                .right_0()
                .w(px(right))
                .bg(rgb(EDGE)),
        )
        .child(layer(
            0.0,
            Some(FADE_HEIGHT),
            FADE_LEFT,
            90.0,
            UNDERLAY,
            FADE,
        ))
        .child(layer(
            FADE_HEIGHT,
            Some(SHADE_TOP - FADE_HEIGHT),
            SHADE_RIGHT_LEFT,
            90.0,
            UNDERLAY,
            SHADE_FLOOR,
        ))
        .child(layer(
            SHADE_TOP,
            Some(SHADE_RAMP),
            SHADE_LEFT,
            180.0,
            UNDERLAY,
            SHADE,
        ))
        .child(layer(
            SHADE_TOP + SHADE_RAMP,
            None,
            SHADE_LEFT,
            180.0,
            SHADE,
            SHADE_FLOOR,
        ))
        .child(body.flex_1().min_w_0().min_h_0().flex())
        .children(told.map(|message| {
            div()
                .absolute()
                .left_0()
                .right_0()
                .bottom(px(TOAST_BOTTOM))
                .flex()
                .justify_center()
                .child(toast(message, TELL_BADGE, theme, on_dismiss).w_auto())
        }))
}

pub fn edge(color: Rgba, spread: f32) -> BoxShadow {
    BoxShadow {
        color: color.into(),
        offset: point(px(0.0), px(0.0)),
        blur_radius: px(0.0),
        spread_radius: px(spread),
        inset: true,
    }
}

pub fn shell() -> Div {
    div()
        .relative()
        .flex()
        .flex_col()
        .min_w_0()
        .min_h_0()
        .px(px(3.0))
        .pb(px(3.0))
        .rounded(px(12.0))
        .bg(rgb(SHELL))
        .shadow(vec![edge(rgba(0xffffff0f), 1.0)])
}

pub fn shell_head(theme: &Theme) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(6.0))
        .h(px(28.0))
        .pl(px(9.0))
        .pr(px(6.0))
        .text_size(px(12.0))
        .line_height(relative(1.0))
        .font_weight(FontWeight::MEDIUM)
        .text_color(ink(theme, 0.55))
}

pub fn inner(theme: &Theme) -> Div {
    div()
        .relative()
        .flex()
        .flex_col()
        .flex_1()
        .min_h_0()
        .overflow_hidden()
        .rounded(px(11.0))
        .shadow(vec![BoxShadow {
            color: ink(theme, 0.07).into(),
            offset: point(px(0.0), px(1.0)),
            blur_radius: px(0.0),
            spread_radius: px(0.0),
            inset: true,
        }])
}

pub fn cap(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .flex()
        .text_size(px(10.0))
        .line_height(relative(1.0))
        .font_weight(FontWeight::SEMIBOLD)
        .letter_spacing(px(0.7))
        .text_color(ink(theme, 0.45))
        .child(text.into().to_uppercase())
}

pub fn button(
    id: impl Into<gpui::ElementId>,
    height: f32,
    font: f32,
    theme: &Theme,
) -> gpui::Stateful<Div> {
    div()
        .id(id)
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .gap(px(6.0))
        .h(px(height))
        .px(px(12.0))
        .rounded(px(8.0))
        .cursor_pointer()
        .bg(ink(theme, 0.07))
        .text_size(px(font))
        .line_height(relative(1.0))
        .font_weight(FontWeight::MEDIUM)
}

pub fn dot(color: Rgba) -> Div {
    div().flex_none().size(px(6.0)).rounded(px(3.0)).bg(color)
}

pub fn person(colors: [u32; 2], size: f32) -> Div {
    div()
        .flex_none()
        .size(px(size))
        .rounded(px(size / 2.0))
        .bg(linear_gradient(
            135.0,
            linear_color_stop(rgb(colors[0]), 0.0),
            linear_color_stop(rgb(colors[1]), 1.0),
        ))
}

pub fn count(value: u32, sign: char, color: u32, theme: &Theme) -> Div {
    div()
        .flex_none()
        .font_family(theme.word(WordToken::ShapeMono))
        .text_size(px(11.5))
        .text_color(rgb(color))
        .child(format!("{sign}{value}"))
}

pub fn icon(kind: Kind, size: f32) -> Div {
    let (label, color) = match kind {
        Kind::Go => ("GO", 0x79c7e3),
        Kind::Markdown => ("M", 0x7aa7f0),
    };
    div()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(size))
        .text_size(px(7.0))
        .line_height(relative(1.0))
        .font_weight(FontWeight::BOLD)
        .text_color(rgb(color))
        .child(label)
}
