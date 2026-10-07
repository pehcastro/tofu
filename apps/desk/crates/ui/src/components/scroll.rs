use std::time::{Duration, Instant};

use desk_motion::tokens::{EASE_OUT, HOVER_MS, PANEL_OUT_MS};
use gpui::{
    AnyElement, App, Bounds, Context, DispatchPhase, ElementId, Entity, ListState, MouseButton,
    MouseDownEvent, MouseMoveEvent, MouseUpEvent, Pixels, Result, ScrollHandle, Task, Window,
    canvas, div, fill, millis, point, prelude::*, px, size,
};

use crate::components::paint::tint;
use crate::live::ActiveTheme;
use crate::theme::ColorToken;

const LINGER: Duration = millis(600);
const THUMB_WIDTH: f32 = 4.0;
const THUMB_HOT: f32 = 6.0;
const THUMB_INSET: f32 = 2.0;
const THUMB_MIN: f32 = 24.0;
const TAIL_SLACK: f32 = 1.0;

#[derive(Clone, Copy)]
struct Fade {
    from: f32,
    to: f32,
    since: Instant,
}

impl Fade {
    fn span(&self) -> Duration {
        if self.to > self.from {
            HOVER_MS
        } else {
            PANEL_OUT_MS
        }
    }

    fn at(&self, now: Instant) -> f32 {
        let t = now.saturating_duration_since(self.since).as_secs_f32() / self.span().as_secs_f32();
        self.from + (self.to - self.from) * EASE_OUT(t.clamp(0.0, 1.0))
    }

    fn moving(&self, now: Instant) -> bool {
        now.saturating_duration_since(self.since) < self.span() && self.from != self.to
    }

    fn go(&mut self, to: f32, now: Instant) {
        if to != self.to {
            *self = Fade {
                from: self.at(now),
                to,
                since: now,
            };
        }
    }
}

#[derive(Clone)]
enum Track {
    Handle(ScrollHandle),
    List(ListState),
}

impl Track {
    fn bounds(&self) -> Bounds<Pixels> {
        match self {
            Track::Handle(handle) => handle.bounds(),
            Track::List(list) => list.viewport_bounds(),
        }
    }

    fn offset(&self) -> Pixels {
        match self {
            Track::Handle(handle) => handle.offset().y,
            Track::List(list) => list.scroll_px_offset_for_scrollbar().y,
        }
    }

    fn max(&self) -> Pixels {
        match self {
            Track::Handle(handle) => handle.max_offset().y,
            Track::List(list) => list.max_offset_for_scrollbar().y,
        }
    }

    fn set(&self, offset: Pixels) {
        match self {
            Track::Handle(handle) => handle.set_offset(point(handle.offset().x, offset)),
            Track::List(list) => list.set_offset_from_scrollbar(point(px(0.0), offset)),
        }
    }
}

struct Viewport {
    track: Track,
    hovered: bool,
    pinned: bool,
    bar: Fade,
    hide: Option<Task<Result<()>>>,
    grab: Option<Pixels>,
    thumb_hovered: bool,
}

impl Viewport {
    fn wake(&mut self, cx: &mut Context<Self>) {
        self.bar.go(1.0, Instant::now());
        self.hide = (!self.hovered && self.grab.is_none()).then(|| {
            cx.spawn(async move |this, cx| {
                cx.background_executor().timer(LINGER).await;
                this.update(cx, |viewport, cx| {
                    if !viewport.hovered && viewport.grab.is_none() {
                        viewport.bar.go(0.0, Instant::now());
                        cx.notify();
                    }
                })
            })
        });
        cx.notify();
    }
}

#[derive(IntoElement)]
pub struct ScrollArea {
    id: ElementId,
    max_h: Option<f32>,
    follow_tail: bool,
    children: Vec<AnyElement>,
}

impl ScrollArea {
    pub fn new(id: impl Into<ElementId>) -> Self {
        ScrollArea {
            id: id.into(),
            max_h: None,
            follow_tail: false,
            children: Vec::new(),
        }
    }

    pub fn max_h(mut self, height: f32) -> Self {
        self.max_h = Some(height);
        self
    }

    pub fn follow_tail(mut self, follow: bool) -> Self {
        self.follow_tail = follow;
        self
    }
}

