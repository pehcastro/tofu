use desk_ui::components::paint::tint;
use desk_ui::theme::{ColorToken, Theme};
use draw_doc::{Element, Rect};
use gpui::{
    App, Background, BorderStyle, Bounds, Corners, Edges, PathBuilder, Pixels, Point, RenderImage,
    Rgba, ShapedLine, SharedString, TextAlign, TextRun, Window, fill, hsla, outline, point, px,
    quad, rgb, rgb_to_hsla, size, transparent_black,
};
use serde_json::json;
use std::cell::Cell;
use std::sync::Arc;
use std::time::Instant;

use crate::edit::{self, Handle, Side};
use crate::shape::{self, Kind};
use crate::soft::{self, Body, Noise, Outline, Spot, Step};
use crate::style::{Dash, PAPER};
use crate::view::View;

pub const HANDLE: f32 = 8.0;
const KAPPA: f64 = 0.552_284_8;
const ROUND_MOST: f64 = 32.0;
const ROUND_SHARE: f64 = 0.25;
const HEAD_LENGTH: f64 = 15.0;
const HEAD_SPREAD: f64 = 0.45;
const THINNEST: f32 = 0.5;
const SMALLEST_TEXT: f32 = 1.5;
const SELECTION_GAP: f32 = 4.0;
const CARET_WIDTH: f32 = 1.5;
const FIELD_LEAST: f32 = 80.0;
const FIELD_PAD: f32 = 4.0;
const HATCHURE_SHARE: f32 = 0.5;
const SKETCH_BELOW_ZOOM: f64 = 0.5;
const SKETCH_SIDE: f32 = 24.0;
const SKETCH_TEXT: f32 = 6.0;
const GREEK_SHARE: f32 = 0.5;
const DEFAULT_INKS: [&str; 4] = ["#000000", "#1e1e1e", "#1d1d1d", "#000"];
const INK_ON_LIGHT: u32 = 0x1e1e1e;
const INK_ON_DARK: u32 = 0xe9ecef;
const LIGHT_GROUND: f32 = 0.5;
const LUMA: [f32; 3] = [0.2126, 0.7152, 0.0722];
const DOT_INK: f32 = 0.22;
const DOT_SIDE: f32 = 1.5;
const GRID_SIZE: f64 = 20.0;
const BIND_GLOW: f32 = 6.0;
const BIND_GLOW_OPACITY: u8 = 45;
const DOT_GAP_LEAST: f64 = 16.0;
const CONNECT_RADIUS: f32 = 4.0;
const CONNECT_LINE: f32 = 1.5;
const CONNECT_FILL: u32 = 0xffffff;
const CONNECT_GLOW: f32 = 8.0;
const CONNECT_GLOW_ALPHA: f32 = 0.24;
const BRUSH_GREY: f32 = 0.56;
const BRUSH_FILL: f32 = 0.102;
const BRUSH_STROKE: f32 = 0.251;
const DRAW_PASSES: u64 = 2;
const WOBBLE_SHARE: f64 = 1.0 / 3.0;
const DRAW_ROUND: f64 = 2.0;
const DRAW_BELOW_ZOOM: f64 = 0.25;
const HATCH_GAP: f64 = 8.0;
const HATCH_INK: f32 = 0.6;
const HATCH_LINE: f64 = 0.5;
const PICTURE_WAIT: f32 = 0.06;
const PICTURE_EDGE: f32 = 0.3;
const PICTURE_RADIUS: f32 = 4.0;
const LASER_INK: u32 = 0xff3b30;
const LASER_WIDTH: f32 = 4.0;
const LASER_THINNEST: f32 = 0.25;

pub struct Palette {
    pub ink: Rgba,
    pub accent: Rgba,
    pub field: Rgba,
    pub ground: Rgba,
    pub dots: Rgba,
}

fn hex(written: &str) -> Option<Rgba> {
    let hex = written.strip_prefix('#')?;
    let hex = match hex.len() {
        3 => hex.chars().flat_map(|c| [c, c]).collect(),
        _ => hex.to_owned(),
    };
    u32::from_str_radix(hex.get(..6)?, 16).ok().map(rgb)
}

