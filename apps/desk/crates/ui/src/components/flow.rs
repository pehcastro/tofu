use std::cell::Cell;
use std::time::{Duration, Instant};

use draw_doc::{BoundKind, Element, Look, Scene, flow, flow_scene};
use gpui::{
    App, BorderStyle, Bounds, Corners, Edges, Font, FontWeight, PathBuilder, Pixels, Point, Rgba,
    SharedString, TextAlign, TextRun, TransformationMatrix, Window, point, px, quad, rgb_to_hsla,
    size, transparent_black,
};
use serde_json::Value;

use crate::components::glyph::Glyph;
use crate::components::paint::{ink, tint};
use crate::components::size::{FONT_BODY, FONT_SMALL, RADIUS_ROW, T2, T3};
use crate::theme::{ColorToken, Theme};

const CARD_PAD: f32 = 12.0;
const ICON: f32 = 16.0;
const ICON_GAP: f32 = 8.0;
const LABEL: f32 = FONT_BODY;
const PILL_TEXT: f32 = FONT_SMALL;
const PILL_PAD: (f32, f32) = (8.0, 3.0);
const CAPTION: f32 = 12.0;
const EDGE: f32 = 1.5;
const ELBOW: f32 = 8.0;
const HEAD: f32 = 10.0;
const HEAD_LEAST: f32 = 6.0;
const HEAD_SPREAD: f32 = 0.45;
const DASH: f32 = 4.0;
const BORDER: f32 = 0.14;
const PILL_FILL: f32 = 0.06;
const SHARPEST: f32 = 0.0001;

pub struct Ink {
    card: Rgba,
    border: Rgba,
    text: Rgba,
    icon: Rgba,
    line: Rgba,
    pill: Rgba,
    muted: Rgba,
}

impl Ink {
    pub fn of(theme: &Theme) -> Ink {
        Ink {
            card: theme.color(ColorToken::ToastFill),
            border: ink(theme, BORDER),
            text: theme.color(ColorToken::TextStrong),
            icon: theme.color(ColorToken::TextIcon),
            line: ink(theme, T3),
            pill: theme.color(ColorToken::CardsOuterFill),
            muted: ink(theme, T2),
        }
    }
}

pub fn glyph_named(name: &str) -> Option<Glyph> {
    Some(match name {
        "file" | "doc" | "license" => Glyph::File,
        "folder" => Glyph::Folder,
        "check" | "done" | "ok" => Glyph::Check,
        "mail" | "email" | "inbox" => Glyph::Inbox,
        "help" | "question" | "decide" | "warn" | "attention" => Glyph::Attention,
        "play" | "start" | "rocket" => Glyph::Rocket,
        "search" | "filter" => Glyph::Filter,
        "eye" | "review" | "reveal" => Glyph::Reveal,
        "person" | "user" => Glyph::Person,
        "code" => Glyph::Code,
        "terminal" | "shell" => Glyph::Terminal,
        "chat" | "talk" => Glyph::Talk,
        "globe" | "web" => Glyph::Globe,
        "database" | "db" => Glyph::Database,
        "lock" => Glyph::Lock,
        "link" => Glyph::Link,
        "clock" | "cron" | "wait" => Glyph::Cron,
        "star" => Glyph::Star,
        "sparkle" => Glyph::Sparkle,
        "bug" => Glyph::Bug,
        "book" => Glyph::Book,
        "branch" => Glyph::Branch,
        "pencil" | "edit" => Glyph::Pencil,
        "trash" | "delete" => Glyph::Trash,
        "image" => Glyph::Image,
        "agent" | "agents" => Glyph::Agents,
        "brain" => Glyph::Brain,
        _ => return None,
    })
}

pub enum Part<'a> {
    Card {
        label: &'a str,
        icon: Option<Glyph>,
    },
    Route {
        dashed: bool,
        label: Option<&'a str>,
    },
    Frame,
    Dot,
    Caption(&'a str),
    Hidden,
    Other,
}

