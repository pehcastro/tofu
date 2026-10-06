use gpui::{Div, FontWeight, Rgba, Stateful, div, prelude::*, px};

use super::fixture::{
    CONSOLE, CONSOLE_TAIL, DELETE_STEP, Engine, NOTES, STEPS, TARGET, TELL_ATTACH, TELL_BACK,
    TELL_COPY_SELECTOR, TELL_EFFORT, TELL_FORWARD, TELL_HAND_TAB, TELL_MENTION_STEP, TELL_MODEL,
    TELL_RELOAD,
};
use super::paint::{
    AGENT, CHAT, INK, LEFT, LINK, LIVE, MONO, PAPER, RIGHT, SHELL, T2, T3, UP, WARN, black, cursor,
    glyph, hex, medium, mono, ring, ringed, shadow, spacer, square, text, tint, white,
};
use super::{Act, Browser, Tab};
use gpui::Context;

const GLOBE: &str = r#"<circle cx="8" cy="8" r="5.5"/><path d="M2.5 8h11M8 2.5c2 2 2 9 0 11M8 2.5c-2 2-2 9 0 11"/>"#;
const PROMPT: &str = r#"<path d="M3 5l3 3-3 3M8 11h5"/>"#;
const RELOAD: &str = r#"<path d="M12.5 8A4.5 4.5 0 1 1 11 4.6M12.5 3v2.5H10"/>"#;
const POINTER: &str = r#"<path d="M3 3l4 10 1.5-4L12.5 7.5z"/>"#;
const AT: &str = r#"<circle cx="8" cy="8" r="2.5"/><path d="M10.5 8v1a1.75 1.75 0 0 0 3.5 0V8a6 6 0 1 0-2.4 4.8"/>"#;
const PLUS: &str = r#"<path d="M8 3v10M3 8h10"/>"#;
const SEND: &str = r#"<path d="M8 12.5v-9M4.5 7L8 3.5 11.5 7"/>"#;
const BADGE: &str = "5 notes";

fn shell() -> Div {
    ringed(12.0, white(0.06))
        .flex()
        .flex_col()
        .min_h_0()
        .min_w_0()
        .px(px(3.0))
        .pb(px(3.0))
        .bg(SHELL)
}

fn inner() -> Div {
    div()
        .flex_1()
        .min_h_0()
        .flex()
        .flex_col()
        .overflow_hidden()
        .rounded(px(11.0))
}

fn icon_button(
    id: &'static str,
    icon: &str,
    size: f32,
    act: Act,
    scale: f32,
    cx: &mut Context<Browser>,
) -> Stateful<Div> {
    Browser::hit(
        id,
        square(size, 7.0).child(glyph(icon, 13.0, white(0.45), scale)),
        act,
        cx,
    )
}

fn button(label: &'static str, height: f32, size: f32) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .gap(px(6.0))
        .h(px(height))
        .px(px(12.0))
        .rounded(px(8.0))
        .bg(white(0.07))
        .text_size(px(size))
        .line_height(px(size))
        .font_weight(FontWeight::MEDIUM)
        .child(label)
}

fn bubble(max: f32) -> Div {
    div()
        .self_end()
        .max_w(px(max))
        .bg(white(0.08))
        .rounded(px(14.0))
        .py(px(9.0))
        .px(px(14.0))
}

fn chip(label: &'static str) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(6.0))
        .h(px(24.0))
        .px(px(9.0))
        .rounded(px(7.0))
        .text_size(px(12.5))
        .line_height(px(12.5))
        .font_weight(FontWeight::MEDIUM)
        .text_color(white(T2))
        .child(label)
}

