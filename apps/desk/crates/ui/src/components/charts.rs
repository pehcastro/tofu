use std::cell::{Cell, OnceCell, RefCell};
use std::f32::consts::{FRAC_PI_2, TAU};
use std::rc::Rc;
use std::time::{Duration, Instant};

use desk_motion::reduced_motion;
use desk_motion::tokens::{EASE_OUT, HOVER_MS, TOGGLE_MS};
use gpui::{
    AnchoredPositionMode, App, Background, Bounds, Canvas, ClickEvent, ContentMask, Context,
    Corners, Deferred, Div, Entity, Font, FontWeight, MouseMoveEvent, PathBuilder, Pixels, Rgba,
    SharedString, Stateful, StyleRefinement, TextRun, Window, anchored, canvas, deferred, div,
    fill, linear_color_stop, linear_gradient, pattern_slash, point, prelude::*, px, rgb, size,
};

use crate::components::card::dots;
use crate::components::chip::mono;
use crate::components::paint::{ink, ring, tint};
use crate::components::size::{T1, T2, T3};
use crate::live::ActiveTheme;
use crate::theme::{ColorToken, Theme};

pub const MINT: u32 = 0x86e0b3;
pub const AMBER: u32 = 0xe8c98a;
pub const LILAC: u32 = 0xb9a6ea;
pub const ROSE: u32 = 0xf1737d;
pub const SKY: u32 = 0x9db8f0;

const STAGGER: Duration = Duration::from_millis(160);
const WIPE: Duration = Duration::from_millis(300);

const TIP_OUT: Duration = Duration::from_millis(100);
const TIP_GAP: f32 = 16.0;
const TIP_PAD_X: f32 = 10.0;
const TIP_PAD_Y: f32 = 6.0;
const TIP_LINE: f32 = 14.0;
const TIP_TEXT: f32 = 12.0;
const TIP_NOTE: f32 = 11.0;
const TIP_STACK: f32 = 6.0;
const TIP_SPLIT: f32 = 8.0;
const TIP_VALUE_GAP: f32 = 16.0;
const TIP_MIN: f32 = 128.0;
const TIP_RULE: f32 = 0.08;
const MARK_DOT: f32 = 10.0;
const MARK_LINE: f32 = 4.0;
const MARK_DASH: f32 = 2.0;
const POINT_MARK: f32 = 7.0;
const POINT_RING: f32 = 0.25;
const LINE_AIM_Y: f32 = 0.25;
const ACTIVE_DOT: f32 = 4.0;
const ACTIVE_RIM: f32 = 2.0;

const COLUMN_CELLS: usize = 10;
const COLUMN_CELL: f32 = 9.0;
const CELL_RADIUS: f32 = 2.5;
const COLUMN_CELL_GAP: f32 = 3.0;
const COLUMN_GAP: f32 = 5.0;
const COLUMNS_HEIGHT: f32 = COLUMN_CELLS as f32 * (COLUMN_CELL + COLUMN_CELL_GAP) - COLUMN_CELL_GAP;
const CHART_GAP: f32 = 12.0;
const AXIS_TEXT: f32 = 11.5;
const AXIS_LINE: f32 = 14.0;
const EMPTY: f32 = 0.05;
const BODY: f32 = 0.55;
const TOP: f32 = 0.9;

const GRID_COLUMNS: usize = 25;
const GRID_CELL: f32 = 12.0;
const GRID_GAP: f32 = 3.0;
const DIMMED: f32 = 0.35;
const FORK_CELL: f32 = 0.5;

const METER_CELLS: usize = 20;
const METER_CELL: f32 = 5.0;
const METER_RADIUS: f32 = 1.5;
const METER_GAP: f32 = 2.0;
const METER_MAX: f32 = 150.0;
const METER_ON: f32 = 0.8;
const METER_OFF: f32 = 0.09;
const METER_WARN_ABOVE: u32 = 70;
const METER_FULL: u32 = 100;
const METER_STEP: u32 = 5;
const LIMIT_PERCENT: f32 = 15.0;
const LIMIT_PERCENT_LINE: f32 = 18.0;
const LIMIT_SMALL: f32 = 11.0;
const LIMIT_SMALL_LINE: f32 = 14.0;
const LIMIT_GAP: f32 = 4.0;
const LIMIT_PROJECTION: f32 = 0.38;
const METER_TOP: f32 = LIMIT_PERCENT_LINE + LIMIT_GAP;
const METER_HEIGHT: f32 = METER_TOP + METER_CELL + LIMIT_GAP + LIMIT_SMALL_LINE;

const LINE_VIEW: (f32, f32) = (700.0, 150.0);
const LINE_AREA: f32 = 0.06;
const LINE_INK: f32 = 0.85;
const LINE_WIDTH: f32 = 2.0;
const THRESHOLD_INK: f32 = 0.6;
const THRESHOLD_DASH: [f32; 2] = [3.0, 4.0];
const COMPACT_INK: f32 = 0.25;
const COMPACT_DASH: [f32; 2] = [2.0, 3.0];
const COMPACT_SPAN: (f32, f32) = (8.0, 148.0);
const REPORT_RADIUS: f32 = 4.0;

const SPARK_VIEW: (f32, f32) = (70.0, 20.0);
const SPARK_INK: f32 = 0.55;
const SPARK_WIDTH: f32 = 1.3;
const SPARK_END: f32 = 2.0;
const SPARK_BASE: f32 = 19.0;
const SPARK_RISE: f32 = 18.0;

const RIBBON_VIEW: (f32, f32) = (1100.0, 300.0);
const RIBBON_LANES: [f32; 2] = [104.0, 206.0];
const RIBBON_STEP: f32 = 30.0;
const RIBBON_FLOOR: f32 = 4.0;
const RIBBON_RISE: f32 = 46.0;
const RIBBON_CAPACITY: f32 = 250.0;
const RIBBON_ON: f32 = 0.22;
const RIBBON_OFF: f32 = 0.08;
const RIBBON_EDGE_ON: f32 = 0.85;
const RIBBON_EDGE_OFF: f32 = 0.3;
const FUNNEL_FILL: f32 = 0.03;
const FUNNEL_EDGE: f32 = 0.22;
const FUNNEL_DASH: [f32; 2] = [3.0, 3.0];
const BRANCH_INK: f32 = 0.35;
const BRANCH_WIDTH: f32 = 1.3;
const BRANCH_DASH: [f32; 2] = [4.0, 3.0];
const BRANCH_STEPS: usize = 16;
const BRANCH_BEND: f32 = 20.0;
const BRANCH_LEAD: f32 = 10.0;
const NOW_INK: f32 = 0.6;
const NOW_LENGTH: f32 = 70.0;
const NOW_DASH: [f32; 2] = [2.0, 4.0];
const END_INK: f32 = 0.45;
const END_WIDTH: f32 = 1.5;
const END_REACH: f32 = 14.0;
const END_HALF: f32 = 8.0;
const HEAD: f32 = 12.0;
const HEAD_INK: f32 = 0.25;
const RIBBON_REACH_STEPS: f32 = 1.25;
const DOT_PICKED: f32 = 4.5;
const DOT_PLAIN: f32 = 2.5;
const DOT_ON: f32 = 0.9;
const DOT_OFF: f32 = 0.45;
const DOT_HALO: f32 = 3.0;
const LABEL_WIDTH: f32 = 180.0;
const LABEL_ABOVE: f32 = 62.0;
const LABEL_BELOW: f32 = 22.0;
const LABEL_TEXT: f32 = 12.5;
const LABEL_LINE: f32 = 16.0;
const LABEL_OFF: f32 = 0.55;
const NOTE_WIDTH: f32 = 90.0;
const NOTE_LINE: f32 = 15.0;
const LABELS_FROM_SCALE: f32 = 0.6;
const LEGEND_AT: (f32, f32) = (24.0, 14.0);
const LEGEND_TEXT: f32 = 12.0;
const LEGEND_SHAPE: (f32, f32) = (40.0, 14.0);
const LEGEND_FILL: f32 = 0.12;
const LEGEND_EDGE: f32 = 0.4;
const LEGEND_POINTS: [(f32, f32); 4] = [(0.0, 5.0), (40.0, 1.0), (40.0, 13.0), (0.0, 9.0)];
const LIVE_AT: (f32, f32) = (1010.0, 96.0);
const BACKDROP_INK: f32 = 0.6;

const TREND_PLOT: f32 = 140.0;
const TREND_HEADROOM: f32 = 0.88;
const TREND_STROKE: f32 = 1.5;
const TREND_DASH: [f32; 2] = [3.0, 3.0];
const TREND_GRADIENT_TOP: f32 = 0.32;
const TREND_SOLID: f32 = 0.16;
const TREND_DOTTED: f32 = 0.5;
const TREND_DOT_PITCH: f32 = 6.0;
const TREND_DOT_COLUMNS: usize = 3;
const TREND_DOT: f32 = 0.9;
const GLOW: [(f32, f32); 2] = [(7.0, 0.08), (3.5, 0.2)];
const REST_DOT: f32 = 3.0;
const REST_HOLE: f32 = 1.5;
const CURSOR_INK: f32 = 0.3;
const CURSOR_DASH: [f32; 2] = [3.0, 3.0];
const GUIDE_INK: f32 = 0.07;
const GUIDE_LINES: usize = 4;

const BAR_PLOT: f32 = 140.0;
const BAR_FILL: f32 = 0.62;
const BAR_RADIUS: f32 = 3.0;
const BAR_ROW: f32 = 18.0;
const BAR_ROW_GAP: f32 = 8.0;
const BAR_LABEL: f32 = 84.0;
const DUOTONE_FAINT: f32 = 0.4;
const HATCH_BASE: f32 = 0.3;
const HATCH: (f32, f32) = (1.5, 3.5);
const GRADIENT_STOPS: (f32, f32) = (0.2, 0.9);
const HOVER_DIM: f32 = 0.7;

const ROUND_SIDE: f32 = 180.0;
const DONUT_RADIUS: f32 = 76.0;
const DONUT_POP: f32 = 5.0;
const DONUT_PAD: f32 = 0.035;
const ARC_STEP: f32 = 0.04;
const CENTER_VALUE: f32 = 22.0;
const CENTER_LINE: f32 = 26.0;

const RADIAL_OUTER: f32 = 80.0;
const RING_WIDTH: f32 = 12.0;
const RING_GAP: f32 = 5.0;
const RING_TRACK: f32 = 0.07;
const SEMI_FOOT: f32 = 4.0;

const RADAR_VIEW: (f32, f32) = (240.0, 210.0);
const RADAR_RADIUS: f32 = 72.0;
const RADAR_LABEL: f32 = 18.0;
const RADAR_FILL: f32 = 0.3;
const RADAR_STROKE: f32 = 1.5;
const RADAR_LEVELS: usize = 4;
const RADAR_REACH: f32 = 24.0;
const RADAR_GRID: f32 = 0.12;
const RADAR_GRID_DASH: [f32; 2] = [3.0, 4.0];
const RADAR_SPOKE: f32 = 0.08;
const RADAR_LABEL_BOX: (f32, f32) = (72.0, 14.0);

const LEGEND_SWATCH: f32 = 8.0;
const LEGEND_GAP: (f32, f32) = (14.0, 4.0);
const LEGEND_SPLIT: f32 = 6.0;
const LEGEND_RADIUS: f32 = 2.0;

struct Entry {
    elapsed: f32,
    reduced: bool,
}

impl Entry {
    fn read(born: &mut Option<Instant>, window: &mut Window, cx: &App) -> Self {
        let elapsed = born.get_or_insert_with(Instant::now).elapsed();
        if elapsed < STAGGER + TOGGLE_MS.max(WIPE) {
            window.request_animation_frame();
        }
        Entry {
            elapsed: elapsed.as_secs_f32(),
            reduced: reduced_motion(cx),
        }
    }

