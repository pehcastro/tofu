use std::sync::Arc;

use gpui::{
    Div, FontWeight, Image, ImageFormat, Img, Rgba, SharedString, div, img, prelude::*, px,
};

pub const SANS: &str = "Geist";
pub const MONO: &str = "Geist Mono";

pub const LIVE: Rgba = hex(0x86e0b3);
pub const WARN: Rgba = hex(0xe8c98a);
pub const ADD: Rgba = hex(0x7fd6a6);
pub const DEL: Rgba = hex(0xee8a8f);
pub const SHELL: Rgba = tint(0x18171e, 0.92);

pub const T2: f32 = 0.6;
pub const T3: f32 = 0.38;

pub const GO: &[u8] = include_bytes!("../chat/assets/go.svg");
pub const REACT: &[u8] = include_bytes!("../chat/assets/react_ts.svg");

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

pub fn spacer() -> Div {
    div().flex_1().min_w_0()
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

fn raster(bytes: Vec<u8>, size: f32) -> Img {
    img(Arc::new(Image::from_bytes(ImageFormat::Svg, bytes)))
        .flex_none()
        .size(px(size))
}

pub fn stroked(body: &str, size: f32, color: Rgba, width: f32, scale: f32) -> Img {
    let pixels = size * scale;
    let svg = format!(
        r#"<svg xmlns="http://www.w3.org/2000/svg" width="{pixels}" height="{pixels}" viewBox="0 0 16 16" fill="none" stroke="{}" stroke-width="{width}" stroke-linecap="round" stroke-linejoin="round">{body}</svg>"#,
        css(color)
    );
    raster(svg.into_bytes(), size)
}

pub fn glyph(body: &str, size: f32, color: Rgba, scale: f32) -> Img {
    stroked(body, size, color, 1.5, scale)
}

pub fn file_icon(bytes: &[u8], size: f32, scale: f32) -> Img {
    let pixels = size * scale;
    let source = String::from_utf8_lossy(bytes).replacen(
        "<svg ",
        &format!(r#"<svg width="{pixels}" height="{pixels}" "#),
        1,
    );
    raster(source.into_bytes(), size)
}