fn word<'a>(element: &'a Element, field: &str) -> Option<&'a str> {
    element.extra.get(field).and_then(Value::as_str)
}

fn bound_text<'a>(element: &Element, scene: &'a Scene) -> Option<&'a str> {
    let bound = element.bound_elements().ok()?;
    let text = bound.iter().find(|bound| bound.kind == BoundKind::Text)?;
    word(scene.get(&text.id)?, "text")
}

pub fn part<'a>(element: &'a Element, scene: &'a Scene) -> Part<'a> {
    let data = element.extra.get("customData");
    let keyed = data.and_then(|data| data.get("key")).is_some();
    match element.kind.as_str() {
        "rectangle" if keyed => Part::Card {
            label: bound_text(element, scene).unwrap_or_default(),
            icon: data
                .and_then(|data| data.get("icon"))
                .and_then(Value::as_str)
                .and_then(glyph_named),
        },
        "rectangle" if word(element, "strokeStyle") == Some("dashed") => Part::Frame,
        "arrow" => Part::Route {
            dashed: matches!(word(element, "strokeStyle"), Some("dashed" | "dotted")),
            label: bound_text(element, scene),
        },
        "ellipse" if element.width <= 16.0 => Part::Dot,
        "text" => match word(element, "containerId").and_then(|id| scene.get(id)) {
            None => Part::Caption(word(element, "text").unwrap_or_default()),
            Some(host) => match part(host, scene) {
                Part::Card { .. } | Part::Route { .. } => Part::Hidden,
                _ => Part::Other,
            },
        },
        _ => Part::Other,
    }
}

pub fn trace(element: &Element) -> Vec<(f64, f64)> {
    element
        .points
        .iter()
        .flatten()
        .map(|[x, y]| (element.x + x, element.y + y))
        .collect()
}

fn text(
    said: &str,
    at: Point<Pixels>,
    font_size: Pixels,
    color: Rgba,
    window: &mut Window,
    cx: &mut App,
) -> Pixels {
    let run = TextRun {
        len: said.len(),
        font: Font {
            weight: FontWeight::MEDIUM,
            ..window.text_style().font()
        },
        color: rgb_to_hsla(color),
        background_color: None,
        underline: None,
        strikethrough: None,
        letter_spacing: None,
    };
    let shaped = window.text_system().shape_line(
        SharedString::from(said.to_owned()),
        font_size,
        &[run],
        None,
    );
    let width = shaped.width();
    let top = point(at.x, at.y - font_size * 0.65);
    if let Err(error) = shaped.paint(top, font_size * 1.3, TextAlign::Left, None, window, cx) {
        eprintln!("desk: flow text failed: {error}");
    }
    width
}

fn measure(said: &str, font_size: Pixels, window: &Window) -> Pixels {
    let run = TextRun {
        len: said.len(),
        font: window.text_style().font(),
        color: transparent_black(),
        background_color: None,
        underline: None,
        strikethrough: None,
        letter_spacing: None,
    };
    window
        .text_system()
        .shape_line(SharedString::from(said.to_owned()), font_size, &[run], None)
        .width()
}

pub fn card(
    area: Bounds<Pixels>,
    zoom: f32,
    label: &str,
    icon: Option<Glyph>,
    ink: &Ink,
    window: &mut Window,
    cx: &mut App,
) {
    window.paint_quad(quad(
        area,
        Corners::all(px(RADIUS_ROW * zoom)),
        ink.card,
        Edges::all(px(1.0)),
        ink.border,
        BorderStyle::Solid,
    ));
    let middle = area.origin.y + area.size.height / 2.0;
    let mut left = area.origin.x + px(CARD_PAD * zoom);
    if let Some(icon) = icon {
        let side = px(ICON * zoom);
        let place = Bounds::new(point(left, middle - side / 2.0), size(side, side));
        if let Err(error) = window.paint_svg(
            place,
            icon.path().into(),
            None,
            TransformationMatrix::unit(),
            rgb_to_hsla(ink.icon),
            cx,
        ) {
            eprintln!("desk: flow icon failed: {error}");
        }
        left += side + px(ICON_GAP * zoom);
    }
    text(
        label,
        point(left, middle),
        px(LABEL * zoom),
        ink.text,
        window,
        cx,
    );
}

