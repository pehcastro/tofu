use gpui::{Hsla, Rgba, rgb_to_hsla};

use super::super::passes;
use super::super::{Effect, Stage};
use super::{Ascii, Knob, Shape, Toggle};

const RAMP: usize = 256;
const INK_LIFT: f32 = 0.3;
const LIGHT_WASH: f32 = 0.55;
const LIGHT_INK: f32 = 0.35;
const FLICKER_RATE: f32 = 14.0;
const FLICKER_REACH: f32 = 0.4;
const STIR_LIFT: f32 = 0.35;
const STIR_SCRAMBLE: f32 = 0.15;
const DEPTH_DIM: f32 = 0.6;
const DEPTH_THIN: f32 = 0.5;
const GRAIN_REACH: f32 = 0.5;
const GLOW_FROM: f32 = 0.65;
const REVEAL_SPAN: f32 = 0.35;
const WARP_FREQUENCY: f32 = 1.7;
const WARP_REACH: f32 = 0.5;
const OCTAVES: usize = 3;
const WARP_STEPS: usize = 4;
const SPECK_SALT: u32 = 0x7f4a;
const ARC_BEND: f32 = 0.7;
const FOCUS_FULL: f32 = 0.2;
const FOCUS_REACH: f32 = 0.45;
const RISE_PEAK: f32 = 0.4;
const HORIZON_PEAK: f32 = 0.08;
const GLOW_NEAR: f32 = 1.1;
const GLOW_FAR: f32 = 1.8;
const GLOW_NEAR_INK: f32 = 0.22;
const GLOW_FAR_INK: f32 = 0.12;
const FIRST_STOP: f32 = 1.0 / 3.0;
const UNSEEN: f32 = 0.5 / 255.0;

#[derive(Clone, Copy, Default)]
pub struct Cell {
    pub back: [f32; 3],
    pub light: f32,
    pub ink: [f32; 3],
    pub glyph: usize,
    pub alpha: f32,
    pub glow: f32,
}

#[derive(Clone, Copy)]
pub struct Wash {
    pub at: [f32; 2],
    pub size: [f32; 2],
    pub top: Rgba,
    pub top_from: f32,
    pub bottom: Rgba,
}

#[derive(Clone, Copy)]
pub struct Halo {
    pub centre: [f32; 2],
    pub side: f32,
    pub color: Rgba,
}

#[derive(Clone, Copy)]
pub struct Ink {
    pub at: [f32; 2],
    pub glyph: usize,
    pub color: Hsla,
}

#[derive(Clone, Copy)]
pub struct Metrics {
    pub lift: f32,
    pub inset: f32,
}

#[derive(Default)]
pub struct Cells {
    pub washes: Vec<Wash>,
    pub halos: Vec<Halo>,
    pub inks: Vec<Ink>,
    all: Vec<Cell>,
    by_glyph: Vec<usize>,
    starts: Vec<usize>,
}

fn rgba(c: [f32; 3], a: f32) -> Rgba {
    Rgba::new(c[0], c[1], c[2], a)
}

impl Cells {
    fn wash(&mut self, field: &Field, gradient: f32) {
        self.washes.clear();
        if gradient <= 0.0 {
            return;
        }
        let stride = field.columns + 1;
        let corner = |edge: usize, line: usize| {
            field
                .lattice
                .get(line * stride + edge)
                .copied()
                .unwrap_or_default()
        };
        for row in 0..field.rows {
            for column in 0..field.columns {
                let Some(here) = self.all.get(row * field.columns + column) else {
                    continue;
                };
                let below = self
                    .all
                    .get((row + 1) * field.columns + column)
                    .filter(|_| row + 1 < field.rows)
                    .unwrap_or(here);
                if gradient * here.light.max(below.light) < UNSEEN {
                    continue;
                }
                let (a, b) = (corner(column, row), corner(column + 1, row));
                let (c, d) = (corner(column, row + 1), corner(column + 1, row + 1));
                let (left, right) = ((a[0] + c[0]) / 2.0, (b[0] + d[0]) / 2.0);
                let (top, bottom) = ((a[1] + b[1]) / 2.0, (c[1] + d[1]) / 2.0);
                if right <= left || bottom <= top {
                    continue;
                }
                self.washes.push(Wash {
                    at: [left, top],
                    size: [right - left, bottom - top],
                    top: rgba(here.back, gradient * here.light),
                    top_from: if row == 0 { FIRST_STOP } else { 0.0 },
                    bottom: rgba(below.back, gradient * below.light),
                });
            }
        }
    }

