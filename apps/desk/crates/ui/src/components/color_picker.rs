use std::cell::Cell;
use std::rc::Rc;

use gpui::{
    App, Bounds, ColorExt, Context, DispatchPhase, Div, ElementId, Entity, EntityInputHandler,
    EventEmitter, FocusHandle, Focusable, Hsla, KeyDownEvent, MouseButton, MouseDownEvent,
    MouseMoveEvent, MouseUpEvent, Pixels, Point, Rgba, Subscription, Window,
    canvas, div, fill, hsla, hsla_to_rgba, linear_color_stop, linear_gradient, point, prelude::*,
    px, relative, rgb_to_hsla, size, transparent_black, white,
};

use crate::components::form::TextInput;
use crate::components::overlay::{Dismissed, Popover};
use crate::components::size::{FIELD, RADIUS_POP, RADIUS_ROW};
use crate::live::ActiveTheme;
use crate::theme::ColorToken;

const CARD_WIDTH: f32 = 240.0;
const CARD_PAD: f32 = 12.0;
const CARD_GAP: f32 = 10.0;
const SQUARE_HEIGHT: f32 = 150.0;
const BAR_HEIGHT: f32 = 12.0;
const HANDLE: f32 = 14.0;
const HANDLE_RING: f32 = 2.0;
const CHECKER: f32 = 6.0;
const BAR_CELLS: usize = 36;
const SWATCH_CELLS: usize = 5;
const HUE_STOPS: usize = 6;
const CARD_HEIGHT: f32 = 2.0 * CARD_PAD + SQUARE_HEIGHT + 2.0 * BAR_HEIGHT + FIELD + 3.0 * CARD_GAP;

pub fn popover(
    id: impl Into<ElementId>,
    trigger: impl ParentElement + Styled + IntoElement,
    open: Option<Entity<ColorPicker>>,
    close: impl Fn(&Dismissed, &mut Window, &mut App) + 'static,
) -> Popover {
    Popover::new(id, trigger)
        .open(open.is_some())
        .fit(CARD_WIDTH)
        .on_dismiss(close)
        .children(open.map(|picker| div().w(px(CARD_WIDTH)).h(px(CARD_HEIGHT)).child(picker)))
}

pub enum ColorPicked {
    Moving(Hsla),
    Settled(Hsla),
}

#[derive(Clone, Copy)]
struct Hsv {
    h: f32,
    s: f32,
    v: f32,
    a: f32,
}

#[derive(Clone, Copy)]
enum Track {
    Square,
    Hue,
    Alpha,
}

pub struct ColorPicker {
    hsv: Hsv,
    input: Entity<TextInput>,
    focus: FocusHandle,
    shown: String,
    stale_field: bool,
    invalid: bool,
    unsettled: bool,
    drag: Option<Track>,
    square: Rc<Cell<Bounds<Pixels>>>,
    hue: Rc<Cell<Bounds<Pixels>>>,
    alpha: Rc<Cell<Bounds<Pixels>>>,
    _watch: [Subscription; 2],
}

impl EventEmitter<ColorPicked> for ColorPicker {}

impl ColorPicker {
    pub fn new(initial: Hsla, window: &mut Window, cx: &mut Context<Self>) -> Self {
        let input = TextInput::new("#rrggbb".into(), window, cx);
        let typed = cx.observe(&input, |this, input, cx| this.typed(&input, cx));
        let left = cx.on_blur(&input.focus_handle(cx), window, |this, _, cx| {
            this.settle(cx)
        });
        let focus = cx.focus_handle();
        window.focus(&focus, cx);
        let mut picker = ColorPicker {
            hsv: to_hsv(initial, 0.0),
            input,
            focus,
            shown: String::new(),
            stale_field: false,
            invalid: false,
            unsettled: false,
            drag: None,
            square: Rc::default(),
            hue: Rc::default(),
            alpha: Rc::default(),
            _watch: [typed, left],
        };
        picker.write_field(window, cx);
        picker
    }

    pub fn set(&mut self, color: Hsla, cx: &mut Context<Self>) {
        self.hsv = to_hsv(color, self.hsv.h);
        self.invalid = false;
        self.stale_field = true;
        cx.notify();
    }

    pub fn color(&self) -> Hsla {
        from_hsv(self.hsv)
    }

    fn typed(&mut self, input: &Entity<TextInput>, cx: &mut Context<Self>) {
        let text = input.read(cx).text();
        if text == self.shown {
            return;
        }
        let text = text.to_owned();
        match parse_color(&text) {
            Ok(color) => {
                self.hsv = to_hsv(color, self.hsv.h);
                self.invalid = false;
                self.unsettled = true;
                cx.emit(ColorPicked::Moving(color));
            }
            Err(_) => self.invalid = true,
        }
        self.shown = text;
        cx.notify();
    }

