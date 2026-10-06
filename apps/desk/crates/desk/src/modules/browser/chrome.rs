use gpui::{
    Div, FontWeight, IntoElement, Rgba, Stateful, TextTransform, div, img, linear_color_stop,
    linear_gradient, prelude::*, px,
};

use super::fixture::{
    TELL_CHANGELOG, TELL_CLOSE, TELL_CLOSE_DATA, TELL_CLOSE_EDITOR, TELL_CRON, TELL_DOCS,
    TELL_LINK, TELL_MAXIMIZE, TELL_MINIMIZE, TELL_NEW_SESSION, TELL_NEW_WORKSPACE, TELL_PALETTE,
    TELL_REOPEN, TELL_SWITCH,
};
use super::paint::{
    CHAT, CROSS, DOWN, LIVE, POP, RIGHT, SANS, T2, T3, WARN, glyph, hex, medium, ringed, shadow,
    spacer, square, text, white,
};
use super::{Act, Browser};
use gpui::Context;

const SIDEBAR: &str = r#"<rect x="2" y="3" width="12" height="10" rx="2"/><path d="M6 3v10"/>"#;
const PIN: &str = r#"<path d="M6 2h4l-.5 4 2 2H4.5l2-2zM8 8v6"/>"#;
const CODE: &str = r#"<path d="M5.5 5L2.5 8l3 3M10.5 5l3 3-3 3"/>"#;
const DATA: &str = r#"<path d="M3 4c0-1.1 2.2-2 5-2s5 .9 5 2v8c0 1.1-2.2 2-5 2s-5-.9-5-2zM3 4c0 1.1 2.2 2 5 2s5-.9 5-2"/>"#;
const SEARCH: &str = r#"<circle cx="7" cy="7" r="4"/><path d="M10 10l3 3"/>"#;
const BELL: &str = r#"<path d="M4 11V7a4 4 0 0 1 8 0v4l1 1.5H3zM6.5 14h3"/>"#;
const MINIMIZE: &str = r#"<path d="M4 8.5h8"/>"#;
const MAXIMIZE: &str = r#"<rect x="4" y="4" width="8" height="8" rx="2"/>"#;
const BRANCH: &str = r#"<circle cx="5" cy="4" r="1.5"/><circle cx="5" cy="12" r="1.5"/><circle cx="11" cy="6" r="1.5"/><path d="M5 5.5v5M11 7.5c0 2-6 1.5-6 3"/>"#;

const BOARD: [f32; 2] = [1440.0, 900.0];
const INSET: [f32; 2] = [20.0, 18.0];
const POP_RIGHT: f32 = 150.0;
const POP_TOP: f32 = 56.0;

fn tell(id: &'static str, el: Div, said: &'static str, cx: &mut Context<Browser>) -> Stateful<Div> {
    Browser::hit(id, el, Act::Tell(said), cx)
}