    fn halo(&mut self, places: &[Place], cell: f32, glowing: bool) {
        self.halos.clear();
        for (here, place) in self.all.iter().zip(places).filter(|_| glowing) {
            if here.glow * GLOW_NEAR_INK < UNSEEN {
                continue;
            }
            for (reach, strength) in [(GLOW_FAR, GLOW_FAR_INK), (GLOW_NEAR, GLOW_NEAR_INK)] {
                if here.glow * strength < UNSEEN {
                    continue;
                }
                let side = cell * reach;
                self.halos.push(Halo {
                    centre: place.at,
                    side,
                    color: rgba(here.ink, here.glow * strength),
                });
            }
        }
    }

    fn ink(&mut self, places: &[Place], glyphs: usize, metrics: Metrics) {
        self.group(glyphs);
        self.inks.clear();
        let (all, by_glyph) = (&self.all, &self.by_glyph);
        self.inks.extend(by_glyph.iter().filter_map(|&at| {
            let (here, place) = (all.get(at)?, places.get(at)?);
            Some(Ink {
                at: [place.at[0] - metrics.inset, place.at[1] + metrics.lift],
                glyph: here.glyph,
                color: rgb_to_hsla(rgba(here.ink, here.alpha)),
            })
        }));
    }

    fn group(&mut self, glyphs: usize) {
        let seen = |cell: &Cell| cell.alpha >= UNSEEN;
        self.starts.clear();
        self.starts.resize(glyphs.max(1) + 1, 0);
        for cell in self.all.iter().filter(|cell| seen(cell)) {
            if let Some(count) = self.starts.get_mut(cell.glyph + 1) {
                *count += 1;
            }
        }
        for at in 1..self.starts.len() {
            self.starts[at] += self.starts[at - 1];
        }
        let total = self.starts.last().copied().unwrap_or(0);
        self.by_glyph.clear();
        self.by_glyph.resize(total, 0);
        for (at, cell) in self.all.iter().enumerate().filter(|(_, cell)| seen(cell)) {
            let Some(next) = self.starts.get_mut(cell.glyph) else {
                continue;
            };
            if let Some(slot) = self.by_glyph.get_mut(*next) {
                *slot = at;
            }
            *next += 1;
        }
    }
}

#[derive(Clone, Copy, Default)]
pub struct Place {
    pub at: [f32; 2],
    keep: f32,
}

