use std::sync::Arc;

use gpui::{
    BoxShadow, Div, FontWeight, Image, ImageFormat, Img, Rgba, SharedString, div, img, point,
    prelude::*, px,
};

pub const SANS: &str = "Geist";
pub const MONO: &str = "Geist Mono";

pub const LIVE: Rgba = hex(0x86e0b3);
pub const WARN: Rgba = hex(0xe8c98a);
pub const LINK: Rgba = hex(0x9db8f0);
pub const AGENT: Rgba = hex(0x8b6cf0);
pub const SHELL: Rgba = tint(0x18171e, 0.92);
pub const POP: Rgba = tint(0x1e1d24, 0.96);
pub const PAPER: Rgba = hex(0xf7f6f3);
pub const INK: Rgba = hex(0x1d1c1a);

pub const T2: f32 = 0.6;
pub const T3: f32 = 0.38;

pub const CHAT: &str = r#"<path d="M3 4.5A1.5 1.5 0 0 1 4.5 3h7A1.5 1.5 0 0 1 13 4.5v5a1.5 1.5 0 0 1-1.5 1.5H7l-3 2.5V11h.5"/>"#;
pub const LEFT: &str = r#"<path d="M10 4L6 8l4 4"/>"#;
pub const RIGHT: &str = r#"<path d="M6 4l4 4-4 4"/>"#;
pub const DOWN: &str = r#"<path d="M5 6.5l3 3 3-3"/>"#;
pub const UP: &str = r#"<path d="M5 9.5l3-3 3 3"/>"#;
pub const CROSS: &str = r#"<path d="M4.5 4.5l7 7M11.5 4.5l-7 7"/>"#;

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

pub const fn black(a: f32) -> Rgba {
    Rgba::new(0.0, 0.0, 0.0, a)
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

pub fn ringed(radius: f32, color: Rgba) -> Div {
    div().relative().rounded(px(radius)).child(
        div()
            .absolute()
            .inset_0()
            .rounded(px(radius))
            .border_1()
            .border_color(color),
    )
}

pub fn shadow(blur: f32, y: f32, alpha: f32) -> Vec<BoxShadow> {
    vec![BoxShadow {
        color: black(alpha).into(),
        offset: point(px(0.0), px(y)),
        blur_radius: px(blur),
        spread_radius: px(0.0),
        inset: false,
    }]
}

pub fn ring(width: f32, color: Rgba) -> BoxShadow {
    BoxShadow {
        color: color.into(),
        offset: point(px(0.0), px(0.0)),
        blur_radius: px(0.0),
        spread_radius: px(width),
        inset: false,
    }
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

pub fn raster(svg: String, size: f32) -> Img {
    img(Arc::new(Image::from_bytes(
        ImageFormat::Svg,
        svg.into_bytes(),
    )))
    .flex_none()
    .size(px(size))
}

pub fn glyph(body: &str, size: f32, color: Rgba, scale: f32) -> Img {
    let pixels = size * scale;
    raster(
        format!(
            r#"<svg xmlns="http://www.w3.org/2000/svg" width="{pixels}" height="{pixels}" viewBox="0 0 16 16" fill="none" stroke="{}" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">{body}</svg>"#,
            css(color)
        ),
        size,
    )
}

pub fn cursor(scale: f32) -> Img {
    let pixels = 18.0 * scale;
    raster(
        format!(
            r##"<svg xmlns="http://www.w3.org/2000/svg" width="{pixels}" height="{pixels}" viewBox="0 0 16 16"><path d="M3 2l10 5.5-4.2 1.2L7 13z" fill="#8b6cf0" stroke="#fff" stroke-width="1"/></svg>"##
        ),
        18.0,
    )
}
