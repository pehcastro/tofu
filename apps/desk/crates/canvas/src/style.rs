use desk_ui::theme::Theme;
use draw_doc::{Element, Look};
use gpui::Rgba;
use serde_json::{Value, json};

use crate::edit;
use crate::paint::Palette;
use crate::shape::{self, Kind};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Hue {
    pub name: &'static str,
    solid: &'static str,
    semi: &'static str,
}

const fn hue(name: &'static str, solid: &'static str, semi: &'static str) -> Hue {
    Hue { name, solid, semi }
}

pub const HUES: [Hue; 12] = [
    hue("Black", "#1d1d1d", "#e8e8e8"),
    hue("Grey", "#9fa8b2", "#eceef0"),
    hue("Light violet", "#e085f4", "#f5eafa"),
    hue("Violet", "#ae3ec9", "#ecdcf2"),
    hue("Blue", "#4465e9", "#dce1f8"),
    hue("Light blue", "#4ba1f1", "#ddedfa"),
    hue("Yellow", "#f1ac4b", "#f9f0e6"),
    hue("Orange", "#e16919", "#f8e2d4"),
    hue("Green", "#099268", "#d3e9e3"),
    hue("Light green", "#4cb05e", "#dbf0e0"),
    hue("Light red", "#f87777", "#f4dadb"),
    hue("Red", "#e03131", "#f4dadb"),
];

pub const PAPER: &str = "#fcfffe";
const SEMI_LIFT: f32 = 0.82;
pub const OPACITIES: [u8; 5] = [10, 25, 50, 75, 100];
const OPACITY_NAMES: [&str; 5] = ["10%", "25%", "50%", "75%", "100%"];

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Fill {
    None,
    Semi,
    Solid,
    Pattern,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Dash {
    Draw,
    Dashed,
    Dotted,
    Solid,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Size {
    S,
    M,
    L,
    Xl,
}

impl Fill {
    pub const ALL: [Fill; 4] = [Fill::None, Fill::Semi, Fill::Solid, Fill::Pattern];

    fn of(element: &Element) -> Fill {
        match shape::word(element, "backgroundColor") {
            None | Some("transparent") => Fill::None,
            Some(color) if color.eq_ignore_ascii_case(PAPER) => Fill::Semi,
            Some(_) => match shape::word(element, "fillStyle") {
                Some("solid") | None => Fill::Solid,
                Some(_) => Fill::Pattern,
            },
        }
    }

    fn name(self) -> &'static str {
        match self {
            Fill::None => "None",
            Fill::Semi => "Semi",
            Fill::Solid => "Solid",
            Fill::Pattern => "Pattern",
        }
    }
}

impl Dash {
    pub const ALL: [Dash; 4] = [Dash::Draw, Dash::Dashed, Dash::Dotted, Dash::Solid];

    pub(crate) fn of(element: &Element) -> Dash {
        match shape::word(element, "strokeStyle") {
            Some("dashed") => Dash::Dashed,
            Some("dotted") => Dash::Dotted,
            _ if shape::number(element, "roughness").unwrap_or(0.0) > 0.0 => Dash::Draw,
            _ => Dash::Solid,
        }
    }

    fn name(self) -> &'static str {
        match self {
            Dash::Draw => "Draw",
            Dash::Dashed => "Dashed",
            Dash::Dotted => "Dotted",
            Dash::Solid => "Solid",
        }
    }
}

impl Size {
    pub const ALL: [Size; 4] = [Size::S, Size::M, Size::L, Size::Xl];

    fn stroke(self) -> f64 {
        match self {
            Size::S => 2.0,
            Size::M => 3.5,
            Size::L => 5.0,
            Size::Xl => 10.0,
        }
    }

    fn text(self) -> f64 {
        match self {
            Size::S => 18.0,
            Size::M => 24.0,
            Size::L => 36.0,
            Size::Xl => 44.0,
        }
    }

