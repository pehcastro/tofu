use std::sync::Arc;

use gpui::{
    AnyElement, Div, FontWeight, Image, IntoElement, div, img, linear_color_stop, linear_gradient,
    prelude::*, px,
};

use super::paint::{T3, WARN, glyph, hex, medium, spacer, text, tint, white};

const SIDEBAR: &str = r#"<rect x="2" y="3" width="12" height="10" rx="2"/><path d="M6 3v10"/>"#;
const CHAT: &str = r#"<path d="M3 4.5A1.5 1.5 0 0 1 4.5 3h7A1.5 1.5 0 0 1 13 4.5v5a1.5 1.5 0 0 1-1.5 1.5H7l-3 2.5V11h.5"/>"#;
const PIN: &str = r#"<path d="M6 2h4l-.5 4 2 2H4.5l2-2zM8 8v6"/>"#;
const CODE: &str = r#"<path d="M5.5 5L2.5 8l3 3M10.5 5l3 3-3 3"/>"#;
const DATA: &str = r#"<path d="M3 4c0-1.1 2.2-2 5-2s5 .9 5 2v8c0 1.1-2.2 2-5 2s-5-.9-5-2zM3 4c0 1.1 2.2 2 5 2s5-.9 5-2"/>"#;
const SEARCH: &str = r#"<circle cx="7" cy="7" r="4"/><path d="M10 10l3 3"/>"#;
const BELL: &str = r#"<path d="M4 11V7a4 4 0 0 1 8 0v4l1 1.5H3zM6.5 14h3"/>"#;
const MINIMIZE: &str = r#"<path d="M4 8.5h8"/>"#;
const MAXIMIZE: &str = r#"<rect x="4" y="4" width="8" height="8" rx="2"/>"#;
const CLOSE: &str = r#"<path d="M4.5 4.5l7 7M11.5 4.5l-7 7"/>"#;
const BRANCH: &str = r#"<circle cx="5" cy="4" r="1.5"/><circle cx="5" cy="12" r="1.5"/><circle cx="11" cy="6" r="1.5"/><path d="M5 5.5v5M11 7.5c0 2-6 1.5-6 3"/>"#;
const DOWN: &str = r#"<path d="M5 6.5l3 3 3-3"/>"#;
const RIGHT: &str = r#"<path d="M6 4l4 4-4 4"/>"#;

pub const CONTROLS: [&str; 22] = [
    "Hides or shows the sidebar (ctrl b)",
    "Opens the work workspace.",
    "Opens the editor workspace.",
    "Opens the data workspace.",
    "Opens a new workspace with one empty tile: pick a module, or start from a preset (work 1+2, editor, data).",
    "Closes the Sub-agents screen and returns to the last workspace.",
    "Opens the command palette: files, sessions, screens, settings and commands in one search (alt k).",
    "Notifications: quiet-amber-heron wants to run rm -rf build/.",
    "Account: pehcastro, found through gh and git config.",
    "Minimizes tofu to the taskbar.",
    "Maximizes the window. Hold the pointer here for the Windows snap layouts.",
    "Closes the window. clear-sable-eagle is still working, so tofu asks before it stops the turn.",
    "Opens the notes-app project picker.",
    "Starts a new session in notes-app and opens its chat in the work workspace.",
    "The session on screen.",
    "Switches the desk to quiet-amber-heron: its chat and feeds replace these, the tabs stay.",
    "Opens History and branches",
    "Opens the Session screen",
    "Opens the Context screen",
    "Opens the Limits screen",
    "Opens the Classifier screen: 4 decisions this turn",
    "One scheduled job: quiet-amber-heron runs its loop every 10 minutes. Opens the schedule.",
];

pub const INACTIVE: [(&str, &str); 3] = [
    ("fond-sandy-mink", "2h"),
    ("tidy-ochre-wren", "1d"),
    ("crisp-azure-swift", "3d"),
];

pub struct Wire<'a> {
    pub scale: f32,
    pub inactive_open: bool,
    pub control: &'a dyn Fn(usize, Div) -> AnyElement,
    pub inactive: &'a dyn Fn(Div) -> AnyElement,
}

