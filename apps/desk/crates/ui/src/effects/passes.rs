use std::array::from_fn;
use std::f32::consts::SQRT_2;

use super::Rgb;

const BAYER: [[u8; 4]; 4] = [[0, 8, 2, 10], [12, 4, 14, 6], [3, 11, 1, 9], [15, 7, 13, 5]];
const GLOW_STEP: usize = 4;
const GLOW_PHASES: [(bool, u32); GLOW_STEP] = [(false, 160), (false, 224), (true, 32), (true, 96)];
const BLUR_PASSES: usize = 2;
const ONE: u32 = 256;
const CRT_BEND: f32 = 0.18;
const CRT_EDGE: f32 = 80.0;
const CRT_MASK: u32 = 160;
const CRT_LINE: u32 = 190;
const CRT_BLOOM_CUT: u8 = 110;
const CRT_BLOOM_RADIUS: u8 = 3;
const WHITE: Rgb = [255; 3];
const REACH_RAMP: f32 = 0.25;
const RISE_PEAK: f32 = 0.6;
const RISE_BLOOM_SHARE: u8 = 4;

type Glow = [f32; 3];
type Light = [u32; 3];

fn weight(strength: u8) -> u32 {
    u32::from(strength) + u32::from(strength >> 7)
}

pub fn gain(strength: u8) -> f32 {
    weight(strength) as f32 / ONE as f32
}

pub fn crt_bend(curve: u8) -> f32 {
    f32::from(curve) / 255.0 * CRT_BEND
}

pub fn crt_edge(su: f32, sv: f32) -> f32 {
    ((1.0 - su.abs()).min(1.0 - sv.abs()) * CRT_EDGE).clamp(0.0, 1.0)
}

fn mask_factor(row: usize, column: usize, channel: usize, weight: u32) -> u32 {
    let mut raw = if channel == column { ONE } else { CRT_MASK };
    if row == 2 {
        raw = (raw * CRT_LINE) >> 8;
    }
    ONE - (((ONE - raw) * weight) >> 8)
}

pub fn crt_mask_mean(strength: u8) -> f32 {
    let weight = weight(strength);
    let mut sum = 0;
    for row in 0..3 {
        for column in 0..3 {
            for channel in 0..3 {
                sum += mask_factor(row, column, channel, weight);
            }
        }
    }
    sum as f32 / (27 * ONE) as f32
}

pub fn scan_mean(period: u8, strength: u8) -> f32 {
    1.0 - gain(strength) / f32::from(period.max(2))
}

