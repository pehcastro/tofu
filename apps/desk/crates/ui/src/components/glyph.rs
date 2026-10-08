use std::borrow::Cow;

use gpui::SharedString;

use crate::components::size::{RING_STROKE, SPIN_SIZES};

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
    Pencil,
    Window,
}

impl Glyph {
    const ALL: [Glyph; 13] = [
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
        Glyph::Pencil,
        Glyph::Window,
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
            Glyph::Pencil => "glyphs/pencil.svg",
            Glyph::Window => "glyphs/window.svg",
        }
    }

    fn svg(self) -> &'static str {
        match self {
            Glyph::Pin => include_str!("../../assets/icons/pin.svg"),
            Glyph::Lock => include_str!("../../assets/icons/lock.svg"),
            Glyph::Check => include_str!("../../assets/icons/checkmark.svg"),
            Glyph::Trace => include_str!("../../assets/icons/pulse.svg"),
            Glyph::Chevron => include_str!("../../assets/icons/chevron-down.svg"),
            Glyph::Attach => include_str!("../../assets/icons/attach.svg"),
            Glyph::Send => include_str!("../../assets/icons/arrow-up.svg"),
            Glyph::File => include_str!("../../assets/icons/file.svg"),
            Glyph::Terminal => include_str!("../../assets/icons/terminal.svg"),
            Glyph::Chat => include_str!("../../assets/icons/comment.svg"),
            Glyph::Agents => include_str!("../../assets/icons/person-multiple.svg"),
            Glyph::Pencil => include_str!("../../assets/icons/pencil.svg"),
            Glyph::Window => include_str!("../../assets/icons/window.svg"),
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
