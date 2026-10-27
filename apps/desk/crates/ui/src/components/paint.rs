use std::collections::HashMap;
use std::f32::consts::{FRAC_PI_2, FRAC_PI_4, TAU};
use std::time::{Duration, Instant};

use desk_motion::tokens::{PRESS, PRESS_MS};
use gpui::{
    App, Bounds, BoxShadow, ContentMask, Context, Div, ElementId, Entity, Global, KeyDownEvent,
    Motion, PathBuilder, Pixels, Rgba, Stateful, Subscription, Svg, WeakEntity, Window, WindowId,
    canvas, div, point, prelude::*, px, rgb_to_hsla, rgba, svg,
};

use crate::components::glyph::Glyph;
use crate::components::size::{PRESS_FILL, RING_STROKE, RISE};
use crate::theme::{ColorToken, NumberToken, Theme};

pub fn ink(theme: &Theme, alpha: f32) -> Rgba {
    tint(theme.color(ColorToken::TextStrong), alpha)
}

pub fn tint(color: Rgba, alpha: f32) -> Rgba {
    Rgba { alpha, ..color }
}

pub fn solid(over: Rgba, base: Rgba) -> Rgba {
    let mix = |top: f32, bottom: f32| top * over.alpha + bottom * (1.0 - over.alpha);
    Rgba::new(
        mix(over.red, base.red),
        mix(over.green, base.green),
        mix(over.blue, base.blue),
        1.0,
    )
}

pub fn ms(theme: &Theme, token: NumberToken) -> Duration {
    Duration::from_secs_f32(theme.number(token) / 1000.0)
}

pub fn ring(color: Rgba) -> BoxShadow {
    BoxShadow {
        color: color.into(),
        offset: point(px(0.0), px(0.0)),
        blur_radius: px(0.0),
        spread_radius: px(1.0),
        inset: true,
    }
}

pub fn focus_ring(element: Stateful<Div>, theme: &Theme) -> Stateful<Div> {
    let color = theme.color(ColorToken::FocusRing);
    element
        .tab_index(0)
        .focus_visible(move |style| style.shadow(vec![ring(color)]))
}

pub fn arrowed(event: &KeyDownEvent, at: usize, count: usize) -> Option<usize> {
    let keys = &event.keystroke;
    match (keys.modifiers.modified(), keys.key.as_str()) {
        (false, "left") => Some(at.saturating_sub(1)),
        (false, "right") => Some(at.saturating_add(1).min(count.saturating_sub(1))),
        _ => None,
    }
}

pub fn halo(color: Rgba, spread: f32) -> BoxShadow {
    BoxShadow {
        color: color.into(),
        offset: point(px(0.0), px(0.0)),
        blur_radius: px(0.0),
        spread_radius: px(spread),
        inset: false,
    }
}

pub fn drop(color: Rgba, y: f32, blur: f32) -> BoxShadow {
    BoxShadow {
        color: color.into(),
        offset: point(px(0.0), px(y)),
        blur_radius: px(blur),
        spread_radius: px(0.0),
        inset: false,
    }
}

pub fn top_light(color: Rgba) -> BoxShadow {
    BoxShadow {
        color: color.into(),
        offset: point(px(0.0), px(1.0)),
        blur_radius: px(0.0),
        spread_radius: px(0.0),
        inset: true,
    }
}

pub fn glyph(glyph: Glyph, size: f32, color: Rgba) -> Svg {
    svg()
        .path(glyph.path())
        .size(px(size))
        .flex_none()
        .text_color(rgb_to_hsla(color))
}

pub fn presented(element: Div, progress: f32, reduced: bool) -> Div {
    let rise = if reduced { 0.0 } else { RISE };
    element.opacity(progress).mt(px(rise * (1.0 - progress)))
}

pub fn spinning_arc(
    id: impl Into<ElementId>,
    size: f32,
    track: Rgba,
    arc: Rgba,
    theme: &Theme,
) -> impl IntoElement {
    Spinner::new(id, size, track, arc, theme)
}

#[derive(IntoElement)]
pub struct Spinner {
    id: ElementId,
    size: f32,
    track: Rgba,
    mark: Rgba,
    period: Duration,
    still: bool,
}

impl Spinner {
    pub fn new(
        id: impl Into<ElementId>,
        size: f32,
        track: Rgba,
        mark: Rgba,
        theme: &Theme,
    ) -> Self {
        Self {
            id: id.into(),
            size,
            track,
            mark,
            period: ms(theme, NumberToken::MotionSpin),
            still: false,
        }
    }

