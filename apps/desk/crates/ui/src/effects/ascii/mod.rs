mod field;
mod panel;
mod view;

use serde_json::{Map, Value, json};

use super::Rgb;
use crate::effects::controls::{Dial, Unit};

pub use panel::{AsciiChanged, AsciiPanel};
pub use view::GradientAscii;

pub const LEAST_COLORS: usize = 2;
pub const MOST_COLORS: usize = 8;
pub const MOST_CUSTOM: usize = 32;
const KNOBS: usize = 19;
const TOGGLES: usize = 3;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Shape {
    Horizon,
    Pinch,
    Rise,
    Eclipse,
}

impl Shape {
    pub const ALL: [Shape; 4] = [Shape::Horizon, Shape::Pinch, Shape::Rise, Shape::Eclipse];

    pub fn name(self) -> &'static str {
        match self {
            Shape::Horizon => "Horizon",
            Shape::Pinch => "Pinch",
            Shape::Rise => "Rise",
            Shape::Eclipse => "Eclipse",
        }
    }

    fn key(self) -> &'static str {
        match self {
            Shape::Horizon => "horizon",
            Shape::Pinch => "pinch",
            Shape::Rise => "rise",
            Shape::Eclipse => "eclipse",
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Click {
    Wave,
    Burst,
    Off,
}

impl Click {
    pub const ALL: [Click; 3] = [Click::Wave, Click::Burst, Click::Off];

    pub fn name(self) -> &'static str {
        match self {
            Click::Wave => "Wave",
            Click::Burst => "Burst",
            Click::Off => "None",
        }
    }

    fn key(self) -> &'static str {
        match self {
            Click::Wave => "wave",
            Click::Burst => "burst",
            Click::Off => "none",
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Charset {
    Classic,
    Letters,
    Blocks,
    Custom,
}

impl Charset {
    pub const ALL: [Charset; 4] = [
        Charset::Classic,
        Charset::Letters,
        Charset::Blocks,
        Charset::Custom,
    ];

    pub fn name(self) -> &'static str {
        match self {
            Charset::Classic => "Classic",
            Charset::Letters => "Letters",
            Charset::Blocks => "Blocks",
            Charset::Custom => "Custom",
        }
    }

    fn key(self) -> &'static str {
        match self {
            Charset::Classic => "classic",
            Charset::Letters => "letters",
            Charset::Blocks => "blocks",
            Charset::Custom => "custom",
        }
    }

    fn glyphs(self) -> Option<&'static str> {
        match self {
            Charset::Classic => Some(" =+xX0S8#@"),
            Charset::Letters => Some(" ilrtjcvxznuoeaskhdbpqgmwMW"),
            Charset::Blocks => Some(" \u{2591}\u{2592}\u{2593}\u{2588}"),
            Charset::Custom => None,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Preset {
    Atmosphere,
    Ember,
    Aurora,
    Eclipse,
    Mono,
}

impl Preset {
    pub const ALL: [Preset; 5] = [
        Preset::Atmosphere,
        Preset::Ember,
        Preset::Aurora,
        Preset::Eclipse,
        Preset::Mono,
    ];

    pub fn name(self) -> &'static str {
        match self {
            Preset::Atmosphere => "Atmosphere",
            Preset::Ember => "Ember",
            Preset::Aurora => "Aurora",
            Preset::Eclipse => "Eclipse",
            Preset::Mono => "Mono",
        }
    }

    pub fn apply(self, ascii: &mut Ascii) {
        let (shape, colors, softness, scale, flow, variation): (Shape, &[u32], _, _, _, _) =
            match self {
                Preset::Atmosphere => (
                    Shape::Horizon,
                    &[0x0b0d1f, 0x1d2b6b, 0x4b6fd6, 0xf08a5d, 0xffd6a5],
                    0.5,
                    1.0,
                    0.5,
                    0.5,
                ),
                Preset::Ember => (
                    Shape::Rise,
                    &[0x120606, 0x5c1209, 0xc8401b, 0xff9a3c, 0xffe7b0],
                    0.4,
                    1.2,
                    0.7,
                    0.6,
                ),
                Preset::Aurora => (
                    Shape::Pinch,
                    &[0x04121a, 0x0b4f4a, 0x19b38a, 0x8af0c8, 0xe4fff4],
                    0.6,
                    1.0,
                    0.6,
                    0.8,
                ),
                Preset::Eclipse => (
                    Shape::Eclipse,
                    &[0x05050a, 0x2a1840, 0x8a3fbf, 0xff7ad9, 0xfff0fa],
                    0.45,
                    1.0,
                    0.3,
                    0.35,
                ),
                Preset::Mono => (
                    Shape::Horizon,
                    &[0x0a0a0a, 0x555555, 0xf2f2f2],
                    0.55,
                    1.0,
                    0.4,
                    0.5,
                ),
            };
        ascii.shape = shape;
        ascii.colors = colors.iter().map(|&code| rgb(code)).collect();
        ascii.knobs[Knob::Softness as usize] = softness;
        ascii.knobs[Knob::Scale as usize] = scale;
        ascii.knobs[Knob::Flow as usize] = flow;
        ascii.knobs[Knob::Variation as usize] = variation;
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Knob {
    Cell,
    Density,
    GlyphOpacity,
    GradientOpacity,
    Glow,
    Depth,
    Flicker,
    Softness,
    Scale,
    Flow,
    Speed,
    Grain,
    Variation,
    Scramble,
    Intro,
    OriginX,
    OriginY,
    Angle,
    Curve,
}

impl Knob {
    pub const ALL: [Knob; KNOBS] = [
        Knob::Cell,
        Knob::Density,
        Knob::GlyphOpacity,
        Knob::GradientOpacity,
        Knob::Glow,
        Knob::Depth,
        Knob::Flicker,
        Knob::Softness,
        Knob::Scale,
        Knob::Flow,
        Knob::Speed,
        Knob::Grain,
        Knob::Variation,
        Knob::Scramble,
        Knob::Intro,
        Knob::OriginX,
        Knob::OriginY,
        Knob::Angle,
        Knob::Curve,
    ];

    fn spec(self) -> (&'static str, &'static str, f32, f32, f32, f32, Unit) {
        match self {
            Knob::Cell => ("Cell Size", "cell", 8.0, 40.0, 1.0, 18.0, Unit::Px),
            Knob::Density => ("Density", "density", 0.0, 1.0, 0.01, 0.94, Unit::Percent),
            Knob::GlyphOpacity => (
                "Glyph Opacity",
                "glyph_opacity",
                0.0,
                1.0,
                0.01,
                1.0,
                Unit::Percent,
            ),
            Knob::GradientOpacity => (
                "Gradient Opacity",
                "gradient_opacity",
                0.0,
                1.0,
                0.01,
                0.8,
                Unit::Percent,
            ),
            Knob::Glow => ("Glow", "glow", 0.0, 1.0, 0.01, 0.4, Unit::Percent),
            Knob::Depth => (
                "Depth of Field",
                "depth_of_field",
                0.0,
                1.0,
                0.01,
                0.25,
                Unit::Percent,
            ),
            Knob::Flicker => ("Flicker", "flicker", 0.0, 1.0, 0.01, 0.3, Unit::Percent),
            Knob::Softness => ("Softness", "softness", 0.0, 1.0, 0.01, 0.5, Unit::Percent),
            Knob::Scale => ("Size", "scale", 0.25, 3.0, 0.05, 1.0, Unit::Times),
            Knob::Flow => ("Flow", "flow", 0.0, 1.0, 0.01, 0.5, Unit::Percent),
            Knob::Speed => ("Speed", "speed", 0.0, 2.0, 0.05, 0.5, Unit::Times),
            Knob::Grain => ("Grain", "grain", 0.0, 1.0, 0.01, 0.15, Unit::Percent),
            Knob::Variation => ("Variation", "variation", 0.0, 1.0, 0.01, 0.5, Unit::Percent),
            Knob::Scramble => (
                "Scramble Radius",
                "scramble_radius",
                0.0,
                400.0,
                5.0,
                120.0,
                Unit::Px,
            ),
            Knob::Intro => ("Intro", "intro", 0.0, 5.0, 0.1, 1.6, Unit::Seconds),
            Knob::OriginX => ("Origin X", "origin_x", 0.0, 1.0, 0.01, 0.5, Unit::Percent),
            Knob::OriginY => ("Origin Y", "origin_y", 0.0, 1.0, 0.01, 0.5, Unit::Percent),
            Knob::Angle => ("Angle", "angle", 0.0, 360.0, 1.0, 0.0, Unit::Degrees),
            Knob::Curve => (
                "Shape Curve",
                "shape_curve",
                0.0,
                1.0,
                0.01,
                1.0,
                Unit::Percent,
            ),
        }
    }

    pub fn dial(self, ascii: &Ascii) -> Dial {
        let (label, _, least, most, step, _, unit) = self.spec();
        Dial {
            label,
            value: ascii.knob(self),
            least,
            most,
            step,
            unit,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Toggle {
    Mouse,
    Light,
    Underneath,
}

impl Toggle {
    pub const ALL: [Toggle; TOGGLES] = [Toggle::Mouse, Toggle::Light, Toggle::Underneath];

    pub fn name(self) -> &'static str {
        match self {
            Toggle::Mouse => "Mouse Interaction",
            Toggle::Light => "Light Mode",
            Toggle::Underneath => "Image underneath",
        }
    }

    fn key(self) -> &'static str {
        match self {
            Toggle::Mouse => "mouse_interaction",
            Toggle::Light => "light_mode",
            Toggle::Underneath => "image_underneath",
        }
    }

    fn at_start(self) -> bool {
        matches!(self, Toggle::Mouse | Toggle::Underneath)
    }
}

#[derive(Clone, Debug, PartialEq)]
pub struct Ascii {
    pub colors: Vec<Rgb>,
    pub shape: Shape,
    pub click: Click,
    pub charset: Charset,
    pub custom: String,
    knobs: [f32; KNOBS],
    toggles: [bool; TOGGLES],
}

impl Default for Ascii {
    fn default() -> Self {
        let mut ascii = Ascii {
            colors: Vec::new(),
            shape: Shape::Horizon,
            click: Click::Wave,
            charset: Charset::Classic,
            custom: String::new(),
            knobs: Knob::ALL.map(|knob| knob.spec().5),
            toggles: Toggle::ALL.map(Toggle::at_start),
        };
        Preset::Atmosphere.apply(&mut ascii);
        ascii
    }
}

fn rgb(code: u32) -> Rgb {
    [(code >> 16) as u8, (code >> 8) as u8, code as u8]
}

fn hex(color: Rgb) -> String {
    format!("#{:02x}{:02x}{:02x}", color[0], color[1], color[2])
}

fn parse_hex(text: &str) -> Result<Rgb, String> {
    let digits = text
        .strip_prefix('#')
        .filter(|digits| digits.len() == 6)
        .ok_or_else(|| format!("{text} is not a #rrggbb colour"))?;
    let code = u32::from_str_radix(digits, 16).map_err(|_| format!("{text} is not hex"))?;
    Ok(rgb(code))
}

impl Ascii {
    pub fn knob(&self, knob: Knob) -> f32 {
        self.knobs[knob as usize]
    }

    pub fn set_knob(&mut self, knob: Knob, value: f32) {
        let (_, _, least, most, ..) = knob.spec();
        self.knobs[knob as usize] = value.clamp(least, most);
    }

    pub fn toggle(&self, toggle: Toggle) -> bool {
        self.toggles[toggle as usize]
    }

    pub fn set_toggle(&mut self, toggle: Toggle, on: bool) {
        self.toggles[toggle as usize] = on;
    }

    pub fn characters(&self) -> &str {
        self.charset.glyphs().unwrap_or(&self.custom)
    }

    pub fn preset(&self) -> Option<Preset> {
        Preset::ALL.into_iter().find(|preset| {
            let mut probe = self.clone();
            preset.apply(&mut probe);
            probe == *self
        })
    }

    pub fn to_json(&self) -> Value {
        let mut out = Map::new();
        out.insert("shape".into(), json!(self.shape.key()));
        out.insert("click_effect".into(), json!(self.click.key()));
        out.insert("characters".into(), json!(self.charset.key()));
        out.insert("custom_characters".into(), json!(self.custom));
        let colors: Vec<String> = self.colors.iter().copied().map(hex).collect();
        out.insert("colors".into(), json!(colors));
        for knob in Knob::ALL {
            out.insert(knob.spec().1.into(), json!(self.knob(knob)));
        }
        for toggle in Toggle::ALL {
            out.insert(toggle.key().into(), json!(self.toggle(toggle)));
        }
        Value::Object(out)
    }

    pub fn from_json(value: &Value) -> Result<Ascii, String> {
        let mut ascii = Ascii::default();
        let Some(fields) = value.as_object() else {
            return Err(format!("ascii holds {value}, not an object"));
        };
        let text = |key: &str| -> Result<Option<&str>, String> {
            match fields.get(key) {
                None => Ok(None),
                Some(Value::String(text)) => Ok(Some(text)),
                Some(other) => Err(format!("ascii.{key} holds {other}, not text")),
            }
        };
        if let Some(key) = text("shape")? {
            ascii.shape = keyed(&Shape::ALL, Shape::key, key, "shape")?;
        }
        if let Some(key) = text("click_effect")? {
            ascii.click = keyed(&Click::ALL, Click::key, key, "click_effect")?;
        }
        if let Some(key) = text("characters")? {
            ascii.charset = keyed(&Charset::ALL, Charset::key, key, "characters")?;
        }
        if let Some(custom) = text("custom_characters")? {
            ascii.custom = custom.chars().take(MOST_CUSTOM).collect();
        }
        if let Some(colors) = fields.get("colors") {
            let listed = colors
                .as_array()
                .ok_or_else(|| format!("ascii.colors holds {colors}, not a list"))?;
            if !(LEAST_COLORS..=MOST_COLORS).contains(&listed.len()) {
                return Err(format!(
                    "ascii.colors holds {} colours, not {LEAST_COLORS} to {MOST_COLORS}",
                    listed.len()
                ));
            }
            ascii.colors = listed
                .iter()
                .map(|color| {
                    color
                        .as_str()
                        .ok_or_else(|| format!("ascii.colors holds {color}, not text"))
                        .and_then(parse_hex)
                })
                .collect::<Result<_, _>>()?;
        }
        for knob in Knob::ALL {
            let (_, key, least, most, ..) = knob.spec();
            let Some(found) = fields.get(key) else {
                continue;
            };
            let number = found
                .as_f64()
                .ok_or_else(|| format!("ascii.{key} holds {found}, not a number"))?
                as f32;
            if !(least..=most).contains(&number) {
                return Err(format!("ascii.{key} is {number}, not {least} to {most}"));
            }
            ascii.knobs[knob as usize] = number;
        }
        for toggle in Toggle::ALL {
            match fields.get(toggle.key()) {
                None => {}
                Some(Value::Bool(on)) => ascii.toggles[toggle as usize] = *on,
                Some(other) => {
                    return Err(format!(
                        "ascii.{} holds {other}, not a switch",
                        toggle.key()
                    ));
                }
            }
        }
        Ok(ascii)
    }
}

fn keyed<T: Copy>(
    all: &[T],
    key: fn(T) -> &'static str,
    found: &str,
    what: &str,
) -> Result<T, String> {
    all.iter()
        .copied()
        .find(|known| key(*known) == found)
        .ok_or_else(|| format!("ascii.{what} is {found}, which the desk does not know"))
}
