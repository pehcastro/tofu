use desk_canvas::{Dash, Fill, HUES, OPACITIES, Pick, Route, Size, Tool};
use desk_ui::components::paint::ink;
use desk_ui::components::size::{FONT_SMALL, T1, T2};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, Bounds, ClickEvent, Context, DispatchPhase, Div, ElementId, Focusable, MouseButton,
    MouseDownEvent, MouseMoveEvent, MouseUpEvent, Pixels, Rgba, SharedString, Stateful, Window,
    canvas, div, prelude::*, px, relative,
};

use super::marks;
use super::{Draw, INSET, ISLAND_PAD, PANEL_WIDTH, TOOL_BOX, face, island};

const SWATCH: f32 = 14.0;
const SWATCH_RING: f32 = 1.0;
const SWATCH_COLUMNS_WIDE: f32 = 140.0;
const TINT_CELL: f32 = 12.0;
const TINT_SATURATION: f32 = 0.75;
const TINT_HUES: u16 = 12;
const TINT_LIGHTS: [f32; 5] = [0.25, 0.4, 0.55, 0.7, 0.85];
const SEGMENT: f32 = 32.0;
const SLIDER_TALL: f32 = 16.0;
const RAIL: f32 = 2.0;
const THUMB: f32 = 12.0;

pub(super) struct Section {
    pub title: &'static str,
    pub picks: Vec<Pick>,
}

pub(super) fn sections(tool: Tool, routes: bool) -> Vec<Section> {
    let section = |title, picks| Section { title, picks };
    let fills = !matches!(tool, Tool::Text | Tool::Line);
    let dashes = tool != Tool::Text;
    match tool {
        Tool::Hand | Tool::Eraser | Tool::Frame | Tool::Note | Tool::Laser => Vec::new(),
        Tool::Select
        | Tool::Rectangle
        | Tool::Ellipse
        | Tool::Arrow
        | Tool::Line
        | Tool::Pencil
        | Tool::Text => [
            Some(section("Color", HUES.map(Pick::Color).to_vec())),
            Some(section("Opacity", OPACITIES.map(Pick::Opacity).to_vec())),
            fills.then(|| section("Fill", Fill::ALL.map(Pick::Fill).to_vec())),
            dashes.then(|| section("Dash", Dash::ALL.map(Pick::Dash).to_vec())),
            Some(section("Size", Size::ALL.map(Pick::Size).to_vec())),
            routes.then(|| section("Arrow", Route::ALL.map(Pick::Route).to_vec())),
        ]
        .into_iter()
        .flatten()
        .collect(),
    }
}

pub(super) fn caption(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, T2))
        .child(text.into())
}

pub(super) fn swatch(
    id: impl Into<ElementId>,
    color: Option<Rgba>,
    on: bool,
    theme: &Theme,
) -> Stateful<Div> {
    let ring = match on {
        true => theme.color(ColorToken::FocusRing),
        false => theme.color(ColorToken::ButtonFill),
    };
    let dot = div().size(px(SWATCH)).rounded_full();
    let dot = match color {
        Some(color) => dot.bg(color),
        None => dot.border_1().border_color(ink(theme, T2)),
    };
    div()
        .id(id)
        .flex_none()
        .p(px(SWATCH_RING))
        .rounded_full()
        .border_2()
        .border_color(ring)
        .cursor_pointer()
        .child(dot)
}

fn hsl(hue: f32, saturation: f32, lightness: f32) -> u32 {
    let chroma = (1.0 - (2.0 * lightness - 1.0).abs()) * saturation;
    let sector = hue / 60.0;
    let second = chroma * (1.0 - (sector % 2.0 - 1.0).abs());
    let (red, green, blue) = match sector as u32 {
        0 => (chroma, second, 0.0),
        1 => (second, chroma, 0.0),
        2 => (0.0, chroma, second),
        3 => (0.0, second, chroma),
        4 => (second, 0.0, chroma),
        _ => (chroma, 0.0, second),
    };
    let floor = lightness - chroma / 2.0;
    let byte = |channel: f32| ((channel + floor) * 255.0).round().clamp(0.0, 255.0) as u32;
    (byte(red) << 16) | (byte(green) << 8) | byte(blue)
}

fn percent_at(bounds: Bounds<Pixels>, x: Pixels) -> u8 {
    let width = f32::from(bounds.size.width).max(1.0);
    let share = (f32::from(x - bounds.origin.x) / width).clamp(0.0, 1.0);
    (share * 100.0).round() as u8
}

impl Draw {
    pub(super) fn hue_grid(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let canvas = self.canvas.read(cx);
        let faces: Vec<_> = HUES
            .into_iter()
            .enumerate()
            .map(|(at, hue)| {
                let pick = Pick::Color(hue);
                swatch(
                    ("draw-swatch", at),
                    pick.swatch(theme),
                    canvas.picked(pick),
                    theme,
                )
                .aria_label(pick.name())
                .on_click(cx.listener(move |draw, _: &ClickEvent, window, cx| {
                    draw.context = None;
                    draw.canvas
                        .update(cx, |canvas, cx| canvas.restyle(pick, window, cx));
                }))
            })
            .collect();
        div()
            .w(px(SWATCH_COLUMNS_WIDE))
            .flex()
            .flex_wrap()
            .gap_1()
            .children(faces)
    }

