mod act;
mod create;
mod edit;
mod paint;
mod shape;
mod soft;
mod style;
mod theme;
mod view;

use std::cell::Cell;
use std::path::PathBuf;
use std::time::{Duration, Instant};

use desk_ui::components::flow::Ink;
use desk_ui::live::ActiveTheme;
use draw_doc::{ArrowEnd, BoundKind, DocError, Element, Look, Op, Rect, Scene};
use gpui::{
    App, Bounds, ContentMask, Context, CursorStyle, ElementInputHandler, EntityInputHandler,
    EventEmitter, FocusHandle, Focusable, ImgResourceLoader, KeyDownEvent, KeyUpEvent,
    LineFragment, MouseButton, MouseDownEvent, MouseMoveEvent, MouseUpEvent, Pixels, Point,
    Resource, ScrollWheelEvent, SharedString, TextRun, UTF16Selection, Window, canvas, div, fill,
    prelude::*, px,
};
use std::ops::Range;

pub use act::{Act, Ask};
use create::Mint;
pub use create::Tool;
use edit::{Handle, Side};
use paint::{Batch, Frame, Palette};
use shape::Kind;
use style::Style;
pub use style::{BLANK, Backdrop, Dash, Fill, GROUNDS, HUES, OPACITIES, Pick, Route, Size, ground};
pub use view::View;

const HIT_SLACK: f64 = 6.0;
const HANDLE_REACH: f32 = 8.0;
const ZOOM_STEP: f64 = 0.002;
const ZOOM_NUDGE: f64 = 0.1;
const PAINT_WINDOW: usize = 120;
const LEAST_DRAG: f64 = 3.0;
const BIND_GAP: f64 = 6.0;
const BIND_REACH: f64 = 12.0;
const BRUSH_LEAST: f32 = 4.0;
const CONNECT_REACH: f32 = 8.0;
const ELBOW_HANDLE_LEAST: f64 = 20.0;
const WRAP_SLACK: f64 = 1.0;
const LASER_FADE: Duration = Duration::from_millis(700);

pub enum Framing {
    Fit,
    At(View),
}

pub struct Changed;

enum Gesture {
    Idle,
    Pan(Point<Pixels>),
    Erase(Vec<String>),
    Brush {
        start: Point<Pixels>,
        from: (f64, f64),
        to: Option<(f64, f64)>,
        kept: Vec<String>,
    },
    Move {
        from: Vec<Element>,
        at: (f64, f64),
    },
    Resize {
        from: Box<Element>,
        grip: Handle,
    },
    Bend(Box<Element>),
    Tip {
        from: Box<Element>,
        end: ArrowEnd,
    },
    Create {
        from: Box<Element>,
        trace: Vec<(f64, f64)>,
        anchor: Option<Anchor>,
    },
}

struct Anchor {
    target: String,
    side: Side,
}

#[derive(Clone, Copy, PartialEq)]
enum Part {
    Body,
    Connect(Side),
    Grip(Handle),
    Bend,
    End(ArrowEnd),
}

#[derive(Clone, PartialEq)]
struct Hover {
    id: String,
    part: Part,
}

fn measure(window: &Window, text: &str, size: f64) -> f64 {
    let font = window.text_style().font();
    let width = text
        .split('\n')
        .map(|line| {
            let run = TextRun {
                len: line.len(),
                font: font.clone(),
                color: gpui::black(),
                background_color: None,
                underline: None,
                strikethrough: None,
                letter_spacing: None,
            };
            window
                .text_system()
                .shape_line(
                    SharedString::from(line.to_owned()),
                    px(size as f32),
                    &[run],
                    None,
                )
                .width()
        })
        .fold(px(0.0), Pixels::max);
    f64::from(width)
}

fn wrap(window: &Window, text: &str, size: f64, room: f64) -> String {
    let font = window.text_style().font();
    let mut wrapper = window.text_system().line_wrapper(font, px(size as f32));
    let mut rows = Vec::new();
    for line in text.split('\n') {
        let fragments = [LineFragment::text(line)];
        let mut from = 0;
        for boundary in wrapper.wrap_line(&fragments, px(room as f32)) {
            rows.push(line.get(from..boundary.ix).unwrap_or_default().trim_end());
            from = boundary.ix;
        }
        rows.push(line.get(from..).unwrap_or_default());
    }
    rows.join("\n")
}

fn fit(
    window: &Window,
    label: &Element,
    raw: &str,
    host: Option<&Element>,
) -> (Element, Option<Element>) {
    let font = shape::font_size(label);
    let room = host
        .filter(|host| Kind::of(host).labelled())
        .map(|host| (host.width - edit::LABEL_PAD * 2.0).max(font));
    let shown = match room {
        Some(room) => wrap(window, raw, font, room),
        None => raw.to_owned(),
    };
    let text = edit::retexted(label, &shown, raw, measure(window, &shown, font));
    let Some(host) = host else {
        return (text, None);
    };
    let host = match room {
        Some(_) => edit::grown(host, text.height),
        None => host.clone(),
    };
    (edit::centred(&text, &host), Some(host))
}

fn rect((ax, ay): (f64, f64), (bx, by): (f64, f64)) -> Rect {
    Rect {
        min_x: ax.min(bx),
        min_y: ay.min(by),
        max_x: ax.max(bx),
        max_y: ay.max(by),
    }
}

struct Editing {
    id: String,
    text: String,
    fresh: Option<Box<Element>>,
}

pub struct Canvas {
    scene: Scene,
    view: View,
    fit: bool,
    bounds: Bounds<Pixels>,
    selected: Vec<String>,
    gesture: Gesture,
    preview: Vec<Element>,
    editing: Option<Editing>,
    failure: Option<SharedString>,
    focus: FocusHandle,
    paints: Vec<f64>,
    drags: Vec<f64>,
    hover: Option<Hover>,
    tool: Tool,
    locked: bool,
    mint: Mint,
    style: Style,
    space: bool,
    backdrop: Backdrop,
    pointer: Option<Point<Pixels>>,
    folder: Option<PathBuf>,
    lasering: bool,
    trail: Vec<((f64, f64), Instant)>,
}

impl EventEmitter<Changed> for Canvas {}

impl EventEmitter<Ask> for Canvas {}