impl Browser {
    pub(super) fn chat_tile(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        let step = &STEPS[self.step];
        let short = format!("{} · step {} of {}", step.short, self.step + 1, STEPS.len());
        let picked = self.picked.then(|| {
            bubble(320.0)
                .flex()
                .flex_wrap()
                .child("why does ")
                .child(
                    mono(12.5, 21.0, LINK, "span.badge")
                        .px(px(6.0))
                        .rounded(px(6.0))
                        .bg(tint(0x9db8f0, 0.15)),
                )
                .child(" say 5 after the delete?")
        });
        shell()
            .child(
                div()
                    .h(px(28.0))
                    .flex_none()
                    .flex()
                    .items_center()
                    .gap(px(6.0))
                    .pl(px(9.0))
                    .pr(px(6.0))
                    .child(glyph(CHAT, 13.0, white(T2), scale))
                    .child(medium(12.0, 12.0, white(0.55), "Chat"))
                    .child(spacer())
                    .child(text(12.0, 12.0, white(T3), "clear-sable-eagle")),
            )
            .child(
                inner()
                    .child(
                        div()
                            .flex_1()
                            .overflow_hidden()
                            .pt(px(14.0))
                            .px(px(22.0))
                            .flex()
                            .flex_col()
                            .gap(px(14.0))
                            .text_size(px(13.5))
                            .line_height(px(21.0))
                            .child(bubble(300.0).child("Delete a note in the web client and check the badge goes down."))
                            .child(div().child("The browser sub-agent is doing it in the Browser tile; it reports what the badge says before and after."))
                            .child(
                                div()
                                    .flex()
                                    .items_center()
                                    .gap(px(9.0))
                                    .text_size(px(13.0))
                                    .child(text(8.0, 21.0, LIVE, "●"))
                                    .child("browser")
                                    .child(div().text_color(white(T3)).child(short))
                                    .child(spacer())
                                    .child(Browser::hit(
                                        "mention-step",
                                        square(22.0, 7.0).child(glyph(AT, 13.0, white(0.45), scale)),
                                        Act::Tell(TELL_MENTION_STEP),
                                        cx,
                                    )),
                            )
                            .children(picked),
                    )
                    .child(
                        div().pt(px(10.0)).px(px(12.0)).pb(px(12.0)).child(
                            ringed(16.0, white(0.09))
                                .bg(black(0.22))
                                .p(px(6.0))
                                .flex()
                                .items_center()
                                .gap(px(4.0))
                                .child(icon_button("attach", PLUS, 28.0, Act::Tell(TELL_ATTACH), scale, cx))
                                .child(
                                    text(14.0, 21.0, white(T3), "Ask, or pick an element in the page to mention it")
                                        .flex_1()
                                        .min_w_0()
                                        .overflow_hidden()
                                        .text_ellipsis()
                                        .pl(px(2.0)),
                                )
                                .child(Browser::hit(
                                    "model",
                                    chip("Opus 5").child(glyph(UP, 13.0, white(T3), scale)),
                                    Act::Tell(TELL_MODEL),
                                    cx,
                                ))
                                .child(Browser::hit("effort", chip("Medium"), Act::Tell(TELL_EFFORT), cx))
                                .child(
                                    square(28.0, 14.0)
                                        .bg(white(0.18))
                                        .child(glyph(SEND, 16.0, black(0.6), scale)),
                                ),
                        ),
                    ),
            )
    }

