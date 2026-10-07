use std::collections::{HashMap, VecDeque};
use std::panic::Location;
use std::time::{Duration, Instant};

use desk_motion::tokens::EASE_OUT;
use desk_motion::{Glide, GlideKind};
use gpui::{
    AnyElement, App, AppContext, AvailableSpace, Bounds, BoxShadow, Context, DispatchPhase, Div,
    Element, ElementId, Entity, EntityId, FontWeight, Global, GlobalElementId, InspectorElementId,
    LayoutId, MouseMoveEvent, PathBuilder, Pixels, Point, Position, Rgba, SharedString, Stateful,
    StrikethroughStyle, Style, Task, UnderlineStyle, Window, WindowId, canvas, deferred, div,
    point, prelude::*, px, size,
};

use crate::components::paint::{drop, ink, tint};
use crate::components::size::{FONT_BODY, GLASS_BLUR, LINE_BODY, T1};
use crate::theme::{ColorToken, NumberToken, Theme};

const TIP_OPEN: Duration = Duration::from_millis(300);
const TIP_CLOSE: Duration = Duration::from_millis(100);
const WARM: Duration = Duration::from_millis(400);
const CARD_OPEN: Duration = Duration::from_millis(500);
const FADE_IN: Duration = Duration::from_millis(100);
const FADE_OUT: Duration = Duration::from_millis(75);
const ARROW_CLEARANCE: f32 = 8.0;
const EDGE: f32 = 8.0;
const ARROW_WIDTH: f32 = 10.0;
const ARROW_HEIGHT: f32 = 5.0;
const ARROW_INSET: f32 = 6.0;
const RIM_WIDTH: f32 = 1.0;
const HULL_PAD: f32 = 5.0;
const RADIUS: f32 = 8.0;
const TIP_PAD_X: f32 = 8.0;
const TIP_PAD_Y: f32 = 4.0;
const TIP_TEXT: f32 = 12.0;
const TIP_LINE: f32 = 16.0;
const CARD_PAD: f32 = 12.0;
const CARD_WIDTH: f32 = 260.0;
const FROST_FILL: f32 = 0.92;
const RIM: f32 = 0.5;
const SHADOW_INK: f32 = 0.1;
const SHADOW: [(f32, f32, f32); 2] = [(20.0, 25.0, -5.0), (8.0, 10.0, -6.0)];
const LOG_LENGTH: usize = 6;
const CARD_BODY: &str = "desk-hover-card";

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Edge {
    Frame,
    Text,
}

