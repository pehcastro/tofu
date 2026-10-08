use std::sync::Arc;

use gpui::{Div, Image, ImageFormat, Rgba, div, img, prelude::*, px};

#[derive(Clone, Copy)]
pub enum Glyph {
    Sidebar,
    Chat,
    Pin,
    Code,
    Data,
    Search,
    Bell,
    Minimize,
    Maximize,
    Close,
    Down,
    Right,
    Up,
    Plus,
    Send,
    People,
    File,
    Terminal,
    Branch,
}

impl Glyph {
    fn body(self) -> &'static str {
        match self {
            Glyph::Sidebar => {
                r#"<rect x="2" y="3" width="12" height="10" rx="2"/><path d="M6 3v10"/>"#
            }
            Glyph::Chat => {
                r#"<path d="M3 4.5A1.5 1.5 0 0 1 4.5 3h7A1.5 1.5 0 0 1 13 4.5v5a1.5 1.5 0 0 1-1.5 1.5H7l-3 2.5V11h.5"/>"#
            }
            Glyph::Pin => r#"<path d="M6 2h4l-.5 4 2 2H4.5l2-2zM8 8v6"/>"#,
            Glyph::Code => r#"<path d="M5.5 5L2.5 8l3 3M10.5 5l3 3-3 3"/>"#,
            Glyph::Data => {
                r#"<path d="M3 4c0-1.1 2.2-2 5-2s5 .9 5 2v8c0 1.1-2.2 2-5 2s-5-.9-5-2zM3 4c0 1.1 2.2 2 5 2s5-.9 5-2"/>"#
            }
            Glyph::Search => r#"<circle cx="7" cy="7" r="4"/><path d="M10 10l3 3"/>"#,
            Glyph::Bell => r#"<path d="M4 11V7a4 4 0 0 1 8 0v4l1 1.5H3zM6.5 14h3"/>"#,
            Glyph::Minimize => r#"<path d="M4 8.5h8"/>"#,
            Glyph::Maximize => r#"<rect x="4" y="4" width="8" height="8" rx="2"/>"#,
            Glyph::Close => r#"<path d="M4.5 4.5l7 7M11.5 4.5l-7 7"/>"#,
            Glyph::Down => r#"<path d="M4 6l4 4 4-4"/>"#,
            Glyph::Right => r#"<path d="M6 4l4 4-4 4"/>"#,
            Glyph::Up => r#"<path d="M5 9.5l3-3 3 3"/>"#,
            Glyph::Plus => r#"<path d="M8 3v10M3 8h10"/>"#,
            Glyph::Send => r#"<path d="M8 12.5v-9M4.5 7L8 3.5 11.5 7"/>"#,
            Glyph::People => {
                r#"<circle cx="6" cy="6" r="2"/><circle cx="11" cy="10" r="2"/><path d="M3 13c.4-1.6 1.6-2.4 3-2.4M14 15c-.4-1.6-1.6-2.4-3-2.4"/>"#
            }
            Glyph::File => r#"<path d="M5 3h4l3 3v7H5z M9 3v3h3"/>"#,
            Glyph::Terminal => r#"<path d="M3 5l3 3-3 3M8 11h5"/>"#,
            Glyph::Branch => {
                r#"<circle cx="5" cy="4" r="1.5"/><circle cx="5" cy="12" r="1.5"/><circle cx="11" cy="6" r="1.5"/><path d="M5 5.5v5M11 7.5c0 2-6 1.5-6 3"/>"#
            }
        }
    }
}

fn channel(value: f32) -> u8 {
    (value.clamp(0.0, 1.0) * 255.0).round() as u8
}

pub fn glyph(shape: Glyph, size: f32, color: Rgba) -> Div {
    let source = format!(
        r#"<svg xmlns="http://www.w3.org/2000/svg" width="{size}" height="{size}" viewBox="0 0 16 16" fill="none" stroke="rgb({},{},{})" stroke-opacity="{}" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">{}</svg>"#,
        channel(color.red),
        channel(color.green),
        channel(color.blue),
        color.alpha,
        shape.body(),
    );
    div().flex_none().size(px(size)).child(
        img(Arc::new(Image::from_bytes(
            ImageFormat::Svg,
            source.into_bytes(),
        )))
        .size(px(size)),
    )
}
