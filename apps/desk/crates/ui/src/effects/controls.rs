use std::rc::Rc;

use gpui::{
    AnyElement, App, Bounds, ClickEvent, CursorStyle, Div, ElementId, Entity, FontWeight,
    HitboxBehavior, Hsla, MouseButton, MouseDownEvent, MouseMoveEvent, MouseUpEvent, Pixels, Rgba,
    SharedString, Stateful, Window, canvas, div, fill, hsla_to_rgba, point, prelude::*, px,
    relative, size,
};

use crate::components::color_picker::{self, ColorPicker};
use crate::components::form::switch_bare;
use crate::components::glyph::Glyph;
use crate::components::overlay::Dismissed;
use crate::components::paint::{glyph, ink, ring};
use crate::components::size::{FONT_SMALL, RADIUS_ROW, T1, T2, T3};
use crate::metrics::ICON_SMALL;
use crate::theme::Theme;

const TILE: f32 = 40.0;
const TILE_GAP: f32 = 6.0;
const TILE_PAD: f32 = 12.0;
const TILE_FILL: f32 = 0.05;
const BAND_FILL: f32 = 0.09;
const HANDLE_WIDTH: f32 = 2.0;
const HANDLE_TALL: f32 = 18.0;
const TICKS: usize = 10;
const TICK_TALL: f32 = 6.0;
const TICK_INK: f32 = 0.1;
const VALUE_INK: f32 = 0.8;
const PANEL_PAD: f32 = 10.0;
const SWATCH_GAP: f32 = 6.0;
const SWATCH_TALL: f32 = 26.0;
const SWATCH_RING: f32 = 0.14;
const STEP_BUTTON: f32 = 20.0;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Unit {
    Px,
    Percent,
    Times,
    Seconds,
    Count,
    Degrees,
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Dial {
    pub label: &'static str,
    pub value: f32,
    pub least: f32,
    pub most: f32,
    pub step: f32,
    pub unit: Unit,
}

impl Dial {
    fn share(&self) -> f32 {
        let span = self.most - self.least;
        if span <= 0.0 {
            return 0.0;
        }
        ((self.value - self.least) / span).clamp(0.0, 1.0)
    }

    fn at(&self, x: Pixels, bounds: Bounds<Pixels>) -> f32 {
        let width = f32::from(bounds.size.width);
        let share = if width <= 0.0 {
            0.0
        } else {
            (f32::from(x - bounds.origin.x) / width).clamp(0.0, 1.0)
        };
        let raw = self.least + share * (self.most - self.least);
        let stepped = if self.step > 0.0 {
            ((raw - self.least) / self.step).round() * self.step + self.least
        } else {
            raw
        };
        stepped.clamp(self.least, self.most)
    }

    fn shown(&self) -> String {
        match self.unit {
            Unit::Px => format!("{:.0}px", self.value),
            Unit::Percent => format!("{:.0}%", self.value * 100.0),
            Unit::Times => format!("{:.2}x", self.value),
            Unit::Seconds => format!("{:.1}s", self.value),
            Unit::Count => format!("{:.0}", self.value),
            Unit::Degrees => format!("{:.0}\u{b0}", self.value),
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Settle {
    Moving,
    Settled,
}

pub type Act = Rc<dyn Fn(&ClickEvent, &mut Window, &mut App)>;
type Change = Rc<dyn Fn(f32, Settle, &mut Window, &mut App)>;

pub fn section(tiles: Div) -> Div {
    tiles.w_full().pt(px(TILE_GAP)).pb(px(PANEL_PAD))
}

pub fn grid() -> Div {
    div()
        .grid()
        .grid_cols(1)
        .gap(px(TILE_GAP))
        .text_size(px(FONT_SMALL))
}

fn tile(theme: &Theme) -> Div {
    div()
        .relative()
        .h(px(TILE))
        .min_w_0()
        .flex()
        .items_center()
        .gap_2()
        .px(px(TILE_PAD))
        .rounded(px(RADIUS_ROW))
        .overflow_hidden()
        .bg(ink(theme, TILE_FILL))
}

fn label(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .flex_1()
        .min_w_0()
        .truncate()
        .text_color(ink(theme, T2))
        .child(text.into())
}

pub fn field_tile(name: &'static str, field: impl IntoElement, theme: &Theme) -> Div {
    tile(theme)
        .col_span_full()
        .child(div().flex_none().text_color(ink(theme, T2)).child(name))
        .child(div().flex_1().min_w_0().child(field))
}

pub fn slider_tile(
    id: impl Into<ElementId>,
    dial: Dial,
    theme: &Theme,
    on_change: impl Fn(f32, Settle, &mut Window, &mut App) + 'static,
) -> Div {
    let key: ElementId = id.into();
    let share = dial.share();
    let band = ink(theme, BAND_FILL);
    let tick = ink(theme, TICK_INK);
    let handle = ink(theme, T2);
    let changed: Change = Rc::new(on_change);
    let listen = canvas(
        move |bounds, window, cx| {
            let held = window.use_keyed_state(key, cx, |_, _| false);
            (window.insert_hitbox(bounds, HitboxBehavior::Normal), held)
        },
        move |bounds, (hitbox, held), window, _| {
            window.set_cursor_style(CursorStyle::PointingHand, &hitbox);
            let tall = f32::from(bounds.size.height);
            let width = f32::from(bounds.size.width);
            window.paint_quad(fill(
                Bounds::new(bounds.origin, size(px(width * share), bounds.size.height)),
                band,
            ));
            for at in 1..TICKS {
                let x = width * at as f32 / TICKS as f32;
                if x <= width * share {
                    continue;
                }
                window.paint_quad(fill(
                    Bounds::new(
                        bounds.origin + point(px(x), px((tall - TICK_TALL) / 2.0)),
                        size(px(1.0), px(TICK_TALL)),
                    ),
                    tick,
                ));
            }
            window.paint_quad(fill(
                Bounds::new(
                    bounds.origin
                        + point(
                            px((width * share - HANDLE_WIDTH).max(0.0)),
                            px((tall - HANDLE_TALL) / 2.0),
                        ),
                    size(px(HANDLE_WIDTH), px(HANDLE_TALL)),
                ),
                handle,
            ));
            let (pressed, moved, released) = (held.clone(), held.clone(), held);
            let (start, drag, end) = (changed.clone(), changed.clone(), changed.clone());
            window.on_mouse_event(move |event: &MouseDownEvent, phase, window, cx| {
                if phase.bubble() && event.button == MouseButton::Left && hitbox.is_hovered(window)
                {
                    pressed.update(cx, |held, _| *held = true);
                    start(
                        dial.at(event.position.x, bounds),
                        Settle::Moving,
                        window,
                        cx,
                    );
                }
            });
            window.on_mouse_event(move |event: &MouseMoveEvent, phase, window, cx| {
                if phase.bubble() && *moved.read(cx) {
                    drag(
                        dial.at(event.position.x, bounds),
                        Settle::Moving,
                        window,
                        cx,
                    );
                }
            });
            window.on_mouse_event(move |event: &MouseUpEvent, phase, window, cx| {
                if phase.bubble() && event.button == MouseButton::Left && *released.read(cx) {
                    released.update(cx, |held, _| *held = false);
                    end(
                        dial.at(event.position.x, bounds),
                        Settle::Settled,
                        window,
                        cx,
                    );
                }
            });
        },
    )
    .absolute()
    .inset_0();
    tile(theme)
        .child(listen)
        .child(label(dial.label, theme))
        .child(
            div()
                .flex_none()
                .font_weight(FontWeight::BOLD)
                .text_color(ink(theme, VALUE_INK))
                .child(dial.shown()),
        )
}

pub fn picker_tile(
    id: impl Into<ElementId>,
    name: &'static str,
    value: impl Into<SharedString>,
    theme: &Theme,
) -> Stateful<Div> {
    tile(theme)
        .w_full()
        .id(id)
        .cursor_pointer()
        .child(label(name, theme))
        .child(
            div()
                .flex_none()
                .font_weight(FontWeight::MEDIUM)
                .text_color(ink(theme, T1))
                .child(value.into()),
        )
        .child(glyph(Glyph::Chevron, ICON_SMALL, ink(theme, T3)))
}

pub fn switch_tile(
    id: impl Into<ElementId>,
    name: &'static str,
    on: bool,
    theme: &Theme,
) -> Stateful<Div> {
    tile(theme)
        .id(id)
        .cursor_pointer()
        .child(label(name, theme))
        .child(switch_bare("switch", name, on, theme))
}

pub fn swatch(id: impl Into<ElementId>, color: Rgba, theme: &Theme) -> Stateful<Div> {
    div()
        .id(id)
        .w_full()
        .h(px(SWATCH_TALL))
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .bg(color)
        .shadow(vec![ring(ink(theme, SWATCH_RING))])
}

pub fn swatch_editor(
    at: usize,
    color: [u8; 3],
    picker: Option<Entity<ColorPicker>>,
    theme: &Theme,
    open: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
    close: impl Fn(&Dismissed, &mut Window, &mut App) + 'static,
) -> AnyElement {
    let shown = swatch(("swatch", at), to_rgba(color), theme).on_click(open);
    color_picker::popover(
        ("swatch-pop", at),
        div().w_full().child(shown),
        picker,
        close,
    )
    .into_any_element()
}

pub struct Stops {
    pub fewer: Option<Act>,
    pub more: Option<Act>,
}

pub fn to_rgba(color: [u8; 3]) -> Rgba {
    let [r, g, b] = color.map(|c| f32::from(c) / 255.0);
    Rgba::new(r, g, b, 1.0)
}

pub fn from_hsla(color: Hsla) -> [u8; 3] {
    let rgba = hsla_to_rgba(color);
    [rgba.red, rgba.green, rgba.blue].map(|c| (c.clamp(0.0, 1.0) * 255.0).round() as u8)
}

pub fn colors_tile(
    name: &'static str,
    stops: Option<Stops>,
    swatches: Vec<AnyElement>,
    theme: &Theme,
) -> Div {
    let step = |id: &'static str, mark: &'static str, act: Option<Act>| {
        let live = act.is_some();
        div()
            .id(id)
            .flex()
            .flex_none()
            .items_center()
            .justify_center()
            .size(px(STEP_BUTTON))
            .rounded(px(RADIUS_ROW))
            .text_color(ink(theme, if live { T1 } else { T3 }))
            .when_some(act, |button, act| {
                button
                    .cursor_pointer()
                    .on_click(move |event, window, cx| act(event, window, cx))
            })
            .child(mark)
    };
    tile(theme)
        .row_span(2)
        .h(px(2.0 * TILE + TILE_GAP))
        .flex_col()
        .items_stretch()
        .justify_center()
        .gap(px(SWATCH_GAP))
        .child(
            div()
                .flex()
                .items_center()
                .gap_1()
                .child(label(name, theme))
                .when_some(stops, |row, stops| {
                    row.child(step("colors-fewer", "\u{2212}", stops.fewer))
                        .child(step("colors-more", "+", stops.more))
                }),
        )
        .child(
            div().flex().gap(px(SWATCH_GAP)).w(relative(1.0)).children(
                swatches
                    .into_iter()
                    .map(|swatch| div().flex_1().min_w_0().flex().flex_col().child(swatch)),
            ),
        )
}