#[derive(Clone, Copy, Default)]
struct Tone {
    color: [f32; 3],
    alpha: f32,
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Finish {
    bend: Option<f32>,
    shade: f32,
    grain: f32,
    glow: f32,
}

impl Finish {
    pub fn of(after: &[Stage]) -> Finish {
        let mut finish = Finish {
            bend: None,
            shade: 1.0,
            grain: 0.0,
            glow: 0.0,
        };
        for stage in after {
            let seen = passes::gain(stage.opacity);
            let dim = |mean: f32| 1.0 - (1.0 - mean) * seen;
            match stage.effect {
                Effect::Crt { curve, bloom } => {
                    finish.bend = Some(passes::crt_bend(curve) * seen);
                    finish.shade *= dim(passes::crt_mask_mean(stage.strength));
                    finish.glow += passes::gain(bloom) * seen;
                }
                Effect::Scanlines { period } => {
                    finish.shade *= dim(passes::scan_mean(period, stage.strength))
                }
                Effect::Grain { amount, .. } => {
                    finish.grain += passes::grain_reach(amount, stage.strength) * seen
                }
                Effect::Glow { .. } => finish.glow += passes::gain(stage.strength) * seen,
                Effect::Duotone { .. }
                | Effect::Dither { .. }
                | Effect::Fade { .. }
                | Effect::Halftone { .. }
                | Effect::PixelSort { .. } => {}
            }
        }
        finish
    }
}

#[derive(Clone, Copy, PartialEq)]
struct Grid {
    cell: f32,
    width: f32,
    height: f32,
    finish: Finish,
}

#[derive(Clone, Copy, Default)]
pub struct Spot {
    pub x: f32,
    pub y: f32,
    pub strength: f32,
}

#[derive(Clone, Copy, Default)]
pub struct Ring {
    pub x: f32,
    pub y: f32,
    pub radius: f32,
    pub width: f32,
    pub strength: f32,
}

pub const SPOTS: usize = 25;
pub const RINGS: usize = 6;

#[derive(Clone, Copy)]
pub struct Frame {
    pub time: f32,
    pub reveal: f32,
    pub glyphs: usize,
    pub metrics: Metrics,
    pub spots: [Spot; SPOTS],
    pub lit: usize,
    pub rings: [Ring; RINGS],
    pub waves: usize,
}

#[derive(Clone)]
pub struct Field {
    pub columns: usize,
    pub rows: usize,
    pub places: Vec<Place>,
    pub lattice: Vec<[f32; 2]>,
    grid: Option<Grid>,
    back: Vec<Tone>,
    ink: Vec<[f32; 3]>,
}

fn to_linear(channel: u8) -> f32 {
    linear(f32::from(channel) / 255.0)
}

fn linear(c: f32) -> f32 {
    if c <= 0.04045 {
        c / 12.92
    } else {
        ((c + 0.055) / 1.055).powf(2.4)
    }
}

fn to_srgb(c: f32) -> f32 {
    let c = c.clamp(0.0, 1.0);
    if c <= 0.003_130_8 {
        c * 12.92
    } else {
        1.055 * c.powf(1.0 / 2.4) - 0.055
    }
}

fn oklab(color: [u8; 3]) -> [f32; 3] {
    let [r, g, b] = color.map(to_linear);
    let l = (0.412_221_46 * r + 0.536_332_55 * g + 0.051_445_995 * b).cbrt();
    let m = (0.211_903_5 * r + 0.680_699_5 * g + 0.107_396_96 * b).cbrt();
    let s = (0.088_302_46 * r + 0.281_718_85 * g + 0.629_978_7 * b).cbrt();
    [
        0.210_454_26 * l + 0.793_617_8 * m - 0.004_072_047 * s,
        1.977_998_5 * l - 2.428_592_2 * m + 0.450_593_7 * s,
        0.025_904_037 * l + 0.782_771_77 * m - 0.808_675_77 * s,
    ]
}

fn from_oklab([l, a, b]: [f32; 3]) -> [f32; 3] {
    let l2 = (l + 0.396_337_78 * a + 0.215_803_76 * b).powi(3);
    let m2 = (l - 0.105_561_346 * a - 0.063_854_17 * b).powi(3);
    let s2 = (l - 0.089_484_18 * a - 1.291_485_5 * b).powi(3);
    [
        4.076_741_7 * l2 - 3.307_711_6 * m2 + 0.230_969_94 * s2,
        -1.268_438 * l2 + 2.609_757_4 * m2 - 0.341_319_4 * s2,
        -0.004_196_086_3 * l2 - 0.703_418_6 * m2 + 1.707_614_7 * s2,
    ]
    .map(to_srgb)
}

fn ramp(stops: &[[f32; 3]], at: f32) -> [f32; 3] {
    let Some(last) = stops.len().checked_sub(1) else {
        return [0.0; 3];
    };
    let scaled = at.clamp(0.0, 1.0) * last as f32;
    let low = (scaled.floor() as usize).min(last);
    let high = (low + 1).min(last);
    let toward = scaled - low as f32;
    let (from, to) = (stops[low], stops[high]);
    from_oklab([0, 1, 2].map(|c| from[c] + (to[c] - from[c]) * toward))
}

fn hash(x: u32, y: u32, salt: u32) -> f32 {
    let mut h =
        x.wrapping_mul(0x8da6_b343) ^ y.wrapping_mul(0xd816_3841) ^ salt.wrapping_mul(0xcb1a_b31f);
    h ^= h >> 15;
    h = h.wrapping_mul(0x2c1b_3c6d);
    h ^= h >> 12;
    h = h.wrapping_mul(0x297a_2d39);
    h ^= h >> 15;
    (h >> 8) as f32 / (1u32 << 24) as f32
}

fn value_noise(x: f32, y: f32) -> f32 {
    let (left, top) = (x.floor(), y.floor());
    let (fx, fy) = (x - left, y - top);
    let (sx, sy) = (fx * fx * (3.0 - 2.0 * fx), fy * fy * (3.0 - 2.0 * fy));
    let (cx, cy) = (left as i32 as u32, top as i32 as u32);
    let at = |dx: u32, dy: u32| hash(cx.wrapping_add(dx), cy.wrapping_add(dy), 0x51ed);
    let upper = at(0, 0) + (at(1, 0) - at(0, 0)) * sx;
    let lower = at(0, 1) + (at(1, 1) - at(0, 1)) * sx;
    upper + (lower - upper) * sy
}

fn fbm(x: f32, y: f32) -> f32 {
    let (mut sum, mut weight, mut frequency, mut total) = (0.0, 0.5, 1.0, 0.0);
    for _ in 0..OCTAVES {
        sum += value_noise(x * frequency, y * frequency) * weight;
        total += weight;
        weight *= 0.5;
        frequency *= 2.0;
    }
    sum / total
}

fn warp(bend: f32, su: f32, sv: f32) -> [f32; 2] {
    let reach = (su * su + sv * sv).sqrt();
    if bend <= 0.0 || reach <= 0.0 {
        return [su, sv];
    }
    let mut r = reach;
    for _ in 0..WARP_STEPS {
        r -= (r + bend * r * r * r - reach) / (1.0 + 3.0 * bend * r * r);
    }
    [su * r / reach, sv * r / reach]
}

fn light_tone(color: [f32; 3], wash: bool) -> Tone {
    let lit = color.map(linear);
    let alpha = lit[0].max(lit[1]).max(lit[2]);
    let color = match (wash, alpha > 0.0) {
        (true, _) => color.map(|c| c + (1.0 - c) * LIGHT_WASH),
        (false, true) => lit.map(|c| to_srgb(c / alpha)),
        (false, false) => color,
    };
    Tone { color, alpha }
}

fn shape(shape: Shape, x: f32, y: f32, soft: f32, time: f32, wave: f32) -> f32 {
    let lit = match shape {
        Shape::Horizon => {
            let line = 0.08 + 0.03 * wave * (x * 2.2 + time * 0.7).sin();
            let d = y - line;
            if d < 0.0 {
                (d / (soft * 1.6)).exp()
            } else {
                (-d / (soft * 0.45)).exp()
            }
        }
        Shape::Pinch => {
            let waist = soft * (0.15 + 3.0 * y * y);
            (-x.abs() / waist).exp() * (1.0 - 0.3 * y.abs())
        }
        Shape::Rise => {
            let edge = 0.15
                - wave
                    * (0.06 * (x * 3.0 + time * 0.9).sin() + 0.04 * (x * 7.0 - time * 1.3).sin());
            let d = (y - edge) / (soft * 0.5);
            (1.0 / (1.0 + (-d).exp())) * (1.0 - 0.25 * x.abs())
        }
        Shape::Eclipse => {
            let d = (x * x + y * y).sqrt() - 0.26;
            if d < 0.0 {
                0.9 * (d / (soft * 0.12)).exp()
            } else {
                (-d / (soft * 0.8)).exp()
            }
        }
    };
    lit.clamp(0.0, 1.0)
}

impl Field {
    pub fn new() -> Field {
        Field {
            columns: 0,
            rows: 0,
            places: Vec::new(),
            lattice: Vec::new(),
            grid: None,
            back: vec![Tone::default(); RAMP],
            ink: vec![[0.0; 3]; RAMP],
        }
    }

