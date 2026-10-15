use std::fmt;
use std::rc::Rc;

use desk_markdown::{
    Component, Json, PropError, PropKind, PropSpec, PropValue, Registry, validate,
};
use gpui::{
    App, Div, IntoElement, SharedString, UniformListScrollHandle, Window, div, prelude::*, px,
};

use crate::components::card::{caption, inner_card};
use crate::components::charts::{
    BarLayout, BarLook, Bars, Donut, Said, Series, Slice, Sparkline, hues,
};
use crate::components::chip::file_chip;
use crate::components::diff::{DiffCard, FileChange, FileDiff, PatchError};
use crate::components::glyph::Glyph;
use crate::components::list::owned_table;
use crate::components::paint::{glyph, ink, ring, tint};
use crate::components::size::{
    CALLS_PAD_X, CALLS_PAD_Y, CAPTION_TEXT, FAIL_RING, FAIL_TINT, GRID_ROW,
};
use crate::metrics::ICON_SMALL;
use crate::theme::{ColorToken, NumberToken, Theme};

const CHART_HEIGHT: f32 = 160.0;
const SPARK_HEIGHT: f32 = 48.0;
const DONUT_HOLE: f32 = 0.62;

#[derive(Clone)]
pub enum Tone {
    Info,
    Warn,
    Error,
}

#[derive(Clone)]
pub enum Chart {
    Line(Vec<u32>, Vec<Said>),
    Bars(Vec<Series>, Vec<SharedString>, Vec<Said>),
    Donut(Vec<Slice>, Vec<Said>),
}

#[derive(Clone)]
pub enum Rich {
    Card(Option<SharedString>),
    Callout(Tone),
    FileLink(SharedString),
    Diff(Rc<FileDiff>),
    Table(Vec<SharedString>, Rc<[Vec<SharedString>]>),
    Chart(Chart),
}

