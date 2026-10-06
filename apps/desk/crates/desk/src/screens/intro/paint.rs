use std::sync::Arc;

use gpui::{
    BoxShadow, Div, FontWeight, Image, ImageFormat, Img, Rgba, SharedString, div, img, point,
    prelude::*, px,
};

pub const BACKDROP: &[u8] = include_bytes!("../../../../../assets/intro/backdrop.jpg");
const FONTS: [&[u8]; 6] = [
    include_bytes!("../../../../../assets/fonts/Geist-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-SemiBold.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-SemiBold.ttf"),
];

pub fn load_fonts(cx: &gpui::App) -> Result<(), String> {
    cx.text_system()
        .add_fonts(
            FONTS
                .iter()
                .map(|font| std::borrow::Cow::Borrowed(*font))
                .collect(),
        )
        .map_err(|error| format!("the screen cannot load the Geist fonts: {error}"))
}

pub fn jpeg(bytes: &'static [u8]) -> Arc<Image> {
    Arc::new(Image::from_bytes(ImageFormat::Jpeg, bytes.to_vec()))
}

pub const SANS: &str = "Geist";
pub const MONO: &str = "Geist Mono";
pub const LIVE: Rgba = hex(0x86e0b3);
pub const WARN: Rgba = hex(0xe8c98a);
pub const CARET: Rgba = hex(0x9db8f0);
pub const SURFACE: u32 = 0x131218;
pub const POP: Rgba = tint(0x1e1d24, 0.94);
pub const T3: f32 = 0.38;

pub const DOWN: &str = r#"<path d="M5 6.5l3 3 3-3"/>"#;
pub const PLUS: &str = r#"<path d="M8 3v10M3 8h10"/>"#;
pub const SEND: &str = r#"<path d="M8 12.5v-9M4.5 7L8 3.5 11.5 7"/>"#;
pub const FOLDER: &str = r#"<path d="M2.5 4.5h4l1.5 1.5h5.5v6.5h-11z"/>"#;
pub const BRANCH: &str = r#"<circle cx="5" cy="4" r="1.5"/><circle cx="5" cy="12" r="1.5"/><circle cx="11" cy="6" r="1.5"/><path d="M5 5.5v5M11 7.5c0 2-6 1.5-6 3"/>"#;

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

pub fn spacer() -> Div {
    div().flex_1().min_w_0()
}

pub fn shadow(blur: f32, y: f32, alpha: f32) -> Vec<BoxShadow> {
    vec![BoxShadow {
        color: Rgba::new(0.0, 0.0, 0.0, alpha).into(),
        offset: point(px(0.0), px(y)),
        blur_radius: px(blur),
        spread_radius: px(0.0),
        inset: false,
    }]
}

pub fn ringed(radius: f32, alpha: f32) -> Div {
    div().relative().rounded(px(radius)).child(
        div()
            .absolute()
            .inset_0()
            .rounded(px(radius))
            .border_1()
            .border_color(white(alpha)),
    )
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
const ARROW: &str = r#"<path d="M3 8h9M9 5l3 3-3 3"/>"#;
const TOAST_BOTTOM: f32 = 62.0;

pub fn toast(
    message: SharedString,
    scale: f32,
    dismiss: impl Fn(&gpui::ClickEvent, &mut gpui::Window, &mut gpui::App) + 'static,
) -> Div {
    div()
        .absolute()
        .left_0()
        .right_0()
        .bottom(px(TOAST_BOTTOM))
        .flex()
        .justify_center()
        .child(
            ringed(12.0, 0.12)
                .max_w(px(620.0))
                .flex()
                .items_center()
                .gap(px(10.0))
                .pt(px(9.0))
                .pb(px(9.0))
                .pl(px(14.0))
                .pr(px(10.0))
                .bg(tint(0x1e1d24, 0.96))
                .shadow(shadow(40.0, 18.0, 0.6))
                .text_size(px(13.0))
                .line_height(px(18.0))
                .child(glyph(ARROW, 13.0, white(T3), scale))
                .child(div().flex_1().child(message))
                .child(
                    text(11.0, 15.0, white(T3), "not drawn yet")
                        .px(px(7.0))
                        .py(px(2.0))
                        .rounded_full()
                        .bg(white(0.06)),
                )
                .child(
                    div()
                        .id("dismiss")
                        .flex()
                        .items_center()
                        .justify_center()
                        .size(px(22.0))
                        .rounded(px(7.0))
                        .text_color(white(0.45))
                        .cursor_pointer()
                        .on_click(dismiss)
                        .child("×"),
                ),
        )
}
