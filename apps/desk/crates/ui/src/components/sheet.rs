use std::f32::consts::TAU;
use std::rc::Rc;
use std::time::{Duration, Instant};

use desk_motion::reduced_motion;
use desk_motion::tokens::{EASE_OUT, HOVER_MS, PANEL_IN_MS, PANEL_OUT_MS};
use gpui::{
    AnyElement, App, Background, Bounds, BoxShadow, DispatchPhase, Div, ElementId, Empty, Entity,
    Length, Motion, MouseButton, Pixels, Point, SharedString, SpringConfig, SpringState, Stateful,
    Window, div, linear_color_stop, linear_gradient, point, prelude::*, px, relative,
};

use crate::components::button::{ButtonKind, button};
use crate::components::overlay::Side;
use crate::components::paint::{ink, ring, tint};
use crate::components::size::{
    BUTTON_GAP, FONT_BODY, HEADER, HEADER_PAD_LEFT, POP_SHADOW_BLUR, RADIUS_POP, SHEET_HANDLE,
    SHEET_HANDLE_HEIGHT, SHEET_HANDLE_WIDTH, SHEET_INSET, SHEET_RING, SHEET_SHADOW_Y, SHEET_SHARE,
    T1,
};
use crate::live::ActiveTheme;
use crate::theme::{ColorToken, Theme};

const SETTLED: f32 = 0.002;
const VISUAL_TO_PERIOD: f32 = 1.2;
const CLOSE_SHARE: f32 = 0.25;
const FLING: f32 = 400.0;
const STALE: Duration = Duration::from_millis(50);
const CLICK_SLOP: f32 = 3.0;
const KNOB_HIT: f32 = 18.0;
const KNOB_LIT: f32 = 0.34;
const SCRIM_TOP: f32 = 0.2;
const SCRIM_BOTTOM: f32 = 0.48;
const SHEET_SIZE: f32 = 360.0;
const MODAL_WIDTH: f32 = 420.0;
const MODAL_SHARE: f32 = 0.9;
pub(crate) const MODAL_RISE: f32 = 8.0;

type Handler = Rc<dyn Fn(&mut Window, &mut App)>;

#[derive(Clone, Copy)]
struct Grab {
    origin: Point<Pixels>,
    start: f32,
    shown: f32,
    velocity: f32,
    at: Instant,
    moved: bool,
}

pub(crate) struct Slide {
    open: bool,
    from: SpringState,
    since: Instant,
    extent: f32,
    grab: Option<Grab>,
}

pub(crate) struct Pose {
    pub(crate) shown: f32,
    pub(crate) hidden: f32,
    pub(crate) opacity: f32,
    measured: bool,
}

impl Slide {
    pub(crate) fn new() -> Self {
        Slide {
            open: false,
            from: SpringState::default(),
            since: Instant::now(),
            extent: 0.0,
            grab: None,
        }
    }

    fn spring(&self) -> SpringConfig {
        let visual = if self.open { PANEL_IN_MS } else { PANEL_OUT_MS };
        let frequency = TAU / (VISUAL_TO_PERIOD * visual.as_secs_f32());
        SpringConfig::new(frequency * frequency, 2.0 * frequency, 1.0)
    }

    fn target(&self) -> f32 {
        if self.open { 1.0 } else { 0.0 }
    }

    fn state(&self, now: Instant) -> SpringState {
        let elapsed = now.saturating_duration_since(self.since).as_secs_f32();
        self.spring().step(self.from, self.target(), elapsed)
    }

    fn shown(&self, now: Instant) -> f32 {
        self.grab
            .map_or_else(|| self.state(now).position, |grab| grab.shown)
            .clamp(0.0, 1.0)
    }

    fn moving(&self, now: Instant) -> bool {
        self.grab.is_none()
            && !self
                .spring()
                .is_settled(self.state(now), self.target(), SETTLED)
    }

