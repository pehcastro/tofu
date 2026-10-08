use std::fs::{File, OpenOptions};
use std::io::{self, BufWriter, Write};
use std::rc::Rc;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use desk_motion::tokens::{EASE_OUT, PANEL_OUT_MS};
use desk_motion::{Glide, GlideKind, reduced_motion};
use gpui::{
    AnyElement, App, Bounds, Corners, Div, ElementId, Entity, FocusHandle, MouseButton, Pixels,
    Point, Rgba, ScrollHandle, SharedString, Stateful, Window, canvas, div, point, prelude::*, px,
    size,
};

use crate::component::icon;
use crate::components::chip::badge;
use crate::components::glyph::Glyph;
use crate::components::overlay::Popover;
use crate::components::paint::{arrowed, glyph, ink, ms, pressed, ring};
use crate::components::size::{
    CAPTION_TEXT, CLOSE_BOX, DIM_TEXT, FONT_BODY, FONT_SMALL, FONT_TAB, HOVER, HTAB_ON, NEW_TAB,
    RADIUS_ROW, RADIUS_TAB, ROW_PAD_X, ROW_PAD_Y, SCREEN_ON, SCREEN_RING, SHELL_TEXT, T1, TAB,
    TAB_GAP, TAB_IN_HEADER, TAB_MAX_WIDTH, TAB_PAD_LEFT, TAB_PAD_LINK, TAB_PAD_TAIL, TAB_SEPARATOR,
};
use crate::components::tooltip::{Edge, tooltip};
use crate::components::width::Width;
use crate::icon::Icon;
use crate::metrics::{HAIRLINE, ICON, ICON_SMALL, ICON_TINY};
use crate::theme::{ColorToken, NumberToken, Theme};

const TRACE_VAR: &str = "DESK_MOTION_TRACE";
const HEADER_GAP: f32 = 4.0;
const NEW_BUTTON_GAP: f32 = 2.0;
const MORE_LIST_MIN: f32 = 180.0;
const DIRTY_DOT: f32 = 7.0;
const TAB_MIN_WIDTH: f32 = 72.0;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TabMark {
    Close,
    Pinned,
    Locked,
    Dirty,
}

#[derive(Clone)]
pub struct Tab {
    pub label: SharedString,
    pub icon: Option<Glyph>,
    pub count: Option<u32>,
    pub mark: TabMark,
    pub flag: Option<TabFlag>,
}

