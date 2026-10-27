use std::collections::HashMap;

use desk_ui::effects::controls::{Dial, Unit};
use desk_ui::effects::{Effect as Pass, Rgb, Stage};
use serde_json::{Map, Value};

use super::paint::SURFACE;
use super::remembered::Effect;

const GRAIN_SEED: u32 = 7;
const GLOW_THRESHOLD: u8 = 150;
const GLOW_CELL_PX: f32 = 4.0;
const CRT_BLOOM: u8 = 200;
const DITHER_STRENGTH: u8 = 210;
const HALFTONE_STRENGTH: u8 = 150;
const FULL: u8 = u8::MAX;

#[derive(Clone, Copy, PartialEq, Eq, Hash, Debug)]
pub enum Knob {
    Opacity,
    Anchor,
    Levels,
    Cell,
    Dot,
    Amount,
    Size,
    Spacing,
    Strength,
    Radius,
    Threshold,
    Curve,
    Mask,
}

#[derive(Clone, Copy, PartialEq, Eq, Hash, Debug)]
pub enum Hue {
    Shadow,
    Light,
    Glow,
}

impl Knob {
    fn key(self) -> &'static str {
        match self {
            Knob::Opacity => "opacity",
            Knob::Anchor => "anchor",
            Knob::Levels => "levels",
            Knob::Cell => "cell",
            Knob::Dot => "dot",
            Knob::Amount => "amount",
            Knob::Size => "size",
            Knob::Spacing => "spacing",
            Knob::Strength => "strength",
            Knob::Radius => "radius",
            Knob::Threshold => "threshold",
            Knob::Curve => "curve",
            Knob::Mask => "mask",
        }
    }

    fn spec(self, effect: Effect) -> (&'static str, f32, f32, f32, f32, Unit) {
        let share = |byte: u8| f32::from(byte) / 255.0;
        match self {
            Knob::Opacity => ("Opacity", 0.0, 1.0, 0.01, 1.0, Unit::Percent),
            Knob::Anchor => {
                let start = if effect == Effect::Fade { 0.5 } else { 0.0 };
                ("Position", 0.0, 1.0, 0.01, start, Unit::Percent)
            }
            Knob::Levels => ("Levels", 2.0, 16.0, 1.0, 6.0, Unit::Count),
            Knob::Cell => ("Cell", 1.0, 8.0, 1.0, 1.0, Unit::Px),
            Knob::Dot => ("Dot Size", 2.0, 24.0, 1.0, 6.0, Unit::Px),
            Knob::Amount => ("Amount", 0.0, 0.5, 0.005, share(14), Unit::Percent),
            Knob::Size => ("Size", 1.0, 6.0, 1.0, 1.0, Unit::Px),
            Knob::Spacing => ("Spacing", 2.0, 12.0, 1.0, 3.0, Unit::Px),
            Knob::Strength => ("Strength", 0.0, 1.0, 0.01, share(90), Unit::Percent),
            Knob::Radius => ("Radius", 20.0, 800.0, 4.0, 240.0, Unit::Px),
            Knob::Threshold => ("Threshold", 0.0, 1.0, 0.01, share(140), Unit::Percent),
            Knob::Curve => ("Curve", 0.0, 1.0, 0.01, share(160), Unit::Percent),
            Knob::Mask => ("Mask Strength", 0.0, 1.0, 0.01, 1.0, Unit::Percent),
        }
    }
}

impl Hue {
    pub fn name(self) -> &'static str {
        match self {
            Hue::Shadow => "Shadow",
            Hue::Light => "Light",
            Hue::Glow => "Colour",
        }
    }

    fn key(self) -> &'static str {
        match self {
            Hue::Shadow => "shadow",
            Hue::Light => "light",
            Hue::Glow => "color",
        }
    }

    fn start(self) -> Rgb {
        match self {
            Hue::Shadow => [22, 12, 34],
            Hue::Light => [240, 200, 146],
            Hue::Glow => [255; 3],
        }
    }
}

pub fn knobs(effect: Effect) -> &'static [Knob] {
    match effect {
        Effect::Sort => &[Knob::Opacity, Knob::Threshold],
        Effect::Dither => &[Knob::Opacity, Knob::Anchor, Knob::Levels, Knob::Cell],
        Effect::Fade => &[Knob::Opacity, Knob::Anchor],
        Effect::Duotone => &[Knob::Opacity],
        Effect::Halftone => &[Knob::Opacity, Knob::Anchor, Knob::Dot],
        Effect::Grain => &[Knob::Opacity, Knob::Amount, Knob::Size],
        Effect::Scan => &[Knob::Opacity, Knob::Spacing, Knob::Strength],
        Effect::Glow => &[Knob::Opacity, Knob::Anchor, Knob::Radius],
        Effect::Crt => &[Knob::Opacity, Knob::Curve, Knob::Mask],
        Effect::Ascii => &[],
    }
}

pub fn hues(effect: Effect) -> &'static [Hue] {
    match effect {
        Effect::Duotone => &[Hue::Shadow, Hue::Light],
        Effect::Glow => &[Hue::Glow],
        Effect::Sort
        | Effect::Dither
        | Effect::Fade
        | Effect::Halftone
        | Effect::Grain
        | Effect::Scan
        | Effect::Crt
        | Effect::Ascii => &[],
    }
}

#[derive(Clone, Default, PartialEq, Debug)]
pub struct Tuning {
    knobs: HashMap<(Effect, Knob), f32>,
    hues: HashMap<Hue, Rgb>,
}