impl ParentElement for ScrollArea {
    fn extend(&mut self, elements: impl IntoIterator<Item = AnyElement>) {
        self.children.extend(elements);
    }
}

fn thumb(view: Bounds<Pixels>, offset: Pixels, max: Pixels) -> Bounds<Pixels> {
    let height = view.size.height;
    let track = height - px(2.0 * THUMB_INSET);
    let length = (track * (height / (height + max)))
        .max(px(THUMB_MIN))
        .min(track);
    let travel = track - length;
    let progress = (-offset / max).clamp(0.0, 1.0);
    Bounds::new(
        point(
            view.right() - px(THUMB_INSET + THUMB_WIDTH),
            view.top() + px(THUMB_INSET) + travel * progress,
        ),
        size(px(THUMB_WIDTH), length),
    )
}

fn rail(view: Bounds<Pixels>) -> Bounds<Pixels> {
    Bounds::new(
        point(view.right() - px(THUMB_HOT + THUMB_INSET), view.top()),
        size(px(THUMB_HOT + THUMB_INSET), view.size.height),
    )
}

fn offset_at(view: Bounds<Pixels>, thumb_top: Pixels, max: Pixels) -> Pixels {
    let bar = thumb(view, px(0.0), max);
    let travel = view.size.height - px(2.0 * THUMB_INSET) - bar.size.height;
    if travel <= px(0.0) {
        return px(0.0);
    }
    let progress = ((thumb_top - bar.top()) / travel).clamp(0.0, 1.0);
    -max * progress
}

fn settle(viewport: &mut Viewport, offset: Pixels, cx: &mut Context<Viewport>) {
    let max = viewport.track.max();
    let offset = offset.clamp(-max, px(0.0));
    viewport.track.set(offset);
    viewport.pinned = offset <= -max + px(TAIL_SLACK);
    viewport.wake(cx);
}

fn listen(window: &mut Window, state: &Entity<Viewport>, track: &Track) {
    let down_state = state.clone();
    let down_track = track.clone();
    window.on_mouse_event(move |event: &MouseDownEvent, phase, _, cx| {
        let view = down_track.bounds();
        if phase != DispatchPhase::Bubble
            || event.button != MouseButton::Left
            || !rail(view).contains(&event.position)
        {
            return;
        }
        cx.stop_propagation();
        down_state.update(cx, |viewport, cx| {
            let max = viewport.track.max();
            let offset = viewport.track.offset();
            let bar = thumb(view, offset, max);
            let y = event.position.y;
            if y < bar.top() {
                settle(viewport, offset + view.size.height, cx);
            } else if y > bar.bottom() {
                settle(viewport, offset - view.size.height, cx);
            } else {
                viewport.grab = Some(y - bar.top());
                viewport.wake(cx);
            }
        });
    });
    let move_state = state.clone();
    let move_track = track.clone();
    window.on_mouse_event(move |event: &MouseMoveEvent, phase, _, cx| {
        if phase != DispatchPhase::Bubble {
            return;
        }
        let view = move_track.bounds();
        let grab = move_state.read(cx).grab;
        match grab {
            Some(grab) if event.dragging() => {
                cx.stop_propagation();
                move_state.update(cx, |viewport, cx| {
                    let max = viewport.track.max();
                    settle(viewport, offset_at(view, event.position.y - grab, max), cx);
                });
            }
            Some(_) => move_state.update(cx, |viewport, cx| {
                viewport.grab = None;
                viewport.wake(cx);
            }),
            None => {
                let over = rail(view).contains(&event.position);
                move_state.update(cx, |viewport, cx| {
                    if viewport.thumb_hovered != over {
                        viewport.thumb_hovered = over;
                        cx.notify();
                    }
                });
            }
        }
    });
    let up_state = state.clone();
    window.on_mouse_event(move |event: &MouseUpEvent, phase, _, cx| {
        if phase != DispatchPhase::Bubble || event.button != MouseButton::Left {
            return;
        }
        up_state.update(cx, |viewport, cx| {
            if viewport.grab.take().is_some() {
                cx.stop_propagation();
                viewport.wake(cx);
            }
        });
    });
}

fn viewport(id: ElementId, track: Track, window: &mut Window, cx: &mut App) -> Entity<Viewport> {
    window.use_keyed_state(id, cx, |_, _| Viewport {
        track,
        hovered: false,
        pinned: true,
        bar: Fade {
            from: 0.0,
            to: 0.0,
            since: Instant::now(),
        },
        hide: None,
        grab: None,
        thumb_hovered: false,
    })
}