impl Palette {
    pub fn new(theme: &Theme, ground: Option<&str>) -> Self {
        let custom = ground.and_then(hex);
        let ink = match custom {
            Some(ground)
                if LUMA[0] * ground.red + LUMA[1] * ground.green + LUMA[2] * ground.blue
                    > LIGHT_GROUND =>
            {
                rgb(INK_ON_LIGHT)
            }
            Some(_) => rgb(INK_ON_DARK),
            None => theme.color(ColorToken::TextStrong),
        };
        Palette {
            ink,
            accent: theme.color(ColorToken::FocusRing),
            field: theme.color(ColorToken::FieldFill),
            ground: custom.unwrap_or_else(|| theme.color(ColorToken::CardsInnerFill)),
            dots: match custom {
                Some(_) => tint(ink, DOT_INK),
                None => theme.color(ColorToken::CardsDots),
            },
        }
    }

    pub fn color(&self, written: Option<&str>) -> Option<Rgba> {
        let written = written?;
        if written == "transparent" {
            return None;
        }
        if DEFAULT_INKS.contains(&written) {
            return Some(self.ink);
        }
        if written.eq_ignore_ascii_case(PAPER) {
            return Some(self.ground);
        }
        hex(written)
    }
}

pub fn handle(
    view: &View,
    origin: Point<Pixels>,
    element: &Element,
    grip: Handle,
) -> Bounds<Pixels> {
    let (x, y) = grip.of(element);
    let middle = view.screen(origin, x, y);
    let half = px(HANDLE / 2.0);
    Bounds::new(
        point(middle.x - half, middle.y - half),
        size(px(HANDLE), px(HANDLE)),
    )
}

fn dot(middle: Point<Pixels>, radius: f32) -> Bounds<Pixels> {
    let r = px(radius);
    Bounds::new(point(middle.x - r, middle.y - r), size(r * 2.0, r * 2.0))
}

pub struct Frame<'a> {
    pub view: View,
    pub origin: Point<Pixels>,
    pub palette: &'a Palette,
    pub soft: Cell<f64>,
}

#[derive(Default)]
pub struct Batch {
    strokes: Vec<(Rgba, Pixels, PathBuilder)>,
}

impl Batch {
    fn stroke(&mut self, color: Rgba, width: Pixels) -> &mut PathBuilder {
        let at = match self
            .strokes
            .iter()
            .position(|(have, wide, _)| *have == color && *wide == width)
        {
            Some(at) => at,
            None => {
                self.strokes
                    .push((color, width, PathBuilder::stroke(width)));
                self.strokes.len() - 1
            }
        };
        &mut self.strokes[at].2
    }

    pub fn paint(self, window: &mut Window) {
        for (color, _, path) in self.strokes {
            if let Ok(path) = path.build() {
                window.paint_path(path, color);
            }
        }
    }
}

fn wings(tip: (f64, f64), from: (f64, f64)) -> Option<((f64, f64), (f64, f64))> {
    let (dx, dy) = (from.0 - tip.0, from.1 - tip.1);
    let length = dx.hypot(dy);
    if length <= f64::EPSILON {
        return None;
    }
    let reach = HEAD_LENGTH.min(length / 2.0);
    let (ux, uy) = (dx / length, dy / length);
    let wing = |turn: f64| {
        let (sin, cos) = turn.sin_cos();
        (
            tip.0 + (ux * cos - uy * sin) * reach,
            tip.1 + (ux * sin + uy * cos) * reach,
        )
    };
    Some((wing(HEAD_SPREAD), wing(-HEAD_SPREAD)))
}

enum Head {
    Open,
    Solid,
}

impl Head {
    fn of(written: Option<&str>) -> Option<Head> {
        match written? {
            "triangle" | "triangle_outline" | "diamond" | "diamond_outline" => Some(Head::Solid),
            _ => Some(Head::Open),
        }
    }
}

fn guide(element: &Element) -> (Vec<Spot>, bool) {
    let trace = shape::trace(element);
    let rounded = element.extra.get("roundness").is_some_and(|r| !r.is_null());
    match (Kind::of(element), trace.as_slice()) {
        (Kind::Line | Kind::Arrow, [a, through, b]) if rounded && !edit::elbowed(element) => {
            let control = (
                2.0 * through.0 - (a.0 + b.0) / 2.0,
                2.0 * through.1 - (a.1 + b.1) / 2.0,
            );
            (vec![*a, control, *b], true)
        }
        _ => (trace, false),
    }
}