pub fn grain_reach(amount: u8, strength: u8) -> f32 {
    f32::from(amount) / 255.0 * gain(strength)
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

pub fn dither(pixels: &mut [u8], width: usize, levels: u8, cell: u8, strength: u8) {
    let cell = usize::from(cell.max(1));
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
        let band = &tables[(y / cell) & 3];
        for (x, px) in row.iter_mut().enumerate() {
            let table = &band[(x / cell) & 3];
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

pub fn fade(pixels: &mut [u8], width: usize, height: usize, color: Rgb, anchor: u8, strength: u8) {
    let start = f32::from(anchor.min(254)) / 255.0;
    let tall = height.max(1) as f32;
    let full = weight(strength) as f32;
    for (y, row) in rows(pixels, width) {
        let share = ((y as f32 + 0.5) / tall - start) / (1.0 - start);
        let weight = (share.clamp(0.0, 1.0) * full).round() as u32;
        if weight == 0 {
            continue;
        }
        for px in row {
            for (value, to) in px.iter_mut().zip(color) {
                *value = blend(*value, to, weight);
            }
        }
    }
}

pub fn reveal(
    pixels: &mut [u8],
    before: &[u8],
    width: usize,
    height: usize,
    anchor: u8,
    opacity: u8,
) {
    let start = f32::from(anchor) / 255.0;
    let tall = height.max(1) as f32;
    let full = weight(opacity) as f32;
    let stride = width.saturating_mul(4).max(4);
    for ((y, row), was) in rows(pixels, width).zip(before.chunks_exact(stride)) {
        let cover = (((y as f32 + 0.5) / tall - start) / REACH_RAMP + 1.0).clamp(0.0, 1.0);
        let weight = (cover * full).round() as u32;
        for (px, was) in row.iter_mut().zip(was.as_chunks::<4>().0) {
            for (value, from) in px.iter_mut().zip(was).take(3) {
                *value = blend(*from, *value, weight);
            }
        }
    }
}

pub fn grain(pixels: &mut [u8], width: usize, amount: u8, size: u8, seed: u32, strength: u8) {
    let weight = weight(strength);
    let amount = i64::from(amount);
    let size = usize::from(size.max(1));
    for (y, row) in rows(pixels, width) {
        for (x, px) in row.iter_mut().enumerate() {
            let (gx, gy) = ((x / size) as u64, (y / size) as u64);
            let shift = ((noise(gx, gy, u64::from(seed)) * amount + (1 << 22)) >> 23) as i32;
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
    color: Rgb,
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
                for ((value, add), hue) in px.iter_mut().zip(light).zip(color) {
                    let add = ((add + ONE / 2) >> 8) * u32::from(hue) / 255;
                    *value = (u32::from(*value) + add).min(255) as u8;
                }
            }
        }
    }
}

pub fn crt(pixels: &mut [u8], width: usize, height: usize, curve: u8, bloom: u8, strength: u8) {
    if width == 0 || height == 0 {
        return;
    }
    let from = pixels.to_vec();
    let bend = crt_bend(curve);
    let (wide, tall) = (width as f32, height as f32);
    let sample = |x: f32, y: f32| -> [f32; 3] {
        let (left, top) = (x.floor(), y.floor());
        let (dx, dy) = (x - left, y - top);
        let at = |cx: f32, cy: f32| {
            let column = (cx.max(0.0) as usize).min(width - 1);
            let line = (cy.max(0.0) as usize).min(height - 1);
            let start = (line * width + column) * 4;
            from.get(start..start + 3)
                .map_or([0.0; 3], |px| from_fn(|c| f32::from(px[c])))
        };
        let (a, b) = (at(left, top), at(left + 1.0, top));
        let (c, d) = (at(left, top + 1.0), at(left + 1.0, top + 1.0));
        from_fn(|k| {
            let upper = a[k] + (b[k] - a[k]) * dx;
            let lower = c[k] + (d[k] - c[k]) * dx;
            upper + (lower - upper) * dy
        })
    };
    let weight = weight(strength);
    let mask: [[[u32; 3]; 3]; 3] = from_fn(|row| {
        from_fn(|column| from_fn(|channel| mask_factor(row, column, channel, weight)))
    });
    for (y, row) in rows(pixels, width) {
        let v = (y as f32 + 0.5) / tall * 2.0 - 1.0;
        let band = &mask[y % 3];
        for (x, px) in row.iter_mut().enumerate() {
            let u = (x as f32 + 0.5) / wide * 2.0 - 1.0;
            let stretch = 1.0 + bend * (u * u + v * v);
            let (su, sv) = (u * stretch, v * stretch);
            let edge = crt_edge(su, sv);
            let seen = sample((su + 1.0) / 2.0 * wide - 0.5, (sv + 1.0) / 2.0 * tall - 0.5);
            let factors = &band[x % 3];
            for ((value, light), factor) in px.iter_mut().zip(seen).zip(factors) {
                let lit = (light * edge).round() as u32;
                *value = ((lit * factor + ONE / 2) >> 8).min(255) as u8;
            }
        }
    }
    glow(
        pixels,
        width,
        height,
        CRT_BLOOM_CUT,
        CRT_BLOOM_RADIUS,
        WHITE,
        bloom,
    );
}

pub struct Rise {
    pub threshold: u8,
    pub radius: u8,
    pub color: Rgb,
    pub anchor: u8,
}

pub fn rising_glow(pixels: &mut [u8], width: usize, height: usize, rise: Rise, strength: u8) {
    let bloom = (rise.radius / RISE_BLOOM_SHARE).max(1);
    glow(
        pixels,
        width,
        height,
        rise.threshold,
        bloom,
        rise.color,
        strength,
    );
    let spread = (f32::from(rise.radius) * GLOW_STEP as f32).max(1.0);
    let source = height as f32 * (1.0 - f32::from(rise.anchor) / 255.0);
    let peak = weight(strength) as f32 * RISE_PEAK;
    for (y, row) in rows(pixels, width) {
        let near = (1.0 - (y as f32 + 0.5 - source).abs() / spread).max(0.0);
        let lift = (near * near * peak).round() as u32;
        if lift == 0 {
            continue;
        }
        let add: [u32; 3] = from_fn(|c| (u32::from(rise.color[c]) * lift + ONE / 2) >> 8);
        for px in row {
            for (value, add) in px.iter_mut().zip(add) {
                let room = 255 - u32::from(*value);
                *value += ((add * room + 127) / 255) as u8;
            }
        }
    }
}

fn luma_at_share(pixels: &[u8], share: u8) -> u8 {
    let mut counts = [0u64; 256];
    for px in pixels.as_chunks::<4>().0 {
        counts[usize::from(luma(px))] += 1;
    }
    let total: u64 = counts.iter().sum();
    let wanted = total * u64::from(share) / 255;
    let mut seen = 0;
    for (level, count) in counts.iter().enumerate() {
        seen += count;
        if seen >= wanted {
            return level as u8;
        }
    }
    u8::MAX
}

pub fn pixel_sort(pixels: &mut [u8], width: usize, share: u8, strength: u8) {
    let weight = weight(strength);
    let threshold = luma_at_share(pixels, share);
    let mut sorted: Vec<[u8; 4]> = Vec::with_capacity(width);
    for (_, row) in rows(pixels, width) {
        let mut rest = row;
        while !rest.is_empty() {
            let dark = rest.iter().take_while(|px| luma(px) <= threshold).count();
            let (_, tail) = std::mem::take(&mut rest).split_at_mut(dark);
            let bright = tail.iter().take_while(|px| luma(px) > threshold).count();
            let (run, after) = tail.split_at_mut(bright);
            sorted.clear();
            sorted.extend_from_slice(run);
            sorted.sort_unstable_by_key(luma);
            for (px, to) in run.iter_mut().zip(&sorted) {
                for (value, target) in px.iter_mut().zip(to) {
                    *value = blend(*value, *target, weight);
                }
            }
            rest = after;
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
