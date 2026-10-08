use std::cell::Cell;
use std::f32::consts::TAU;
use std::panic::Location;
use std::rc::Rc;
use std::time::{Duration, Instant};

use desk_motion::tokens::{EASE_OUT, PANEL_IN_MS, PANEL_OUT_MS};
use desk_motion::{GlideKind, reduced_motion};
use gpui::{
    Animation, AnimationExt, AnyElement, App, Bounds, ClickEvent, Context, Corners, DispatchPhase,
    Display, Div, Element, ElementId, Entity, FocusHandle, GlobalElementId, InspectorElementId,
    KeyDownEvent, LayoutId, MouseButton, MouseDownEvent, Pixels, Point, Position, Rgba,
    SharedString, Size, SpringConfig, SpringState, Stateful, Style, Task, Window, canvas, deferred,
    div, point, prelude::*, px, relative,
};

use crate::component::{control, icon};
use crate::components::avatar::spinner;
use crate::components::card::caption;
use crate::components::chip::{chip, flat_chip, kbd};
use crate::components::glyph::Glyph;
use crate::components::list::{HoverList, Marker, separator};
use crate::components::paint::{drop, glyph, ink, ring, tint};
use crate::components::size::{
    BADGE_FILL, CAPTION_TEXT, CHIP_FILL, FONT_BODY, GLASS_BLUR, GLASS_FILL, HEADER,
    HEADER_PAD_LEFT, HOVER, MENU_PAD, MENU_WIDTH, POP_RING, POP_SHADOW_BLUR, POP_SHADOW_Y,
    POPOVER_PAD_X, POPOVER_PAD_Y, POPOVER_WIDTH, RADIUS_POP, RADIUS_ROW, RISE, ROW_PAD_X,
    ROW_PAD_Y, SHEET_HANDLE, SHEET_HANDLE_HEIGHT, SHEET_HANDLE_WIDTH, SHEET_INSET, SHEET_RING,
    SHEET_SHADOW_Y, SHEET_SHARE, T1, TOAST_BOTTOM, TOAST_RIGHT, TOAST_WIDTH,
};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{ICON_SMALL, ICON_TINY, RADIUS_CONTROL, TOAST_DISMISS};
use crate::theme::{ColorToken, Theme};

const PLACEMENT_OFFSET: f32 = 6.0;
const SUBMENU_OFFSET: f32 = 2.0;
const CAPTION_GAP: f32 = 6.0;
const MENU_SHADOW_Y: f32 = 8.0;
const MENU_SHADOW_BLUR: f32 = 24.0;
const MENU_SHADOW_SHARE: f32 = 0.5;
const TOAST_HEIGHT: f32 = 44.0;
const TOAST_LIFETIME: Duration = Duration::from_millis(4000);
const TOAST_SETTLED: f32 = 0.01;
const STACK_GAP: f32 = 14.0;
const STACK_SHRINK: f32 = 0.05;
const STACK_VISIBLE: usize = 3;
const SWIPE_DISTANCE: f32 = 45.0;
const SWIPE_FLING: f32 = 110.0;
const SWIPE_RESIST: f32 = 0.1;
const SWIPE_STALE: Duration = Duration::from_millis(50);
const VISUAL_TO_PERIOD: f32 = 1.2;

fn opaque_fill(theme: &Theme) -> Rgba {
    tint(theme.color(ColorToken::ToastFill), 1.0)
}

fn floating(ring_alpha: f32, shadow_y: f32, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_col()
        .bg(opaque_fill(theme))
        .shadow(vec![
            ring(ink(theme, ring_alpha)),
            drop(theme.color(ColorToken::Shadow), shadow_y, POP_SHADOW_BLUR),
        ])
        .text_size(px(FONT_BODY))
        .text_color(ink(theme, T1))
        .occlude()
}

pub fn menu_surface(theme: &Theme) -> Div {
    let shadow = theme.color(ColorToken::Shadow);
    floating(POP_RING, POP_SHADOW_Y, theme)
        .bg(tint(theme.color(ColorToken::ToastFill), GLASS_FILL))
        .backdrop_blur(px(GLASS_BLUR))
        .shadow(vec![
            ring(ink(theme, POP_RING)),
            drop(
                tint(shadow, shadow.alpha * MENU_SHADOW_SHARE),
                MENU_SHADOW_Y,
                MENU_SHADOW_BLUR,
            ),
        ])
}

pub fn sheet(title: SharedString, actions: impl IntoElement, theme: &Theme) -> Div {
    floating(SHEET_RING, SHEET_SHADOW_Y, theme)
        .absolute()
        .left(px(SHEET_INSET))
        .right(px(SHEET_INSET))
        .bottom(px(SHEET_INSET))
        .h(relative(SHEET_SHARE))
        .rounded(px(RADIUS_POP))
        .child(
            div().flex().justify_center().pt_1p5().child(
                div()
                    .w(px(SHEET_HANDLE_WIDTH))
                    .h(px(SHEET_HANDLE_HEIGHT))
                    .rounded_full()
                    .bg(ink(theme, SHEET_HANDLE)),
            ),
        )
        .child(
            div()
                .h(px(HEADER))
                .flex()
                .flex_none()
                .items_center()
                .gap_2()
                .px(px(HEADER_PAD_LEFT))
                .child(div().flex_1().truncate().child(title))
                .child(actions),
        )
}