    pub fn still(mut self, still: bool) -> Self {
        self.still = still;
        self
    }
}

fn paint_arc(bounds: Bounds<Pixels>, turn: f32, arc: &SpinArc, window: &mut Window) {
    let (radius, center) = (arc.radius, bounds.center());
    let at = |angle: f32| {
        point(
            center.x + px(radius * angle.cos()),
            center.y + px(radius * angle.sin()),
        )
    };
    let start = TAU * turn - 3.0 * FRAC_PI_4;
    let mut path = PathBuilder::stroke(px(RING_STROKE));
    path.move_to(at(start));
    path.arc_to(
        point(px(radius), px(radius)),
        px(0.0),
        false,
        true,
        at(start + FRAC_PI_2),
    );
    if let Ok(path) = path.build() {
        window.paint_path(path, arc.mark);
    }
}

const SPIN_STEPS_PER_SECOND: f64 = 12.0;
const SPIN_WAKE_SLACK: Duration = Duration::from_millis(2);

struct SpinEpoch(Instant);

impl Global for SpinEpoch {}

#[derive(Clone, Copy)]
struct SpinClock {
    step: f64,
    wait: Duration,
}

impl SpinClock {
    fn now(cx: &mut App) -> Self {
        let now = cx.background_executor().now();
        let epoch = match cx.try_global::<SpinEpoch>() {
            Some(epoch) => epoch.0,
            None => {
                cx.set_global(SpinEpoch(now));
                now
            }
        };
        let since = now.saturating_duration_since(epoch) + SPIN_WAKE_SLACK;
        let step = (since.as_secs_f64() * SPIN_STEPS_PER_SECOND).floor();
        let next = epoch + Duration::from_secs_f64((step + 1.0) / SPIN_STEPS_PER_SECOND);
        SpinClock {
            step,
            wait: next.saturating_duration_since(now),
        }
    }

    fn turn(self, period: Duration) -> f32 {
        let steps = (period.as_secs_f64() * SPIN_STEPS_PER_SECOND)
            .round()
            .max(1.0);
        ((self.step % steps) / steps) as f32
    }
}

#[derive(Clone, Copy)]
struct SpinArc {
    radius: f32,
    mark: Rgba,
    period: Duration,
}

struct SpinSpot {
    arc: SpinArc,
    at: Option<(Bounds<Pixels>, ContentMask<Pixels>)>,
    woken: bool,
}

#[derive(Default)]
struct SpinLayers(HashMap<WindowId, WeakEntity<SpinLayer>>);

impl Global for SpinLayers {}

pub struct SpinLayer {
    spots: Vec<(WeakEntity<SpinSpot>, Subscription)>,
    painted: bool,
    ticking: bool,
}

impl SpinLayer {
    pub fn new(window: &Window, cx: &mut App) -> Entity<Self> {
        let layer = cx.new(|_| SpinLayer {
            spots: Vec::new(),
            painted: false,
            ticking: false,
        });
        let layers = &mut cx.default_global::<SpinLayers>().0;
        layers.retain(|_, kept| kept.upgrade().is_some());
        layers.insert(window.window_handle().window_id(), layer.downgrade());
        layer
    }

    fn of(window: &Window, cx: &App) -> Option<Entity<Self>> {
        let layers = &cx.try_global::<SpinLayers>()?.0;
        layers.get(&window.window_handle().window_id())?.upgrade()
    }

    fn keep(&mut self, spot: &Entity<SpinSpot>, cx: &mut Context<Self>) {
        let id = spot.entity_id();
        if self.spots.iter().any(|(kept, _)| kept.entity_id() == id) {
            return;
        }
        let released = cx.observe_release(spot, move |layer, _, cx| {
            layer.spots.retain(|(kept, _)| kept.entity_id() != id);
            cx.notify();
        });
        self.spots.push((spot.downgrade(), released));
    }

    fn paint(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.painted = true;
        let clock = SpinClock::now(cx);
        let mut drawn = false;
        for (spot, _) in &self.spots {
            let Some(spot) = spot.upgrade() else {
                continue;
            };
            let spot = spot.read(cx);
            if let Some((bounds, mask)) = spot.at {
                drawn = true;
                window.with_content_mask(Some(mask), |window| {
                    paint_arc(bounds, clock.turn(spot.arc.period), &spot.arc, window)
                });
            }
        }
        if !drawn || self.ticking || cx.reduce_motion() {
            return;
        }
        self.ticking = true;
        cx.spawn(async move |layer, cx| {
            cx.background_executor().timer(clock.wait).await;
            let stepped = layer.update(cx, |layer, cx| {
                layer.ticking = false;
                cx.notify();
            });
            if stepped.is_err() {
                eprintln!("desk_ui: the spinner layer closed before its next step");
            }
        })
        .detach();
    }
}

