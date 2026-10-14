use std::array::from_fn;
use std::f32::consts::SQRT_2;

use super::Rgb;

const BAYER: [[u8; 4]; 4] = [[0, 8, 2, 10], [12, 4, 14, 6], [3, 11, 1, 9], [15, 7, 13, 5]];
const GLOW_STEP: usize = 4;
const GLOW_PHASES: [(bool, u32); GLOW_STEP] = [(false, 160), (false, 224), (true, 32), (true, 96)];
const BLUR_PASSES: usize = 2;
const ONE: u32 = 256;

type Glow = [f32; 3];
type Light = [u32; 3];

fn weight(strength: u8) -> u32 {
    u32::from(strength) + u32::from(strength >> 7)
}

fn blend(from: u8, to: u8, weight: u32) -> u8 {
    ((u32::from(from) * (ONE - weight) + u32::from(to) * weight + ONE / 2) >> 8) as u8
}

fn luma(px: &[u8; 4]) -> u8 {
    ((54 * u32::from(px[0]) + 183 * u32::from(px[1]) + 19 * u32::from(px[2]) + ONE / 2) >> 8) as u8
}

fn rows(pixels: &mut [u8], width: usize) -> impl Iterator<Item = (usize, &mut [[u8; 4]])> {
    pixels
        .chunks_exact_mut(width.saturating_mul(4).max(4))
        .map(|row| row.as_chunks_mut::<4>().0)
        .enumerate()
}

pub fn duotone(pixels: &mut [u8], shadow: Rgb, light: Rgb, strength: u8) {
    let weight = weight(strength);
    let gradient: [[u8; 3]; 256] = from_fn(|level| {
        from_fn(|c| {
            let (from, to) = (f32::from(shadow[c]), f32::from(light[c]));
            (from + (to - from) * level as f32 / 255.0).round() as u8
        })
    });
    for px in pixels.as_chunks_mut::<4>().0 {
        let target = gradient[usize::from(luma(px))];
        for (value, to) in px.iter_mut().zip(target) {
            *value = blend(*value, to, weight);
        }
    }
}

pub fn dither(pixels: &mut [u8], width: usize, levels: u8, strength: u8) {
    let steps = f32::from(levels.max(2) - 1);
    let weight = weight(strength);
    let tables: [[[u8; 256]; 4]; 4] = from_fn(|y| {
        from_fn(|x| {
            let offset = (f32::from(BAYER[y][x]) + 0.5) / 16.0 - 0.5;
            from_fn(|v| {
                let level = (v as f32 / 255.0 * steps + offset)
                    .round()
                    .clamp(0.0, steps);
                blend(v as u8, (level / steps * 255.0).round() as u8, weight)
            })
        })
    });
    for (y, row) in rows(pixels, width) {
        let band = &tables[y & 3];
        for (x, px) in row.iter_mut().enumerate() {
            let table = &band[x & 3];
            for value in px.iter_mut().take(3) {
                *value = table[usize::from(*value)];
            }
        }
    }
}

pub fn halftone(pixels: &mut [u8], width: usize, height: usize, cell: u8, strength: u8) {
    if width == 0 || height == 0 {
        return;
    }
    let cell = usize::from(cell.max(2));
    let columns = width.div_ceil(cell);
    let weight = weight(strength) as i32;
    let half = cell as f32 / 2.0;
    let reach: [i32; 256] =
        from_fn(|level| (half * SQRT_2 * (level as f32 / 255.0).sqrt() * 256.0).round() as i32);
    let distances: Vec<i32> = (0..cell * cell)
        .map(|i| {
            let dx = (i % cell) as f32 + 0.5 - half;
            let dy = (i / cell) as f32 + 0.5 - half;
            ((dx * dx + dy * dy).sqrt() * 256.0).round() as i32
        })
        .collect();
    let radii: Vec<i32> = (0..height.div_ceil(cell) * columns)
        .map(|i| {
            let x = ((i % columns) * cell + cell / 2).min(width - 1);
            let y = ((i / columns) * cell + cell / 2).min(height - 1);
            pixels
                .get((y * width + x) * 4..)
                .and_then(<[u8]>::first_chunk::<4>)
                .map_or(0, |px| reach[usize::from(luma(px))])
        })
        .collect();
    for (y, row) in rows(pixels, width) {
        let band = radii.get(y / cell * columns..).unwrap_or_default();
        let offsets = &distances[(y % cell) * cell..][..cell];
        for (segment, radius) in row.chunks_mut(cell).zip(band) {
            for (px, distance) in segment.iter_mut().zip(offsets) {
                let cover = (radius - distance + 128).clamp(0, 256);
                let factor = 256 - (((256 - cover) * weight + 128) >> 8);
                for value in px.iter_mut().take(3) {
                    *value = ((i32::from(*value) * factor + 128) >> 8) as u8;
                }
            }
        }
    }
}

pub fn grain(pixels: &mut [u8], width: usize, amount: u8, seed: u32, strength: u8) {
    let weight = weight(strength);
    let amount = i64::from(amount);
    for (y, row) in rows(pixels, width) {
        for (x, px) in row.iter_mut().enumerate() {
            let shift =
                ((noise(x as u64, y as u64, u64::from(seed)) * amount + (1 << 22)) >> 23) as i32;
            for value in px.iter_mut().take(3) {
                let target = (i32::from(*value) + shift).clamp(0, 255) as u8;
                *value = blend(*value, target, weight);
            }
        }
    }
}

