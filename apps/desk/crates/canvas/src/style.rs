use desk_ui::theme::Theme;
use draw_doc::Element;
use gpui::Rgba;
use serde_json::{Value, json};

use crate::paint::Palette;
use crate::shape;

#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Pick {
    Stroke(&'static str),
    Background(&'static str),
    Fill(&'static str),
    Width(u8),
    Line(&'static str),
}

pub const STROKES: [&str; 5] = ["#1e1e1e", "#e03131", "#2f9e44", "#1971c2", "#f08c00"];
pub const BACKGROUNDS: [&str; 5] = ["transparent", "#ffc9c9", "#b2f2bb", "#a5d8ff", "#ffec99"];
pub const FILLS: [&str; 2] = ["solid", "hachure"];
pub const WIDTHS: [u8; 3] = [1, 2, 4];
pub const LINES: [&str; 3] = ["solid", "dashed", "dotted"];

pub type Style = [Pick; 5];

pub const DEFAULT: Style = [
    Pick::Stroke("#1e1e1e"),
    Pick::Background("transparent"),
    Pick::Fill("solid"),
    Pick::Width(2),
    Pick::Line("solid"),
];

impl Pick {
    pub fn field(self) -> &'static str {
        match self {
            Pick::Stroke(_) => "strokeColor",
            Pick::Background(_) => "backgroundColor",
            Pick::Fill(_) => "fillStyle",
            Pick::Width(_) => "strokeWidth",
            Pick::Line(_) => "strokeStyle",
        }
    }

    pub fn value(self) -> Value {
        match self {
            Pick::Stroke(word) | Pick::Background(word) | Pick::Fill(word) | Pick::Line(word) => {
                json!(word)
            }
            Pick::Width(width) => json!(width),
        }
    }

    pub fn slot(self) -> usize {
        match self {
            Pick::Stroke(_) => 0,
            Pick::Background(_) => 1,
            Pick::Fill(_) => 2,
            Pick::Width(_) => 3,
            Pick::Line(_) => 4,
        }
    }

    pub fn on(self, element: &Element) -> bool {
        match self {
            Pick::Width(width) => shape::number(element, self.field()) == Some(f64::from(width)),
            Pick::Stroke(word) | Pick::Background(word) | Pick::Fill(word) | Pick::Line(word) => {
                shape::word(element, self.field()).is_some_and(|had| had.eq_ignore_ascii_case(word))
            }
        }
    }

    pub fn swatch(self, theme: &Theme) -> Option<Rgba> {
        match self {
            Pick::Stroke(word) | Pick::Background(word) => Palette::new(theme).color(Some(word)),
            Pick::Fill(_) | Pick::Width(_) | Pick::Line(_) => None,
        }
    }
}
