use std::borrow::Cow;

use desk_core::control::TELL_BADGE;
use desk_ui::components::overlay::toast;
use desk_ui::components::paint::{ink, ring, tint};
use desk_ui::theme::{ColorToken, NumberToken, Theme, WordToken};
use gpui::{
    App, Bounds, ClickEvent, Div, ElementId, FontWeight, PathBuilder, Pixels, Rgba, SharedString,
    Stateful, Window, canvas, div, fill, linear_color_stop, linear_gradient, point, prelude::*, px,
    relative, rgb, size,
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

pub const BASE: f32 = 0.9;
pub const T2: f32 = 0.6;
pub const T3: f32 = 0.38;
pub const ROW_ON: f32 = 0.07;
pub const WELL: f32 = 0.25;
pub const GAP: f32 = 10.0;

const FRAME: [f32; 4] = [274.0, 66.0, 34.0, 56.0];
const BODY_TEXT: f32 = 14.0;
const BODY_LINE: f32 = 23.0;
const TITLE: f32 = 19.0;
const SHELL_PAD: f32 = 3.0;
const SHELL_RING: f32 = 0.06;
const HEAD: f32 = 28.0;
const HEAD_TEXT: f32 = 12.0;
const HEAD_INK: f32 = 0.55;
const CAP_INK: f32 = 0.45;
const CAP_TRACKING: f32 = 0.07;
const BUTTON_FILL: f32 = 0.07;
const BUTTON_TEXT: f32 = 13.0;
const BUTTON_PAD: f32 = 12.0;
const BUTTON_RADIUS: f32 = 8.0;
const TOAST_BOTTOM: f32 = 44.0;
const SPARK_STROKE: f32 = 1.3;
const SPARK_DASH: [f32; 2] = [2.0, 3.0];
const SPARK_DOT: f32 = 2.0;

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
        .map_err(|error| format!("the screen cannot load Geist: {error}"))
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
        .flex_col()
        .pl(px(left))
        .pt(px(top))
        .pr(px(right))
        .pb(px(bottom))
        .bg(rgb(UNDERLAY))
        .font_family(theme.word(WordToken::ShapeFont))
        .text_size(px(BODY_TEXT))
        .line_height(px(BODY_LINE))
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
        .child(
            div()
                .absolute()
                .top_0()
                .h(px(FADE_HEIGHT))
                .left(px(FADE_LEFT))
                .right(px(right))
                .bg(linear_gradient(
                    90.0,
                    linear_color_stop(rgb(UNDERLAY), 0.0),
                    linear_color_stop(rgb(FADE), 1.0),
                )),
        )
        .child(
            div()
                .absolute()
                .top(px(FADE_HEIGHT))
                .h(px(SHADE_TOP - FADE_HEIGHT))
                .left(px(SHADE_RIGHT_LEFT))
                .right(px(right))
                .bg(linear_gradient(
                    90.0,
                    linear_color_stop(rgb(UNDERLAY), 0.0),
                    linear_color_stop(rgb(SHADE_FLOOR), 1.0),
                )),
        )
        .child(
            div()
                .absolute()
                .top(px(SHADE_TOP))
                .h(px(SHADE_RAMP))
                .left(px(SHADE_LEFT))
                .right(px(right))
                .bg(linear_gradient(
                    180.0,
                    linear_color_stop(rgb(UNDERLAY), 0.0),
                    linear_color_stop(rgb(SHADE), 1.0),
                )),
        )
        .child(
            div()
                .absolute()
                .top(px(SHADE_TOP + SHADE_RAMP))
                .bottom_0()
                .left(px(SHADE_LEFT))
                .right(px(right))
                .bg(linear_gradient(
                    180.0,
                    linear_color_stop(rgb(SHADE), 0.0),
                    linear_color_stop(rgb(SHADE_FLOOR), 1.0),
                )),
        )
        .child(body.flex_1().min_h_0().flex().flex_col().gap(px(GAP)))
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

pub fn headline(title: &'static str) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(GAP))
        .px_1()
        .child(
            div()
                .text_size(px(TITLE))
                .line_height(px(TITLE))
                .font_weight(FontWeight::SEMIBOLD)
                .child(title),
        )
}

pub fn shell(theme: &Theme) -> Div {
    div()
        .flex()
        .flex_col()
        .min_w_0()
        .min_h_0()
        .px(px(SHELL_PAD))
        .pb(px(SHELL_PAD))
        .rounded(px(theme.number(NumberToken::CardsOuterRadius)))
        .bg(theme.color(ColorToken::CardsOuterFill))
        .shadow(vec![ring(ink(theme, SHELL_RING))])
}

pub fn shell_head(theme: &Theme) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap_1p5()
        .h(px(HEAD))
        .pl(px(9.0))
        .pr_1p5()
        .text_size(px(HEAD_TEXT))
        .line_height(relative(1.0))
        .font_weight(FontWeight::MEDIUM)
        .text_color(ink(theme, HEAD_INK))
}