pub fn popover(theme: &Theme) -> Div {
    floating(POP_RING, POP_SHADOW_Y, theme)
        .w(px(POPOVER_WIDTH))
        .gap_2()
        .px(px(POPOVER_PAD_X))
        .py(px(POPOVER_PAD_Y))
        .rounded(px(RADIUS_POP))
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Side {
    Top,
    Right,
    Bottom,
    Left,
}

impl Side {
    fn opposite(self) -> Self {
        match self {
            Side::Top => Side::Bottom,
            Side::Right => Side::Left,
            Side::Bottom => Side::Top,
            Side::Left => Side::Right,
        }
    }

    fn fits(
        self,
        anchor: Bounds<Pixels>,
        size: Size<Pixels>,
        gap: Pixels,
        room: Size<Pixels>,
    ) -> bool {
        match self {
            Side::Top => anchor.top() - gap - size.height >= px(0.0),
            Side::Right => anchor.right() + gap + size.width <= room.width,
            Side::Bottom => anchor.bottom() + gap + size.height <= room.height,
            Side::Left => anchor.left() - gap - size.width >= px(0.0),
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Align {
    Start,
    Center,
    End,
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Placement {
    pub side: Side,
    pub align: Align,
    pub offset: f32,
}

impl Placement {
    pub fn below() -> Self {
        Placement {
            side: Side::Bottom,
            align: Align::Start,
            offset: PLACEMENT_OFFSET,
        }
    }

    fn origin(
        self,
        anchor: Bounds<Pixels>,
        size: Size<Pixels>,
        room: Size<Pixels>,
        rest: f32,
    ) -> Point<Pixels> {
        let gap = px(self.offset);
        let flipped = self.side.opposite();
        let side =
            if !self.side.fits(anchor, size, gap, room) && flipped.fits(anchor, size, gap, room) {
                flipped
            } else {
                self.side
            };
        let (start, extent, span, limit) = match side {
            Side::Top | Side::Bottom => (anchor.left(), anchor.size.width, size.width, room.width),
            Side::Left | Side::Right => {
                (anchor.top(), anchor.size.height, size.height, room.height)
            }
        };
        let cross = match self.align {
            Align::Start => start,
            Align::Center => start + (extent - span) / 2.0,
            Align::End => start + extent - span,
        }
        .min(limit - span)
        .max(px(0.0));
        let slide = px(RISE * rest);
        match side {
            Side::Top => point(cross, anchor.top() - gap - size.height + slide),
            Side::Right => point(anchor.right() + gap - slide, cross),
            Side::Bottom => point(cross, anchor.bottom() + gap - slide),
            Side::Left => point(anchor.left() - gap - size.width + slide, cross),
        }
    }
}

impl Default for Placement {
    fn default() -> Self {
        Placement::below()
    }
}

type Anchor = Rc<Cell<Bounds<Pixels>>>;

fn measure(anchor: &Anchor) -> impl IntoElement {
    let anchor = anchor.clone();
    canvas(move |bounds, _, _| anchor.set(bounds), |_, _, _, _| {})
        .absolute()
        .size_full()
}

struct Beside {
    anchor: Anchor,
    placement: Placement,
    rest: f32,
    panel: Stateful<Div>,
}

impl Beside {
    fn reveal(mut self, t: f32) -> Self {
        self.rest = 1.0 - t;
        self.panel = self.panel.opacity(t);
        self
    }
}

impl IntoElement for Beside {
    type Element = BesideElement;

    fn into_element(self) -> BesideElement {
        BesideElement {
            anchor: self.anchor,
            placement: self.placement,
            rest: self.rest,
            panel: self.panel.into_any_element(),
        }
    }
}

struct BesideElement {
    anchor: Anchor,
    placement: Placement,
    rest: f32,
    panel: AnyElement,
}

impl IntoElement for BesideElement {
    type Element = Self;

    fn into_element(self) -> Self {
        self
    }
}

impl Element for BesideElement {
    type RequestLayoutState = LayoutId;
    type PrepaintState = ();

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
    ) -> (LayoutId, LayoutId) {
        let panel = self.panel.request_layout(window, cx);
        let style = Style {
            position: Position::Absolute,
            display: Display::Flex,
            ..Style::default()
        };
        (window.request_layout(style, [panel], cx), panel)
    }

    fn prepaint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        bounds: Bounds<Pixels>,
        panel: &mut LayoutId,
        window: &mut Window,
        cx: &mut App,
    ) {
        let size = window.layout_bounds(*panel).size;
        let origin =
            self.placement
                .origin(self.anchor.get(), size, window.viewport_size(), self.rest);
        let offset = origin - bounds.origin;
        window.with_element_offset(point(offset.x.round(), offset.y.round()), |window| {
            self.panel.prepaint(window, cx);
        });
    }

    fn paint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        _: Bounds<Pixels>,
        _: &mut LayoutId,
        _: &mut (),
        window: &mut Window,
        cx: &mut App,
    ) {
        self.panel.paint(window, cx);
    }
}

fn open_beside(
    id: impl Into<ElementId>,
    anchor: Anchor,
    placement: Placement,
    panel: Stateful<Div>,
    cx: &App,
) -> AnyElement {
    let beside = Beside {
        anchor,
        placement,
        rest: 0.0,
        panel,
    };
    let beside = if reduced_motion(cx) {
        beside.into_any_element()
    } else {
        beside
            .with_animation(
                id,
                Animation::new(PANEL_IN_MS).with_easing(EASE_OUT),
                Beside::reveal,
            )
            .into_any_element()
    };
    deferred(beside).into_any_element()
}

#[derive(IntoElement)]
pub struct Popover {
    id: ElementId,
    trigger: AnyElement,
    anchor: Anchor,
    open: bool,
    placement: Placement,
    width: PanelWidth,
    children: Vec<AnyElement>,
}

#[derive(Clone, Copy)]
enum PanelWidth {
    Fixed(f32),
    Fit { min: f32 },
}

impl Popover {
    pub fn new(
        id: impl Into<ElementId>,
        trigger: impl ParentElement + Styled + IntoElement,
    ) -> Self {
        let anchor = Anchor::default();
        Popover {
            id: id.into(),
            trigger: trigger
                .relative()
                .child(measure(&anchor))
                .into_any_element(),
            anchor,
            open: false,
            placement: Placement::below(),
            width: PanelWidth::Fixed(POPOVER_WIDTH),
            children: Vec::new(),
        }
    }

    pub fn open(mut self, open: bool) -> Self {
        self.open = open;
        self
    }

    pub fn placement(mut self, placement: Placement) -> Self {
        self.placement = placement;
        self
    }

    pub fn width(mut self, width: f32) -> Self {
        self.width = PanelWidth::Fixed(width);
        self
    }

    pub fn fit(mut self, min: f32) -> Self {
        self.width = PanelWidth::Fit { min };
        self
    }
}

impl ParentElement for Popover {
    fn extend(&mut self, elements: impl IntoIterator<Item = AnyElement>) {
        self.children.extend(elements);
    }
}

impl RenderOnce for Popover {
    fn render(self, _: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let panel = self.open.then(|| {
            let panel = popover(&theme);
            let panel = match self.width {
                PanelWidth::Fixed(width) => panel.w(px(width)),
                PanelWidth::Fit { min } => panel.w_auto().min_w(px(min)),
            };
            let panel = panel.id("popover-panel").children(self.children);
            open_beside(self.id, self.anchor, self.placement, panel, cx)
        });
        div().child(self.trigger).children(panel)
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum MenuIcon {
    Icon(Icon),
    Glyph(Glyph),
}

impl From<Icon> for MenuIcon {
    fn from(icon: Icon) -> Self {
        MenuIcon::Icon(icon)
    }
}

impl From<Glyph> for MenuIcon {
    fn from(glyph: Glyph) -> Self {
        MenuIcon::Glyph(glyph)
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum MenuItem {
    Action {
        label: SharedString,
        keys: Option<SharedString>,
        icon: Option<MenuIcon>,
    },
    Separator,
    Caption(SharedString),
    Submenu {
        label: SharedString,
        icon: Option<MenuIcon>,
        items: Vec<MenuItem>,
    },
}

impl MenuItem {
    pub fn action(label: impl Into<SharedString>) -> Self {
        MenuItem::Action {
            label: label.into(),
            keys: None,
            icon: None,
        }
    }

    pub fn icon(self, leading: impl Into<MenuIcon>) -> Self {
        match self {
            MenuItem::Action { label, keys, .. } => MenuItem::Action {
                label,
                keys,
                icon: Some(leading.into()),
            },
            MenuItem::Submenu { label, items, .. } => MenuItem::Submenu {
                label,
                icon: Some(leading.into()),
                items,
            },
            MenuItem::Separator | MenuItem::Caption(_) => self,
        }
    }

    fn picks(&self) -> bool {
        matches!(self, MenuItem::Action { .. } | MenuItem::Submenu { .. })
    }

    fn span(&self) -> usize {
        match self {
            MenuItem::Submenu { items, .. } => 1 + items.iter().map(MenuItem::span).sum::<usize>(),
            MenuItem::Action { .. } | MenuItem::Separator | MenuItem::Caption(_) => 1,
        }
    }
}

pub fn actions<L: Into<SharedString>>(labels: impl IntoIterator<Item = L>) -> Vec<MenuItem> {
    labels.into_iter().map(MenuItem::action).collect()
}

fn preorder(items: &[MenuItem], top: usize) -> usize {
    items.iter().take(top).map(MenuItem::span).sum()
}

fn children_of(items: &[MenuItem], at: usize) -> &[MenuItem] {
    match items.get(at) {
        Some(MenuItem::Submenu { items, .. }) => items,
        Some(MenuItem::Action { .. } | MenuItem::Separator | MenuItem::Caption(_)) | None => &[],
    }
}

fn step_to(items: &[MenuItem], from: usize, down: bool) -> usize {
    let count = items.len();
    (1..=count)
        .map(|step| match down {
            true => (from + step) % count,
            false => (from % count + count - step % count) % count,
        })
        .find(|ix| items.get(*ix).is_some_and(MenuItem::picks))
        .unwrap_or(from)
}

fn first_pick(items: &[MenuItem], from: usize) -> usize {
    match items.get(from).is_some_and(MenuItem::picks) {
        true => from,
        false => step_to(items, from, true),
    }
}

enum MenuRow {
    Pick(Stateful<Div>),
    Inert(Div),
}

fn menu_entry(id: impl Into<ElementId>, item: &MenuItem, theme: &Theme) -> MenuRow {
    match item {
        MenuItem::Action {
            label,
            keys,
            icon: lead,
        } => MenuRow::Pick(
            menu_row(id, theme)
                .aria_label(label.clone())
                .children(lead.map(|lead| leading(lead, theme)))
                .child(div().flex_1().child(label.clone()))
                .children(keys.clone().map(|keys| kbd(keys, theme))),
        ),
        MenuItem::Submenu {
            label, icon: lead, ..
        } => MenuRow::Pick(
            menu_row(id, theme)
                .aria_label(label.clone())
                .children(lead.map(|lead| leading(lead, theme)))
                .child(div().flex_1().child(label.clone()))
                .child(icon(Icon::Arrow, ICON_TINY, ink(theme, CAPTION_TEXT))),
        ),
        MenuItem::Caption(label) => MenuRow::Inert(
            div()
                .px(px(ROW_PAD_X))
                .pt(px(ROW_PAD_Y))
                .pb(px(CAPTION_GAP))
                .child(caption(label.clone(), theme)),
        ),
        MenuItem::Separator => MenuRow::Inert(separator(theme).my_1()),
    }
}

pub fn menu(
    id: &'static str,
    items: &[MenuItem],
    theme: &Theme,
    on_pick: impl Fn(&usize, &mut Window, &mut App) + 'static,
) -> HoverList {
    let on_pick = Rc::new(on_pick);
    let frame = menu_surface(theme)
        .w(px(MENU_WIDTH))
        .p(px(MENU_PAD))
        .rounded(px(RADIUS_POP));
    let list = HoverList::within(
        id,
        frame,
        ink(theme, HOVER),
        Corners::all(px(RADIUS_ROW)),
        theme,
    )
    .backdrop(opaque_fill(theme));
    items.iter().enumerate().fold(list, |list, (ix, item)| {
        match menu_entry((id, ix), item, theme) {
            MenuRow::Pick(row) => {
                let (on_pick, at) = (on_pick.clone(), preorder(items, ix));
                list.item(row.on_click(move |_, window, cx| on_pick(&at, window, cx)))
            }
            MenuRow::Inert(row) => list.inert(row),
        }
    })
}

fn leading(lead: MenuIcon, theme: &Theme) -> Div {
    let color = ink(theme, CAPTION_TEXT);
    let mark = match lead {
        MenuIcon::Icon(mark) => icon(mark, ICON_SMALL, color),
        MenuIcon::Glyph(mark) => glyph(mark, ICON_SMALL, color),
    };
    div().flex_none().mr(px(ROW_PAD_X)).child(mark)
}

fn menu_row(id: impl Into<ElementId>, theme: &Theme) -> Stateful<Div> {
    div()
        .id(id)
        .flex()
        .items_center()
        .px(px(ROW_PAD_X))
        .py(px(ROW_PAD_Y))
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .text_color(ink(theme, T1))
}

pub fn toast(
    message: SharedString,
    badge: &'static str,
    theme: &Theme,
    on_dismiss: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> Div {
    floating(POP_RING, POP_SHADOW_Y, theme)
        .flex_row()
        .items_center()
        .gap_2p5()
        .w(px(TOAST_WIDTH))
        .py_2()
        .pl_3p5()
        .pr_2p5()
        .rounded(px(RADIUS_POP))
        .child(icon(Icon::Arrow, ICON_SMALL, ink(theme, CAPTION_TEXT)))
        .child(div().flex_1().child(message))
        .child(kbd(badge, theme))
        .child(
            control("toast-dismiss", "Dismiss", theme)
                .size(px(TOAST_DISMISS))
                .rounded(px(RADIUS_CONTROL))
                .child(icon(Icon::Close, ICON_TINY, ink(theme, CAPTION_TEXT)))
                .on_click(on_dismiss),
        )
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ToastKind {
    Success,
    Error,
    Info,
    Loading,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct ToastId(usize);

#[derive(Clone, Copy)]
enum Clock {
    Held,
    Running(Instant),
    Paused(Duration),
}

#[derive(Clone, Copy)]
struct Track {
    state: SpringState,
    target: f32,
}

impl Track {
    fn at(value: f32) -> Self {
        Track {
            state: SpringState {
                position: value,
                velocity: 0.0,
            },
            target: value,
        }
    }

    fn step(&mut self, spring: SpringConfig, elapsed: f32, snap: bool) -> bool {
        self.state = spring.step(self.state, self.target, elapsed);
        let settled = snap || spring.is_settled(self.state, self.target, TOAST_SETTLED);
        if settled {
            *self = Track::at(self.target);
        }
        !settled
    }

    fn value(&self) -> f32 {
        self.state.position
    }
}

struct Toast {
    id: ToastId,
    kind: ToastKind,
    message: SharedString,
    clock: Clock,
    leaving: bool,
    lift: Track,
    shift: Track,
    scale: Track,
    opacity: Track,
    face: Track,
    mark: Track,
}

impl Toast {
    fn fall(&mut self) {
        self.lift.target -= TOAST_HEIGHT;
        self.leave();
    }

    fn leave(&mut self) {
        self.leaving = true;
        self.clock = Clock::Held;
        self.opacity.target = 0.0;
    }
}

struct Swipe {
    id: ToastId,
    origin: f32,
    shift: f32,
    velocity: f32,
    at: Instant,
}

fn visual_spring(span: Duration) -> SpringConfig {
    let frequency = TAU / (VISUAL_TO_PERIOD * span.as_secs_f32());
    SpringConfig::new(frequency * frequency, 2.0 * frequency, 1.0)
}

pub struct Toaster {
    toasts: Vec<Toast>,
    issued: usize,
    expanded: bool,
    swipe: Option<Swipe>,
    ticked: Option<Instant>,
    wake: Option<Task<gpui::Result<()>>>,
}

impl Toaster {
    pub fn new(cx: &mut App) -> Entity<Toaster> {
        cx.new(|_| Toaster {
            toasts: Vec::new(),
            issued: 0,
            expanded: false,
            swipe: None,
            ticked: None,
            wake: None,
        })
    }

    pub fn show(
        &mut self,
        kind: ToastKind,
        message: impl Into<SharedString>,
        cx: &mut Context<Self>,
    ) -> ToastId {
        let id = ToastId(self.issued);
        self.issued += 1;
        self.toasts.push(Toast {
            id,
            kind,
            message: message.into(),
            clock: self.clock(kind),
            leaving: false,
            lift: Track::at(-TOAST_HEIGHT),
            shift: Track::at(0.0),
            scale: Track::at(1.0),
            opacity: Track::at(0.0),
            face: Track::at(1.0),
            mark: Track::at(1.0),
        });
        self.settle(cx);
        id
    }

    pub fn resolve(
        &mut self,
        id: ToastId,
        kind: ToastKind,
        message: impl Into<SharedString>,
        cx: &mut Context<Self>,
    ) {
        let clock = self.clock(kind);
        if let Some(toast) = self.live(id) {
            toast.kind = kind;
            toast.message = message.into();
            toast.clock = clock;
            toast.mark = Track {
                target: 1.0,
                ..Track::at(0.0)
            };
        }
        self.settle(cx);
    }

    pub fn dismiss(&mut self, id: ToastId, cx: &mut Context<Self>) {
        if let Some(toast) = self.live(id) {
            toast.fall();
        }
        self.settle(cx);
    }

    fn live(&mut self, id: ToastId) -> Option<&mut Toast> {
        self.toasts
            .iter_mut()
            .find(|toast| toast.id == id && !toast.leaving)
    }

    fn clock(&self, kind: ToastKind) -> Clock {
        match (kind, self.expanded) {
            (ToastKind::Loading, _) => Clock::Held,
            (_, true) => Clock::Paused(TOAST_LIFETIME),
            (_, false) => Clock::Running(Instant::now() + TOAST_LIFETIME),
        }
    }

    fn hover(&mut self, inside: bool, cx: &mut Context<Self>) {
        let now = Instant::now();
        self.expanded = inside;
        for toast in &mut self.toasts {
            toast.clock = match (toast.clock, inside) {
                (Clock::Running(until), true) => {
                    Clock::Paused(until.saturating_duration_since(now))
                }
                (Clock::Paused(left), false) => Clock::Running(now + left),
                (clock, _) => clock,
            };
        }
        self.settle(cx);
    }

    fn settle(&mut self, cx: &mut Context<Self>) {
        let now = Instant::now();
        for toast in &mut self.toasts {
            if matches!(toast.clock, Clock::Running(until) if until <= now) {
                toast.fall();
            }
        }
        let expanded = self.expanded;
        for (depth, toast) in self
            .toasts
            .iter_mut()
            .rev()
            .filter(|toast| !toast.leaving)
            .enumerate()
        {
            let behind = depth as f32;
            let (lift, scale) = if expanded {
                (behind * (TOAST_HEIGHT + STACK_GAP), 1.0)
            } else {
                (behind * STACK_GAP, 1.0 - behind * STACK_SHRINK)
            };
            toast.lift.target = lift;
            toast.scale.target = scale;
            toast.opacity.target = if depth < STACK_VISIBLE { 1.0 } else { 0.0 };
            toast.face.target = if expanded || depth == 0 { 1.0 } else { 0.0 };
        }
        let next = self
            .toasts
            .iter()
            .filter_map(|toast| match toast.clock {
                Clock::Running(until) => Some(until),
                Clock::Held | Clock::Paused(_) => None,
            })
            .min();
        self.wake = next.map(|until| {
            cx.spawn(async move |this, cx| {
                cx.background_executor()
                    .timer(until.saturating_duration_since(Instant::now()))
                    .await;
                this.update(cx, |toaster, cx| toaster.settle(cx))
            })
        });
        cx.notify();
    }

    fn grab(&mut self, id: ToastId, x: f32) {
        self.swipe = Some(Swipe {
            id,
            origin: x,
            shift: 0.0,
            velocity: 0.0,
            at: Instant::now(),
        });
    }

    fn drag(&mut self, x: f32) -> bool {
        let Some(swipe) = self.swipe.as_mut() else {
            return false;
        };
        let now = Instant::now();
        let pulled = x - swipe.origin;
        let shift = pulled.max(pulled * SWIPE_RESIST);
        let elapsed = now.saturating_duration_since(swipe.at).as_secs_f32();
        if elapsed > 0.0 {
            swipe.velocity = (shift - swipe.shift) / elapsed;
        }
        swipe.shift = shift;
        swipe.at = now;
        let id = swipe.id;
        if let Some(toast) = self.live(id) {
            toast.shift = Track::at(shift);
        }
        true
    }

    fn let_go(&mut self, cx: &mut Context<Self>) {
        let Some(swipe) = self.swipe.take() else {
            return;
        };
        let fresh = swipe.at.elapsed() < SWIPE_STALE;
        let velocity = if fresh { swipe.velocity } else { 0.0 };
        if let Some(toast) = self.live(swipe.id) {
            toast.shift.state.velocity = velocity;
            if swipe.shift >= SWIPE_DISTANCE || velocity >= SWIPE_FLING {
                toast.shift.target = TOAST_WIDTH;
                toast.leave();
            } else {
                toast.shift.target = 0.0;
            }
        }
        self.settle(cx);
    }

    fn tick(&mut self, reduced: bool) -> bool {
        let now = Instant::now();
        let elapsed = self.ticked.map_or(0.0, |ticked| {
            now.saturating_duration_since(ticked).as_secs_f32()
        });
        let (enter, exit) = (visual_spring(PANEL_IN_MS), visual_spring(PANEL_OUT_MS));
        let dragged = self.swipe.as_ref().map(|swipe| swipe.id);
        let mut moving = false;
        for toast in &mut self.toasts {
            let spring = if toast.leaving { exit } else { enter };
            moving |= toast.lift.step(spring, elapsed, reduced);
            moving |= toast.scale.step(spring, elapsed, reduced);
            if dragged != Some(toast.id) {
                moving |= toast.shift.step(spring, elapsed, reduced);
            }
            moving |= toast.opacity.step(spring, elapsed, false);
            moving |= toast.face.step(spring, elapsed, false);
            moving |= toast.mark.step(enter, elapsed, false);
        }
        self.toasts
            .retain(|toast| !toast.leaving || toast.opacity.value() > TOAST_SETTLED);
        if self.toasts.is_empty() {
            self.expanded = false;
        }
        self.ticked = moving.then_some(now);
        moving
    }

    fn card(&self, toast: &Toast, theme: &Theme, cx: &mut Context<Self>) -> Stateful<Div> {
        let scale = toast.scale.value();
        let (width, height) = (TOAST_WIDTH * scale, TOAST_HEIGHT * scale);
        let (inset_x, inset_y) = ((TOAST_WIDTH - width) / 2.0, (TOAST_HEIGHT - height) / 2.0);
        let shadow = theme.color(ColorToken::Shadow);
        let (id, toaster) = (toast.id, cx.entity());
        let mark = match toast.kind {
            ToastKind::Success => glyph(
                Glyph::Check,
                ICON_SMALL,
                theme.color(ColorToken::StatusLive),
            )
            .into_any_element(),
            ToastKind::Error => icon(
                Icon::Close,
                ICON_SMALL,
                theme.color(ColorToken::StatusDanger),
            )
            .into_any_element(),
            ToastKind::Info => {
                icon(Icon::Arrow, ICON_SMALL, ink(theme, CAPTION_TEXT)).into_any_element()
            }
            ToastKind::Loading => spinner(("toast-spinner", id.0), theme).into_any_element(),
        };
        div()
            .id(("toast", id.0))
            .absolute()
            .left(px(inset_x + toast.shift.value()))
            .bottom(px(toast.lift.value() + inset_y))
            .w(px(width))
            .h(px(height))
            .rounded(px(RADIUS_POP))
            .overflow_hidden()
            .bg(opaque_fill(theme))
            .shadow(vec![
                ring(ink(theme, POP_RING)),
                drop(
                    tint(shadow, shadow.alpha * MENU_SHADOW_SHARE),
                    MENU_SHADOW_Y,
                    MENU_SHADOW_BLUR,
                ),
            ])
            .opacity(toast.opacity.value().clamp(0.0, 1.0))
            .text_size(px(FONT_BODY))
            .text_color(ink(theme, T1))
            .on_mouse_down(MouseButton::Left, move |event, _, cx| {
                toaster.update(cx, |toaster, _| toaster.grab(id, event.position.x.as_f32()));
            })
            .child(
                div()
                    .absolute()
                    .left(px(-inset_x))
                    .top(px(-inset_y))
                    .w(px(TOAST_WIDTH))
                    .h(px(TOAST_HEIGHT))
                    .flex()
                    .items_center()
                    .gap_2p5()
                    .pl_3p5()
                    .pr_2p5()
                    .opacity(toast.face.value().clamp(0.0, 1.0))
                    .child(
                        div()
                            .flex()
                            .flex_none()
                            .opacity(toast.mark.value().clamp(0.0, 1.0))
                            .child(mark),
                    )
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .truncate()
                            .child(toast.message.clone()),
                    )
                    .child(
                        control("toast-dismiss", "Dismiss", theme)
                            .size(px(TOAST_DISMISS))
                            .rounded(px(RADIUS_CONTROL))
                            .child(icon(Icon::Close, ICON_TINY, ink(theme, CAPTION_TEXT)))
                            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                                this.dismiss(id, cx);
                            })),
                    ),
            )
    }
}

impl Render for Toaster {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        if self.tick(reduced_motion(cx)) {
            window.request_animation_frame();
        }
        if self.toasts.is_empty() {
            return div().id("toaster").absolute();
        }
        let live = self.toasts.iter().filter(|toast| !toast.leaving).count();
        let tall = if self.expanded {
            live as f32 * (TOAST_HEIGHT + STACK_GAP) - STACK_GAP
        } else {
            TOAST_HEIGHT + live.clamp(1, STACK_VISIBLE).saturating_sub(1) as f32 * STACK_GAP
        };
        let cards: Vec<_> = self
            .toasts
            .iter()
            .map(|toast| self.card(toast, &theme, cx))
            .collect();
        let (mover, releaser) = (cx.entity(), cx.entity());
        div()
            .id("toaster")
            .absolute()
            .right(px(TOAST_RIGHT))
            .bottom(px(TOAST_BOTTOM))
            .w(px(TOAST_WIDTH))
            .h(px(tall.max(TOAST_HEIGHT)))
            .occlude()
            .on_hover(cx.listener(|this, inside: &bool, _, cx| this.hover(*inside, cx)))
            .on_mouse_move_all(move |event, phase, _, _, cx| {
                if phase == DispatchPhase::Capture {
                    mover.update(cx, |toaster, cx| {
                        if toaster.drag(event.position.x.as_f32()) {
                            cx.notify();
                        }
                    });
                }
            })
            .on_mouse_up_all(move |event, phase, _, _, cx| {
                if phase == DispatchPhase::Capture && event.button == MouseButton::Left {
                    releaser.update(cx, |toaster, cx| toaster.let_go(cx));
                }
            })
            .children(cards)
    }
}

enum Opened {
    Closed,
    Below,
    At(Point<Pixels>),
}

enum MenuKey {
    Ignored,
    Handled,
    Close,
    Pick(usize),
}

struct Sub {
    at: usize,
    highlighted: Option<usize>,
}

struct MenuState {
    items: Vec<MenuItem>,
    opened: Opened,
    highlighted: usize,
    chosen: usize,
    opens: usize,
    dismissed_at: Option<Point<Pixels>>,
    focus: FocusHandle,
    return_focus: Option<FocusHandle>,
    sub: Option<Sub>,
    sub_row: Anchor,
    sub_panel: Anchor,
}

impl MenuState {
    fn new(items: Vec<MenuItem>, cx: &mut Context<Self>) -> Self {
        MenuState {
            items,
            opened: Opened::Closed,
            highlighted: 0,
            chosen: 0,
            opens: 0,
            dismissed_at: None,
            focus: cx.focus_handle(),
            return_focus: None,
            sub: None,
            sub_row: Anchor::default(),
            sub_panel: Anchor::default(),
        }
    }

    fn open(&mut self, at: Opened, window: &mut Window, cx: &mut Context<Self>) {
        self.opened = at;
        self.highlighted = first_pick(&self.items, self.chosen);
        self.sub = None;
        self.opens += 1;
        let before = window.focused(cx).filter(|focused| *focused != self.focus);
        self.return_focus = before.or(self.return_focus.take());
        window.focus(&self.focus, cx);
        cx.notify();
    }

    fn close(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.opened = Opened::Closed;
        self.sub = None;
        if let Some(before) = &self.return_focus {
            window.focus(before, cx);
        }
        cx.notify();
    }

    fn dismiss(&mut self, at: Point<Pixels>, cx: &mut Context<Self>) {
        let in_sub = self.sub.is_some() && self.sub_panel.get().contains(&at);
        if !in_sub && !matches!(self.opened, Opened::Closed) {
            self.opened = Opened::Closed;
            self.sub = None;
            self.dismissed_at = Some(at);
            cx.notify();
        }
    }

    fn choose(&mut self, pick: usize, window: &mut Window, cx: &mut Context<Self>) {
        let top = (0..self.items.len()).find(|top| {
            preorder(&self.items, *top) == pick
                && matches!(self.items.get(*top), Some(MenuItem::Action { .. }))
        });
        if let Some(top) = top {
            self.chosen = top;
        }
        self.close(window, cx);
    }

    fn hover(&mut self, ix: usize, cx: &mut Context<Self>) {
        let opens = matches!(self.items.get(ix), Some(MenuItem::Submenu { .. })).then_some(ix);
        if self.highlighted != ix || self.sub.as_ref().map(|sub| sub.at) != opens {
            self.highlighted = ix;
            self.sub = opens.map(|at| Sub {
                at,
                highlighted: None,
            });
            cx.notify();
        }
    }

    fn hover_sub(&mut self, row: usize, cx: &mut Context<Self>) {
        if let Some(sub) = &mut self.sub
            && sub.highlighted != Some(row)
        {
            sub.highlighted = Some(row);
            cx.notify();
        }
    }

    fn key(&mut self, key: &str, cx: &mut Context<Self>) -> MenuKey {
        let inside = self
            .sub
            .as_ref()
            .and_then(|sub| Some((sub.at, sub.highlighted?)));
        if let Some((at, row)) = inside {
            let children = children_of(&self.items, at);
            let next = match key {
                "down" | "up" => Some(step_to(children, row, key == "down")),
                "left" | "escape" => None,
                "enter" => {
                    return MenuKey::Pick(preorder(&self.items, at) + 1 + preorder(children, row));
                }
                _ => return MenuKey::Ignored,
            };
            self.sub = next.map(|row| Sub {
                at,
                highlighted: Some(row),
            });
        } else {
            let current = self.items.get(self.highlighted);
            match key {
                "down" | "up" => {
                    self.highlighted = step_to(&self.items, self.highlighted, key == "down");
                    self.sub = None;
                }
                "right" | "enter" if matches!(current, Some(MenuItem::Submenu { .. })) => {
                    let row = first_pick(children_of(&self.items, self.highlighted), 0);
                    self.sub = Some(Sub {
                        at: self.highlighted,
                        highlighted: Some(row),
                    });
                }
                "enter" if matches!(current, Some(MenuItem::Action { .. })) => {
                    return MenuKey::Pick(preorder(&self.items, self.highlighted));
                }
                "escape" => return MenuKey::Close,
                _ => return MenuKey::Ignored,
            }
        }
        cx.notify();
        MenuKey::Handled
    }
}

type OnPick = Rc<dyn Fn(&usize, &mut Window, &mut App)>;

fn menu_panel(
    state: &Entity<MenuState>,
    trigger: &Anchor,
    placement: Placement,
    on_pick: Option<OnPick>,
    cx: &App,
) -> Vec<AnyElement> {
    let theme = ActiveTheme::theme(cx);
    let menu = state.read(cx);
    let anchor = match menu.opened {
        Opened::Closed => return Vec::new(),
        Opened::Below => trigger.clone(),
        Opened::At(at) => Rc::new(Cell::new(Bounds::new(at, Size::default()))),
    };
    let pick: OnPick = {
        let state = state.clone();
        Rc::new(move |ix: &usize, window: &mut Window, cx: &mut App| {
            state.update(cx, |menu, cx| menu.choose(*ix, window, cx));
            if let Some(on_pick) = &on_pick {
                on_pick(ix, window, cx);
            }
        })
    };
    let fill = ink(&theme, HOVER);
    let corners = Corners::all(px(RADIUS_ROW));
    let marker = Marker {
        at: menu.highlighted,
        kind: GlideKind::Eased,
        fill,
        corners,
    };
    let sub_at = menu.sub.as_ref().map(|sub| sub.at);
    let rows = HoverList::within("menu-rows", div().flex().flex_col(), fill, corners, &theme)
        .backdrop(opaque_fill(&theme))
        .keyed(Some(marker));
    let rows = menu
        .items
        .iter()
        .enumerate()
        .fold(rows, |rows, (ix, item)| {
            match menu_entry(("menu-item", ix), item, &theme) {
                MenuRow::Inert(row) => rows.inert(row),
                MenuRow::Pick(row) => {
                    let (hovered, clicked) = (state.clone(), pick.clone());
                    let at = preorder(&menu.items, ix);
                    let opens = matches!(item, MenuItem::Submenu { .. });
                    rows.item(
                        row.when(sub_at == Some(ix), |row| {
                            row.relative().child(measure(&menu.sub_row))
                        })
                        .on_mouse_move(move |_, _, cx| {
                            hovered.update(cx, |menu, cx| menu.hover(ix, cx));
                        })
                        .when(!opens, |row| {
                            row.on_click(move |_, window, cx| clicked(&at, window, cx))
                        }),
                    )
                }
            }
        });
    let submenu = menu.sub.as_ref().map(|sub| {
        let children = children_of(&menu.items, sub.at);
        let base = preorder(&menu.items, sub.at) + 1;
        let marker = sub.highlighted.map(|at| Marker {
            at,
            kind: GlideKind::Eased,
            fill,
            corners,
        });
        let list = HoverList::within(
            "menu-sub-rows",
            div().flex().flex_col(),
            fill,
            corners,
            &theme,
        )
        .backdrop(opaque_fill(&theme))
        .keyed(marker);
        let list = children.iter().enumerate().fold(list, |list, (row, item)| {
            match menu_entry(("menu-sub-item", row), item, &theme) {
                MenuRow::Inert(entry) => list.inert(entry),
                MenuRow::Pick(entry) => {
                    let (hovered, clicked) = (state.clone(), pick.clone());
                    let at = base + preorder(children, row);
                    list.item(
                        entry
                            .on_mouse_move(move |_, _, cx| {
                                hovered.update(cx, |menu, cx| menu.hover_sub(row, cx));
                            })
                            .on_click(move |_, window, cx| clicked(&at, window, cx)),
                    )
                }
            }
        });
        let panel = menu_surface(&theme)
            .id("menu-sub-panel")
            .relative()
            .w(px(MENU_WIDTH))
            .p(px(MENU_PAD))
            .rounded(px(RADIUS_POP))
            .overflow_hidden()
            .child(measure(&menu.sub_panel))
            .child(list);
        let beside = Placement {
            side: Side::Right,
            align: Align::Start,
            offset: SUBMENU_OFFSET,
        };
        open_beside(
            ("menu-sub", sub.at),
            menu.sub_row.clone(),
            beside,
            panel,
            cx,
        )
    });
    let (keys, enter, outside) = (state.clone(), pick.clone(), state.clone());
    let panel = menu_surface(&theme)
        .id("menu-panel")
        .track_focus(&menu.focus)
        .w(px(MENU_WIDTH))
        .p(px(MENU_PAD))
        .rounded(px(RADIUS_POP))
        .overflow_hidden()
        .on_key_down(move |event: &KeyDownEvent, window, cx| {
            match keys.update(cx, |menu, cx| menu.key(&event.keystroke.key, cx)) {
                MenuKey::Ignored => return,
                MenuKey::Handled => {}
                MenuKey::Close => keys.update(cx, |menu, cx| menu.close(window, cx)),
                MenuKey::Pick(ix) => enter(&ix, window, cx),
            }
            cx.stop_propagation();
        })
        .on_mouse_down_out(move |event: &MouseDownEvent, _, cx| {
            outside.update(cx, |menu, cx| menu.dismiss(event.position, cx));
        })
        .child(rows);
    std::iter::once(open_beside(
        ("menu-open", menu.opens),
        anchor,
        placement,
        panel,
        cx,
    ))
    .chain(submenu)
    .collect()
}

fn opener(id: &'static str, label: SharedString, open: bool, theme: &Theme) -> Stateful<Div> {
    let lit = ink(theme, CHIP_FILL + BADGE_FILL);
    chip(label, Some(Glyph::Chevron), theme)
        .id(id)
        .cursor_pointer()
        .when(open, |trigger| trigger.bg(lit))
        .hover(move |style| style.bg(lit))
}

fn opens_menu(trigger: Stateful<Div>, menu: &Entity<MenuState>, anchor: &Anchor) -> Stateful<Div> {
    let toggle = menu.clone();
    trigger.relative().child(measure(anchor)).on_mouse_down(
        MouseButton::Left,
        move |event, window, cx| {
            toggle.update(cx, |menu, cx| {
                if menu.dismissed_at.take() != Some(event.position) {
                    menu.open(Opened::Below, window, cx);
                }
            });
        },
    )
}

type BuildTrigger = Rc<dyn Fn(bool, &Theme) -> Stateful<Div>>;

pub struct MenuButton {
    menu: Entity<MenuState>,
    label: SharedString,
    trigger: Option<BuildTrigger>,
    placement: Placement,
    on_pick: Option<OnPick>,
}

impl MenuButton {
    pub fn new(label: SharedString, items: Vec<MenuItem>, cx: &mut App) -> Entity<MenuButton> {
        cx.new(|cx| {
            let menu = cx.new(|cx| MenuState::new(items, cx));
            cx.observe(&menu, |_, _, cx| cx.notify()).detach();
            MenuButton {
                menu,
                label,
                trigger: None,
                placement: Placement::below(),
                on_pick: None,
            }
        })
    }

    pub fn trigger(&mut self, trigger: impl Fn(bool, &Theme) -> Stateful<Div> + 'static) {
        self.trigger = Some(Rc::new(trigger));
    }

    pub fn items(&mut self, items: Vec<MenuItem>, cx: &mut Context<Self>) {
        self.menu.update(cx, |menu, cx| {
            if menu.items != items {
                menu.items = items;
                menu.chosen = 0;
                cx.notify();
            }
        });
    }

    pub fn placement(&mut self, placement: Placement) {
        self.placement = placement;
    }

    pub fn on_pick(&mut self, on_pick: impl Fn(&usize, &mut Window, &mut App) + 'static) {
        self.on_pick = Some(Rc::new(on_pick));
    }
}

impl Render for MenuButton {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let anchor = Anchor::default();
        let open = !matches!(self.menu.read(cx).opened, Opened::Closed);
        let trigger = match &self.trigger {
            Some(build) => build(open, &theme),
            None => opener("menu-trigger", self.label.clone(), open, &theme),
        };
        div()
            .flex()
            .flex_col()
            .items_start()
            .child(opens_menu(trigger, &self.menu, &anchor))
            .children(menu_panel(
                &self.menu,
                &anchor,
                self.placement,
                self.on_pick.clone(),
                cx,
            ))
    }
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum DropdownTrigger {
    #[default]
    Chip,
    Flat,
}

pub struct Dropdown {
    menu: Entity<MenuState>,
    trigger: DropdownTrigger,
}

impl Dropdown {
    pub fn new(items: Vec<MenuItem>, cx: &mut App) -> Entity<Dropdown> {
        cx.new(|cx| {
            let menu = cx.new(|cx| MenuState::new(items, cx));
            cx.observe(&menu, |_, _, cx| cx.notify()).detach();
            Dropdown {
                menu,
                trigger: DropdownTrigger::default(),
            }
        })
    }

    pub fn trigger(&mut self, trigger: DropdownTrigger) {
        self.trigger = trigger;
    }

    pub fn chosen(&self, cx: &App) -> usize {
        self.menu.read(cx).chosen
    }
}

impl Render for Dropdown {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let menu = self.menu.read(cx);
        let label = match menu.items.get(menu.chosen) {
            Some(MenuItem::Action { label, .. }) => label.clone(),
            Some(MenuItem::Separator | MenuItem::Caption(_) | MenuItem::Submenu { .. }) | None => {
                SharedString::default()
            }
        };
        let anchor = Anchor::default();
        let open = !matches!(menu.opened, Opened::Closed);
        let trigger = match self.trigger {
            DropdownTrigger::Chip => opener("dropdown-trigger", label, open, &theme),
            DropdownTrigger::Flat => flat_chip("dropdown-trigger", label, &theme),
        };
        div()
            .flex()
            .flex_col()
            .items_start()
            .child(opens_menu(trigger, &self.menu, &anchor))
            .children(menu_panel(
                &self.menu,
                &anchor,
                Placement::below(),
                None,
                cx,
            ))
    }
}

pub fn context_menu(items: Vec<MenuItem>) -> ContextMenu {
    ContextMenu {
        id: ElementId::Name("context-menu".into()),
        items,
        children: Vec::new(),
        on_pick: None,
        open_at: None,
    }
}

#[derive(IntoElement)]
pub struct ContextMenu {
    id: ElementId,
    items: Vec<MenuItem>,
    children: Vec<AnyElement>,
    on_pick: Option<OnPick>,
    open_at: Option<Point<Pixels>>,
}

impl ContextMenu {
    pub fn id(mut self, id: impl Into<ElementId>) -> Self {
        self.id = id.into();
        self
    }

    pub fn open_at(mut self, at: Option<Point<Pixels>>) -> Self {
        self.open_at = at;
        self
    }

    pub fn on_pick(mut self, on_pick: impl Fn(&usize, &mut Window, &mut App) + 'static) -> Self {
        self.on_pick = Some(Rc::new(on_pick));
        self
    }
}

impl ParentElement for ContextMenu {
    fn extend(&mut self, elements: impl IntoIterator<Item = AnyElement>) {
        self.children.extend(elements);
    }
}

impl RenderOnce for ContextMenu {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let state =
            window.use_keyed_state(self.id.clone(), cx, |_, cx| MenuState::new(Vec::new(), cx));
        let items = self.items;
        state.update(cx, |menu, _| menu.items = items);
        if let Some(at) = self.open_at {
            state.update(cx, |menu, cx| menu.open(Opened::At(at), window, cx));
        }
        let opener = state.clone();
        div()
            .id(self.id)
            .on_mouse_down(MouseButton::Right, move |event, window, cx| {
                opener.update(cx, |menu, cx| {
                    menu.open(Opened::At(event.position), window, cx)
                });
            })
            .children(self.children)
            .children(menu_panel(
                &state,
                &Anchor::default(),
                Placement::below(),
                self.on_pick,
                cx,
            ))
    }
}