    fn settle(&mut self, cx: &mut Context<Self>) {
        if std::mem::take(&mut self.unsettled) {
            cx.emit(ColorPicked::Settled(self.color()));
        }
    }

    fn entered(&mut self, event: &KeyDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        if event.keystroke.key == "enter" {
            self.settle(cx);
        }
    }

    fn write_field(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let hex = to_hex(self.color());
        self.shown.clone_from(&hex);
        self.stale_field = false;
        self.input.update(cx, |input, cx| {
            let all = input.text().encode_utf16().count();
            input.replace_text_in_range(Some(0..all), &hex, window, cx);
        });
    }

    fn start(
        &mut self,
        track: Track,
        at: Point<Pixels>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.drag = Some(track);
        self.drag_to(at, window, cx);
    }

    fn drag_to(&mut self, at: Point<Pixels>, window: &mut Window, cx: &mut Context<Self>) {
        let Some(track) = self.drag else {
            return;
        };
        match track {
            Track::Square => {
                let bounds = self.square.get();
                self.hsv.s = fraction(at.x - bounds.origin.x, bounds.size.width);
                self.hsv.v = 1.0 - fraction(at.y - bounds.origin.y, bounds.size.height);
            }
            Track::Hue => {
                let bounds = self.hue.get();
                self.hsv.h = fraction(at.x - bounds.origin.x, bounds.size.width);
            }
            Track::Alpha => {
                let bounds = self.alpha.get();
                self.hsv.a = fraction(at.x - bounds.origin.x, bounds.size.width);
            }
        }
        self.invalid = false;
        self.write_field(window, cx);
        cx.emit(ColorPicked::Moving(self.color()));
        cx.notify();
    }

    fn track(
        &self,
        track: Track,
        bounds: &Rc<Cell<Bounds<Pixels>>>,
        cx: &mut Context<Self>,
    ) -> Div {
        let bounds = bounds.clone();
        div()
            .relative()
            .w_full()
            .cursor_pointer()
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(move |this, event: &MouseDownEvent, window, cx| {
                    this.start(track, event.position, window, cx);
                }),
            )
            .child(
                canvas(move |area, _, _| bounds.set(area), |_, (), _, _| {})
                    .absolute()
                    .size_full(),
            )
    }

    fn drag_listener(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let moved = cx.entity().downgrade();
        let released = moved.clone();
        canvas(
            |_, _, _| {},
            move |_, (), window, _| {
                window.on_mouse_event(
                    move |event: &MouseMoveEvent, phase, window, cx: &mut App| {
                        if phase == DispatchPhase::Bubble {
                            moved
                                .update(cx, |this, cx| this.drag_to(event.position, window, cx))
                                .ok();
                        }
                    },
                );
                window.on_mouse_event(move |_: &MouseUpEvent, phase, _, cx: &mut App| {
                    if phase == DispatchPhase::Bubble {
                        released
                            .update(cx, |this, cx| {
                                if this.drag.take().is_some() {
                                    this.unsettled = true;
                                }
                                this.settle(cx);
                                cx.notify();
                            })
                            .ok();
                    }
                });
            },
        )
        .absolute()
        .size_full()
    }
}

