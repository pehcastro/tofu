use std::borrow::Cow;

use gpui::SharedString;

use crate::components::size::{RING_STROKE, SPIN_SIZES};

macro_rules! stroked {
    ($width:literal, $body:literal) => {
        concat!(
            r##"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="#000" stroke-width=""##,
            $width,
            r##"" stroke-linecap="round" stroke-linejoin="round">"##,
            $body,
            "</svg>"
        )
    };
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Glyph {
    Pin,
    Lock,
    Check,
    Trace,
    Chevron,
    Attach,
    Send,
    File,
    Terminal,
    Chat,
    Agents,
}

impl Glyph {
    const ALL: [Glyph; 11] = [
        Glyph::Pin,
        Glyph::Lock,
        Glyph::Check,
        Glyph::Trace,
        Glyph::Chevron,
        Glyph::Attach,
        Glyph::Send,
        Glyph::File,
        Glyph::Terminal,
        Glyph::Chat,
        Glyph::Agents,
    ];

    pub fn path(self) -> &'static str {
        match self {
            Glyph::Pin => "glyphs/pin.svg",
            Glyph::Lock => "glyphs/lock.svg",
            Glyph::Check => "glyphs/check.svg",
            Glyph::Trace => "glyphs/trace.svg",
            Glyph::Chevron => "glyphs/chevron.svg",
            Glyph::Attach => "glyphs/attach.svg",
            Glyph::Send => "glyphs/send.svg",
            Glyph::File => "glyphs/file.svg",
            Glyph::Terminal => "glyphs/terminal.svg",
            Glyph::Chat => "glyphs/chat.svg",
            Glyph::Agents => "glyphs/agents.svg",
        }
    }

    fn svg(self) -> &'static str {
        match self {
            Glyph::Pin => stroked!("1.5", r#"<path d="M6 2h4l-.5 4 2 2H4.5l2-2zM8 8v6"/>"#),
            Glyph::Lock => stroked!(
                "1.5",
                r#"<rect x="3.5" y="7" width="9" height="6.5" rx="1.5"/><path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2"/>"#
            ),
            Glyph::Check => stroked!("2.6", r#"<path d="M3.5 8.5l3 3 6-7"/>"#),
            Glyph::Trace => stroked!(
                "1.5",
                r#"<circle cx="8" cy="8" r="2.5"/><path d="M10.5 8v1a2 2 0 0 0 4 0V8a6.5 6.5 0 1 0-2.6 5.2"/>"#
            ),
            Glyph::Chevron => stroked!("1.5", r#"<path d="M4.5 6.5L8 10l3.5-3.5"/>"#),
            Glyph::Attach => stroked!(
                "1.5",
                r#"<path d="M12.5 7.5l-4.6 4.6a3 3 0 0 1-4.2-4.2l5-5a2 2 0 0 1 2.8 2.8l-5 5a1 1 0 0 1-1.4-1.4l4.6-4.6"/>"#
            ),
            Glyph::Send => stroked!("1.75", r#"<path d="M8 13V3.5M4 7.5l4-4 4 4"/>"#),
            Glyph::File => stroked!("1.5", r#"<path d="M4 2h5l3 3v9H4zM9 2v3h3"/>"#),
            Glyph::Terminal => stroked!(
                "1.5",
                r#"<rect x="2" y="3" width="12" height="10" rx="2"/><path d="M5 7l2 1.5L5 10M8.5 10.5H11"/>"#
            ),
            Glyph::Chat => stroked!(
                "1.5",
                r#"<path d="M3 4.5A1.5 1.5 0 0 1 4.5 3h7A1.5 1.5 0 0 1 13 4.5v5a1.5 1.5 0 0 1-1.5 1.5H7l-3 2.5V11h.5A1.5 1.5 0 0 1 3 9.5z"/>"#
            ),
            Glyph::Agents => stroked!(
                "1.5",
                r#"<circle cx="6" cy="6" r="2.25"/><circle cx="11.5" cy="7" r="1.75"/><path d="M2 13c.5-2.2 2-3.5 4-3.5s3.5 1.3 4 3.5M10.5 10.2c1.6 0 2.9 1 3.3 2.8"/>"#
            ),
        }
    }

    pub fn entries() -> Vec<(String, Cow<'static, [u8]>)> {
        let glyphs = Glyph::ALL.iter().map(|glyph| {
            (
                glyph.path().to_owned(),
                Cow::Borrowed(glyph.svg().as_bytes()),
            )
        });
        let marks = SPIN_SIZES.map(|size| {
            let size = f32::from(size);
            (
                spinner_path(size).to_string(),
                Cow::Owned(spinner_svg(size).into_bytes()),
            )
        });
        glyphs.chain(marks).collect()
    }
}

pub const MARK_BLEED: f32 = 1.0;

pub fn spinner_path(size: f32) -> SharedString {
    let size = size
        .round()
        .clamp(f32::from(*SPIN_SIZES.start()), f32::from(*SPIN_SIZES.end()));
    format!("glyphs/arc-{size}.svg").into()
}

fn spinner_svg(size: f32) -> String {
    let radius = (size - RING_STROKE) / 2.0;
    let center = size / 2.0 + MARK_BLEED;
    let canvas = size + 2.0 * MARK_BLEED;
    let reach = radius * std::f32::consts::FRAC_1_SQRT_2;
    let (left, right, top) = (center - reach, center + reach, center - reach);
    format!(
        r##"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {canvas} {canvas}" fill="none" stroke="#000" stroke-width="{RING_STROKE}"><path d="M{left} {top}A{radius} {radius} 0 0 1 {right} {top}"/></svg>"##
    )
}