impl Render for SpinLayer {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        self.painted = false;
        let layer = cx.entity();
        canvas(
            |_, _, _| {},
            move |_, (), window, cx| layer.update(cx, |layer, cx| layer.paint(window, cx)),
        )
        .absolute()
        .size_full()
    }
}

fn spin_in_place(
    spot: &Entity<SpinSpot>,
    bounds: Bounds<Pixels>,
    window: &mut Window,
    cx: &mut App,
) {
    let clock = SpinClock::now(cx);
    let (arc, woken) = {
        let spot = spot.read(cx);
        (spot.arc, spot.woken)
    };
    paint_arc(bounds, clock.turn(arc.period), &arc, window);
    if woken {
        return;
    }
    spot.update(cx, |spot, _| spot.woken = true);
    let (spot, view) = (spot.downgrade(), window.current_view());
    window
        .spawn(cx, async move |cx| {
            cx.background_executor().timer(clock.wait).await;
            let stepped = cx.update(|_, cx| {
                if spot.update(cx, |spot, _| spot.woken = false).is_ok() {
                    cx.notify(view);
                }
            });
            if stepped.is_err() {
                eprintln!("desk_ui: a spinner's window closed before its next step");
            }
        })
        .detach();
}

fn spin(spot: &Entity<SpinSpot>, bounds: Bounds<Pixels>, window: &mut Window, cx: &mut App) {
    match SpinLayer::of(window, cx).filter(|layer| !layer.read(cx).painted) {
        Some(_) => {
            let mask = window.content_mask();
            spot.update(cx, |spot, _| spot.at = Some((bounds, mask)));
        }
        None => spin_in_place(spot, bounds, window, cx),
    }
}

impl RenderOnce for Spinner {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let arc = SpinArc {
            radius: (self.size - RING_STROKE) / 2.0,
            mark: self.mark,
            period: self.period,
        };
        let spot = (!self.still && !cx.reduce_motion()).then(|| {
            let spot = window.use_keyed_state(self.id, cx, |_, _| SpinSpot {
                arc,
                at: None,
                woken: false,
            });
            spot.update(cx, |spot, _| {
                spot.arc = arc;
                spot.at = None;
            });
            if let Some(layer) = SpinLayer::of(window, cx) {
                layer.update(cx, |layer, cx| layer.keep(&spot, cx));
            }
            spot
        });
        let face = canvas(
            |_, _, _| {},
            move |bounds, (), window, cx| match &spot {
                Some(spot) => spin(spot, bounds, window, cx),
                None => paint_arc(bounds, 0.0, &arc, window),
            },
        )
        .size_full();
        div()
            .relative()
            .flex_none()
            .size(px(self.size))
            .rounded_full()
            .border(px(RING_STROKE))
            .border_color(self.track)
            .child(div().absolute().inset(px(-RING_STROKE)).child(face))
    }
}

const PRESS_SINK: f32 = 1.0;
const PRESS_SHADE: f32 = 0.16;

pub fn pressed(element: Stateful<Div>, _backdrop: Rgba) -> Stateful<Div> {
    pressed_by(element, None)
}

pub fn pressed_by(element: Stateful<Div>, group: Option<&'static str>) -> Stateful<Div> {
    let spring = || Motion::new(PRESS_MS).with_spring(PRESS);
    let shade = |alpha| tint(rgba(0x000000ff), alpha);
    let held = move |style: gpui::StyleRefinement| {
        style
            .top(px(PRESS_SINK))
            .inset_ring_color(shade(PRESS_SHADE))
    };
    let element = sinkable(element)
        .inset_ring(px(PRESS_FILL))
        .inset_ring_color(shade(0.0))
        .transitions(|transitions| transitions.top(spring()).inset_ring_color(spring()));
    match group {
        Some(group) => element.group_active(group, held),
        None => element.active(held),
    }
}

fn sinkable(mut element: Stateful<Div>) -> Stateful<Div> {
    if element.style().inset.top.is_none() {
        element = element.top(px(0.0));
    }
    element
}
