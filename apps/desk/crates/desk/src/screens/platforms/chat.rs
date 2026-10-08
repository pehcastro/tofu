use gpui::{Context, Div, FontWeight, div, prelude::*, px, relative};

use super::fixture::{
    ASK, ASK_BUTTONS, COMPOSER, EFFORT, FAIL_ROW, FLEET, LEAD_FAIL, LEAD_FAIL_TIME, LEAD_FIRST,
    LEAD_LAST, MODEL, RUNNING_TOOL, SESSION, TOOLS, WAITING, YOU_SAID, YOU_TIME,
};
use super::glyph::{Glyph, glyph};
use super::paint::{
    CAPTION, DEL_INK, FAINT, LEAD, LEAD_INK, RED, SOFT, STRONG, WARN, dropping, ink, medium, mono,
    rgb, ring, strong, text, tint, word_list, words,
};
use super::{Action, Platforms};

const EDGE: f32 = 29.0;
const BUBBLE_SHARE: f32 = 0.86;
const BODY: f32 = 14.0;
const LEADING: f32 = 23.0;
const CARD_EDGE: f32 = 15.0;
const COMPOSER_TALL: f32 = 40.0;
const ASK_TELLS: [&str; 3] = [
    "Allowed once: bash runs npm run build.",
    "Denied: the lead is told npm run build was refused.",
    "Always here: npm run build is allowed in notes-app from now on.",
];
const ATTACH: &str =
    "Attaches a file or an image; it goes to the lead as a reference, like an @ mention.";
const PICK_MODEL: &str =
    "Picks the model the lead runs on from your accounts; the change applies from the next turn.";
const CYCLE_EFFORT: &str = "Cycles the effort: low, medium, high.";

fn lead(time: &str, says: Div) -> Div {
    div()
        .flex()
        .flex_col()
        .gap(px(4.0))
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(8.0))
                .child(
                    div()
                        .px(px(7.0))
                        .rounded(px(6.0))
                        .bg(tint(LEAD, 0.14))
                        .child(medium("lead", 11.5, LEAD_INK)),
                )
                .child(text(time.to_owned(), 11.5, FAINT)),
        )
        .child(says)
}

fn spinner() -> Div {
    div()
        .flex_none()
        .size(px(11.0))
        .rounded_full()
        .border(px(1.5))
        .border_color(ink(0.2))
}

fn line(children: impl IntoIterator<Item = Div>) -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(8.0))
        .min_w_0()
        .overflow_hidden()
        .children(children)
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
        .flex_none()
        .items_center()
        .gap(px(6.0))
        .h(px(26.0))
        .px(px(12.0))
        .rounded(px(8.0))
        .bg(fill)
        .child(medium(label.to_owned(), 12.5, ink_color))
        .child(
            div()
                .flex()
                .justify_center()
                .w(px(18.6))
                .rounded(px(5.0))
                .bg(key_fill)
                .child(mono(key.to_owned(), 11.0, key_ink).font_weight(FontWeight::MEDIUM)),
        )
}

fn chip(label: &str) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .h(px(24.0))
        .px(px(9.0))
        .rounded(px(7.0))
        .child(medium(label.to_owned(), 12.5, ink(0.85)))
}

