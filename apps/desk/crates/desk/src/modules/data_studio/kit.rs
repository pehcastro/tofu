use std::borrow::Cow;

use desk_core::control::TELL_BADGE;
use desk_ui::components::overlay::toast;
use desk_ui::components::paint::ink;
use desk_ui::theme::{Theme, WordToken};
use gpui::{
    App, BoxShadow, ClickEvent, Div, FontWeight, Rgba, SharedString, Stateful, Window, div, point,
    prelude::*, px, relative, rgb, rgba,
};

const UNDERLAY: u32 = 0x1b1a20;
const FRAME: [f32; 4] = [268.0, 60.0, 28.0, 56.0];
const TOAST_BOTTOM: f32 = 44.0;

pub const T2: f32 = 0.6;
pub const T3: f32 = 0.38;
pub const LIVE: u32 = 0x86e0b3;
pub const LINK: u32 = 0x9db8f0;
pub const WARN: u32 = 0xe8c98a;
pub const DANGER: u32 = 0xf1737d;
pub const LINE: u32 = 0x0000_0059;
pub const SHELL: u32 = 0x19181f;
pub const INNER: u32 = 0x18171e;
pub const POP: u32 = 0x1e1d24f0;

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
        .map_err(|error| format!("the data studio cannot load Geist: {error}"))
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
        .text_color(ink(theme, 0.9))
        .child(body.flex_1().min_w_0().min_h_0())
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

pub fn drop(y: f32, blur: f32, alpha: u32) -> BoxShadow {
    BoxShadow {
        color: rgba(alpha).into(),
        offset: point(px(0.0), px(y)),
        blur_radius: px(blur),
        spread_radius: px(0.0),
        inset: false,
    }
}

pub fn cap(text: &str, theme: &Theme) -> Div {
    div()
        .flex()
        .text_size(px(10.0))
        .line_height(relative(1.0))
        .font_weight(FontWeight::SEMIBOLD)
        .text_color(ink(theme, 0.45))
        .child(text.to_uppercase())
}

pub fn mono(theme: &Theme, size: f32) -> Div {
    div()
        .flex_none()
        .font_family(theme.word(WordToken::ShapeMono))
        .text_size(px(size))
}

pub fn button(id: impl Into<gpui::ElementId>, height: f32, theme: &Theme) -> Stateful<Div> {
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
        .text_size(px(12.5))
        .line_height(relative(1.0))
        .font_weight(FontWeight::MEDIUM)
}

pub fn primary(id: impl Into<gpui::ElementId>, height: f32, theme: &Theme) -> Stateful<Div> {
    button(id, height, theme)
        .bg(ink(theme, 0.9))
        .text_color(rgb(0x111111))
}

pub fn chip(label: &'static str, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .h(px(24.0))
        .px(px(9.0))
        .rounded(px(7.0))
        .bg(ink(theme, 0.07))
        .text_size(px(12.5))
        .line_height(relative(1.0))
        .font_weight(FontWeight::MEDIUM)
        .text_color(ink(theme, 0.85))
        .child(label)
}

pub fn chevron(theme: &Theme) -> Div {
    div()
        .flex_none()
        .text_size(px(10.0))
        .text_color(ink(theme, T3))
        .child("⌄")
}
