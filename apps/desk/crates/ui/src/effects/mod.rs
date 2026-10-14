mod cache;
mod fit;
mod passes;

use gpui::RenderImage;
use image::{Frame, ImageError, RgbaImage};

pub use cache::{EffectCache, EffectKey, Lookup, Source};
pub use fit::cover;

pub type Rgb = [u8; 3];

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum Effect {
    Duotone { shadow: Rgb, light: Rgb },
    Dither { levels: u8 },
    Halftone { cell: u8 },
    Grain { amount: u8, seed: u32 },
    Scanlines { period: u8 },
    Glow { threshold: u8, radius: u8 },
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub struct Stage {
    pub effect: Effect,
    pub strength: u8,
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct EffectParams {
    pub stages: Vec<Stage>,
}

pub fn apply(pixels: &mut [u8], width: u32, height: u32, params: &EffectParams) {
    let (width, height) = (width as usize, height as usize);
    for stage in &params.stages {
        let strength = stage.strength;
        match stage.effect {
            Effect::Duotone { shadow, light } => passes::duotone(pixels, shadow, light, strength),
            Effect::Dither { levels } => passes::dither(pixels, width, levels, strength),
            Effect::Halftone { cell } => passes::halftone(pixels, width, height, cell, strength),
            Effect::Grain { amount, seed } => passes::grain(pixels, width, amount, seed, strength),
            Effect::Scanlines { period } => passes::scanlines(pixels, width, period, strength),
            Effect::Glow { threshold, radius } => {
                passes::glow(pixels, width, height, threshold, radius, strength)
            }
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
