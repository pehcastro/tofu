use gpui::{Div, FontWeight, Rgba, SharedString, div, prelude::*, px};

use super::paint::{
    LINK, MONO, ON_LIGHT, PLUS, SEND, T2, T3, UP, WARN, black, glyph, medium, mono, ringed, spacer,
    spinner, text, tint, white,
};
use super::{Overlay, Wire};

fn field() -> Div {
    ringed(16.0, white(0.09)).bg(black(0.22))
}

fn add(size: f32, scale: f32) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(size))
        .rounded(px(7.0))
        .child(glyph(PLUS, 16.0, white(0.45), scale))
}

fn chip(label: &'static str, open: bool, lit: bool, scale: f32) -> Div {
    if lit {
        ringed(7.0, white(0.2)).bg(white(0.12))
    } else {
        div()
    }
    .flex()
    .flex_none()
    .items_center()
    .gap(px(6.0))
    .h(px(24.0))
    .px(px(9.0))
    .rounded(px(7.0))
    .child(medium(12.5, 12.5, white(0.85), label))
    .when(open, |chip| chip.child(glyph(UP, 13.0, white(T3), scale)))
}

fn send(fill: Rgba, ink: Rgba, scale: f32) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(28.0))
        .rounded(px(14.0))
        .bg(fill)
        .child(glyph(SEND, 16.0, ink, scale))
}

pub fn compact(placeholder: &'static str, scale: f32) -> Div {
    field()
        .w(px(772.0))
        .p(px(6.0))
        .flex()
        .items_center()
        .gap(px(4.0))
        .child(add(28.0, scale))
        .child(
            text(14.0, 23.0, white(T3), placeholder)
                .flex_1()
                .min_w_0()
                .pl(px(2.0))
                .overflow_hidden()
                .text_ellipsis(),
        )
        .child(chip("Opus 5", true, false, scale))
        .child(chip("Medium", false, false, scale))
        .child(send(white(0.18), black(0.6), scale))
}

pub fn tall(
    typed: &'static str,
    trigger: &'static str,
    trigger_opens: Overlay,
    wire: &Wire,
    scale: f32,
) -> Div {
    field()
        .pt(px(12.0))
        .pr(px(12.0))
        .pb(px(10.0))
        .pl(px(16.0))
        .flex()
        .flex_col()
        .gap(px(10.0))
        .child(
            div()
                .flex()
                .items_center()
                .h(px(22.0))
                .line_height(px(22.0))
                .when(!typed.is_empty(), |line| {
                    line.child(text(14.0, 22.0, white(0.9), typed))
                })
                .child(wire.on(text(14.0, 22.0, LINK, trigger), trigger_opens))
                .child(div().w(px(1.5)).h(px(16.0)).bg(white(1.0))),
        )
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(6.0))
                .child(add(26.0, scale))
                .child(spacer())
                .child(wire.on(
                    chip("Opus 5", true, wire.overlay == Overlay::Picker, scale),
                    Overlay::Picker,
                ))
                .child(chip("Medium", false, false, scale))
                .child(send(white(0.9), ON_LIGHT, scale)),
        )
}

pub fn kbd(key: &'static str, on_light: bool) -> Div {
    let (fill, ink) = if on_light {
        (black(0.08), black(0.5))
    } else {
        (white(0.08), white(0.5))
    };
    text(11.0, 16.0, ink, key)
        .font_family(MONO)
        .font_weight(FontWeight::MEDIUM)
        .py(px(1.0))
        .px(px(6.0))
        .rounded(px(5.0))
        .bg(fill)
}

pub fn button(label: &'static str, key: &'static str, primary: bool) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .gap(px(6.0))
        .h(px(28.0))
        .px(px(12.0))
        .rounded(px(8.0))
        .bg(if primary { white(0.9) } else { white(0.07) })
        .child(medium(
            13.0,
            13.0,
            if primary { ON_LIGHT } else { white(0.9) },
            label,
        ))
        .child(kbd(key, primary))
}

pub struct Ask {
    pub command: &'static str,
    pub risk: &'static str,
    pub always: &'static str,
}

pub fn ask_bar(ask: &Ask) -> Div {
    ringed(14.0, tint(0xe8c98a, 0.25))
        .w(px(788.0))
        .py(px(10.0))
        .px(px(14.0))
        .bg(tint(0xe8c98a, 0.06))
        .flex()
        .flex_col()
        .gap(px(8.0))
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(8.0))
                .text_size(px(13.5))
                .child(text(13.5, 23.0, WARN, "?").font_weight(FontWeight::SEMIBOLD))
                .child(text(13.5, 23.0, white(0.9), "bash wants"))
                .child(mono(12.5, 23.0, white(0.9), ask.command))
                .child(spacer())
                .child(text(12.0, 23.0, white(T3), ask.risk))
                .child(text(12.0, 23.0, white(T3), "why")),
        )
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(6.0))
                .child(button("Allow once", "1", true))
                .child(button("Deny", "2", false))
                .child(button(ask.always, "3", false))
                .child(spacer())
                .child(text(
                    12.0,
                    23.0,
                    white(T3),
                    "the chat still takes what you type",
                )),
        )
}

pub fn working(phase: &'static str, queued: &'static str, scale: f32) -> Div {
    div()
        .w(px(768.0))
        .flex()
        .items_center()
        .gap(px(10.0))
        .px(px(4.0))
        .pb(px(6.0))
        .text_size(px(12.0))
        .text_color(white(T3))
        .child(spinner(11.0, scale))
        .child(div().child(phase))
        .child(div().child("· 3m 20s"))
        .child(spacer())
        .child(div().child(queued))
        .child(text(12.0, 23.0, white(T2), "take it back"))
}

pub fn queued_line(said: impl Into<SharedString>) -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(8.0))
        .px(px(6.0))
        .text_size(px(12.5))
        .child(
            medium(12.5, 12.5, WARN, "queued")
                .h(px(24.0))
                .flex()
                .items_center()
                .px(px(9.0))
                .rounded(px(7.0))
                .bg(tint(0xe8c98a, 0.1)),
        )
        .child(div().flex_1().text_color(white(T2)).child(said.into()))
        .child(text(12.5, 23.0, white(T3), "×"))
}
