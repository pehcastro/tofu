use gpui::{AnyElement, Div, FontWeight, Rgba, SharedString, div, prelude::*, px};

use super::paint::{
    ADD, AGENT_FILL, AGENT_INK, CHECK, DEL, OPEN, RIGHT, T1, T3, TRACE, TRACE_MARK, black, glyph,
    medium, mono, ringed, spacer, spinner, text, tint, white,
};

pub const COLUMN: f32 = 760.0;

#[derive(Clone)]
pub enum Piece {
    Plain(SharedString),
    Code(SharedString),
}

#[derive(Clone)]
pub enum Block {
    Para(Vec<Piece>),
    Heading(SharedString),
    List(Vec<SharedString>),
}

#[derive(Clone, Copy)]
pub enum Mark {
    Prompt,
    Running,
}

#[derive(Clone, Copy)]
pub enum Outcome {
    Done,
    Running,
}

#[derive(Clone)]
pub enum Row {
    You {
        time: SharedString,
        text: SharedString,
        trace: bool,
    },
    Queued {
        text: SharedString,
    },
    Lead {
        time: SharedString,
        body: Vec<Block>,
    },
    Tools {
        count: SharedString,
        detail: SharedString,
        calls: Vec<[&'static str; 3]>,
    },
    Agent {
        outcome: Outcome,
        name: SharedString,
        summary: SharedString,
        link: bool,
    },
    Command {
        mark: Mark,
        command: SharedString,
        tail: Vec<(SharedString, Rgba)>,
    },
    Failure {
        head: SharedString,
        detail: SharedString,
        link: SharedString,
    },
    Note(SharedString),
    Foot(SharedString),
}

pub fn plain(body: impl Into<SharedString>) -> Piece {
    Piece::Plain(body.into())
}

pub fn code(body: impl Into<SharedString>) -> Piece {
    Piece::Code(body.into())
}

pub fn code_chip(body: SharedString) -> Div {
    mono(12.5, 21.0, white(T1), body)
        .h(px(21.0))
        .px(px(6.0))
        .rounded(px(6.0))
        .bg(white(0.08))
}

pub fn prose(pieces: &[Piece]) -> Div {
    match pieces {
        [Piece::Plain(only)] => div().child(only.clone()),
        _ => div()
            .flex()
            .flex_wrap()
            .items_center()
            .children(pieces.iter().flat_map(|piece| {
                match piece {
                    Piece::Plain(body) => body
                        .split_inclusive(' ')
                        .map(|word| {
                            div()
                                .flex_none()
                                .whitespace_nowrap()
                                .child(SharedString::from(word.to_owned()))
                                .into_any_element()
                        })
                        .collect::<Vec<_>>(),
                    Piece::Code(body) => vec![code_chip(body.clone()).into_any_element()],
                }
            })),
    }
}

pub fn agent_pill(name: SharedString) -> Div {
    medium(11.5, 11.5, AGENT_INK, name)
        .py(px(3.0))
        .px(px(7.0))
        .rounded(px(6.0))
        .bg(AGENT_FILL)
}

fn who(children: Vec<AnyElement>) -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(8.0))
        .mb(px(2.0))
        .text_size(px(11.5))
        .line_height(px(18.0))
        .text_color(white(0.45))
        .children(children)
}

fn lead_block(index: usize, block: &Block) -> Div {
    match block {
        Block::Para(pieces) => prose(pieces).when(index > 0, |para| para.mt(px(6.0))),
        Block::Heading(title) => div()
            .mt(px(10.0))
            .font_weight(FontWeight::SEMIBOLD)
            .child(title.clone()),
        Block::List(items) => div()
            .mt(px(4.0))
            .flex()
            .flex_col()
            .children(items.iter().map(|item| {
                div()
                    .relative()
                    .pl(px(20.0))
                    .child(
                        div()
                            .absolute()
                            .left(px(6.0))
                            .top(px(9.0))
                            .size(px(5.0))
                            .rounded_full()
                            .bg(white(0.9)),
                    )
                    .child(item.clone())
            })),
    }
}

fn tool_line() -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(8.0))
        .w_full()
        .text_size(px(13.0))
        .line_height(px(20.0))
        .text_color(white(0.55))
}