impl Canvas {
    pub fn new(scene: Scene, cx: &mut Context<Self>) -> Self {
        Canvas {
            scene,
            view: View::new(0.0, 0.0, 1.0),
            fit: true,
            bounds: Bounds::default(),
            selected: Vec::new(),
            gesture: Gesture::Idle,
            preview: Vec::new(),
            editing: None,
            failure: None,
            focus: cx.focus_handle(),
            paints: Vec::with_capacity(PAINT_WINDOW),
            drags: Vec::new(),
            hover: None,
            tool: Tool::Select,
            locked: false,
            mint: Mint::new(),
            style: style::DEFAULT,
            space: false,
            backdrop: BLANK,
            pointer: None,
            folder: None,
            lasering: false,
            trail: Vec::new(),
        }
    }

    pub fn backdrop(&self) -> &Backdrop {
        &self.backdrop
    }

    pub fn set_backdrop(&mut self, backdrop: Backdrop, cx: &mut Context<Self>) {
        self.backdrop = backdrop;
        cx.notify();
    }

    pub fn zoom(&self) -> f64 {
        self.view.zoom()
    }

    pub fn nudge_zoom(&mut self, steps: f64, cx: &mut Context<Self>) {
        let target = ((self.view.zoom() / ZOOM_NUDGE).round() + steps) * ZOOM_NUDGE;
        self.zoom_to(target, cx);
    }

    pub fn zoom_to(&mut self, zoom: f64, cx: &mut Context<Self>) {
        let factor = zoom / self.view.zoom();
        self.view = self
            .view
            .zoomed(self.bounds.origin, self.bounds.center(), factor);
        eprintln!("desk: canvas zoom {:.2}", self.view.zoom());
        cx.emit(Changed);
        cx.notify();
    }

    pub fn scene(&self) -> &Scene {
        &self.scene
    }

    pub fn apply(&mut self, ops: Vec<Op>, cx: &mut Context<Self>) -> Result<(), DocError> {
        self.scene.apply(ops)?;
        self.selected.retain(|id| self.scene.get(id).is_some());
        cx.notify();
        Ok(())
    }

    pub fn fit(&mut self, to: Rect, cx: &mut Context<Self>) -> View {
        self.view = View::fit(to, self.bounds.size);
        self.fit = false;
        cx.emit(Changed);
        cx.notify();
        self.view
    }

    pub fn drawn(&self, pad: Pixels) -> Option<Bounds<Pixels>> {
        let content = self.content()?;
        let origin = self.bounds.origin;
        let near = self.view.screen(origin, content.min_x, content.min_y);
        let far = self.view.screen(origin, content.max_x, content.max_y);
        let padded = Bounds::from_corners(
            gpui::point(near.x - pad, near.y - pad),
            gpui::point(far.x + pad, far.y + pad),
        );
        let clipped = padded.intersect(&self.bounds);
        (!clipped.is_empty()).then_some(clipped)
    }

    pub fn tool(&self) -> (Tool, bool) {
        (self.tool, self.locked)
    }

    pub fn set_tool(&mut self, tool: Tool, cx: &mut Context<Self>) {
        if self.tool != tool {
            eprintln!(
                "desk: draw: cursor {} {:?}",
                tool.name().to_lowercase(),
                tool.cursor()
            );
        }
        self.tool = tool;
        self.hover = None;
        cx.emit(Changed);
        cx.notify();
    }

    pub fn toggle_lock(&mut self, cx: &mut Context<Self>) {
        self.locked = !self.locked;
        cx.emit(Changed);
        cx.notify();
    }

    pub fn typing(&self) -> bool {
        self.editing.is_some()
    }

    pub fn styling(&self) -> bool {
        self.tool.draws() || !self.selected.is_empty()
    }

    fn chosen(&self) -> impl Iterator<Item = &Element> {
        self.selected.iter().filter_map(|id| self.scene.get(id))
    }

    pub fn picked(&self, pick: Pick) -> bool {
        match self.selected.is_empty() {
            true => self.style.has(pick),
            false => self.chosen().all(|element| pick.on(element)),
        }
    }

    pub fn routes(&self) -> bool {
        self.tool == Tool::Arrow
            || self
                .chosen()
                .any(|element| Kind::of(element) == Kind::Arrow)
    }

    pub fn defaults(&self, pick: Pick) -> bool {
        self.style.has(pick)
    }

    pub fn set_default(&mut self, pick: Pick, cx: &mut Context<Self>) {
        self.style.set(pick);
        eprintln!("desk: draw default {pick:?}");
        cx.emit(Changed);
        cx.notify();
    }

    pub fn restyle(&mut self, pick: Pick, window: &mut Window, cx: &mut Context<Self>) {
        self.focus.focus(window, cx);
        self.style.set(pick);
        let mut ops = Vec::new();
        for element in self.chosen() {
            let mut styled = element.clone();
            pick.apply(&mut styled);
            let label = self
                .label_of(element)
                .filter(|id| *id != element.id)
                .and_then(|id| self.scene.get(&id));
            if let Pick::Size(size) = pick
                && let Some(label) = label
            {
                let mut sized = label.clone();
                sized.extra.insert(
                    "fontSize".to_owned(),
                    serde_json::json!(size.label(Kind::of(element))),
                );
                let raw = shape::raw(label).unwrap_or_default();
                let (fitted, host) = fit(window, &sized, raw, Some(&styled));
                styled = host.unwrap_or(styled);
                ops.push(Op::Update(Box::new(fitted)));
            }
            ops.push(Op::Update(Box::new(styled)));
        }
        let count = self.selected.len();
        if !ops.is_empty() {
            match self.scene.apply(ops) {
                Ok(()) => eprintln!("desk: draw styled {count} shapes {pick:?}"),
                Err(error) => self.fail(&error),
            }
        }
        cx.emit(Changed);
        cx.notify();
    }

    fn stays(&self) -> bool {
        self.locked || self.tool.keeps()
    }

    fn created(&mut self, cx: &mut Context<Self>) {
        if !self.stays() {
            self.set_tool(Tool::Select, cx);
        }
    }

    fn target(&self, x: f64, y: f64) -> Option<&Element> {
        let slack = BIND_REACH / self.view.zoom();
        self.scene.elements().iter().rev().find(|element| {
            Kind::of(element).resizable()
                && shape::container(element).is_none()
                && shape::hit(element, x, y, slack)
        })
    }

