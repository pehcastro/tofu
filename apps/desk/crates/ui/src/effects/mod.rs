pub mod ascii;
mod cache;
pub mod controls;
mod fit;
mod passes;

use gpui::RenderImage;
use image::{Frame, ImageError, RgbaImage};

pub use cache::{EffectCache, EffectKey, Lookup, Source};
pub use fit::cover;

pub type Rgb = [u8; 3];

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum Effect {
    Duotone {
        shadow: Rgb,
        light: Rgb,
    },
    Dither {
        levels: u8,
        cell: u8,
        anchor: u8,
    },
    Fade {
        color: Rgb,
        anchor: u8,
    },
    Halftone {
        cell: u8,
        anchor: u8,
    },
    Grain {
        amount: u8,
        size: u8,
        seed: u32,
    },
    Scanlines {
        period: u8,
    },
    Glow {
        threshold: u8,
        radius: u8,
        color: Rgb,
        anchor: u8,
    },
    Crt {
        curve: u8,
        bloom: u8,
    },
    PixelSort {
        threshold: u8,
    },
}

impl Effect {
    fn reach(&self) -> u8 {
        match *self {
            Effect::Dither { anchor, .. } | Effect::Halftone { anchor, .. } => anchor,
            Effect::Duotone { .. }
            | Effect::Glow { .. }
            | Effect::Fade { .. }
            | Effect::Grain { .. }
            | Effect::Scanlines { .. }
            | Effect::Crt { .. }
            | Effect::PixelSort { .. } => 0,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub struct Stage {
    pub effect: Effect,
    pub strength: u8,
    pub opacity: u8,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct EffectParams {
    pub stages: Vec<Stage>,
}

pub fn apply(pixels: &mut [u8], width: u32, height: u32, params: &EffectParams) {
    let (width, height) = (width as usize, height as usize);
    for stage in &params.stages {
        let strength = stage.strength;
        let reach = stage.effect.reach();
        let before = (stage.opacity < u8::MAX || reach > 0).then(|| pixels.to_vec());
        match stage.effect {
            Effect::Duotone { shadow, light } => passes::duotone(pixels, shadow, light, strength),
            Effect::Dither { levels, cell, .. } => {
                passes::dither(pixels, width, levels, cell, strength)
            }
            Effect::Fade { color, anchor } => {
                passes::fade(pixels, width, height, color, anchor, strength)
            }
            Effect::Halftone { cell, .. } => {
                passes::halftone(pixels, width, height, cell, strength)
            }
            Effect::Grain { amount, size, seed } => {
                passes::grain(pixels, width, amount, size, seed, strength)
            }
            Effect::Scanlines { period } => passes::scanlines(pixels, width, period, strength),
            Effect::Glow {
                threshold,
                radius,
                color,
                anchor,
            } => {
                let rise = passes::Rise {
                    threshold,
                    radius,
                    color,
                    anchor,
                };
                passes::rising_glow(pixels, width, height, rise, strength)
            }
            Effect::Crt { curve, bloom } => {
                passes::crt(pixels, width, height, curve, bloom, strength)
            }
            Effect::PixelSort { threshold } => {
                passes::pixel_sort(pixels, width, threshold, strength)
            }
        }
        if let Some(before) = before {
            passes::reveal(pixels, &before, width, height, reach, stage.opacity);
        }
    }
}

pub fn render(
    bytes: &[u8],
    width: u32,
    height: u32,
    params: &EffectParams,
) -> Result<RgbaImage, ImageError> {
    let source = image::load_from_memory(bytes)?.into_rgba8();
    let mut image = cover(&source, width, height)?;
    apply(&mut image, width, height, params);
    Ok(image)
}

pub fn to_render_image(mut image: RgbaImage) -> RenderImage {
    for px in image.pixels_mut() {
        px.0.swap(0, 2);
    }
    RenderImage::new([Frame::new(image)])
}