#[derive(Clone)]
pub struct TabFlag {
    pub icon: Icon,
    pub tip: SharedString,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TabEvent {
    Select(usize),
    Close(usize),
    New,
}

type OnTab = Rc<dyn Fn(&TabEvent, &mut Window, &mut App)>;
type Shut = Rc<dyn Fn(usize, &mut Window, &mut App)>;
type Press = Rc<dyn Fn(usize, Point<Pixels>, &mut Window, &mut App)>;
type Pointed = Rc<dyn Fn(usize, &mut Window, &mut App)>;
type Row = (String, &'static str, f32);

#[derive(Clone, PartialEq)]
enum Slot {
    Tab {
        ix: usize,
        label: SharedString,
        close: bool,
    },
    More,
    Button,
    Inert,
}

impl Slot {
    fn holds(&self, tab: usize) -> bool {
        matches!(self, Slot::Tab { ix, .. } if *ix == tab)
    }

    fn label(&self) -> Option<&SharedString> {
        match self {
            Slot::Tab { label, .. } => Some(label),
            Slot::More | Slot::Button | Slot::Inert => None,
        }
    }
}

#[derive(Clone, Copy, PartialEq)]
struct Spot {
    at: usize,
    on_close: bool,
}

#[derive(Clone, Copy)]
struct Fade {
    from: f32,
    to: f32,
    started: Instant,
    span: Duration,
}

impl Fade {
    fn rest(value: f32, now: Instant) -> Self {
        Self {
            from: value,
            to: value,
            started: now,
            span: Duration::ZERO,
        }
    }

    fn value(&self, now: Instant) -> f32 {
        let elapsed = now.saturating_duration_since(self.started);
        if elapsed >= self.span {
            return self.to;
        }
        let done = elapsed.as_secs_f32() / self.span.as_secs_f32();
        self.from + (self.to - self.from) * EASE_OUT(done)
    }

    fn aim(&mut self, to: f32, span: Duration, now: Instant) {
        if self.to != to {
            *self = Self {
                from: self.value(now),
                to,
                started: now,
                span,
            };
        }
    }

    fn moving(&self, now: Instant) -> bool {
        now.saturating_duration_since(self.started) < self.span
    }
}

struct Lit {
    label: SharedString,
    reveal: Fade,
    tint: Fade,
}

#[derive(Clone, Copy, Default)]
struct Look {
    lit: bool,
    reveal: f32,
    tint: f32,
}

#[derive(Clone, Copy)]
struct Slide {
    from: Pixels,
    to: Pixels,
    started: Instant,
    span: Duration,
}

impl Slide {
    fn at(&self, now: Instant) -> Option<Pixels> {
        let elapsed = now.saturating_duration_since(self.started);
        (elapsed < self.span).then(|| {
            let eased = EASE_OUT(elapsed.as_secs_f32() / self.span.as_secs_f32());
            self.from + (self.to - self.from) * eased
        })
    }
}

#[derive(Clone, Copy)]
struct Closing {
    ix: usize,
    width: Pixels,
    started: Instant,
}

impl Closing {
    fn progress(&self, now: Instant) -> f32 {
        let elapsed = now.saturating_duration_since(self.started);
        if elapsed >= PANEL_OUT_MS {
            return 1.0;
        }
        EASE_OUT(elapsed.as_secs_f32() / PANEL_OUT_MS.as_secs_f32())
    }
}

struct Trace {
    out: BufWriter<File>,
    busy: bool,
}

impl Trace {
    fn open() -> Option<Self> {
        let path = std::env::var_os(TRACE_VAR)?;
        match OpenOptions::new().create(true).append(true).open(&path) {
            Ok(file) => Some(Self {
                out: BufWriter::new(file),
                busy: false,
            }),
            Err(error) => {
                eprintln!("{TRACE_VAR}: {error}");
                None
            }
        }
    }

    fn write(&mut self, rows: &[Row]) -> io::Result<()> {
        let ms = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map_or(0.0, |since| since.as_secs_f64() * 1000.0);
        for (element, property, value) in rows {
            writeln!(self.out, "{ms:.3}\t{element}\t{property}\t{value:.3}")?;
        }
        self.out.flush()
    }
}

struct Frame {
    looks: Vec<Look>,
    closing: Option<Closing>,
    finished: bool,
    busy: bool,
}

struct Motion {
    bounds: Vec<Bounds<Pixels>>,
    pointer: Option<Point<Pixels>>,
    marker: Glide,
    marked: Option<(SharedString, Bounds<Pixels>)>,
    closing: Option<Closing>,
    lit: Vec<Lit>,
    trace: Option<Trace>,
    widths: Vec<(SharedString, Pixels)>,
    more: Pixels,
    fixed: Pixels,
    listing: bool,
    list_focus: FocusHandle,
    strip_focus: FocusHandle,
    scroll: ScrollHandle,
    revealed: Option<(usize, Option<Pixels>)>,
    slide: Option<Slide>,
}

fn unlearned_width(tab: &Tab, font: f32, window: &Window) -> Pixels {
    let mut style = window.text_style();
    style.font_weight = gpui::FontWeight::MEDIUM;
    let run = style.to_run(tab.label.len());
    let label = window
        .text_system()
        .layout_line(&tab.label, px(font), &[run], None)
        .width;
    let lead = if tab.icon.is_some() {
        px(ICON_SMALL + TAB_GAP)
    } else {
        px(0.0)
    };
    (label + lead + px(TAB_PAD_LEFT + TAB_PAD_LINK + TAB_PAD_TAIL + CLOSE_BOX))
        .clamp(px(TAB_MIN_WIDTH), px(TAB_MAX_WIDTH))
}

fn close_box(tab: Bounds<Pixels>) -> Bounds<Pixels> {
    let side = px(CLOSE_BOX);
    Bounds::new(
        point(
            tab.right() - px(TAB_PAD_TAIL) - side,
            tab.center().y - side * 0.5,
        ),
        size(side, side),
    )
}

fn settled(bounds: Bounds<Pixels>, now: Instant) -> Glide {
    let mut marker = Glide::new(GlideKind::Eased);
    marker.set_reduced(true);
    marker.retarget(bounds, now);
    marker
}

impl Motion {
    fn new(list_focus: FocusHandle, strip_focus: FocusHandle) -> Self {
        Self {
            listing: false,
            list_focus,
            strip_focus,
            bounds: Vec::new(),
            pointer: None,
            marker: Glide::new(GlideKind::Eased),
            marked: None,
            closing: None,
            lit: Vec::new(),
            trace: Trace::open(),
            widths: Vec::new(),
            more: px(TAB_MAX_WIDTH),
            fixed: px(0.0),
            scroll: ScrollHandle::new(),
            revealed: None,
            slide: None,
        }
    }

    fn floor(&self, more: Option<usize>) -> Pixels {
        more.and_then(|at| self.scroll.bounds_for_item(at + 1))
            .map_or(-self.scroll.max_offset().x, |item| {
                (self.scroll.bounds().left() - item.left()).min(px(0.0))
            })
    }

    fn scroll_for(&self, item: Bounds<Pixels>, more: Option<usize>) -> Pixels {
        let view = self.scroll.bounds();
        let x = self.scroll.offset().x;
        let to = if item.left() + x < view.left() {
            view.left() - item.left()
        } else if item.right() + x > view.right() {
            view.right() - item.right()
        } else {
            x
        };
        to.min(px(0.0)).max(self.floor(more))
    }

    fn reveal(
        &mut self,
        (chosen, more): (Option<usize>, Option<usize>),
        room: Option<Pixels>,
        span: Option<Duration>,
        now: Instant,
    ) {
        let wanted = chosen.map(|at| (at, room));
        if self.revealed == wanted {
            return;
        }
        self.revealed = wanted;
        let Some(at) = chosen else {
            return;
        };
        let Some(item) = self.scroll.bounds_for_item(at + 1) else {
            return self.scroll.scroll_to_item(at + 1);
        };
        let (from, to) = (self.scroll.offset().x, self.scroll_for(item, more));
        self.slide = match span {
            Some(span) if from != to => Some(Slide {
                from,
                to,
                started: now,
                span,
            }),
            _ => {
                self.scroll.set_offset(point(to, self.scroll.offset().y));
                None
            }
        };
    }

    fn slid(&mut self, more: Option<usize>, now: Instant) -> bool {
        let offset = self.scroll.offset();
        let sliding = self.slide.and_then(|slide| slide.at(now));
        let x = match (self.slide, sliding) {
            (_, Some(x)) => x,
            (Some(slide), None) => slide.to,
            (None, None) => offset.x,
        };
        if sliding.is_none() {
            self.slide = None;
        }
        let x = x.max(self.floor(more));
        if x != offset.x {
            self.scroll.set_offset(point(x, offset.y));
        }
        sliding.is_some()
    }

    fn learn(&mut self, items: &[Bounds<Pixels>], slots: &[Slot], flow: usize, gap: f32) {
        let closing = self.closing.map(|closing| closing.ix);
        let mut fixed = px(0.0);
        for (bounds, slot) in items.iter().zip(slots) {
            let width = bounds.size.width + px(gap);
            match slot {
                Slot::Tab { ix, label, .. } if *ix < flow => {
                    if closing == Some(*ix) {
                        continue;
                    }
                    match self.widths.iter_mut().find(|(known, _)| known == label) {
                        Some((_, known)) => *known = width,
                        None => self.widths.push((label.clone(), width)),
                    }
                }
                Slot::More => self.more = width,
                Slot::Tab { .. } | Slot::Button | Slot::Inert => fixed += width,
            }
        }
        self.fixed = fixed;
    }

    fn fitting(
        &self,
        tabs: &[Tab],
        room: Option<Pixels>,
        (font, gap): (f32, f32),
        window: &Window,
    ) -> usize {
        let Some(room) = room.map(|room| room - self.fixed) else {
            return tabs.len();
        };
        let mut used = px(0.0);
        for (at, tab) in tabs.iter().enumerate() {
            used += self
                .widths
                .iter()
                .find(|(known, _)| *known == tab.label)
                .map_or_else(
                    || unlearned_width(tab, font, window) + px(gap),
                    |(_, width)| *width,
                );
            let more = if at + 1 < tabs.len() {
                self.more
            } else {
                px(0.0)
            };
            if used + more > room {
                return at.max(1);
            }
        }
        tabs.len()
    }

    fn spot(&self, slots: &[Slot]) -> Option<Spot> {
        let pointer = self.pointer?;
        self.bounds
            .iter()
            .zip(slots)
            .enumerate()
            .find_map(|(at, (bounds, slot))| match slot {
                Slot::Tab { close, .. } if bounds.contains(&pointer) => Some(Spot {
                    at,
                    on_close: *close && close_box(*bounds).contains(&pointer),
                }),
                _ => None,
            })
    }

    fn point(&mut self, pointer: Option<Point<Pixels>>, slots: &[Slot]) -> bool {
        let before = self.spot(slots);
        self.pointer = pointer;
        before != self.spot(slots)
    }

    fn pointed(&self, slots: &[Slot], flow: usize) -> Option<usize> {
        let pointer = self.pointer?;
        let mut end = None;
        for (bounds, slot) in self.bounds.iter().zip(slots) {
            let lead = match slot {
                Slot::Tab { ix, .. } if *ix < flow => *ix,
                Slot::Tab { .. } | Slot::More | Slot::Button | Slot::Inert => continue,
            };
            if bounds.contains(&pointer) {
                return Some(lead);
            }
            end = Some(bounds.right());
        }
        end.is_none_or(|end| pointer.x > end).then_some(flow)
    }

    fn measure(
        &mut self,
        items: Vec<Bounds<Pixels>>,
        slots: &[Slot],
        chosen: Option<usize>,
        reduced: bool,
        mouse: Point<Pixels>,
        now: Instant,
    ) -> bool {
        let before = self.spot(slots);
        self.pointer = self.pointer.map(|_| mouse);
        self.bounds = items;
        let target =
            chosen.and_then(|at| Some((slots.get(at)?.label()?.clone(), *self.bounds.get(at)?)));
        match (&target, &self.marked) {
            (None, _) => self.marker = Glide::new(GlideKind::Eased),
            (Some(want), Some(had)) if want == had => {}
            (Some((label, bounds)), Some((was, _))) if label == was => {
                self.marker = settled(*bounds, now)
            }
            (Some((_, bounds)), Some(_)) => {
                self.marker.set_reduced(reduced);
                self.marker.retarget(*bounds, now);
            }
            (Some((_, bounds)), None) => self.marker = settled(*bounds, now),
        }
        self.marked = target;
        before != self.spot(slots)
    }

    fn frame(
        &mut self,
        id: &str,
        slots: &[Slot],
        chosen: Option<usize>,
        (enter, leave): (Duration, Duration),
        now: Instant,
    ) -> Frame {
        let closing = self.closing;
        let finished = closing.is_some_and(|closing| closing.progress(now) >= 1.0);
        if finished {
            self.closing = None;
        }
        let spot = self.spot(slots);
        self.lit
            .retain(|lit| slots.iter().any(|slot| slot.label() == Some(&lit.label)));
        let count = slots
            .iter()
            .filter_map(|slot| match slot {
                Slot::Tab { ix, .. } => Some(ix.saturating_add(1)),
                Slot::More | Slot::Button | Slot::Inert => None,
            })
            .max()
            .unwrap_or(0);
        let mut looks = vec![Look::default(); count];
        let mut moving = false;
        for (at, slot) in slots.iter().enumerate() {
            let Slot::Tab { ix, label, .. } = slot else {
                continue;
            };
            let hovered = spot.is_some_and(|spot| spot.at == at);
            let reveal = if hovered || chosen == Some(at) {
                1.0
            } else {
                0.0
            };
            let tint = if spot == Some(Spot { at, on_close: true }) {
                1.0
            } else {
                0.0
            };
            if !self.lit.iter().any(|lit| lit.label == *label) {
                self.lit.push(Lit {
                    label: label.clone(),
                    reveal: Fade::rest(reveal, now),
                    tint: Fade::rest(tint, now),
                });
            }
            let Some(lit) = self.lit.iter_mut().find(|lit| lit.label == *label) else {
                continue;
            };
            lit.reveal
                .aim(reveal, if reveal > 0.0 { enter } else { leave }, now);
            lit.tint
                .aim(tint, if tint > 0.0 { Duration::ZERO } else { leave }, now);
            moving |= lit.reveal.moving(now) || lit.tint.moving(now);
            if let Some(look) = looks.get_mut(*ix) {
                *look = Look {
                    lit: hovered,
                    reveal: lit.reveal.value(now),
                    tint: lit.tint.value(now),
                };
            }
        }
        let busy = moving || (closing.is_some() && !finished);
        if self.trace.as_ref().is_some_and(|trace| busy || trace.busy) {
            let rows = self.rows(id, slots, closing, now);
            self.record(&rows);
        }
        if let Some(trace) = self.trace.as_mut() {
            trace.busy = busy;
        }
        Frame {
            looks,
            closing,
            finished,
            busy,
        }
    }

    fn rows(&self, id: &str, slots: &[Slot], closing: Option<Closing>, now: Instant) -> Vec<Row> {
        let mut rows = Vec::new();
        for slot in slots {
            let Slot::Tab {
                ix, label, close, ..
            } = slot
            else {
                continue;
            };
            let element = format!("{id}/{label}");
            if let Some(closing) = closing.filter(|closing| closing.ix == *ix) {
                let left = 1.0 - closing.progress(now);
                rows.push((element.clone(), "width", closing.width.as_f32() * left));
                rows.push((element.clone(), "opacity", left));
            }
            let lit = self.lit.iter().find(|lit| lit.label == *label);
            if let Some(lit) = lit.filter(|_| *close) {
                rows.push((element.clone(), "close_opacity", lit.reveal.value(now)));
                rows.push((element, "close_tint", lit.tint.value(now)));
            }
        }
        rows
    }

    fn record(&mut self, rows: &[Row]) {
        let Some(trace) = self.trace.as_mut() else {
            return;
        };
        if let Err(error) = trace.write(rows) {
            eprintln!("{TRACE_VAR}: {error}");
            self.trace = None;
        }
    }

    fn paint_marker(&mut self, id: &str, now: Instant) -> (Option<Bounds<Pixels>>, bool) {
        let was = self.marker.moving();
        let marker = self.marker.sample(now).map(|(bounds, _)| bounds);
        let moving = self.marker.moving();
        let shifting = was || moving || self.closing.is_some();
        if let Some(bounds) = marker.filter(|_| self.trace.is_some() && shifting) {
            let element = format!("{id}/marker");
            self.record(&[
                (element.clone(), "x", bounds.origin.x.as_f32()),
                (element, "width", bounds.size.width.as_f32()),
            ]);
        }
        (marker, moving)
    }
}

#[derive(Clone, Copy)]
struct Fold {
    width: Pixels,
    progress: f32,
}

#[derive(Clone, Copy)]
struct Pick {
    ix: usize,
    chosen: bool,
    look: Look,
    fold: Option<Fold>,
    font: f32,
    dim: Rgba,
}

fn close(ix: usize, look: Look, theme: &Theme, shut: Shut) -> Stateful<Div> {
    round(("close", ix).into(), Icon::Close, ix, look, theme, shut)
}

fn round(
    id: ElementId,
    mark: Icon,
    ix: usize,
    look: Look,
    theme: &Theme,
    shut: Shut,
) -> Stateful<Div> {
    let faint = ink(theme, HOVER * look.tint);
    div()
        .id(id)
        .aria_label("Close tab")
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(CLOSE_BOX))
        .rounded_full()
        .opacity(look.reveal)
        .when(look.tint > 0.0, |close| close.bg(faint))
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .on_click(move |_, window, cx| {
            cx.stop_propagation();
            shut(ix, window, cx)
        })
        .child(icon(mark, ICON_TINY, ink(theme, CAPTION_TEXT)))
}

pub(crate) fn tile_control(
    id: SharedString,
    (mark, label): (Icon, &'static str),
    shown: bool,
    theme: &Theme,
    (window, cx): (&mut Window, &mut App),
    shut: impl Fn(&mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let now = Instant::now();
    let target = if shown { 1.0 } else { 0.0 };
    let fade = window.use_keyed_state(id.clone(), cx, |_, _| Fade::rest(target, now));
    let span = ms(
        theme,
        if shown {
            NumberToken::MotionBase
        } else {
            NumberToken::MotionFast
        },
    );
    let (reveal, moving) = fade.update(cx, |fade, _| {
        fade.aim(target, span, now);
        (fade.value(now), fade.moving(now))
    });
    if moving {
        window.request_animation_frame();
    }
    let look = Look {
        lit: false,
        reveal,
        tint: 0.0,
    };
    let hover = theme.color(ColorToken::StateHover);
    pressed(
        round(
            SharedString::from(format!("{id}-button")).into(),
            mark,
            0,
            look,
            theme,
            Rc::new(move |_, window, cx| shut(window, cx)),
        )
        .aria_label(label)
        .cursor_pointer()
        .hover(move |style| style.bg(hover)),
        theme.color(ColorToken::CardsInnerFill),
    )
    .when(reveal <= 0.0, |close| close.invisible())
}

fn mark(tab: &Tab, ix: usize, look: Look, theme: &Theme, shut: &Shut) -> Div {
    let slot = div()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(CLOSE_BOX));
    match tab.mark {
        TabMark::Close => slot.child(close(ix, look, theme, shut.clone())),
        TabMark::Pinned => slot.child(glyph(Glyph::Pin, ICON_TINY, ink(theme, CAPTION_TEXT))),
        TabMark::Locked => slot.child(glyph(Glyph::Lock, ICON_TINY, ink(theme, CAPTION_TEXT))),
        TabMark::Dirty => slot.child(div().size(px(DIRTY_DOT)).rounded_full().bg(ink(theme, T1))),
    }
}

fn link(tab: &Tab, font: f32, theme: &Theme, color: Rgba) -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(TAB_GAP))
        .min_w_0()
        .pl(px(TAB_PAD_LEFT))
        .pr(px(TAB_PAD_LINK))
        .text_size(px(font))
        .font_weight(gpui::FontWeight::MEDIUM)
        .children(
            tab.flag
                .as_ref()
                .map(|flag| icon(flag.icon, ICON_SMALL, color)),
        )
        .children(tab.icon.map(|lead| glyph(lead, ICON_SMALL, color)))
        .child(div().min_w_0().truncate().child(tab.label.clone()))
        .children(tab.count.map(|count| badge(count.to_string(), theme)))
}