    fn set_open(&mut self, open: bool, now: Instant) {
        if open != self.open && self.grab.is_none() {
            self.from = self.state(now);
            self.open = open;
            self.since = now;
        }
    }

    fn grab(&mut self, at: Point<Pixels>, now: Instant) {
        if self.extent > 0.0 {
            let shown = self.shown(now);
            self.grab = Some(Grab {
                origin: at,
                start: shown,
                shown,
                velocity: self.state(now).velocity,
                at: now,
                moved: false,
            });
        }
    }

    fn drag(&mut self, at: Point<Pixels>, side: Side, now: Instant) {
        let extent = self.extent;
        let Some(grab) = self.grab.as_mut() else {
            return;
        };
        let toward_edge = outward(side, at - grab.origin);
        let shown = (grab.start - toward_edge / extent).clamp(0.0, 1.0);
        let elapsed = now.saturating_duration_since(grab.at).as_secs_f32();
        if elapsed > 0.0 {
            grab.velocity = (shown - grab.shown) / elapsed;
        }
        grab.shown = shown;
        grab.at = now;
        grab.moved |= toward_edge.abs() > CLICK_SLOP;
    }

    fn let_go(&mut self, now: Instant) -> bool {
        let Some(grab) = self.grab.take() else {
            return false;
        };
        let fresh = now.saturating_duration_since(grab.at) < STALE;
        let velocity = if fresh { grab.velocity } else { 0.0 };
        let fling = velocity * self.extent;
        let close =
            !grab.moved || fling < -FLING || (grab.shown < 1.0 - CLOSE_SHARE && fling <= FLING);
        self.from = SpringState {
            position: grab.shown,
            velocity,
        };
        self.open = !close;
        self.since = now;
        close
    }
}

fn outward(side: Side, delta: Point<Pixels>) -> f32 {
    match side {
        Side::Top => -delta.y.as_f32(),
        Side::Right => delta.x.as_f32(),
        Side::Bottom => delta.y.as_f32(),
        Side::Left => -delta.x.as_f32(),
    }
}

fn vertical(side: Side) -> bool {
    matches!(side, Side::Top | Side::Bottom)
}

fn extent(side: Side, bounds: &Bounds<Pixels>) -> f32 {
    if vertical(side) {
        bounds.size.height.as_f32()
    } else {
        bounds.size.width.as_f32()
    }
}

fn inward(side: Side, distance: f32) -> Point<Pixels> {
    match side {
        Side::Top => point(px(0.0), px(distance)),
        Side::Right => point(px(-distance), px(0.0)),
        Side::Bottom => point(px(0.0), px(-distance)),
        Side::Left => point(px(distance), px(0.0)),
    }
}

fn place(side: Side, panel: Div, size: Length, push: f32) -> Div {
    let (inset, edge) = (px(SHEET_INSET), px(SHEET_INSET - push));
    match side {
        Side::Top => panel.left(inset).right(inset).top(edge).h(size),
        Side::Right => panel.top(inset).bottom(inset).right(edge).w(size),
        Side::Bottom => panel.left(inset).right(inset).bottom(edge).h(size),
        Side::Left => panel.top(inset).bottom(inset).left(edge).w(size),
    }
}

pub(crate) fn scrim_fill(shown: f32, theme: &Theme) -> Background {
    let shade = theme.color(ColorToken::Shadow);
    linear_gradient(
        180.0,
        linear_color_stop(tint(shade, SCRIM_TOP * shown), 0.0),
        linear_color_stop(tint(shade, SCRIM_BOTTOM * shown), 1.0),
    )
}

