use gpui::{Div, div, prelude::*, px};

use super::fixture::{
    AGENTS, Agent, DIFF, EDITED, GROUPS, RAN, READ, SHEET_AGENT, SHEET_STATE, State, TABLE_HEAD,
    THINKING,
};
use super::glyph::{Glyph, glyph_at};
use super::paint::{
    ADD_INK, CAPTION, DEL_INK, FAINT, GREEN, MONO, RED, RING_DARK, STRONG, WARN, at, avatar,
    caption, halo, ink, kind_color, medium, mono, rgb, ring, strong, text, tint, words,
};

const LEFT: f32 = 17.0;
const DOING: f32 = 125.0;
const TIME_FROM_RIGHT: f32 = 101.0;
const HEAD_TOP: f32 = 38.0;
const GROUP: f32 = 25.0;
const ROW: f32 = 40.0;
const INNER_TOP: f32 = 36.0;
const INNER_INSET: f32 = 3.0;
const SHEET_SHARE: f32 = 0.66;
const SHEET_INSET: f32 = 6.0;
const EVENT_LEFT: f32 = 52.0;
const EVENT_RIGHT: f32 = 16.0;

fn row(agent: &Agent, top: f32, width: f32) -> Vec<Div> {
    let color = kind_color(&agent.kind);
    let mut out = vec![
        at(avatar(&agent.kind, 16.0, 8.0), LEFT, top + 12.0),
        at(text(agent.name, 12.5, color), 41.0, top + 11.5),
        at(text(agent.doing, 12.5, STRONG), DOING, top + 3.0),
        at(text(agent.now, 11.5, FAINT), DOING, top + 21.0),
        at(
            text(agent.time, 11.5, FAINT),
            width - TIME_FROM_RIGHT,
            top + 12.5,
        ),
    ];
    let wait = match agent.state {
        State::Working => None,
        State::Asking => Some(WARN),
        State::Failed => Some(RED),
    };
    out.extend(wait.map(|color| {
        at(
            div()
                .size(px(9.0))
                .rounded_full()
                .bg(color)
                .shadow(vec![halo(RING_DARK, 2.0)]),
            26.0,
            top + 26.0,
        )
    }));
    out
}

pub fn table(width: f32, height: f32) -> Div {
    let mut cells = vec![
        at(caption(TABLE_HEAD[0]), LEFT, HEAD_TOP + 6.0 - INNER_TOP),
        at(caption(TABLE_HEAD[1]), DOING, HEAD_TOP + 6.0 - INNER_TOP),
        at(
            caption(TABLE_HEAD[2]),
            width - TIME_FROM_RIGHT,
            HEAD_TOP + 6.0 - INNER_TOP,
        ),
    ];
    let mut top = HEAD_TOP + 22.0 - INNER_TOP;
    for (state, name, count) in GROUPS {
        cells.push(glyph_at(Glyph::Down, 11.0, CAPTION, 0.4, top + 10.0));
        cells.push(at(
            div()
                .flex()
                .gap(px(6.0))
                .child(caption(name))
                .child(strong(count, 10.0, tint(CAPTION, CAPTION.alpha * 0.7)).font_family(MONO)),
            34.0,
            top + 8.5,
        ));
        top += GROUP;
        for agent in AGENTS.iter().filter(|agent| agent.state == state) {
            cells.extend(row(agent, top, width));
            top += ROW;
        }
    }
    at(
        div()
            .w(px(width))
            .h(px(height - INNER_TOP - INNER_INSET))
            .overflow_hidden()
            .children(cells),
        0.0,
        INNER_TOP,
    )
}

fn event(verb: &str, time: &str, top: f32, width: f32) -> Vec<Div> {
    let agent = &AGENTS[SHEET_AGENT];
    vec![
        at(avatar(&agent.kind, 24.0, 12.0), 16.0, top + 9.0),
        at(
            strong(agent.name, 13.0, kind_color(&agent.kind)),
            EVENT_LEFT,
            top + 10.0,
        ),
        at(text(verb.to_owned(), 13.0, FAINT), 112.7, top + 10.0),
        at(
            div()
                .w(px(width - EVENT_LEFT - 37.0))
                .flex()
                .justify_end()
                .child(text(time.to_owned(), 11.5, FAINT)),
            EVENT_LEFT,
            top + 11.0,
        ),
    ]
}

fn block(width: f32, height: f32) -> Div {
    div()
        .w(px(width - EVENT_LEFT - EVENT_RIGHT))
        .h(px(height))
        .rounded(px(10.0))
        .overflow_hidden()
        .bg(rgb(0, 0, 0, 0.26))
        .shadow(vec![ring(ink(0.06))])
}