    pub(super) fn browser_tile(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        let tab = |id: &'static str,
                   on: bool,
                   icon: &str,
                   label: &'static str,
                   tail: Option<Div>,
                   which: Tab,
                   cx: &mut Context<Self>| {
            let ink = if on { white(1.0) } else { white(0.5) };
            Browser::hit(
                id,
                div()
                    .flex()
                    .items_center()
                    .gap(px(6.0))
                    .h(px(31.0))
                    .px(px(9.0))
                    .rounded_t(px(9.0))
                    .when(on, |tab| tab.bg(white(0.05)))
                    .child(glyph(icon, 13.0, ink, scale))
                    .child(medium(12.5, 12.5, ink, label))
                    .children(tail),
                Act::Tab(which),
                cx,
            )
        };
        let engines = Engine::ALL.map(|engine| {
            let on = engine == self.engine;
            Browser::hit(
                engine.label(),
                text(
                    12.0,
                    18.0,
                    if on { white(1.0) } else { white(0.5) },
                    engine.label(),
                )
                .py(px(3.0))
                .px(px(10.0))
                .rounded(px(6.0))
                .when(on, |button| button.bg(white(0.12))),
                Act::Engine(engine),
                cx,
            )
        });
        let head = div()
            .h(px(36.0))
            .flex_none()
            .flex()
            .items_end()
            .gap(px(2.0))
            .pl(px(12.0))
            .pr(px(6.0))
            .child(tab(
                "tab-page",
                self.tab == Tab::Page,
                GLOBE,
                "Browser",
                None,
                Tab::Page,
                cx,
            ))
            .child(tab(
                "tab-console",
                self.tab == Tab::Console,
                PROMPT,
                "Console",
                Some(medium(12.5, 12.5, WARN, "1")),
                Tab::Console,
                cx,
            ))
            .child(spacer())
            .child(
                div()
                    .self_center()
                    .flex()
                    .gap(px(2.0))
                    .p(px(2.0))
                    .rounded(px(8.0))
                    .bg(white(0.04))
                    .children(engines),
            );
        let toolbar = div()
            .h(px(45.0))
            .flex_none()
            .flex()
            .items_center()
            .gap(px(4.0))
            .px(px(8.0))
            .border_b_1()
            .border_color(black(0.35))
            .child(icon_button(
                "back",
                LEFT,
                26.0,
                Act::Tell(TELL_BACK),
                scale,
                cx,
            ))
            .child(icon_button(
                "forward",
                RIGHT,
                26.0,
                Act::Tell(TELL_FORWARD),
                scale,
                cx,
            ))
            .child(icon_button(
                "reload",
                RELOAD,
                26.0,
                Act::Tell(TELL_RELOAD),
                scale,
                cx,
            ))
            .child(
                div()
                    .flex_1()
                    .flex()
                    .items_center()
                    .gap(px(8.0))
                    .h(px(30.0))
                    .px(px(10.0))
                    .mx(px(4.0))
                    .rounded(px(9.0))
                    .bg(black(0.28))
                    .text_size(px(13.0))
                    .child(text(8.0, 18.0, LIVE, "●"))
                    .child(
                        div()
                            .flex()
                            .child(mono(13.0, 18.0, white(0.9), "localhost:5871"))
                            .child(mono(13.0, 18.0, white(T3), "/notes")),
                    )
                    .child(spacer())
                    .child(text(12.0, 18.0, white(T3), self.engine.line())),
            )
            .child(Browser::hit(
                "pick",
                button("Pick", 30.0, 12.5)
                    .when(self.picking, |pick| {
                        pick.bg(tint(0x9db8f0, 0.2)).text_color(LINK)
                    })
                    .child(glyph(
                        POINTER,
                        13.0,
                        if self.picking { LINK } else { white(0.9) },
                        scale,
                    )),
                Act::Pick,
                cx,
            ));
        let banner = (self.engine == Engine::Chrome).then(|| {
            div()
                .flex_none()
                .flex()
                .items_center()
                .gap(px(10.0))
                .py(px(8.0))
                .px(px(14.0))
                .bg(tint(0xe8c98a, 0.07))
                .border_b_1()
                .border_color(tint(0xe8c98a, 0.2))
                .text_size(px(12.5))
                .child(div().flex_1().text_color(WARN).child(
                    "This is your real Chrome tab, with your logins. tofu only touches the tab you handed it: localhost:5871.",
                ))
                .child(Browser::hit("hand-tab", button("Hand over another tab", 26.0, 12.0), Act::Tell(TELL_HAND_TAB), cx))
        });
        let body = match self.tab {
            Tab::Page => self.page(scale, cx),
            Tab::Console => console(),
        };
        let (dot, label, long) = if self.took {
            (
                WARN,
                "you",
                format!(
                    "You have the page. The browser sub-agent waits and resumes from step {} when you hand back.",
                    self.step + 1
                ),
            )
        } else {
            (AGENT, "browser", STEPS[self.step].long.to_string())
        };
        let footer = div()
            .h(px(47.0))
            .flex_none()
            .flex()
            .items_center()
            .gap(px(10.0))
            .px(px(14.0))
            .border_t_1()
            .border_color(black(0.35))
            .text_size(px(13.0))
            .child(div().size(px(8.0)).rounded(px(4.0)).bg(dot))
            .child(label)
            .child(
                text(13.0, 23.0, white(T3), long)
                    .flex_1()
                    .min_w_0()
                    .overflow_hidden()
                    .text_ellipsis(),
            )
            .child(icon_button("prev", LEFT, 26.0, Act::Prev, scale, cx))
            .child(mono(
                12.0,
                23.0,
                white(T3),
                format!("{} / {}", self.step + 1, STEPS.len()),
            ))
            .child(icon_button("next", RIGHT, 26.0, Act::Next, scale, cx))
            .child(Browser::hit(
                "take",
                button(
                    if self.took { "Hand back" } else { "Take over" },
                    28.0,
                    13.0,
                ),
                Act::Take,
                cx,
            ));
        shell().child(head).child(
            inner()
                .child(toolbar)
                .children(banner)
                .child(
                    div()
                        .flex_1()
                        .min_h_0()
                        .p(px(12.0))
                        .flex()
                        .relative()
                        .child(body),
                )
                .child(footer),
        )
    }