    fn colors(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let custom = swatch("draw-custom", None, self.picking, theme)
            .aria_label("Custom color")
            .on_click(cx.listener(|draw, _: &ClickEvent, _, cx| {
                draw.picking = !draw.picking;
                cx.notify();
            }));
        let mut colors = div().flex().flex_col().gap_1().child(
            div()
                .flex()
                .items_start()
                .gap_1()
                .child(self.hue_grid(theme, cx))
                .child(custom),
        );
        if self.picking {
            let canvas = self.canvas.read(cx);
            let cells: Vec<_> = TINT_LIGHTS
                .into_iter()
                .flat_map(|light| {
                    (0..TINT_HUES).map(move |step| {
                        let degrees = f32::from(step) * 360.0 / f32::from(TINT_HUES);
                        hsl(degrees, TINT_SATURATION, light)
                    })
                })
                .enumerate()
                .map(|(at, color)| {
                    let pick = Pick::Tint(color);
                    let on = canvas.picked(pick);
                    div()
                        .id(("draw-tint", at))
                        .size(px(TINT_CELL))
                        .rounded_sm()
                        .bg(gpui::rgb(color))
                        .cursor_pointer()
                        .when(on, |cell| {
                            cell.border_2()
                                .border_color(theme.color(ColorToken::FocusRing))
                        })
                        .on_click(cx.listener(move |draw, _: &ClickEvent, window, cx| {
                            draw.canvas
                                .update(cx, |canvas, cx| canvas.restyle(pick, window, cx));
                        }))
                })
                .collect();
            colors = colors.child(
                div()
                    .id("draw-tints")
                    .flex()
                    .flex_wrap()
                    .gap(px(2.0))
                    .children(cells),
            );
        }
        colors
    }

    fn opacity(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let shown = self
            .opacity_drag
            .unwrap_or_else(|| self.canvas.read(cx).opacity());
        let share = f32::from(shown) / 100.0;
        let accent = theme.color(ColorToken::FocusRing);
        let weak = cx.weak_entity();
        let dragging = self.opacity_drag.is_some();
        let listen = canvas(
            |_, _, _| (),
            move |bounds, (), window, _| {
                let held = weak.clone();
                window.on_mouse_event(move |event: &MouseDownEvent, phase, _, cx| {
                    if phase != DispatchPhase::Bubble
                        || event.button != MouseButton::Left
                        || !bounds.contains(&event.position)
                    {
                        return;
                    }
                    let percent = percent_at(bounds, event.position.x);
                    if let Err(error) = held.update(cx, |draw, cx| {
                        draw.opacity_drag = Some(percent);
                        cx.notify();
                    }) {
                        eprintln!("desk: draw: opacity: the draw tile is gone: {error}");
                    }
                });
                if !dragging {
                    return;
                }
                let moved = weak.clone();
                window.on_mouse_event(move |event: &MouseMoveEvent, phase, _, cx| {
                    if phase != DispatchPhase::Bubble {
                        return;
                    }
                    let percent = percent_at(bounds, event.position.x);
                    if let Err(error) = moved.update(cx, |draw, cx| {
                        draw.opacity_drag = Some(percent);
                        cx.notify();
                    }) {
                        eprintln!("desk: draw: opacity: the draw tile is gone: {error}");
                    }
                });
                let released = weak.clone();
                window.on_mouse_event(move |event: &MouseUpEvent, phase, window, cx| {
                    if phase != DispatchPhase::Bubble || event.button != MouseButton::Left {
                        return;
                    }
                    if let Err(error) = released.update(cx, |draw, cx| {
                        let Some(percent) = draw.opacity_drag.take() else {
                            return;
                        };
                        eprintln!("desk: draw: opacity {percent}%");
                        draw.canvas.update(cx, |canvas, cx| {
                            canvas.restyle(Pick::Opacity(percent), window, cx)
                        });
                        cx.notify();
                    }) {
                        eprintln!("desk: draw: opacity: the draw tile is gone: {error}");
                    }
                });
            },
        )
        .absolute()
        .size_full();
        let middle = (SLIDER_TALL - RAIL) / 2.0;
        let slider = div()
            .id("draw-opacity")
            .relative()
            .w_full()
            .h(px(SLIDER_TALL))
            .cursor_pointer()
            .child(
                div()
                    .absolute()
                    .left_0()
                    .right_0()
                    .top(px(middle))
                    .h(px(RAIL))
                    .rounded_full()
                    .bg(ink(theme, T2)),
            )
            .child(
                div()
                    .absolute()
                    .left_0()
                    .top(px(middle))
                    .h(px(RAIL))
                    .w(relative(share))
                    .rounded_full()
                    .bg(accent),
            )
            .child(
                div()
                    .absolute()
                    .top(px((SLIDER_TALL - THUMB) / 2.0))
                    .left(relative(share))
                    .ml(px(-THUMB / 2.0))
                    .size(px(THUMB))
                    .rounded_full()
                    .bg(accent),
            )
            .child(listen);
        div()
            .flex()
            .flex_col()
            .gap_1()
            .child(
                div()
                    .flex()
                    .justify_between()
                    .child(caption("Opacity", theme))
                    .child(caption(format!("{shown}%"), theme)),
            )
            .child(slider)
    }