pub(crate) fn pose(
    slide: &Entity<Slide>,
    open: bool,
    window: &mut Window,
    cx: &mut App,
) -> Option<Pose> {
    let now = Instant::now();
    let reduced = reduced_motion(cx);
    let (shown, grabbing, moving, extent, open) = slide.update(cx, |slide, _| {
        slide.set_open(open, now);
        (
            slide.shown(now),
            slide.grab.is_some(),
            slide.moving(now),
            slide.extent,
            slide.open,
        )
    });
    if moving {
        window.request_animation_frame();
    }
    let still = reduced && !grabbing;
    (open || moving || grabbing).then_some(Pose {
        shown,
        hidden: if still { 0.0 } else { 1.0 - shown },
        opacity: if still { shown } else { 1.0 },
        measured: extent > 0.0,
    })
}

fn stage(id: ElementId, slide: Entity<Slide>, side: Side) -> Stateful<Div> {
    div()
        .absolute()
        .top_0()
        .left_0()
        .size_full()
        .overflow_hidden()
        .on_children_prepainted(move |bounds, _, cx| {
            if let Some(panel) = bounds.last() {
                let measured = extent(side, panel);
                slide.update(cx, |slide, _| slide.extent = measured);
            }
        })
        .id(id)
}

fn panel(side: Side, size: Length, pose: &Pose, extent: f32, theme: &Theme) -> Div {
    let push = pose.hidden * (extent + SHEET_INSET);
    place(
        side,
        card(pose, inward(side, SHEET_SHADOW_Y.abs()), theme).absolute(),
        size,
        push,
    )
    .when(vertical(side), |panel| panel.flex_col())
}

fn modal_card(pose: &Pose, theme: &Theme) -> Div {
    card(pose, point(px(0.0), px(SHEET_SHADOW_Y.abs())), theme)
        .relative()
        .top(px(pose.hidden * MODAL_RISE))
        .w(px(MODAL_WIDTH))
        .max_w(relative(MODAL_SHARE))
        .opacity(pose.shown)
}

fn card(pose: &Pose, offset: Point<Pixels>, theme: &Theme) -> Div {
    let shadow = theme.color(ColorToken::Shadow);
    div()
        .flex()
        .bg(tint(theme.color(ColorToken::ToastFill), 1.0))
        .rounded(px(RADIUS_POP))
        .shadow(vec![
            ring(ink(theme, SHEET_RING)),
            BoxShadow {
                color: tint(shadow, shadow.alpha * pose.shown).into(),
                offset,
                blur_radius: px(POP_SHADOW_BLUR),
                spread_radius: px(0.0),
                inset: false,
            },
        ])
        .text_size(px(FONT_BODY))
        .text_color(ink(theme, T1))
        .opacity(pose.opacity)
        .when(!pose.measured, |panel| panel.invisible())
        .occlude()
}

#[derive(IntoElement)]
pub struct Drawer {
    id: ElementId,
    side: Side,
    open: bool,
    on_close: Option<Handler>,
    children: Vec<AnyElement>,
}

impl Drawer {
    pub fn new(id: impl Into<ElementId>) -> Self {
        Drawer {
            id: id.into(),
            side: Side::Bottom,
            open: false,
            on_close: None,
            children: Vec::new(),
        }
    }

    pub fn side(mut self, side: Side) -> Self {
        self.side = side;
        self
    }

    pub fn open(mut self, open: bool) -> Self {
        self.open = open;
        self
    }

    pub fn on_close(mut self, on_close: impl Fn(&mut Window, &mut App) + 'static) -> Self {
        self.on_close = Some(Rc::new(on_close));
        self
    }
}

impl ParentElement for Drawer {
    fn extend(&mut self, elements: impl IntoIterator<Item = AnyElement>) {
        self.children.extend(elements);
    }
}