pub fn row(row: &Row, first: bool, scale: f32) -> Div {
    match row {
        Row::You {
            time,
            text: said,
            trace,
        } => div()
            .self_end()
            .max_w(px(520.0))
            .when(!first, |bubble| bubble.mt(px(22.0)))
            .pt(px(7.0))
            .px(px(14.0))
            .pb(px(10.0))
            .rounded(px(14.0))
            .bg(white(0.08))
            .child(who([
                div().child("You").into_any_element(),
                text(11.5, 18.0, white(T3), time.clone()).into_any_element(),
            ]
            .into_iter()
            .chain(trace.then(|| glyph(TRACE_MARK, 13.0, TRACE, scale).into_any_element()))
            .collect()))
            .child(said.clone()),
        Row::Queued { text: said } => ringed(14.0, white(0.14))
            .self_end()
            .max_w(px(520.0))
            .mt(px(16.0))
            .pt(px(7.0))
            .px(px(14.0))
            .pb(px(10.0))
            .child(who(vec![
                div().child("You").into_any_element(),
                div().child("queued").into_any_element(),
            ]))
            .child(said.clone()),
        Row::Lead { time, body } => div()
            .mt(px(14.0))
            .child(who(vec![
                agent_pill("lead".into()).into_any_element(),
                text(11.5, 18.0, white(T3), time.clone()).into_any_element(),
            ]))
            .children(
                body.iter()
                    .enumerate()
                    .map(|(index, block)| lead_block(index, block)),
            ),
        Row::Tools {
            count,
            detail,
            calls,
        } => div()
            .flex()
            .flex_col()
            .child(
                tool_line()
                    .mt(px(10.0))
                    .child(glyph(
                        if calls.is_empty() { RIGHT } else { OPEN },
                        13.0,
                        white(0.55),
                        scale,
                    ))
                    .child(count.clone())
                    .child(div().text_color(white(T3)).child(detail.clone())),
            )
            .when(!calls.is_empty(), |line| {
                line.child(
                    div()
                        .mt(px(6.0))
                        .ml(px(21.0))
                        .py(px(8.0))
                        .px(px(12.0))
                        .rounded(px(9.0))
                        .bg(black(0.2))
                        .flex()
                        .flex_col()
                        .gap(px(3.0))
                        .children(calls.iter().map(|[verb, what, tail]| {
                            div()
                                .flex()
                                .items_center()
                                .gap(px(8.0))
                                .child(text(12.5, 19.0, white(T3), *verb).w(px(44.0)))
                                .child(mono(12.5, 19.0, white(T1), *what).flex_1())
                                .child(text(12.0, 19.0, white(T3), *tail))
                        })),
                )
            }),
        Row::Agent {
            outcome,
            name,
            summary,
            link,
        } => tool_line()
            .mt(px(10.0))
            .text_color(white(0.85))
            .child(match outcome {
                Outcome::Done => glyph(CHECK, 13.0, ADD, scale),
                Outcome::Running => spinner(11.0, scale),
            })
            .child(agent_pill(name.clone()))
            .child(div().text_color(white(T3)).child(summary.clone()))
            .when(*link, |line| {
                line.child(spacer())
                    .child(text(12.0, 20.0, white(T3), "open in Sub-agents"))
            }),
        Row::Command {
            mark,
            command,
            tail,
        } => tool_line()
            .mt(px(10.0))
            .child(match mark {
                Mark::Prompt => mono(13.0, 20.0, white(T3), "⟩")
                    .w(px(13.0))
                    .flex()
                    .justify_center()
                    .into_any_element(),
                Mark::Running => spinner(11.0, scale).into_any_element(),
            })
            .child(mono(12.5, 20.0, white(0.55), command.clone()))
            .children(
                tail.iter()
                    .map(|(word, color)| text(12.5, 20.0, *color, word.clone())),
            ),
        Row::Failure { head, detail, link } => ringed(9.0, tint(0xf1737d, 0.18))
            .mt(px(6.0))
            .flex()
            .items_center()
            .gap(px(8.0))
            .py(px(6.0))
            .px(px(10.0))
            .bg(tint(0xf1737d, 0.07))
            .text_size(px(13.0))
            .line_height(px(20.0))
            .child(
                mono(13.0, 20.0, DEL, "!")
                    .w(px(13.0))
                    .flex()
                    .justify_center(),
            )
            .child(
                div()
                    .flex_1()
                    .flex()
                    .whitespace_nowrap()
                    .child(format!("{head} "))
                    .child(div().text_color(white(T3)).child(detail.clone())),
            )
            .child(text(12.0, 20.0, white(T3), link.clone())),
        Row::Note(note) => div()
            .mt(px(6.0))
            .text_size(px(13.0))
            .line_height(px(20.0))
            .text_color(white(0.45))
            .child(note.clone()),
        Row::Foot(foot) => div()
            .mt(px(8.0))
            .text_size(px(11.5))
            .text_color(white(0.3))
            .child(foot.clone()),
    }
}