    fn segments(
        &self,
        title: &'static str,
        picks: Vec<Pick>,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Div {
        let accent = theme.color(ColorToken::FocusRing);
        let plain = ink(theme, T1);
        let mut faces: Vec<AnyElement> = Vec::with_capacity(picks.len());
        for (at, pick) in picks.into_iter().enumerate() {
            let on = self.canvas.read(cx).picked(pick);
            let button = face((title, at), on, theme)
                .size(px(SEGMENT))
                .aria_label(pick.name())
                .child(marks::paint(
                    marks::of_pick(pick),
                    if on { accent } else { plain },
                ))
                .on_click(cx.listener(move |draw, _: &ClickEvent, window, cx| {
                    draw.canvas
                        .update(cx, |canvas, cx| canvas.restyle(pick, window, cx));
                }));
            let tip = format!("{title}  {}", pick.name());
            faces.push(
                tooltip(
                    (title, at + 100),
                    button,
                    Edge::Frame,
                    tip,
                    theme,
                    window,
                    cx,
                )
                .into_any_element(),
            );
        }
        div()
            .flex()
            .flex_col()
            .gap_1()
            .child(caption(title, theme))
            .child(div().flex().gap_0p5().children(faces))
    }

    pub(super) fn style_rows(
        &self,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Div {
        let canvas = self.canvas.read(cx);
        let tool = match canvas.tool().0 {
            tool if tool.draws() => tool,
            _ => Tool::Select,
        };
        let mut rows = div().flex().flex_col().gap_2();
        for section in sections(tool, canvas.routes()) {
            rows = rows.child(match section.picks.first() {
                Some(Pick::Color(_)) => div()
                    .flex()
                    .flex_col()
                    .gap_1()
                    .child(caption(section.title, theme))
                    .child(self.colors(theme, cx)),
                Some(Pick::Opacity(_)) => self.opacity(theme, cx),
                _ => self.segments(section.title, section.picks, theme, window, cx),
            });
        }
        rows
    }

    pub(super) fn docked(
        &self,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Option<Stateful<Div>> {
        if !self.canvas.read(cx).styling() {
            return None;
        }
        let clear_of_toolbar = INSET + TOOL_BOX + ISLAND_PAD * 4.0;
        Some(
            div()
                .id("draw-styles-dock")
                .absolute()
                .right(px(INSET))
                .top(px(INSET))
                .bottom(px(clear_of_toolbar))
                .flex()
                .flex_col()
                .child(
                    island(theme)
                        .id("draw-styles")
                        .occlude()
                        .w(px(PANEL_WIDTH))
                        .max_h_full()
                        .overflow_y_scroll()
                        .p_2()
                        .child(self.style_rows(theme, window, cx)),
                ),
        )
    }

    pub(super) fn compact(
        &self,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let canvas = self.canvas.read(cx);
        let color = HUES
            .into_iter()
            .find(|hue| canvas.picked(Pick::Color(*hue)))
            .and_then(|hue| Pick::Color(hue).swatch(theme));
        let button = face("draw-styles-button", self.styles_open, theme)
            .child(swatch("draw-styles-dot", color, false, theme))
            .on_click(cx.listener(|draw, _: &ClickEvent, window, cx| {
                draw.styles_open = !draw.styles_open;
                eprintln!("desk: draw: style popover open {}", draw.styles_open);
                draw.canvas
                    .update(cx, |canvas, cx| canvas.focus_handle(cx).focus(window, cx));
                cx.notify();
            }));
        let popover = self.styles_open.then(|| {
            island(theme)
                .id("draw-styles-popover")
                .occlude()
                .w(px(PANEL_WIDTH))
                .mb(px(ISLAND_PAD * 2.0))
                .p_2()
                .child(self.style_rows(theme, window, cx))
        });
        div()
            .relative()
            .flex_none()
            .size(px(TOOL_BOX))
            .child(
                div()
                    .id("draw-styles-compact")
                    .absolute()
                    .right_0()
                    .bottom_0()
                    .flex()
                    .flex_col()
                    .items_end()
                    .children(popover)
                    .child(button)
                    .on_mouse_down_out(cx.listener(|draw, _: &MouseDownEvent, _, cx| {
                        if draw.styles_open {
                            draw.styles_open = false;
                            eprintln!("desk: draw: popover closed by click outside");
                            cx.notify();
                        }
                    })),
            )
            .into_any_element()
    }
}