pub fn window(backdrop: &Arc<Image>, wire: &Wire, main: Div) -> Div {
    div()
        .relative()
        .size_full()
        .overflow_hidden()
        .font_family(super::paint::SANS)
        .text_size(px(14.0))
        .line_height(px(23.0))
        .text_color(white(0.9))
        .child(img(backdrop.clone()).absolute().size_full())
        .child(
            div()
                .absolute()
                .left(px(20.0))
                .top(px(18.0))
                .right(px(20.0))
                .bottom(px(18.0))
                .rounded(px(8.0))
                .overflow_hidden()
                .flex()
                .flex_col()
                .child(header(wire))
                .child(
                    div().flex_1().min_h_0().flex().child(sidebar(wire)).child(
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
                .child(footer(wire)),
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
                .pr(px(5.0))
                .child(glyph(icon, 13.0, ink, scale))
                .child(medium(13.0, 13.0, ink, label)),
        )
        .child(tail)
}

fn header(wire: &Wire) -> Div {
    let scale = wire.scale;
    let c = wire.control;
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
                .child(c(
                    0,
                    square(28.0, 7.0).child(glyph(SIDEBAR, 16.0, white(0.45), scale)),
                ))
                .child(
                    text(13.0, 13.0, white(0.75), "tofu")
                        .pl(px(6.0))
                        .font_weight(FontWeight::BOLD),
                ),
        )
        .child(c(
            1,
            header_tab(
                CHAT,
                "work",
                div()
                    .w(px(18.0))
                    .flex()
                    .justify_center()
                    .child(glyph(PIN, 11.0, white(T3), scale)),
                scale,
            ),
        ))
        .child(c(2, header_tab(CODE, "editor", hidden_close(), scale)))
        .child(c(3, header_tab(DATA, "data", hidden_close(), scale)))
        .child(c(
            4,
            div()
                .flex()
                .items_center()
                .h(px(30.0))
                .px(px(10.0))
                .rounded(px(9.0))
                .child(medium(13.0, 13.0, quiet, "+")),
        ))
        .child(
            div()
                .flex_none()
                .w(px(1.0))
                .h(px(16.0))
                .mx(px(6.0))
                .bg(white(0.12)),
        )
        .child(
            super::paint::ringed(9.0, white(0.12))
                .flex()
                .flex_none()
                .items_center()
                .gap(px(7.0))
                .h(px(30.0))
                .pl(px(11.0))
                .pr(px(6.0))
                .bg(white(0.07))
                .child(medium(13.0, 13.0, white(0.95), "Sub-agents"))
                .child(c(
                    5,
                    square(20.0, 7.0).child(medium(13.0, 13.0, white(0.45), "×")),
                )),
        )
        .child(spacer().h_full())
        .child(c(
            6,
            div()
                .flex()
                .items_center()
                .gap(px(8.0))
                .h(px(28.0))
                .px(px(11.0))
                .rounded(px(9.0))
                .child(glyph(SEARCH, 13.0, quiet, scale))
                .child(medium(13.0, 13.0, white(T3), "Alt K")),
        ))
        .child(c(
            7,
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
        ))
        .child(c(
            8,
            square(28.0, 0.0).child(
                square(22.0, 11.0)
                    .bg(linear_gradient(
                        135.0,
                        linear_color_stop(hex(0x6b7a8f), 0.0),
                        linear_color_stop(hex(0x3a4250), 1.0),
                    ))
                    .child(text(10.0, 10.0, white(0.9), "p").font_weight(FontWeight::SEMIBOLD)),
            ),
        ))
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(2.0))
                .mx(px(6.0))
                .children(
                    [(9, MINIMIZE), (10, MAXIMIZE), (11, CLOSE)].map(|(at, icon)| {
                        c(
                            at,
                            square(28.0, 8.0).w(px(34.0)).child(glyph(
                                icon,
                                13.0,
                                white(0.62),
                                scale,
                            )),
                        )
                    }),
                ),
        )
}

fn session_row(on: bool, live: bool, name: &'static str, tail: &'static str) -> Div {
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
        .when(live, |row| {
            row.child(text(8.0, 18.0, super::paint::LIVE, "●"))
        })
        .when(!live, |row| row.child(div().flex_none().w(px(8.0))))
        .child(
            div()
                .flex_1()
                .text_color(white(if on {
                    0.9
                } else if live {
                    0.6
                } else {
                    T3
                }))
                .child(name),
        )
        .child(text(11.5, 18.0, white(T3), tail))
}

fn sidebar(wire: &Wire) -> Div {
    let c = wire.control;
    div()
        .w(px(248.0))
        .flex_none()
        .flex()
        .flex_col()
        .px(px(8.0))
        .pt(px(2.0))
        .pb(px(10.0))
        .child(c(
            12,
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
                .child(glyph(DOWN, 13.0, white(T3), wire.scale)),
        ))
        .child(
            div()
                .flex()
                .items_center()
                .pt(px(16.0))
                .px(px(10.0))
                .pb(px(4.0))
                .child(medium(11.5, 16.0, white(T3), "Running").flex_1())
                .child(c(13, medium(11.5, 16.0, white(T3), "+"))),
        )
        .child(c(
            14,
            session_row(true, true, "clear-sable-eagle", "working"),
        ))
        .child(c(
            15,
            session_row(false, true, "quiet-amber-heron", "loop 10m"),
        ))
        .child((wire.inactive)(
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
                .child(glyph(
                    if wire.inactive_open { DOWN } else { RIGHT },
                    13.0,
                    white(T3),
                    wire.scale,
                ))
                .child(div().flex_1().child("Inactive"))
                .child(text(11.5, 18.0, white(T3), "3")),
        ))
        .when(wire.inactive_open, |side| {
            side.children(INACTIVE.map(|(name, tail)| session_row(false, false, name, tail)))
        })
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

fn footer(wire: &Wire) -> Div {
    let c = wire.control;
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
        .child(c(
            16,
            status([
                glyph(BRANCH, 13.0, ink, wire.scale).into_any_element(),
                word("main"),
                quiet("↑2"),
            ]),
        ))
        .child(c(17, status([word("clear-sable-eagle")])))
        .child(spacer())
        .child(c(
            18,
            status([
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
            ]),
        ))
        .child(c(
            19,
            status([word("claude-sub"), quiet("5h"), word("34%")]),
        ))
        .child(c(20, status([word("classifier 4")])))
        .child(c(21, status([word("cron 1")])))
}

pub fn toast(message: &'static str, close: AnyElement, scale: f32) -> Div {
    div()
        .absolute()
        .left_0()
        .right_0()
        .bottom(px(44.0))
        .flex()
        .justify_center()
        .child(
            super::paint::ringed(12.0, white(0.12))
                .flex()
                .items_center()
                .gap(px(10.0))
                .max_w(px(620.0))
                .py(px(9.0))
                .pl(px(14.0))
                .pr(px(10.0))
                .bg(tint(0x1e1d24, 0.96))
                .text_size(px(13.0))
                .line_height(px(18.0))
                .child(glyph(super::paint::ARROW, 13.0, white(T3), scale))
                .child(div().flex_1().child(message))
                .child(
                    text(11.0, 15.0, white(T3), "not drawn yet")
                        .px(px(7.0))
                        .py(px(2.0))
                        .rounded(px(999.0))
                        .bg(white(0.06)),
                )
                .child(close),
        )
}
