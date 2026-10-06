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
pub const FAIL: Rgba = hex(0xf1737d);
pub const LINK: Rgba = hex(0x9db8f0);
pub const TRACE: Rgba = hex(0xb9a6ea);
pub const SHELL: Rgba = tint(0x18171e, 0.92);
pub const T2: f32 = 0.6;
pub const T3: f32 = 0.38;

pub const TRACE_MARK: &str = r#"<circle cx="8" cy="8" r="2.5"/><path d="M10.5 8v1a1.75 1.75 0 0 0 3.5 0V8a6 6 0 1 0-2.4 4.8"/>"#;
pub const CHECK: &str = r#"<path d="M3.5 8.5l3 3 6-7"/>"#;
pub const RIGHT: &str = r#"<path d="M6 4l4 4-4 4"/>"#;
pub const DOWN: &str = r#"<path d="M4 6l4 4 4-4"/>"#;
pub const SEARCH: &str = r#"<circle cx="7" cy="7" r="4"/><path d="M10 10l3 3"/>"#;
pub const GLOBE: &str = r#"<circle cx="8" cy="8" r="5.5"/><path d="M2.5 8h11M8 2.5c2 2 2 9 0 11M8 2.5c-2 2-2 9 0 11"/>"#;
pub const ARROW: &str = r#"<path d="M3 8h9M9 5l3 3-3 3"/>"#;

pub const GO: &[u8] = include_bytes!("assets/go.svg");
pub const REACT: &[u8] = include_bytes!("assets/react_ts.svg");

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

pub fn semibold(size: f32, line: f32, color: Rgba, body: impl Into<SharedString>) -> Div {
    text(size, line, color, body).font_weight(FontWeight::SEMIBOLD)
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

pub fn glyph(body: &str, size: f32, color: Rgba, scale: f32) -> Img {
    let pixels = size * scale;
    let svg = format!(
        r#"<svg xmlns="http://www.w3.org/2000/svg" width="{pixels}" height="{pixels}" viewBox="0 0 16 16" fill="none" stroke="{}" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">{body}</svg>"#,
        css(color)
    );
    raster(svg.into_bytes(), size)
}

pub fn file_icon(path: &str, size: f32, scale: f32) -> Option<Img> {
    let bytes = if path.ends_with(".go") {
        GO
    } else if path.ends_with(".tsx") {
        REACT
    } else {
        return None;
    };
    let pixels = size * scale;
    let source = String::from_utf8_lossy(bytes).replacen(
        "<svg ",
        &format!(r#"<svg width="{pixels}" height="{pixels}" "#),
        1,
    );
    Some(raster(source.into_bytes(), size))
}

pub fn ring_arc(size: f32, scale: f32) -> Img {
    let pixels = size * scale;
    let radius = (size - 1.5) / 2.0;
    let centre = size / 2.0;
    let offset = radius * std::f32::consts::FRAC_1_SQRT_2;
    let (left, right, top) = (centre - offset, centre + offset, centre - offset);
    let svg = format!(
        r#"<svg xmlns="http://www.w3.org/2000/svg" width="{pixels}" height="{pixels}" viewBox="0 0 {size} {size}" fill="none" stroke-width="1.5"><circle cx="{centre}" cy="{centre}" r="{radius}" stroke="{}"/><path d="M{left} {top}A{radius} {radius} 0 0 1 {right} {top}" stroke="{}"/></svg>"#,
        css(tint(0x86e0b3, 0.18)),
        css(LIVE)
    );
    raster(svg.into_bytes(), size)
}
