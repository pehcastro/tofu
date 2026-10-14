use std::fmt;

use desk_markdown::{
    Component, Json, PropError, PropKind, PropSpec, PropValue, Registry, validate,
};
use gpui::{Div, SharedString, div, prelude::*, px};

use crate::components::card::{caption, inner_card};
use crate::components::chip::file_chip;
use crate::components::glyph::Glyph;
use crate::components::paint::{glyph, ink, ring, tint};
use crate::components::size::{CALLS_PAD_X, CALLS_PAD_Y, CAPTION_TEXT, FAIL_RING, FAIL_TINT};
use crate::metrics::ICON_SMALL;
use crate::theme::{ColorToken, NumberToken, Theme};

pub enum Tone {
    Info,
    Warn,
    Error,
}

pub enum Rich {
    Card(Option<SharedString>),
    Callout(Tone),
    FileLink(SharedString),
}

pub enum Refused {
    Prop(PropError),
    Tone(String),
    Line(f64),
}

impl fmt::Display for Refused {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Prop(PropError::UnknownComponent(name)) => write!(f, "UnknownComponent {name}"),
            Self::Prop(error) => write!(f, "{error}"),
            Self::Tone(kind) => write!(f, "prop kind is {kind:?}, not info, warn or error"),
            Self::Line(line) => write!(f, "prop line is {line}, not a line number"),
        }
    }
}

pub fn registry() -> Registry {
    let prop = |name: &str, kind, required| PropSpec {
        name: name.to_owned(),
        kind,
        required,
    };
    let mut registry = Registry::default();
    registry.define("Card", vec![prop("title", PropKind::Text, false)]);
    registry.define("Callout", vec![prop("kind", PropKind::Text, true)]);
    registry.define(
        "FileLink",
        vec![
            prop("path", PropKind::Text, true),
            prop("line", PropKind::Number, false),
        ],
    );
    registry
}

pub fn read<C>(component: &Component<C>, registry: &Registry) -> Result<Rich, Refused> {
    validate(component, registry).map_err(Refused::Prop)?;
    let prop = |name: &str| {
        component
            .props
            .iter()
            .find(|(key, _)| key == name)
            .map(|(_, value)| value)
    };
    let text = |name: &str| match prop(name) {
        Some(PropValue::Text(text) | PropValue::Json(Json::String(text))) => Some(text.clone()),
        _ => None,
    };
    match component.name.as_str() {
        "Card" => Ok(Rich::Card(text("title").map(SharedString::from))),
        "Callout" => match text("kind").unwrap_or_default().as_str() {
            "info" => Ok(Rich::Callout(Tone::Info)),
            "warn" => Ok(Rich::Callout(Tone::Warn)),
            "error" => Ok(Rich::Callout(Tone::Error)),
            other => Err(Refused::Tone(other.to_owned())),
        },
        "FileLink" => {
            let path = text("path").unwrap_or_default();
            match prop("line") {
                Some(PropValue::Json(Json::Number(line))) => {
                    match line.fract() == 0.0 && (1.0..=f64::from(u32::MAX)).contains(line) {
                        true => Ok(Rich::FileLink(format!("{path}:{line}").into())),
                        false => Err(Refused::Line(*line)),
                    }
                }
                _ => Ok(Rich::FileLink(path.into())),
            }
        }
        name => Err(Refused::Prop(PropError::UnknownComponent(name.to_owned()))),
    }
}

pub fn frame(rich: &Rich, children: Vec<Div>, theme: &Theme) -> Div {
    match rich {
        Rich::Card(title) => div().flex().child(
            inner_card(theme)
                .px(px(CALLS_PAD_X))
                .py(px(CALLS_PAD_Y))
                .gap_2()
                .children(title.clone().map(|title| caption(title, theme)))
                .children(children),
        ),
        Rich::Callout(tone) => {
            let (color, name) = match tone {
                Tone::Info => (theme.color(ColorToken::StatusAccent), "info"),
                Tone::Warn => (theme.color(ColorToken::StatusWarn), "warn"),
                Tone::Error => (theme.color(ColorToken::StatusDanger), "error"),
            };
            div()
                .flex()
                .flex_col()
                .gap_2()
                .px(px(CALLS_PAD_X))
                .py(px(CALLS_PAD_Y))
                .rounded(px(theme.number(NumberToken::CardsInnerRadius)))
                .bg(tint(color, FAIL_TINT))
                .shadow(vec![ring(tint(color, FAIL_RING))])
                .child(caption(name, theme).text_color(color))
                .children(children)
        }
        Rich::FileLink(label) => div().flex().child(file_chip(
            glyph(Glyph::File, ICON_SMALL, ink(theme, CAPTION_TEXT)),
            label.clone(),
            theme,
        )),
    }
}
