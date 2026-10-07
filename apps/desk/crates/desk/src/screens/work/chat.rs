use gpui::{Div, FontWeight, div, prelude::*, px};

use super::fixture::{
    ASK, ASK_BUTTONS, COMPOSER, EFFORT, FAIL_ROW, FLEET, LEAD_FAIL, LEAD_FAIL_TIME, LEAD_FIRST,
    LEAD_LAST, MODEL, RUNNING_TOOL, TOOLS, WAITING, YOU_SAID, YOU_TIME,
};
use super::glyph::{Glyph, glyph_at};
use super::paint::{
    CAPTION, DEL_INK, FAINT, LEAD, LEAD_INK, RED, SOFT, STRONG, WARN, at, ink, medium, mono, rgb,
    ring, strong, text, tint, word_list, words,
};

const EDGE: f32 = 29.0;
const BUBBLE_MAX: f32 = 548.0;
const BODY: f32 = 14.0;
const LEADING: f32 = 23.0;
const HALF_LEADING: f32 = 2.5;
const CARD_EDGE: f32 = 15.0;

fn lead_head(time: &str, top: f32) -> [Div; 2] {
    [
        at(
            div()
                .h(px(17.5))
                .px(px(7.0))
                .pt(px(1.0))
                .rounded(px(6.0))
                .bg(tint(LEAD, 0.14))
                .child(medium("lead", 11.5, LEAD_INK)),
            EDGE,
            top + 0.3,
        ),
        at(text(time.to_owned(), 11.5, FAINT), EDGE + 45.2, top + 1.0),
    ]
}

fn spinner(x: f32, y: f32) -> Div {
    at(
        div()
            .size(px(11.0))
            .rounded_full()
            .border(px(1.5))
            .border_color(ink(0.2)),
        x,
        y,
    )
}

fn paragraph(content: &str, top: f32, width: f32) -> Div {
    at(
        words(content, BODY, LEADING, STRONG).w(px(width)),
        EDGE,
        top - HALF_LEADING,
    )
}

fn button(label: &str, key: &str, primary: bool) -> Div {
    let (fill, ink_color, key_fill, key_ink) = if primary {
        (
            ink(0.9),
            rgb(17, 17, 17, 1.0),
            rgb(0, 0, 0, 0.08),
            rgb(0, 0, 0, 0.5),
        )
    } else {
        (ink(0.07), STRONG, ink(0.08), ink(0.5))
    };
    div()
        .flex()
        .items_center()
        .gap(px(6.0))
        .h(px(26.0))
        .px(px(12.0))
        .rounded(px(8.0))
        .bg(fill)
        .child(medium(label.to_owned(), 12.5, ink_color))
        .child(
            div()
                .w(px(18.6))
                .h(px(18.0))
                .rounded(px(5.0))
                .bg(key_fill)
                .flex()
                .justify_center()
                .pt(px(2.0))
                .child(mono(key.to_owned(), 11.0, key_ink).font_weight(FontWeight::MEDIUM)),
        )
}

fn chip(label: &str) -> Div {
    div()
        .h(px(24.0))
        .px(px(9.0))
        .pt(px(2.8))
        .rounded(px(7.0))
        .child(medium(label.to_owned(), 12.5, ink(0.85)))
}