    fn create(&mut self, element: Element, anchor: Option<Anchor>, cx: &mut Context<Self>) {
        let drag = element.width.hypot(element.height) * self.view.zoom();
        if drag < LEAST_DRAG {
            return;
        }
        let id = element.id.clone();
        let kind = element.kind.clone();
        let mut binds = Vec::with_capacity(2);
        let element = match Kind::of(&element) {
            Kind::Arrow => {
                let drawn = match shape::trace(&element).as_slice() {
                    [start, .., end] => vec![*start, *end],
                    other => other.to_vec(),
                };
                let mut trace = drawn.clone();
                let mut leaving = None;
                let last = drawn.len().saturating_sub(1);
                for (end, at, near) in [
                    (ArrowEnd::Start, 0, 1.min(last)),
                    (ArrowEnd::End, last, last.saturating_sub(1)),
                ] {
                    let (Some(&(x, y)), Some(&toward)) = (drawn.get(at), drawn.get(near)) else {
                        continue;
                    };
                    let anchored = match (end, &anchor) {
                        (ArrowEnd::Start, Some(anchor)) => self
                            .scene
                            .get(&anchor.target)
                            .map(|target| (target, anchor.side.of(target, BIND_GAP))),
                        _ => self
                            .target(x, y)
                            .map(|target| (target, edit::edge(target, toward, BIND_GAP))),
                    };
                    let Some((target, outline)) = anchored else {
                        continue;
                    };
                    if let Some(slot) = trace.get_mut(at) {
                        *slot = outline;
                    }
                    let fixed_point = edit::share(outline, target);
                    if end == ArrowEnd::Start {
                        leaving = Some(fixed_point);
                    }
                    binds.push(Op::BindArrow {
                        arrow: id.clone(),
                        end,
                        target: target.id.clone(),
                        fixed_point,
                    });
                }
                match (edit::elbowed(&element), trace.as_slice()) {
                    (true, [start, .., end]) => shape::with_trace(
                        &element,
                        &edit::elbow(
                            *start,
                            *end,
                            edit::across(leaving, *start, *end),
                            edit::midpoint(&element),
                        ),
                    ),
                    _ => shape::with_trace(&element, &trace),
                }
            }
            _ => element,
        };
        let ops = std::iter::once(Op::Add {
            at: None,
            element: Box::new(element),
        })
        .chain(binds)
        .collect();
        match self.scene.apply(ops) {
            Ok(()) => {
                eprintln!("desk: draw created {kind} {id}");
                if !self.stays() {
                    self.selected = vec![id];
                }
                self.created(cx);
            }
            Err(error) => self.fail(&error),
        }
    }

    fn delete(&mut self) {
        let mut doomed: Vec<String> = Vec::new();
        for id in self.chosen().flat_map(|element| self.doomed(element)) {
            if !doomed.contains(&id) {
                doomed.push(id);
            }
        }
        self.remove(doomed);
    }

    fn doomed(&self, element: &Element) -> Vec<String> {
        let mut ids = vec![element.id.clone()];
        ids.extend(self.label_of(element).filter(|label| *label != element.id));
        ids
    }

    fn remove(&mut self, ids: Vec<String>) {
        if ids.is_empty() {
            return;
        }
        let gone = ids.len();
        let ops = ids.into_iter().map(|id| Op::Delete { id }).collect();
        match self.scene.apply(ops) {
            Ok(()) => {
                eprintln!("desk: draw deleted {gone} shapes");
                self.selected.clear();
            }
            Err(error) => self.fail(&error),
        }
    }

    fn erase(&mut self, x: f64, y: f64) {
        let found = self
            .hit(x, y)
            .map(|element| self.doomed(element))
            .unwrap_or_default();
        if let Gesture::Erase(ids) = &mut self.gesture {
            for id in found {
                if !ids.contains(&id) {
                    ids.push(id);
                }
            }
        }
    }

    pub fn show(&mut self, scene: Scene, framing: Framing, cx: &mut Context<Self>) {
        self.scene = scene;
        (self.view, self.fit) = match framing {
            Framing::Fit => (self.view, true),
            Framing::At(view) => (view, false),
        };
        self.selected.clear();
        self.gesture = Gesture::Idle;
        self.preview.clear();
        self.editing = None;
        self.failure = None;
        self.hover = None;
        cx.notify();
    }

    fn fail(&mut self, error: &DocError) {
        eprintln!("desk: canvas failed: {error}");
        self.failure = Some(SharedString::from(error.to_string()));
    }

    fn hit(&self, x: f64, y: f64) -> Option<&Element> {
        let slack = HIT_SLACK / self.view.zoom();
        self.scene
            .elements()
            .iter()
            .rev()
            .filter(|element| shape::container(element).is_none())
            .find(|element| shape::hit(element, x, y, slack))
    }

    fn label_of(&self, element: &Element) -> Option<String> {
        if Kind::of(element) == Kind::Text {
            return Some(element.id.clone());
        }
        let bound = element.bound_elements().ok()?;
        bound
            .into_iter()
            .find(|bound| bound.kind == BoundKind::Text && self.scene.get(&bound.id).is_some())
            .map(|bound| bound.id)
    }

    fn relabel(&mut self, host: &Element) -> Option<Editing> {
        if let Some(id) = self.label_of(host) {
            let text = self.scene.get(&id).and_then(shape::raw)?.to_owned();
            return Some(Editing {
                id,
                text,
                fresh: None,
            });
        }
        if !Kind::of(host).labelled() {
            return None;
        }
        let label = create::label(&mut self.mint, &self.style, host)?;
        Some(Editing {
            id: label.id.clone(),
            text: String::new(),
            fresh: Some(Box::new(label)),
        })
    }

    fn lone(&self) -> Option<&Element> {
        match self.selected.as_slice() {
            [id] => self.scene.get(id),
            _ => None,
        }
    }

    fn grip(&self, at: Point<Pixels>, corner: bool) -> Option<Hover> {
        let element = self.lone()?;
        if self.tool != Tool::Select || !Kind::of(element).resizable() {
            return None;
        }
        let reach = px(HANDLE_REACH);
        Handle::ALL
            .into_iter()
            .filter(|grip| grip.corner() == corner)
            .find(|grip| {
                let (x, y) = grip.of(element);
                let (along_x, along_y) = grip.along(element);
                let middle = self.view.screen(self.bounds.origin, x, y);
                (at.x - middle.x).abs() <= self.view.length(along_x).max(reach)
                    && (at.y - middle.y).abs() <= self.view.length(along_y).max(reach)
            })
            .map(|grip| Hover {
                id: element.id.clone(),
                part: Part::Grip(grip),
            })
    }