impl Render for ColorPicker {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if self.stale_field {
            self.write_field(window, cx);
        }
        let theme = ActiveTheme::theme(cx);
        let color = self.color();
        let opaque = from_hsv(Hsv { a: 1.0, ..self.hsv });
        let pure = hsla(self.hsv.h, 1.0, 0.5, 1.0);
        let square = self
            .track(Track::Square, &self.square, cx)
            .h(px(SQUARE_HEIGHT))
            .child(
                div()
                    .absolute()
                    .size_full()
                    .rounded(px(RADIUS_ROW))
                    .overflow_hidden()
                    .bg(pure)
                    .child(div().absolute().size_full().bg(linear_gradient(
                        90.0,
                        linear_color_stop(white(), 0.0),
                        linear_color_stop(white().opacity(0.0), 1.0),
                    )))
                    .child(div().absolute().size_full().bg(linear_gradient(
                        180.0,
                        linear_color_stop(transparent_black(), 0.0),
                        linear_color_stop(hsla(0.0, 0.0, 0.0, 1.0), 1.0),
                    ))),
            )
            .child(handle(self.hsv.s, 1.0 - self.hsv.v, SQUARE_HEIGHT, opaque));
        let hue = self
            .track(Track::Hue, &self.hue, cx)
            .h(px(BAR_HEIGHT))
            .child(
                div()
                    .absolute()
                    .size_full()
                    .flex()
                    .rounded_full()
                    .overflow_hidden()
                    .children((0..HUE_STOPS).map(|stop| {
                        let at = |n: usize| hsla(n as f32 / HUE_STOPS as f32, 1.0, 0.5, 1.0);
                        div().flex_1().h_full().bg(linear_gradient(
                            90.0,
                            linear_color_stop(at(stop), 0.0),
                            linear_color_stop(at(stop + 1), 1.0),
                        ))
                    })),
            )
            .child(handle(self.hsv.h, 0.5, BAR_HEIGHT, pure));
        let alpha = self
            .track(Track::Alpha, &self.alpha, cx)
            .h(px(BAR_HEIGHT))
            .child(
                checker(BAR_CELLS, 2)
                    .absolute()
                    .size_full()
                    .rounded_full()
                    .child(div().absolute().size_full().bg(linear_gradient(
                        90.0,
                        linear_color_stop(opaque.opacity(0.0), 0.0),
                        linear_color_stop(opaque, 1.0),
                    ))),
            )
            .child(handle(self.hsv.a, 0.5, BAR_HEIGHT, color));
        let field = div()
            .flex_1()
            .h(px(FIELD))
            .rounded(px(RADIUS_ROW))
            .overflow_hidden()
            .border_1()
            .border_color(if self.invalid {
                theme.color(ColorToken::StatusDanger)
            } else {
                theme.color(ColorToken::FieldFill)
            })
            .on_key_down(cx.listener(Self::entered))
            .child(self.input.clone());
        let swatch = checker(SWATCH_CELLS, SWATCH_CELLS)
            .flex_none()
            .size(px(FIELD))
            .rounded(px(RADIUS_ROW))
            .child(div().absolute().size_full().bg(color));
        div()
            .track_focus(&self.focus)
            .relative()
            .flex()
            .flex_col()
            .gap(px(CARD_GAP))
            .w(px(CARD_WIDTH))
            .p(px(CARD_PAD))
            .rounded(px(RADIUS_POP))
            .bg(theme.color(ColorToken::CardsInnerFill))
            .child(square)
            .child(hue)
            .child(alpha)
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(CARD_GAP))
                    .child(field)
                    .child(swatch),
            )
            .when(self.drag.is_some(), |card| {
                card.child(self.drag_listener(cx))
            })
    }
}

fn handle(x: f32, y: f32, height: f32, fill: Hsla) -> Div {
    div()
        .absolute()
        .left(relative(x))
        .top(px(y * height))
        .ml(px(-HANDLE / 2.0))
        .mt(px(-HANDLE / 2.0))
        .size(px(HANDLE))
        .rounded_full()
        .border(px(HANDLE_RING))
        .border_color(white())
        .shadow_sm()
        .bg(fill)
}

fn checker(cols: usize, rows: usize) -> Div {
    let light = hsla(0.0, 0.0, 0.85, 1.0);
    let dark = hsla(0.0, 0.0, 0.6, 1.0);
    let cell = size(px(CHECKER), px(CHECKER));
    div().relative().overflow_hidden().child(
        canvas(
            |_, _, _| {},
            move |bounds, (), window, _| {
                for row in 0..rows {
                    for col in 0..cols {
                        let at = point(px(col as f32 * CHECKER), px(row as f32 * CHECKER));
                        let shade = if (row + col) % 2 == 0 { light } else { dark };
                        window.paint_quad(fill(Bounds::new(bounds.origin + at, cell), shade));
                    }
                }
            },
        )
        .absolute()
        .size_full(),
    )
}

fn fraction(offset: Pixels, length: Pixels) -> f32 {
    if length <= px(0.0) {
        return 0.0;
    }
    (offset / length).clamp(0.0, 1.0)
}

fn to_hsv(color: Hsla, previous_hue: f32) -> Hsv {
    let rgba = hsla_to_rgba(color);
    let (r, g, b) = (rgba.red, rgba.green, rgba.blue);
    let max = r.max(g).max(b);
    let delta = max - r.min(g).min(b);
    let h = if delta <= f32::EPSILON {
        previous_hue
    } else if max == r {
        ((g - b) / delta).rem_euclid(6.0) / 6.0
    } else if max == g {
        ((b - r) / delta + 2.0) / 6.0
    } else {
        ((r - g) / delta + 4.0) / 6.0
    };
    let s = if max <= f32::EPSILON {
        0.0
    } else {
        delta / max
    };
    Hsv {
        h,
        s,
        v: max,
        a: rgba.alpha,
    }
}