    fn cell(&self, share: f32) -> f32 {
        let delay = if self.reduced {
            0.0
        } else {
            STAGGER.as_secs_f32() * share
        };
        EASE_OUT(((self.elapsed - delay) / TOGGLE_MS.as_secs_f32()).clamp(0.0, 1.0))
    }

    fn wipe(&self) -> (f32, f32) {
        if self.reduced {
            (1.0, self.cell(0.0))
        } else {
            (
                EASE_OUT((self.elapsed / WIPE.as_secs_f32()).clamp(0.0, 1.0)),
                1.0,
            )
        }
    }
}

fn share(index: usize, count: usize) -> f32 {
    index as f32 / count.saturating_sub(1).max(1) as f32
}

fn rise(color: Rgba, shown: f32) -> Rgba {
    tint(color, EMPTY + (color.alpha - EMPTY) * shown)
}

fn faded(color: Rgba, alpha: f32) -> Rgba {
    tint(color, color.alpha * alpha)
}

fn stack(cells: usize, cell: f32, gap: f32) -> f32 {
    (cells as f32 * (cell + gap) - gap).max(0.0)
}

fn nearest(
    spots: impl Iterator<Item = (f32, f32)>,
    at: (f32, f32),
    weight_y: f32,
) -> Option<(usize, (f32, f32), f32)> {
    spots
        .enumerate()
        .map(|(index, spot)| {
            (
                index,
                spot,
                (spot.0 - at.0).hypot((spot.1 - at.1) * weight_y),
            )
        })
        .min_by(|a, b| a.2.total_cmp(&b.2))
}

fn polar(center: (f32, f32), radius: f32, angle: f32) -> (f32, f32) {
    (
        center.0 + radius * angle.sin(),
        center.1 - radius * angle.cos(),
    )
}

fn arc(center: (f32, f32), radius: f32, from: f32, to: f32) -> Vec<(f32, f32)> {
    let steps = ((to - from).abs() / ARC_STEP).ceil().max(1.0) as usize;
    (0..=steps)
        .map(|step| {
            polar(
                center,
                radius,
                from + (to - from) * step as f32 / steps as f32,
            )
        })
        .collect()
}

fn bearing(center: (f32, f32), at: (f32, f32)) -> (f32, f32) {
    let (dx, dy) = (at.0 - center.0, at.1 - center.1);
    (dx.hypot(dy), dx.atan2(-dy).rem_euclid(TAU))
}

struct Lattice {
    count: usize,
    gap: f32,
    height: f32,
    radius: f32,
}

impl Lattice {
    fn pitch(&self, span: f32) -> f32 {
        (span + self.gap) / self.count.max(1) as f32
    }

    fn column(&self, at: f32, span: f32) -> Option<usize> {
        (self.count > 0 && (0.0..span).contains(&at))
            .then(|| ((at / self.pitch(span)) as usize).min(self.count - 1))
    }

    fn center(&self, column: usize, span: f32) -> f32 {
        let pitch = self.pitch(span);
        column as f32 * pitch + (pitch - self.gap) / 2.0
    }

    fn paint(self, cells: Vec<(usize, f32, Rgba)>) -> Canvas<()> {
        canvas(
            |_, _, _| (),
            move |bounds: Bounds<Pixels>, (), window, _| {
                let pitch = self.pitch(f32::from(bounds.size.width));
                let side = size(px(pitch - self.gap), px(self.height));
                for (column, y, color) in &cells {
                    let origin = bounds.origin + point(px(*column as f32 * pitch), px(*y));
                    window.paint_quad(
                        fill(Bounds::new(origin, side), *color).corner_radii(px(self.radius)),
                    );
                }
            },
        )
    }
}

#[derive(Clone, Copy, Default, PartialEq, Eq)]
pub enum Mark {
    #[default]
    Hidden,
    Dot,
    Line,
    Dashed,
}

impl Mark {
    fn width(self) -> f32 {
        match self {
            Mark::Hidden => 0.0,
            Mark::Dot => MARK_DOT,
            Mark::Line => MARK_LINE,
            Mark::Dashed => MARK_DASH,
        }
    }

    fn paint(self, color: Rgba) -> Option<Div> {
        let mark = div().flex_none().w(px(self.width()));
        match self {
            Mark::Hidden => None,
            Mark::Dot => Some(mark.h(px(MARK_DOT)).rounded(px(MARK_DASH)).bg(color)),
            Mark::Line => Some(mark.self_stretch().rounded(px(MARK_DASH)).bg(color)),
            Mark::Dashed => Some(
                mark.self_stretch()
                    .border_l_2()
                    .border_dashed()
                    .border_color(color),
            ),
        }
    }
}

#[derive(Clone)]
pub struct Row {
    pub name: SharedString,
    pub value: SharedString,
    pub color: Option<Rgba>,
}

#[derive(Clone, Default)]
pub struct Said {
    pub title: Option<SharedString>,
    pub rows: Vec<Row>,
    pub foot: Option<Row>,
    pub note: Option<SharedString>,
    pub mark: Mark,
}

impl Said {
    pub fn new(value: impl Into<SharedString>, label: impl Into<SharedString>) -> Self {
        Said::default().row(label, value, None)
    }

    pub fn titled(title: impl Into<SharedString>) -> Self {
        Said {
            title: Some(title.into()),
            ..Said::default()
        }
    }

    pub fn row(
        mut self,
        name: impl Into<SharedString>,
        value: impl Into<SharedString>,
        color: Option<Rgba>,
    ) -> Self {
        self.rows.push(Row {
            name: name.into(),
            value: value.into(),
            color,
        });
        self
    }

    pub fn foot(
        mut self,
        name: impl Into<SharedString>,
        value: impl Into<SharedString>,
        tone: Option<Rgba>,
    ) -> Self {
        self.foot = Some(Row {
            name: name.into(),
            value: value.into(),
            color: tone,
        });
        self
    }

    pub fn note(mut self, note: impl Into<SharedString>) -> Self {
        self.note = Some(note.into());
        self
    }

    pub fn mark(mut self, mark: Mark) -> Self {
        self.mark = mark;
        self
    }

    fn card(&self, theme: &Theme) -> Div {
        let value = |text: &SharedString, color: Rgba| {
            div()
                .flex_none()
                .font_family(mono(theme))
                .font_weight(FontWeight::MEDIUM)
                .text_color(color)
                .child(text.clone())
        };
        let line = |row: &Row, color: Rgba| {
            div()
                .flex_1()
                .flex()
                .items_center()
                .justify_between()
                .gap(px(TIP_VALUE_GAP))
                .child(div().text_color(ink(theme, T2)).child(row.name.clone()))
                .child(value(&row.value, color))
        };
        let rows = self.rows.iter().map(|row| {
            let marked = div().flex().gap(px(TIP_SPLIT));
            let marked = match self.mark {
                Mark::Dot => marked.items_center(),
                Mark::Hidden | Mark::Line | Mark::Dashed => marked.items_stretch(),
            };
            marked
                .children(self.mark.paint(row.color.unwrap_or_else(|| ink(theme, T2))))
                .child(line(row, ink(theme, T1)))
        });
        let foot = self.foot.as_ref().map(|foot| {
            div()
                .flex()
                .pt(px(TIP_STACK))
                .border_t_1()
                .border_color(ink(theme, TIP_RULE))
                .child(line(foot, foot.color.unwrap_or_else(|| ink(theme, T1))))
        });
        div()
            .flex()
            .flex_col()
            .gap(px(TIP_STACK))
            .children(self.title.clone().map(|title| {
                div()
                    .font_weight(FontWeight::MEDIUM)
                    .text_color(ink(theme, T1))
                    .child(title)
            }))
            .children(rows)
            .children(foot)
            .children(self.note.clone().map(|note| {
                div()
                    .text_size(px(TIP_NOTE))
                    .text_color(ink(theme, T3))
                    .child(note)
            }))
    }

    fn size(&self, theme: &Theme, window: &Window) -> [f32; 2] {
        let style = window.text_style();
        let measure = |text: &SharedString, size: f32, weight: FontWeight, family: bool| {
            let base = style.font();
            let run = TextRun {
                len: text.len(),
                font: Font {
                    weight,
                    family: if family {
                        mono(theme)
                    } else {
                        base.family.clone()
                    },
                    ..base
                },
                color: style.color,
                background_color: None,
                underline: None,
                strikethrough: None,
                letter_spacing: None,
            };
            f32::from(
                window
                    .text_system()
                    .shape_line(text.clone(), px(size), &[run], None)
                    .width(),
            )
        };
        let line = |row: &Row| {
            measure(&row.name, TIP_TEXT, FontWeight::NORMAL, false)
                + TIP_VALUE_GAP
                + measure(&row.value, TIP_TEXT, FontWeight::MEDIUM, true)
        };
        let marked = match self.mark {
            Mark::Hidden => 0.0,
            Mark::Dot | Mark::Line | Mark::Dashed => self.mark.width() + TIP_SPLIT,
        };
        let title = self
            .title
            .iter()
            .map(|title| measure(title, TIP_TEXT, FontWeight::MEDIUM, false));
        let rows = self.rows.iter().map(|row| marked + line(row));
        let foot = self.foot.iter().map(line);
        let note = self
            .note
            .iter()
            .map(|note| measure(note, TIP_NOTE, FontWeight::NORMAL, false));
        let widths: Vec<f32> = title.chain(rows).chain(foot).chain(note).collect();
        let lines = widths.iter().map(|_| TIP_LINE + TIP_STACK).sum::<f32>()
            + self.foot.iter().map(|_| TIP_STACK).sum::<f32>();
        [
            widths.into_iter().fold(TIP_MIN, f32::max).ceil() + TIP_PAD_X * 2.0 + 2.0,
            lines - TIP_STACK + TIP_PAD_Y * 2.0 + 2.0,
        ]
    }
}

#[derive(Clone)]
pub struct Series {
    pub name: SharedString,
    pub color: Rgba,
    pub values: Vec<f32>,
}

#[derive(Clone)]
pub struct Slice {
    pub name: SharedString,
    pub value: f32,
    pub color: Rgba,
}

pub struct Aim {
    at: usize,
    anchor: (f32, f32),
    said: Said,
}

const PIN_CELL: bool = false;
const PIN_POINT: bool = true;
const PIN_ACTIVE: bool = false;

#[derive(Clone, Copy)]
struct Fade {
    from: f32,
    showing: bool,
    since: Instant,
}

impl Fade {
    fn span(&self) -> Duration {
        if self.showing { HOVER_MS } else { TIP_OUT }
    }

    fn done(&self, now: Instant) -> bool {
        now.saturating_duration_since(self.since) >= self.span()
    }

    fn value(&self, now: Instant) -> f32 {
        let to = if self.showing { 1.0 } else { 0.0 };
        let elapsed = now.saturating_duration_since(self.since).as_secs_f32();
        let eased = EASE_OUT((elapsed / self.span().as_secs_f32()).min(1.0));
        (self.from + (to - self.from) * eased).clamp(0.0, 1.0)
    }
}

#[derive(Default)]
struct Hover {
    at: Option<usize>,
    said: Said,
    held: [f32; 2],
    anchor: [f32; 2],
    pointer: [f32; 2],
    fade: Option<Fade>,
    shade: f32,
}

enum Moved {
    Still,
    Pointer,
    Point,
}