fn tab_frame(
    id: &SharedString,
    tab: &Tab,
    pick: Pick,
    theme: &Theme,
    (on, shut, press, menu): (&OnTab, &Shut, Option<&Press>, Option<&Press>),
) -> Stateful<Div> {
    let Pick {
        ix,
        chosen,
        look,
        fold,
        font,
        dim,
    } = pick;
    let select = on.clone();
    let text = if chosen || look.lit {
        theme.color(ColorToken::TextStrong)
    } else {
        dim
    };
    let content = div()
        .flex()
        .items_center()
        .min_w_0()
        .pr(px(TAB_PAD_TAIL))
        .child(link(tab, font, theme, text))
        .child(mark(tab, ix, look, theme, shut))
        .when_some(fold, |content, fold| content.w(fold.width).flex_none());
    let (press, menu) = (press.cloned(), menu.cloned());
    div()
        .id((id.clone(), ix))
        .flex()
        .items_center()
        .min_w(px(TAB_MIN_WIDTH))
        .max_w(px(TAB_MAX_WIDTH))
        .when_some(press, |tab, press| {
            tab.on_mouse_down(MouseButton::Left, move |event, window, cx| {
                press(ix, event.position, window, cx)
            })
        })
        .when_some(menu, |tab, menu| {
            tab.on_mouse_down(MouseButton::Right, move |event, window, cx| {
                cx.stop_propagation();
                menu(ix, event.position, window, cx)
            })
        })
        .cursor_pointer()
        .text_color(text)
        .when_some(fold, |tab, Fold { width, progress }| {
            let left = 1.0 - progress;
            tab.w(width * left)
                .min_w_0()
                .overflow_hidden()
                .opacity(left)
        })
        .on_click(move |_, window, cx| select(&TabEvent::Select(ix), window, cx))
        .child(content)
}