fn from_hsv(hsv: Hsv) -> Hsla {
    let sector = (hsv.h * 6.0).rem_euclid(6.0);
    let chroma = hsv.v * hsv.s;
    let x = chroma * (1.0 - ((sector % 2.0) - 1.0).abs());
    let (r, g, b) = match sector as u8 {
        0 => (chroma, x, 0.0),
        1 => (x, chroma, 0.0),
        2 => (0.0, chroma, x),
        3 => (0.0, x, chroma),
        4 => (x, 0.0, chroma),
        _ => (chroma, 0.0, x),
    };
    let m = hsv.v - chroma;
    rgb_to_hsla(Rgba::new(r + m, g + m, b + m, hsv.a))
}

pub fn parse_color(text: &str) -> Result<Hsla, String> {
    let text = text.trim().to_ascii_lowercase();
    if let Some(hex) = text.strip_prefix('#') {
        return parse_hex(hex.trim());
    }
    let Some((name, rest)) = text.split_once('(') else {
        return Err(format!("not a colour: {text}"));
    };
    let Some(body) = rest.strip_suffix(')') else {
        return Err("missing )".into());
    };
    let args: Vec<&str> = body
        .split(|c: char| c == ',' || c == '/' || c.is_whitespace())
        .filter(|arg| !arg.is_empty())
        .collect();
    let alpha = match args.get(3) {
        None if args.len() == 3 => 1.0,
        Some(arg) if args.len() == 4 => unit(arg, 1.0)?,
        _ => return Err(format!("{name}() takes 3 or 4 values")),
    };
    match name.trim() {
        "rgb" | "rgba" => {
            let channel = |arg: &str| unit(arg, 255.0);
            Ok(rgb_to_hsla(Rgba::new(
                channel(args[0])?,
                channel(args[1])?,
                channel(args[2])?,
                alpha,
            )))
        }
        "hsl" | "hsla" => {
            let degrees: f32 = args[0]
                .trim_end_matches("deg")
                .parse()
                .map_err(|_| format!("bad hue: {}", args[0]))?;
            let s = unit(args[1].trim_end_matches('%'), 100.0)?;
            let l = unit(args[2].trim_end_matches('%'), 100.0)?;
            Ok(hsla((degrees / 360.0).rem_euclid(1.0), s, l, alpha))
        }
        other => Err(format!("unknown colour function: {other}")),
    }
}

fn unit(arg: &str, scale: f32) -> Result<f32, String> {
    let (number, scale) = match arg.strip_suffix('%') {
        Some(percent) => (percent, 100.0),
        None => (arg, scale),
    };
    let value: f32 = number.parse().map_err(|_| format!("not a number: {arg}"))?;
    if !(0.0..=scale).contains(&value) {
        return Err(format!("out of range 0 to {scale}: {arg}"));
    }
    Ok(value / scale)
}

fn parse_hex(hex: &str) -> Result<Hsla, String> {
    if !hex.bytes().all(|b| b.is_ascii_hexdigit()) {
        return Err(format!("not hex: #{hex}"));
    }
    let digits: Vec<u8> = hex
        .chars()
        .filter_map(|c| c.to_digit(16))
        .filter_map(|d| u8::try_from(d).ok())
        .collect();
    let pairs: Vec<u8> = match digits.len() {
        3 | 4 => digits.iter().map(|d| d * 17).collect(),
        6 | 8 => digits
            .chunks(2)
            .map(|pair| pair[0] * 16 + pair[1])
            .collect(),
        _ => return Err(format!("#{hex} needs 3, 4, 6 or 8 digits")),
    };
    let channel = |i: usize| f32::from(pairs.get(i).copied().unwrap_or(255)) / 255.0;
    Ok(rgb_to_hsla(Rgba::new(
        channel(0),
        channel(1),
        channel(2),
        channel(3),
    )))
}

pub fn to_hex(color: Hsla) -> String {
    let rgba = hsla_to_rgba(color);
    let byte = |v: f32| (v.clamp(0.0, 1.0) * 255.0).round() as u8;
    let rgb = format!(
        "#{:02x}{:02x}{:02x}",
        byte(rgba.red),
        byte(rgba.green),
        byte(rgba.blue)
    );
    if rgba.alpha < 1.0 {
        format!("{rgb}{:02x}", byte(rgba.alpha))
    } else {
        rgb
    }
}