impl Hover {
    fn follow(&mut self, pointer: [f32; 2], aim: Option<Aim>) -> Moved {
        self.pointer = pointer;
        let Some(aim) = aim else {
            return if self.fade.is_some() {
                Moved::Pointer
            } else {
                Moved::Still
            };
        };
        let now = Instant::now();
        let entering = !self.fade.is_some_and(|fade| fade.showing);
        if entering {
            self.held = [0.0; 2];
            self.fade = Some(Fade {
                from: self.fade.map_or(0.0, |fade| fade.value(now)),
                showing: true,
                since: now,
            });
        }
        if Some(aim.at) == self.at && !entering {
            return Moved::Pointer;
        }
        self.at = Some(aim.at);
        self.said = aim.said;
        self.anchor = [aim.anchor.0, aim.anchor.1];
        Moved::Point
    }

    fn leave(&mut self) -> bool {
        let Some(fade) = self.fade.filter(|fade| fade.showing) else {
            return false;
        };
        let now = Instant::now();
        self.at = None;
        self.fade = Some(Fade {
            from: fade.value(now),
            showing: false,
            since: now,
        });
        true
    }

    fn step(&mut self, window: &Window) -> bool {
        let now = Instant::now();
        self.shade = self.fade.map_or(0.0, |fade| fade.value(now));
        let Some(fade) = self.fade else {
            return false;
        };
        if !fade.done(now) {
            window.request_animation_frame();
        }
        if !fade.showing && fade.done(now) {
            self.fade = None;
            return false;
        }
        true
    }
}

pub struct TipView {
    area: Rc<Cell<Bounds<Pixels>>>,
    hover: Rc<RefCell<Hover>>,
    pin: bool,
}

impl Render for TipView {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        div().absolute().children(self.layer(&theme, window))
    }
}

pub struct Tip {
    area: Rc<Cell<Bounds<Pixels>>>,
    hover: Rc<RefCell<Hover>>,
    view: OnceCell<Entity<TipView>>,
    pin: bool,
}

impl Tip {
    fn new(pin: bool) -> Self {
        Tip {
            area: Rc::default(),
            hover: Rc::default(),
            view: OnceCell::new(),
            pin,
        }
    }

    fn probe(&self) -> Div {
        let area = self.area.clone();
        div()
            .absolute()
            .inset_0()
            .child(canvas(move |bounds, _, _| area.set(bounds), |_, (), _, _| {}).size_full())
            .children(self.view.get().cloned())
    }

    fn tick(&self, window: &Window) {
        self.hover.borrow_mut().step(window);
    }

    fn shade(&self) -> f32 {
        self.hover.borrow().shade
    }

    fn at(&self) -> Option<usize> {
        self.hover.borrow().at
    }

    fn dim(&self, index: usize) -> f32 {
        let hover = self.hover.borrow();
        if hover.at.is_none_or(|at| at == index) {
            1.0
        } else {
            1.0 - HOVER_DIM * hover.shade
        }
    }
}

impl TipView {
    fn layer(&self, theme: &Theme, window: &mut Window) -> Option<Deferred> {
        let mut hover = self.hover.borrow_mut();
        if !hover.step(window) {
            return None;
        }
        let [width, height] = hover.said.size(theme, window);
        hover.held = [hover.held[0].max(width), hover.held[1].max(height)];
        let [width, height] = hover.held;
        let scale = window.scale_factor();
        let snap = |value: f32| (value * scale).round() / scale;
        let bounds = self.area.get();
        let area = bounds.size;
        let pinned = |x: f32, y: f32| {
            anchored()
                .position_mode(AnchoredPositionMode::Window)
                .position(bounds.origin + point(px(x), px(y)))
        };
        let place = |at: f32, size: f32, room: f32| {
            let after = at + TIP_GAP;
            let spot = if after + size <= room {
                after
            } else {
                at - TIP_GAP - size
            };
            snap(spot.min(room - size).max(0.0))
        };
        let [x, y] = hover.pointer;
        let bubble = super::tooltip::overlay_surface(theme)
            .child(hover.said.card(theme))
            .w(px(width))
            .px(px(TIP_PAD_X))
            .py(px(TIP_PAD_Y))
            .text_size(px(TIP_TEXT))
            .line_height(px(TIP_LINE))
            .whitespace_nowrap();
        let [mark_x, mark_y] = hover.anchor;
        let marker = self.pin.then(|| {
            pinned(mark_x - POINT_MARK / 2.0, mark_y - POINT_MARK / 2.0).child(
                div()
                    .size(px(POINT_MARK))
                    .rounded_full()
                    .bg(theme.color(ColorToken::TextStrong))
                    .shadow(vec![ring(ink(theme, POINT_RING))]),
            )
        });
        let bubble = pinned(
            place(x, width, f32::from(area.width)),
            place(y, height, f32::from(area.height)),
        )
        .child(bubble);
        Some(deferred(div().opacity(hover.shade).children(marker).child(bubble)).priority(1))
    }
}

pub trait Chart: Render {
    fn extent(&self) -> (Option<f32>, f32);
    fn tip(&self) -> &Tip;
    fn aim(&self, at: (f32, f32), area: (f32, f32)) -> Option<Aim>;
}

pub fn cached<T: Chart>(chart: &Entity<T>, cx: &App) -> Div {
    let read = chart.read(cx);
    let (width, height) = read.extent();
    let style = StyleRefinement::default().h(px(height));
    let style = match width {
        Some(width) => style.w(px(width)).flex_none(),
        None => style.w_full(),
    };
    let mut frame = div().relative();
    frame.style().refine(&style);
    frame
        .child(chart.clone().cached(style))
        .children(read.tip().view.get().cloned())
}

fn hovered<T: Chart>(frame: Stateful<Div>, cx: &Context<T>) -> Stateful<Div> {
    frame
        .on_mouse_move(cx.listener(|chart, event: &MouseMoveEvent, _, cx| {
            let area = chart.tip().area.get();
            let at = event.position - area.origin;
            let at = (f32::from(at.x), f32::from(at.y));
            let moved = if area.contains(&event.position) {
                let aim = chart.aim(
                    at,
                    (f32::from(area.size.width), f32::from(area.size.height)),
                );
                chart.tip().hover.borrow_mut().follow([at.0, at.1], aim)
            } else if chart.tip().hover.borrow_mut().leave() {
                Moved::Point
            } else {
                Moved::Still
            };
            let tip = chart.tip();
            match moved {
                Moved::Still => {}
                Moved::Pointer => {
                    if let Some(view) = tip.view.get() {
                        view.update(cx, |_, cx| cx.notify());
                    }
                }
                Moved::Point => {
                    tip.view.get_or_init(|| {
                        cx.new(|_| TipView {
                            area: tip.area.clone(),
                            hover: tip.hover.clone(),
                            pin: tip.pin,
                        })
                    });
                    cx.notify();
                }
            }
        }))
        .on_hover(cx.listener(|chart, over: &bool, _, cx| {
            if !*over && chart.tip().hover.borrow_mut().leave() {
                cx.notify();
            }
        }))
}

fn axis_row(axis: &(SharedString, SharedString), theme: &Theme) -> Div {
    div()
        .h(px(AXIS_LINE))
        .flex()
        .justify_between()
        .text_size(px(AXIS_TEXT))
        .line_height(px(AXIS_LINE))
        .font_family(mono(theme))
        .text_color(ink(theme, T3))
        .child(axis.0.clone())
        .child(axis.1.clone())
}

fn centered(said: &Said, theme: &Theme) -> Div {
    let lines = said.rows.first().map(|row| {
        [
            div()
                .text_size(px(CENTER_VALUE))
                .line_height(px(CENTER_LINE))
                .font_weight(FontWeight::SEMIBOLD)
                .text_color(ink(theme, T1))
                .child(row.value.clone()),
            div()
                .text_size(px(AXIS_TEXT))
                .text_color(ink(theme, T3))
                .child(row.name.clone()),
        ]
    });
    div()
        .absolute()
        .inset_0()
        .flex()
        .flex_col()
        .items_center()
        .justify_center()
        .children(lines.into_iter().flatten())
}

pub fn legend(items: impl IntoIterator<Item = (SharedString, Rgba)>, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_wrap()
        .gap_x(px(LEGEND_GAP.0))
        .gap_y(px(LEGEND_GAP.1))
        .text_size(px(AXIS_TEXT))
        .text_color(ink(theme, T2))
        .children(items.into_iter().map(|(name, color)| {
            div()
                .flex()
                .items_center()
                .gap(px(LEGEND_SPLIT))
                .child(
                    div()
                        .size(px(LEGEND_SWATCH))
                        .rounded(px(LEGEND_RADIUS))
                        .bg(color),
                )
                .child(name)
        }))
}

#[derive(Clone)]
pub struct Shape {
    pub points: Vec<(f32, f32)>,
    pub closed: bool,
    pub fill: Option<Background>,
    pub stroke: Option<(Rgba, f32, Option<[f32; 2]>)>,
}

impl Shape {
    fn line(points: Vec<(f32, f32)>, color: Rgba, width: f32, dash: Option<[f32; 2]>) -> Self {
        Shape {
            points,
            closed: false,
            fill: None,
            stroke: Some((color, width, dash)),
        }
    }

    fn area(
        points: Vec<(f32, f32)>,
        fill: impl Into<Background>,
        stroke: Option<(Rgba, f32, Option<[f32; 2]>)>,
    ) -> Self {
        Shape {
            points,
            closed: true,
            fill: Some(fill.into()),
            stroke,
        }
    }
}

type Dot = ((f32, f32), f32, Rgba);

fn ringed(center: (f32, f32), radius: f32, rim: f32, color: Rgba, hole: Rgba) -> [Dot; 2] {
    [(center, radius + rim, hole), (center, radius, color)]
}

fn plot(
    view: (f32, f32),
    shapes: Vec<Shape>,
    dots: Vec<Dot>,
    (reveal, alpha): (f32, f32),
) -> Canvas<()> {
    canvas(
        |_, _, _| (),
        move |bounds: Bounds<Pixels>, (), window, _| {
            let sx = f32::from(bounds.size.width) / view.0;
            let sy = f32::from(bounds.size.height) / view.1;
            let at = |(x, y): (f32, f32)| bounds.origin + point(px(x * sx), px(y * sy));
            let bleed = px(SPARK_END * 2.0 + DOT_HALO);
            let shown = Bounds::new(
                bounds.origin - point(bleed, bleed),
                size(
                    (bounds.size.width + bleed * 2.0) * reveal,
                    bounds.size.height + bleed * 2.0,
                ),
            );
            let mask = ContentMask {
                bounds: shown,
                ..Default::default()
            };
            window.with_content_mask(Some(mask), |window| {
                for shape in &shapes {
                    let trace = |mut path: PathBuilder| {
                        for (index, spot) in shape.points.iter().enumerate() {
                            if index == 0 {
                                path.move_to(at(*spot));
                            } else {
                                path.line_to(at(*spot));
                            }
                        }
                        if shape.closed {
                            path.close();
                        }
                        path.build()
                    };
                    if let Some(background) = shape.fill
                        && let Ok(path) = trace(PathBuilder::fill())
                    {
                        window.paint_path(path, background.opacity(alpha));
                    }
                    if let Some((color, width, dash)) = shape.stroke {
                        let builder = PathBuilder::stroke(px(width));
                        let builder = match dash {
                            Some([on, off]) => builder.dash_array(&[px(on), px(off)]),
                            None => builder,
                        };
                        if let Ok(path) = trace(builder) {
                            window.paint_path(path, faded(color, alpha));
                        }
                    }
                }
                for (center, radius, color) in &dots {
                    let origin = at(*center) - point(px(*radius), px(*radius));
                    let side = px(radius * 2.0);
                    window.paint_quad(
                        fill(Bounds::new(origin, size(side, side)), faded(*color, alpha))
                            .corner_radii(px(*radius)),
                    );
                }
            });
        },
    )
}

pub struct DotColumns {
    heights: Vec<usize>,
    tips: Vec<Said>,
    axis: (SharedString, SharedString),
    tip: Tip,
    born: Option<Instant>,
}