fn body(element: &Element, drawn: Option<f64>) -> Option<Body> {
    let corner = |radius: f64| drawn.map_or(radius, |width| radius.max(width * DRAW_ROUND));
    let (left, top) = (element.x, element.y);
    let (right, bottom) = (left + element.width, top + element.height);
    let (mx, my) = ((left + right) / 2.0, (top + bottom) / 2.0);
    match Kind::of(element) {
        Kind::Rectangle | Kind::Other => {
            let round = element.extra.get("roundness").is_some_and(|r| !r.is_null());
            let radius = match round {
                true => (element.width.min(element.height) * ROUND_SHARE).min(ROUND_MOST),
                false => 0.0,
            };
            Some(Body::Corners {
                points: vec![(left, top), (right, top), (right, bottom), (left, bottom)],
                closed: true,
                round: corner(radius),
            })
        }
        Kind::Diamond => Some(Body::Corners {
            points: vec![(mx, top), (right, my), (mx, bottom), (left, my)],
            closed: true,
            round: corner(0.0),
        }),
        Kind::Ellipse => {
            let (ka, kb) = (element.width / 2.0 * KAPPA, element.height / 2.0 * KAPPA);
            Some(Body::Curve(Outline {
                start: (right, my),
                steps: vec![
                    Step::Cubic((right, my + kb), (mx + ka, bottom), (mx, bottom)),
                    Step::Cubic((mx - ka, bottom), (left, my + kb), (left, my)),
                    Step::Cubic((left, my - kb), (mx - ka, top), (mx, top)),
                    Step::Cubic((mx + ka, top), (right, my - kb), (right, my)),
                ],
                closed: true,
            }))
        }
        Kind::Line | Kind::Arrow => {
            let (guide, curved) = guide(element);
            match (curved, guide.as_slice()) {
                (true, [a, control, b]) => Some(Body::Curve(Outline {
                    start: *a,
                    steps: vec![Step::Quad(*control, *b)],
                    closed: false,
                })),
                _ => Some(Body::Corners {
                    points: guide,
                    closed: false,
                    round: corner(0.0),
                }),
            }
        }
        Kind::Freedraw => Some(Body::Corners {
            points: shape::trace(element),
            closed: false,
            round: 0.0,
        }),
        Kind::Text | Kind::Image => None,
    }
}