impl RenderOnce for Drawer {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let slide = window.use_keyed_state(self.id.clone(), cx, |_, _| Slide::new());
        let Some(pose) = pose(&slide, self.open, window, cx) else {
            return Empty.into_any_element();
        };
        let theme = ActiveTheme::theme(cx);
        let side = self.side;
        let dismiss = self.on_close.clone().filter(|_| self.open);
        let size = relative(SHEET_SHARE).into();
        let drawn = panel(side, size, &pose, slide.read(cx).extent, &theme)
            .when(matches!(side, Side::Top), |panel| panel.flex_col_reverse())
            .when(matches!(side, Side::Left), |panel| panel.flex_row_reverse())
            .child(knob(side, &slide, self.on_close, &theme))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .flex_1()
                    .min_h_0()
                    .min_w_0()
                    .overflow_hidden()
                    .children(self.children),
            );
        stage(self.id, slide, side)
            .when_some(dismiss, |stage, on_close| {
                stage.on_mouse_down(MouseButton::Left, move |_, window, cx| {
                    cx.stop_propagation();
                    on_close(window, cx);
                })
            })
            .child(drawn)
            .into_any_element()
    }
}

fn knob(
    side: Side,
    slide: &Entity<Slide>,
    on_close: Option<Handler>,
    theme: &Theme,
) -> impl IntoElement {
    let (width, height) = if vertical(side) {
        (SHEET_HANDLE_WIDTH, SHEET_HANDLE_HEIGHT)
    } else {
        (SHEET_HANDLE_HEIGHT, SHEET_HANDLE_WIDTH)
    };
    let lit = ink(theme, KNOB_LIT);
    let (grabber, mover, releaser) = (slide.clone(), slide.clone(), slide.clone());
    div()
        .id("drawer-knob")
        .group("drawer-knob")
        .aria_label("Close")
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .when(vertical(side), |knob| knob.w_full().h(px(KNOB_HIT)))
        .when(!vertical(side), |knob| knob.h_full().w(px(KNOB_HIT)))
        .cursor_grab()
        .on_mouse_down(MouseButton::Left, move |event, _, cx| {
            grabber.update(cx, |slide, cx| {
                slide.grab(event.position, Instant::now());
                cx.notify();
            });
            cx.stop_propagation();
        })
        .on_mouse_move_all(move |event, phase, _, _, cx| {
            if phase == DispatchPhase::Capture {
                mover.update(cx, |slide, cx| {
                    if slide.grab.is_some() {
                        slide.drag(event.position, side, Instant::now());
                        cx.notify();
                    }
                });
            }
        })
        .on_mouse_up_all(move |event, phase, _, window, cx| {
            if phase != DispatchPhase::Capture || event.button != MouseButton::Left {
                return;
            }
            let closed = releaser.update(cx, |slide, cx| {
                let held = slide.grab.is_some();
                let closed = slide.let_go(Instant::now());
                if held {
                    cx.notify();
                }
                closed
            });
            if let Some(on_close) = on_close.as_ref().filter(|_| closed) {
                on_close(window, cx);
            }
        })
        .child(
            div()
                .id("drawer-knob-bar")
                .w(px(width))
                .h(px(height))
                .rounded_full()
                .bg(ink(theme, SHEET_HANDLE))
                .transitions(|transitions| {
                    transitions.bg(Motion::new(HOVER_MS).with_easing(EASE_OUT))
                })
                .group_hover("drawer-knob", move |style| style.bg(lit)),
        )
}

#[derive(IntoElement)]
pub struct Sheet {
    id: ElementId,
    title: SharedString,
    side: Side,
    open: bool,
    on_cancel: Option<Handler>,
    on_confirm: Option<Handler>,
    confirm: SharedString,
    aside: Option<(SharedString, Handler)>,
    modal: bool,
    children: Vec<AnyElement>,
}

impl Sheet {
    pub fn new(id: impl Into<ElementId>, title: impl Into<SharedString>) -> Self {
        Sheet {
            id: id.into(),
            title: title.into(),
            side: Side::Right,
            open: false,
            on_cancel: None,
            on_confirm: None,
            confirm: "Confirm".into(),
            aside: None,
            modal: false,
            children: Vec::new(),
        }
    }

    pub fn side(mut self, side: Side) -> Self {
        self.side = side;
        self
    }