fn bar(state: Entity<Viewport>, window: &mut Window, cx: &App) -> impl IntoElement + use<> {
    let now = Instant::now();
    let viewport = state.read(cx);
    let track = viewport.track.clone();
    let opacity = viewport.bar.at(now);
    let hot = viewport.thumb_hovered || viewport.grab.is_some();
    if viewport.bar.moving(now) {
        window.request_animation_frame();
    }
    let color = ActiveTheme::theme(cx).color(ColorToken::Scrollbar);
    canvas(
        |_, _, _| {},
        move |_, (), window, _| {
            let max = track.max();
            if max <= px(0.0) {
                return;
            }
            listen(window, &state, &track);
            if opacity <= 0.0 {
                return;
            }
            let bounds = thumb(track.bounds(), track.offset(), max);
            let grow = if hot { THUMB_HOT - THUMB_WIDTH } else { 0.0 };
            let bounds = Bounds::new(
                point(bounds.left() - px(grow), bounds.top()),
                size(bounds.size.width + px(grow), bounds.size.height),
            );
            window.paint_quad(
                fill(bounds, tint(color, color.alpha * opacity))
                    .corner_radii(bounds.size.width / 2.0),
            );
        },
    )
    .absolute()
    .top_0()
    .left_0()
    .size_full()
}

#[derive(IntoElement)]
pub struct Scrollbar {
    id: ElementId,
    track: Track,
}

pub fn scrollbar(id: impl Into<ElementId>, handle: &ScrollHandle) -> Scrollbar {
    Scrollbar {
        id: id.into(),
        track: Track::Handle(handle.clone()),
    }
}

pub fn list_scrollbar(id: impl Into<ElementId>, list: &ListState) -> Scrollbar {
    Scrollbar {
        id: id.into(),
        track: Track::List(list.clone()),
    }
}

impl RenderOnce for Scrollbar {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let state = viewport(self.id.clone(), self.track, window, cx);
        let wheel_state = state.clone();
        let hover_state = state.clone();
        div()
            .id(self.id)
            .absolute()
            .top_0()
            .left_0()
            .size_full()
            .on_hover(move |hovered, _, cx| {
                hover_state.update(cx, |viewport, cx| {
                    viewport.hovered = *hovered;
                    viewport.wake(cx);
                })
            })
            .on_scroll_wheel(move |_, _, cx| {
                wheel_state.update(cx, |viewport, cx| viewport.wake(cx))
            })
            .child(bar(state, window, cx))
    }
}

impl RenderOnce for ScrollArea {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let handle = window
            .use_keyed_state(ElementId::from((self.id.clone(), "handle")), cx, |_, _| {
                ScrollHandle::new()
            })
            .read(cx)
            .clone();
        let state = viewport(self.id.clone(), Track::Handle(handle.clone()), window, cx);
        let pinned = state.read(cx).pinned;
        if self.follow_tail && pinned {
            handle.scroll_to_bottom();
        }
        let wheel_state = state.clone();
        let hover_state = state.clone();
        let overlay = bar(state, window, cx);
        div()
            .id(self.id)
            .relative()
            .flex()
            .flex_col()
            .w_full()
            .when(self.max_h.is_none(), |area| {
                area.flex_1().h_full().min_h_0()
            })
            .on_hover(move |hovered, _, cx| {
                hover_state.update(cx, |viewport, cx| {
                    viewport.hovered = *hovered;
                    viewport.wake(cx);
                })
            })
            .on_scroll_wheel(move |_, _, cx| {
                wheel_state.update(cx, |viewport, cx| {
                    let max = viewport.track.max();
                    if max > px(0.0) {
                        cx.stop_propagation();
                    }
                    viewport.pinned = viewport.track.offset() <= -max + px(TAIL_SLACK);
                    viewport.wake(cx);
                })
            })
            .child(
                div()
                    .id("scroll-viewport")
                    .w_full()
                    .map(|viewport| match self.max_h {
                        Some(height) => viewport.max_h(px(height)),
                        None => viewport.flex_1().min_h_0(),
                    })
                    .overflow_y_scroll()
                    .track_scroll(&handle)
                    .children(self.children),
            )
            .child(overlay)
    }
}