fn flagged(
    item: Stateful<Div>,
    (id, ix, tab): (&SharedString, usize, &Tab),
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> Stateful<Div> {
    match &tab.flag {
        Some(flag) => tooltip(
            (SharedString::from(format!("{id}-flag")), ix),
            item,
            Edge::Frame,
            flag.tip.clone(),
            theme,
            window,
            cx,
        ),
        None => item,
    }
}

fn more_tab(id: &SharedString, hidden: usize, theme: &Theme) -> Stateful<Div> {
    div()
        .id((id.clone(), usize::MAX - 1))
        .aria_label("More tabs")
        .flex()
        .flex_none()
        .items_center()
        .px(px(TAB_PAD_LEFT))
        .cursor_pointer()
        .font_weight(gpui::FontWeight::MEDIUM)
        .text_color(ink(theme, DIM_TEXT))
        .child(format!("{hidden} more"))
}

fn shut_list(state: &Entity<Motion>, cx: &mut App) {
    state.update(cx, |motion, cx| {
        if motion.listing {
            motion.listing = false;
            cx.notify();
        }
    });
}

fn hidden_row(
    id: &SharedString,
    ix: usize,
    tab: &Tab,
    theme: &Theme,
    (state, on): (&Entity<Motion>, &OnTab),
) -> Stateful<Div> {
    let hover = ink(theme, HOVER);
    let (state, on) = (state.clone(), on.clone());
    div()
        .id(ElementId::Name(format!("{id}-hidden-{ix}").into()))
        .flex()
        .items_center()
        .gap(px(TAB_GAP))
        .px(px(ROW_PAD_X))
        .py(px(ROW_PAD_Y))
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .hover(move |row| row.bg(hover))
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, T1))
        .on_click(move |_, window, cx| {
            cx.stop_propagation();
            shut_list(&state, cx);
            on(&TabEvent::Select(ix), window, cx)
        })
        .children(
            tab.icon
                .map(|lead| glyph(lead, ICON_SMALL, ink(theme, DIM_TEXT))),
        )
        .child(div().min_w_0().truncate().child(tab.label.clone()))
}