    fn hovered(&self, at: Point<Pixels>, (x, y): (f64, f64)) -> Option<Hover> {
        if self.space {
            return None;
        }
        if let Some(corner) = self.grip(at, true) {
            return Some(corner);
        }
        if let Some(arrow) = self
            .lone()
            .filter(|arrow| self.tool == Tool::Select && Kind::of(arrow) == Kind::Arrow)
        {
            let near = |(hx, hy): (f64, f64)| {
                let middle = self.view.screen(self.bounds.origin, hx, hy);
                (at.x - middle.x).abs() <= px(HANDLE_REACH)
                    && (at.y - middle.y).abs() <= px(HANDLE_REACH)
            };
            let ends = [ArrowEnd::Start, ArrowEnd::End]
                .into_iter()
                .find_map(|end| {
                    edit::tip(arrow, end)
                        .filter(|tip| near(*tip))
                        .map(|_| Part::End(end))
                });
            let bend = edit::bend_point(arrow, ELBOW_HANDLE_LEAST / self.view.zoom())
                .filter(|point| near(*point))
                .map(|_| Part::Bend);
            if let Some(part) = ends.or(bend) {
                return Some(Hover {
                    id: arrow.id.clone(),
                    part,
                });
            }
        }
        let target = match self.tool {
            Tool::Select | Tool::Arrow => self.target(x, y),
            _ => None,
        };
        let connect = target.and_then(|target| {
            Side::ALL
                .into_iter()
                .find(|side| {
                    let (sx, sy) = side.of(target, 0.0);
                    let middle = self.view.screen(self.bounds.origin, sx, sy);
                    f32::from(at.x - middle.x).hypot(f32::from(at.y - middle.y)) <= CONNECT_REACH
                })
                .map(|side| Hover {
                    id: target.id.clone(),
                    part: Part::Connect(side),
                })
        });
        connect.or_else(|| self.grip(at, false)).or_else(|| {
            target.map(|target| Hover {
                id: target.id.clone(),
                part: Part::Body,
            })
        })
    }

    fn pressed(&self, x: f64, y: f64) -> Option<String> {
        let slack = HIT_SLACK / self.view.zoom();
        let found = self.scene.elements().iter().rev().find_map(|element| {
            match shape::container(element) {
                Some(host) => shape::hit(element, x, y, 0.0).then(|| host.to_owned()),
                None => shape::pressed(element, x, y, slack).then(|| element.id.clone()),
            }
        });
        found.or_else(|| {
            self.chosen()
                .find(|element| shape::hit(element, x, y, 0.0))
                .map(|element| element.id.clone())
        })
    }

    fn cursor(&self) -> CursorStyle {
        match (&self.gesture, &self.hover) {
            (Gesture::Pan(_), _) => CursorStyle::ClosedHand,
            _ if self.space => CursorStyle::OpenHand,
            (Gesture::Resize { grip, .. }, _) => grip.cursor(),
            (Gesture::Bend(_), _) => CursorStyle::ClosedHand,
            (Gesture::Create { .. } | Gesture::Tip { .. }, _) => CursorStyle::Crosshair,
            (
                Gesture::Idle,
                Some(Hover {
                    part: Part::Bend | Part::End(_),
                    ..
                }),
            ) => CursorStyle::PointingHand,
            (
                Gesture::Idle,
                Some(Hover {
                    part: Part::Grip(grip),
                    ..
                }),
            ) => grip.cursor(),
            (
                Gesture::Idle,
                Some(Hover {
                    part: Part::Connect(_),
                    ..
                }),
            ) => CursorStyle::Crosshair,
            _ => self.tool.cursor(),
        }
    }

    fn mouse_down(&mut self, event: &MouseDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        self.focus.focus(window, cx);
        self.commit(window);
        if self.space || self.tool == Tool::Hand {
            self.gesture = Gesture::Pan(event.position);
            cx.notify();
            return;
        }
        let (x, y) = self.view.scene(self.bounds.origin, event.position);
        if self.tool == Tool::Laser {
            self.lasering = true;
            self.trail.push(((x, y), Instant::now()));
            cx.notify();
            return;
        }
        if self.tool == Tool::Eraser {
            self.gesture = Gesture::Erase(Vec::new());
            self.erase(x, y);
            cx.notify();
            return;
        }
        if event.click_count >= 2 && self.tool == Tool::Select {
            self.editing = self.hit(x, y).cloned().and_then(|host| self.relabel(&host));
            cx.notify();
            return;
        }
        let hover = self.hovered(event.position, (x, y));
        if let Some(Hover {
            id,
            part: Part::Connect(side),
        }) = &hover
            && let Some(start) = self.scene.get(id).map(|host| side.of(host, BIND_GAP))
            && let Some(from) = create::fresh(Tool::Arrow, &mut self.mint, &self.style, start)
        {
            self.selected.clear();
            self.gesture = Gesture::Create {
                from: Box::new(from),
                trace: vec![start, start],
                anchor: Some(Anchor {
                    target: id.clone(),
                    side: *side,
                }),
            };
            cx.notify();
            return;
        }
        if let Some(from) = create::fresh(self.tool, &mut self.mint, &self.style, (x, y)) {
            self.selected.clear();
            match self.tool {
                Tool::Text => {
                    self.editing = Some(Editing {
                        id: from.id.clone(),
                        text: String::new(),
                        fresh: Some(Box::new(from)),
                    });
                    self.created(cx);
                }
                _ => {
                    self.gesture = Gesture::Create {
                        from: Box::new(from),
                        trace: vec![(x, y), (x, y)],
                        anchor: None,
                    };
                }
            }
            cx.notify();
            return;
        }
        if let Some(Hover {
            id,
            part: Part::Bend,
        }) = &hover
            && let Some(arrow) = self.scene.get(id)
        {
            self.gesture = Gesture::Bend(Box::new(arrow.clone()));
            cx.notify();
            return;
        }
        if let Some(Hover {
            id,
            part: Part::End(end),
        }) = &hover
            && let Some(arrow) = self.scene.get(id)
        {
            self.gesture = Gesture::Tip {
                from: Box::new(arrow.clone()),
                end: *end,
            };
            cx.notify();
            return;
        }
        let shift = event.modifiers.shift;
        let grabbed = match hover {
            Some(Hover {
                id,
                part: Part::Grip(grip),
            }) => self.scene.get(&id).cloned().map(|from| (from, grip)),
            _ => None,
        };
        self.gesture = match (grabbed, self.pressed(x, y)) {
            (Some((from, grip)), _) => Gesture::Resize {
                from: Box::new(from),
                grip,
            },
            (None, Some(id)) => {
                match (shift, self.selected.iter().position(|had| *had == id)) {
                    (true, Some(at)) => {
                        self.selected.remove(at);
                    }
                    (true, None) => self.selected.extend(self.grouped(&id)),
                    (false, Some(_)) => {}
                    (false, None) => self.selected = self.grouped(&id),
                }
                Gesture::Move {
                    from: self
                        .chosen()
                        .filter(|element| !act::locked(element))
                        .cloned()
                        .collect(),
                    at: (x, y),
                }
            }
            (None, None) => {
                if !shift {
                    self.selected.clear();
                }
                Gesture::Brush {
                    start: event.position,
                    from: (x, y),
                    to: None,
                    kept: self.selected.clone(),
                }
            }
        };
        cx.emit(Changed);
        cx.notify();
    }

