use gpui::{Rgba, Styled, WindowBackgroundAppearance, px};

use crate::theme::{ColorToken, NumberToken, Theme};

pub const FROST_BLUR: f32 = 24.0;

const BLUR_SPREAD: f32 = 80.0;

pub fn shell(theme: &Theme) -> (WindowBackgroundAppearance, Rgba) {
    let mut fill = theme.color(ColorToken::SurfaceWindow);
    fill.alpha = theme.number(NumberToken::GlassTint).clamp(0.0, 1.0);
    match fill.alpha < 1.0 {
        true => (WindowBackgroundAppearance::Blurred, fill),
        false => (WindowBackgroundAppearance::Opaque, fill),
    }
}

pub fn frosted<E: Styled>(element: E, theme: &Theme) -> E {
    let blur = theme.number(NumberToken::ShapeBlur);
    match blur > 0.0 {
        true => element.backdrop_blur(px(blur * blur / BLUR_SPREAD)),
        false => element,
    }
}