    pub fn modal(mut self) -> Self {
        self.modal = true;
        self
    }

    pub fn confirm(mut self, label: impl Into<SharedString>) -> Self {
        self.confirm = label.into();
        self
    }

    pub fn aside(
        mut self,
        label: impl Into<SharedString>,
        on_aside: impl Fn(&mut Window, &mut App) + 'static,
    ) -> Self {
        self.aside = Some((label.into(), Rc::new(on_aside)));
        self
    }

    pub fn open(mut self, open: bool) -> Self {
        self.open = open;
        self
    }

    pub fn on_cancel(mut self, on_cancel: impl Fn(&mut Window, &mut App) + 'static) -> Self {
        self.on_cancel = Some(Rc::new(on_cancel));
        self
    }

    pub fn on_confirm(mut self, on_confirm: impl Fn(&mut Window, &mut App) + 'static) -> Self {
        self.on_confirm = Some(Rc::new(on_confirm));
        self
    }
}

impl ParentElement for Sheet {
    fn extend(&mut self, elements: impl IntoIterator<Item = AnyElement>) {
        self.children.extend(elements);
    }
}

impl RenderOnce for Sheet {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let slide = window.use_keyed_state(self.id.clone(), cx, |_, _| Slide::new());
        let Some(pose) = pose(&slide, self.open, window, cx) else {
            return Empty.into_any_element();
        };
        let theme = ActiveTheme::theme(cx);
        let call = |handler: &Option<Handler>| {
            let handler = handler.clone();
            move |window: &mut Window, cx: &mut App| {
                if let Some(handler) = &handler {
                    handler(window, cx);
                }
            }
        };
        let (outside, cancel, confirm) = (
            call(&self.on_cancel),
            call(&self.on_cancel),
            call(&self.on_confirm),
        );
        let scrim = div()
            .id("sheet-scrim")
            .absolute()
            .top_0()
            .left_0()
            .size_full()
            .bg(scrim_fill(pose.shown, &theme))
            .when(self.open, |scrim| {
                scrim
                    .occlude()
                    .on_click(move |_, window, cx| outside(window, cx))
            });
        let modal = self.modal;
        let surface = if modal {
            modal_card(&pose, &theme)
        } else {
            panel(
                self.side,
                px(SHEET_SIZE).into(),
                &pose,
                slide.read(cx).extent,
                &theme,
            )
        };
        let drawn = surface
            .flex_col()
            .child(
                div()
                    .h(px(HEADER))
                    .flex()
                    .flex_none()
                    .items_center()
                    .px(px(HEADER_PAD_LEFT))
                    .child(div().flex_1().truncate().child(self.title)),
            )
            .child(
                div()
                    .flex()
                    .flex_col()
                    .when(!modal, |body| body.flex_1().min_h_0())
                    .px(px(HEADER_PAD_LEFT))
                    .overflow_hidden()
                    .children(self.children),
            )
            .child(
                div()
                    .flex()
                    .flex_none()
                    .justify_end()
                    .gap(px(BUTTON_GAP))
                    .p(px(HEADER_PAD_LEFT))
                    .child(
                        button("sheet-cancel", "Cancel", None, ButtonKind::Plain, &theme)
                            .on_click(move |_, window, cx| cancel(window, cx)),
                    )
                    .children(self.aside.map(|(label, on_aside)| {
                        button("sheet-aside", label, None, ButtonKind::Plain, &theme)
                            .on_click(move |_, window, cx| on_aside(window, cx))
                    }))
                    .child(
                        button(
                            "sheet-confirm",
                            self.confirm,
                            None,
                            ButtonKind::Primary,
                            &theme,
                        )
                        .on_click(move |_, window, cx| confirm(window, cx)),
                    ),
            );
        stage(self.id, slide, self.side)
            .when(modal, |stage| stage.flex().items_center().justify_center())
            .child(scrim)
            .child(drawn)
            .into_any_element()
    }
}