    fn mouse_move(&mut self, event: &MouseMoveEvent, window: &mut Window, cx: &mut Context<Self>) {
        self.pointer = Some(event.position);
        let (x, y) = self.view.scene(self.bounds.origin, event.position);
        if self.lasering {
            self.trail.push(((x, y), Instant::now()));
            cx.notify();
            return;
        }
        let landing = || {
            self.target(x, y).map(|target| Hover {
                id: target.id.clone(),
                part: Part::Body,
            })
        };
        let hover = match &self.gesture {
            Gesture::Idle => self.hovered(event.position, (x, y)),
            Gesture::Create { from, .. } if Kind::of(from) == Kind::Arrow => landing(),
            Gesture::Tip { .. } => landing(),
            _ => None,
        };
        if hover != self.hover {
            self.hover = hover;
            cx.notify();
        }
        let held = match &self.gesture {
            Gesture::Resize { from, grip } => {
                Some(self.resize(from, *grip, (x, y), event.modifiers.shift, window))
            }
            Gesture::Tip { from, end } => Some(Ok(vec![self.tip(from, *end, (x, y))])),
            _ => None,
        };
        let previewed = match &mut self.gesture {
            Gesture::Idle => return,
            Gesture::Pan(last) => {
                self.view = self.view.panned(event.position - *last);
                *last = event.position;
                cx.notify();
                return;
            }
            Gesture::Erase(_) => {
                self.erase(x, y);
                cx.notify();
                return;
            }
            Gesture::Brush {
                start,
                from,
                to,
                kept,
            } => {
                let moved = event.position - *start;
                if to.is_none() && f32::from(moved.x).hypot(f32::from(moved.y)) < BRUSH_LEAST {
                    return;
                }
                *to = Some((x, y));
                let area = rect(*from, (x, y));
                let mut picked = kept.clone();
                picked.extend(
                    self.scene
                        .elements()
                        .iter()
                        .filter(|element| {
                            shape::container(element).is_none()
                                && !kept.contains(&element.id)
                                && shape::brushed(element, &area)
                        })
                        .map(|element| element.id.clone()),
                );
                if picked != self.selected {
                    self.selected = picked;
                    cx.emit(Changed);
                }
                cx.notify();
                return;
            }
            Gesture::Move { from, at } => edit::moved(&self.scene, from, (x - at.0, y - at.1)),
            Gesture::Resize { .. } | Gesture::Tip { .. } => held.unwrap_or(Ok(Vec::new())),
            Gesture::Bend(from) => Ok(vec![edit::dragged(from, (x, y))]),
            Gesture::Create { from, trace, .. } => {
                if Kind::of(from) != Kind::Freedraw {
                    trace.pop();
                }
                trace.push((x, y));
                Ok(vec![create::drawn(from, trace)])
            }
        };
        match previewed {
            Ok(preview) => self.preview = preview,
            Err(error) => self.fail(&error),
        }
        cx.notify();
    }

    fn menu(&mut self, event: &MouseDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        self.focus.focus(window, cx);
        self.commit(window);
        let (x, y) = self.view.scene(self.bounds.origin, event.position);
        let pressed = self.pressed(x, y);
        match &pressed {
            Some(id) if self.selected.contains(id) => {}
            Some(id) => self.selected = self.grouped(id),
            None => self.selected.clear(),
        }
        self.pointer = Some(event.position);
        cx.emit(Ask::Menu {
            at: event.position,
            shape: pressed.is_some(),
        });
        cx.emit(Changed);
        cx.notify();
    }

    fn mouse_up(&mut self, _: &MouseUpEvent, _: &mut Window, cx: &mut Context<Self>) {
        self.lasering = false;
        let gesture = std::mem::replace(&mut self.gesture, Gesture::Idle);
        let mut preview = std::mem::take(&mut self.preview);
        match gesture {
            Gesture::Create { anchor, .. } => {
                if let Some(element) = preview.pop() {
                    self.create(element, anchor, cx);
                }
                self.hover = None;
                cx.emit(Changed);
                cx.notify();
                return;
            }
            Gesture::Brush { .. } => {
                let picked = self.selected.len();
                self.timed("brush", &format!("{picked} selected"));
                cx.emit(Changed);
                cx.notify();
                return;
            }
            Gesture::Erase(ids) => {
                self.remove(ids);
                cx.emit(Changed);
                cx.notify();
                return;
            }
            Gesture::Resize { from, .. } => {
                if let Some(sized) = preview.iter().find(|element| element.id == from.id) {
                    eprintln!(
                        "desk: canvas resize {} {}x{}",
                        sized.id, sized.width, sized.height
                    );
                }
                self.timed("drag", &format!("{} moving", preview.len()));
            }
            Gesture::Move { .. } => self.timed("drag", &format!("{} moving", preview.len())),
            Gesture::Bend(from) => {
                let points = preview.first().map_or(0, |bent| shape::trace(bent).len());
                let middle = preview.first().map_or(0.0, edit::midpoint);
                eprintln!(
                    "desk: draw bend {} {points} points elbowMidPoint {middle:.3}",
                    from.id
                );
            }
            Gesture::Tip { end, .. } => {
                let onto = self.hover.take().map(|hover| hover.id);
                if let Some(arrow) = preview.pop() {
                    let id = arrow.id.clone();
                    let target = onto.as_deref().and_then(|onto| self.scene.get(onto));
                    match edit::retipped(&self.scene, arrow, end, target)
                        .and_then(|ops| self.scene.apply(ops))
                    {
                        Ok(()) => eprintln!("desk: draw: arrow {id} {end:?} bound to {onto:?}"),
                        Err(error) => self.fail(&error),
                    }
                }
                cx.emit(Changed);
                cx.notify();
                return;
            }
            Gesture::Idle | Gesture::Pan(_) => {}
        }
        if !preview.is_empty()
            && let Err(error) = self.scene.apply(edit::updates(preview))
        {
            self.fail(&error);
        }
        cx.notify();
    }

