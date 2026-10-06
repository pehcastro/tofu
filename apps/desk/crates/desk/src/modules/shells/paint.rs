use std::sync::Arc;

use gpui::{
    Div, FontWeight, Image, ImageFormat, Img, Rgba, SharedString, div, img, prelude::*, px,
};

pub const SANS: &str = "Geist";
pub const MONO: &str = "Geist Mono";

pub const LIVE: Rgba = hex(0x86e0b3);
pub const WARN: Rgba = hex(0xe8c98a);
pub const TRACE: Rgba = hex(0xb9a6ea);

pub const T2: f32 = 0.6;
pub const T3: f32 = 0.38;

pub const CLOSE: &str = r#"<path d="M4.5 4.5l7 7M11.5 4.5l-7 7"/>"#;
pub const DOWN: &str = r#"<path d="M5 6.5l3 3 3-3"/>"#;
pub const RIGHT: &str = r#"<path d="M6 4l4 4-4 4"/>"#;
pub const PROMPT: &str = r#"<path d="M3 5l3 3-3 3M8 11h5"/>"#;
pub const ARROW: &str = r#"<path d="M3 8h9M9 5l3 3-3 3"/>"#;
pub const TRACE_MARK: &str = r#"<circle cx="8" cy="8" r="2.5"/><path d="M10.5 8v1a1.75 1.75 0 0 0 3.5 0V8a6 6 0 1 0-2.4 4.8"/>"#;

pub const fn hex(rgb: u32) -> Rgba {
    tint(rgb, 1.0)
}

pub const fn tint(rgb: u32, a: f32) -> Rgba {
    Rgba::new(
        ((rgb >> 16) & 0xff) as f32 / 255.0,
        ((rgb >> 8) & 0xff) as f32 / 255.0,
        (rgb & 0xff) as f32 / 255.0,
        a,
    )
}

pub const fn white(a: f32) -> Rgba {
    Rgba::new(1.0, 1.0, 1.0, a)
}

pub fn text(size: f32, line: f32, color: Rgba, body: impl Into<SharedString>) -> Div {
    div()
        .flex_none()
        .whitespace_nowrap()
        .text_size(px(size))
        .line_height(px(line))
        .text_color(color)
        .child(body.into())
}

pub fn medium(size: f32, line: f32, color: Rgba, body: impl Into<SharedString>) -> Div {
    text(size, line, color, body).font_weight(FontWeight::MEDIUM)
}

pub fn mono(size: f32, line: f32, color: Rgba, body: impl Into<SharedString>) -> Div {
    text(size, line, color, body).font_family(MONO)
}

pub fn spacer() -> Div {
    div().flex_1().min_w_0()
}

pub fn square(size: f32, radius: f32) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(size))
        .rounded(px(radius))
}

fn css(color: Rgba) -> String {
    let channel = |value: f32| (value * 255.0).round() as u8;
    format!(
        "#{:02x}{:02x}{:02x}\" stroke-opacity=\"{}",
        channel(color.color.red),
        channel(color.color.green),
        channel(color.color.blue),
        color.alpha
    )
}

pub fn glyph(body: &str, size: f32, color: Rgba, scale: f32) -> Img {
    let pixels = size * scale;
    let svg = format!(
        r#"<svg xmlns="http://www.w3.org/2000/svg" width="{pixels}" height="{pixels}" viewBox="0 0 16 16" fill="none" stroke="{}" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">{body}</svg>"#,
        css(color)
    );
    img(Arc::new(Image::from_bytes(
        ImageFormat::Svg,
        svg.into_bytes(),
    )))
    .flex_none()
    .size(px(size))
}