impl Platforms {
    pub(super) fn chat(cx: &mut Context<Self>) -> Div {
        let (failing, code, rest) = (LEAD_FAIL[0], LEAD_FAIL[1], LEAD_FAIL[2]);
        let transcript = div()
            .id("platforms-transcript")
            .flex_1()
            .min_h_0()
            .overflow_y_scroll()
            .flex()
            .flex_col()
            .gap(px(14.0))
            .px(px(EDGE))
            .py(px(16.0))
            .child(
                div()
                    .self_end()
                    .max_w(relative(BUBBLE_SHARE))
                    .pt(px(7.0))
                    .pb(px(10.0))
                    .px(px(14.0))
                    .rounded(px(14.0))
                    .bg(ink(0.08))
                    .child(line([
                        text("You", 11.5, CAPTION),
                        text(YOU_TIME, 11.5, FAINT),
                    ]))
                    .child(words(YOU_SAID, BODY, LEADING, STRONG)),
            )
            .child(line([
                glyph(Glyph::Right, 13.0, SOFT),
                text(TOOLS.0, 13.0, SOFT),
                text(TOOLS.1, 13.0, FAINT),
            ]))
            .child(lead(
                LEAD_FIRST.0,
                words(LEAD_FIRST.1, BODY, LEADING, STRONG),
            ))
            .child(line([
                spinner(),
                text(FLEET.0, 13.0, ink(0.85)),
                text(FLEET.1, 13.0, FAINT).flex_shrink_1().truncate(),
            ]))
            .child(lead(
                LEAD_FAIL_TIME,
                words(failing, BODY, LEADING, STRONG)
                    .child(
                        div()
                            .flex_none()
                            .px(px(6.0))
                            .rounded(px(6.0))
                            .bg(ink(0.08))
                            .child(mono(code, 12.5, ink(0.92))),
                    )
                    .children(word_list(rest)),
            ))
            .child(
                line([
                    mono("!", 13.0, DEL_INK),
                    text(FAIL_ROW.0, 13.0, STRONG),
                    text(FAIL_ROW.1, 13.0, FAINT).flex_shrink_1().truncate(),
                    div().flex_1(),
                    text(FAIL_ROW.2, 12.0, FAINT),
                ])
                .flex_none()
                .h(px(32.0))
                .px(px(12.0))
                .rounded(px(9.0))
                .bg(tint(RED, 0.07))
                .shadow(vec![ring(tint(RED, 0.18))]),
            )
            .child(lead(LEAD_LAST.0, words(LEAD_LAST.1, BODY, LEADING, STRONG)))
            .child(line([
                spinner(),
                mono(RUNNING_TOOL.0, 12.5, SOFT),
                text(RUNNING_TOOL.1, 12.5, WARN),
            ]));
        let asks =
            ASK_BUTTONS
                .iter()
                .zip(ASK_TELLS)
                .enumerate()
                .map(|(index, ((label, key), tell))| {
                    Self::hot(button(label, key, index == 0), Action::Tell(tell), cx)
                });
        let asks: Vec<Div> = asks.collect();
        let ask = div()
            .flex()
            .flex_col()
            .gap(px(6.0))
            .px(px(12.0))
            .py(px(10.0))
            .rounded(px(13.0))
            .bg(tint(WARN, 0.06))
            .shadow(vec![ring(tint(WARN, 0.24))])
            .child(line([
                strong("?", 13.0, WARN),
                text(ASK.0, 13.0, STRONG),
                mono(ASK.1, 12.5, STRONG)
                    .flex_shrink_1()
                    .min_w_0()
                    .truncate(),
                div().flex_1(),
                text(ASK.2, 12.0, FAINT).flex_shrink_1().truncate(),
            ]))
            .child(div().flex().flex_wrap().gap(px(6.0)).children(asks));
        let composer = line([
            Self::hot(
                div().flex_none().child(glyph(Glyph::Plus, 16.0, CAPTION)),
                Action::Tell(ATTACH),
                cx,
            ),
            div()
                .flex_1()
                .min_w_0()
                .child(text(COMPOSER, BODY, FAINT).truncate()),
            dropping(COMPOSER_TALL)
                .child(Self::hot(
                    chip(MODEL)
                        .gap(px(4.0))
                        .child(glyph(Glyph::Up, 13.0, FAINT)),
                    Action::Tell(PICK_MODEL),
                    cx,
                ))
                .child(Self::hot(chip(EFFORT), Action::Tell(CYCLE_EFFORT), cx)),
            div()
                .flex()
                .flex_none()
                .items_center()
                .justify_center()
                .size(px(28.0))
                .rounded_full()
                .bg(ink(0.18))
                .child(glyph(Glyph::Send, 16.0, rgb(0, 0, 0, 0.6))),
        ])
        .flex_none()
        .h(px(COMPOSER_TALL))
        .pl(px(12.0))
        .pr(px(6.0))
        .rounded(px(16.0))
        .bg(rgb(0, 0, 0, 0.22))
        .shadow(vec![ring(ink(0.09))]);
        div()
            .flex()
            .flex_col()
            .flex_1()
            .min_h_0()
            .child(
                line([
                    glyph(Glyph::Chat, 13.0, ink(0.6)),
                    medium("Chat", 12.0, SOFT),
                    div().flex_1(),
                    medium(SESSION, 12.0, FAINT).flex_shrink_1().truncate(),
                ])
                .flex_none()
                .h(px(28.0))
                .px(px(12.0)),
            )
            .child(transcript)
            .child(
                div()
                    .flex()
                    .flex_none()
                    .flex_col()
                    .gap(px(10.0))
                    .px(px(CARD_EDGE))
                    .pb(px(CARD_EDGE))
                    .child(
                        line([
                            spinner(),
                            text(WAITING.0, 12.0, FAINT),
                            text(WAITING.1, 12.0, FAINT),
                        ])
                        .pl(px(2.0)),
                    )
                    .child(ask)
                    .child(composer),
            )
    }
}