    fn resize(
        &self,
        from: &Element,
        grip: Handle,
        to: (f64, f64),
        keep_aspect: bool,
        window: &Window,
    ) -> Result<Vec<Element>, DocError> {
        let label = Kind::of(from)
            .labelled()
            .then(|| self.label_of(from))
            .flatten()
            .and_then(|id| self.scene.get(&id));
        let Some(label) = label else {
            return edit::resized(&self.scene, from, grip, to, keep_aspect, (0.0, &|_| 0.0));
        };
        let raw = shape::raw(label).unwrap_or_default();
        let font = shape::font_size(label);
        let pad = edit::LABEL_PAD * 2.0;
        let widest = raw
            .split_whitespace()
            .map(|word| measure(window, word, font))
            .fold(0.0, f64::max)
            .ceil()
            + WRAP_SLACK
            + pad;
        let tall = |width: f64| {
            let rows = wrap(window, raw, font, (width - pad).max(font))
                .split('\n')
                .count();
            shape::line_step(label) * rows as f64 + pad
        };
        let mut changed = edit::resized(&self.scene, from, grip, to, keep_aspect, (widest, &tall))?;
        let fitted = changed
            .iter()
            .find(|element| element.id == from.id)
            .map(|host| fit(window, label, raw, Some(host)).0);
        if let Some(fitted) = fitted
            && let Some(slot) = changed.iter_mut().find(|element| element.id == fitted.id)
        {
            *slot = fitted;
        }
        Ok(changed)
    }

    fn tip(&self, from: &Element, end: ArrowEnd, at: (f64, f64)) -> Element {
        let other = match end {
            ArrowEnd::Start => edit::tip(from, ArrowEnd::End),
            ArrowEnd::End => edit::tip(from, ArrowEnd::Start),
        }
        .unwrap_or(at);
        let point = self
            .target(at.0, at.1)
            .map_or(at, |target| edit::edge(target, other, BIND_GAP));
        edit::tipped(from, end, point)
    }

    fn timed(&mut self, what: &str, count: &str) {
        let mut frames = std::mem::take(&mut self.drags);
        if frames.is_empty() {
            return;
        }
        frames.sort_by(f64::total_cmp);
        let median = frames.get(frames.len() / 2).copied().unwrap_or_default();
        let worst = frames.last().copied().unwrap_or_default();
        eprintln!(
            "desk: draw {what} {} frames {count} median {median:.2} ms worst {worst:.2} ms",
            frames.len()
        );
    }

    fn scroll(&mut self, event: &ScrollWheelEvent, window: &mut Window, cx: &mut Context<Self>) {
        let delta = event.delta.pixel_delta(window.line_height());
        self.view = match event.modifiers.secondary() {
            true => {
                let zoomed = self.view.zoomed(
                    self.bounds.origin,
                    event.position,
                    (f64::from(delta.y) * ZOOM_STEP).exp(),
                );
                eprintln!("desk: canvas zoom {:.2}", zoomed.zoom());
                cx.emit(Changed);
                zoomed
            }
            false => self.view.panned(delta),
        };
        cx.stop_propagation();
        cx.notify();
    }