    fn page(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        let deleted = self.step > DELETE_STEP;
        let aiming = self.step == DELETE_STEP;
        let ringing = self.picking || self.step == 1 || self.step == 3;
        let badge_ring = if self.picking {
            vec![ring(6.0, tint(0x9db8f0, 0.25)), ring(2.0, LINK)]
        } else {
            vec![ring(2.0, AGENT)]
        };
        let rows = NOTES
            .iter()
            .filter(|(title, _)| !deleted || *title != TARGET)
            .map(|&(title, at)| {
                let marked = aiming && title == TARGET;
                div()
                    .flex()
                    .items_center()
                    .gap(px(12.0))
                    .h(px(45.0))
                    .border_b_1()
                    .border_color(hex(0xece9e2))
                    .when(marked, |row| row.bg(tint(0x8b6cf0, 0.08)))
                    .child(
                        div()
                            .size(px(14.0))
                            .rounded(px(4.0))
                            .border(px(1.5))
                            .border_color(hex(0xc9c4b8)),
                    )
                    .child(div().flex_1().child(title))
                    .child(text(12.0, 22.0, hex(0x8a857a), at))
                    .child(
                        text(12.5, 22.0, hex(0xaa3333), "Delete")
                            .py(px(3.0))
                            .px(px(9.0))
                            .rounded(px(7.0))
                            .when(marked, |button| {
                                button.bg(hex(0xffffff)).shadow(vec![ring(2.0, AGENT)])
                            }),
                    )
            });
        let cursor_top = if aiming { 120.0 } else { 28.0 };
        let agent = (!self.took && self.step < STEPS.len() - 1).then(|| {
            div()
                .absolute()
                .right(px(20.0))
                .top(px(cursor_top))
                .flex()
                .items_center()
                .gap(px(6.0))
                .child(cursor(scale))
                .child(
                    medium(11.5, 11.5, hex(0xffffff), "browser")
                        .py(px(4.0))
                        .px(px(7.0))
                        .rounded(px(6.0))
                        .bg(AGENT),
                )
        });
        let picker = self.picking.then(|| {
            div()
                .absolute()
                .right(px(16.0))
                .top(px(60.0))
                .w(px(240.0))
                .py(px(10.0))
                .px(px(12.0))
                .rounded(px(10.0))
                .bg(tint(0x1e1d24, 0.96))
                .shadow(shadow(34.0, 14.0, 0.35))
                .text_color(white(0.9))
                .text_size(px(12.5))
                .line_height(px(18.0))
                .flex()
                .flex_col()
                .gap(px(8.0))
                .child(mono(12.5, 18.0, LINK, "span.badge"))
                .child(text(
                    12.5,
                    18.0,
                    white(T2),
                    format!("\"{BADGE}\" · header › nav › span"),
                ))
                .child(
                    div()
                        .flex()
                        .gap(px(6.0))
                        .child(Browser::hit(
                            "mention",
                            button("Mention in chat", 26.0, 12.0)
                                .bg(white(0.9))
                                .text_color(hex(0x111111)),
                            Act::Mention,
                            cx,
                        ))
                        .child(Browser::hit(
                            "copy-selector",
                            button("Copy selector", 26.0, 12.0),
                            Act::Tell(TELL_COPY_SELECTOR),
                            cx,
                        )),
                )
        });
        div()
            .flex_1()
            .relative()
            .rounded(px(10.0))
            .overflow_hidden()
            .bg(PAPER)
            .text_color(INK)
            .text_size(px(14.0))
            .line_height(px(22.0))
            .child(
                div()
                    .h(px(55.0))
                    .flex()
                    .items_center()
                    .gap(px(12.0))
                    .px(px(26.0))
                    .border_b_1()
                    .border_color(hex(0xe6e3dc))
                    .child(text(17.0, 22.0, INK, "Notes").font_weight(FontWeight::SEMIBOLD))
                    .child(spacer())
                    .child(
                        text(12.5, 18.0, PAPER, BADGE)
                            .py(px(3.0))
                            .px(px(10.0))
                            .rounded_full()
                            .bg(INK)
                            .when(ringing, |badge| badge.shadow(badge_ring)),
                    ),
            )
            .child(
                div()
                    .py(px(14.0))
                    .px(px(26.0))
                    .flex()
                    .flex_col()
                    .children(rows),
            )
            .children(agent)
            .children(picker)
    }
}

fn console() -> Div {
    let line = |at: &'static str, said: &'static str, code: Option<&'static str>, warn: bool| {
        let ink: Rgba = if warn { WARN } else { white(0.65) };
        div()
            .flex()
            .child(text(12.0, 20.0, white(T3), at).font_family(MONO))
            .child(text(12.0, 20.0, ink, format!("  {said}")).font_family(MONO))
            .children(code.map(|code| mono(12.0, 20.0, LIVE, code)))
            .when(code == Some("200"), |line| {
                line.child(mono(12.0, 20.0, ink, CONSOLE_TAIL))
            })
    };
    div()
        .flex_1()
        .rounded(px(10.0))
        .bg(black(0.3))
        .py(px(12.0))
        .px(px(14.0))
        .children(CONSOLE.map(|(at, said, code, warn)| line(at, said, code, warn)))
}