    pub fn fits(&self, cell: f32, width: f32, height: f32, finish: Finish) -> bool {
        self.grid
            == Some(Grid {
                cell,
                width,
                height,
                finish,
            })
    }

    pub fn fit(&mut self, cell: f32, width: f32, height: f32, finish: Finish) {
        if self.fits(cell, width, height, finish) {
            return;
        }
        self.grid = Some(Grid {
            cell,
            width,
            height,
            finish,
        });
        let (columns, rows) = (
            (width / cell).ceil() as usize,
            (height / cell).ceil() as usize,
        );
        self.columns = columns;
        self.rows = rows;
        let bend = finish.bend.unwrap_or(0.0);
        let shown = |x: f32, y: f32| {
            let [u, v] = warp(bend, x / width * 2.0 - 1.0, y / height * 2.0 - 1.0);
            [(u + 1.0) / 2.0 * width, (v + 1.0) / 2.0 * height]
        };
        let half = cell / 2.0;
        self.lattice.clear();
        for line in 0..=rows {
            let y = match line {
                0 => 0.0,
                _ => (line as f32 * cell + half).min(height),
            };
            self.lattice
                .extend((0..=columns).map(|edge| shown((edge as f32 * cell).min(width), y)));
        }
        self.places.clear();
        self.places.extend((0..rows).flat_map(|row| {
            (0..columns).map(move |column| {
                let (x, y) = ((column as f32 + 0.5) * cell, (row as f32 + 0.5) * cell);
                let (su, sv) = (x / width * 2.0 - 1.0, y / height * 2.0 - 1.0);
                let edge = finish.bend.map_or(1.0, |_| passes::crt_edge(su, sv));
                let speck = (hash(column as u32, row as u32, SPECK_SALT) - 0.5) * 2.0;
                Place {
                    at: shown(x, y),
                    keep: (edge * finish.shade * (1.0 + speck * finish.grain)).clamp(0.0, 1.0),
                }
            })
        }));
    }

