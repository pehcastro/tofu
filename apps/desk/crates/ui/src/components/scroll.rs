use std::time::{Duration, Instant};

use desk_motion::tokens::{EASE_OUT, HOVER_MS, PANEL_OUT_MS};
use gpui::{
    Along, AnyElement, App, Axis, Bounds, Context, DispatchPhase, ElementId, Entity, ListState,
    MouseButton, MouseDownEvent, MouseMoveEvent, MouseUpEvent, Pixels, Result, ScrollHandle, Task,
    Window, canvas, div, fill, millis, point, prelude::*, px, size,
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
enum Source {
    Handle(ScrollHandle),
    List(ListState),
}

#[derive(Clone)]
struct Track {
    source: Source,
    axis: Axis,
}

impl Track {
    fn bounds(&self) -> Bounds<Pixels> {
        match &self.source {
            Source::Handle(handle) => handle.bounds(),
            Source::List(list) => list.viewport_bounds(),
        }
    }

    fn offset(&self) -> Pixels {
        match &self.source {
            Source::Handle(handle) => handle.offset().along(self.axis),
            Source::List(list) => list.scroll_px_offset_for_scrollbar().along(self.axis),
        }
    }

    fn max(&self) -> Pixels {
        match &self.source {
            Source::Handle(handle) => handle.max_offset().along(self.axis),
            Source::List(list) => list.max_offset_for_scrollbar().along(self.axis),
        }
    }

    fn set(&self, offset: Pixels) {
        match &self.source {
            Source::Handle(handle) => {
                handle.set_offset(handle.offset().apply_along(self.axis, |_| offset))
            }
            Source::List(list) => list.set_offset_from_scrollbar(
                point(px(0.0), px(0.0)).apply_along(self.axis, |_| offset),
            ),
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
    handle: Option<ScrollHandle>,
    children: Vec<AnyElement>,
}

impl ScrollArea {
    pub fn new(id: impl Into<ElementId>) -> Self {
        ScrollArea {
            id: id.into(),
            max_h: None,
            follow_tail: false,
            handle: None,
            children: Vec::new(),
        }
    }

    pub fn track(mut self, handle: &ScrollHandle) -> Self {
        self.handle = Some(handle.clone());
        self
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

fn lay(axis: Axis, along: Pixels, across: Pixels, length: Pixels, thick: Pixels) -> Bounds<Pixels> {
    match axis {
        Axis::Vertical => Bounds::new(point(across, along), size(thick, length)),
        Axis::Horizontal => Bounds::new(point(along, across), size(length, thick)),
    }
}

fn far_edge(view: Bounds<Pixels>, axis: Axis) -> Pixels {
    view.origin.along(axis.invert()) + view.size.along(axis.invert())
}

fn track_length(view: Bounds<Pixels>, axis: Axis) -> Pixels {
    let corner = match axis {
        Axis::Vertical => px(0.0),
        Axis::Horizontal => px(THUMB_HOT + THUMB_INSET),
    };
    view.size.along(axis) - px(2.0 * THUMB_INSET) - corner
}

fn thumb(view: Bounds<Pixels>, axis: Axis, offset: Pixels, max: Pixels) -> Bounds<Pixels> {
    let extent = view.size.along(axis);
    let track = track_length(view, axis);
    let length = (track * (extent / (extent + max)))
        .max(px(THUMB_MIN))
        .min(track);
    let progress = (-offset / max).clamp(0.0, 1.0);
    lay(
        axis,
        view.origin.along(axis) + px(THUMB_INSET) + (track - length) * progress,
        far_edge(view, axis) - px(THUMB_INSET + THUMB_WIDTH),
        length,
        px(THUMB_WIDTH),
    )
}

fn rail(view: Bounds<Pixels>, axis: Axis) -> Bounds<Pixels> {
    let thick = px(THUMB_HOT + THUMB_INSET);
    lay(
        axis,
        view.origin.along(axis),
        far_edge(view, axis) - thick,
        view.size.along(axis),
        thick,
    )
}

fn offset_at(view: Bounds<Pixels>, axis: Axis, thumb_start: Pixels, max: Pixels) -> Pixels {
    let bar = thumb(view, axis, px(0.0), max);
    let travel = track_length(view, axis) - bar.size.along(axis);
    if travel <= px(0.0) {
        return px(0.0);
    }
    let progress = ((thumb_start - bar.origin.along(axis)) / travel).clamp(0.0, 1.0);
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
        let axis = down_track.axis;
        if phase != DispatchPhase::Bubble
            || event.button != MouseButton::Left
            || !rail(view, axis).contains(&event.position)
        {
            return;
        }
        cx.stop_propagation();
        down_state.update(cx, |viewport, cx| {
            let max = viewport.track.max();
            let offset = viewport.track.offset();
            let bar = thumb(view, axis, offset, max);
            let at = event.position.along(axis);
            let start = bar.origin.along(axis);
            let page = view.size.along(axis);
            if at < start {
                settle(viewport, offset + page, cx);
            } else if at > start + bar.size.along(axis) {
                settle(viewport, offset - page, cx);
            } else {
                viewport.grab = Some(at - start);
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
        let axis = move_track.axis;
        let grab = move_state.read(cx).grab;
        match grab {
            Some(grab) if event.dragging() => {
                cx.stop_propagation();
                move_state.update(cx, |viewport, cx| {
                    let max = viewport.track.max();
                    let start = event.position.along(axis) - grab;
                    settle(viewport, offset_at(view, axis, start, max), cx);
                });
            }
            Some(_) => move_state.update(cx, |viewport, cx| {
                viewport.grab = None;
                viewport.wake(cx);
            }),
            None => {
                let over = rail(view, axis).contains(&event.position);
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
            let axis = track.axis;
            let resting = thumb(track.bounds(), axis, track.offset(), max);
            let grow = px(if hot { THUMB_HOT - THUMB_WIDTH } else { 0.0 });
            let across = axis.invert();
            let bounds = Bounds::new(
                resting.origin.apply_along(across, |at| at - grow),
                resting.size.apply_along(across, |thick| thick + grow),
            );
            window.paint_quad(
                fill(bounds, tint(color, color.alpha * opacity))
                    .corner_radii(bounds.size.along(across) / 2.0),
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
        track: Track {
            source: Source::Handle(handle.clone()),
            axis: Axis::Vertical,
        },
    }
}

pub fn list_scrollbar(id: impl Into<ElementId>, list: &ListState) -> Scrollbar {
    Scrollbar {
        id: id.into(),
        track: Track {
            source: Source::List(list.clone()),
            axis: Axis::Vertical,
        },
    }
}

impl Scrollbar {
    pub fn axis(mut self, axis: Axis) -> Self {
        self.track.axis = axis;
        self
    }
}

impl RenderOnce for Scrollbar {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let axis = self.track.axis;
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
            .on_scroll_wheel(move |event, window, cx| {
                let delta = event.delta.pixel_delta(window.line_height());
                if delta.along(axis) != px(0.0) {
                    wheel_state.update(cx, |viewport, cx| viewport.wake(cx))
                }
            })
            .child(bar(state, window, cx))
    }
}

impl RenderOnce for ScrollArea {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let handle = match self.handle {
            Some(handle) => handle,
            None => window
                .use_keyed_state(ElementId::from((self.id.clone(), "handle")), cx, |_, _| {
                    ScrollHandle::new()
                })
                .read(cx)
                .clone(),
        };
        let track = Track {
            source: Source::Handle(handle.clone()),
            axis: Axis::Vertical,
        };
        let state = viewport(self.id.clone(), track, window, cx);
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