impl DotColumns {
    pub fn new(heights: Vec<usize>, tips: Vec<Said>, axis: (SharedString, SharedString)) -> Self {
        DotColumns {
            heights,
            tips,
            axis,
            tip: Tip::new(PIN_CELL),
            born: None,
        }
    }

    fn lattice(&self) -> Lattice {
        Lattice {
            count: self.heights.len(),
            gap: COLUMN_GAP,
            height: COLUMN_CELL,
            radius: CELL_RADIUS,
        }
    }
}

impl Chart for DotColumns {
    fn extent(&self) -> (Option<f32>, f32) {
        (None, COLUMNS_HEIGHT + CHART_GAP + AXIS_LINE)
    }

    fn tip(&self) -> &Tip {
        &self.tip
    }

    fn aim(&self, at: (f32, f32), area: (f32, f32)) -> Option<Aim> {
        let lattice = self.lattice();
        let index = lattice.column(at.0, area.0)?;
        let height = self.heights.get(index)?.min(&COLUMN_CELLS);
        Some(Aim {
            at: index,
            anchor: (
                lattice.center(index, area.0),
                COLUMNS_HEIGHT - stack(*height, COLUMN_CELL, COLUMN_CELL_GAP),
            ),
            said: self.tips.get(index).cloned().unwrap_or_default(),
        })
    }
}

impl Render for DotColumns {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let entry = Entry::read(&mut self.born, window, cx);
        self.tip.tick(window);
        let count = self.heights.len();
        let mut cells = Vec::with_capacity(count * COLUMN_CELLS);
        for (index, &height) in self.heights.iter().enumerate() {
            let shown = entry.cell(share(index, count));
            let dim = self.tip.dim(index);
            for cell in 0..COLUMN_CELLS {
                let color = if cell >= height {
                    ink(&theme, EMPTY)
                } else if index + 1 == count {
                    faded(rgb(MINT), dim)
                } else if cell + 1 == height {
                    ink(&theme, TOP * dim)
                } else {
                    ink(&theme, BODY * dim)
                };
                let y = COLUMNS_HEIGHT - stack(cell + 1, COLUMN_CELL, COLUMN_CELL_GAP);
                cells.push((index, y, rise(color, shown)));
            }
        }
        hovered(
            div()
                .id("dot-columns")
                .relative()
                .size_full()
                .flex()
                .flex_col()
                .gap(px(CHART_GAP))
                .child(self.tip.probe())
                .child(self.lattice().paint(cells).w_full().h(px(COLUMNS_HEIGHT)))
                .child(axis_row(&self.axis, &theme)),
            cx,
        )
    }
}

#[derive(Clone)]
pub struct GridPart {
    pub name: SharedString,
    pub tokens: usize,
    pub color: Rgba,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Slot {
    Part(usize),
    Fork,
    Free,
}

pub struct DotGrid {
    parts: Vec<GridPart>,
    capacity: usize,
    fork_at: usize,
    selected: Option<usize>,
    tip: Tip,
    born: Option<Instant>,
}

impl DotGrid {
    pub fn new(
        parts: Vec<GridPart>,
        capacity: usize,
        fork_at: usize,
        selected: Option<usize>,
    ) -> Self {
        DotGrid {
            parts,
            capacity,
            fork_at,
            selected,
            tip: Tip::new(PIN_CELL),
            born: None,
        }
    }

    fn rows(&self) -> usize {
        self.capacity.div_ceil(GRID_COLUMNS)
    }

    fn slot(&self, index: usize) -> Slot {
        let mut end = 0;
        for (part, slice) in self.parts.iter().enumerate() {
            end += slice.tokens;
            if index < end {
                return Slot::Part(part);
            }
        }
        if index + 1 == self.fork_at {
            Slot::Fork
        } else {
            Slot::Free
        }
    }

    fn said(&self, slot: Slot) -> Said {
        match slot {
            Slot::Part(part) => self
                .parts
                .get(part)
                .map(|slice| Said::new(format!("{}k", slice.tokens), slice.name.clone()))
                .unwrap_or_default(),
            Slot::Fork => Said::new(format!("{}k", self.fork_at), "fork line"),
            Slot::Free => {
                let used: usize = self.parts.iter().map(|slice| slice.tokens).sum();
                Said::new(format!("{}k", self.capacity.saturating_sub(used)), "free")
            }
        }
    }

    fn lattice() -> Lattice {
        Lattice {
            count: GRID_COLUMNS,
            gap: GRID_GAP,
            height: GRID_CELL,
            radius: CELL_RADIUS,
        }
    }
}

impl Chart for DotGrid {
    fn extent(&self) -> (Option<f32>, f32) {
        (None, stack(self.rows(), GRID_CELL, GRID_GAP))
    }

    fn tip(&self) -> &Tip {
        &self.tip
    }

    fn aim(&self, at: (f32, f32), area: (f32, f32)) -> Option<Aim> {
        let lattice = Self::lattice();
        let column = lattice.column(at.0, area.0)?;
        let row = (at.1 >= 0.0).then(|| (at.1 / (GRID_CELL + GRID_GAP)) as usize)?;
        let index = (row * GRID_COLUMNS + column).min(self.capacity.saturating_sub(1));
        let row = index / GRID_COLUMNS;
        let column = index % GRID_COLUMNS;
        Some(Aim {
            at: index,
            anchor: (
                lattice.center(column, area.0),
                row as f32 * (GRID_CELL + GRID_GAP),
            ),
            said: self.said(self.slot(index)),
        })
    }
}

impl Render for DotGrid {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let entry = Entry::read(&mut self.born, window, cx);
        self.tip.tick(window);
        let cells = (0..self.capacity)
            .map(|index| {
                let column = index % GRID_COLUMNS;
                let color = match self.slot(index) {
                    Slot::Part(part) => self.parts.get(part).map_or(ink(&theme, EMPTY), |slice| {
                        let dim = self.selected.is_some_and(|picked| picked != part);
                        let alpha = if dim { DIMMED } else { 1.0 };
                        rise(
                            faded(slice.color, alpha),
                            entry.cell(share(column, GRID_COLUMNS)),
                        )
                    }),
                    Slot::Fork => tint(rgb(ROSE), FORK_CELL),
                    Slot::Free => ink(&theme, EMPTY),
                };
                (
                    column,
                    (index / GRID_COLUMNS) as f32 * (GRID_CELL + GRID_GAP),
                    color,
                )
            })
            .collect();
        hovered(
            div()
                .id("dot-grid")
                .relative()
                .size_full()
                .cursor_pointer()
                .child(self.tip.probe())
                .child(Self::lattice().paint(cells).size_full()),
            cx,
        )
        .on_click(cx.listener(|grid, _: &ClickEvent, _, cx| {
            if let Some(Slot::Part(part)) = grid.tip.at().map(|index| grid.slot(index)) {
                grid.selected = (grid.selected != Some(part)).then_some(part);
                cx.notify();
            }
        }))
    }
}

pub struct DotMeter {
    percent: Option<u32>,
    projection: SharedString,
    projection_warn: bool,
    reset: SharedString,
    tip: Tip,
    born: Option<Instant>,
}

impl DotMeter {
    pub fn new(
        percent: Option<u32>,
        projection: impl Into<SharedString>,
        projection_warn: bool,
        reset: impl Into<SharedString>,
    ) -> Self {
        DotMeter {
            percent,
            projection: projection.into(),
            projection_warn,
            reset: reset.into(),
            tip: Tip::new(PIN_CELL),
            born: None,
        }
    }

    fn filled(percent: u32) -> usize {
        ((percent + METER_STEP / 2) / METER_STEP) as usize
    }

    fn lattice() -> Lattice {
        Lattice {
            count: METER_CELLS,
            gap: METER_GAP,
            height: METER_CELL,
            radius: METER_RADIUS,
        }
    }
}

impl Chart for DotMeter {
    fn extent(&self) -> (Option<f32>, f32) {
        (None, METER_HEIGHT)
    }

    fn tip(&self) -> &Tip {
        &self.tip
    }

    fn aim(&self, _: (f32, f32), area: (f32, f32)) -> Option<Aim> {
        let percent = self.percent?;
        let last = Self::filled(percent).clamp(1, METER_CELLS) - 1;
        let label = if self.projection.is_empty() {
            self.reset.clone()
        } else {
            format!("{} · {}", self.projection, self.reset).into()
        };
        Some(Aim {
            at: 0,
            anchor: (
                Self::lattice().center(last, area.0.min(METER_MAX)),
                METER_TOP,
            ),
            said: Said::new(format!("{percent}%"), label),
        })
    }
}

impl Render for DotMeter {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let entry = Entry::read(&mut self.born, window, cx);
        self.tip.tick(window);
        let reset = div()
            .h(px(LIMIT_SMALL_LINE))
            .text_size(px(LIMIT_SMALL))
            .line_height(px(LIMIT_SMALL_LINE))
            .text_color(ink(&theme, T3))
            .truncate()
            .child(self.reset.clone());
        let frame = div()
            .id("dot-meter")
            .relative()
            .size_full()
            .flex()
            .flex_col()
            .gap(px(LIMIT_GAP))
            .child(self.tip.probe());
        let Some(percent) = self.percent else {
            return hovered(frame.child(reset), cx);
        };
        let filled = Self::filled(percent);
        let on = if percent >= METER_FULL || percent > METER_WARN_ABOVE {
            rgb(AMBER)
        } else {
            ink(&theme, METER_ON)
        };
        let cells = (0..METER_CELLS)
            .map(|cell| {
                let color = if cell < filled {
                    rise(on, entry.cell(share(cell, METER_CELLS)))
                } else {
                    ink(&theme, METER_OFF)
                };
                (cell, 0.0, color)
            })
            .collect();
        let projection = if self.projection_warn {
            rgb(AMBER)
        } else {
            ink(&theme, LIMIT_PROJECTION)
        };
        hovered(
            frame
                .child(
                    div()
                        .h(px(LIMIT_PERCENT_LINE))
                        .flex()
                        .items_baseline()
                        .gap(px(CHART_GAP - LIMIT_GAP))
                        .min_w_0()
                        .child(
                            div()
                                .font_family(mono(&theme))
                                .text_size(px(LIMIT_PERCENT))
                                .line_height(px(LIMIT_PERCENT_LINE))
                                .child(format!("{percent}%")),
                        )
                        .child(
                            div()
                                .min_w_0()
                                .truncate()
                                .text_size(px(LIMIT_SMALL))
                                .text_color(projection)
                                .child(self.projection.clone()),
                        ),
                )
                .child(
                    Self::lattice()
                        .paint(cells)
                        .w_full()
                        .max_w(px(METER_MAX))
                        .h(px(METER_CELL)),
                )
                .child(reset),
            cx,
        )
    }
}

pub struct Sparkline {
    values: Vec<u32>,
    tips: Vec<Said>,
    tip: Tip,
    born: Option<Instant>,
}

impl Sparkline {
    pub fn new(values: Vec<u32>, tips: Vec<Said>) -> Self {
        Sparkline {
            values,
            tips,
            tip: Tip::new(PIN_POINT),
            born: None,
        }
    }

    fn points(&self) -> Vec<(f32, f32)> {
        let top = self.values.iter().copied().max().unwrap_or(0).max(1) as f32;
        let gaps = self.values.len().saturating_sub(1).max(1) as f32;
        self.values
            .iter()
            .enumerate()
            .map(|(index, &value)| {
                (
                    index as f32 * SPARK_VIEW.0 / gaps,
                    SPARK_BASE - value as f32 / top * SPARK_RISE,
                )
            })
            .collect()
    }
}

impl Chart for Sparkline {
    fn extent(&self) -> (Option<f32>, f32) {
        (Some(SPARK_VIEW.0), SPARK_VIEW.1)
    }

    fn tip(&self) -> &Tip {
        &self.tip
    }