impl Tuning {
    pub fn knob(&self, effect: Effect, knob: Knob) -> f32 {
        self.knobs
            .get(&(effect, knob))
            .copied()
            .unwrap_or(knob.spec(effect).4)
    }

    pub fn set_knob(&mut self, effect: Effect, knob: Knob, value: f32) {
        let (_, least, most, ..) = knob.spec(effect);
        self.knobs.insert((effect, knob), value.clamp(least, most));
    }

    pub fn hue(&self, hue: Hue) -> Rgb {
        self.hues.get(&hue).copied().unwrap_or(hue.start())
    }

    pub fn set_hue(&mut self, hue: Hue, color: Rgb) {
        self.hues.insert(hue, color);
    }

    pub fn dial(&self, effect: Effect, knob: Knob) -> Dial {
        let (label, least, most, step, _, unit) = knob.spec(effect);
        Dial {
            label,
            value: self.knob(effect, knob),
            least,
            most,
            step,
            unit,
        }
    }

    fn share(&self, effect: Effect, knob: Knob) -> u8 {
        (self.knob(effect, knob).clamp(0.0, 1.0) * 255.0).round() as u8
    }

    fn count(&self, effect: Effect, knob: Knob) -> u8 {
        self.knob(effect, knob).round().clamp(0.0, 255.0) as u8
    }

    pub fn stage(&self, effect: Effect) -> Option<Stage> {
        let anchor = self.share(effect, Knob::Anchor);
        let (pass, strength) = match effect {
            Effect::Sort => (
                Pass::PixelSort {
                    threshold: self.share(effect, Knob::Threshold),
                },
                FULL,
            ),
            Effect::Dither => (
                Pass::Dither {
                    levels: self.count(effect, Knob::Levels),
                    cell: self.count(effect, Knob::Cell),
                    anchor,
                },
                DITHER_STRENGTH,
            ),
            Effect::Fade => (
                Pass::Fade {
                    color: [(SURFACE >> 16) as u8, (SURFACE >> 8) as u8, SURFACE as u8],
                    anchor,
                },
                FULL,
            ),
            Effect::Duotone => (
                Pass::Duotone {
                    shadow: self.hue(Hue::Shadow),
                    light: self.hue(Hue::Light),
                },
                FULL,
            ),
            Effect::Halftone => (
                Pass::Halftone {
                    cell: self.count(effect, Knob::Dot),
                    anchor,
                },
                HALFTONE_STRENGTH,
            ),
            Effect::Grain => (
                Pass::Grain {
                    amount: self.share(effect, Knob::Amount),
                    size: self.count(effect, Knob::Size),
                    seed: GRAIN_SEED,
                },
                FULL,
            ),
            Effect::Scan => (
                Pass::Scanlines {
                    period: self.count(effect, Knob::Spacing),
                },
                self.share(effect, Knob::Strength),
            ),
            Effect::Glow => (
                Pass::Glow {
                    threshold: GLOW_THRESHOLD,
                    radius: (self.knob(effect, Knob::Radius) / GLOW_CELL_PX)
                        .round()
                        .clamp(1.0, 255.0) as u8,
                    color: self.hue(Hue::Glow),
                    anchor,
                },
                FULL,
            ),
            Effect::Crt => (
                Pass::Crt {
                    curve: self.share(effect, Knob::Curve),
                    bloom: CRT_BLOOM,
                },
                self.share(effect, Knob::Mask),
            ),
            Effect::Ascii => return None,
        };
        Some(Stage {
            effect: pass,
            strength,
            opacity: self.share(effect, Knob::Opacity),
        })
    }

    pub fn to_json(&self) -> Value {
        let mut all = Map::new();
        for effect in Effect::ALL {
            let mut one = Map::new();
            for knob in knobs(effect) {
                if let Some(value) = self.knobs.get(&(effect, *knob)) {
                    one.insert(knob.key().into(), Value::from(*value));
                }
            }
            for hue in hues(effect) {
                if let Some(color) = self.hues.get(hue) {
                    one.insert(hue.key().into(), Value::from(color.to_vec()));
                }
            }
            if !one.is_empty() {
                all.insert(effect.key().into(), Value::Object(one));
            }
        }
        Value::Object(all)
    }

    pub fn from_json(value: &Value) -> Result<Tuning, String> {
        let all = value
            .as_object()
            .ok_or_else(|| "tuning is not an object".to_owned())?;
        let mut tuning = Tuning::default();
        for effect in Effect::ALL {
            let Some(one) = all.get(effect.key()) else {
                continue;
            };
            for knob in knobs(effect) {
                let Some(found) = one.get(knob.key()) else {
                    continue;
                };
                let value = found.as_f64().ok_or_else(|| {
                    format!("tuning.{}.{} is not a number", effect.key(), knob.key())
                })?;
                tuning.set_knob(effect, *knob, value as f32);
            }
            for hue in hues(effect) {
                let Some(found) = one.get(hue.key()) else {
                    continue;
                };
                let color = rgb(found).ok_or_else(|| {
                    format!(
                        "tuning.{}.{} is not three numbers from 0 to 255",
                        effect.key(),
                        hue.key()
                    )
                })?;
                tuning.set_hue(*hue, color);
            }
        }
        Ok(tuning)
    }
}

fn rgb(value: &Value) -> Option<Rgb> {
    let parts = value.as_array()?;
    let mut color = [0; 3];
    if parts.len() != color.len() {
        return None;
    }
    for (slot, part) in color.iter_mut().zip(parts) {
        *slot = u8::try_from(part.as_u64()?).ok()?;
    }
    Some(color)
}
