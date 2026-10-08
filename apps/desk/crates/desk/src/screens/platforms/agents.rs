use gpui::{Div, div, prelude::*, px, relative};

use super::fixture::{
    AGENTS, Agent, DIFF, EDITED, GROUPS, RAN, READ, SHEET_AGENT, SHEET_STATE, State, TABLE_HEAD,
    THINKING,
};
use super::glyph::{Glyph, glyph};
use super::paint::{
    ADD_INK, CAPTION, DEL_INK, FAINT, GREEN, MONO, RED, RING_DARK, STRONG, WARN, avatar, caption,
    halo, ink, kind_color, medium, mono, rgb, ring, strong, text, tint, words,
};

const AGENT_WIDTH: f32 = 108.0;
const TIME_WIDTH: f32 = 84.0;
const ROW: f32 = 40.0;
const GROUP: f32 = 25.0;
const SHEET_SHARE: f32 = 0.66;
const SHEET_INSET: f32 = 6.0;
const EVENT_INDENT: f32 = 36.0;
const RING: f32 = 22.0;
const RING_ARC: f32 = 4.0;

fn columns(agent: Div, doing: Div, time: Div) -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(8.0))
        .px(px(16.0))
        .child(div().flex_none().w(px(AGENT_WIDTH)).min_w_0().child(agent))
        .child(div().flex_1().min_w_0().child(doing))
        .child(div().flex_none().w(px(TIME_WIDTH)).child(time))
}

fn mark(agent: &Agent) -> Div {
    let working = agent.state == State::Working;
    let wait = match agent.state {
        State::Working => None,
        State::Asking => Some(WARN),
        State::Failed => Some(RED),
    };
    let circle = || div().size(px(RING)).rounded_full().border(px(1.5));
    div()
        .relative()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(RING))
        .when(working, |mark| {
            mark.child(
                circle()
                    .absolute()
                    .inset_0()
                    .border_color(rgb(134, 224, 179, 0.18)),
            )
            .child(
                div()
                    .absolute()
                    .inset_0()
                    .h(px(RING_ARC))
                    .overflow_hidden()
                    .child(circle().border_color(rgb(134, 224, 179, 1.0))),
            )
        })
        .child(avatar(&agent.kind, 16.0, 8.0))
        .children(wait.map(|color| {
            div()
                .absolute()
                .inset_0()
                .flex()
                .items_end()
                .justify_end()
                .child(
                    div()
                        .size(px(9.0))
                        .rounded_full()
                        .bg(color)
                        .shadow(vec![halo(RING_DARK, 2.0)]),
                )
        }))
}

fn row(agent: &Agent) -> Div {
    columns(
        div()
            .flex()
            .items_center()
            .gap(px(6.0))
            .child(mark(agent))
            .child(
                text(agent.name, 12.5, kind_color(&agent.kind))
                    .flex_shrink_1()
                    .min_w_0()
                    .truncate(),
            ),
        div()
            .flex()
            .flex_col()
            .child(text(agent.doing, 12.5, STRONG).truncate())
            .child(text(agent.now, 11.5, FAINT).truncate()),
        text(agent.time, 11.5, FAINT),
    )
    .flex_none()
    .h(px(ROW))
}

pub fn table() -> Div {
    let mut body = div().flex().flex_col().child(
        columns(
            caption(TABLE_HEAD[0]),
            caption(TABLE_HEAD[1]),
            caption(TABLE_HEAD[2]),
        )
        .h(px(GROUP)),
    );
    for (state, name, count) in GROUPS {
        body = body
            .child(
                div()
                    .flex()
                    .flex_none()
                    .items_center()
                    .gap(px(6.0))
                    .h(px(GROUP))
                    .px(px(16.0))
                    .child(glyph(Glyph::Down, 11.0, CAPTION))
                    .child(caption(name))
                    .child(
                        strong(count, 10.0, tint(CAPTION, CAPTION.alpha * 0.7)).font_family(MONO),
                    ),
            )
            .children(AGENTS.iter().filter(|agent| agent.state == state).map(row));
    }
    body
}

fn event(verb: &'static str, time: &'static str) -> Div {
    let agent = &AGENTS[SHEET_AGENT];
    div()
        .flex()
        .items_center()
        .gap(px(10.0))
        .child(avatar(&agent.kind, 24.0, 12.0))
        .child(strong(agent.name, 13.0, kind_color(&agent.kind)))
        .child(text(verb, 13.0, FAINT))
        .child(div().flex_1())
        .child(text(time, 11.5, FAINT))
}

fn block() -> Div {
    div()
        .ml(px(EVENT_INDENT))
        .rounded(px(10.0))
        .overflow_hidden()
        .bg(rgb(0, 0, 0, 0.26))
        .shadow(vec![ring(ink(0.06))])
}