fn header_tab(
    on: bool,
    icon: &str,
    label: &'static str,
    tail: impl IntoElement,
    scale: f32,
) -> Div {
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

fn caps(body: &'static str, top: f32) -> Div {
    text(10.0, 10.0, white(0.45), body)
        .font_weight(FontWeight::SEMIBOLD)
        .letter_spacing(px(0.7))
        .text_transform(TextTransform::Uppercase)
        .pt(px(top))
        .pb(px(6.0))
        .px(px(10.0))
}

fn pop(width: f32) -> Div {
    ringed(12.0, white(0.12))
        .absolute()
        .right(px(POP_RIGHT))
        .top(px(POP_TOP))
        .w(px(width))
        .p(px(6.0))
        .flex()
        .flex_col()
        .bg(POP)
        .shadow(shadow(50.0, 22.0, 0.6))
        .text_size(px(13.0))
}

fn pop_row() -> Div {
    div()
        .flex()
        .items_start()
        .gap(px(10.0))
        .py(px(7.0))
        .px(px(10.0))
        .rounded(px(8.0))
        .line_height(px(18.0))
}

fn notice(dot: Rgba, ink: Rgba, title: Div, under: &'static str) -> Div {
    pop_row().child(text(8.0, 18.0, dot, "●")).child(
        div()
            .flex_1()
            .flex()
            .flex_col()
            .text_color(ink)
            .child(title)
            .child(text(12.0, 18.0, white(T3), under)),
    )
}

impl Browser {
    pub(super) fn window(&self, main: Div, scale: f32, cx: &mut Context<Self>) -> Div {
        let [left, top] = INSET;
        let header = self.header(scale, cx);
        let sidebar = self.sidebar(scale, cx);
        let footer = footer(scale, cx);
        let bell = self.bell.then(|| bell_pop(cx));
        let account = self.account.then(|| account_pop(cx));
        div()
            .relative()
            .w(px(BOARD[0]))
            .h(px(BOARD[1]))
            .overflow_hidden()
            .font_family(SANS)
            .text_size(px(14.0))
            .line_height(px(23.0))
            .text_color(white(0.9))
            .child(img(self.backdrop.clone()).absolute().size_full())
            .child(
                div()
                    .absolute()
                    .left(px(left))
                    .top(px(top))
                    .right(px(left))
                    .bottom(px(top))
                    .flex()
                    .flex_col()
                    .child(header)
                    .child(
                        div().flex_1().min_h_0().flex().child(sidebar).child(
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
                    .child(footer),
            )
            .children(bell)
            .children(account)
    }

    fn header(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        let quiet = white(0.55);
        let close = |id: &'static str, said: &'static str, cx: &mut Context<Self>| {
            tell(id, square(18.0, 5.0), said, cx)
        };
        let left = div()
            .w(px(if self.side { 232.0 } else { 50.0 }))
            .flex_none()
            .h_full()
            .flex()
            .items_center()
            .gap(px(4.0))
            .pl(px(10.0))
            .child(Browser::hit(
                "side",
                square(28.0, 7.0).child(glyph(SIDEBAR, 16.0, white(0.45), scale)),
                Act::Side,
                cx,
            ))
            .when(self.side, |left| {
                left.child(
                    text(13.0, 13.0, white(0.75), "tofu")
                        .pl(px(6.0))
                        .font_weight(FontWeight::BOLD),
                )
            });
        div()
            .h(px(40.0))
            .flex_none()
            .flex()
            .items_center()
            .gap(px(3.0))
            .child(left)
            .child(tell(
                "work",
                header_tab(
                    true,
                    CHAT,
                    "work",
                    div().w(px(18.0)).flex().justify_center().child(glyph(
                        PIN,
                        11.0,
                        white(T3),
                        scale,
                    )),
                    scale,
                ),
                TELL_LINK,
                cx,
            ))
            .child(tell(
                "editor",
                header_tab(
                    false,
                    CODE,
                    "editor",
                    close("close-editor", TELL_CLOSE_EDITOR, cx),
                    scale,
                ),
                TELL_LINK,
                cx,
            ))
            .child(tell(
                "data",
                header_tab(
                    false,
                    DATA,
                    "data",
                    close("close-data", TELL_CLOSE_DATA, cx),
                    scale,
                ),
                TELL_LINK,
                cx,
            ))
            .child(tell(
                "new-workspace",
                div()
                    .flex()
                    .items_center()
                    .h(px(30.0))
                    .px(px(10.0))
                    .rounded(px(9.0))
                    .child(medium(13.0, 13.0, quiet, "+")),
                TELL_NEW_WORKSPACE,
                cx,
            ))
            .child(spacer().h_full())
            .child(tell(
                "palette",
                div()
                    .flex()
                    .items_center()
                    .gap(px(8.0))
                    .h(px(28.0))
                    .px(px(11.0))
                    .rounded(px(9.0))
                    .child(glyph(SEARCH, 13.0, quiet, scale))
                    .child(medium(13.0, 13.0, white(T3), "Alt K")),
                TELL_PALETTE,
                cx,
            ))
            .child(Browser::hit(
                "bell",
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
                Act::Bell,
                cx,
            ))
            .child(Browser::hit(
                "account",
                square(28.0, 0.0).child(
                    square(22.0, 11.0)
                        .bg(linear_gradient(
                            135.0,
                            linear_color_stop(hex(0x6b7a8f), 0.0),
                            linear_color_stop(hex(0x3a4250), 1.0),
                        ))
                        .child(text(10.0, 10.0, white(0.9), "p").font_weight(FontWeight::SEMIBOLD)),
                ),
                Act::Account,
                cx,
            ))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(2.0))
                    .mx(px(6.0))
                    .children(
                        [
                            ("minimize", MINIMIZE, TELL_MINIMIZE),
                            ("maximize", MAXIMIZE, TELL_MAXIMIZE),
                            ("close", CROSS, TELL_CLOSE),
                        ]
                        .map(|(id, icon, said)| {
                            tell(
                                id,
                                square(28.0, 8.0).w(px(34.0)).child(glyph(
                                    icon,
                                    13.0,
                                    white(0.62),
                                    scale,
                                )),
                                said,
                                cx,
                            )
                        }),
                    ),
            )
    }

    fn sidebar(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        if !self.side {
            return div().w(px(8.0)).flex_none();
        }
        let session = |on: bool, name: &'static str, tail: &'static str| {
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
                        .when(!on, |name| name.text_color(white(T2)))
                        .child(name),
                )
                .child(text(11.5, 18.0, white(T3), tail))
        };
        let quiet_row = || {
            div()
                .flex()
                .items_center()
                .gap(px(9.0))
                .py(px(7.0))
                .px(px(10.0))
                .rounded(px(9.0))
                .text_size(px(13.0))
                .line_height(px(18.0))
                .text_color(white(T3))
        };
        let reopen = self.inactive.then(|| {
            TELL_REOPEN.map(|(name, ago, said)| {
                tell(
                    name,
                    quiet_row()
                        .child(div().w(px(8.0)))
                        .child(div().flex_1().child(name))
                        .child(text(11.5, 18.0, white(T3), ago)),
                    said,
                    cx,
                )
            })
        });
        div()
            .w(px(248.0))
            .flex_none()
            .flex()
            .flex_col()
            .px(px(8.0))
            .pt(px(2.0))
            .pb(px(10.0))
            .child(tell(
                "project",
                div()
                    .flex()
                    .items_center()
                    .gap(px(10.0))
                    .py(px(8.0))
                    .px(px(10.0))
                    .rounded(px(10.0))
                    .child(
                        square(26.0, 8.0).bg(white(0.1)).child(
                            text(12.0, 12.0, white(0.9), "n").font_weight(FontWeight::SEMIBOLD),
                        ),
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
                TELL_LINK,
                cx,
            ))
            .child(
                div()
                    .flex()
                    .items_center()
                    .pt(px(16.0))
                    .px(px(10.0))
                    .pb(px(4.0))
                    .child(medium(11.5, 16.0, white(T3), "Running").flex_1())
                    .child(tell(
                        "new-session",
                        medium(11.5, 16.0, white(T3), "+"),
                        TELL_NEW_SESSION,
                        cx,
                    )),
            )
            .child(tell(
                "on-session",
                session(true, "clear-sable-eagle", "working"),
                TELL_LINK,
                cx,
            ))
            .child(tell(
                "switch-session",
                session(false, "quiet-amber-heron", "loop 10m"),
                TELL_SWITCH,
                cx,
            ))
            .child(Browser::hit(
                "inactive",
                quiet_row()
                    .mt(px(6.0))
                    .child(glyph(
                        if self.inactive { DOWN } else { RIGHT },
                        13.0,
                        white(T3),
                        scale,
                    ))
                    .child(div().flex_1().child("Inactive"))
                    .child(text(11.5, 18.0, white(T3), "3")),
                Act::Inactive,
                cx,
            ))
            .children(reopen.into_iter().flatten())
    }
}

fn status(
    id: &'static str,
    said: &'static str,
    children: Vec<gpui::AnyElement>,
    cx: &mut Context<Browser>,
) -> Stateful<Div> {
    tell(
        id,
        div()
            .flex()
            .flex_none()
            .items_center()
            .gap(px(6.0))
            .h(px(22.0))
            .px(px(8.0))
            .rounded(px(6.0))
            .children(children),
        said,
        cx,
    )
}

fn footer(scale: f32, cx: &mut Context<Browser>) -> Div {
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
        .child(status(
            "branch",
            TELL_LINK,
            vec![
                glyph(BRANCH, 13.0, ink, scale).into_any_element(),
                word("main"),
                quiet("↑2"),
            ],
            cx,
        ))
        .child(status(
            "session",
            TELL_LINK,
            vec![word("clear-sable-eagle")],
            cx,
        ))
        .child(spacer())
        .child(status(
            "ctx",
            TELL_LINK,
            vec![
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
            ],
            cx,
        ))
        .child(status(
            "limits",
            TELL_LINK,
            vec![word("claude-sub"), quiet("5h"), word("34%")],
            cx,
        ))
        .child(status(
            "classifier",
            TELL_LINK,
            vec![word("classifier 4")],
            cx,
        ))
        .child(status("cron", TELL_CRON, vec![word("cron 1")], cx))
}

fn bell_pop(cx: &mut Context<Browser>) -> Div {
    pop(330.0)
        .child(caps("Needs you", 8.0))
        .child(tell(
            "notice-approval",
            notice(
                WARN,
                white(0.9),
                div()
                    .flex()
                    .child("quiet-amber-heron wants to run ")
                    .child(div().font_family(super::paint::MONO).child("rm -rf build/")),
                "waiting 2m · opens the approval",
            ),
            TELL_LINK,
            cx,
        ))
        .child(caps("Earlier", 10.0))
        .child(tell(
            "notice-ts",
            notice(
                white(T3),
                white(T2),
                div().child("ts-dev finished: sort order is now by date"),
                "12m · opens Sub-agents",
            ),
            TELL_LINK,
            cx,
        ))
        .child(tell(
            "notice-limits",
            notice(
                white(T3),
                white(T2),
                div().child("claude-sub · work is back in 40m"),
                "1h · opens Limits",
            ),
            TELL_LINK,
            cx,
        ))
}

fn account_pop(cx: &mut Context<Browser>) -> Div {
    let row = |label: &'static str, tail: Div| {
        pop_row()
            .items_center()
            .child(div().flex_1().child(label))
            .child(tail)
    };
    pop(250.0)
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(10.0))
                .pt(px(8.0))
                .px(px(10.0))
                .pb(px(10.0))
                .line_height(px(17.0))
                .child(square(32.0, 16.0).bg(linear_gradient(
                    135.0,
                    linear_color_stop(hex(0x6b7a8f), 0.0),
                    linear_color_stop(hex(0x3a4250), 1.0),
                )))
                .child(div().flex().flex_col().child("pehcastro").child(text(
                    12.0,
                    17.0,
                    white(T3),
                    "found through gh and git config",
                ))),
        )
        .child(tell(
            "accounts",
            row(
                "Accounts and models",
                text(12.0, 18.0, white(T3), "5 accounts"),
            ),
            TELL_LINK,
            cx,
        ))
        .child(tell(
            "settings",
            row(
                "Settings",
                text(11.0, 16.0, white(0.5), "ctrl ,")
                    .font_family(super::paint::MONO)
                    .px(px(6.0))
                    .py(px(1.0))
                    .rounded(px(5.0))
                    .bg(white(0.08)),
            ),
            TELL_LINK,
            cx,
        ))
        .child(div().h(px(1.0)).bg(white(0.07)).my(px(4.0)).mx(px(6.0)))
        .child(caps("Resources", 6.0))
        .child(tell(
            "changelog",
            pop_row().text_color(white(T2)).child("What's new in 0.5.1"),
            TELL_CHANGELOG,
            cx,
        ))
        .child(tell(
            "docs",
            pop_row().text_color(white(T2)).child("Docs"),
            TELL_DOCS,
            cx,
        ))
}
