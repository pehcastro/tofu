use std::borrow::Cow;

use desk_core::control::TELL_BADGE;
use desk_ui::components::card::{caption, outer_card};
use desk_ui::components::chip::mono;
use desk_ui::components::overlay::toast;
use desk_ui::components::paint::{ink, ring};
use desk_ui::components::scroll::ScrollArea;
use desk_ui::components::size::{HEADER, HEADER_PAD_LEFT, HEADER_PAD_RIGHT, SHELL_PAD, T3};
use desk_ui::metrics::TOAST_BOTTOM;
use desk_ui::theme::{ColorToken, Theme, WordToken};
use gpui::{
    AnyElement, App, ClickEvent, Context, Div, FontWeight, HighlightStyle, Rgba, SharedString,
    StyledText, div, prelude::*, px, relative, rgb_to_hsla,
};

const FONTS: [&[u8]; 6] = [
    include_bytes!("../../../../../assets/fonts/Geist-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-SemiBold.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-SemiBold.ttf"),
];

const GAP: f32 = 10.0;
const FRAME_PAD: f32 = 20.0;
const SHELL_RING: f32 = 0.06;
pub const BODY_TEXT: f32 = 0.9;
const FONT_BASE: f32 = 14.0;
pub const LINE: f32 = 23.0;
const TITLE: f32 = 19.0;
const PILL_ON: f32 = 0.12;
const PILL_OFF: f32 = 0.5;

pub struct Pills {
    pad: f32,
    radius: f32,
    fill: f32,
    font: f32,
    line: f32,
    item_x: f32,
    item_y: f32,
    item_radius: f32,
}

pub const HEADER_PILLS: Pills = Pills {
    pad: 2.0,
    radius: 9.0,
    fill: 0.05,
    font: 12.5,
    line: LINE,
    item_x: 11.0,
    item_y: 4.0,
    item_radius: 7.0,
};

pub const SHELL_PILLS: Pills = Pills {
    pad: 2.0,
    radius: 8.0,
    fill: 0.04,
    font: 12.0,
    line: 12.0,
    item_x: 10.0,
    item_y: 3.0,
    item_radius: 6.0,
};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Mark {
    Plain,
    Strong,
    Warn,
    Dim,
    Mono,
}

pub fn rich(parts: &[(&str, Mark)], theme: &Theme) -> StyledText {
    let mut text = String::new();
    let mut looks = Vec::new();
    let mut monos = Vec::new();
    for (part, mark) in parts {
        let range = text.len()..text.len() + part.len();
        text.push_str(part);
        let color = |color: Rgba| HighlightStyle {
            color: Some(rgb_to_hsla(color)),
            ..HighlightStyle::default()
        };
        match mark {
            Mark::Plain => {}
            Mark::Strong => looks.push((
                range,
                HighlightStyle {
                    font_weight: Some(FontWeight::SEMIBOLD),
                    ..HighlightStyle::default()
                },
            )),
            Mark::Warn => looks.push((range, color(theme.color(ColorToken::StatusWarn)))),
            Mark::Dim => looks.push((range, color(ink(theme, T3)))),
            Mark::Mono => monos.push((range, mono(theme))),
        }
    }
    StyledText::new(text)
        .with_highlights(looks)
        .with_font_family_overrides(monos)
}

pub fn load_fonts(cx: &App) -> Result<(), String> {
    let text = cx.text_system();
    if text.all_font_names().iter().any(|name| name == "Geist") {
        return Ok(());
    }
    text.add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("the desk cannot load Geist: {error}"))
}

pub fn title(text: &'static str) -> Div {
    div()
        .text_size(px(TITLE))
        .line_height(relative(1.0))
        .font_weight(FontWeight::SEMIBOLD)
        .child(text)
}

pub fn note(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .text_size(px(12.0))
        .text_color(ink(theme, T3))
        .child(text.into())
}

pub fn figure(text: &'static str, width: f32, theme: &Theme) -> Div {
    div()
        .min_w(px(width))
        .flex()
        .justify_end()
        .font_family(mono(theme))
        .child(text)
}

pub fn panel(title: impl Into<SharedString>, trailing: Option<AnyElement>, theme: &Theme) -> Div {
    outer_card(theme)
        .flex_col()
        .shadow(vec![ring(ink(theme, SHELL_RING))])
        .px(px(SHELL_PAD))
        .pb(px(SHELL_PAD))
        .child(
            div()
                .h(px(HEADER))
                .flex()
                .flex_none()
                .items_center()
                .gap(px(6.0))
                .pl(px(HEADER_PAD_LEFT))
                .pr(px(HEADER_PAD_RIGHT))
                .line_height(relative(1.0))
                .child(caption(title.into(), theme).flex_1())
                .children(trailing),
        )
}

pub fn pills<V: 'static, T: Copy + PartialEq + 'static>(
    id: &'static str,
    options: &[(T, &'static str)],
    chosen: T,
    look: &Pills,
    theme: &Theme,
    cx: &mut Context<V>,
    pick: fn(&mut V, T),
) -> Div {
    div()
        .flex()
        .flex_none()
        .gap(px(2.0))
        .p(px(look.pad))
        .rounded(px(look.radius))
        .bg(ink(theme, look.fill))
        .text_size(px(look.font))
        .line_height(px(look.line))
        .children(options.iter().enumerate().map(|(at, (value, label))| {
            let value = *value;
            let on = value == chosen;
            div()
                .id((id, at))
                .px(px(look.item_x))
                .py(px(look.item_y))
                .rounded(px(look.item_radius))
                .cursor_pointer()
                .when(on, |pill| {
                    pill.bg(ink(theme, PILL_ON))
                        .text_color(theme.color(ColorToken::TextStrong))
                })
                .when(!on, |pill| pill.text_color(ink(theme, PILL_OFF)))
                .on_click(cx.listener(move |view, _: &ClickEvent, _, cx| {
                    pick(view, value);
                    cx.notify();
                }))
                .child(*label)
        }))
}

pub fn told<V: 'static>(
    text: Option<&'static str>,
    theme: &Theme,
    cx: &mut Context<V>,
    dismiss: fn(&mut V),
) -> Option<Div> {
    text.map(|text| {
        div()
            .absolute()
            .left_0()
            .right_0()
            .bottom(px(TOAST_BOTTOM))
            .flex()
            .justify_center()
            .child(toast(
                SharedString::from(text),
                TELL_BADGE,
                theme,
                cx.listener(move |view, _: &ClickEvent, _, cx| {
                    dismiss(view);
                    cx.notify();
                }),
            ))
    })
}

pub fn window(theme: &Theme, body: Div) -> Div {
    div()
        .size_full()
        .min_w_0()
        .relative()
        .flex()
        .flex_col()
        .bg(theme.color(ColorToken::SurfaceWindow))
        .font_family(theme.word(WordToken::ShapeFont))
        .text_size(px(FONT_BASE))
        .line_height(px(LINE))
        .text_color(ink(theme, BODY_TEXT))
        .child(
            ScrollArea::new("screen").child(body.min_h_full().p(px(FRAME_PAD)).flex().flex_col()),
        )
}

pub fn panes() -> Div {
    div().flex().flex_wrap().gap(px(GAP))
}

pub fn fraction<E: Styled>(item: E, grow: f32, least: f32) -> E {
    item.flex_grow(grow).flex_basis(px(least)).min_w(px(least))
}

pub fn ellipsis<E: Styled>(item: E) -> E {
    item.flex_shrink_1().min_w_0().truncate()
}