fn more_list(
    id: &SharedString,
    trigger: Stateful<Div>,
    hidden: &[(usize, &Tab)],
    (theme, backdrop): (&Theme, Rgba),
    (state, on): (&Entity<Motion>, &OnTab),
    cx: &App,
) -> AnyElement {
    let Motion {
        listing,
        list_focus,
        ..
    } = state.read(cx);
    let (listing, list_focus) = (*listing, list_focus.clone());
    let (toggler, focus) = (state.clone(), list_focus.clone());
    let trigger = pressed(
        trigger.on_click(move |_, window, cx| {
            toggler.update(cx, |motion, cx| {
                motion.listing = !listing;
                cx.notify();
            });
            if !listing {
                window.focus(&focus, cx);
            }
        }),
        backdrop,
    );
    let (outside, escaper) = (state.clone(), state.clone());
    Popover::new(ElementId::Name(format!("{id}-more-list").into()), trigger)
        .open(listing)
        .fit(MORE_LIST_MIN)
        .child(
            div()
                .flex()
                .flex_col()
                .track_focus(&list_focus)
                .on_key_down(move |event, _, cx| {
                    if event.keystroke.key == "escape" {
                        cx.stop_propagation();
                        shut_list(&escaper, cx);
                    }
                })
                .on_mouse_down_out(move |_, _, cx| shut_list(&outside, cx))
                .children(
                    hidden
                        .iter()
                        .map(|(ix, tab)| hidden_row(id, *ix, tab, theme, (state, on))),
                ),
        )
        .into_any_element()
}