pub fn pill(
    middle: Point<Pixels>,
    said: &str,
    zoom: f32,
    ink: &Ink,
    window: &mut Window,
    cx: &mut App,
) {
    let font_size = px(PILL_TEXT * zoom);
    let wide = measure(said, font_size, window);
    let (pad_x, pad_y) = (px(PILL_PAD.0 * zoom), px(PILL_PAD.1 * zoom));
    let tall = font_size * 1.3 + pad_y * 2.0;
    let area = Bounds::new(
        point(middle.x - wide / 2.0 - pad_x, middle.y - tall / 2.0),
        size(wide + pad_x * 2.0, tall),
    );
    window.paint_quad(quad(
        area,
        Corners::all(tall / 2.0),
        solid_over(ink),
        Edges::all(px(1.0)),
        ink.border,
        BorderStyle::Solid,
    ));
    text(
        said,
        point(middle.x - wide / 2.0, middle.y),
        font_size,
        ink.muted,
        window,
        cx,
    );
}

fn solid_over(ink: &Ink) -> Rgba {
    let over = tint(ink.text, PILL_FILL);
    crate::components::paint::solid(over, ink.pill)
}

pub fn middle(points: &[Point<Pixels>]) -> Option<Point<Pixels>> {
    let a = points.get(points.len().checked_sub(1)? / 2)?;
    let b = points.get(points.len() / 2)?;
    Some(point((a.x + b.x) / 2.0, (a.y + b.y) / 2.0))
}

pub fn route(points: &[Point<Pixels>], zoom: f32, dashed: bool, ink: &Ink, window: &mut Window) {
    let [first, .., last] = points else {
        return;
    };
    let width = px(EDGE * zoom.max(0.5));
    let mut path = match dashed {
        true => PathBuilder::stroke(width).dash_array(&[px(DASH * zoom), px(DASH * zoom)]),
        false => PathBuilder::stroke(width),
    };
    path.move_to(*first);
    for corner in points.windows(3) {
        let [before, at, after] = corner else {
            continue;
        };
        let reach = |towards: Point<Pixels>| {
            let (dx, dy) = (f32::from(towards.x - at.x), f32::from(towards.y - at.y));
            let length = dx.hypot(dy).max(SHARPEST);
            let cut = (ELBOW * zoom).min(length / 2.0);
            point(at.x + px(dx / length * cut), at.y + px(dy / length * cut))
        };
        path.line_to(reach(*before));
        path.curve_to(reach(*after), *at);
    }
    let from = points.iter().rev().nth(1).unwrap_or(first);
    let (dx, dy) = (f32::from(from.x - last.x), f32::from(from.y - last.y));
    let length = dx.hypot(dy).max(SHARPEST);
    let (ux, uy) = (dx / length, dy / length);
    let reach = (HEAD * zoom).max(HEAD_LEAST).min(length);
    let base = reach * HEAD_SPREAD.cos();
    path.line_to(point(last.x + px(ux * base), last.y + px(uy * base)));
    if let Ok(path) = path.build() {
        window.paint_path(path, ink.line);
    }
    let wing = |turn: f32| {
        let (sin, cos) = turn.sin_cos();
        point(
            last.x + px((ux * cos - uy * sin) * reach),
            last.y + px((ux * sin + uy * cos) * reach),
        )
    };
    let mut head = PathBuilder::fill();
    head.move_to(wing(HEAD_SPREAD));
    head.line_to(*last);
    head.line_to(wing(-HEAD_SPREAD));
    head.close();
    if let Ok(head) = head.build() {
        window.paint_path(head, ink.line);
    }
}

pub fn frame(area: Bounds<Pixels>, zoom: f32, ink: &Ink, window: &mut Window) {
    window.paint_quad(quad(
        area,
        Corners::all(px(RADIUS_ROW * zoom * 1.5)),
        transparent_black(),
        Edges::all(px(1.0)),
        ink.line,
        BorderStyle::Dashed,
    ));
}