    fn aim(&self, at: (f32, f32), _: (f32, f32)) -> Option<Aim> {
        let (index, anchor, _) = nearest(self.points().into_iter(), at, LINE_AIM_Y)?;
        Some(Aim {
            at: index,
            anchor,
            said: self.tips.get(index).cloned().unwrap_or_default(),
        })
    }
}

impl Render for Sparkline {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let entry = Entry::read(&mut self.born, window, cx);
        let points = self.points();
        let end = points.last().copied().unwrap_or((SPARK_VIEW.0, SPARK_BASE));
        self.tip.tick(window);
        hovered(
            div()
                .id("sparkline")
                .relative()
                .size_full()
                .child(self.tip.probe())
                .child(
                    plot(
                        SPARK_VIEW,
                        vec![Shape::line(
                            points,
                            ink(&theme, SPARK_INK),
                            SPARK_WIDTH,
                            None,
                        )],
                        vec![(end, SPARK_END, theme.color(ColorToken::TextStrong))],
                        entry.wipe(),
                    )
                    .size_full(),
                ),
            cx,
        )
    }
}

pub struct ContextLine {
    points: Vec<(f32, f32)>,
    tips: Vec<Said>,
    threshold: f32,
    compacted_at: f32,
    reports: Vec<(f32, f32)>,
    tip: Tip,
    born: Option<Instant>,
}

impl ContextLine {
    pub fn new(
        points: Vec<(f32, f32)>,
        tips: Vec<Said>,
        threshold: f32,
        compacted_at: f32,
        reports: Vec<(f32, f32)>,
    ) -> Self {
        ContextLine {
            points,
            tips,
            threshold,
            compacted_at,
            reports,
            tip: Tip::new(PIN_POINT),
            born: None,
        }
    }
}

impl Chart for ContextLine {
    fn extent(&self) -> (Option<f32>, f32) {
        (None, LINE_VIEW.1)
    }

    fn tip(&self) -> &Tip {
        &self.tip
    }

    fn aim(&self, at: (f32, f32), area: (f32, f32)) -> Option<Aim> {
        let scale = (area.0 / LINE_VIEW.0, area.1 / LINE_VIEW.1);
        let spots = self.points.iter().map(|&(x, y)| (x * scale.0, y * scale.1));
        let (index, anchor, _) = nearest(spots, at, LINE_AIM_Y)?;
        Some(Aim {
            at: index,
            anchor,
            said: self.tips.get(index).cloned().unwrap_or_default(),
        })
    }
}

impl Render for ContextLine {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let entry = Entry::read(&mut self.born, window, cx);
        let mut area = self.points.clone();
        area.extend([(LINE_VIEW.0, LINE_VIEW.1), (0.0, LINE_VIEW.1)]);
        let shapes = vec![
            Shape::line(
                vec![(0.0, self.threshold), (LINE_VIEW.0, self.threshold)],
                tint(rgb(ROSE), THRESHOLD_INK),
                1.0,
                Some(THRESHOLD_DASH),
            ),
            Shape::area(area, ink(&theme, LINE_AREA), None),
            Shape::line(self.points.clone(), ink(&theme, LINE_INK), LINE_WIDTH, None),
            Shape::line(
                vec![
                    (self.compacted_at, COMPACT_SPAN.0),
                    (self.compacted_at, COMPACT_SPAN.1),
                ],
                ink(&theme, COMPACT_INK),
                1.0,
                Some(COMPACT_DASH),
            ),
        ];
        let reports = self
            .reports
            .iter()
            .map(|&spot| (spot, REPORT_RADIUS, rgb(LILAC)))
            .collect();
        self.tip.tick(window);
        hovered(
            div()
                .id("context-line")
                .relative()
                .size_full()
                .child(self.tip.probe())
                .child(plot(LINE_VIEW, shapes, reports, entry.wipe()).size_full()),
            cx,
        )
    }
}

#[derive(Clone, Copy)]
pub struct RibbonSegment {
    pub name: &'static str,
    pub kind: &'static str,
    pub lane: usize,
    pub x0: f32,
    pub context: &'static [u16],
    pub turns: &'static [&'static str],
}

impl RibbonSegment {
    fn x(&self, turn: usize) -> f32 {
        self.x0 + turn as f32 * RIBBON_STEP
    }

    fn y(&self) -> f32 {
        RIBBON_LANES
            .get(self.lane)
            .copied()
            .unwrap_or(RIBBON_LANES[0])
    }

    fn last(&self) -> usize {
        self.context.len().saturating_sub(1)
    }

    fn half(&self, turn: usize) -> f32 {
        self.context.get(turn).map_or(RIBBON_FLOOR, |&value| {
            RIBBON_FLOOR + f32::from(value) / RIBBON_CAPACITY * RIBBON_RISE
        }) / 2.0
    }
}

#[derive(Clone, Copy)]
pub struct RibbonNote {
    pub at: (f32, f32),
    pub title: &'static str,
    pub value: Option<&'static str>,
}

#[derive(Clone, Copy)]
pub struct RibbonBranch {
    pub from: usize,
    pub turn: usize,
    pub to: usize,
}

pub struct ContextRibbon {
    segments: Vec<RibbonSegment>,
    funnels: Vec<(usize, usize)>,
    branch: Option<RibbonBranch>,
    notes: Vec<RibbonNote>,
    legend: SharedString,
    live: usize,
    scale: f32,
    selected: (usize, usize),
    tip: Tip,
    born: Option<Instant>,
}

impl ContextRibbon {
    pub fn new(
        segments: Vec<RibbonSegment>,
        funnels: Vec<(usize, usize)>,
        branch: Option<RibbonBranch>,
        notes: Vec<RibbonNote>,
        legend: impl Into<SharedString>,
        live: usize,
    ) -> Self {
        let selected = segments
            .get(live)
            .map_or((0, 0), |segment| (live, segment.last()));
        ContextRibbon {
            segments,
            funnels,
            branch,
            notes,
            legend: legend.into(),
            live,
            scale: 1.0,
            selected,
            tip: Tip::new(PIN_POINT),
            born: None,
        }
    }

    pub fn scale(mut self, scale: f32) -> Self {
        self.scale = scale;
        self
    }

    fn turns(&self) -> impl Iterator<Item = (usize, usize, &RibbonSegment)> {
        self.segments
            .iter()
            .enumerate()
            .flat_map(|(index, segment)| {
                (0..segment.context.len()).map(move |turn| (index, turn, segment))
            })
    }

    fn shapes(&self, theme: &Theme) -> Vec<Shape> {
        let mut shapes = Vec::new();
        let funnel_stroke = Some((ink(theme, FUNNEL_EDGE), 1.0, Some(FUNNEL_DASH)));
        for &(from, to) in &self.funnels {
            let (Some(a), Some(b)) = (self.segments.get(from), self.segments.get(to)) else {
                continue;
            };
            let (xa, xb, y) = (a.x(a.last()), b.x0, a.y());
            let (ha, hb) = (a.half(a.last()), b.half(0));
            shapes.push(Shape::area(
                vec![(xa, y - ha), (xb, y - hb), (xb, y + hb), (xa, y + ha)],
                ink(theme, FUNNEL_FILL),
                funnel_stroke,
            ));
        }
        if let Some(branch) = self.branch
            && let (Some(from), Some(to)) =
                (self.segments.get(branch.from), self.segments.get(branch.to))
        {
            let start = (from.x(branch.turn), from.y());
            let first = (start.0, to.y() - BRANCH_BEND);
            let second = (start.0 + BRANCH_LEAD, to.y());
            let end = (to.x0, to.y());
            let curve = (0..=BRANCH_STEPS)
                .map(|step| {
                    let t = step as f32 / BRANCH_STEPS as f32;
                    let u = 1.0 - t;
                    let mix = |a: f32, b: f32, c: f32, d: f32| {
                        u * u * u * a + 3.0 * u * u * t * b + 3.0 * u * t * t * c + t * t * t * d
                    };
                    (
                        mix(start.0, first.0, second.0, end.0),
                        mix(start.1, first.1, second.1, end.1),
                    )
                })
                .collect();
            shapes.push(Shape::line(
                curve,
                ink(theme, BRANCH_INK),
                BRANCH_WIDTH,
                Some(BRANCH_DASH),
            ));
        }
        for (index, segment) in self.segments.iter().enumerate() {
            let on = index == self.selected.0;
            let y = segment.y();
            let top =
                (0..segment.context.len()).map(|turn| (segment.x(turn), y - segment.half(turn)));
            let bottom = (0..segment.context.len())
                .rev()
                .map(|turn| (segment.x(turn), y + segment.half(turn)));
            shapes.push(Shape::area(
                top.chain(bottom).collect(),
                ink(theme, if on { RIBBON_ON } else { RIBBON_OFF }),
                Some((
                    ink(theme, if on { RIBBON_EDGE_ON } else { RIBBON_EDGE_OFF }),
                    1.0,
                    None,
                )),
            ));
        }
        if let Some(live) = self.segments.get(self.live) {
            let (x, y) = (live.x(live.last()), live.y());
            shapes.push(Shape::line(
                vec![(x, y), (x + NOW_LENGTH, y)],
                tint(rgb(MINT), NOW_INK),
                BRANCH_WIDTH,
                Some(NOW_DASH),
            ));
        }
        if let Some(branch) = self.branch
            && let Some(to) = self.segments.get(branch.to)
        {
            let x = to.x(to.last()) + END_REACH;
            shapes.push(Shape::line(
                vec![(x, to.y() - END_HALF), (x, to.y() + END_HALF)],
                ink(theme, END_INK),
                END_WIDTH,
                None,
            ));
        }
        shapes
    }

    fn dots(&self, theme: &Theme, entry: &Entry) -> Vec<Dot> {
        let mut dots = Vec::new();
        for (index, turn, segment) in self.turns() {
            let shown = entry.cell(share(turn, segment.context.len()));
            let center = (segment.x(turn), segment.y());
            if self.selected == (index, turn) {
                dots.push((
                    center,
                    DOT_PICKED + DOT_HALO,
                    faded(ink(theme, POINT_RING), shown),
                ));
                dots.push((
                    center,
                    DOT_PICKED,
                    faded(theme.color(ColorToken::TextStrong), shown),
                ));
            } else {
                let plain = ink(
                    theme,
                    if index == self.selected.0 {
                        DOT_ON
                    } else {
                        DOT_OFF
                    },
                );
                dots.push((center, DOT_PLAIN, faded(plain, shown)));
            }
        }
        dots
    }
}

impl Chart for ContextRibbon {
    fn extent(&self) -> (Option<f32>, f32) {
        (Some(RIBBON_VIEW.0 * self.scale), RIBBON_VIEW.1 * self.scale)
    }

    fn tip(&self) -> &Tip {
        &self.tip
    }

    fn aim(&self, at: (f32, f32), _: (f32, f32)) -> Option<Aim> {
        let spots = self
            .turns()
            .map(|(_, turn, segment)| (segment.x(turn) * self.scale, segment.y() * self.scale));
        let (flat, anchor, reach) = nearest(spots, at, 1.0)?;
        let (_, turn, segment) = self.turns().nth(flat)?;
        (reach <= RIBBON_STEP * RIBBON_REACH_STEPS * self.scale).then(|| Aim {
            at: flat,
            anchor,
            said: Said::new(
                format!(
                    "{}k",
                    segment.context.get(turn).copied().unwrap_or_default()
                ),
                format!(
                    "turn {} · {}",
                    turn + 1,
                    segment.turns.get(turn).copied().unwrap_or_default()
                ),
            ),
        })
    }
}

