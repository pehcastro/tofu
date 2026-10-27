use std::ops::Range;
use std::sync::Arc;

use gpui::{
    App, Bounds, Canvas, Context, DispatchPhase, Div, FontWeight, HitboxBehavior, Pixels, Point,
    Rgba, ScrollWheelEvent, SharedString, Stateful, TextAlign, UniformListScrollHandle, WeakEntity,
    Window, canvas, div, fill, point, prelude::*, px, rgb, rgb_to_hsla, size, uniform_list,
};

use crate::components::charts::{LILAC, MINT};
use crate::components::paint::{ink, tint};
use crate::components::size::{T1, T2, T3};
use crate::components::tooltip::{Edge, tooltip};
use crate::live::ActiveTheme;
use crate::theme::{ColorToken, Theme};

pub const ROW: f32 = 28.0;
const TREE: f32 = 255.0;
const TREE_PAD: f32 = 8.0;
const INDENT: f32 = 13.0;
const BAR: f32 = 14.0;
const BAR_RADIUS: f32 = 3.0;
const BAR_LEAST: f32 = 2.0;
const BAR_ALPHA: f32 = 0.8;
const GROUP_FILL: f32 = 0.2;
const GRID: f32 = 0.06;
const LABEL_ROOM: f32 = 96.0;
const LABEL_GAP: f32 = 6.0;
const LABEL_TEXT: f32 = 11.5;
const AXIS_TEXT: f32 = 11.0;
const NOW_DOT: f32 = 5.0;
const TICKS_MOST: i64 = 6;
const ZOOM_PER_PIXEL: f32 = 0.004;
const NARROWEST_MS: i64 = 1_000;
const DAY_MS: i64 = 86_400_000;
const STEPS_MS: [i64; 18] = [
    1_000, 2_000, 5_000, 10_000, 15_000, 30_000, 60_000, 120_000, 300_000, 600_000, 900_000,
    1_800_000, 3_600_000, 7_200_000, 10_800_000, 21_600_000, 43_200_000, DAY_MS,
];

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum SpanKind {
    Group,
    Model,
    Tool,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Span {
    pub name: SharedString,
    pub detail: SharedString,
    pub depth: usize,
    pub start_ms: i64,
    pub length_ms: i64,
    pub kind: SpanKind,
    pub failed: bool,
    pub live: bool,
    pub label: SharedString,
}

impl Span {
    fn end_ms(&self) -> i64 {
        self.start_ms.saturating_add(self.length_ms.max(0))
    }
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Timeline {
    spans: Arc<[Span]>,
    end_ms: i64,
    now_ms: Option<i64>,
    clock_ms: Option<i64>,
}

impl Timeline {
    pub fn new(spans: Vec<Span>, now_ms: Option<i64>, clock_ms: Option<i64>) -> Self {
        let last = spans
            .iter()
            .map(Span::end_ms)
            .chain(now_ms)
            .max()
            .unwrap_or(0)
            .max(1);
        let step_ms = step_for(last);
        Timeline {
            spans: spans.into(),
            end_ms: last
                .saturating_add(step_ms - 1)
                .saturating_div(step_ms)
                .saturating_mul(step_ms)
                .max(1),
            now_ms,
            clock_ms,
        }
    }

    pub fn len(&self) -> usize {
        self.spans.len()
    }

    pub fn is_empty(&self) -> bool {
        self.spans.is_empty()
    }

    pub fn end_ms(&self) -> i64 {
        self.end_ms
    }
}

pub struct Waterfall {
    timeline: Timeline,
    from_ms: i64,
    to_ms: i64,
    rows: UniformListScrollHandle,
}

impl Default for Waterfall {
    fn default() -> Self {
        Waterfall {
            timeline: Timeline::default(),
            from_ms: 0,
            to_ms: 1,
            rows: UniformListScrollHandle::new(),
        }
    }
}

#[derive(Clone, Copy, Debug)]
struct Axis {
    from_ms: i64,
    to_ms: i64,
    step_ms: i64,
    now_ms: Option<i64>,
    clock_ms: Option<i64>,
}

impl Waterfall {
    pub fn show(&mut self, timeline: Timeline, cx: &mut Context<Self>) {
        if timeline == self.timeline {
            return;
        }
        let whole = self.from_ms <= 0 && self.to_ms >= self.timeline.end_ms;
        let end = timeline.end_ms;
        (self.from_ms, self.to_ms) = match whole {
            true => (0, end),
            false => fit(self.from_ms, self.to_ms.saturating_sub(self.from_ms), end),
        };
        self.timeline = timeline;
        cx.notify();
    }

    fn axis(&self) -> Axis {
        Axis {
            from_ms: self.from_ms,
            to_ms: self.to_ms.max(self.from_ms.saturating_add(1)),
            step_ms: step_for(self.to_ms.saturating_sub(self.from_ms)),
            now_ms: self.timeline.now_ms,
            clock_ms: self.timeline.clock_ms,
        }
    }

    fn wheel(
        &mut self,
        delta: Point<Pixels>,
        zoom: bool,
        x: Pixels,
        lane: Bounds<Pixels>,
        cx: &mut Context<Self>,
    ) -> bool {
        let width = f32::from(lane.size.width).max(1.0);
        let shown = self.to_ms.saturating_sub(self.from_ms).max(1);
        let end = self.timeline.end_ms;
        let (from, length) = match zoom {
            true => {
                let turned = match delta.y == Pixels::ZERO {
                    true => f32::from(delta.x),
                    false => f32::from(delta.y),
                };
                let at = (f32::from(x - lane.left()) / width).clamp(0.0, 1.0);
                let pivot = self.from_ms as f32 + at * shown as f32;
                let length = (shown as f32 * (-turned * ZOOM_PER_PIXEL).exp()) as i64;
                let length = length.clamp(NARROWEST_MS.min(end), end);
                ((pivot - at * length as f32) as i64, length)
            }
            false if delta.x.abs() > delta.y.abs() => {
                let moved = (f32::from(delta.x) / width * shown as f32) as i64;
                (self.from_ms.saturating_sub(moved), shown)
            }
            false => return false,
        };
        let (from_ms, to_ms) = fit(from, length, end);
        if (from_ms, to_ms) != (self.from_ms, self.to_ms) {
            (self.from_ms, self.to_ms) = (from_ms, to_ms);
            cx.notify();
        }
        true
    }

    fn header(axis: Axis, theme: &Theme) -> Div {
        let text = ink(theme, T3);
        let live = theme.color(ColorToken::StatusLive);
        div()
            .h(px(ROW))
            .flex()
            .flex_none()
            .items_center()
            .text_size(px(AXIS_TEXT))
            .text_color(text)
            .child(div().w(px(TREE)).flex_none().pl(px(TREE_PAD)).child("SPAN"))
            .child(axis.lane(theme, move |bounds, window, cx| {
                for ms in axis.ticks() {
                    let x = across(bounds, axis.share(ms)) + px(LABEL_GAP);
                    label(bounds, x, axis.said(ms).into(), AXIS_TEXT, text, window, cx);
                }
                if let Some(now) = axis.now_ms.filter(|now| axis.holds(*now)) {
                    let dot = Bounds::new(
                        point(
                            across(bounds, axis.share(now)) - px(NOW_DOT / 2.0),
                            bounds.bottom() - px(NOW_DOT),
                        ),
                        size(px(NOW_DOT), px(NOW_DOT)),
                    );
                    window.paint_quad(fill(dot, live).corner_radii(px(NOW_DOT / 2.0)));
                }
            }))
    }

    fn row(
        &self,
        at: usize,
        axis: Axis,
        theme: &Theme,
        window: &mut Window,
        cx: &mut App,
    ) -> Option<Stateful<Div>> {
        let span = self.spans().get(at)?;
        let from = axis.share(span.start_ms);
        let to = axis.share(span.end_ms());
        let danger = theme.color(ColorToken::StatusDanger);
        let bar = match (span.failed, span.kind) {
            (true, _) => danger,
            (false, SpanKind::Group) => ink(theme, GROUP_FILL),
            (false, SpanKind::Model) => tint(rgb(LILAC), BAR_ALPHA),
            (false, SpanKind::Tool) => tint(rgb(MINT), BAR_ALPHA),
        };
        let text = match (span.live, span.failed) {
            (_, true) => danger,
            (true, false) => theme.color(ColorToken::StatusLive),
            (false, false) => ink(theme, T3),
        };
        let said = span.label.clone();
        let line = div()
            .id(("waterfall-row", at))
            .w_full()
            .h(px(ROW))
            .flex()
            .items_center()
            .child(Self::tree(span, theme))
            .child(axis.lane(theme, move |bounds, window, cx| {
                if to < 0.0 || from > 1.0 {
                    return;
                }
                let left = across(bounds, from.max(0.0));
                let end = across(bounds, to.min(1.0));
                let shape = Bounds::new(
                    point(left, bounds.top() + px((ROW - BAR) / 2.0)),
                    size((end - left).max(px(BAR_LEAST)), px(BAR)),
                );
                window.paint_quad(fill(shape, bar).corner_radii(px(BAR_RADIUS)));
                if to <= 1.0 {
                    label(
                        bounds,
                        end + px(LABEL_GAP),
                        said,
                        LABEL_TEXT,
                        text,
                        window,
                        cx,
                    );
                }
            }));
        Some(tooltip(
            ("waterfall-tip", at),
            line,
            Edge::Frame,
            axis.told(span),
            theme,
            window,
            cx,
        ))
    }

    fn spans(&self) -> &[Span] {
        &self.timeline.spans
    }

    fn tree(span: &Span, theme: &Theme) -> Div {
        let indent = INDENT * span.depth as f32;
        let name: Rgba = if span.failed {
            theme.color(ColorToken::StatusDanger)
        } else {
            ink(theme, T1)
        };
        div()
            .w(px(TREE))
            .flex_none()
            .flex()
            .items_center()
            .gap(px(6.0))
            .pl(px(TREE_PAD + indent))
            .pr(px(TREE_PAD))
            .min_w_0()
            .child(
                div()
                    .flex_none()
                    .font_weight(match span.kind {
                        SpanKind::Group => FontWeight::SEMIBOLD,
                        SpanKind::Model | SpanKind::Tool => FontWeight::MEDIUM,
                    })
                    .text_color(name)
                    .child(span.name.clone()),
            )
            .child(
                div()
                    .flex_shrink_1()
                    .min_w_0()
                    .truncate()
                    .text_color(ink(theme, T2))
                    .child(span.detail.clone()),
            )
    }
}

impl Render for Waterfall {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let axis = self.axis();
        let this = cx.entity().downgrade();
        div()
            .id("waterfall")
            .occlude()
            .relative()
            .size_full()
            .flex()
            .flex_col()
            .child(Self::header(axis, &theme))
            .child(
                uniform_list(
                    "waterfall-rows",
                    self.timeline.len(),
                    cx.processor(|waterfall: &mut Self, range: Range<usize>, window, cx| {
                        let theme = ActiveTheme::theme(cx);
                        let axis = waterfall.axis();
                        range
                            .filter_map(|at| waterfall.row(at, axis, &theme, window, cx))
                            .collect::<Vec<_>>()
                    }),
                )
                .track_scroll(&self.rows)
                .flex_1()
                .w_full(),
            )
            .child(wheel_catcher(this))
    }
}

fn wheel_catcher(this: WeakEntity<Waterfall>) -> Canvas<gpui::Hitbox> {
    canvas(
        |bounds, window, _| window.insert_hitbox(bounds, HitboxBehavior::Normal),
        move |bounds, hitbox, window, _| {
            let lane = Bounds::from_corners(
                point(bounds.left() + px(TREE), bounds.top()),
                point(
                    (bounds.right() - px(LABEL_ROOM)).max(bounds.left() + px(TREE + 1.0)),
                    bounds.bottom(),
                ),
            );
            window.on_mouse_event(move |event: &ScrollWheelEvent, phase, window, cx| {
                if phase != DispatchPhase::Capture || !hitbox.should_handle_scroll(window) {
                    return;
                }
                let delta = event.delta.pixel_delta(window.line_height());
                let zoom = event.modifiers.control;
                let x = event.position.x;
                let handled = this
                    .update(cx, |waterfall, cx| {
                        waterfall.wheel(delta, zoom, x, lane, cx)
                    })
                    .unwrap_or(false);
                if handled {
                    cx.stop_propagation();
                }
            });
        },
    )
    .absolute()
    .top_0()
    .left_0()
    .size_full()
}

impl Axis {
    fn lane(
        self,
        theme: &Theme,
        marks: impl FnOnce(Bounds<Pixels>, &mut Window, &mut App) + 'static,
    ) -> Canvas<()> {
        let grid = ink(theme, GRID);
        let live = theme.color(ColorToken::StatusLive);
        canvas(
            |_, _, _| {},
            move |bounds, (), window, cx| {
                for ms in self.ticks() {
                    window.paint_quad(fill(rule(bounds, self.share(ms)), grid));
                }
                if let Some(now) = self.now_ms.filter(|now| self.holds(*now)) {
                    window.paint_quad(fill(rule(bounds, self.share(now)), live));
                }
                marks(bounds, window, cx);
            },
        )
        .flex_1()
        .h_full()
        .mr(px(LABEL_ROOM))
    }

    fn ticks(self) -> impl Iterator<Item = i64> {
        let step = self.step_ms.max(1);
        let base = self.clock_ms.unwrap_or(0).saturating_add(self.from_ms);
        let first = self
            .from_ms
            .saturating_add((step - base.rem_euclid(step)).rem_euclid(step));
        (0..)
            .map(move |tick: i64| first.saturating_add(tick.saturating_mul(step)))
            .take_while(move |ms| *ms <= self.to_ms)
    }

    fn holds(self, ms: i64) -> bool {
        (self.from_ms..=self.to_ms).contains(&ms)
    }

    fn share(self, ms: i64) -> f32 {
        ms.saturating_sub(self.from_ms) as f32 / self.to_ms.saturating_sub(self.from_ms) as f32
    }

    fn said(self, ms: i64) -> String {
        match self.clock_ms {
            Some(clock) => wall(clock.saturating_add(ms), self.step_ms < 60_000),
            None => duration(ms),
        }
    }

    fn told(self, span: &Span) -> String {
        let lasted = duration(span.length_ms);
        let started = match self.clock_ms {
            Some(clock) => format!(
                "started {}",
                wall(clock.saturating_add(span.start_ms), true)
            ),
            None => format!("starts at {}", duration(span.start_ms)),
        };
        let before = match span.start_ms < self.from_ms {
            true => ", before the shown range",
            false => "",
        };
        format!("{started}{before} · lasted {lasted}")
    }
}

fn step_for(shown: i64) -> i64 {
    let shown = shown.max(1);
    STEPS_MS
        .iter()
        .copied()
        .find(|step| shown.saturating_add(step - 1) / step <= TICKS_MOST)
        .unwrap_or_else(|| shown.saturating_add(TICKS_MOST - 1) / TICKS_MOST)
        .max(1)
}

fn fit(from: i64, length: i64, end: i64) -> (i64, i64) {
    let length = length.clamp(1, end.max(1));
    let from = from.clamp(0, end.saturating_sub(length).max(0));
    (from, from.saturating_add(length))
}

fn wall(ms: i64, seconds: bool) -> String {
    let day = ms.rem_euclid(DAY_MS);
    let (hours, minutes, rest) = (day / 3_600_000, day / 60_000 % 60, day / 1_000 % 60);
    match seconds {
        true => format!("{hours:02}:{minutes:02}:{rest:02}"),
        false => format!("{hours:02}:{minutes:02}"),
    }
}

fn across(bounds: Bounds<Pixels>, share: f32) -> Pixels {
    bounds.left() + (bounds.size.width * share).round()
}

fn rule(bounds: Bounds<Pixels>, share: f32) -> Bounds<Pixels> {
    Bounds::new(
        point(across(bounds, share), bounds.top()),
        size(px(1.0), bounds.size.height),
    )
}

fn label(
    bounds: Bounds<Pixels>,
    x: Pixels,
    said: SharedString,
    text_size: f32,
    color: Rgba,
    window: &mut Window,
    cx: &mut App,
) {
    let mut style = window.text_style();
    style.font_size = px(text_size).into();
    style.color = rgb_to_hsla(color);
    let line_height = style.line_height_in_pixels(window.rem_size());
    let run = style.to_run(said.len());
    let line = window
        .text_system()
        .shape_line(said, px(text_size), &[run], None);
    let top = bounds.top() + ((bounds.size.height - line_height) / 2.0).round();
    if let Err(error) = line.paint(
        point(x, top),
        line_height,
        TextAlign::Left,
        None,
        window,
        cx,
    ) {
        eprintln!("desk: a waterfall label was not painted: {error}");
    }
}

pub fn duration(ms: i64) -> String {
    let ms = ms.max(0);
    match ms {
        0..1_000 if ms > 0 => format!("{ms}ms"),
        0..10_000 if ms % 1_000 != 0 => format!("{:.1}s", ms as f64 / 1e3),
        0..60_000 => format!("{}s", ms / 1_000),
        60_000..3_600_000 => match ms / 1_000 % 60 {
            0 => format!("{}m", ms / 60_000),
            seconds => format!("{}m {seconds}s", ms / 60_000),
        },
        _ => format!("{}h {}m", ms / 3_600_000, ms % 3_600_000 / 60_000),
    }
}