pub fn dot(area: Bounds<Pixels>, ink: &Ink, window: &mut Window) {
    window.paint_quad(quad(
        area,
        Corners::all(area.size.width / 2.0),
        ink.line,
        Edges::default(),
        transparent_black(),
        BorderStyle::Solid,
    ));
}

pub fn caption(
    at: Point<Pixels>,
    said: &str,
    zoom: f32,
    ink: &Ink,
    window: &mut Window,
    cx: &mut App,
) {
    let font_size = px(CAPTION * zoom);
    text(
        said,
        point(at.x, at.y + font_size * 0.65),
        font_size,
        ink.muted,
        window,
        cx,
    );
}

pub struct FlowBlock {
    pub source: SharedString,
    scene: Scene,
    nodes: usize,
    span: (f64, f64, f64, f64),
    laid: Duration,
    logged: Cell<bool>,
}

impl FlowBlock {
    pub fn read(source: &str) -> Result<FlowBlock, String> {
        let started = Instant::now();
        let parsed = flow(source).map_err(|error| error.to_string())?;
        let scene = flow_scene(&parsed, Look::Ui).map_err(|error| error.to_string())?;
        let span = scene.elements().iter().fold(
            (
                f64::INFINITY,
                f64::INFINITY,
                f64::NEG_INFINITY,
                f64::NEG_INFINITY,
            ),
            |(left, top, right, bottom), element| {
                let seen = element.bounds();
                (
                    left.min(seen.min_x),
                    top.min(seen.min_y),
                    right.max(seen.max_x),
                    bottom.max(seen.max_y),
                )
            },
        );
        Ok(FlowBlock {
            source: source.to_owned().into(),
            nodes: parsed.graph.nodes.len(),
            scene,
            span,
            laid: started.elapsed(),
            logged: Cell::new(false),
        })
    }

    pub fn size(&self, zoom: f32) -> (f32, f32) {
        let (left, top, right, bottom) = self.span;
        (
            ((right - left) as f32) * zoom,
            ((bottom - top) as f32) * zoom,
        )
    }

    pub fn paint(
        &self,
        origin: Point<Pixels>,
        zoom: f32,
        theme: &Theme,
        window: &mut Window,
        cx: &mut App,
    ) {
        let started = Instant::now();
        let ink = Ink::of(theme);
        let (left, top, ..) = self.span;
        let at = |(x, y): (f64, f64)| {
            point(
                origin.x + px(((x - left) as f32) * zoom),
                origin.y + px(((y - top) as f32) * zoom),
            )
        };
        let area = |element: &Element| {
            let seen = element.bounds();
            Bounds::from_corners(at((seen.min_x, seen.min_y)), at((seen.max_x, seen.max_y)))
        };
        let mut pills = Vec::new();
        for element in self.scene.elements() {
            match part(element, &self.scene) {
                Part::Card { label, icon } => {
                    card(area(element), zoom, label, icon, &ink, window, cx)
                }
                Part::Route { dashed, label } => {
                    let points: Vec<_> = trace(element).into_iter().map(at).collect();
                    route(&points, zoom, dashed, &ink, window);
                    if let (Some(label), Some(middle)) = (label, middle(&points)) {
                        pills.push((middle, label));
                    }
                }
                Part::Frame => frame(area(element), zoom, &ink, window),
                Part::Dot => dot(area(element), &ink, window),
                Part::Caption(said) => {
                    caption(at((element.x, element.y)), said, zoom, &ink, window, cx)
                }
                Part::Hidden | Part::Other => {}
            }
        }
        for (middle, label) in pills {
            pill(middle, label, zoom, &ink, window, cx);
        }
        if !self.logged.replace(true) {
            eprintln!(
                "desk: chat flow {} nodes laid out in {} us, painted in {} us",
                self.nodes,
                self.laid.as_micros(),
                started.elapsed().as_micros()
            );
        }
    }
}