    pub fn source(&self, x: f32, y: f32) -> (f32, f32) {
        let Some(grid) = self
            .grid
            .filter(|grid| grid.width > 0.0 && grid.height > 0.0)
        else {
            return (x, y);
        };
        let (u, v) = (x / grid.width * 2.0 - 1.0, y / grid.height * 2.0 - 1.0);
        let stretch = 1.0 + grid.finish.bend.unwrap_or(0.0) * (u * u + v * v);
        (
            (u * stretch + 1.0) / 2.0 * grid.width,
            (v * stretch + 1.0) / 2.0 * grid.height,
        )
    }

    pub fn tune(&mut self, ascii: &Ascii) {
        let stops: Vec<[f32; 3]> = ascii.colors.iter().copied().map(oklab).collect();
        let light = ascii.toggle(Toggle::Light);
        for (at, (back, ink)) in self.back.iter_mut().zip(&mut self.ink).enumerate() {
            let level = at as f32 / (RAMP - 1) as f32;
            let color = ramp(&stops, level);
            let lifted = ramp(&stops, level + INK_LIFT);
            *back = light_tone(color, light);
            *ink = match light {
                true => lifted.map(|c| c * LIGHT_INK),
                false => lifted,
            };
        }
    }

    pub fn step(&self, ascii: &Ascii, frame: &Frame, cells: &mut Cells) {
        let Some(grid) = self.grid else {
            return;
        };
        self.shade(grid, ascii, frame, &mut cells.all);
        cells.wash(self, ascii.knob(Knob::GradientOpacity));
        cells.halo(&self.places, grid.cell, ascii.knob(Knob::Glow) > 0.0);
        cells.ink(&self.places, frame.glyphs, frame.metrics);
    }

