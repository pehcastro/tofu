use std::sync::Arc;

use gpui::{
    AnyElement, Div, FontWeight, Image, IntoElement, div, img, linear_color_stop, linear_gradient,
    prelude::*, px,
};

use super::paint::{BRANCH, SANS, T3, WARN, glyph, hex, medium, spacer, text, white};

const SIDEBAR: &str = r#"<rect x="2" y="3" width="12" height="10" rx="2"/><path d="M6 3v10"/>"#;
const PIN: &str = r#"<path d="M6 2h4l-.5 4 2 2H4.5l2-2zM8 8v6"/>"#;
const CHAT: &str = r#"<path d="M3 4.5A1.5 1.5 0 0 1 4.5 3h7A1.5 1.5 0 0 1 13 4.5v5a1.5 1.5 0 0 1-1.5 1.5H7l-3 2.5V11h.5"/>"#;
const CODE: &str = r#"<path d="M5.5 5L2.5 8l3 3M10.5 5l3 3-3 3"/>"#;
const DATA: &str = r#"<path d="M3 4c0-1.1 2.2-2 5-2s5 .9 5 2v8c0 1.1-2.2 2-5 2s-5-.9-5-2zM3 4c0 1.1 2.2 2 5 2s5-.9 5-2"/>"#;
const SEARCH: &str = r#"<circle cx="7" cy="7" r="4"/><path d="M10 10l3 3"/>"#;
const BELL: &str = r#"<path d="M4 11V7a4 4 0 0 1 8 0v4l1 1.5H3zM6.5 14h3"/>"#;
const MINIMIZE: &str = r#"<path d="M4 8.5h8"/>"#;
const MAXIMIZE: &str = r#"<rect x="4" y="4" width="8" height="8" rx="2"/>"#;
const CLOSE: &str = r#"<path d="M4.5 4.5l7 7M11.5 4.5l-7 7"/>"#;

const BOARD: [f32; 2] = [1440.0, 900.0];
const WINDOW_INSET: [f32; 2] = [20.0, 18.0];
const CLOSED_SIDEBAR: f32 = 50.0;
const GUTTER: f32 = 8.0;

pub fn window(backdrop: &Arc<Image>, scale: f32, main: impl IntoElement) -> Div {
    let [left, top] = WINDOW_INSET;
    div()
        .relative()
        .w(px(BOARD[0]))
        .h(px(BOARD[1]))
        .overflow_hidden()
        .font_family(SANS)
        .text_size(px(14.0))
        .line_height(px(23.0))
        .text_color(white(0.9))
        .child(img(backdrop.clone()).absolute().size_full())
        .child(
            div()
                .absolute()
                .left(px(left))
                .top(px(top))
                .right(px(left))
                .bottom(px(top))
                .flex()
                .flex_col()
                .child(header(scale))
                .child(
                    div()
                        .flex_1()
                        .min_h_0()
                        .flex()
                        .pl(px(GUTTER))
                        .pt(px(2.0))
                        .pr(px(GUTTER))
                        .pb(px(GUTTER))
                        .child(main),
                )
                .child(footer(scale)),
        )
}

fn square(size: f32, radius: f32) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(size))
        .rounded(px(radius))
}

fn header_tab(on: bool, icon: &str, label: &'static str, tail: Div, scale: f32) -> Div {
    let ink = if on { white(1.0) } else { white(0.55) };
    div()
        .flex()
        .flex_none()
        .items_center()
        .h(px(28.0))
        .pr(px(3.0))
        .rounded(px(8.0))
        .when(on, |tab| tab.bg(white(0.1)))
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(7.0))
                .h(px(28.0))
                .pl(px(10.0))
                .pr(px(6.0))
                .child(glyph(icon, 13.0, ink, scale))
                .child(medium(13.0, 13.0, ink, label)),
        )
        .child(tail)
}

fn header(scale: f32) -> Div {
    let quiet = white(0.55);
    let hidden_close = || div().flex_none().size(px(18.0));
    div()
        .h(px(40.0))
        .flex_none()
        .flex()
        .items_center()
        .gap(px(3.0))
        .child(
            div()
                .w(px(CLOSED_SIDEBAR))
                .flex_none()
                .h_full()
                .flex()
                .items_center()
                .pl(px(10.0))
                .child(square(28.0, 7.0).child(glyph(SIDEBAR, 16.0, white(0.45), scale))),
        )
        .child(header_tab(
            true,
            CHAT,
            "work",
            div()
                .w(px(18.0))
                .flex()
                .justify_center()
                .child(glyph(PIN, 11.0, white(T3), scale)),
            scale,
        ))
        .child(header_tab(false, CODE, "editor", hidden_close(), scale))
        .child(header_tab(false, DATA, "data", hidden_close(), scale))
        .child(
            div()
                .flex()
                .items_center()
                .h(px(30.0))
                .px(px(10.0))
                .child(medium(13.0, 13.0, quiet, "+")),
        )
        .child(spacer().h_full())
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(8.0))
                .h(px(28.0))
                .px(px(11.0))
                .child(glyph(SEARCH, 13.0, quiet, scale))
                .child(medium(13.0, 13.0, white(T3), "Alt K")),
        )
        .child(
            square(28.0, 7.0)
                .relative()
                .child(glyph(BELL, 16.0, white(0.45), scale))
                .child(
                    div()
                        .absolute()
                        .right(px(6.0))
                        .top(px(6.0))
                        .size(px(6.0))
                        .rounded(px(3.0))
                        .bg(WARN),
                ),
        )
        .child(
            square(28.0, 0.0).child(
                square(22.0, 11.0)
                    .bg(linear_gradient(
                        135.0,
                        linear_color_stop(hex(0x6b7a8f), 0.0),
                        linear_color_stop(hex(0x3a4250), 1.0),
                    ))
                    .child(text(10.0, 10.0, white(0.9), "p").font_weight(FontWeight::SEMIBOLD)),
            ),
        )
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(2.0))
                .mx(px(6.0))
                .children([MINIMIZE, MAXIMIZE, CLOSE].map(|icon| {
                    square(28.0, 8.0)
                        .w(px(34.0))
                        .child(glyph(icon, 13.0, white(0.62), scale))
                })),
        )
}

fn status(children: impl IntoIterator<Item = AnyElement>) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(6.0))
        .h(px(22.0))
        .px(px(8.0))
        .rounded(px(6.0))
        .children(children)
}

fn footer(scale: f32) -> Div {
    let ink = white(0.5);
    let word = |body: &'static str| medium(12.0, 12.0, ink, body).into_any_element();
    let quiet = |body: &'static str| medium(12.0, 12.0, white(T3), body).into_any_element();
    div()
        .h(px(30.0))
        .flex_none()
        .flex()
        .items_center()
        .gap(px(2.0))
        .px(px(10.0))
        .child(status([
            glyph(BRANCH, 13.0, ink, scale).into_any_element(),
            word("main"),
            quiet("↑2"),
        ]))
        .child(status([word("clear-sable-eagle")]))
        .child(spacer())
        .child(status([
            word("ctx"),
            div()
                .w(px(40.0))
                .h(px(5.0))
                .rounded(px(3.0))
                .bg(white(0.08))
                .overflow_hidden()
                .child(div().w(px(3.2)).h(px(5.0)).rounded(px(3.0)).bg(white(0.6)))
                .into_any_element(),
            word("20k"),
        ]))
        .child(status([word("claude-sub"), quiet("5h"), word("34%")]))
        .child(status([word("classifier 4")]))
        .child(status([word("cron 1")]))
}