impl Edge {
    fn painted(self, line: Bounds<Pixels>, window: &Window) -> Bounds<Pixels> {
        match self {
            Edge::Frame => line,
            Edge::Text => {
                let style = window.text_style();
                let rem = window.rem_size();
                let text = window.text_system();
                let font = text.resolve_font(&style.font());
                let size = style.font_size.to_pixels(rem);
                let ascent = text.ascent(font, size);
                let glyphs = ascent + text.descent(font, size).abs();
                let leading = (style.line_height_in_pixels(rem) - glyphs) / 2.0 + ascent
                    - text.cap_height(font, size);
                let top = (line.top() + leading.max(px(0.0))).min(line.bottom());
                Bounds::from_corners(point(line.left(), top), line.bottom_right())
            }
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Kind {
    Tip,
    Card,
}

impl Kind {
    fn open(self) -> Duration {
        match self {
            Kind::Tip => TIP_OPEN,
            Kind::Card => CARD_OPEN,
        }
    }
}

#[derive(Clone)]
enum Event {
    Enter(ElementId),
    Leave(ElementId),
    Open(ElementId),
    Retarget(ElementId),
    Close,
}

struct Fade {
    from: f32,
    to: f32,
    at: Instant,
}

impl Fade {
    fn alpha(&self, now: Instant) -> f32 {
        let span = match self.to > self.from {
            true => FADE_IN,
            false => FADE_OUT,
        };
        let done =
            (now.saturating_duration_since(self.at).as_secs_f32() / span.as_secs_f32()).min(1.0);
        self.from + (self.to - self.from) * EASE_OUT(done)
    }

    fn running(&self, now: Instant) -> bool {
        self.alpha(now) != self.to
    }
}

#[derive(Clone, Copy)]
struct Placement {
    bounds: Bounds<Pixels>,
    anchor: Bounds<Pixels>,
    below: bool,
}

impl Placement {
    fn arrow_tip(&self) -> Pixels {
        match self.below {
            true => self.bounds.top() - px(ARROW_HEIGHT),
            false => self.bounds.bottom() + px(ARROW_HEIGHT),
        }
    }
}

struct Floating {
    kind: Kind,
    fade: Fade,
    glide: Glide,
    reduced: bool,
    shown: bool,
    current: Option<ElementId>,
    hovered: Option<ElementId>,
    rects: HashMap<ElementId, Bounds<Pixels>>,
    placed: Option<Placement>,
    hull: Option<Vec<Point<Pixels>>>,
    closed: Option<Instant>,
    hosts: Vec<EntityId>,
    timer: Option<Task<gpui::Result<()>>>,
    log: VecDeque<(Instant, Event)>,
    watcher: Option<EntityId>,
}

impl Floating {
    fn new(kind: Kind) -> Self {
        Self {
            kind,
            fade: Fade {
                from: 0.0,
                to: 0.0,
                at: Instant::now(),
            },
            glide: Glide::new(GlideKind::Spring),
            reduced: false,
            shown: false,
            current: None,
            hovered: None,
            rects: HashMap::new(),
            placed: None,
            hull: None,
            closed: None,
            hosts: Vec::new(),
            timer: None,
            log: VecDeque::new(),
            watcher: None,
        }
    }

    fn record(&mut self, event: Event, cx: &mut Context<Self>) {
        if self.log.len() == LOG_LENGTH {
            self.log.pop_front();
        }
        self.log.push_back((Instant::now(), event));
        self.notify_watcher(cx);
    }

    fn notify_watcher(&self, cx: &mut Context<Self>) {
        if let Some(watcher) = self.watcher {
            AppContext::notify(cx, watcher);
        }
    }

    fn warm(&self, now: Instant) -> bool {
        self.shown
            || self
                .closed
                .is_some_and(|closed| now.saturating_duration_since(closed) < WARM)
    }

    fn enter(&mut self, key: ElementId, view: EntityId, cx: &mut Context<Self>) {
        self.record(Event::Enter(key.clone()), cx);
        self.hovered = Some(key.clone());
        self.hull = None;
        self.timer = None;
        if !self.hosts.contains(&view) {
            self.hosts.push(view);
        }
        if self.warm(Instant::now()) {
            self.show(key, cx);
            return;
        }
        let delay = self.kind.open();
        self.timer = Some(cx.spawn(async move |this, cx| {
            cx.background_executor().timer(delay).await;
            this.update(cx, |floating, cx| {
                if floating.hovered.as_ref() == Some(&key) {
                    floating.show(key, cx);
                }
            })
        }));
    }

    fn hold(&mut self) {
        self.hovered = Some(CARD_BODY.into());
        self.hull = None;
        self.timer = None;
    }

    fn leave(&mut self, key: ElementId, exit: Point<Pixels>, cx: &mut Context<Self>) {
        self.record(Event::Leave(key.clone()), cx);
        if self.hovered.as_ref() != Some(&key) {
            return;
        }
        self.hovered = None;
        self.timer = None;
        if !self.shown {
            return;
        }
        match self.kind {
            Kind::Tip => {
                self.timer = Some(cx.spawn(async move |this, cx| {
                    cx.background_executor().timer(TIP_CLOSE).await;
                    this.update(cx, |floating, cx| {
                        if floating.hovered.is_none() {
                            floating.close(cx);
                        }
                    })
                }));
            }
            Kind::Card => {
                let toward = match key == CARD_BODY.into() {
                    true => self
                        .current
                        .as_ref()
                        .and_then(|current| self.rects.get(current))
                        .copied(),
                    false => self.placed.map(|placed| placed.bounds),
                };
                match toward {
                    Some(rect) => self.hull = Some(hull(exit, rect)),
                    None => self.close(cx),
                }
            }
        }
    }

    fn travel(&mut self, at: Point<Pixels>, cx: &mut Context<Self>) {
        let outside = self
            .hull
            .as_ref()
            .is_some_and(|hull| !inside_hull(hull, at));
        if outside && self.hovered.is_none() {
            self.close(cx);
        }
    }

    fn show(&mut self, key: ElementId, cx: &mut Context<Self>) {
        let now = Instant::now();
        let event = match self.shown {
            true => Event::Retarget(key.clone()),
            false => Event::Open(key.clone()),
        };
        if !self.shown {
            self.fade = Fade {
                from: self.fade.alpha(now),
                to: 1.0,
                at: now,
            };
        }
        if self.current.as_ref() != Some(&key) {
            self.placed = None;
        }
        self.shown = true;
        self.current = Some(key);
        self.record(event, cx);
        self.notify_hosts(cx);
    }

    fn close(&mut self, cx: &mut Context<Self>) {
        let now = Instant::now();
        self.shown = false;
        self.hull = None;
        self.timer = None;
        self.closed = Some(now);
        self.glide.hide(now);
        self.fade = Fade {
            from: self.fade.alpha(now),
            to: 0.0,
            at: now,
        };
        self.record(Event::Close, cx);
        self.notify_hosts(cx);
    }

    fn notify_hosts(&mut self, cx: &mut Context<Self>) {
        for host in self.hosts.clone() {
            AppContext::notify(cx, host);
        }
    }

    fn alpha(&self, now: Instant) -> f32 {
        match self.reduced {
            true => f32::from(u8::from(self.shown)),
            false => self.fade.alpha(now),
        }
    }

    fn fading(&self, now: Instant) -> bool {
        !self.reduced && self.fade.running(now)
    }
}

fn hull(exit: Point<Pixels>, rect: Bounds<Pixels>) -> Vec<Point<Pixels>> {
    let pad = px(HULL_PAD);
    let mut points = [
        point(exit.x - pad, exit.y - pad),
        point(exit.x + pad, exit.y - pad),
        point(exit.x + pad, exit.y + pad),
        point(exit.x - pad, exit.y + pad),
        rect.origin,
        rect.top_right(),
        rect.bottom_right(),
        rect.bottom_left(),
    ];
    points.sort_by(|a, b| {
        a.x.as_f32()
            .total_cmp(&b.x.as_f32())
            .then(a.y.as_f32().total_cmp(&b.y.as_f32()))
    });
    let cross = |o: Point<Pixels>, a: Point<Pixels>, b: Point<Pixels>| {
        (a.x - o.x).as_f32() * (b.y - o.y).as_f32() - (a.y - o.y).as_f32() * (b.x - o.x).as_f32()
    };
    let chain = |ordered: &mut dyn Iterator<Item = Point<Pixels>>| {
        let mut half: Vec<Point<Pixels>> = Vec::new();
        for next in ordered {
            while let [.., a, b] = half[..]
                && cross(a, b, next) <= 0.0
            {
                half.pop();
            }
            half.push(next);
        }
        half.pop();
        half
    };
    let mut lower = chain(&mut points.iter().copied());
    lower.extend(chain(&mut points.iter().rev().copied()));
    lower
}

fn inside_hull(hull: &[Point<Pixels>], at: Point<Pixels>) -> bool {
    hull.iter().zip(hull.iter().cycle().skip(1)).all(|(a, b)| {
        (b.x - a.x).as_f32() * (at.y - a.y).as_f32() - (b.y - a.y).as_f32() * (at.x - a.x).as_f32()
            >= 0.0
    })
}

#[derive(Default)]
struct Floats(HashMap<WindowId, [Entity<Floating>; 2]>);

impl Global for Floats {}

fn floating(kind: Kind, window: &Window, cx: &mut App) -> Entity<Floating> {
    let id = window.window_handle().window_id();
    let found = cx.default_global::<Floats>().0.get(&id).cloned();
    let [tip, card] = found.unwrap_or_else(|| {
        let made = [Kind::Tip, Kind::Card].map(|kind| cx.new(|_| Floating::new(kind)));
        cx.default_global::<Floats>().0.insert(id, made.clone());
        made
    });
    match kind {
        Kind::Tip => tip,
        Kind::Card => card,
    }
}

pub struct TooltipDebug {
    pub events: String,
    pub hovered: Vec<String>,
    pub targets: Vec<(String, Bounds<Pixels>)>,
}

pub fn tooltip_debug(view: EntityId, window: &Window, cx: &mut App) -> TooltipDebug {
    let mut debug = TooltipDebug {
        events: String::new(),
        hovered: Vec::new(),
        targets: Vec::new(),
    };
    for kind in [Kind::Tip, Kind::Card] {
        floating(kind, window, cx).update(cx, |floating, _| {
            floating.watcher = Some(view);
            debug
                .hovered
                .extend(floating.hovered.as_ref().map(ToString::to_string));
            debug.targets.extend(
                floating
                    .rects
                    .iter()
                    .map(|(key, rect)| (key.to_string(), *rect)),
            );
            if kind == Kind::Tip {
                debug.events = event_line(&floating.log);
            }
        });
    }
    debug
}

fn event_line(log: &VecDeque<(Instant, Event)>) -> String {
    let mut last = None;
    let mut line = Vec::new();
    for (at, event) in log {
        let gap = last.map_or(0, |last| at.saturating_duration_since(last).as_millis());
        last = Some(*at);
        let said = match event {
            Event::Enter(key) => format!("enter {key}"),
            Event::Leave(key) => format!("leave {key}"),
            Event::Open(key) => format!("open {key}"),
            Event::Retarget(key) => format!("retarget {key}"),
            Event::Close => "close".to_string(),
        };
        line.push(format!("+{gap}ms {said}"));
    }
    line.join("  ")
}

pub fn overlay_fill(theme: &Theme) -> Rgba {
    let window = theme.color(ColorToken::SurfaceWindow);
    let outer = theme.color(ColorToken::CardsOuterFill);
    let light = |color: Rgba| color.red + color.green + color.blue;
    let darkest = match light(outer) <= light(window) {
        true => outer,
        false => window,
    };
    let frosted = theme.number(NumberToken::ShapeBlur) > 0.0;
    tint(
        darkest,
        match frosted {
            true => FROST_FILL,
            false => 1.0,
        },
    )
}

fn overlay_rim(theme: &Theme) -> Rgba {
    let edge = theme.color(ColorToken::Separator);
    tint(edge, edge.alpha * RIM)
}

pub fn overlay_surface(theme: &Theme) -> Div {
    let shadow = tint(theme.color(ColorToken::Shadow), SHADOW_INK);
    let base = div().bg(overlay_fill(theme));
    let base = match theme.number(NumberToken::ShapeBlur) > 0.0 {
        true => base.backdrop_blur(px(GLASS_BLUR)),
        false => base,
    };
    base.rounded(px(RADIUS))
        .border(px(RIM_WIDTH))
        .border_color(overlay_rim(theme))
        .text_color(ink(theme, T1))
        .shadow(
            SHADOW
                .iter()
                .map(|&(y, blur, spread)| BoxShadow {
                    spread_radius: px(spread),
                    ..drop(shadow, y, blur)
                })
                .collect(),
        )
}

fn own_text(theme: &Theme) -> Div {
    let mut layer = div()
        .text_color(ink(theme, T1))
        .text_size(px(FONT_BODY))
        .line_height(px(LINE_BODY))
        .font_weight(FontWeight::NORMAL)
        .not_italic();
    let text = layer.text_style();
    text.underline = Some(UnderlineStyle::default());
    text.strikethrough = Some(StrikethroughStyle::default());
    layer
}

pub fn tooltip(
    key: impl Into<ElementId>,
    target: Stateful<Div>,
    edge: Edge,
    text: impl Into<SharedString>,
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> Stateful<Div> {
    let body = overlay_surface(theme)
        .px(px(TIP_PAD_X))
        .py(px(TIP_PAD_Y))
        .text_size(px(TIP_TEXT))
        .line_height(px(TIP_LINE))
        .whitespace_nowrap()
        .child(text.into());
    float(
        Kind::Tip,
        key.into(),
        (target, edge),
        body,
        theme,
        window,
        cx,
    )
}

pub fn hover_card(
    key: impl Into<ElementId>,
    target: Stateful<Div>,
    edge: Edge,
    card: impl IntoElement,
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> Stateful<Div> {
    let state = floating(Kind::Card, window, cx);
    let body = overlay_surface(theme)
        .id(CARD_BODY)
        .occlude()
        .max_w(px(CARD_WIDTH))
        .p(px(CARD_PAD))
        .on_hover(move |inside, window, cx| {
            let exit = window.mouse_position();
            state.update(cx, |floating, cx| match *inside {
                true => floating.hold(),
                false => floating.leave(CARD_BODY.into(), exit, cx),
            })
        })
        .child(card);
    float(
        Kind::Card,
        key.into(),
        (target, edge),
        body,
        theme,
        window,
        cx,
    )
}

fn float(
    kind: Kind,
    key: ElementId,
    (target, edge): (Stateful<Div>, Edge),
    body: impl IntoElement + 'static,
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> Stateful<Div> {
    let state = floating(kind, window, cx);
    let view = window.current_view();
    let reduced = cx.reduce_motion();
    let alpha = state.update(cx, |floating, _| {
        floating.reduced = reduced;
        floating.glide.set_reduced(reduced);
        let alpha = floating.alpha(Instant::now());
        (floating.current.as_ref() == Some(&key) && alpha > 0.0).then_some(alpha)
    });
    let (hover, probe, place) = (state.clone(), state.clone(), state);
    let (mine, placed_key) = (key.clone(), key.clone());
    let target = target
        .relative()
        .on_hover(move |inside, window, cx| {
            let exit = window.mouse_position();
            hover.update(cx, |floating, cx| match *inside {
                true => floating.enter(key.clone(), view, cx),
                false => floating.leave(key.clone(), exit, cx),
            })
        })
        .child(
            canvas(
                move |bounds, window, cx| {
                    probe.update(cx, |floating, _| {
                        floating
                            .rects
                            .insert(mine.clone(), edge.painted(bounds, window));
                        if floating.current.as_ref() == Some(&mine)
                            && floating.fading(Instant::now())
                        {
                            window.request_animation_frame();
                        }
                    })
                },
                |_, (), _, _| {},
            )
            .absolute()
            .inset_0()
            .size_full(),
        );
    let Some(alpha) = alpha else {
        return target;
    };
    target.child(
        deferred(Placed {
            key: placed_key,
            alpha,
            fill: overlay_fill(theme),
            rim: overlay_rim(theme),
            state: place,
            panel: own_text(theme)
                .opacity(alpha)
                .child(body)
                .into_any_element(),
        })
        .priority(1),
    )
}

struct Placed {
    key: ElementId,
    alpha: f32,
    fill: Rgba,
    rim: Rgba,
    state: Entity<Floating>,
    panel: AnyElement,
}

impl IntoElement for Placed {
    type Element = Self;

    fn into_element(self) -> Self {
        self
    }
}

impl Element for Placed {
    type RequestLayoutState = ();
    type PrepaintState = Option<Placement>;

    fn id(&self) -> Option<ElementId> {
        None
    }

    fn source_location(&self) -> Option<&'static Location<'static>> {
        None
    }

    fn request_layout(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        window: &mut Window,
        cx: &mut App,
    ) -> (LayoutId, ()) {
        let style = Style {
            position: Position::Absolute,
            ..Style::default()
        };
        (window.request_layout(style, [], cx), ())
    }

    fn prepaint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        _: Bounds<Pixels>,
        _: &mut (),
        window: &mut Window,
        cx: &mut App,
    ) -> Option<Placement> {
        let now = Instant::now();
        let (anchor, moving) = self.state.update(cx, |floating, _| {
            let target = floating.rects.get(&self.key).copied()?;
            if floating.shown {
                floating.glide.retarget(target, now);
            }
            let anchor = floating.glide.sample(now).map_or(target, |(rect, _)| rect);
            Some((anchor, floating.glide.moving()))
        })?;
        if moving {
            window.request_animation_frame();
        }
        let room = size(AvailableSpace::MaxContent, AvailableSpace::MaxContent);
        let measured = self.panel.layout_as_root(room, window, cx);
        let viewport = window.viewport_size();
        let reach = px(ARROW_CLEARANCE + ARROW_HEIGHT);
        let above = (anchor.top() - reach - measured.height).round();
        let below = above < px(EDGE);
        let y = match below {
            true => (anchor.bottom() + reach).round(),
            false => above,
        };
        let centred = anchor.center().x - measured.width / 2.0;
        let widest = (viewport.width - measured.width - px(EDGE)).max(px(EDGE));
        let x = centred.clamp(px(EDGE), widest);
        let origin = point(x.round(), y);
        let placement = Placement {
            bounds: Bounds::new(origin, measured),
            anchor,
            below,
        };
        self.panel.prepaint_at(origin, window, cx);
        self.state
            .update(cx, |floating, _| floating.placed = Some(placement));
        Some(placement)
    }

    fn paint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        _: Bounds<Pixels>,
        _: &mut (),
        placement: &mut Option<Placement>,
        window: &mut Window,
        cx: &mut App,
    ) {
        let Some(placement) = placement else {
            return;
        };
        self.panel.paint(window, cx);
        let placed = placement.bounds;
        let half = px(ARROW_WIDTH / 2.0);
        let low = placed.left() + px(ARROW_INSET) + half;
        let high = (placed.right() - px(ARROW_INSET) - half).max(low);
        let tip_x = placement.anchor.center().x.clamp(low, high);
        let (edge_y, inward) = match placement.below {
            true => (placed.top(), px(RIM_WIDTH)),
            false => (placed.bottom(), px(-RIM_WIDTH)),
        };
        let tip = point(tip_x, placement.arrow_tip());
        let mut body = PathBuilder::fill();
        body.move_to(point(tip_x - half, edge_y + inward));
        body.line_to(point(tip_x + half, edge_y + inward));
        body.line_to(tip);
        body.close();
        if let Ok(body) = body.build() {
            window.paint_path(body, tint(self.fill, self.alpha));
        }
        let seam_y = edge_y + inward / 2.0;
        let mut outline = PathBuilder::stroke(px(RIM_WIDTH));
        outline.move_to(point(tip_x - half, seam_y));
        outline.line_to(tip);
        outline.line_to(point(tip_x + half, seam_y));
        if let Ok(outline) = outline.build() {
            window.paint_path(outline, tint(self.rim, self.rim.alpha * self.alpha));
        }
        let state = self.state.clone();
        window.on_mouse_event(move |event: &MouseMoveEvent, phase, _, cx| {
            if phase == DispatchPhase::Bubble {
                state.update(cx, |floating, cx| floating.travel(event.position, cx));
            }
        });
    }
}
