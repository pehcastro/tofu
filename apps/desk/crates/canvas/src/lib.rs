mod create;
mod edit;
mod paint;
mod shape;
mod style;
mod view;

use std::time::Instant;

use desk_ui::live::ActiveTheme;
use draw_doc::{ArrowEnd, BoundKind, DocError, Element, Op, Rect, Scene};
use gpui::{
    App, Bounds, ContentMask, Context, ElementInputHandler, EntityInputHandler, EventEmitter,
    FocusHandle, Focusable, KeyDownEvent, MouseButton, MouseDownEvent, MouseMoveEvent,
    MouseUpEvent, Pixels, Point, ScrollWheelEvent, SharedString, TextRun, UTF16Selection, Window,
    canvas, div, fill, prelude::*, px,
};
use std::ops::Range;

use create::Mint;
pub use create::Tool;
use edit::Handle;
use paint::{Batch, Frame, Palette};
use shape::Kind;
use style::Style;
pub use style::{BACKGROUNDS, FILLS, LINES, Pick, STROKES, WIDTHS};
pub use view::View;

const HIT_SLACK: f64 = 6.0;
const HANDLE_REACH: f32 = 8.0;
const ZOOM_STEP: f64 = 0.002;
const PAINT_WINDOW: usize = 120;
const LEAST_DRAG: f64 = 3.0;
const BIND_GAP: f64 = 6.0;

pub enum Framing {
    Fit,
    At(View),
}

pub struct ToolChanged;

enum Gesture {
    Idle,
    Pan(Point<Pixels>),
    Move {
        from: Box<Element>,
        at: (f64, f64),
    },
    Resize {
        from: Box<Element>,
        grip: Handle,
    },
    Create {
        from: Box<Element>,
        trace: Vec<(f64, f64)>,
    },
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
    selected: Option<String>,
    gesture: Gesture,
    preview: Vec<Element>,
    editing: Option<Editing>,
    failure: Option<SharedString>,
    focus: FocusHandle,
    paints: Vec<f64>,
    tool: Tool,
    locked: bool,
    mint: Mint,
    style: Style,
}

impl EventEmitter<ToolChanged> for Canvas {}

impl Canvas {
    pub fn new(scene: Scene, cx: &mut Context<Self>) -> Self {
        Canvas {
            scene,
            view: View::new(0.0, 0.0, 1.0),
            fit: true,
            bounds: Bounds::default(),
            selected: None,
            gesture: Gesture::Idle,
            preview: Vec::new(),
            editing: None,
            failure: None,
            focus: cx.focus_handle(),
            paints: Vec::with_capacity(PAINT_WINDOW),
            tool: Tool::Select,
            locked: false,
            mint: Mint::new(),
            style: style::DEFAULT,
        }
    }

    pub fn scene(&self) -> &Scene {
        &self.scene
    }

    pub fn apply(&mut self, ops: Vec<Op>, cx: &mut Context<Self>) -> Result<(), DocError> {
        self.scene.apply(ops)?;
        if let Some(id) = &self.selected
            && self.scene.get(id).is_none()
        {
            self.selected = None;
        }
        cx.notify();
        Ok(())
    }

    pub fn fit(&mut self, to: Rect, cx: &mut Context<Self>) -> View {
        self.view = View::fit(to, self.bounds.size);
        self.fit = false;
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
        self.tool = tool;
        cx.emit(ToolChanged);
        cx.notify();
    }

    pub fn toggle_lock(&mut self, cx: &mut Context<Self>) {
        self.locked = !self.locked;
        cx.emit(ToolChanged);
        cx.notify();
    }

    pub fn typing(&self) -> bool {
        self.editing.is_some()
    }

    pub fn styling(&self) -> bool {
        self.tool != Tool::Select || self.selected.is_some()
    }

    pub fn picked(&self, pick: Pick) -> bool {
        match self.selected.as_deref().and_then(|id| self.scene.get(id)) {
            Some(element) => pick.on(element),
            None => self.style.get(pick.slot()) == Some(&pick),
        }
    }

    pub fn restyle(&mut self, pick: Pick, window: &mut Window, cx: &mut Context<Self>) {
        self.focus.focus(window, cx);
        if let Some(slot) = self.style.get_mut(pick.slot()) {
            *slot = pick;
        }
        if let Some(element) = self.selected.as_deref().and_then(|id| self.scene.get(id)) {
            let mut styled = element.clone();
            styled.extra.insert(pick.field().to_owned(), pick.value());
            match self.scene.apply(vec![Op::Update(Box::new(styled))]) {
                Ok(()) => eprintln!(
                    "desk: draw styled 1 shapes {}={}",
                    pick.field(),
                    pick.value()
                ),
                Err(error) => self.fail(&error),
            }
        }
        cx.emit(ToolChanged);
        cx.notify();
    }