    pub fn label(self, host: Kind) -> f64 {
        match (host, self) {
            (Kind::Arrow, Size::S) => 18.0,
            (Kind::Arrow, Size::M) => 20.0,
            (Kind::Arrow, Size::L) => 24.0,
            (Kind::Arrow, Size::Xl) => 28.0,
            (_, Size::S) => 18.0,
            (_, Size::M) => 22.0,
            (_, Size::L) => 26.0,
            (_, Size::Xl) => 32.0,
        }
    }

    fn name(self) -> &'static str {
        match self {
            Size::S => "S",
            Size::M => "M",
            Size::L => "L",
            Size::Xl => "XL",
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Route {
    Curve,
    Elbow,
}

impl Route {
    pub const ALL: [Route; 2] = [Route::Curve, Route::Elbow];
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Pick {
    Color(Hue),
    Tint(u32),
    Opacity(u8),
    Fill(Fill),
    Dash(Dash),
    Size(Size),
    Route(Route),
}

fn put(element: &mut Element, field: &str, value: Value) {
    element.extra.insert(field.to_owned(), value);
}

fn written(color: u32) -> String {
    format!("#{:06x}", color & 0x00ff_ffff)
}

fn lifted(color: u32) -> String {
    let channel = |shift: u32| {
        let value = ((color >> shift) & 0xff) as f32;
        (value + (255.0 - value) * SEMI_LIFT).round() as u32
    };
    written((channel(16) << 16) | (channel(8) << 8) | channel(0))
}

fn semi(element: &Element) -> String {
    if let Some(hue) = HUES.iter().find(|hue| Pick::Color(**hue).on(element)) {
        return hue.semi.to_owned();
    }
    shape::word(element, "strokeColor")
        .and_then(|color| color.strip_prefix('#'))
        .filter(|hex| hex.len() == 6)
        .and_then(|hex| u32::from_str_radix(hex, 16).ok())
        .map_or_else(|| HUES[0].semi.to_owned(), lifted)
}

fn stroked(element: &mut Element, stroke: String) {
    put(element, "strokeColor", json!(stroke));
    if let Fill::Solid | Fill::Pattern = Fill::of(element) {
        put(element, "backgroundColor", json!(semi(element)));
    }
}

impl Pick {
    pub fn name(self) -> &'static str {
        match self {
            Pick::Color(hue) => hue.name,
            Pick::Tint(_) => "Custom",
            Pick::Opacity(percent) => OPACITIES
                .iter()
                .position(|stop| *stop == percent)
                .and_then(|at| OPACITY_NAMES.get(at))
                .copied()
                .unwrap_or("100%"),
            Pick::Fill(fill) => fill.name(),
            Pick::Dash(dash) => dash.name(),
            Pick::Size(size) => size.name(),
            Pick::Route(Route::Curve) => "Curve",
            Pick::Route(Route::Elbow) => "Elbow",
        }
    }

    pub fn apply(self, element: &mut Element) {
        match self {
            Pick::Color(hue) => stroked(element, hue.solid.to_owned()),
            Pick::Tint(color) => stroked(element, written(color)),
            Pick::Opacity(percent) => put(element, "opacity", json!(percent)),
            Pick::Fill(fill) => {
                let tint = semi(element);
                let (background, style) = match fill {
                    Fill::None => ("transparent", "solid"),
                    Fill::Semi => (PAPER, "solid"),
                    Fill::Solid => (tint.as_str(), "solid"),
                    Fill::Pattern => (tint.as_str(), "hachure"),
                };
                put(element, "backgroundColor", json!(background));
                put(element, "fillStyle", json!(style));
            }
            Pick::Dash(dash) => {
                let (style, roughness) = match dash {
                    Dash::Draw => ("solid", 1),
                    Dash::Dashed => ("dashed", 0),
                    Dash::Dotted => ("dotted", 0),
                    Dash::Solid => ("solid", 0),
                };
                put(element, "strokeStyle", json!(style));
                put(element, "roughness", json!(roughness));
            }
            Pick::Size(size) => match (Kind::of(element), shape::container(element)) {
                (Kind::Text, None) => put(element, "fontSize", json!(size.text())),
                (Kind::Text, Some(_)) => put(element, "fontSize", json!(size.label(Kind::Other))),
                _ => put(element, "strokeWidth", json!(size.stroke())),
            },
            Pick::Route(route) if Kind::of(element) == Kind::Arrow && !self.on(element) => {
                let elbowed = route == Route::Elbow;
                put(element, "elbowed", json!(elbowed));
                let roundness = match route {
                    Route::Curve => json!({ "type": 2 }),
                    Route::Elbow => Value::Null,
                };
                put(element, "roundness", roundness);
                if let [start, .., end] = shape::trace(element).as_slice() {
                    let routed = edit::routed(element, &[*start, *end], *start, *end);
                    *element = shape::with_trace(element, &routed);
                }
            }
            Pick::Route(_) => {}
        }
    }

    pub fn on(self, element: &Element) -> bool {
        match self {
            Pick::Color(hue) => shape::word(element, "strokeColor")
                .is_some_and(|had| had.eq_ignore_ascii_case(hue.solid)),
            Pick::Tint(color) => shape::word(element, "strokeColor")
                .is_some_and(|had| had.eq_ignore_ascii_case(&written(color))),
            Pick::Opacity(percent) => {
                shape::number(element, "opacity").unwrap_or(100.0) == f64::from(percent)
            }
            Pick::Fill(fill) => Fill::of(element) == fill,
            Pick::Dash(dash) => Dash::of(element) == dash,
            Pick::Size(size) => match Kind::of(element) {
                Kind::Text => {
                    [size.text(), size.label(Kind::Other)].contains(&shape::font_size(element))
                }
                _ => shape::number(element, "strokeWidth") == Some(size.stroke()),
            },
            Pick::Route(route) => {
                Kind::of(element) == Kind::Arrow
                    && edit::elbowed(element) == (route == Route::Elbow)
            }
        }
    }

    pub fn swatch(self, theme: &Theme) -> Option<Rgba> {
        match self {
            Pick::Color(hue) => Palette::new(theme, None).color(Some(hue.solid)),
            Pick::Tint(color) => Some(gpui::rgb(color)),
            Pick::Opacity(_) | Pick::Fill(_) | Pick::Dash(_) | Pick::Size(_) | Pick::Route(_) => {
                None
            }
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Style {
    color: Pick,
    opacity: u8,
    fill: Fill,
    dash: Dash,
    size: Size,
    route: Route,
}

pub const DEFAULT: Style = Style {
    color: Pick::Color(HUES[0]),
    opacity: 100,
    fill: Fill::None,
    dash: Dash::Draw,
    size: Size::M,
    route: Route::Curve,
};

impl Style {
    pub fn picks(self) -> [Pick; 6] {
        [
            self.color,
            Pick::Opacity(self.opacity),
            Pick::Fill(self.fill),
            Pick::Dash(self.dash),
            Pick::Size(self.size),
            Pick::Route(self.route),
        ]
    }

    pub fn has(self, pick: Pick) -> bool {
        self.picks().contains(&pick)
    }

    pub fn opacity(self) -> u8 {
        self.opacity
    }

    pub fn set(&mut self, pick: Pick) {
        match pick {
            Pick::Color(_) | Pick::Tint(_) => self.color = pick,
            Pick::Opacity(percent) => self.opacity = percent,
            Pick::Fill(fill) => self.fill = fill,
            Pick::Dash(dash) => self.dash = dash,
            Pick::Size(size) => self.size = size,
            Pick::Route(route) => self.route = route,
        }
    }
}

#[derive(Clone, Debug, PartialEq)]
pub struct Backdrop {
    pub color: Option<String>,
    pub dots: bool,
    pub look: Look,
}

pub const BLANK: Backdrop = Backdrop {
    color: None,
    dots: true,
    look: Look::Sketch,
};

pub const GROUNDS: [Option<&str>; 6] = [
    None,
    Some("#ffffff"),
    Some("#f8f9fa"),
    Some("#f5faff"),
    Some("#fffce8"),
    Some("#fdf8f6"),
];

pub fn ground(color: Option<&str>, theme: &Theme) -> Rgba {
    Palette::new(theme, color).ground
}
