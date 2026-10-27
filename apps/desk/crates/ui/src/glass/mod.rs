pub mod bench;

use gpui::{Filter, Refraction, Styled, px};

use crate::theme::{ChoiceToken, NumberToken, Theme};

const CIRCULAR_PROFILE: f32 = 1.0;
const BLUR_SPREAD: f32 = 80.0;

fn spread(blur: f32) -> f32 {
    blur * blur / BLUR_SPREAD
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum GlassMode {
    Frosted,
    Liquid,
}

impl GlassMode {
    pub fn label(self) -> &'static str {
        match self {
            GlassMode::Frosted => "frosted",
            GlassMode::Liquid => "liquid",
        }
    }

    pub fn other(self) -> Self {
        match self {
            GlassMode::Frosted => GlassMode::Liquid,
            GlassMode::Liquid => GlassMode::Frosted,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Glass {
    pub mode: GlassMode,
    pub frost: f32,
    pub refraction: f32,
    pub edge: f32,
    pub dispersion: f32,
}

impl Glass {
    pub fn frosted(theme: &Theme) -> Self {
        Glass {
            mode: GlassMode::Frosted,
            frost: spread(theme.number(NumberToken::ShapeBlur)),
            refraction: 0.0,
            edge: 0.0,
            dispersion: 0.0,
        }
    }

    pub fn themed(theme: &Theme) -> Self {
        match theme.choice(ChoiceToken::GlassMode) {
            "liquid" => Glass {
                mode: GlassMode::Liquid,
                refraction: theme.number(NumberToken::GlassRefraction),
                edge: theme.number(NumberToken::GlassEdge),
                dispersion: theme.number(NumberToken::GlassDispersion),
                ..Glass::frosted(theme)
            },
            _ => Glass::frosted(theme),
        }
    }

    pub fn apply<E: Styled>(self, element: E) -> E {
        let frost = (self.frost > 0.0).then(|| Filter::Blur(px(self.frost)));
        let lens = match self.mode {
            GlassMode::Frosted => None,
            GlassMode::Liquid => Some(Filter::Refract(Refraction {
                offset: px(self.refraction),
                edge: px(self.edge),
                profile: CIRCULAR_PROFILE,
                dispersion: self.dispersion,
            })),
        };
        match (frost, lens) {
            (None, None) => element,
            (frost, lens) => {
                element.backdrop_filter(frost.into_iter().chain(lens).collect::<Vec<_>>())
            }
        }
    }
}