    fn key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let keys = &event.keystroke;
        if keys.key == "space" && self.editing.is_none() {
            cx.stop_propagation();
            self.hold_space(true, cx);
            return;
        }
        let used = match &mut self.editing {
            Some(editing) => match keys.key.as_str() {
                "enter" if keys.modifiers.shift => {
                    editing.text.push('\n');
                    true
                }
                "enter" | "escape" => {
                    self.commit(window);
                    true
                }
                "backspace" => {
                    editing.text.pop();
                    true
                }
                _ => false,
            },
            None => {
                let redo = keys.key == "y" || (keys.key == "z" && keys.modifiers.shift);
                let plain = !keys.modifiers.secondary() && !keys.modifiers.alt;
                let shift = keys.modifiers.shift;
                let act = match (keys.modifiers.secondary(), shift, keys.key.as_str()) {
                    (true, false, "c") => Some(Act::Copy),
                    (true, false, "x") => Some(Act::Cut),
                    (true, false, "v") => Some(Act::Paste),
                    (true, false, "d") => Some(Act::Duplicate),
                    (true, false, "a") => Some(Act::SelectAll),
                    (true, false, "g") => Some(Act::Group),
                    (true, true, "g") => Some(Act::Ungroup),
                    (true, true, "l") => Some(Act::Lock),
                    (true, true, "]") => Some(Act::Front),
                    (true, true, "[") => Some(Act::Back),
                    (false, true, "1" | "!") => Some(Act::Fit),
                    _ => None,
                };
                if let Some(act) = act {
                    cx.stop_propagation();
                    return self.act(act, window, cx);
                }
                let stepped = match (keys.modifiers.secondary(), keys.key.as_str()) {
                    (false, "escape") => self.cancel(cx).then_some(Ok(true)),
                    (true, _) if redo => Some(self.scene.redo()),
                    (true, "z") => Some(self.scene.undo()),
                    (false, "delete" | "backspace") => {
                        self.delete();
                        Some(Ok(true))
                    }
                    (false, "q") if plain => {
                        self.toggle_lock(cx);
                        Some(Ok(true))
                    }
                    (false, key) if plain => Tool::of_key(key).map(|tool| {
                        self.set_tool(tool, cx);
                        Ok(true)
                    }),
                    _ => None,
                };
                match stepped {
                    Some(Ok(_)) => true,
                    Some(Err(error)) => {
                        self.fail(&error);
                        true
                    }
                    None => false,
                }
            }
        };
        if used {
            cx.stop_propagation();
            cx.emit(Changed);
            cx.notify();
        }
    }

    fn cancel(&mut self, cx: &mut Context<Self>) -> bool {
        match std::mem::replace(&mut self.gesture, Gesture::Idle) {
            Gesture::Brush { kept, .. } => {
                self.selected = kept;
                self.drags.clear();
            }
            Gesture::Create { .. }
            | Gesture::Move { .. }
            | Gesture::Resize { .. }
            | Gesture::Tip { .. }
            | Gesture::Bend(_) => {
                self.preview.clear();
                self.drags.clear();
            }
            Gesture::Idle | Gesture::Pan(_) | Gesture::Erase(_) if self.tool != Tool::Select => {
                self.set_tool(Tool::Select, cx);
            }
            Gesture::Idle | Gesture::Pan(_) | Gesture::Erase(_) if !self.selected.is_empty() => {
                self.selected.clear();
            }
            Gesture::Idle | Gesture::Pan(_) | Gesture::Erase(_) => return false,
        }
        true
    }

    fn key_up(&mut self, event: &KeyUpEvent, _: &mut Window, cx: &mut Context<Self>) {
        if event.keystroke.key == "space" {
            self.hold_space(false, cx);
        }
    }

    pub fn hold_space(&mut self, held: bool, cx: &mut Context<Self>) {
        if self.space != held {
            self.space = held;
            cx.notify();
        }
    }

    fn commit(&mut self, window: &mut Window) {
        let Some(Editing { id, text, fresh }) = self.editing.take() else {
            return;
        };
        let added = fresh.is_some();
        if added && text.is_empty() {
            return;
        }
        let Some(from) = fresh
            .map(|fresh| *fresh)
            .or_else(|| self.scene.get(&id).cloned())
        else {
            return;
        };
        let host = shape::container(&from).and_then(|host| self.scene.get(host));
        let (label, host) = fit(window, &from, &text, host);
        let rows = shape::label(&label).map_or(0, |shown| shown.split('\n').count());
        let font = shape::font_size(&label);
        let ops = match added {
            true => {
                let host = host.map(|host| Op::Update(Box::new(edit::hosting(&host, &id))));
                std::iter::once(Op::Add {
                    at: None,
                    element: Box::new(label),
                })
                .chain(host)
                .collect()
            }
            false => std::iter::once(label)
                .chain(host)
                .map(|element| Op::Update(Box::new(element)))
                .collect(),
        };
        match self.scene.apply(ops) {
            Ok(()) => eprintln!(
                "desk: draw {} text {id} {} chars {rows} rows font {font}",
                match added {
                    true => "created",
                    false => "edited",
                },
                text.chars().count()
            ),
            Err(error) => self.fail(&error),
        }
    }

    fn content(&self) -> Option<Rect> {
        self.scene
            .elements()
            .iter()
            .filter(|element| element.is_deleted != Some(true))
            .map(Element::bounds)
            .reduce(|a, b| Rect {
                min_x: a.min_x.min(b.min_x),
                min_y: a.min_y.min(b.min_y),
                max_x: a.max_x.max(b.max_x),
                max_y: a.max_y.max(b.max_y),
            })
    }

    fn paint(&mut self, bounds: Bounds<Pixels>, window: &mut Window, cx: &mut Context<Self>) {
        let started = Instant::now();
        self.bounds = bounds;
        if self.fit {
            self.fit = false;
            if let Some(content) = self.content() {
                self.view = View::fit(content, bounds.size);
                cx.defer_in(window, |_, _, cx| cx.emit(Changed));
            }
        }
        let palette = Palette::new(&ActiveTheme::theme(cx), self.backdrop.color.as_deref());
        let ui = (self.backdrop.look == Look::Ui).then(|| Ink::of(&ActiveTheme::theme(cx)));
        window.paint_quad(fill(bounds, palette.ground));
        let frame = Frame {
            view: self.view,
            origin: bounds.origin,
            palette: &palette,
            soft: Cell::new(0.0),
        };
        let visible = self.view.visible(bounds);
        let editing = self.editing.as_ref().map(|editing| editing.id.as_str());
        let erased: &[String] = match &self.gesture {
            Gesture::Erase(ids) => ids,
            _ => &[],
        };
        let mask = ContentMask {
            bounds,
            ..Default::default()
        };
        let typed = self.editing.as_ref().and_then(|editing| {
            let element = editing
                .fresh
                .as_deref()
                .or_else(|| self.scene.get(&editing.id))?;
            let host = shape::container(element).and_then(|id| self.scene.get(id));
            Some(fit(window, element, &editing.text, host))
        });
        window.with_content_mask(Some(mask), |window| {
            if self.backdrop.dots {
                frame.dots(bounds, window);
            }
            let mut batch = Batch::default();
            for element in self.scene.cull(visible) {
                if erased.contains(&element.id) {
                    continue;
                }
                let shown = match &typed {
                    Some((_, Some(host))) if host.id == element.id => host,
                    _ => self.shown(element),
                };
                if Kind::of(shown) == Kind::Image {
                    let image = self.picture(shown).and_then(|path| {
                        window
                            .use_asset::<ImgResourceLoader>(&Resource::Path(path.into()), cx)?
                            .ok()
                    });
                    frame.picture(shown, image, window);
                    continue;
                }
                let edited = editing == Some(shown.id.as_str());
                if !edited
                    && let Some(ink) = &ui
                    && theme::ui(&frame, shown, &self.scene, ink, window, cx)
                {
                    continue;
                }
                if edited || !frame.sketch(shown, &mut batch, window) {
                    frame.element(shown, edited, window, cx);
                }
            }
            batch.paint(window);
            for moved in &self.preview {
                let culled = self
                    .scene
                    .get(&moved.id)
                    .is_some_and(|element| element.bounds().intersects(&visible));
                if culled {
                    continue;
                }
                let themed = ui
                    .as_ref()
                    .is_some_and(|ink| theme::ui(&frame, moved, &self.scene, ink, window, cx));
                if !themed {
                    frame.element(moved, false, window, cx);
                }
            }
            for selected in self.chosen() {
                frame.selection(self.shown(selected), window);
            }
            if let Some(lone) = self.lone().filter(|_| self.tool == Tool::Select) {
                let lone = self.shown(lone);
                if Kind::of(lone).resizable() {
                    frame.grips(lone, window);
                }
                if Kind::of(lone) == Kind::Arrow {
                    let least = ELBOW_HANDLE_LEAST / self.view.zoom();
                    let knobs = [
                        edit::tip(lone, ArrowEnd::Start),
                        edit::tip(lone, ArrowEnd::End),
                        edit::bend_point(lone, least),
                    ];
                    for at in knobs.into_iter().flatten() {
                        frame.knob(frame.view.screen(frame.origin, at.0, at.1), window);
                    }
                }
            }
            if let Gesture::Brush {
                from, to: Some(to), ..
            } = &self.gesture
            {
                frame.brush(&rect(*from, *to), window);
            }
            if let Some(hover) = &self.hover
                && let Some(target) = self.scene.get(&hover.id)
            {
                match (&self.gesture, hover.part) {
                    (Gesture::Create { .. } | Gesture::Tip { .. }, _) => {
                        frame.highlight(target, window, cx)
                    }
                    (Gesture::Idle, Part::Connect(side)) => {
                        frame.connectors(target, Some(side), window)
                    }
                    (Gesture::Idle, Part::Body) => frame.connectors(target, None, window),
                    _ => {}
                }
            }
            if let Some((label, host)) = &typed {
                let shown = shape::label(label).unwrap_or_default();
                frame.field(label, shown, host.as_ref(), window, cx);
            }
            let fade = LASER_FADE.as_secs_f32();
            let lit: Vec<((f64, f64), f32)> = self
                .trail
                .iter()
                .map(|(at, since)| (*at, 1.0 - since.elapsed().as_secs_f32() / fade))
                .collect();
            frame.laser(&lit, window);
        });
        self.trail.retain(|(_, since)| since.elapsed() < LASER_FADE);
        if !self.trail.is_empty() {
            window.request_animation_frame();
        }
        let soft = frame.soft.get();
        if soft > 0.0 {
            eprintln!("desk: draw: soft paint {soft:.2}ms");
        }
        let spent = started.elapsed().as_secs_f64() * 1000.0;
        if let Gesture::Brush { to: Some(_), .. } = self.gesture {
            eprintln!("desk: draw: frame {spent:.2}ms");
            self.drags.push(spent);
        }
        if let Gesture::Move { .. } | Gesture::Resize { .. } = self.gesture
            && !self.preview.is_empty()
        {
            eprintln!(
                "desk: draw drag frame {spent:.2} ms {} moving",
                self.preview.len()
            );
            self.drags.push(spent);
        }
        self.paints.push(spent);
        if self.paints.len() >= PAINT_WINDOW {
            self.paints.sort_by(f64::total_cmp);
            let median = self
                .paints
                .get(PAINT_WINDOW / 2)
                .copied()
                .unwrap_or_default();
            let worst = self.paints.last().copied().unwrap_or_default();
            eprintln!(
                "desk: canvas paint {} shapes median {median:.2} ms worst {worst:.2} ms",
                self.scene.elements().len()
            );
            self.paints.clear();
        }
    }

    fn shown<'a>(&'a self, element: &'a Element) -> &'a Element {
        self.preview
            .iter()
            .find(|moved| moved.id == element.id)
            .unwrap_or(element)
    }
}

