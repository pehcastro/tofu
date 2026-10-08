use gpui::{BoxShadow, Div, FontWeight, Rgba, SharedString, div, point, prelude::*, px};

use super::fixture::Kind;

pub const SANS: &str = "Geist";

pub fn dropping(height: f32) -> Div {
    div()
        .flex()
        .flex_wrap()
        .justify_end()
        .items_center()
        .flex_shrink_1()
        .min_w_0()
        .h(px(height))
        .overflow_hidden()
        .child(div().w_0().h(px(height)))
}
pub const MONO: &str = "Geist Mono";
const BASELINE_DROP: f32 = 1.0;

pub const fn rgb(r: u8, g: u8, b: u8, a: f32) -> Rgba {
    Rgba::new(r as f32 / 255.0, g as f32 / 255.0, b as f32 / 255.0, a)
}

pub const fn ink(a: f32) -> Rgba {
    rgb(255, 255, 255, a)
}

pub const STRONG: Rgba = ink(0.9);
pub const SOFT: Rgba = ink(0.55);
pub const FAINT: Rgba = ink(0.38);
pub const CAPTION: Rgba = ink(0.45);
pub const GREEN: Rgba = rgb(134, 224, 179, 1.0);
pub const WARN: Rgba = rgb(232, 201, 138, 1.0);
pub const RED: Rgba = rgb(241, 115, 125, 1.0);
pub const DEL_INK: Rgba = rgb(238, 138, 143, 1.0);
pub const ADD_INK: Rgba = rgb(127, 214, 166, 1.0);
pub const LEAD: Rgba = rgb(185, 166, 234, 1.0);
pub const LEAD_INK: Rgba = rgb(207, 194, 242, 1.0);
pub const RING_DARK: Rgba = rgb(23, 22, 28, 1.0);
pub const SHELL: Rgba = rgb(24, 23, 30, 0.92);

pub fn kind_color(kind: &Kind) -> Rgba {
    match kind {
        Kind::Go => rgb(121, 192, 255, 1.0),
        Kind::Ts => LEAD,
        Kind::Explore => rgb(143, 208, 170, 1.0),
        Kind::Research => WARN,
        Kind::Browser => rgb(134, 212, 212, 1.0),
        Kind::Py => rgb(199, 217, 122, 1.0),
        Kind::Qa => rgb(240, 163, 181, 1.0),
    }
}

pub fn tint(color: Rgba, a: f32) -> Rgba {
    Rgba { alpha: a, ..color }
}

pub fn line(size: f32) -> f32 {
    match (size * 2.0).round() as u32 {
        16 => 10.0,
        20 => 13.0,
        21 | 22 => 14.0,
        23 => 15.0,
        24 => 16.0,
        25 | 26 => 17.0,
        28 => 18.0,
        _ => (size * 1.3).round(),
    }
}

fn fit(size: f32) -> f32 {
    let advance = match (size * 2.0).round() as u32 {
        28 => 0.966,
        26 => 0.99,
        25 | 23 => 0.98,
        24 => 0.985,
        _ => 1.0,
    };
    size * advance
}

pub fn text(content: impl Into<SharedString>, size: f32, color: Rgba) -> Div {
    div()
        .flex_none()
        .whitespace_nowrap()
        .text_size(px(fit(size)))
        .line_height(px(line(size)))
        .text_color(color)
        .child(content.into())
}

pub fn medium(content: impl Into<SharedString>, size: f32, color: Rgba) -> Div {
    text(content, size, color).font_weight(FontWeight::MEDIUM)
}

pub fn strong(content: impl Into<SharedString>, size: f32, color: Rgba) -> Div {
    text(content, size, color).font_weight(FontWeight::SEMIBOLD)
}

pub fn mono(content: impl Into<SharedString>, size: f32, color: Rgba) -> Div {
    text(content, size, color)
        .text_size(px(size))
        .font_family(MONO)
}

pub fn caption(content: &str) -> Div {
    strong(content.to_uppercase(), 10.0, CAPTION)
}

pub fn ring(color: Rgba) -> BoxShadow {
    BoxShadow {
        color: color.into(),
        offset: point(px(0.0), px(0.0)),
        blur_radius: px(0.0),
        spread_radius: px(1.0),
        inset: true,
    }
}

pub fn halo(color: Rgba, spread: f32) -> BoxShadow {
    BoxShadow {
        inset: false,
        spread_radius: px(spread),
        ..ring(color)
    }
}

pub fn words(content: &str, size: f32, leading: f32, color: Rgba) -> Div {
    div()
        .flex()
        .flex_row()
        .flex_wrap()
        .text_size(px(fit(size)))
        .line_height(px(leading))
        .pt(px(BASELINE_DROP))
        .text_color(color)
        .children(word_list(content))
}

pub fn word_list(content: &str) -> impl Iterator<Item = Div> + '_ {
    content.split_inclusive(' ').map(|word| {
        div()
            .flex_none()
            .whitespace_nowrap()
            .child(SharedString::from(word.to_owned()))
    })
}

pub fn avatar(kind: &Kind, size: f32, letter_size: f32) -> Div {
    let color = kind_color(kind);
    div()
        .flex_none()
        .size(px(size))
        .rounded_full()
        .bg(tint(color, 0.16))
        .flex()
        .items_center()
        .justify_center()
        .child(strong(initial(kind), letter_size, color))
}

fn initial(kind: &Kind) -> &'static str {
    match kind {
        Kind::Go => "G",
        Kind::Ts => "T",
        Kind::Explore => "E",
        Kind::Research => "R",
        Kind::Browser => "B",
        Kind::Py => "P",
        Kind::Qa => "Q",
    }
}
