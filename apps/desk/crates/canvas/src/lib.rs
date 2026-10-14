mod edit;
mod paint;
mod shape;
mod view;

use std::time::Instant;

use desk_ui::live::ActiveTheme;
use draw_doc::{BoundKind, DocError, Element, Rect, Scene};
use gpui::{
    App, Bounds, ContentMask, Context, FocusHandle, Focusable, KeyDownEvent, MouseButton,
    MouseDownEvent, MouseMoveEvent, MouseUpEvent, Pixels, Point, ScrollWheelEvent, SharedString,
    TextRun, Window, canvas, div, fill, prelude::*, px,
};

use edit::Corner;
use paint::{Frame, Palette};
use shape::Kind;
pub use view::View;

const HIT_SLACK: f64 = 6.0;
const ZOOM_STEP: f64 = 0.002;
const PAINT_WINDOW: usize = 120;

pub enum Framing {
    Fit,
    At(View),
}

enum Gesture {
    Idle,
    Pan(Point<Pixels>),
    Move { from: Box<Element>, at: (f64, f64) },
    Resize { from: Box<Element>, corner: Corner },
}

struct Editing {
    id: String,
    text: String,
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
}

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

    fn grabbed_corner(&self, at: Point<Pixels>) -> Option<(Element, Corner)> {
        let element = self.selected.as_deref().and_then(|id| self.scene.get(id))?;
        if !Kind::of(element).resizable() {
            return None;
        }
        Corner::ALL
            .into_iter()
            .find(|corner| {
                paint::handle(&self.view, self.bounds.origin, element, *corner).contains(&at)
            })
            .map(|corner| (element.clone(), corner))
    }

    fn mouse_down(&mut self, event: &MouseDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        self.focus.focus(window, cx);
        self.commit(window);
        let (x, y) = self.view.scene(self.bounds.origin, event.position);
        if event.click_count >= 2 {
            let label = self.hit(x, y).and_then(|element| self.label_of(element));
            self.editing = label.and_then(|id| {
                let text = self
                    .scene
                    .get(&id)
                    .and_then(shape::label)?
                    .replace('\n', " ");
                Some(Editing { id, text })
            });
            cx.notify();
            return;
        }
        self.gesture = match self.grabbed_corner(event.position) {
            Some((from, corner)) => Gesture::Resize {
                from: Box::new(from),
                corner,
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
                    Gesture::Pan(event.position)
                }
            },
        };
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
            Gesture::Resize { from, corner } => edit::resized(&self.scene, from, *corner, (x, y)),
        };
        match previewed {
            Ok(preview) => self.preview = preview,
            Err(error) => self.fail(&error),
        }
        cx.notify();
    }

    fn mouse_up(&mut self, _: &MouseUpEvent, _: &mut Window, cx: &mut Context<Self>) {
        self.gesture = Gesture::Idle;
        let preview = std::mem::take(&mut self.preview);
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
            true => self.view.zoomed(
                self.bounds.origin,
                event.position,
                (-f64::from(delta.y) * ZOOM_STEP).exp(),
            ),
            false => self.view.panned(delta),
        };
        cx.stop_propagation();
        cx.notify();
    }

    fn key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let keys = &event.keystroke;
        let used = match &mut self.editing {
            Some(editing) => match keys.key.as_str() {
                "enter" => {
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
                _ => match (&keys.key_char, keys.modifiers.secondary()) {
                    (Some(typed), false) => {
                        editing.text.push_str(typed);
                        true
                    }
                    _ => false,
                },
            },
            None => {
                let redo = keys.key == "y" || (keys.key == "z" && keys.modifiers.shift);
                let stepped = match (keys.modifiers.secondary(), keys.key.as_str()) {
                    (true, _) if redo => Some(self.scene.redo()),
                    (true, "z") => Some(self.scene.undo()),
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
            cx.notify();
        }
    }

    fn commit(&mut self, window: &mut Window) {
        let Some(Editing { id, text }) = self.editing.take() else {
            return;
        };
        let Some(from) = self.scene.get(&id).cloned() else {
            return;
        };
        let size = px(shape::font_size(&from) as f32);
        let run = TextRun {
            len: text.len(),
            font: window.text_style().font(),
            color: gpui::black(),
            background_color: None,
            underline: None,
            strikethrough: None,
            letter_spacing: None,
        };
        let width = window
            .text_system()
            .shape_line(SharedString::from(text.clone()), size, &[run], None)
            .width();
        let committed = edit::retexted(&self.scene, &from, &text, f64::from(width))
            .and_then(|changed| self.scene.apply(edit::updates(changed)));
        if let Err(error) = committed {
            self.fail(&error);
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
            for element in self.scene.cull(visible) {
                let shown = self.shown(element);
                frame.element(shown, editing == Some(shown.id.as_str()), window, cx);
            }
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
                && let Some(element) = self.scene.get(&editing.id)
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
            eprintln!(
                "desk: canvas paint {} shapes {median:.2} ms",
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

impl Focusable for Canvas {
    fn focus_handle(&self, _: &App) -> FocusHandle {
        self.focus.clone()
    }
}

impl Render for Canvas {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let this = cx.entity();
        div()
            .id("draw-canvas")
            .track_focus(&self.focus)
            .relative()
            .size_full()
            .overflow_hidden()
            .on_mouse_down(MouseButton::Left, cx.listener(Self::mouse_down))
            .on_mouse_move(cx.listener(Self::mouse_move))
            .on_mouse_up(MouseButton::Left, cx.listener(Self::mouse_up))
            .on_mouse_up_out(MouseButton::Left, cx.listener(Self::mouse_up))
            .on_scroll_wheel(cx.listener(Self::scroll))
            .on_key_down(cx.listener(Self::key))
            .child(
                canvas(
                    |_, _, _| (),
                    move |bounds, (), window, cx| {
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