    fn shade(&self, grid: Grid, ascii: &Ascii, frame: &Frame, cells: &mut Vec<Cell>) {
        cells.resize(self.places.len(), Cell::default());
        let spots = frame.spots.get(..frame.lit).unwrap_or_default();
        let rings = frame.rings.get(..frame.waves).unwrap_or_default();
        let (cell, width, height) = (grid.cell, grid.width, grid.height);
        let scale = ascii.knob(Knob::Scale);
        let flow = ascii.knob(Knob::Flow);
        let curve = ascii.knob(Knob::Curve);
        let arc = 1.0 - (curve * 2.0 - 1.0).abs();
        let wave = (curve * 2.0 - 1.0).max(0.0);
        let variation = ascii.knob(Knob::Variation) * wave;
        let (origin_x, origin_y) = (ascii.knob(Knob::OriginX), ascii.knob(Knob::OriginY));
        let focus =
            (((origin_x - 0.5).powi(2) + (origin_y - 0.5).powi(2)).sqrt() / FOCUS_FULL).min(1.0);
        let peak = focus
            * match ascii.shape {
                Shape::Rise => RISE_PEAK,
                Shape::Horizon => HORIZON_PEAK,
                Shape::Pinch | Shape::Eclipse => 0.0,
            };
        let (sin, cos) = ascii.knob(Knob::Angle).to_radians().sin_cos();
        let soft = 0.04 + ascii.knob(Knob::Softness) * 0.4;
        let density = ascii.knob(Knob::Density).max(0.02);
        let depth = ascii.knob(Knob::Depth);
        let flicker = ascii.knob(Knob::Flicker);
        let grain = ascii.knob(Knob::Grain);
        let glyph_opacity = ascii.knob(Knob::GlyphOpacity);
        let glow = (ascii.knob(Knob::Glow) * (1.0 + grid.finish.glow)).min(1.0);
        let reach = ascii.knob(Knob::Scramble).max(1.0);
        let reach_squared = reach * reach;
        let time = frame.time * ascii.knob(Knob::Speed);
        let drift = time * flow;
        let aspect = width / height.max(1.0);
        let tick = (frame.time * FLICKER_RATE) as u32;
        let last = frame.glyphs.saturating_sub(1);
        let columns = self.columns.max(1);
        for (at, (out, place)) in cells.iter_mut().zip(&self.places).enumerate() {
            let (column, row) = (at % columns, at / columns);
            let (px, py) = ((column as f32 + 0.5) * cell, (row as f32 + 0.5) * cell);
            let (u, v) = (px / width.max(1.0), py / height.max(1.0));
            let (dx, dy) = ((u - origin_x) * aspect, v - origin_y);
            let mut x = (dx * cos + dy * sin) / scale;
            let mut y = (dy * cos - dx * sin) / scale + peak;
            y -= arc * ARC_BEND * x * x;
            if variation > 0.0 {
                let a = fbm(
                    x * WARP_FREQUENCY + drift * 0.35,
                    y * WARP_FREQUENCY - drift * 0.2,
                );
                let b = fbm(
                    x * WARP_FREQUENCY + 3.1 - drift * 0.25,
                    y * WARP_FREQUENCY + 7.7 + drift * 0.3,
                );
                x += (a - 0.5) * variation * WARP_REACH;
                y += (b - 0.5) * variation * WARP_REACH;
            }
            let radial = (-(dx * dx + dy * dy).sqrt() / (FOCUS_REACH * scale)).exp();
            let lit = shape(ascii.shape, x, y, soft, time, wave) * (1.0 - focus + focus * radial);
            let (cx, cy) = (column as u32, row as u32);
            let speck = (hash(cx, cy, tick ^ 0x9e37) - 0.5) * grain * GRAIN_REACH;
            let index = ((lit * (RAMP - 1) as f32) as usize).min(RAMP - 1);
            let tone = self.back[index];
            let back = tone.color.map(|c| (c * (1.0 + speck)).clamp(0.0, 1.0));
            let mut level = lit + (hash(cx, cy, tick) - 0.5) * flicker * FLICKER_REACH;
            let blur = depth * ((v - 0.5).abs() * 2.0).powi(2);
            level *= 1.0 - blur * DEPTH_THIN;
            let mut stir: f32 = 0.0;
            for spot in spots {
                let near = (px - spot.x).powi(2) + (py - spot.y).powi(2);
                if near < reach_squared {
                    stir = stir.max(spot.strength * (1.0 - near.sqrt() / reach));
                }
            }
            for ring in rings {
                let d = ((px - ring.x).powi(2) + (py - ring.y).powi(2)).sqrt();
                let off = (d - ring.radius).abs();
                if off < ring.width {
                    stir = stir.max(ring.strength * (1.0 - off / ring.width));
                }
            }
            level += stir * STIR_LIFT;
            let shown = ((level - (1.0 - density)) / density).clamp(0.0, 1.0);
            let fade =
                ((frame.reveal * (1.0 + REVEAL_SPAN) - (1.0 - v)) / REVEAL_SPAN).clamp(0.0, 1.0);
            let scrambled = stir > STIR_SCRAMBLE || (fade > 0.0 && fade < 1.0);
            let glyph = match (scrambled, last) {
                (_, 0) => 0,
                (true, _) => {
                    1 + (hash(cx, cy, tick.wrapping_add(0x5bd1)) * last as f32) as usize % last
                }
                (false, _) => ((shown * last as f32).round() as usize).min(last),
            };
            let kept = fade * place.keep;
            *out = Cell {
                back,
                light: tone.alpha * kept,
                ink: self.ink[index],
                glyph,
                alpha: glyph_opacity * (0.4 + 0.6 * shown) * (1.0 - blur * DEPTH_DIM) * kept,
                glow: glow * ((shown - GLOW_FROM) / (1.0 - GLOW_FROM)).clamp(0.0, 1.0) * kept,
            };
        }
    }
}
