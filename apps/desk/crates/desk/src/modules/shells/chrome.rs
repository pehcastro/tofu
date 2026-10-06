use std::sync::Arc;

use gpui::{
    AnyElement, Div, FontWeight, Image, IntoElement, div, img, linear_color_stop, linear_gradient,
    prelude::*, px,
};

use super::paint::{
    CLOSE, DOWN, LIVE, RIGHT, SANS, T3, WARN, glyph, hex, medium, spacer, square, text, white,
};

const SIDEBAR: &str = r#"<rect x="2" y="3" width="12" height="10" rx="2"/><path d="M6 3v10"/>"#;
const CHAT: &str = r#"<path d="M3 4.5A1.5 1.5 0 0 1 4.5 3h7A1.5 1.5 0 0 1 13 4.5v5a1.5 1.5 0 0 1-1.5 1.5H7l-3 2.5V11h.5"/>"#;
const PIN: &str = r#"<path d="M6 2h4l-.5 4 2 2H4.5l2-2zM8 8v6"/>"#;
const CODE: &str = r#"<path d="M5.5 5L2.5 8l3 3M10.5 5l3 3-3 3"/>"#;
const DATA: &str = r#"<path d="M3 4c0-1.1 2.2-2 5-2s5 .9 5 2v8c0 1.1-2.2 2-5 2s-5-.9-5-2zM3 4c0 1.1 2.2 2 5 2s5-.9 5-2"/>"#;
const SEARCH: &str = r#"<circle cx="7" cy="7" r="4"/><path d="M10 10l3 3"/>"#;
const BELL: &str = r#"<path d="M4 11V7a4 4 0 0 1 8 0v4l1 1.5H3zM6.5 14h3"/>"#;
const MINIMIZE: &str = r#"<path d="M4 8.5h8"/>"#;
const MAXIMIZE: &str = r#"<rect x="4" y="4" width="8" height="8" rx="2"/>"#;
const BRANCH: &str = r#"<circle cx="5" cy="4" r="1.5"/><circle cx="5" cy="12" r="1.5"/><circle cx="11" cy="6" r="1.5"/><path d="M5 5.5v5M11 7.5c0 2-6 1.5-6 3"/>"#;

const BOARD_WIDTH: f32 = 1440.0;
const BOARD_HEIGHT: f32 = 900.0;
const WINDOW_INSET: [f32; 2] = [20.0, 18.0];

pub fn window(backdrop: &Arc<Image>, scale: f32, main: Div, toast: Option<AnyElement>) -> Div {
    let [left, top] = WINDOW_INSET;
    div()
        .relative()
        .w(px(BOARD_WIDTH))
        .h(px(BOARD_HEIGHT))
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
                    div().flex_1().min_h_0().flex().child(sidebar(scale)).child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .flex()
                            .flex_col()
                            .pt(px(2.0))
                            .pr(px(8.0))
                            .child(main),
                    ),
                )
                .child(footer(scale))
                .children(toast),
        )
}

fn header_tab(icon: &str, label: &'static str, tail: Div, scale: f32) -> Div {
    let ink = white(0.55);
    div()
        .flex()
        .flex_none()
        .items_center()
        .h(px(28.0))
        .pr(px(3.0))
        .rounded(px(8.0))
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
                .w(px(232.0))
                .flex_none()
                .h_full()
                .flex()
                .items_center()
                .gap(px(4.0))
                .pl(px(10.0))
                .child(square(28.0, 7.0).child(glyph(SIDEBAR, 16.0, white(0.45), scale)))
                .child(
                    text(13.0, 13.0, white(0.75), "tofu")
                        .pl(px(6.0))
                        .font_weight(FontWeight::BOLD),
                ),
        )
        .child(header_tab(
            CHAT,
            "work",
            div()
                .w(px(18.0))
                .flex()
                .justify_center()
                .child(glyph(PIN, 11.0, white(T3), scale)),
            scale,
        ))
        .child(header_tab(CODE, "editor", hidden_close(), scale))
        .child(header_tab(DATA, "data", hidden_close(), scale))
        .child(
            div()
                .flex()
                .items_center()
                .h(px(30.0))
                .px(px(10.0))
                .rounded(px(9.0))
                .child(medium(13.0, 13.0, quiet, "+")),
        )
        .child(
            div()
                .flex_none()
                .w(px(1.0))
                .h(px(16.0))
                .mx(px(6.0))
                .bg(white(0.12)),
        )
        .child(
            div()
                .flex()
                .flex_none()
                .items_center()
                .gap(px(7.0))
                .h(px(30.0))
                .pl(px(11.0))
                .pr(px(6.0))
                .rounded(px(9.0))
                .bg(white(0.07))
                .border_1()
                .border_color(white(0.12))
                .child(medium(13.0, 13.0, white(0.95), "Shells"))
                .child(square(20.0, 7.0).child(glyph(CLOSE, 11.0, white(0.45), scale))),
        )
        .child(spacer().h_full())
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(8.0))
                .h(px(28.0))
                .px(px(11.0))
                .rounded(px(9.0))
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

fn session_row(on: bool, name: &'static str, tail: &'static str) -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(9.0))
        .py(px(7.0))
        .px(px(10.0))
        .rounded(px(9.0))
        .text_size(px(13.0))
        .line_height(px(18.0))
        .when(on, |row| row.bg(white(0.08)))
        .child(text(8.0, 18.0, LIVE, "●"))
        .child(
            div()
                .flex_1()
                .when(!on, |name| name.text_color(white(0.6)))
                .child(name),
        )
        .child(text(11.5, 18.0, white(T3), tail))
}

fn sidebar(scale: f32) -> Div {
    div()
        .w(px(248.0))
        .flex_none()
        .flex()
        .flex_col()
        .px(px(8.0))
        .pt(px(2.0))
        .pb(px(10.0))
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(10.0))
                .py(px(8.0))
                .px(px(10.0))
                .rounded(px(10.0))
                .child(
                    square(26.0, 8.0)
                        .bg(white(0.1))
                        .child(text(12.0, 12.0, white(0.9), "n").font_weight(FontWeight::SEMIBOLD)),
                )
                .child(
                    div()
                        .flex_1()
                        .flex()
                        .flex_col()
                        .child(
                            text(14.0, 17.0, white(0.9), "notes-app")
                                .font_weight(FontWeight::SEMIBOLD),
                        )
                        .child(text(12.0, 16.0, white(T3), "main · 3 changed")),
                )
                .child(glyph(DOWN, 13.0, white(T3), scale)),
        )
        .child(
            div()
                .flex()
                .items_center()
                .pt(px(16.0))
                .px(px(10.0))
                .pb(px(4.0))
                .child(medium(11.5, 16.0, white(T3), "Running").flex_1())
                .child(medium(11.5, 16.0, white(T3), "+")),
        )
        .child(session_row(true, "clear-sable-eagle", "working"))
        .child(session_row(false, "quiet-amber-heron", "loop 10m"))
        .child(
            div()
                .mt(px(6.0))
                .flex()
                .items_center()
                .gap(px(9.0))
                .py(px(7.0))
                .px(px(10.0))
                .text_size(px(13.0))
                .line_height(px(18.0))
                .text_color(white(T3))
                .child(glyph(RIGHT, 13.0, white(T3), scale))
                .child(div().flex_1().child("Inactive"))
                .child(text(11.5, 18.0, white(T3), "3")),
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