impl EntityInputHandler for Canvas {
    fn text_for_range(
        &mut self,
        _: Range<usize>,
        _: &mut Option<Range<usize>>,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<String> {
        None
    }

    fn selected_text_range(
        &mut self,
        _: bool,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<UTF16Selection> {
        None
    }

    fn marked_text_range(&self, _: &mut Window, _: &mut Context<Self>) -> Option<Range<usize>> {
        None
    }

    fn unmark_text(&mut self, _: &mut Window, _: &mut Context<Self>) {}

    fn replace_text_in_range(
        &mut self,
        _: Option<Range<usize>>,
        text: &str,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if let Some(editing) = &mut self.editing {
            editing
                .text
                .extend(text.chars().filter(|typed| !typed.is_control()));
            cx.notify();
        }
    }

    fn replace_and_mark_text_in_range(
        &mut self,
        _: Option<Range<usize>>,
        _: &str,
        _: Option<Range<usize>>,
        _: &mut Window,
        _: &mut Context<Self>,
    ) {
    }

    fn bounds_for_range(
        &mut self,
        _: Range<usize>,
        element_bounds: Bounds<Pixels>,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<Bounds<Pixels>> {
        Some(element_bounds)
    }

    fn character_index_for_point(
        &mut self,
        _: Point<Pixels>,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<usize> {
        None
    }

    fn accepts_text_input(&self, _: &mut Window, _: &mut Context<Self>) -> bool {
        self.editing.is_some()
    }
}

impl Focusable for Canvas {
    fn focus_handle(&self, _: &App) -> FocusHandle {
        self.focus.clone()
    }
}

impl Render for Canvas {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let this = cx.entity();
        let focus = self.focus.clone();
        div()
            .id("draw-canvas")
            .track_focus(&self.focus)
            .relative()
            .size_full()
            .overflow_hidden()
            .cursor(self.cursor())
            .on_mouse_down(MouseButton::Left, cx.listener(Self::mouse_down))
            .on_mouse_move(cx.listener(Self::mouse_move))
            .on_hover(cx.listener(|canvas, inside: &bool, _, _| {
                if !inside {
                    canvas.pointer = None;
                }
            }))
            .on_mouse_down(MouseButton::Right, cx.listener(Self::menu))
            .on_mouse_up(MouseButton::Left, cx.listener(Self::mouse_up))
            .on_mouse_up_out(MouseButton::Left, cx.listener(Self::mouse_up))
            .on_mouse_down(
                MouseButton::Middle,
                cx.listener(|canvas, event: &MouseDownEvent, _, _| {
                    canvas.gesture = Gesture::Pan(event.position);
                }),
            )
            .on_mouse_up(MouseButton::Middle, cx.listener(Self::mouse_up))
            .on_mouse_up_out(MouseButton::Middle, cx.listener(Self::mouse_up))
            .on_scroll_wheel(cx.listener(Self::scroll))
            .on_key_down(cx.listener(Self::key))
            .on_key_up(cx.listener(Self::key_up))
            .child(
                canvas(
                    |_, _, _| (),
                    move |bounds, (), window, cx| {
                        window.handle_input(
                            &focus,
                            ElementInputHandler::new(bounds, this.clone()),
                            cx,
                        );
                        this.update(cx, |canvas, cx| canvas.paint(bounds, window, cx))
                    },
                )
                .size_full(),
            )
            .children(self.failure.clone().map(|failure| {
                div()
                    .absolute()
                    .right_3()
                    .bottom_3()
                    .text_color(
                        ActiveTheme::theme(cx).color(desk_ui::theme::ColorToken::StatusDanger),
                    )
                    .child(failure)
            }))
    }
}