    fn created(&mut self, cx: &mut Context<Self>) {
        if !self.locked {
            self.set_tool(Tool::Select, cx);
        }
    }

    fn target(&self, x: f64, y: f64) -> Option<&Element> {
        let slack = HIT_SLACK / self.view.zoom();
        self.scene.elements().iter().rev().find(|element| {
            Kind::of(element).resizable()
                && shape::container(element).is_none()
                && shape::hit(element, x, y, slack)
        })
    }

    fn create(&mut self, element: Element, cx: &mut Context<Self>) {
        let drag = element.width.hypot(element.height) * self.view.zoom();
        if drag < LEAST_DRAG {
            return;
        }
        let id = element.id.clone();
        let kind = element.kind.clone();
        let mut binds = Vec::with_capacity(2);
        let element = match Kind::of(&element) {
            Kind::Arrow => {
                let drawn = shape::trace(&element);
                let mut trace = drawn.clone();
                let last = drawn.len().saturating_sub(1);
                for (end, at, near) in [
                    (ArrowEnd::Start, 0, 1.min(last)),
                    (ArrowEnd::End, last, last.saturating_sub(1)),
                ] {
                    let (Some(&(x, y)), Some(&toward)) = (drawn.get(at), drawn.get(near)) else {
                        continue;
                    };
                    let Some(target) = self.target(x, y) else {
                        continue;
                    };
                    let outline = edit::edge(target, toward, BIND_GAP);
                    if let Some(slot) = trace.get_mut(at) {
                        *slot = outline;
                    }
                    let share =
                        |at: f64, start: f64, span: f64| (at - start) / span.max(f64::EPSILON);
                    binds.push(Op::BindArrow {
                        arrow: id.clone(),
                        end,
                        target: target.id.clone(),
                        fixed_point: [
                            share(outline.0, target.x, target.width),
                            share(outline.1, target.y, target.height),
                        ],
                    });
                }
                shape::with_trace(&element, &trace)
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
                self.selected = Some(id);
                self.created(cx);
            }
            Err(error) => self.fail(&error),
        }
    }

    fn delete(&mut self) {
        let Some(element) = self.selected.as_deref().and_then(|id| self.scene.get(id)) else {
            return;
        };
        let mut ids = vec![element.id.clone()];
        ids.extend(self.label_of(element).filter(|label| *label != element.id));
        let ops = ids.into_iter().map(|id| Op::Delete { id }).collect();
        match self.scene.apply(ops) {
            Ok(()) => self.selected = None,
            Err(error) => self.fail(&error),
        }
    }