fn noise(x: u64, y: u64, seed: u64) -> i64 {
    let mut hash = x.wrapping_mul(0x9e37_79b9_7f4a_7c15)
        ^ y.wrapping_mul(0xc2b2_ae3d_27d4_eb4f)
        ^ seed.wrapping_mul(0x1656_67b1_9e37_79f9);
    hash ^= hash >> 33;
    hash = hash.wrapping_mul(0xff51_afd7_ed55_8ccd);
    hash ^= hash >> 33;
    (hash >> 40) as i64 - (1 << 23)
}

pub fn scanlines(pixels: &mut [u8], width: usize, period: u8, strength: u8) {
    let weight = weight(strength);
    let darken: [u8; 256] = from_fn(|v| blend(v as u8, 0, weight));
    for (_, row) in rows(pixels, width).step_by(usize::from(period.max(2))) {
        for px in row {
            for value in px.iter_mut().take(3) {
                *value = darken[usize::from(*value)];
            }
        }
    }
}

pub fn glow(
    pixels: &mut [u8],
    width: usize,
    height: usize,
    threshold: u8,
    radius: u8,
    strength: u8,
) {
    let columns = width.div_ceil(GLOW_STEP);
    let lines = height.div_ceil(GLOW_STEP);
    if columns == 0 || lines == 0 {
        return;
    }
    let cut = threshold.min(254);
    let mut sums = vec![[0u32; 3]; columns * lines];
    for (y, row) in rows(pixels, width) {
        let Some(band) = sums.get_mut(y / GLOW_STEP * columns..) else {
            break;
        };
        for (block, sum) in row.chunks(GLOW_STEP).zip(band) {
            for px in block {
                for (total, value) in sum.iter_mut().zip(px) {
                    *total += u32::from(value.saturating_sub(cut));
                }
            }
        }
    }
    let gain = 255.0 / (f32::from(255 - cut) * (GLOW_STEP * GLOW_STEP) as f32);
    let mut small: Vec<Glow> = sums
        .iter()
        .map(|sum| sum.map(|total| total as f32 * gain))
        .collect();
    let radius = usize::from(radius);
    let mut scratch = small.clone();
    for _ in 0..BLUR_PASSES {
        for line in 0..lines {
            box_line(&small, &mut scratch, line * columns, 1, columns, radius);
        }
        for column in 0..columns {
            box_line(&scratch, &mut small, column, columns, lines, radius);
        }
    }
    let scale = weight(strength) as f32;
    let fixed: Vec<Light> = small
        .iter()
        .map(|glow| glow.map(|v| (v * scale).round() as u32))
        .collect();
    let mut line = vec![[0; 3]; columns + 2];
    for (y, row) in rows(pixels, width) {
        let (near, far, toward) = phase(y, lines);
        let top = &fixed[near * columns..][..columns];
        let bottom = &fixed[far * columns..][..columns];
        for ((slot, a), b) in line[1..=columns].iter_mut().zip(top).zip(bottom) {
            *slot = mix(*a, *b, toward);
        }
        line[0] = line[1];
        line[columns + 1] = line[columns];
        for (group, window) in row.chunks_mut(GLOW_STEP).zip(line.windows(3)) {
            let &[before, at, after] = window else {
                continue;
            };
            for (px, (forward, toward)) in group.iter_mut().zip(GLOW_PHASES) {
                let light = if forward {
                    mix(at, after, toward)
                } else {
                    mix(before, at, toward)
                };
                for (value, add) in px.iter_mut().zip(light) {
                    *value = (u32::from(*value) + ((add + ONE / 2) >> 8)).min(255) as u8;
                }
            }
        }
    }
}

fn phase(index: usize, count: usize) -> (usize, usize, u32) {
    let (cell, (forward, toward)) = (index / GLOW_STEP, GLOW_PHASES[index % GLOW_STEP]);
    let last = count - 1;
    if forward {
        (cell.min(last), (cell + 1).min(last), toward)
    } else {
        (cell.saturating_sub(1).min(last), cell.min(last), toward)
    }
}

fn mix(from: Light, to: Light, toward: u32) -> Light {
    from_fn(|c| (from[c] * (ONE - toward) + to[c] * toward + ONE / 2) >> 8)
}

fn box_line(
    from: &[Glow],
    to: &mut [Glow],
    start: usize,
    stride: usize,
    count: usize,
    radius: usize,
) {
    let at = |k: isize| from[start + (k.clamp(0, count as isize - 1) as usize) * stride];
    let reach = radius as isize;
    let mut sum = [0.0; 3];
    for k in -reach..=reach {
        add(&mut sum, at(k), 1.0);
    }
    let weight = 1.0 / (2 * radius + 1) as f32;
    for k in 0..count as isize {
        to[start + k as usize * stride] = sum.map(|total| total * weight);
        add(&mut sum, at(k + reach + 1), 1.0);
        add(&mut sum, at(k - reach), -1.0);
    }
}

fn add(sum: &mut Glow, value: Glow, sign: f32) {
    for (total, part) in sum.iter_mut().zip(value) {
        *total += part * sign;
    }
}