pub fn body(width: f32, height: f32) -> Vec<Div> {
    let column = width - 2.0 * EDGE;
    let bubble = BUBBLE_MAX.min(column - 19.4);
    let card = width - 2.0 * CARD_EDGE;
    let (failing, code, rest) = (LEAD_FAIL[0], LEAD_FAIL[1], LEAD_FAIL[2]);
    let mut out = vec![
        at(
            div()
                .w(px(bubble))
                .pt(px(7.0))
                .pb(px(10.5))
                .px(px(14.0))
                .rounded(px(14.0))
                .bg(ink(0.08))
                .child(
                    div()
                        .flex()
                        .gap(px(8.0))
                        .h(px(18.0))
                        .pt(px(1.0))
                        .child(text("You", 11.5, CAPTION))
                        .child(text(YOU_TIME, 11.5, FAINT)),
                )
                .child(words(YOU_SAID, BODY, LEADING, STRONG).mt(px(1.5))),
            width - EDGE - bubble,
            46.0,
        ),
        glyph_at(Glyph::Right, 13.0, SOFT, 29.0, 142.5),
        glyph_at(Glyph::Plus, 16.0, CAPTION, 27.0, height - 43.0),
        glyph_at(Glyph::Up, 13.0, FAINT, width - 143.8, height - 41.5),
        glyph_at(
            Glyph::Send,
            16.0,
            rgb(0, 0, 0, 0.6),
            width - 43.0,
            height - 43.0,
        ),
        at(text(TOOLS.0, 13.0, SOFT), 50.0, 140.0),
        at(text(TOOLS.1, 13.0, FAINT), 98.7, 140.0),
        paragraph(LEAD_FIRST.1, 195.0, column),
        spinner(EDGE, 276.5),
        at(text(FLEET.0, 13.0, ink(0.85)), 48.0, 273.0),
        at(text(FLEET.1, 13.0, FAINT), 143.0, 273.0),
        at(
            words(failing, BODY, LEADING, STRONG)
                .w(px(column))
                .child(
                    div()
                        .flex_none()
                        .h(px(21.0))
                        .mt(px(1.0))
                        .px(px(6.0))
                        .pt(px(1.0))
                        .rounded(px(6.0))
                        .bg(ink(0.08))
                        .child(mono(code, 12.5, ink(0.92))),
                )
                .children(word_list(rest)),
            EDGE,
            328.0 - HALF_LEADING,
        ),
        at(
            div()
                .w(px(column))
                .h(px(32.0))
                .rounded(px(9.0))
                .bg(tint(RED, 0.07))
                .shadow(vec![ring(tint(RED, 0.18))]),
            EDGE,
            378.0,
        ),
        at(mono("!", 13.0, DEL_INK), 41.6, 385.0),
        at(
            div()
                .flex()
                .child(text(FAIL_ROW.0, 13.0, STRONG))
                .child(text(FAIL_ROW.1, 13.0, FAINT)),
            60.0,
            385.0,
        ),
        at(
            div()
                .w(px(column - 10.0))
                .flex()
                .justify_end()
                .child(text(FAIL_ROW.2, 12.0, FAINT)),
            EDGE,
            386.0,
        ),
        paragraph(LEAD_LAST.1, 446.0, column),
        spinner(EDGE, 481.5),
        at(mono(RUNNING_TOOL.0, 12.5, SOFT), 48.0, 478.0),
        at(text(RUNNING_TOOL.1, 12.5, WARN), 191.0, 478.0),
        spinner(17.0, height - 159.0),
        at(text(WAITING.0, 12.0, FAINT), 36.0, height - 162.0),
        at(text(WAITING.1, 12.0, FAINT), 123.5, height - 162.0),
        at(
            div()
                .w(px(card))
                .h(px(75.0))
                .px(px(12.0))
                .pt(px(11.0))
                .rounded(px(13.0))
                .bg(tint(WARN, 0.06))
                .shadow(vec![ring(tint(WARN, 0.24))])
                .flex()
                .flex_col()
                .gap(px(6.0))
                .child(
                    div()
                        .flex()
                        .items_center()
                        .h(px(23.0))
                        .child(strong("?", 13.0, WARN).mr(px(8.0)))
                        .child(text(ASK.0, 13.0, STRONG).mr(px(8.0)))
                        .child(mono(ASK.1, 12.5, STRONG))
                        .child(div().flex_1())
                        .child(text(ASK.2, 12.0, FAINT)),
                )
                .child(
                    div().flex().gap(px(6.0)).children(
                        ASK_BUTTONS
                            .iter()
                            .enumerate()
                            .map(|(index, (label, key))| button(label, key, index == 0)),
                    ),
                ),
            CARD_EDGE,
            height - 138.0,
        ),
        at(
            div()
                .w(px(card))
                .h(px(40.0))
                .rounded(px(16.0))
                .bg(rgb(0, 0, 0, 0.22))
                .shadow(vec![ring(ink(0.09))])
                .flex()
                .items_center()
                .pl(px(40.0))
                .pr(px(6.0))
                .child(text(COMPOSER, BODY, FAINT))
                .child(div().flex_1())
                .child(
                    div()
                        .flex()
                        .pr(px(4.0))
                        .child(chip(MODEL))
                        .child(div().w(px(14.0))),
                )
                .child(chip(EFFORT).mr(px(4.0)))
                .child(div().size(px(28.0)).rounded(px(14.0)).bg(ink(0.18))),
            CARD_EDGE,
            height - 55.0,
        ),
    ];
    out.extend(lead_head(LEAD_FIRST.0, 173.0));
    out.extend(lead_head(LEAD_FAIL_TIME, 306.0));
    out.extend(lead_head(LEAD_LAST.0, 424.0));
    out
}