    pub fn show(&mut self, scene: Scene, framing: Framing, cx: &mut Context<Self>) {
        self.scene = scene;
        (self.view, self.fit) = match framing {
            Framing::Fit => (self.view, true),
            Framing::At(view) => (view, false),
        };
        self.selected = None;
        self.gesture = Gesture::Idle;
        self.preview.clear();
        self.editing = None;
        self.failure = None;
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

    fn grabbed(&self, at: Point<Pixels>) -> Option<(Element, Handle)> {
        let element = self.selected.as_deref().and_then(|id| self.scene.get(id))?;
        if !Kind::of(element).resizable() {
            return None;
        }
        let reach = px(HANDLE_REACH);
        Handle::ALL
            .into_iter()
            .find(|grip| {
                let (x, y) = grip.of(element);
                let middle = self.view.screen(self.bounds.origin, x, y);
                (at.x - middle.x).abs() <= reach && (at.y - middle.y).abs() <= reach
            })
            .map(|grip| (element.clone(), grip))
    }

    fn mouse_down(&mut self, event: &MouseDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        self.focus.focus(window, cx);
        self.commit(window);
        let (x, y) = self.view.scene(self.bounds.origin, event.position);
        if event.click_count >= 2 {
            let label = self.hit(x, y).and_then(|element| self.label_of(element));
            self.editing = label.and_then(|id| {
                let text = self.scene.get(&id).and_then(shape::label)?.to_owned();
                Some(Editing {
                    id,
                    text,
                    fresh: None,
                })
            });
            cx.notify();
            return;
        }
        if let Some(from) = create::fresh(self.tool, &mut self.mint, &self.style, (x, y)) {
            self.selected = None;
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
                    };
                }
            }
            cx.notify();
            return;
        }
        self.gesture = match self.grabbed(event.position) {
            Some((from, grip)) => Gesture::Resize {
                from: Box::new(from),
                grip,
            },
            None => match self.hit(x, y).cloned() {
                Some(element) => {
                    self.selected = Some(element.id.clone());
                    Gesture::Move {
                        from: Box::new(element),
                        at: (x, y),
                    }
                }
                None => {
                    self.selected = None;
                    Gesture::Idle
                }
            },
        };
        cx.emit(ToolChanged);
        cx.notify();
    }

    fn mouse_move(&mut self, event: &MouseMoveEvent, _: &mut Window, cx: &mut Context<Self>) {
        let (x, y) = self.view.scene(self.bounds.origin, event.position);
        let previewed = match &mut self.gesture {
            Gesture::Idle => return,
            Gesture::Pan(last) => {
                self.view = self.view.panned(event.position - *last);
                *last = event.position;
                cx.notify();
                return;
            }
            Gesture::Move { from, at } => edit::moved(&self.scene, from, (x - at.0, y - at.1)),
            Gesture::Resize { from, grip } => {
                edit::resized(&self.scene, from, *grip, (x, y), event.modifiers.shift)
            }
            Gesture::Create { from, trace } => {
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

    fn mouse_up(&mut self, _: &MouseUpEvent, _: &mut Window, cx: &mut Context<Self>) {
        let gesture = std::mem::replace(&mut self.gesture, Gesture::Idle);
        let mut preview = std::mem::take(&mut self.preview);
        if let Gesture::Create { .. } = gesture {
            if let Some(element) = preview.pop() {
                self.create(element, cx);
            }
            cx.emit(ToolChanged);
            cx.notify();
            return;
        }
        if let Gesture::Resize { from, .. } = gesture
            && let Some(sized) = preview.iter().find(|element| element.id == from.id)
        {
            eprintln!(
                "desk: canvas resize {} {}x{}",
                sized.id, sized.width, sized.height
            );
        }
        if !preview.is_empty()
            && let Err(error) = self.scene.apply(edit::updates(preview))
        {
            self.fail(&error);
        }
        cx.notify();
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
                zoomed
            }
            false => self.view.panned(delta),
        };
        cx.stop_propagation();
        cx.notify();
    }

    fn key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let keys = &event.keystroke;
        let used = match &mut self.editing {
            Some(editing) => match keys.key.as_str() {
                "enter" if keys.modifiers.shift => {
                    editing.text.push('\n');
                    true
                }
                "enter" => {
                    self.commit(window);
                    true
                }
                "escape" if editing.fresh.is_some() => {
                    self.commit(window);
                    true
                }
                "escape" => {
                    self.editing = None;
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
                let stepped = match (keys.modifiers.secondary(), keys.key.as_str()) {
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
            cx.emit(ToolChanged);
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
        let size = px(shape::font_size(&from) as f32);
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
                    .shape_line(SharedString::from(line.to_owned()), size, &[run], None)
                    .width()
            })
            .fold(px(0.0), Pixels::max);
        let committed =
            edit::retexted(&self.scene, &from, &text, f64::from(width)).and_then(|changed| {
                let ops = match added {
                    true => changed
                        .into_iter()
                        .map(|element| Op::Add {
                            at: None,
                            element: Box::new(element),
                        })
                        .collect(),
                    false => edit::updates(changed),
                };
                self.scene.apply(ops)
            });
        match committed {
            Ok(()) if added => eprintln!("desk: draw created text {id}"),
            Ok(()) => {}
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
            }
        }
        let palette = Palette::new(&ActiveTheme::theme(cx));
        window.paint_quad(fill(bounds, palette.ground));
        let frame = Frame {
            view: self.view,
            origin: bounds.origin,
            palette: &palette,
        };
        let visible = self.view.visible(bounds);
        let editing = self.editing.as_ref().map(|editing| editing.id.as_str());
        let mask = ContentMask {
            bounds,
            ..Default::default()
        };
        window.with_content_mask(Some(mask), |window| {
            let mut batch = Batch::default();
            for element in self.scene.cull(visible) {
                let shown = self.shown(element);
                let edited = editing == Some(shown.id.as_str());
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
                if !culled {
                    frame.element(moved, false, window, cx);
                }
            }
            if let Some(selected) = self.selected.as_deref().and_then(|id| self.scene.get(id)) {
                frame.selection(self.shown(selected), window);
            }
            if let Some(editing) = &self.editing
                && let Some(element) = editing
                    .fresh
                    .as_deref()
                    .or_else(|| self.scene.get(&editing.id))
            {
                frame.field(element, &editing.text, window, cx);
            }
        });
        self.paints.push(started.elapsed().as_secs_f64() * 1000.0);
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
            .when(self.tool != Tool::Select, |area| area.cursor_crosshair())
            .on_mouse_down(MouseButton::Left, cx.listener(Self::mouse_down))
            .on_mouse_move(cx.listener(Self::mouse_move))
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
                    .left_2()
                    .bottom_2()
                    .text_color(
                        ActiveTheme::theme(cx).color(desk_ui::theme::ColorToken::StatusDanger),
                    )
                    .child(failure)
            }))
    }
}