impl Render for ContextRibbon {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let entry = Entry::read(&mut self.born, window, cx);
        let scale = self.scale;
        let at = |(x, y): (f32, f32)| (px(x * scale), px(y * scale));
        let labelled = scale >= LABELS_FROM_SCALE;
        self.tip.tick(window);
        let mut area = div()
            .id("context-ribbon")
            .relative()
            .size_full()
            .child(self.tip.probe())
            .child(
                div()
                    .absolute()
                    .inset_0()
                    .opacity(BACKDROP_INK)
                    .child(dots(&theme)),
            )
            .child(
                plot(
                    RIBBON_VIEW,
                    self.shapes(&theme),
                    self.dots(&theme, &entry),
                    entry.wipe(),
                )
                .absolute()
                .inset_0(),
            );
        if labelled {
            let (left, top) = at(LEGEND_AT);
            area = area.child(
                div()
                    .absolute()
                    .left(left)
                    .top(top)
                    .flex()
                    .items_center()
                    .gap(px(CHART_GAP - LIMIT_GAP))
                    .text_size(px(LEGEND_TEXT))
                    .text_color(ink(&theme, T3))
                    .child(
                        plot(
                            LEGEND_SHAPE,
                            vec![Shape::area(
                                LEGEND_POINTS.to_vec(),
                                ink(&theme, LEGEND_FILL),
                                Some((ink(&theme, LEGEND_EDGE), 1.0, None)),
                            )],
                            Vec::new(),
                            (1.0, 1.0),
                        )
                        .w(px(LEGEND_SHAPE.0))
                        .h(px(LEGEND_SHAPE.1)),
                    )
                    .child(self.legend.clone()),
            );
            for note in &self.notes {
                let (left, top) = at(note.at);
                let title = div().text_color(ink(&theme, T2)).child(note.title);
                let body = match note.value {
                    Some(value) => div()
                        .w(px(NOTE_WIDTH))
                        .flex()
                        .flex_col()
                        .items_center()
                        .child(title)
                        .child(
                            div()
                                .font_family(mono(&theme))
                                .text_color(ink(&theme, T3))
                                .child(value),
                        ),
                    None => title.text_color(ink(&theme, T3)),
                };
                area = area.child(
                    body.absolute()
                        .left(left)
                        .top(top)
                        .text_size(px(AXIS_TEXT))
                        .line_height(px(NOTE_LINE)),
                );
            }
            let (left, top) = at(LIVE_AT);
            area = area.child(
                div()
                    .absolute()
                    .left(left)
                    .top(top)
                    .text_size(px(AXIS_TEXT))
                    .text_color(theme.color(ColorToken::StatusLive))
                    .child("running"),
            );
            for (index, segment) in self.segments.iter().enumerate() {
                let middle = (segment.x(0) + segment.x(segment.last())) / 2.0;
                let top = if segment.lane == 0 {
                    segment.y() - LABEL_ABOVE
                } else {
                    segment.y() + LABEL_BELOW
                };
                let (left, top) = at((middle, top));
                area = area.child(
                    div()
                        .id(("ribbon-label", index))
                        .absolute()
                        .left(left - px(LABEL_WIDTH / 2.0))
                        .top(top)
                        .w(px(LABEL_WIDTH))
                        .flex()
                        .flex_col()
                        .items_center()
                        .cursor_pointer()
                        .line_height(px(LABEL_LINE))
                        .text_size(px(LABEL_TEXT))
                        .text_color(if index == self.selected.0 {
                            theme.color(ColorToken::TextStrong)
                        } else {
                            ink(&theme, LABEL_OFF)
                        })
                        .on_click(cx.listener(move |ribbon, _: &ClickEvent, _, cx| {
                            let last = ribbon.segments.get(index).map_or(0, RibbonSegment::last);
                            ribbon.selected = (index, last);
                            cx.notify();
                        }))
                        .child(segment.name)
                        .child(
                            div()
                                .text_size(px(AXIS_TEXT))
                                .text_color(ink(&theme, T3))
                                .child(format!(
                                    "{} · {} turns",
                                    segment.kind,
                                    segment.context.len()
                                )),
                        ),
                );
            }
        }
        if let Some(live) = self.segments.get(self.live) {
            let (left, top) = at((live.x(live.last()), live.y()));
            area = area.child(
                div()
                    .absolute()
                    .left(left - px(HEAD / 2.0))
                    .top(top - px(HEAD / 2.0))
                    .size(px(HEAD))
                    .rounded_full()
                    .bg(tint(rgb(MINT), HEAD_INK)),
            );
        }
        hovered(area, cx).on_click(cx.listener(|ribbon, _: &ClickEvent, _, cx| {
            let picked = ribbon
                .tip
                .at()
                .and_then(|flat| ribbon.turns().nth(flat))
                .map(|(index, turn, _)| (index, turn));
            if let Some(picked) = picked {
                ribbon.selected = picked;
                cx.notify();
            }
        }))
    }
}

fn levels(series: &[Series], stacked: bool) -> Vec<Vec<(f32, f32)>> {
    let mut floor: Vec<f32> = Vec::new();
    let mut levels = Vec::with_capacity(series.len());
    for line in series {
        let mut level = Vec::with_capacity(line.values.len());
        for (index, &value) in line.values.iter().enumerate() {
            let base = if stacked {
                floor.get(index).copied().unwrap_or(0.0)
            } else {
                0.0
            };
            if stacked {
                match floor.get_mut(index) {
                    Some(slot) => *slot = base + value,
                    None => floor.push(base + value),
                }
            }
            level.push((base, base + value));
        }
        levels.push(level);
    }
    levels
}