fn shown(count: usize, fit: usize, active: usize) -> Vec<usize> {
    let mut shown: Vec<usize> = (0..count.min(fit)).collect();
    if (shown.len()..count).contains(&active)
        && let Some(last) = shown.last_mut()
    {
        *last = active;
    }
    shown
}

pub fn connected_tabs(
    id: impl Into<ElementId>,
    tabs: &[Tab],
    active: usize,
    room: usize,
    theme: &Theme,
    on: impl Fn(&TabEvent, &mut Window, &mut App) + 'static,
) -> TabStrip {
    TabStrip::connected(id, tabs, active, room, theme, on)
}

fn header_radius(screen: bool) -> f32 {
    if screen { RADIUS_TAB } else { RADIUS_ROW }
}

fn header_fill(screen: bool, theme: &Theme) -> Rgba {
    ink(theme, if screen { SCREEN_ON } else { HTAB_ON })
}

fn new_tab(id: &SharedString, theme: &Theme, on: &OnTab) -> Stateful<Div> {
    let new = on.clone();
    new_tab_glyph(id.clone(), theme).on_click(move |_, window, cx| new(&TabEvent::New, window, cx))
}

pub fn new_tab_glyph(id: impl Into<SharedString>, theme: &Theme) -> Stateful<Div> {
    let glyph = div()
        .id((id.into(), usize::MAX))
        .aria_label("New workspace")
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(NEW_TAB))
        .rounded(px(RADIUS_TAB))
        .cursor_pointer()
        .child(icon(Icon::Plus, ICON, ink(theme, SHELL_TEXT)));
    pressed(glyph, theme.color(ColorToken::CardsOuterFill))
}

fn separator(theme: &Theme) -> Div {
    div()
        .mx_1()
        .w(px(HAIRLINE))
        .h(px(TAB_SEPARATOR))
        .bg(ink(theme, SCREEN_RING))
}

pub fn header_tabs(
    id: impl Into<ElementId>,
    tabs: &[Tab],
    active: usize,
    screens: &[Tab],
    theme: &Theme,
    on: impl Fn(&TabEvent, &mut Window, &mut App) + 'static,
) -> TabStrip {
    TabStrip::header(id, tabs, active, screens, theme, on)
}

enum Shape {
    Connected { room: usize },
    Header { screens: Vec<Tab> },
}

#[derive(IntoElement)]
pub struct TabStrip {
    id: SharedString,
    shape: Shape,
    tabs: Vec<Tab>,
    active: usize,
    theme: Theme,
    on: OnTab,
    press: Option<Press>,
    menu: Option<Press>,
    pointed: Option<Pointed>,
    focus: Option<FocusHandle>,
    new_button: Option<AnyElement>,
}

impl TabStrip {
    pub fn connected(
        id: impl Into<ElementId>,
        tabs: &[Tab],
        active: usize,
        room: usize,
        theme: &Theme,
        on: impl Fn(&TabEvent, &mut Window, &mut App) + 'static,
    ) -> Self {
        Self::shaped(
            id,
            Shape::Connected { room },
            tabs,
            active,
            theme,
            Rc::new(on),
        )
    }

    pub fn header(
        id: impl Into<ElementId>,
        tabs: &[Tab],
        active: usize,
        screens: &[Tab],
        theme: &Theme,
        on: impl Fn(&TabEvent, &mut Window, &mut App) + 'static,
    ) -> Self {
        let screens = screens.to_vec();
        Self::shaped(
            id,
            Shape::Header { screens },
            tabs,
            active,
            theme,
            Rc::new(on),
        )
    }

    fn shaped(
        id: impl Into<ElementId>,
        shape: Shape,
        tabs: &[Tab],
        active: usize,
        theme: &Theme,
        on: OnTab,
    ) -> Self {
        Self {
            id: id.into().to_string().into(),
            shape,
            tabs: tabs.to_vec(),
            active,
            theme: theme.clone(),
            on,
            press: None,
            menu: None,
            pointed: None,
            focus: None,
            new_button: None,
        }
    }

    pub fn new_button(mut self, button: impl IntoElement) -> Self {
        self.new_button = Some(button.into_any_element());
        self
    }

    pub fn focus(mut self, focus: &FocusHandle) -> Self {
        self.focus = Some(focus.clone().tab_stop(true));
        self
    }

    pub fn on_press(
        mut self,
        press: impl Fn(usize, Point<Pixels>, &mut Window, &mut App) + 'static,
    ) -> Self {
        self.press = Some(Rc::new(press));
        self
    }

    pub fn on_menu(
        mut self,
        menu: impl Fn(usize, Point<Pixels>, &mut Window, &mut App) + 'static,
    ) -> Self {
        self.menu = Some(Rc::new(menu));
        self
    }

    pub fn on_point(mut self, pointed: impl Fn(usize, &mut Window, &mut App) + 'static) -> Self {
        self.pointed = Some(Rc::new(pointed));
        self
    }
}