pub enum Refused {
    Prop(PropError),
    Tone(String),
    Line(f64),
    Patch(PatchError),
    ChartKind(String),
    Shape(&'static str),
}

impl fmt::Display for Refused {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Prop(PropError::UnknownComponent(name)) => write!(f, "UnknownComponent {name}"),
            Self::Prop(error) => write!(f, "{error}"),
            Self::Tone(kind) => write!(f, "prop kind is {kind:?}, not info, warn or error"),
            Self::Line(line) => write!(f, "prop line is {line}, not a line number"),
            Self::Patch(error) => write!(f, "prop patch: {error}"),
            Self::ChartKind(kind) => write!(f, "prop kind is {kind:?}, not line, bars or donut"),
            Self::Shape(why) => write!(f, "{why}"),
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
    registry.define(
        "Diff",
        vec![
            prop("path", PropKind::Text, true),
            prop("patch", PropKind::Text, true),
        ],
    );
    registry.define(
        "Table",
        vec![
            prop("columns", PropKind::Array, true),
            prop("rows", PropKind::Array, true),
        ],
    );
    registry.define(
        "Chart",
        vec![
            prop("kind", PropKind::Text, true),
            prop("series", PropKind::Array, true),
            prop("labels", PropKind::Array, false),
        ],
    );
    registry
}

fn cell(json: &Json) -> Result<SharedString, Refused> {
    match json {
        Json::String(text) => Ok(text.clone().into()),
        Json::Number(number) => Ok(number.to_string().into()),
        Json::Bool(flag) => Ok(flag.to_string().into()),
        _ => Err(Refused::Shape("a cell is text, a number or a bool")),
    }
}

fn number(json: &Json) -> Result<f64, Refused> {
    match json {
        Json::Number(number) if number.is_finite() && *number >= 0.0 => Ok(*number),
        _ => Err(Refused::Shape("prop series holds numbers at or above 0")),
    }
}

fn chart(kind: &str, series: &[Json], labels: Vec<SharedString>) -> Result<Chart, Refused> {
    let values = series.iter().map(number).collect::<Result<Vec<_>, _>>()?;
    if values.is_empty() {
        return Err(Refused::Shape("prop series is empty"));
    }
    let label = |at: usize| labels.get(at).cloned().unwrap_or_default();
    let tips = values
        .iter()
        .enumerate()
        .map(|(at, value)| Said::new(value.to_string(), label(at)))
        .collect();
    match kind {
        "line" => {
            let whole = values
                .iter()
                .map(
                    |value| match value.fract() == 0.0 && *value <= f64::from(u32::MAX) {
                        true => Ok(*value as u32),
                        false => Err(Refused::Shape("a line chart takes whole numbers")),
                    },
                )
                .collect::<Result<Vec<_>, _>>()?;
            Ok(Chart::Line(whole, tips))
        }
        "bars" => {
            let series = Series {
                name: "series".into(),
                color: hues(["series"]).first().copied().unwrap_or_default(),
                values: values.iter().map(|value| *value as f32).collect(),
            };
            Ok(Chart::Bars(vec![series], labels, tips))
        }
        "donut" => {
            if labels.len() != values.len() {
                return Err(Refused::Shape("a donut takes one label per value"));
            }
            let colors = hues(labels.iter().map(SharedString::as_ref));
            let slices = labels
                .iter()
                .zip(&values)
                .zip(colors)
                .map(|((name, value), color)| Slice {
                    name: name.clone(),
                    value: *value as f32,
                    color,
                })
                .collect();
            Ok(Chart::Donut(slices, tips))
        }
        other => Err(Refused::ChartKind(other.to_owned())),
    }
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
    let array = |name: &str| match prop(name) {
        Some(PropValue::Json(Json::Array(items))) => items.as_slice(),
        _ => &[],
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
        "Diff" => FileDiff::parse(
            text("path").unwrap_or_default(),
            FileChange::Modified,
            &text("patch").unwrap_or_default(),
            |_| Vec::new(),
        )
        .map(|diff| Rich::Diff(Rc::new(diff)))
        .map_err(Refused::Patch),
        "Table" => {
            let columns = array("columns")
                .iter()
                .map(cell)
                .collect::<Result<Vec<_>, _>>()?;
            let rows = array("rows")
                .iter()
                .map(|row| match row {
                    Json::Array(cells) if cells.len() == columns.len() => {
                        cells.iter().map(cell).collect::<Result<Vec<_>, _>>()
                    }
                    _ => Err(Refused::Shape(
                        "each row is an array with one cell per column",
                    )),
                })
                .collect::<Result<Vec<_>, _>>()?;
            Ok(Rich::Table(columns, rows.into()))
        }
        "Chart" => {
            let labels = array("labels")
                .iter()
                .map(cell)
                .collect::<Result<Vec<_>, _>>()?;
            chart(&text("kind").unwrap_or_default(), array("series"), labels).map(Rich::Chart)
        }
        name => Err(Refused::Prop(PropError::UnknownComponent(name.to_owned()))),
    }
}

#[derive(IntoElement)]
struct ChartTag {
    key: usize,
    chart: Chart,
}

impl RenderOnce for ChartTag {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let key = ("rich-chart", self.key);
        match self.chart {
            Chart::Line(values, tips) => div()
                .h(px(SPARK_HEIGHT))
                .w_full()
                .child(window.use_keyed_state(key, cx, |_, _| Sparkline::new(values, tips))),
            Chart::Bars(series, labels, tips) => {
                div()
                    .h(px(CHART_HEIGHT))
                    .w_full()
                    .child(window.use_keyed_state(key, cx, |_, _| {
                        Bars::new(series, labels, tips, BarLook::Plain, BarLayout::Columns)
                    }))
            }
            Chart::Donut(slices, tips) => div().size(px(CHART_HEIGHT)).child(
                window.use_keyed_state(key, cx, |_, _| Donut::new(slices, DONUT_HOLE, None, tips)),
            ),
        }
    }
}

pub fn frame(rich: &Rich, children: Vec<Div>, key: usize, theme: &Theme) -> Div {
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
        Rich::Diff(diff) => div().child(DiffCard::new(("rich-diff", key), diff.clone())),
        Rich::Table(columns, rows) => {
            let cells = rows.clone();
            div()
                .h(px(GRID_ROW * (rows.len() + 1) as f32))
                .child(owned_table(
                    format!("rich-table-{key}").into(),
                    columns.clone(),
                    rows.len(),
                    &UniformListScrollHandle::new(),
                    theme,
                    move |row, column| {
                        cells
                            .get(row)
                            .and_then(|row| row.get(column))
                            .cloned()
                            .unwrap_or_default()
                    },
                ))
        }
        Rich::Chart(chart) => div().child(ChartTag {
            key,
            chart: chart.clone(),
        }),
    }
}
