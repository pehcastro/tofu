use std::borrow::Cow;
use std::sync::Arc;

use desk_core::control::TELL_BADGE;
use desk_ui::components::overlay::toast;
use desk_ui::theme::Theme;
use gpui::{
    App, BoxShadow, ClickEvent, Div, FontWeight, Image, ImageFormat, Img, Rgba, SharedString,
    Window, div, img, linear_color_stop, linear_gradient, point, prelude::*, px, rgb,
};

pub const SANS: &str = "Geist";
pub const MONO: &str = "Geist Mono";

pub const KW: Rgba = hex(0xc9a7f5);
pub const FN: Rgba = hex(0x8fd0e8);
pub const TY: Rgba = hex(0x9fdcc3);
pub const ST: Rgba = hex(0xe6d38f);
pub const ADD: Rgba = hex(0x7fd6a6);
pub const DEL: Rgba = hex(0xee8a8f);
pub const MARK: Rgba = hex(0x52c68e);
pub const MODIFIED: Rgba = hex(0xdeb04e);
pub const UNTRACKED: Rgba = hex(0x66b0ec);
pub const DANGER: Rgba = hex(0xf1737d);
pub const CONFLICT: Rgba = hex(0xff6b6b);
pub const AGENT: Rgba = hex(0xb9a6ea);
pub const AGENT_TEXT: Rgba = hex(0xcfc2f2);
pub const AGENT_FILL: Rgba = tint(0xb5a1e8, 0.07);
pub const POP: Rgba = tint(0x1e1d24, 0.94);
pub const T2: f32 = 0.6;
pub const T3: f32 = 0.38;

pub const PLUS: &str = r#"<path d="M8 4v8M4 8h8"/>"#;
pub const COLLAPSE: &str = r#"<path d="M5 6l3-3 3 3M5 10l3 3 3-3"/>"#;
pub const SPLIT: &str =
    r#"<rect x="2.5" y="3" width="11" height="10" rx="1.5"/><path d="M8 3v10"/>"#;
pub const PREFS: &str = r#"<path d="M3 5h10M3 11h10"/><circle cx="6" cy="5" r="1.5"/><circle cx="10" cy="11" r="1.5"/>"#;
pub const ROBOT: &str =
    r#"<rect x="3" y="5" width="10" height="8" rx="2"/><path d="M8 2.5V5M6 9h.01M10 9h.01"/>"#;
pub const TRACE: &str = r#"<circle cx="8" cy="8" r="2.5"/><path d="M10.5 8v1a1.75 1.75 0 0 0 3.5 0V8a6 6 0 1 0-2.4 4.8"/>"#;
pub const CHAT: &str = r#"<path d="M3 4.5A1.5 1.5 0 0 1 4.5 3h7A1.5 1.5 0 0 1 13 4.5v5a1.5 1.5 0 0 1-1.5 1.5H7l-3 2.5V11h.5"/>"#;
pub const DOWN: &str = r#"<path d="M5 6.5l3 3 3-3"/>"#;
pub const SEND: &str = r#"<path d="M8 12.5v-9M4.5 7L8 3.5 11.5 7"/>"#;

const UNDERLAY: u32 = 0x232329;
const SHELL: u32 = 0x18171e;
const FRAME: [f32; 4] = [268.0, 60.0, 28.0, 56.0];
const TOAST_BOTTOM: f32 = 44.0;

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
    if [SANS, MONO]
        .iter()
        .all(|family| names.iter().any(|name| name == family))
    {
        return Ok(());
    }
    text.add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("the editor cannot load Geist: {error}"))
}

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

pub fn ring(color: Rgba) -> BoxShadow {
    BoxShadow {
        color: color.into(),
        offset: point(px(0.0), px(0.0)),
        blur_radius: px(0.0),
        spread_radius: px(1.0),
        inset: true,
    }
}

fn raster(bytes: Vec<u8>, size: f32) -> Img {
    img(Arc::new(Image::from_bytes(ImageFormat::Svg, bytes)))
        .flex_none()
        .size(px(size))
}

pub fn glyph(body: &str, size: f32, color: Rgba, scale: f32) -> Img {
    let pixels = size * scale;
    let channel = |value: f32| (value * 255.0).round() as u8;
    let svg = format!(
        r##"<svg xmlns="http://www.w3.org/2000/svg" width="{pixels}" height="{pixels}" viewBox="0 0 16 16" fill="none" stroke="#{:02x}{:02x}{:02x}" stroke-opacity="{}" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">{body}</svg>"##,
        channel(color.color.red),
        channel(color.color.green),
        channel(color.color.blue),
        color.alpha
    );
    raster(svg.into_bytes(), size)
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

pub fn square(size: f32, radius: f32) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(size))
        .rounded(px(radius))
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
        .shadow(vec![ring(white(0.06))])
}

pub fn inner() -> Div {
    div()
        .relative()
        .flex()
        .flex_1()
        .min_h_0()
        .overflow_hidden()
        .rounded(px(11.0))
        .bg(linear_gradient(
            180.0,
            linear_color_stop(white(0.012), 0.0),
            linear_color_stop(white(0.0), 1.0),
        ))
        .shadow(vec![BoxShadow {
            color: white(0.07).into(),
            offset: point(px(0.0), px(1.0)),
            blur_radius: px(0.0),
            spread_radius: px(0.0),
            inset: true,
        }])
}

pub fn cap(body: &str) -> Div {
    div()
        .flex()
        .text_size(px(10.0))
        .line_height(px(10.0))
        .font_weight(FontWeight::SEMIBOLD)
        .letter_spacing(px(0.7))
        .text_color(white(0.45))
        .child(body.to_uppercase())
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
        .font_family(SANS)
        .text_size(px(14.0))
        .line_height(px(23.0))
        .text_color(white(0.9))
        .child(body.flex_1().min_w_0().min_h_0().flex().gap(px(6.0)))
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