fn peak(levels: &[Vec<(f32, f32)>]) -> f32 {
    levels
        .iter()
        .flatten()
        .map(|&(_, top)| top)
        .fold(0.0, f32::max)
        .max(f32::EPSILON)
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum TrendFill {
    Line,
    Gradient,
    Solid,
    Dotted,
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum TrendStroke {
    Solid,
    Dashed,
    Glowing,
}

pub struct Trend {
    series: Vec<Series>,
    tips: Vec<Said>,
    axis: (SharedString, SharedString),
    fill: TrendFill,
    stroke: TrendStroke,
    stacked: bool,
    resting: bool,
    tip: Tip,
    born: Option<Instant>,
}

impl Trend {
    pub fn new(
        series: Vec<Series>,
        tips: Vec<Said>,
        axis: (SharedString, SharedString),
        fill: TrendFill,
        stroke: TrendStroke,
    ) -> Self {
        Trend {
            series,
            tips,
            axis,
            fill,
            stroke,
            stacked: false,
            resting: false,
            tip: Tip::new(PIN_ACTIVE),
            born: None,
        }
    }

    pub fn stacked(mut self) -> Self {
        self.stacked = true;
        self
    }

    pub fn resting_dots(mut self) -> Self {
        self.resting = true;
        self
    }

    fn count(&self) -> usize {
        self.series
            .iter()
            .map(|line| line.values.len())
            .max()
            .unwrap_or(0)
    }

    fn height(value: f32, peak: f32) -> f32 {
        TREND_PLOT - value / peak * TREND_PLOT * TREND_HEADROOM
    }

    fn fill_shape(&self, color: Rgba, level: &[(f32, f32)], peak: f32) -> Vec<Shape> {
        let top = level
            .iter()
            .enumerate()
            .map(|(index, &(_, top))| (index as f32, Self::height(top, peak)));
        let base = level
            .iter()
            .enumerate()
            .rev()
            .map(|(index, &(base, _))| (index as f32, Self::height(base, peak)));
        let outline: Vec<(f32, f32)> = top.chain(base).collect();
        match self.fill {
            TrendFill::Gradient => vec![Shape::area(
                outline,
                linear_gradient(
                    180.0,
                    linear_color_stop(tint(color, TREND_GRADIENT_TOP), 0.0),
                    linear_color_stop(tint(color, 0.0), 1.0),
                ),
                None,
            )],
            TrendFill::Solid => vec![Shape::area(outline, tint(color, TREND_SOLID), None)],
            TrendFill::Line | TrendFill::Dotted => Vec::new(),
        }
    }

    fn texture(color: Rgba, level: &[(f32, f32)], peak: f32) -> Vec<Dot> {
        let mut dots = Vec::new();
        let columns = level.len().saturating_sub(1) * TREND_DOT_COLUMNS;
        for column in 0..=columns {
            let x = column as f32 / TREND_DOT_COLUMNS as f32;
            let (left, right) = (
                column / TREND_DOT_COLUMNS,
                column.div_ceil(TREND_DOT_COLUMNS),
            );
            let (Some(a), Some(b)) = (level.get(left), level.get(right)) else {
                continue;
            };
            let t = x - left as f32;
            let top = Self::height(a.1 + (b.1 - a.1) * t, peak);
            let mut y = Self::height(a.0 + (b.0 - a.0) * t, peak) - TREND_DOT_PITCH / 2.0;
            while y > top {
                dots.push(((x, y), TREND_DOT, tint(color, TREND_DOTTED)));
                y -= TREND_DOT_PITCH;
            }
        }
        dots
    }
}

impl Chart for Trend {
    fn extent(&self) -> (Option<f32>, f32) {
        (None, TREND_PLOT + CHART_GAP + AXIS_LINE)
    }

    fn tip(&self) -> &Tip {
        &self.tip
    }

    fn aim(&self, at: (f32, f32), area: (f32, f32)) -> Option<Aim> {
        let count = self.count();
        let last = count.checked_sub(1)?;
        let pitch = area.0 / last.max(1) as f32;
        let index = ((at.0 / pitch).round().max(0.0) as usize).min(last);
        let levels = levels(&self.series, self.stacked);
        let top = levels
            .iter()
            .filter_map(|level| level.get(index))
            .map(|&(_, top)| top)
            .fold(0.0, f32::max);
        Some(Aim {
            at: index,
            anchor: (index as f32 * pitch, Self::height(top, peak(&levels))),
            said: self.tips.get(index).cloned().unwrap_or_default(),
        })
    }
}

impl Render for Trend {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let entry = Entry::read(&mut self.born, window, cx);
        self.tip.tick(window);
        let hole = theme.color(ColorToken::CardsInnerFill);
        let view = (self.count().saturating_sub(1).max(1) as f32, TREND_PLOT);
        let levels = levels(&self.series, self.stacked);
        let peak = peak(&levels);
        let mut shapes: Vec<Shape> = (1..GUIDE_LINES)
            .map(|line| {
                let y = TREND_PLOT * line as f32 / GUIDE_LINES as f32;
                Shape::line(
                    vec![(0.0, y), (view.0, y)],
                    ink(&theme, GUIDE_INK),
                    1.0,
                    Some(CURSOR_DASH),
                )
            })
            .collect();
        let mut dots = Vec::new();
        for (line, level) in self.series.iter().zip(&levels) {
            shapes.extend(self.fill_shape(line.color, level, peak));
            if self.fill == TrendFill::Dotted {
                dots.extend(Self::texture(line.color, level, peak));
            }
        }
        for (line, level) in self.series.iter().zip(&levels) {
            let top: Vec<(f32, f32)> = level
                .iter()
                .enumerate()
                .map(|(index, &(_, top))| (index as f32, Self::height(top, peak)))
                .collect();
            if self.stroke == TrendStroke::Glowing {
                for (width, alpha) in GLOW {
                    shapes.push(Shape::line(
                        top.clone(),
                        tint(line.color, alpha),
                        width,
                        None,
                    ));
                }
            }
            let dash = (self.stroke == TrendStroke::Dashed).then_some(TREND_DASH);
            if self.resting {
                dots.extend(top.iter().flat_map(|&spot| {
                    ringed(spot, REST_HOLE, REST_DOT - REST_HOLE, line.color, hole)
                }));
            }
            shapes.push(Shape::line(top, line.color, TREND_STROKE, dash));
        }
        if let Some(index) = self.tip.at() {
            let shade = self.tip.shade();
            shapes.push(Shape::line(
                vec![(index as f32, 0.0), (index as f32, TREND_PLOT)],
                ink(&theme, CURSOR_INK * shade),
                1.0,
                Some(CURSOR_DASH),
            ));
            for (line, level) in self.series.iter().zip(&levels) {
                if let Some(&(_, top)) = level.get(index) {
                    dots.extend(ringed(
                        (index as f32, Self::height(top, peak)),
                        ACTIVE_DOT,
                        ACTIVE_RIM,
                        faded(line.color, shade),
                        faded(hole, shade),
                    ));
                }
            }
        }
        hovered(
            div()
                .id("trend")
                .relative()
                .size_full()
                .flex()
                .flex_col()
                .gap(px(CHART_GAP))
                .child(self.tip.probe())
                .child(
                    plot(view, shapes, dots, entry.wipe())
                        .w_full()
                        .h(px(TREND_PLOT)),
                )
                .child(axis_row(&self.axis, &theme)),
            cx,
        )
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum BarLook {
    Plain,
    Duotone,
    Hatched,
    Gradient,
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum BarLayout {
    Columns,
    Rows,
}

struct Segment {
    slot: usize,
    from: f32,
    to: f32,
    color: Rgba,
    foot: bool,
    cap: bool,
}

pub struct Bars {
    series: Vec<Series>,
    labels: Vec<SharedString>,
    tips: Vec<Said>,
    look: BarLook,
    layout: BarLayout,
    tip: Tip,
    born: Option<Instant>,
}

impl Bars {
    pub fn new(
        series: Vec<Series>,
        labels: Vec<SharedString>,
        tips: Vec<Said>,
        look: BarLook,
        layout: BarLayout,
    ) -> Self {
        Bars {
            series,
            labels,
            tips,
            look,
            layout,
            tip: Tip::new(PIN_CELL),
            born: None,
        }
    }

    fn count(&self) -> usize {
        self.series
            .iter()
            .map(|line| line.values.len())
            .max()
            .unwrap_or(0)
    }

    fn row_pitch() -> f32 {
        BAR_ROW + BAR_ROW_GAP
    }

    fn segments(&self, grown: impl Fn(usize) -> f32, dim: impl Fn(usize) -> f32) -> Vec<Segment> {
        let levels = levels(&self.series, true);
        let peak = peak(&levels);
        let last = self.series.len().saturating_sub(1);
        let mut segments = Vec::new();
        for (index, (line, level)) in self.series.iter().zip(&levels).enumerate() {
            for (slot, &(base, top)) in level.iter().enumerate() {
                let grow = grown(slot);
                segments.push(Segment {
                    slot,
                    from: base / peak * grow,
                    to: top / peak * grow,
                    color: faded(line.color, dim(slot)),
                    foot: index == 0,
                    cap: index == last,
                });
            }
        }
        segments
    }

    fn corners(layout: BarLayout, foot: bool, cap: bool) -> Corners<Pixels> {
        let round = |on: bool| if on { px(BAR_RADIUS) } else { px(0.0) };
        match layout {
            BarLayout::Columns => Corners {
                top_left: round(cap),
                top_right: round(cap),
                bottom_right: round(foot),
                bottom_left: round(foot),
            },
            BarLayout::Rows => Corners {
                top_left: round(foot),
                top_right: round(cap),
                bottom_right: round(cap),
                bottom_left: round(foot),
            },
        }
    }

    fn paint(look: BarLook, layout: BarLayout, count: usize, segments: Vec<Segment>) -> Canvas<()> {
        canvas(
            |_, _, _| (),
            move |bounds: Bounds<Pixels>, (), window, _| {
                let (width, height) = (f32::from(bounds.size.width), f32::from(bounds.size.height));
                for segment in &segments {
                    let (x, y, w, h) = match layout {
                        BarLayout::Columns => {
                            let pitch = width / count.max(1) as f32;
                            let thick = pitch * BAR_FILL;
                            let x = segment.slot as f32 * pitch + (pitch - thick) / 2.0;
                            let top = height * (1.0 - segment.to);
                            (x, top, thick, height * (segment.to - segment.from))
                        }
                        BarLayout::Rows => (
                            width * segment.from,
                            segment.slot as f32 * Self::row_pitch(),
                            width * (segment.to - segment.from),
                            BAR_ROW,
                        ),
                    };
                    if w <= 0.0 || h <= 0.0 {
                        continue;
                    }
                    let origin = bounds.origin + point(px(x), px(y));
                    let rect = Bounds::new(origin, size(px(w), px(h)));
                    let corners = Self::corners(layout, segment.foot, segment.cap);
                    let color = segment.color;
                    match look {
                        BarLook::Plain => {
                            window.paint_quad(fill(rect, color).corner_radii(corners))
                        }
                        BarLook::Duotone => {
                            window.paint_quad(
                                fill(rect, faded(color, DUOTONE_FAINT)).corner_radii(corners),
                            );
                            let (half, mut kept) = match layout {
                                BarLayout::Columns => (
                                    Bounds::new(
                                        origin + point(px(w / 2.0), px(0.0)),
                                        size(px(w / 2.0), px(h)),
                                    ),
                                    corners,
                                ),
                                BarLayout::Rows => (
                                    Bounds::new(
                                        origin + point(px(0.0), px(h / 2.0)),
                                        size(px(w), px(h / 2.0)),
                                    ),
                                    corners,
                                ),
                            };
                            match layout {
                                BarLayout::Columns => {
                                    kept.top_left = px(0.0);
                                    kept.bottom_left = px(0.0);
                                }
                                BarLayout::Rows => {
                                    kept.top_left = px(0.0);
                                    kept.top_right = px(0.0);
                                }
                            }
                            window.paint_quad(fill(half, color).corner_radii(kept));
                        }
                        BarLook::Hatched => {
                            window.paint_quad(
                                fill(rect, faded(color, HATCH_BASE)).corner_radii(corners),
                            );
                            window.paint_quad(
                                fill(rect, pattern_slash(color, HATCH.0, HATCH.1))
                                    .corner_radii(corners),
                            );
                        }
                        BarLook::Gradient => {
                            let angle = match layout {
                                BarLayout::Columns => 180.0,
                                BarLayout::Rows => 270.0,
                            };
                            window.paint_quad(
                                fill(
                                    rect,
                                    linear_gradient(
                                        angle,
                                        linear_color_stop(color, GRADIENT_STOPS.0),
                                        linear_color_stop(tint(color, 0.0), GRADIENT_STOPS.1),
                                    ),
                                )
                                .corner_radii(corners),
                            );
                        }
                    }
                }
            },
        )
    }
}

impl Chart for Bars {
    fn extent(&self) -> (Option<f32>, f32) {
        match self.layout {
            BarLayout::Columns => (None, BAR_PLOT + CHART_GAP + AXIS_LINE),
            BarLayout::Rows => (
                None,
                (self.count() as f32 * Self::row_pitch() - BAR_ROW_GAP).max(0.0),
            ),
        }
    }

    fn tip(&self) -> &Tip {
        &self.tip
    }

    fn aim(&self, at: (f32, f32), area: (f32, f32)) -> Option<Aim> {
        let count = self.count();
        let last = count.checked_sub(1)?;
        let levels = levels(&self.series, true);
        let peak = peak(&levels);
        let total = |slot: usize| {
            levels
                .iter()
                .filter_map(|level| level.get(slot))
                .map(|&(_, top)| top)
                .fold(0.0, f32::max)
                / peak
        };
        let (slot, anchor) = match self.layout {
            BarLayout::Columns => {
                let pitch = area.0 / count as f32;
                let slot = ((at.0.max(0.0) / pitch) as usize).min(last);
                (
                    slot,
                    ((slot as f32 + 0.5) * pitch, BAR_PLOT * (1.0 - total(slot))),
                )
            }
            BarLayout::Rows => {
                let slot = ((at.1.max(0.0) / Self::row_pitch()) as usize).min(last);
                let span = area.0 - BAR_LABEL;
                (
                    slot,
                    (
                        BAR_LABEL + span * total(slot),
                        slot as f32 * Self::row_pitch(),
                    ),
                )
            }
        };
        Some(Aim {
            at: slot,
            anchor,
            said: self.tips.get(slot).cloned().unwrap_or_default(),
        })
    }
}

impl Render for Bars {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let entry = Entry::read(&mut self.born, window, cx);
        self.tip.tick(window);
        let count = self.count();
        let segments = self.segments(
            |slot| entry.cell(share(slot, count)),
            |slot| self.tip.dim(slot),
        );
        let bars = Self::paint(self.look, self.layout, count, segments);
        let frame = div()
            .id("bars")
            .relative()
            .size_full()
            .flex()
            .child(self.tip.probe());
        let frame = match self.layout {
            BarLayout::Columns => {
                let axis = (
                    self.labels.first().cloned().unwrap_or_default(),
                    self.labels.last().cloned().unwrap_or_default(),
                );
                frame
                    .flex_col()
                    .gap(px(CHART_GAP))
                    .child(bars.w_full().h(px(BAR_PLOT)))
                    .child(axis_row(&axis, &theme))
            }
            BarLayout::Rows => frame
                .child(
                    div()
                        .w(px(BAR_LABEL))
                        .flex_none()
                        .flex()
                        .flex_col()
                        .gap(px(BAR_ROW_GAP))
                        .text_size(px(AXIS_TEXT))
                        .line_height(px(BAR_ROW))
                        .font_family(mono(&theme))
                        .children(self.labels.iter().enumerate().map(|(slot, label)| {
                            div()
                                .h(px(BAR_ROW))
                                .truncate()
                                .text_color(ink(&theme, T2 * self.tip.dim(slot)))
                                .child(label.clone())
                        })),
                )
                .child(bars.flex_1().h_full()),
        };
        hovered(frame, cx)
    }
}

pub struct Donut {
    slices: Vec<Slice>,
    hole: f32,
    center: Option<Said>,
    tips: Vec<Said>,
    tip: Tip,
    born: Option<Instant>,
}

impl Donut {
    pub fn new(slices: Vec<Slice>, hole: f32, center: Option<Said>, tips: Vec<Said>) -> Self {
        Donut {
            slices,
            hole,
            center,
            tips,
            tip: Tip::new(PIN_CELL),
            born: None,
        }
    }

    fn spans(&self) -> Vec<(f32, f32)> {
        let total = self
            .slices
            .iter()
            .map(|slice| slice.value)
            .sum::<f32>()
            .max(f32::EPSILON);
        let pad = if self.hole > 0.0 { DONUT_PAD } else { 0.0 };
        let mut start = 0.0;
        self.slices
            .iter()
            .map(|slice| {
                let end = start + slice.value / total * TAU;
                let span = (start + pad / 2.0, (end - pad / 2.0).max(start + pad / 2.0));
                start = end;
                span
            })
            .collect()
    }

    fn center() -> (f32, f32) {
        (ROUND_SIDE / 2.0, ROUND_SIDE / 2.0)
    }
}

impl Chart for Donut {
    fn extent(&self) -> (Option<f32>, f32) {
        (Some(ROUND_SIDE), ROUND_SIDE)
    }

    fn tip(&self) -> &Tip {
        &self.tip
    }

    fn aim(&self, at: (f32, f32), _: (f32, f32)) -> Option<Aim> {
        let (reach, angle) = bearing(Self::center(), at);
        if reach < DONUT_RADIUS * self.hole || reach > DONUT_RADIUS + DONUT_POP {
            return None;
        }
        let spans = self.spans();
        let index = spans
            .iter()
            .position(|&(_, end)| angle <= end)
            .unwrap_or(spans.len().saturating_sub(1));
        let &(from, to) = spans.get(index)?;
        Some(Aim {
            at: index,
            anchor: polar(Self::center(), DONUT_RADIUS + DONUT_POP, (from + to) / 2.0),
            said: self.tips.get(index).cloned().unwrap_or_default(),
        })
    }
}

impl Render for Donut {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let entry = Entry::read(&mut self.born, window, cx);
        self.tip.tick(window);
        let (sweep, alpha) = entry.wipe();
        let reach = sweep * TAU;
        let center = Self::center();
        let shapes = self
            .slices
            .iter()
            .zip(self.spans())
            .enumerate()
            .filter(|(_, (_, (from, _)))| *from < reach)
            .map(|(index, (slice, (from, to)))| {
                let to = to.min(reach);
                let lifted = if self.tip.at() == Some(index) {
                    DONUT_POP * self.tip.shade()
                } else {
                    0.0
                };
                let mut outline = arc(center, DONUT_RADIUS + lifted, from, to);
                if self.hole > 0.0 {
                    outline.extend(arc(center, DONUT_RADIUS * self.hole, to, from));
                } else {
                    outline.push(center);
                }
                Shape::area(outline, faded(slice.color, self.tip.dim(index)), None)
            })
            .collect();
        let middle = self
            .center
            .as_ref()
            .filter(|_| self.hole > 0.0)
            .map(|said| centered(said, &theme));
        hovered(
            div()
                .id("donut")
                .relative()
                .size_full()
                .child(self.tip.probe())
                .child(
                    plot((ROUND_SIDE, ROUND_SIDE), shapes, Vec::new(), (1.0, alpha))
                        .absolute()
                        .inset_0(),
                )
                .children(middle),
            cx,
        )
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum RadialShape {
    Full,
    Semi,
}

pub struct Radial {
    rings: Vec<Slice>,
    max: f32,
    shape: RadialShape,
    center: Option<Said>,
    tips: Vec<Said>,
    tip: Tip,
    born: Option<Instant>,
}

impl Radial {
    pub fn new(
        rings: Vec<Slice>,
        max: f32,
        shape: RadialShape,
        center: Option<Said>,
        tips: Vec<Said>,
    ) -> Self {
        Radial {
            rings,
            max: max.max(f32::EPSILON),
            shape,
            center,
            tips,
            tip: Tip::new(PIN_CELL),
            born: None,
        }
    }

    fn middle(&self) -> (f32, f32) {
        match self.shape {
            RadialShape::Full => (ROUND_SIDE / 2.0, ROUND_SIDE / 2.0),
            RadialShape::Semi => (
                ROUND_SIDE / 2.0,
                RADIAL_OUTER + RING_WIDTH / 2.0 + SEMI_FOOT,
            ),
        }
    }

    fn sweep(&self) -> (f32, f32) {
        match self.shape {
            RadialShape::Full => (0.0, TAU),
            RadialShape::Semi => (-FRAC_PI_2, FRAC_PI_2),
        }
    }

    fn radius(ring: usize) -> f32 {
        RADIAL_OUTER - ring as f32 * (RING_WIDTH + RING_GAP)
    }

    fn end(&self, ring: usize, grow: f32) -> f32 {
        let (from, to) = self.sweep();
        let value = self.rings.get(ring).map_or(0.0, |ring| ring.value);
        from + (to - from) * (value / self.max).clamp(0.0, 1.0) * grow
    }
}

impl Chart for Radial {
    fn extent(&self) -> (Option<f32>, f32) {
        let height = match self.shape {
            RadialShape::Full => ROUND_SIDE,
            RadialShape::Semi => self.middle().1 + SEMI_FOOT,
        };
        (Some(ROUND_SIDE), height)
    }

    fn tip(&self) -> &Tip {
        &self.tip
    }

    fn aim(&self, at: (f32, f32), _: (f32, f32)) -> Option<Aim> {
        let (reach, _) = bearing(self.middle(), at);
        let pitch = RING_WIDTH + RING_GAP;
        let ring = ((RADIAL_OUTER + pitch / 2.0 - reach) / pitch).floor();
        if ring < 0.0 || ring as usize >= self.rings.len() {
            return None;
        }
        let ring = ring as usize;
        let tip = polar(self.middle(), Self::radius(ring), self.end(ring, 1.0));
        Some(Aim {
            at: ring,
            anchor: (tip.0, tip.1 - RING_WIDTH / 2.0),
            said: self.tips.get(ring).cloned().unwrap_or_default(),
        })
    }
}

impl Render for Radial {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let entry = Entry::read(&mut self.born, window, cx);
        self.tip.tick(window);
        let (from, to) = self.sweep();
        let middle = self.middle();
        let mut shapes = Vec::new();
        let mut dots = Vec::new();
        for (index, ring) in self.rings.iter().enumerate() {
            let radius = Self::radius(index);
            let end = self.end(index, entry.cell(share(index, self.rings.len())));
            let track = ink(&theme, RING_TRACK);
            let color = faded(ring.color, self.tip.dim(index));
            shapes.push(Shape::line(
                arc(middle, radius, from, to),
                track,
                RING_WIDTH,
                None,
            ));
            if self.shape == RadialShape::Semi {
                for edge in [from, to] {
                    dots.push((polar(middle, radius, edge), RING_WIDTH / 2.0, track));
                }
            }
            if end > from {
                shapes.push(Shape::line(
                    arc(middle, radius, from, end),
                    color,
                    RING_WIDTH,
                    None,
                ));
                for edge in [from, end] {
                    dots.push((polar(middle, radius, edge), RING_WIDTH / 2.0, color));
                }
            }
        }
        let label = self.center.as_ref().map(|said| {
            let label = centered(said, &theme);
            match self.shape {
                RadialShape::Full => label,
                RadialShape::Semi => label.justify_end().pb(px(SEMI_FOOT)),
            }
        });
        hovered(
            div()
                .id("radial")
                .relative()
                .size_full()
                .child(self.tip.probe())
                .child(
                    plot((ROUND_SIDE, self.extent().1), shapes, dots, (1.0, 1.0))
                        .absolute()
                        .inset_0(),
                )
                .children(label),
            cx,
        )
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum RadarLook {
    Filled,
    Lines,
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum RadarGrid {
    Polygon,
    Circle,
}

pub struct Radar {
    axes: Vec<SharedString>,
    series: Vec<Series>,
    look: RadarLook,
    grid: RadarGrid,
    tips: Vec<Said>,
    tip: Tip,
    born: Option<Instant>,
}

impl Radar {
    pub fn new(
        axes: Vec<SharedString>,
        series: Vec<Series>,
        look: RadarLook,
        grid: RadarGrid,
        tips: Vec<Said>,
    ) -> Self {
        Radar {
            axes,
            series,
            look,
            grid,
            tips,
            tip: Tip::new(PIN_ACTIVE),
            born: None,
        }
    }

    fn middle() -> (f32, f32) {
        (RADAR_VIEW.0 / 2.0, RADAR_VIEW.1 / 2.0)
    }

    fn angle(&self, axis: usize) -> f32 {
        axis as f32 * TAU / self.axes.len().max(1) as f32
    }

    fn peak(&self) -> f32 {
        peak(&levels(&self.series, false))
    }

    fn vertex(&self, axis: usize, value: f32, grow: f32) -> (f32, f32) {
        polar(
            Self::middle(),
            value / self.peak() * RADAR_RADIUS * grow,
            self.angle(axis),
        )
    }

    fn outline(&self, line: &Series, grow: f32) -> Vec<(f32, f32)> {
        (0..self.axes.len())
            .map(|axis| self.vertex(axis, line.values.get(axis).copied().unwrap_or(0.0), grow))
            .collect()
    }
}

impl Chart for Radar {
    fn extent(&self) -> (Option<f32>, f32) {
        (Some(RADAR_VIEW.0), RADAR_VIEW.1)
    }

    fn tip(&self) -> &Tip {
        &self.tip
    }

    fn aim(&self, at: (f32, f32), _: (f32, f32)) -> Option<Aim> {
        let count = self.axes.len();
        let (reach, angle) = bearing(Self::middle(), at);
        if count == 0 || reach > RADAR_RADIUS + RADAR_REACH {
            return None;
        }
        let axis = (angle / (TAU / count as f32)).round() as usize % count;
        let top = self
            .series
            .iter()
            .filter_map(|line| line.values.get(axis))
            .copied()
            .fold(0.0, f32::max);
        Some(Aim {
            at: axis,
            anchor: self.vertex(axis, top, 1.0),
            said: self.tips.get(axis).cloned().unwrap_or_default(),
        })
    }
}

impl Render for Radar {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let entry = Entry::read(&mut self.born, window, cx);
        self.tip.tick(window);
        let grow = entry.wipe().0;
        let middle = Self::middle();
        let hole = theme.color(ColorToken::CardsInnerFill);
        let count = self.axes.len();
        let mut shapes = Vec::new();
        for level in 1..=RADAR_LEVELS {
            let radius = RADAR_RADIUS * level as f32 / RADAR_LEVELS as f32;
            let mut ring = match self.grid {
                RadarGrid::Polygon => (0..count)
                    .map(|axis| polar(middle, radius, self.angle(axis)))
                    .collect(),
                RadarGrid::Circle => arc(middle, radius, 0.0, TAU),
            };
            if let Some(&first) = ring.first() {
                ring.push(first);
            }
            shapes.push(Shape::line(
                ring,
                ink(&theme, RADAR_GRID),
                1.0,
                Some(RADAR_GRID_DASH),
            ));
        }
        for axis in 0..count {
            let on = self.tip.at() == Some(axis);
            let alpha = if on {
                RADAR_SPOKE + (CURSOR_INK - RADAR_SPOKE) * self.tip.shade()
            } else {
                RADAR_SPOKE
            };
            shapes.push(Shape::line(
                vec![middle, polar(middle, RADAR_RADIUS, self.angle(axis))],
                ink(&theme, alpha),
                1.0,
                None,
            ));
        }
        let mut dots = Vec::new();
        for line in &self.series {
            let outline = self.outline(line, grow);
            let stroke = Some((line.color, RADAR_STROKE, None));
            shapes.push(match self.look {
                RadarLook::Filled => {
                    Shape::area(outline.clone(), tint(line.color, RADAR_FILL), stroke)
                }
                RadarLook::Lines => Shape {
                    points: outline.clone(),
                    closed: true,
                    fill: None,
                    stroke,
                },
            });
            if self.look == RadarLook::Lines {
                dots.extend(outline.iter().flat_map(|&spot| {
                    ringed(spot, REST_HOLE, REST_DOT - REST_HOLE, line.color, hole)
                }));
            }
            if let Some(axis) = self.tip.at()
                && let Some(&spot) = outline.get(axis)
            {
                let shade = self.tip.shade();
                dots.extend(ringed(
                    spot,
                    ACTIVE_DOT,
                    ACTIVE_RIM,
                    faded(line.color, shade),
                    faded(hole, shade),
                ));
            }
        }
        let labels = self.axes.iter().enumerate().map(|(axis, name)| {
            let (x, y) = polar(middle, RADAR_RADIUS + RADAR_LABEL, self.angle(axis));
            let alpha = if self.tip.at() == Some(axis) { T1 } else { T3 };
            div()
                .absolute()
                .left(px(x - RADAR_LABEL_BOX.0 / 2.0))
                .top(px(y - RADAR_LABEL_BOX.1 / 2.0))
                .w(px(RADAR_LABEL_BOX.0))
                .h(px(RADAR_LABEL_BOX.1))
                .flex()
                .justify_center()
                .text_size(px(AXIS_TEXT))
                .line_height(px(RADAR_LABEL_BOX.1))
                .text_color(ink(&theme, alpha))
                .child(name.clone())
        });
        hovered(
            div()
                .id("radar")
                .relative()
                .size_full()
                .child(self.tip.probe())
                .child(
                    plot(RADAR_VIEW, shapes, dots, (1.0, 1.0))
                        .absolute()
                        .inset_0(),
                )
                .children(labels),
            cx,
        )
    }
}