pub fn sheet() -> Div {
    let agent = &AGENTS[SHEET_AGENT];
    let diff = DIFF.iter().map(|(added, number, code)| {
        let (fill, ink_color, sign) = if *added {
            (rgb(82, 198, 142, 0.1), rgb(198, 236, 214, 1.0), "+")
        } else {
            (tint(RED, 0.1), rgb(245, 192, 196, 1.0), "-")
        };
        div()
            .flex()
            .items_center()
            .h(px(20.0))
            .bg(fill)
            .child(
                div()
                    .w(px(42.0))
                    .flex()
                    .justify_end()
                    .child(mono(*number, 12.0, ink(0.22))),
            )
            .child(
                div()
                    .w(px(26.0))
                    .pl(px(12.0))
                    .opacity(0.7)
                    .child(mono(sign, 12.0, ink_color)),
            )
            .child(mono(*code, 12.0, ink_color).truncate())
    });
    div()
        .absolute()
        .left_0()
        .right_0()
        .bottom_0()
        .m(px(SHEET_INSET))
        .h(relative(SHEET_SHARE))
        .flex()
        .flex_col()
        .rounded(px(14.0))
        .overflow_hidden()
        .bg(rgb(27, 26, 33, 0.97))
        .shadow(vec![ring(ink(0.1))])
        .child(
            div()
                .flex()
                .justify_center()
                .pt(px(6.0))
                .child(div().w(px(36.0)).h(px(4.0)).rounded(px(2.0)).bg(ink(0.18))),
        )
        .child(
            div()
                .id("platforms-sheet")
                .flex_1()
                .min_h_0()
                .overflow_y_scroll()
                .flex()
                .flex_col()
                .gap(px(12.0))
                .px(px(16.0))
                .py(px(10.0))
                .child(
                    div()
                        .flex()
                        .items_center()
                        .gap(px(12.0))
                        .child(avatar(&agent.kind, 22.0, 11.0))
                        .child(
                            div()
                                .flex_1()
                                .min_w_0()
                                .flex()
                                .flex_col()
                                .child(
                                    div()
                                        .flex()
                                        .items_center()
                                        .gap(px(8.0))
                                        .child(strong(agent.name, 14.0, kind_color(&agent.kind)))
                                        .child(text(SHEET_STATE, 12.0, GREEN))
                                        .child(text(format!("· {}", agent.time), 12.0, FAINT)),
                                )
                                .child(text(agent.doing, 13.0, STRONG).truncate()),
                        )
                        .child(
                            div()
                                .flex()
                                .flex_none()
                                .items_center()
                                .h(px(26.0))
                                .px(px(12.0))
                                .rounded(px(8.0))
                                .bg(ink(0.07))
                                .child(medium("Expand", 12.0, STRONG)),
                        ),
                )
                .child(
                    div()
                        .flex()
                        .flex_col()
                        .gap(px(2.0))
                        .px(px(12.0))
                        .py(px(8.0))
                        .rounded(px(10.0))
                        .bg(ink(0.035))
                        .child(caption("thinking"))
                        .child(words(THINKING, 13.0, 20.0, ink(0.6)).italic()),
                )
                .child(event(RAN.0, RAN.1))
                .child(
                    block()
                        .flex()
                        .flex_col()
                        .gap(px(4.0))
                        .p(px(12.0))
                        .child(
                            div()
                                .flex()
                                .items_center()
                                .gap(px(8.0))
                                .child(mono("$", 12.5, FAINT))
                                .child(mono(RAN.2, 12.0, STRONG))
                                .child(div().flex_1())
                                .child(
                                    div()
                                        .px(px(7.0))
                                        .rounded(px(6.0))
                                        .bg(tint(GREEN, 0.12))
                                        .child(mono(RAN.3, 11.0, GREEN)),
                                ),
                        )
                        .child(mono(RAN.4, 11.5, ink(0.55)).pl(px(14.0)).truncate()),
                )
                .child(event(EDITED.0, EDITED.1))
                .child(
                    block()
                        .flex()
                        .flex_col()
                        .pb(px(4.0))
                        .child(
                            div()
                                .flex()
                                .items_center()
                                .gap(px(8.0))
                                .h(px(37.0))
                                .px(px(12.0))
                                .bg(ink(0.024))
                                .child(div().flex_1().child(mono(EDITED.2, 12.0, STRONG)))
                                .child(mono(EDITED.3, 11.5, ADD_INK))
                                .child(mono(EDITED.4, 11.5, DEL_INK)),
                        )
                        .children(diff),
                )
                .child(event(READ.0, READ.1))
                .child(
                    div()
                        .ml(px(EVENT_INDENT))
                        .self_start()
                        .flex()
                        .items_center()
                        .gap(px(10.0))
                        .h(px(24.0))
                        .px(px(10.0))
                        .rounded(px(7.0))
                        .bg(ink(0.05))
                        .shadow(vec![ring(ink(0.05))])
                        .child(mono(READ.2, 12.0, STRONG))
                        .child(text(READ.3, 11.5, FAINT)),
                ),
        )
}
