use image::error::{ParameterError, ParameterErrorKind};
use image::{ImageError, RgbaImage};

const WEIGHT_BITS: u32 = 14;
const WIDE_BITS: u32 = 8;

struct Taps {
    start: usize,
    weights: Vec<u32>,
}

fn taps(from: u32, to: u32) -> Vec<Taps> {
    let ratio = f64::from(from) / f64::from(to);
    let spread = ratio.max(1.0);
    let one = 1u32 << WEIGHT_BITS;
    (0..to)
        .map(|out| {
            let centre = (f64::from(out) + 0.5) * ratio;
            let start = (centre - spread).floor().max(0.0) as usize;
            let end = ((centre + spread).ceil() as usize)
                .min(from as usize)
                .max(start + 1);
            let raw: Vec<f64> = (start..end)
                .map(|i| (1.0 - ((i as f64 + 0.5 - centre) / spread).abs()).max(0.0))
                .collect();
            let total: f64 = raw.iter().sum();
            let mut weights: Vec<u32> = raw
                .iter()
                .map(|w| (w / total * f64::from(one)).round() as u32)
                .collect();
            let sum: u32 = weights.iter().sum();
            if let Some(top) = weights.iter_mut().max() {
                *top = (*top + one).saturating_sub(sum);
            }
            Taps { start, weights }
        })
        .collect()
}

pub fn cover(source: &RgbaImage, width: u32, height: u32) -> Result<RgbaImage, ImageError> {
    if source.width() == 0 || source.height() == 0 {
        return Ok(RgbaImage::new(width, height));
    }
    let scale = (f64::from(width) / f64::from(source.width()))
        .max(f64::from(height) / f64::from(source.height()));
    let crop_width = ((f64::from(width) / scale).round() as u32).clamp(1, source.width());
    let crop_height = ((f64::from(height) / scale).round() as u32).clamp(1, source.height());
    let left = ((source.width() - crop_width) / 2) as usize * 4;
    let top = ((source.height() - crop_height) / 2) as usize;
    let source_stride = source.width() as usize * 4;
    let stride = width as usize * 4;
    let across = taps(crop_width, width);
    let down = taps(crop_height, height);

    let mut wide = vec![0u16; crop_height as usize * stride];
    let lines = source.as_raw().chunks_exact(source_stride).skip(top);
    for (line, row) in wide.chunks_exact_mut(stride).zip(lines) {
        let row = row.get(left..).unwrap_or_default();
        for (px, tap) in line.as_chunks_mut::<4>().0.iter_mut().zip(&across) {
            let mut sum = [0u32; 4];
            let pixels = row
                .get(tap.start * 4..)
                .unwrap_or_default()
                .as_chunks::<4>()
                .0;
            for (pixel, weight) in pixels.iter().zip(&tap.weights) {
                for (total, value) in sum.iter_mut().zip(pixel) {
                    *total += u32::from(*value) * weight;
                }
            }
            *px = sum.map(|total| {
                ((total + (1 << (WEIGHT_BITS - WIDE_BITS - 1))) >> (WEIGHT_BITS - WIDE_BITS)) as u16
            });
        }
    }

    let mut pixels = vec![0u8; height as usize * stride];
    let mut sum = vec![0u32; stride];
    for (line, tap) in pixels.chunks_exact_mut(stride).zip(&down) {
        sum.fill(0);
        let rows = wide
            .get(tap.start * stride..)
            .unwrap_or_default()
            .chunks_exact(stride);
        for (row, weight) in rows.zip(&tap.weights) {
            for (total, value) in sum.iter_mut().zip(row) {
                *total += u32::from(*value) * weight;
            }
        }
        let shift = WEIGHT_BITS + WIDE_BITS;
        for (value, total) in line.iter_mut().zip(&sum) {
            *value = ((total + (1 << (shift - 1))) >> shift).min(255) as u8;
        }
    }
    RgbaImage::from_raw(width, height, pixels).ok_or_else(|| {
        ImageError::Parameter(ParameterError::from_kind(
            ParameterErrorKind::DimensionMismatch,
        ))
    })
}