pub fn cap(text: impl Into<SharedString>, font: f32, theme: &Theme) -> Div {
    div()
        .text_size(px(font))
        .line_height(relative(1.0))
        .font_weight(FontWeight::SEMIBOLD)
        .letter_spacing(px(font * CAP_TRACKING))
        .text_color(ink(theme, CAP_INK))
        .child(text.into().to_uppercase())
}

pub fn faint(text: impl Into<SharedString>, font: f32, alpha: f32, theme: &Theme) -> Div {
    div()
        .flex_none()
        .text_size(px(font))
        .text_color(ink(theme, alpha))
        .child(text.into())
}

pub fn mono(theme: &Theme) -> SharedString {
    theme.word(WordToken::ShapeMono)
}

pub fn pill(
    text: impl Into<SharedString>,
    (fill, color): (Rgba, Rgba),
    font: f32,
    pad: f32,
) -> Div {
    div()
        .flex_none()
        .px(px(pad))
        .rounded_full()
        .bg(fill)
        .text_size(px(font))
        .text_color(color)
        .child(text.into())
}

pub fn shade(theme: &Theme, alpha: f32) -> Rgba {
    tint(theme.color(ColorToken::Shadow), alpha)
}

pub fn button(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    height: f32,
    theme: &Theme,
) -> Stateful<Div> {
    div()
        .id(id)
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .h(px(height))
        .px(px(BUTTON_PAD))
        .rounded(px(BUTTON_RADIUS))
        .cursor_pointer()
        .bg(ink(theme, BUTTON_FILL))
        .text_size(px(BUTTON_TEXT))
        .line_height(relative(1.0))
        .font_weight(FontWeight::MEDIUM)
        .child(label.into())
}

pub fn primary(
    id: impl Into<ElementId>,
    label: &'static str,
    height: f32,
    theme: &Theme,
) -> Stateful<Div> {
    button(id, label, height, theme)
        .bg(theme.color(ColorToken::ButtonPrimary))
        .text_color(theme.color(ColorToken::ButtonPrimaryText))
}

pub fn metric(label: &'static str, value: Div, theme: &Theme) -> Div {
    div()
        .min_w_0()
        .child(faint(label, 11.5, T3, theme))
        .child(value)
}

pub fn figure(text: impl Into<SharedString>, font: f32, line: f32) -> Div {
    div()
        .text_size(px(font))
        .line_height(px(line))
        .font_weight(FontWeight::SEMIBOLD)
        .child(text.into())
}

pub struct Spark {
    pub values: &'static [u32],
    pub width: f32,
    pub height: f32,
    pub floor: f32,
    pub rise: f32,
    pub stroke: Rgba,
    pub area: Option<Rgba>,
    pub dashed: bool,
    pub dot: Option<Rgba>,
}

pub fn spark(line: Spark) -> impl IntoElement {
    let (width, height) = (line.width, line.height);
    canvas(
        |_, _, _| (),
        move |bounds: Bounds<Pixels>, (), window, _| paint_spark(&line, bounds, window),
    )
    .flex_none()
    .w(px(width))
    .h(px(height))
}

fn paint_spark(line: &Spark, bounds: Bounds<Pixels>, window: &mut Window) {
    let top = line
        .values
        .iter()
        .copied()
        .max()
        .filter(|top| *top > 0)
        .unwrap_or(1) as f32;
    let step = line.width / line.values.len().saturating_sub(1).max(1) as f32;
    let at = |index: usize, value: u32| {
        bounds.origin
            + point(
                px(index as f32 * step),
                px(line.floor - value as f32 / top * line.rise),
            )
    };
    if let Some(area) = line.area {
        let mut shape = PathBuilder::fill();
        shape.move_to(bounds.origin + point(px(0.0), px(line.height)));
        for (index, value) in line.values.iter().enumerate() {
            shape.line_to(at(index, *value));
        }
        shape.line_to(bounds.origin + point(px(line.width), px(line.height)));
        shape.close();
        if let Ok(path) = shape.build() {
            window.paint_path(path, area);
        }
    }
    let mut stroke = PathBuilder::stroke(px(SPARK_STROKE));
    if line.dashed {
        stroke = stroke.dash_array(&SPARK_DASH.map(px));
    }
    for (index, value) in line.values.iter().enumerate() {
        if index == 0 {
            stroke.move_to(at(index, *value));
        } else {
            stroke.line_to(at(index, *value));
        }
    }
    if let Ok(path) = stroke.build() {
        window.paint_path(path, line.stroke);
    }
    if let (Some(dot), Some((index, value))) =
        (line.dot, line.values.iter().enumerate().next_back())
    {
        let center = at(index, *value);
        let corner = center - point(px(SPARK_DOT), px(SPARK_DOT));
        window.paint_quad(
            fill(
                Bounds::new(corner, size(px(SPARK_DOT * 2.0), px(SPARK_DOT * 2.0))),
                dot,
            )
            .corner_radii(px(SPARK_DOT)),
        );
    }
}
