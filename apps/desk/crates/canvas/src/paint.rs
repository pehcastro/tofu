use desk_ui::components::paint::tint;
use desk_ui::theme::{ColorToken, Theme};
use draw_doc::Element;
use gpui::{
    App, BorderStyle, Bounds, PathBuilder, Pixels, Point, Rgba, SharedString, TextAlign, TextRun,
    Window, fill, outline, point, px, rgb, rgb_to_hsla, size,
};

use crate::edit::Handle;
use crate::shape::{self, Kind};
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
const DEFAULT_INKS: [&str; 3] = ["#000000", "#1e1e1e", "#000"];

pub struct Palette {
    pub ink: Rgba,
    pub accent: Rgba,
    pub field: Rgba,
    pub ground: Rgba,
}

impl Palette {
    pub fn new(theme: &Theme) -> Self {
        Palette {
            ink: theme.color(ColorToken::TextStrong),
            accent: theme.color(ColorToken::FocusRing),
            field: theme.color(ColorToken::FieldFill),
            ground: theme.color(ColorToken::CardsInnerFill),
        }
    }

    fn color(&self, written: Option<&str>) -> Option<Rgba> {
        let written = written?;
        if written == "transparent" {
            return None;
        }
        if DEFAULT_INKS.contains(&written) {
            return Some(self.ink);
        }
        let hex = written.strip_prefix('#')?;
        let hex = match hex.len() {
            3 => hex.chars().flat_map(|c| [c, c]).collect(),
            _ => hex.to_owned(),
        };
        u32::from_str_radix(hex.get(..6)?, 16).ok().map(rgb)
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

pub struct Frame<'a> {
    pub view: View,
    pub origin: Point<Pixels>,
    pub palette: &'a Palette,
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

impl Frame<'_> {
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

    pub fn element(&self, element: &Element, editing: bool, window: &mut Window, cx: &mut App) {
        let opacity = shape::number(element, "opacity").unwrap_or(100.0) as f32 / 100.0;
        let faded = |color: Rgba| tint(color, color.alpha * opacity);
        let stroke = self
            .palette
            .color(shape::word(element, "strokeColor"))
            .map(faded);
        let background = self
            .palette
            .color(shape::word(element, "backgroundColor"))
            .map(faded);
        let width = self
            .view
            .length(shape::number(element, "strokeWidth").unwrap_or(1.0))
            .max(px(THINNEST));
        let dash = match shape::word(element, "strokeStyle") {
            Some("dashed") => Some([width * 4.0, width * 4.0]),
            Some("dotted") => Some([width, width * 2.0]),
            _ => None,
        };
        let kind = Kind::of(element);
        let mut draw = |trace: &dyn Fn(&mut PathBuilder)| {
            if let Some(color) = background
                && !kind.traced()
            {
                let mut path = PathBuilder::fill();
                trace(&mut path);
                if let Ok(path) = path.build() {
                    window.paint_path(path, color);
                }
            }
            if let Some(color) = stroke {
                let mut path = match dash {
                    Some(dash) => PathBuilder::stroke(width).dash_array(&dash),
                    None => PathBuilder::stroke(width),
                };
                trace(&mut path);
                if let Ok(path) = path.build() {
                    window.paint_path(path, color);
                }
            }
        };
        let (left, top) = (element.x, element.y);
        let (right, bottom) = (left + element.width, top + element.height);
        match kind {
            Kind::Rectangle | Kind::Other => {
                let round = element.extra.get("roundness").is_some_and(|r| !r.is_null());
                let r = match round {
                    true => (element.width.min(element.height) * ROUND_SHARE).min(ROUND_MOST),
                    false => 0.0,
                };
                draw(&|path: &mut PathBuilder| {
                    path.move_to(self.at(element, (left + r, top)));
                    for (before, corner, after) in [
                        ((right - r, top), (right, top), (right, top + r)),
                        ((right, bottom - r), (right, bottom), (right - r, bottom)),
                        ((left + r, bottom), (left, bottom), (left, bottom - r)),
                        ((left, top + r), (left, top), (left + r, top)),
                    ] {
                        path.line_to(self.at(element, before));
                        if r > 0.0 {
                            path.curve_to(self.at(element, after), self.at(element, corner));
                        }
                    }
                    path.close();
                });
            }
            Kind::Diamond => {
                let (mx, my) = ((left + right) / 2.0, (top + bottom) / 2.0);
                draw(&|path: &mut PathBuilder| {
                    path.move_to(self.at(element, (mx, top)));
                    path.line_to(self.at(element, (right, my)));
                    path.line_to(self.at(element, (mx, bottom)));
                    path.line_to(self.at(element, (left, my)));
                    path.close();
                });
            }
            Kind::Ellipse => {
                let (mx, my) = ((left + right) / 2.0, (top + bottom) / 2.0);
                let (a, b) = (element.width / 2.0, element.height / 2.0);
                let (ka, kb) = (a * KAPPA, b * KAPPA);
                draw(&|path: &mut PathBuilder| {
                    path.move_to(self.at(element, (right, my)));
                    for (to, c1, c2) in [
                        ((mx, bottom), (right, my + kb), (mx + ka, bottom)),
                        ((left, my), (mx - ka, bottom), (left, my + kb)),
                        ((mx, top), (left, my - kb), (mx - ka, top)),
                        ((right, my), (mx + ka, top), (right, my - kb)),
                    ] {
                        path.cubic_bezier_to(
                            self.at(element, to),
                            self.at(element, c1),
                            self.at(element, c2),
                        );
                    }
                    path.close();
                });
            }
            Kind::Line | Kind::Arrow | Kind::Freedraw => {
                let trace = shape::trace(element);
                let Some((first, rest)) = trace.split_first() else {
                    return;
                };
                draw(&|path: &mut PathBuilder| {
                    path.move_to(self.at(element, *first));
                    for point in rest {
                        path.line_to(self.at(element, *point));
                    }
                });
                if let Some(color) = stroke
                    && kind == Kind::Arrow
                {
                    let mut ends = Vec::with_capacity(2);
                    if let [a, b, ..] = trace.as_slice() {
                        ends.push((Head::of(shape::word(element, "startArrowhead")), *a, *b));
                    }
                    if let [.., a, b] = trace.as_slice() {
                        ends.push((Head::of(shape::word(element, "endArrowhead")), *b, *a));
                    }
                    for (head, tip, from) in ends {
                        if let Some(head) = head {
                            self.head(element, head, tip, from, (color, width), window);
                        }
                    }
                }
            }
            Kind::Text => {
                if !editing && let Some(color) = stroke {
                    self.text(element, color, window, cx);
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
        let (dx, dy) = (from.0 - tip.0, from.1 - tip.1);
        let length = dx.hypot(dy);
        if length <= f64::EPSILON {
            return;
        }
        let reach = HEAD_LENGTH.min(length / 2.0);
        let wing = |turn: f64| {
            let (sin, cos) = turn.sin_cos();
            let (ux, uy) = (dx / length, dy / length);
            (
                tip.0 + (ux * cos - uy * sin) * reach,
                tip.1 + (ux * sin + uy * cos) * reach,
            )
        };
        let (left, right) = (wing(HEAD_SPREAD), wing(-HEAD_SPREAD));
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
        if !Kind::of(element).resizable() {
            return;
        }
        for grip in Handle::ALL {
            let handle = handle(&self.view, self.origin, element, grip);
            window.paint_quad(fill(handle, self.palette.ground));
            window.paint_quad(outline(handle, self.palette.accent, BorderStyle::Solid));
        }
    }

    pub fn field(&self, element: &Element, text: &str, window: &mut Window, cx: &mut App) {
        let origin = self.view.screen(self.origin, element.x, element.y);
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
        let wide = lines
            .iter()
            .map(|line| line.width())
            .fold(self.view.length(element.width), Pixels::max)
            .max(px(FIELD_LEAST));
        let area = Bounds::new(
            point(origin.x - pad, origin.y - pad),
            size(wide + pad * 2.0, step * lines.len() as f32 + pad * 2.0),
        );
        window.paint_quad(fill(area, self.palette.field));
        window.paint_quad(outline(area, self.palette.accent, BorderStyle::Solid));
        let mut at = origin;
        for line in &lines {
            if let Err(error) = line.paint(at, step, TextAlign::Left, None, window, cx) {
                eprintln!("desk: canvas text failed: {error}");
            }
            at.y += step;
        }
        let last = lines.last().map_or(px(0.0), |line| line.width());
        let caret = Bounds::new(
            point(origin.x + last, at.y - step),
            size(px(CARET_WIDTH), step),
        );
        window.paint_quad(fill(caret, self.palette.ink));
    }
}