pub fn sheet(width: f32, height: f32) -> Div {
    let agent = &AGENTS[SHEET_AGENT];
    let color = kind_color(&agent.kind);
    let w = width - 2.0 * (INNER_INSET + SHEET_INSET);
    let h = (height - INNER_TOP - INNER_INSET) * SHEET_SHARE;
    let mut cells = vec![
        at(
            div().w(px(36.0)).h(px(4.0)).rounded(px(2.0)).bg(ink(0.18)),
            (w - 36.0) / 2.0,
            6.0,
        ),
        at(avatar(&agent.kind, 22.0, 11.0), 16.0, 26.5),
        at(
            div()
                .flex()
                .gap(px(8.0))
                .child(strong(agent.name, 14.0, color))
                .child(div().pt(px(2.0)).child(text(SHEET_STATE, 12.0, GREEN)))
                .child(
                    div()
                        .pt(px(2.0))
                        .child(text(format!("· {}", agent.time), 12.0, FAINT)),
                ),
            50.0,
            18.0,
        ),
        at(text(agent.doing, 13.0, STRONG), 50.0, 39.0),
        at(
            div()
                .w(px(85.0))
                .h(px(26.0))
                .rounded(px(8.0))
                .bg(ink(0.07))
                .pl(px(31.0))
                .pt(px(5.0))
                .child(medium("Expand", 12.0, STRONG)),
            w - 139.0,
            24.5,
        ),
        at(
            div()
                .w(px(w - 32.0))
                .h(px(94.0))
                .rounded(px(10.0))
                .bg(ink(0.035))
                .px(px(12.0))
                .pt(px(8.0))
                .child(caption("thinking"))
                .child(words(THINKING, 13.0, 20.0, ink(0.6)).italic().mt(px(2.0))),
            16.0,
            67.0,
        ),
    ];
    cells.extend(event(RAN.0, RAN.1, 161.0, w));
    cells.push(at(
        block(w, 67.0)
            .child(at(mono("$", 12.5, FAINT), 12.0, 12.0))
            .child(at(mono(RAN.2, 12.0, STRONG), 27.5, 12.0))
            .child(
                div()
                    .absolute()
                    .right(px(12.0))
                    .top(px(7.0))
                    .h(px(27.0))
                    .px(px(7.0))
                    .pt(px(6.0))
                    .rounded(px(6.0))
                    .bg(tint(GREEN, 0.12))
                    .child(mono(RAN.3, 11.0, GREEN)),
            )
            .child(at(mono(RAN.4, 11.5, ink(0.55)), 26.0, 42.0)),
        EVENT_LEFT,
        196.0,
    ));
    cells.extend(event(EDITED.0, EDITED.1, 272.0, w));
    let mut diff = block(w, 105.0)
        .child(
            div()
                .absolute()
                .left_0()
                .right_0()
                .top_0()
                .h(px(37.0))
                .bg(ink(0.024)),
        )
        .child(at(mono(EDITED.2, 12.0, STRONG), 33.0, 10.0))
        .child(
            div()
                .absolute()
                .right(px(12.0))
                .top(px(11.0))
                .flex()
                .gap(px(8.0))
                .child(mono(EDITED.3, 11.5, ADD_INK))
                .child(mono(EDITED.4, 11.5, DEL_INK)),
        );
    for (index, (added, number, code)) in DIFF.iter().enumerate() {
        let (fill, ink_color, sign) = if *added {
            (rgb(82, 198, 142, 0.1), rgb(198, 236, 214, 1.0), "+")
        } else {
            (tint(RED, 0.1), rgb(245, 192, 196, 1.0), "-")
        };
        diff =
            diff.child(at(
                div()
                    .w_full()
                    .h(px(20.0))
                    .bg(fill)
                    .pt(px(2.0))
                    .flex()
                    .child(div().w(px(42.0)).flex().justify_end().child(mono(
                        *number,
                        12.0,
                        ink(0.22),
                    )))
                    .child(
                        div()
                            .w(px(26.0))
                            .pl(px(12.0))
                            .opacity(0.7)
                            .child(mono(sign, 12.0, ink_color)),
                    )
                    .child(mono(*code, 12.0, ink_color)),
                0.0,
                41.0 + 20.0 * index as f32,
            ));
    }
    cells.push(at(diff, EVENT_LEFT, 307.0));
    cells.extend(event(READ.0, READ.1, 421.0, w));
    cells.push(at(
        div()
            .w(px(209.5))
            .h(px(24.0))
            .rounded(px(7.0))
            .bg(ink(0.05))
            .shadow(vec![ring(ink(0.05))])
            .child(at(mono(READ.2, 12.0, STRONG), 27.0, 3.5))
            .child(at(text(READ.3, 11.5, FAINT), 133.8, 4.5)),
        EVENT_LEFT,
        455.0,
    ));
    at(
        div()
            .w(px(w))
            .h(px(h))
            .rounded(px(14.0))
            .overflow_hidden()
            .bg(rgb(27, 26, 33, 0.97))
            .shadow(vec![ring(ink(0.1))])
            .children(cells),
        INNER_INSET + SHEET_INSET,
        height - INNER_INSET - SHEET_INSET - h,
    )
}