impl Frame<'_> {
    fn outline(&self, element: &Element, outline: &Outline, path: &mut PathBuilder) {
        path.move_to(self.at(element, outline.start));
        for step in &outline.steps {
            match *step {
                Step::Line(to) => path.line_to(self.at(element, to)),
                Step::Quad(control, to) => {
                    path.curve_to(self.at(element, to), self.at(element, control));
                }
                Step::Cubic(a, b, to) => path.cubic_bezier_to(
                    self.at(element, to),
                    self.at(element, a),
                    self.at(element, b),
                ),
            }
        }
        if outline.closed {
            path.close();
        }
    }

    fn at(&self, element: &Element, (x, y): (f64, f64)) -> Point<Pixels> {
        let angle = element.angle.unwrap_or(0.0);
        if angle == 0.0 {
            return self.view.screen(self.origin, x, y);
        }
        let (cx, cy) = (
            element.x + element.width / 2.0,
            element.y + element.height / 2.0,
        );
        let (sin, cos) = angle.sin_cos();
        let (dx, dy) = (x - cx, y - cy);
        self.view.screen(
            self.origin,
            cx + dx * cos - dy * sin,
            cy + dx * sin + dy * cos,
        )
    }

    pub fn dots(&self, area: Bounds<Pixels>, window: &mut Window) {
        let mut step = GRID_SIZE;
        while step * self.view.zoom() < DOT_GAP_LEAST {
            step *= 2.0;
        }
        let seen = self.view.visible(area);
        let side = px(DOT_SIDE);
        let mut x = (seen.min_x / step).ceil() * step;
        while x <= seen.max_x {
            let mut y = (seen.min_y / step).ceil() * step;
            while y <= seen.max_y {
                let at = self.view.screen(self.origin, x, y);
                let dot = Bounds::new(
                    point(at.x - side / 2.0, at.y - side / 2.0),
                    size(side, side),
                );
                window.paint_quad(fill(dot, self.palette.dots).corner_radii(side / 2.0));
                y += step;
            }
            x += step;
        }
    }

    pub fn sketch(&self, element: &Element, batch: &mut Batch, window: &mut Window) -> bool {
        if self.view.zoom() >= SKETCH_BELOW_ZOOM {
            return false;
        }
        let kind = Kind::of(element);
        let (stroke, background, width) = self.inks(element);
        if kind == Kind::Text {
            let font = self.view.length(shape::font_size(element));
            if font >= px(SKETCH_TEXT) {
                return false;
            }
            let Some(color) = stroke else {
                return true;
            };
            let origin = self.view.screen(self.origin, element.x, element.y);
            let step = self.view.length(shape::line_step(element));
            let rows = shape::label(element).map_or(0, |text| text.split('\n').count());
            let thick = font * GREEK_SHARE;
            for row in 0..rows {
                let top = origin.y + step * row as f32 + (step - thick) / 2.0;
                let bar = Bounds::new(
                    point(origin.x, top),
                    size(self.view.length(element.width), thick),
                );
                window.paint_quad(fill(bar, tint(color, color.alpha * GREEK_SHARE)));
            }
            return true;
        }
        let bounds = element.bounds();
        let area = Bounds::from_corners(
            self.view.screen(self.origin, bounds.min_x, bounds.min_y),
            self.view.screen(self.origin, bounds.max_x, bounds.max_y),
        );
        if area.size.width.max(area.size.height) >= px(SKETCH_SIDE) {
            return false;
        }
        if kind.traced() {
            let Some(color) = stroke else {
                return true;
            };
            let trace = shape::trace(element);
            let path = batch.stroke(color, width);
            if let Some((first, rest)) = trace.split_first() {
                path.move_to(self.at(element, *first));
                for point in rest {
                    path.line_to(self.at(element, *point));
                }
            }
            if kind == Kind::Arrow
                && let [.., from, tip] = trace.as_slice()
                && let Some((left, right)) = wings(*tip, *from)
            {
                path.move_to(self.at(element, left));
                path.line_to(self.at(element, *tip));
                path.line_to(self.at(element, right));
            }
            return true;
        }
        let side = area.size.width.min(area.size.height);
        let round = element.extra.get("roundness").is_some_and(|r| !r.is_null());
        let radius = match kind {
            Kind::Ellipse => side / 2.0,
            _ if round => side * ROUND_SHARE as f32,
            _ => px(0.0),
        };
        let paint = |color: Option<Rgba>| -> Background {
            color.map_or_else(|| transparent_black().into(), Background::from)
        };
        window.paint_quad(quad(
            area,
            Corners::all(radius),
            paint(background),
            Edges::all(width),
            paint(stroke),
            BorderStyle::Solid,
        ));
        true
    }

    fn inks(&self, element: &Element) -> (Option<Rgba>, Option<Rgba>, Pixels) {
        let opacity = shape::number(element, "opacity").unwrap_or(100.0) as f32 / 100.0;
        let faded = |color: Rgba| tint(color, color.alpha * opacity);
        let stroke = self
            .palette
            .color(shape::word(element, "strokeColor"))
            .map(faded);
        let light = match shape::word(element, "fillStyle") {
            Some("solid") | None => 1.0,
            Some(_) => HATCHURE_SHARE,
        };
        let background = self
            .palette
            .color(shape::word(element, "backgroundColor"))
            .map(|color| tint(color, color.alpha * opacity * light));
        let width = self
            .view
            .length(shape::number(element, "strokeWidth").unwrap_or(1.0))
            .max(px(THINNEST));
        (stroke, background, width)
    }

    pub fn element(&self, element: &Element, editing: bool, window: &mut Window, cx: &mut App) {
        let (stroke, background, width) = self.inks(element);
        let dash = match shape::word(element, "strokeStyle") {
            Some("dashed") => Some([width * 4.0, width * 4.0]),
            Some("dotted") => Some([width, width * 2.0]),
            _ => None,
        };
        let kind = Kind::of(element);
        let thick = shape::number(element, "strokeWidth").unwrap_or(1.0);
        let drawn = (kind != Kind::Freedraw
            && Dash::of(element) == Dash::Draw
            && self.view.zoom() >= DRAW_BELOW_ZOOM)
            .then_some(thick);
        let Some(body) = body(element, drawn) else {
            if !editing && let Some(color) = stroke {
                self.text(element, color, window, cx);
            }
            return;
        };
        if let Some(color) = background
            && !kind.traced()
        {
            let outline = soft::soften(&body, 0.0, None);
            let mut path = PathBuilder::fill();
            self.outline(element, &outline, &mut path);
            if let Ok(path) = path.build() {
                window.paint_path(path, color);
            }
            if !matches!(shape::word(element, "fillStyle"), Some("solid") | None) {
                let ink = stroke.unwrap_or(color);
                let mut path =
                    PathBuilder::stroke(self.view.length(thick * HATCH_LINE).max(px(THINNEST)));
                for (from, to) in soft::hatch(&soft::flatten(&outline), HATCH_GAP) {
                    path.move_to(self.at(element, from));
                    path.line_to(self.at(element, to));
                }
                if let Ok(path) = path.build() {
                    window.paint_path(path, tint(ink, ink.alpha * HATCH_INK));
                }
            }
        }
        if let Some(color) = stroke {
            let started = Instant::now();
            let mut path = match dash {
                Some(dash) => PathBuilder::stroke(width).dash_array(&dash),
                None => PathBuilder::stroke(width),
            };
            match drawn {
                Some(thick) => {
                    for pass in 0..DRAW_PASSES {
                        let mut noise = Noise::new(&element.id, pass);
                        let outline = soft::soften(&body, thick * WOBBLE_SHARE, Some(&mut noise));
                        self.outline(element, &outline, &mut path);
                    }
                }
                None => self.outline(element, &soft::soften(&body, 0.0, None), &mut path),
            }
            if let Ok(path) = path.build() {
                window.paint_path(path, color);
            }
            if drawn.is_some() {
                let spent = started.elapsed().as_secs_f64() * 1000.0;
                self.soft.set(self.soft.get() + spent);
            }
        }
        if let Some(color) = stroke
            && kind == Kind::Arrow
        {
            let (guide, _) = guide(element);
            let mut ends = Vec::with_capacity(2);
            if let [a, b, ..] = guide.as_slice() {
                ends.push((Head::of(shape::word(element, "startArrowhead")), *a, *b));
            }
            if let [.., a, b] = guide.as_slice() {
                ends.push((Head::of(shape::word(element, "endArrowhead")), *b, *a));
            }
            for (head, tip, from) in ends {
                if let Some(head) = head {
                    self.head(element, head, tip, from, (color, width), window);
                }
            }
        }
    }

    fn head(
        &self,
        element: &Element,
        head: Head,
        tip: (f64, f64),
        from: (f64, f64),
        (color, width): (Rgba, Pixels),
        window: &mut Window,
    ) {
        let Some((left, right)) = wings(tip, from) else {
            return;
        };
        let mut path = match head {
            Head::Open => PathBuilder::stroke(width),
            Head::Solid => PathBuilder::fill(),
        };
        path.move_to(self.at(element, left));
        path.line_to(self.at(element, tip));
        path.line_to(self.at(element, right));
        if let Head::Solid = head {
            path.close();
        }
        if let Ok(path) = path.build() {
            window.paint_path(path, color);
        }
    }

    fn text(&self, element: &Element, color: Rgba, window: &mut Window, cx: &mut App) {
        let Some(text) = shape::label(element) else {
            return;
        };
        let font_size = self.view.length(shape::font_size(element));
        if f32::from(font_size) < SMALLEST_TEXT {
            return;
        }
        let step = self.view.length(shape::line_step(element));
        let room = self.view.length(element.width);
        let origin = self.view.screen(self.origin, element.x, element.y);
        let font = window.text_style().font();
        let align = shape::word(element, "textAlign");
        for (row, line) in text.split('\n').enumerate() {
            if line.is_empty() {
                continue;
            }
            let run = TextRun {
                len: line.len(),
                font: font.clone(),
                color: rgb_to_hsla(color),
                background_color: None,
                underline: None,
                strikethrough: None,
                letter_spacing: None,
            };
            let shaped = window.text_system().shape_line(
                SharedString::from(line.to_owned()),
                font_size,
                &[run],
                None,
            );
            let spare = room - shaped.width();
            let shift = match align {
                Some("center") => spare / 2.0,
                Some("right") => spare,
                _ => px(0.0),
            };
            let at = point(origin.x + shift, origin.y + step * row as f32);
            if let Err(error) = shaped.paint(at, step, TextAlign::Left, None, window, cx) {
                eprintln!("desk: canvas text failed: {error}");
            }
        }
    }

    pub fn picture(&self, element: &Element, image: Option<Arc<RenderImage>>, window: &mut Window) {
        let bounds = element.bounds();
        let area = Bounds::from_corners(
            self.view.screen(self.origin, bounds.min_x, bounds.min_y),
            self.view.screen(self.origin, bounds.max_x, bounds.max_y),
        );
        let Some(image) = image else {
            window.paint_quad(quad(
                area,
                Corners::all(px(PICTURE_RADIUS)),
                tint(self.palette.ink, PICTURE_WAIT),
                Edges::all(px(1.0)),
                tint(self.palette.ink, PICTURE_EDGE),
                BorderStyle::Dashed,
            ));
            return;
        };
        if let Err(error) = window.paint_image(area, area, Corners::default(), image, 0, false) {
            eprintln!("desk: canvas image {} failed: {error}", element.id);
        }
    }

    pub fn laser(&self, trail: &[((f64, f64), f32)], window: &mut Window) {
        for pair in trail.windows(2) {
            let [(from, _), (to, life)] = pair else {
                continue;
            };
            let mut path = PathBuilder::stroke(px(LASER_WIDTH * life.max(LASER_THINNEST)));
            path.move_to(self.view.screen(self.origin, from.0, from.1));
            path.line_to(self.view.screen(self.origin, to.0, to.1));
            if let Ok(path) = path.build() {
                window.paint_path(path, tint(rgb(LASER_INK), *life));
            }
        }
    }

    pub fn selection(&self, element: &Element, window: &mut Window) {
        let bounds = element.bounds();
        let gap = px(SELECTION_GAP);
        let top_left = self.view.screen(self.origin, bounds.min_x, bounds.min_y);
        let bottom_right = self.view.screen(self.origin, bounds.max_x, bounds.max_y);
        let frame = Bounds::from_corners(
            point(top_left.x - gap, top_left.y - gap),
            point(bottom_right.x + gap, bottom_right.y + gap),
        );
        window.paint_quad(outline(frame, self.palette.accent, BorderStyle::Solid));
    }

    pub fn grips(&self, element: &Element, window: &mut Window) {
        for grip in Handle::ALL.into_iter().filter(|grip| grip.corner()) {
            let handle = handle(&self.view, self.origin, element, grip);
            window.paint_quad(fill(handle, self.palette.ground));
            window.paint_quad(outline(handle, self.palette.accent, BorderStyle::Solid));
        }
    }

    pub fn connectors(&self, element: &Element, lit: Option<Side>, window: &mut Window) {
        for side in Side::ALL {
            let middle = self.at(element, side.of(element, 0.0));
            if lit == Some(side) {
                window.paint_quad(
                    fill(
                        dot(middle, CONNECT_GLOW),
                        tint(self.palette.accent, CONNECT_GLOW_ALPHA),
                    )
                    .corner_radii(px(CONNECT_GLOW)),
                );
            }
            self.knob(middle, window);
        }
    }

    pub fn knob(&self, middle: Point<Pixels>, window: &mut Window) {
        window.paint_quad(quad(
            dot(middle, CONNECT_RADIUS),
            Corners::all(px(CONNECT_RADIUS)),
            rgb(CONNECT_FILL),
            Edges::all(px(CONNECT_LINE)),
            self.palette.accent,
            BorderStyle::Solid,
        ));
    }

    pub fn brush(&self, area: &Rect, window: &mut Window) {
        let area = Bounds::from_corners(
            self.view.screen(self.origin, area.min_x, area.min_y),
            self.view.screen(self.origin, area.max_x, area.max_y),
        );
        window.paint_quad(quad(
            area,
            Corners::default(),
            hsla(0.0, 0.0, BRUSH_GREY, BRUSH_FILL),
            Edges::all(px(1.0)),
            hsla(0.0, 0.0, BRUSH_GREY, BRUSH_STROKE),
            BorderStyle::Solid,
        ));
    }

    pub fn highlight(&self, element: &Element, window: &mut Window, cx: &mut App) {
        let accent = self.palette.accent;
        let channel = |share: f32| (share.clamp(0.0, 1.0) * 255.0).round() as u8;
        let hex = format!(
            "#{:02x}{:02x}{:02x}",
            channel(accent.red),
            channel(accent.green),
            channel(accent.blue)
        );
        let width = shape::number(element, "strokeWidth").unwrap_or(1.0)
            + f64::from(BIND_GLOW) / self.view.zoom();
        let mut glow = element.clone();
        for (field, value) in [
            ("strokeColor", json!(hex)),
            ("backgroundColor", json!("transparent")),
            ("strokeStyle", json!("solid")),
            ("opacity", json!(BIND_GLOW_OPACITY)),
            ("strokeWidth", json!(width)),
        ] {
            glow.extra.insert(field.to_owned(), value);
        }
        self.element(&glow, false, window, cx);
    }

    pub fn field(
        &self,
        element: &Element,
        text: &str,
        host: Option<&Element>,
        window: &mut Window,
        cx: &mut App,
    ) {
        let font_size = self.view.length(shape::font_size(element));
        let step = self.view.length(shape::line_step(element));
        let font = window.text_style().font();
        let lines: Vec<_> = text
            .split('\n')
            .map(|line| {
                let run = TextRun {
                    len: line.len(),
                    font: font.clone(),
                    color: rgb_to_hsla(self.palette.ink),
                    background_color: None,
                    underline: None,
                    strikethrough: None,
                    letter_spacing: None,
                };
                window.text_system().shape_line(
                    SharedString::from(line.to_owned()),
                    font_size,
                    &[run],
                    None,
                )
            })
            .collect();
        let pad = px(FIELD_PAD);
        let least = match host {
            Some(_) => px(0.0),
            None => self.view.length(element.width),
        };
        let wide = lines
            .iter()
            .map(|line| line.width())
            .fold(least, Pixels::max)
            .max(px(FIELD_LEAST));
        let tall = step * lines.len() as f32;
        let origin = match host.map(Element::bounds) {
            Some(around) => {
                let middle = self.view.screen(
                    self.origin,
                    (around.min_x + around.max_x) / 2.0,
                    (around.min_y + around.max_y) / 2.0,
                );
                point(middle.x - wide / 2.0, middle.y - tall / 2.0)
            }
            None => self.view.screen(self.origin, element.x, element.y),
        };
        let indent = |line: &ShapedLine| match host {
            Some(_) => (wide - line.width()) / 2.0,
            None => px(0.0),
        };
        let area = Bounds::new(
            point(origin.x - pad, origin.y - pad),
            size(wide + pad * 2.0, tall + pad * 2.0),
        );
        window.paint_quad(fill(area, self.palette.field));
        window.paint_quad(outline(area, self.palette.accent, BorderStyle::Solid));
        let mut at = origin;
        for line in &lines {
            let start = point(at.x + indent(line), at.y);
            if let Err(error) = line.paint(start, step, TextAlign::Left, None, window, cx) {
                eprintln!("desk: canvas text failed: {error}");
            }
            at.y += step;
        }
        let last = lines
            .last()
            .map_or(px(0.0), |line| indent(line) + line.width());
        let caret = Bounds::new(
            point(origin.x + last, at.y - step),
            size(px(CARET_WIDTH), step),
        );
        window.paint_quad(fill(caret, self.palette.ink));
    }
}