fn shutter(state: &Entity<Motion>, slots: &Rc<Vec<Slot>>, on: &OnTab, reduced: bool) -> Shut {
    let (state, slots, on) = (state.clone(), slots.clone(), on.clone());
    Rc::new(move |ix, window, cx| {
        let (pending, width) = {
            let motion = state.read(cx);
            let width = slots
                .iter()
                .position(|slot| slot.holds(ix))
                .and_then(|at| motion.bounds.get(at))
                .map(|bounds| bounds.size.width);
            (motion.closing, width)
        };
        if pending.is_some_and(|closing| closing.ix == ix) {
            return;
        }
        let Some(width) = width.filter(|_| !reduced) else {
            return on(&TabEvent::Close(ix), window, cx);
        };
        let ix = match pending {
            Some(closing) => {
                on(&TabEvent::Close(closing.ix), window, cx);
                if ix > closing.ix {
                    ix.saturating_sub(1)
                } else {
                    ix
                }
            }
            None => ix,
        };
        state.update(cx, |motion, cx| {
            motion.closing = Some(Closing {
                ix,
                width,
                started: Instant::now(),
            });
            cx.notify();
        });
    })
}

impl RenderOnce for TabStrip {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let Self {
            id,
            shape,
            tabs,
            active,
            theme,
            on,
            press,
            menu,
            pointed,
            focus,
            new_button,
        } = self;
        let reduced = reduced_motion(cx);
        let state =
            window.use_keyed_state(SharedString::from(format!("{id}-motion")), cx, |_, cx| {
                Motion::new(cx.focus_handle(), cx.focus_handle().tab_stop(true))
            });
        let focus = focus.unwrap_or_else(|| state.read(cx).strip_focus.clone());
        let ringed = focus.is_focused(window) && window.last_input_was_keyboard();
        let focus_ring = theme.color(ColorToken::FocusRing);
        let count = tabs.len()
            + match &shape {
                Shape::Connected { .. } => 0,
                Shape::Header { screens } => screens.len(),
            };
        let stepper = on.clone();
        let slot = |ix: usize, tab: &Tab| Slot::Tab {
            ix,
            label: tab.label.clone(),
            close: tab.mark == TabMark::Close,
        };
        let width = Width::of(format!("{id}-width"), window, cx);
        let (cap, gap, font) = match &shape {
            Shape::Connected { room } => (tabs.len().min(*room), 0.0, FONT_TAB),
            Shape::Header { .. } => (tabs.len(), HEADER_GAP, FONT_BODY),
        };
        let fit = state.read(cx).fitting(
            tabs.get(..cap).unwrap_or_default(),
            width.get(cx),
            (font, gap),
            window,
        );
        let visible = shown(tabs.len(), cap.min(fit), active);
        let hidden = tabs.len().saturating_sub(visible.len());
        let flow = visible
            .iter()
            .filter_map(|ix| Some(slot(*ix, tabs.get(*ix)?)))
            .chain((hidden > 0).then_some(Slot::More));
        let slots: Vec<Slot> = match &shape {
            Shape::Connected { .. } => flow
                .chain(new_button.is_some().then_some(Slot::Button))
                .collect(),
            Shape::Header { screens } => flow
                .chain([Slot::Button, Slot::Inert])
                .chain(
                    screens
                        .iter()
                        .enumerate()
                        .map(|(ix, tab)| slot(tabs.len() + ix, tab)),
                )
                .collect(),
        };
        let slots = Rc::new(slots);
        let chosen = slots.iter().position(|slot| slot.holds(active));
        let room = width.get(cx);
        let more = slots.iter().position(|slot| matches!(slot, Slot::More));
        let spans = (
            ms(&theme, NumberToken::MotionBase),
            ms(&theme, NumberToken::MotionFast),
        );
        let now = Instant::now();
        let (scroll, sliding) = state.update(cx, |motion, _| {
            let span = ms(&theme, NumberToken::MotionTile);
            motion.reveal((chosen, more), room, (!reduced).then_some(span), now);
            (motion.scroll.clone(), motion.slid(more, now))
        });
        if sliding {
            window.request_animation_frame();
        }
        let Frame {
            looks,
            closing,
            finished,
            busy,
        } = state.update(cx, |motion, _| {
            motion.frame(&id, &slots, chosen, spans, now)
        });
        if let Some(ix) = closing.filter(|_| finished).map(|closing| closing.ix) {
            let on = on.clone();
            window.defer(cx, move |window, cx| on(&TabEvent::Close(ix), window, cx));
        }
        if busy {
            window.request_animation_frame();
        }
        let shut = shutter(&state, &slots, &on, reduced);
        let pick = |ix: usize, font: f32, dim: Rgba| Pick {
            ix,
            font,
            dim,
            chosen: ix == active,
            look: looks.get(ix).copied().unwrap_or_default(),
            fold: closing
                .filter(|closing| closing.ix == ix)
                .map(|closing| Fold {
                    width: closing.width,
                    progress: closing.progress(now),
                }),
        };
        let backdrop = theme.color(ColorToken::CardsOuterFill);
        let unseen: Vec<(usize, &Tab)> = tabs
            .iter()
            .enumerate()
            .filter(|(ix, _)| !visible.contains(ix))
            .collect();
        if unseen.is_empty() {
            shut_list(&state, cx);
        }
        let overflow = |trigger: Stateful<Div>, cx: &App| {
            more_list(&id, trigger, &unseen, (&theme, backdrop), (&state, &on), cx)
        };
        let (frame, children, marker_fill, corners) = match shape {
            Shape::Connected { .. } => {
                let top = Corners {
                    top_left: px(RADIUS_TAB),
                    top_right: px(RADIUS_TAB),
                    ..Corners::default()
                };
                let more = (hidden > 0).then(|| {
                    overflow(
                        more_tab(&id, hidden, &theme)
                            .h_full()
                            .rounded_t(px(RADIUS_TAB))
                            .text_size(px(FONT_TAB)),
                        cx,
                    )
                });
                let mut children = Vec::new();
                for (ix, tab) in visible.iter().filter_map(|ix| Some((*ix, tabs.get(*ix)?))) {
                    let item = tab_frame(
                        &id,
                        tab,
                        pick(ix, FONT_TAB, ink(&theme, DIM_TEXT)),
                        &theme,
                        (&on, &shut, press.as_ref(), menu.as_ref()),
                    )
                    .h_full()
                    .rounded_t(px(RADIUS_TAB))
                    .when(ringed && ix == active, |tab| {
                        tab.shadow(vec![ring(focus_ring)])
                    });
                    let item = flagged(pressed(item, backdrop), (&id, ix, tab), &theme, window, cx);
                    children.push(item.into_any_element());
                }
                children.extend(more.into_iter().chain(new_button.map(|button| {
                    div()
                        .flex()
                        .flex_none()
                        .items_center()
                        .pl(px(NEW_BUTTON_GAP))
                        .child(button)
                        .into_any_element()
                })));
                let frame = div().flex().items_stretch().h(px(TAB_IN_HEADER)).min_w_0();
                (frame, children, theme.color(ColorToken::TabsFill), top)
            }
            Shape::Header { screens } => {
                let screen = active >= tabs.len();
                let header = |at: (usize, &Tab, bool), window: &mut Window, cx: &mut App| {
                    let (ix, tab, screen) = at;
                    let item = tab_frame(
                        &id,
                        tab,
                        pick(ix, FONT_BODY, ink(&theme, SHELL_TEXT)),
                        &theme,
                        (&on, &shut, press.as_ref(), menu.as_ref()),
                    )
                    .h(px(if screen { NEW_TAB } else { TAB }))
                    .rounded(px(header_radius(screen)))
                    .when(screen, |tab| {
                        tab.shadow(vec![ring(ink(&theme, SCREEN_RING))])
                    })
                    .when(ringed && ix == active, |tab| {
                        tab.shadow(vec![ring(focus_ring)])
                    });
                    let item = pressed(item, backdrop).occlude();
                    flagged(item, (&id, ix, tab), &theme, window, cx).into_any_element()
                };
                let more = (hidden > 0).then(|| {
                    overflow(
                        more_tab(&id, hidden, &theme)
                            .occlude()
                            .h(px(TAB))
                            .rounded(px(RADIUS_ROW))
                            .text_size(px(FONT_BODY)),
                        cx,
                    )
                });
                let mut children: Vec<AnyElement> = visible
                    .iter()
                    .filter_map(|ix| Some((*ix, tabs.get(*ix)?)))
                    .map(|(ix, tab)| header((ix, tab, false), window, cx))
                    .collect();
                let new_button =
                    new_button.unwrap_or_else(|| new_tab(&id, &theme, &on).into_any_element());
                children.extend(more);
                children.push(
                    div()
                        .flex()
                        .flex_none()
                        .occlude()
                        .child(new_button)
                        .into_any_element(),
                );
                children.push(
                    separator(&theme)
                        .when(screens.is_empty(), |line| line.invisible())
                        .into_any_element(),
                );
                for (ix, tab) in screens.iter().enumerate() {
                    children.push(header((tabs.len() + ix, tab, true), window, cx));
                }
                let frame = div().flex().items_center().gap(px(HEADER_GAP)).min_w_0();
                let corners = Corners::all(px(header_radius(screen)));
                (frame, children, header_fill(screen, &theme), corners)
            }
        };
        let (painter, marker_id) = (state.clone(), id.clone());
        let bars = canvas(
            move |bounds, _, cx| width.record(bounds.size.width, cx),
            move |_, (), window, cx| {
                let (marker, moving) = painter.update(cx, |motion, _| {
                    motion.paint_marker(&marker_id, Instant::now())
                });
                if let Some(bounds) = marker {
                    window.paint_quad(gpui::fill(bounds, marker_fill).corner_radii(corners));
                }
                if moving {
                    window.request_animation_frame();
                }
            },
        )
        .absolute()
        .top_0()
        .left_0()
        .size_full();
        let (measured, mover, leaver) = (state.clone(), state.clone(), state);
        let (seen, felt, left) = (slots.clone(), slots.clone(), slots);
        let flow = tabs.len();
        let shifted = scroll.clone();
        frame
            .relative()
            .w_full()
            .on_children_prepainted(move |bounds, window, cx| {
                let shift = shifted.offset();
                let items: Vec<Bounds<Pixels>> = bounds
                    .get(1..)
                    .unwrap_or_default()
                    .iter()
                    .map(|item| Bounds::new(item.origin + shift, item.size))
                    .collect();
                let mouse = window.mouse_position();
                let moved = measured.update(cx, |motion, _| {
                    motion.learn(&items, &seen, flow, gap);
                    motion.measure(items, &seen, chosen, reduced, mouse, Instant::now())
                });
                if moved {
                    window.request_animation_frame();
                }
            })
            .id(id)
            .overflow_hidden()
            .overflow_x_scroll()
            .track_scroll(&scroll)
            .track_focus(&focus)
            .on_key_down(move |event, window, cx| {
                if let Some(to) = arrowed(event, active, count) {
                    cx.stop_propagation();
                    if to != active {
                        stepper(&TabEvent::Select(to), window, cx);
                    }
                }
            })
            .on_mouse_move(move |event, window, cx| {
                mover.update(cx, |motion, cx| {
                    if motion.point(Some(event.position), &felt) {
                        cx.notify();
                    }
                });
                let at = mover.read(cx).pointed(&felt, flow);
                if let (Some(pointed), Some(at)) = (&pointed, at) {
                    pointed(at, window, cx);
                }
            })
            .on_hover(move |inside, _, cx| {
                if !*inside {
                    leaver.update(cx, |motion, cx| {
                        if motion.point(None, &left) {
                            cx.notify();
                        }
                    });
                }
            })
            .child(bars)
            .children(children)
    }
}
