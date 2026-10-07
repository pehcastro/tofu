mod fixture;

#[cfg(not(any(
    feature = "screen-usage",
    feature = "screen-limits",
    feature = "screen-context"
)))]
#[path = "../usage/frame.rs"]
pub mod frame;
#[cfg(all(
    feature = "screen-context",
    not(any(feature = "screen-usage", feature = "screen-limits"))
))]
use crate::screens::context::frame;
#[cfg(all(feature = "screen-limits", not(feature = "screen-usage")))]
use crate::screens::limits::frame;
#[cfg(feature = "screen-usage")]
use crate::screens::usage::frame;

use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{caption, dots, inner_card, outer_card};
use desk_ui::components::chip::{Tone, mono};
use desk_ui::components::list::row;
use desk_ui::components::paint::ink;
use desk_ui::components::size::{T2, T3};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Div, FontWeight, Rgba, Window, div, prelude::*,
    px, relative,
};

use fixture::{
    BRANCH_NOTE, COMPACT, FORK_HERE, FORKS, HEAD, LANES, MENTION, NAME, OPEN_WORK, OVERVIEW,
    Outcome, PICK_NOTE, RENAME, RIBBON_NOTE, SEGMENTS, STARTED, STATS, STEP, Segment, TURNS, UNDO,
};
use frame::{note, panel, title, told, window};

const CARD_SHADE: f32 = 0.22;
const RIBBON_ON: f32 = 0.22;
const RIBBON_OFF: f32 = 0.08;
const DOT_ON: f32 = 0.9;
const DOT_OFF: f32 = 0.45;
const LABEL_OFF: f32 = 0.55;
const RULE: f32 = 0.35;
const WARN_FILL: f32 = 0.07;

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    let forks = match board {
        None | Some(OVERVIEW) => false,
        Some(FORKS) => true,
        Some(other) => {
            return Err(format!(
                "the session screen draws {OVERVIEW} and {FORKS}, not {other}"
            ));
        }
    };
    frame::load_fonts(cx)?;
    let step = SEGMENTS
        .get(HEAD)
        .map_or(0, |head| head.turns.len().saturating_sub(1));
    Ok(cx
        .new(|_| Session {
            forks,
            turn: 0,
            segment: HEAD,
            step,
            branch: false,
            told: None,
        })
        .into())
}

struct Session {
    forks: bool,
    turn: usize,
    segment: usize,
    step: usize,
    branch: bool,
    told: Option<&'static str>,
}

impl Session {
    fn tell(
        &self,
        id: &'static str,
        label: &'static str,
        text: &'static str,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> impl IntoElement {
        button(id, label, None, ButtonKind::Plain, theme).on_click(cx.listener(
            move |session, _: &ClickEvent, _, cx| {
                session.told = Some(text);
                cx.notify();
            },
        ))
    }

    fn overview(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let header = div()
            .flex()
            .flex_none()
            .items_center()
            .gap(px(10.0))
            .px(px(4.0))
            .child(
                div()
                    .text_size(px(9.0))
                    .text_color(theme.color(ColorToken::StatusLive))
                    .child("●"),
            )
            .child(title(NAME))
            .child(
                div()
                    .text_size(px(13.0))
                    .text_color(ink(theme, T3))
                    .child(STARTED),
            )
            .child(div().flex_1())
            .child(self.tell("rename", "Rename", RENAME, theme, cx))
            .child(self.tell("compact", "Compact", COMPACT, theme, cx))
            .child(
                button("forks", "Forks", None, ButtonKind::Plain, theme).on_click(cx.listener(
                    |session, _: &ClickEvent, _, cx| {
                        session.forks = true;
                        cx.notify();
                    },
                )),
            );
        let stats = div()
            .flex()
            .flex_none()
            .gap(px(10.0))
            .children(STATS.iter().map(|(label, value)| {
                let tail = match *label {
                    "Files changed" => Some(
                        div()
                            .flex()
                            .gap(px(4.0))
                            .text_size(px(13.0))
                            .font_weight(FontWeight::NORMAL)
                            .child(div().text_color(Tone::Added.color(theme)).child("+412"))
                            .child(div().text_color(Tone::Deleted.color(theme)).child("-96")),
                    ),
                    "Tokens" => Some(
                        div()
                            .text_size(px(13.0))
                            .font_weight(FontWeight::NORMAL)
                            .text_color(ink(theme, T3))
                            .child("quota"),
                    ),
                    _ => None,
                };
                outer_card(theme)
                    .flex_1()
                    .min_w_0()
                    .flex_col()
                    .px(px(3.0))
                    .pb(px(3.0))
                    .child(
                        div()
                            .h(px(30.0))
                            .flex()
                            .items_center()
                            .pl(px(9.0))
                            .child(caption(*label, theme)),
                    )
                    .child(
                        inner_card(theme)
                            .flex_row()
                            .items_baseline()
                            .gap(px(4.0))
                            .px(px(14.0))
                            .py(px(10.0))
                            .text_size(px(24.0))
                            .line_height(relative(1.0))
                            .font_weight(FontWeight::SEMIBOLD)
                            .child(*value)
                            .children(tail),
                    )
            }));
        let turns = panel("Turns", Some(note("newest first · click one", theme).into_any_element()), theme)
            .flex_1()
            .min_w_0()
            .child(
                inner_card(theme)
                    .p(px(8.0))
                    .gap(px(2.0))
                    .text_size(px(13.5))
                    .children(TURNS.iter().enumerate().map(|(at, turn)| {
                        let (word, color) = match turn.outcome {
                            Outcome::Running => ("running", theme.color(ColorToken::StatusLive)),
                            Outcome::Stopped => ("stopped", ink(theme, T3)),
                            Outcome::LoopGuard => ("loop guard", theme.color(ColorToken::StatusWarn)),
                        };
                        row(("turn", at), at == self.turn, false, theme)
                            .gap(px(12.0))
                            .on_click(cx.listener(move |session, _: &ClickEvent, _, cx| {
                                session.turn = at;
                                cx.notify();
                            }))
                            .child(div().w(px(44.0)).font_family(mono(theme)).text_size(px(12.0)).text_color(ink(theme, T3)).child(turn.at))
                            .child(div().flex_1().child(turn.title))
                            .child(div().text_size(px(12.5)).text_color(ink(theme, T3)).child(turn.meta))
                            .child(div().w(px(84.0)).flex().justify_end().text_size(px(12.5)).text_color(color).child(word))
                    }))
                    .child(div().flex_1())
                    .child(
                        div()
                            .flex()
                            .gap(px(8.0))
                            .p(px(6.0))
                            .child(fact("Cron", &[("every 30m", true), (" rerun the browser check, until 18:00", false)], theme))
                            .child(fact("Undo", &[("14 turns kept, oldest Oct 1; tofu's own snapshots, never your .git", false)], theme))
                            .child(fact("Context", &[("84k of 250k, forks at 200k", false)], theme)),
                    ),
            );
        let turn = TURNS.get(self.turn).unwrap_or(&TURNS[0]);
        let detail = panel(
            turn.title,
            Some(note(turn.when, theme).into_any_element()),
            theme,
        )
        .w(px(440.0))
        .flex_none()
        .child(
            inner_card(theme)
                .px(px(16.0))
                .py(px(14.0))
                .gap(px(12.0))
                .text_size(px(13.5))
                .line_height(px(21.0))
                .child(div().text_color(ink(theme, T2)).child(turn.summary))
                .child(caption("Sub-agents", theme))
                .children(turn.agents.iter().map(|agent| {
                    div()
                        .flex()
                        .items_center()
                        .gap(px(8.0))
                        .text_size(px(13.0))
                        .child(
                            div()
                                .size(px(10.0))
                                .rounded(px(2.0))
                                .border_1()
                                .border_color(theme.color(ColorToken::Trace)),
                        )
                        .child(agent.name)
                        .child(div().flex_1().text_color(ink(theme, T3)).child(agent.meta))
                        .child(
                            div()
                                .font_family(mono(theme))
                                .text_size(px(11.5))
                                .text_color(ink(theme, T3))
                                .child(agent.trace),
                        )
                }))
                .child(caption("Files", theme))
                .child(
                    div()
                        .flex()
                        .flex_wrap()
                        .gap(px(6.0))
                        .children(turn.files.iter().map(|file| {
                            div()
                                .h(px(22.0))
                                .px(px(8.0))
                                .flex()
                                .items_center()
                                .rounded(px(7.0))
                                .bg(ink(theme, 0.06))
                                .text_size(px(12.5))
                                .child(*file)
                        })),
                )
                .child(div().flex_1())
                .child(
                    div()
                        .flex()
                        .flex_wrap()
                        .gap(px(6.0))
                        .child(self.tell("work", "Open in work", OPEN_WORK, theme, cx))
                        .child(self.tell("mention", "Mention in chat", MENTION, theme, cx))
                        .child(self.tell("undo", "Undo this turn", UNDO, theme, cx))
                        .child(self.tell("fork", "Fork from here", FORK_HERE, theme, cx)),
                ),
        );
        div()
            .gap(px(10.0))
            .pt(px(6.0))
            .px(px(6.0))
            .pb(px(8.0))
            .child(header)
            .child(stats)
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .gap(px(10.0))
                    .child(turns)
                    .child(detail),
            )
    }

    fn ribbons(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let height = |context: u16| 4.0 + f32::from(context) / 250.0 * 46.0;
        let mut area = div()
            .h(px(300.0))
            .flex_none()
            .relative()
            .border_b_1()
            .border_color(Rgba::new(0.0, 0.0, 0.0, RULE))
            .child(dots(theme))
            .child(
                div()
                    .absolute()
                    .left(px(24.0))
                    .top(px(14.0))
                    .text_size(px(12.0))
                    .text_color(ink(theme, T3))
                    .child(RIBBON_NOTE),
            );
        for (index, segment) in SEGMENTS.iter().enumerate() {
            let on = index == self.segment;
            let lane = LANES.get(segment.lane).copied().unwrap_or(LANES[0]);
            let x = |at: usize| segment.x0 + at as f32 * STEP;
            for (at, pair) in segment.context.windows(2).enumerate() {
                let tall = (height(pair[0]) + height(pair[1])) / 2.0;
                area = area.child(
                    div()
                        .absolute()
                        .left(px(x(at)))
                        .top(px(lane - tall / 2.0))
                        .w(px(STEP))
                        .h(px(tall))
                        .bg(ink(theme, if on { RIBBON_ON } else { RIBBON_OFF })),
                );
            }
            for at in 0..segment.context.len() {
                let picked = on && at == self.step;
                let size = if picked { 9.0 } else { 5.0 };
                area = area.child(
                    div()
                        .id(("dot", index * 100 + at))
                        .absolute()
                        .left(px(x(at) - 8.0))
                        .top(px(lane - 8.0))
                        .size(px(16.0))
                        .flex()
                        .items_center()
                        .justify_center()
                        .cursor_pointer()
                        .on_click(cx.listener(move |session, _: &ClickEvent, _, cx| {
                            session.segment = index;
                            session.step = at;
                            cx.notify();
                        }))
                        .child(div().size(px(size)).rounded_full().bg(if picked {
                            theme.color(ColorToken::TextStrong)
                        } else {
                            ink(theme, if on { DOT_ON } else { DOT_OFF })
                        })),
                );
            }
            let middle = (x(0) + x(segment.context.len().saturating_sub(1))) / 2.0;
            let top = if segment.lane == 0 {
                lane - 62.0
            } else {
                lane + 22.0
            };
            area =
                area.child(
                    div()
                        .absolute()
                        .left(px(middle - 90.0))
                        .top(px(top))
                        .w(px(180.0))
                        .flex()
                        .flex_col()
                        .items_center()
                        .line_height(px(16.0))
                        .text_size(px(12.5))
                        .text_color(if on {
                            theme.color(ColorToken::TextStrong)
                        } else {
                            ink(theme, LABEL_OFF)
                        })
                        .child(segment.name)
                        .child(div().text_size(px(11.5)).text_color(ink(theme, T3)).child(
                            format!("{} · {} turns", segment.kind, segment.context.len()),
                        )),
                );
        }
        area.child(mark(300.0, 128.0, "auto fork", "238k → 31k", theme))
            .child(mark(460.0, 128.0, "/compact", "181k → 12k", theme))
            .child(
                div()
                    .absolute()
                    .left(px(234.0))
                    .top(px(166.0))
                    .text_size(px(11.5))
                    .line_height(px(15.0))
                    .text_color(ink(theme, T3))
                    .child("fork at turn 6"),
            )
            .child(
                div()
                    .absolute()
                    .left(px(1010.0))
                    .top(px(96.0))
                    .text_size(px(11.5))
                    .text_color(theme.color(ColorToken::StatusLive))
                    .child("running"),
            )
    }

    fn forks(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let segment: &Segment = SEGMENTS.get(self.segment).unwrap_or(&SEGMENTS[0]);
        let step = self.step.min(segment.turns.len().saturating_sub(1));
        let context = segment.context.get(step).copied().unwrap_or(0);
        let column = || {
            div()
                .flex_1()
                .min_w_0()
                .flex()
                .flex_col()
                .gap(px(10.0))
                .px(px(22.0))
                .py(px(18.0))
                .text_size(px(13.5))
                .line_height(px(21.0))
                .border_r_1()
                .border_color(Rgba::new(0.0, 0.0, 0.0, RULE))
        };
        let about = column()
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(10.0))
                    .child(
                        div()
                            .text_size(px(15.0))
                            .font_weight(FontWeight::SEMIBOLD)
                            .child(segment.name),
                    )
                    .child(
                        div()
                            .text_size(px(12.5))
                            .text_color(ink(theme, T3))
                            .child(segment.kind),
                    ),
            )
            .child(div().text_color(ink(theme, T2)).child(segment.what))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .gap(px(6.0))
                    .pt(px(4.0))
                    .text_size(px(13.0))
                    .child(fact_row("context carried", segment.carried, theme))
                    .child(fact_row("ended", segment.ended, theme)),
            )
            .child(div().flex_1())
            .child(
                div()
                    .flex()
                    .gap(px(6.0))
                    .child(
                        button("open", "Open session", None, ButtonKind::Plain, theme).on_click(
                            cx.listener(|session, _: &ClickEvent, _, cx| {
                                session.forks = false;
                                cx.notify();
                            }),
                        ),
                    )
                    .child(self.tell("act", segment.act, FORK_HERE, theme, cx)),
            );
        let turn = column()
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(10.0))
                    .child(
                        caption(
                            format!("turn {} of {}", step + 1, segment.turns.len()),
                            theme,
                        )
                        .flex_1(),
                    )
                    .child(
                        div()
                            .font_family(mono(theme))
                            .text_size(px(12.0))
                            .text_color(ink(theme, T3))
                            .child(format!("{context}k in context")),
                    ),
            )
            .child(
                div()
                    .text_size(px(15.0))
                    .line_height(px(22.0))
                    .child(segment.turns.get(step).copied().unwrap_or("")),
            )
            .child(
                note(PICK_NOTE, theme)
                    .text_size(px(12.5))
                    .line_height(px(19.0)),
            )
            .child(div().flex_1())
            .child(
                div()
                    .flex()
                    .gap(px(6.0))
                    .child(
                        button(
                            "fork-turn",
                            "Fork from this turn",
                            None,
                            ButtonKind::Primary,
                            theme,
                        )
                        .on_click(cx.listener(
                            |session, _: &ClickEvent, _, cx| {
                                session.told = Some(FORK_HERE);
                                cx.notify();
                            },
                        )),
                    )
                    .child(self.tell("mention", "Mention in chat", MENTION, theme, cx)),
            );
        let branch = div()
            .w(px(360.0))
            .flex_none()
            .flex()
            .flex_col()
            .gap(px(10.0))
            .px(px(18.0))
            .py(px(16.0))
            .text_size(px(13.5))
            .child(caption("Branch", theme))
            .child(
                div()
                    .id("branch")
                    .flex()
                    .items_center()
                    .gap(px(8.0))
                    .px(px(10.0))
                    .py(px(8.0))
                    .rounded(px(9.0))
                    .bg(Rgba::new(0.0, 0.0, 0.0, 0.25))
                    .cursor_pointer()
                    .on_click(cx.listener(|session, _: &ClickEvent, _, cx| {
                        session.branch = !session.branch;
                        cx.notify();
                    }))
                    .child(div().flex_1().font_family(mono(theme)).font_weight(FontWeight::SEMIBOLD).child("main"))
                    .child(div().text_size(px(12.5)).text_color(ink(theme, T3)).child("↑2 · 3 changed")),
            )
            .when(self.branch, |branch| {
                branch.child(
                    div()
                        .px(px(10.0))
                        .py(px(8.0))
                        .rounded(px(9.0))
                        .bg(Rgba { alpha: WARN_FILL, ..theme.color(ColorToken::StatusWarn) })
                        .text_size(px(12.5))
                        .line_height(px(18.0))
                        .text_color(theme.color(ColorToken::StatusWarn))
                        .child("A turn is running and ts-dev is writing web/. A switch waits until the turn ends, or you stop it first."),
                )
            })
            .child(note(BRANCH_NOTE, theme).text_size(px(12.5)).line_height(px(19.0)));
        let shell = panel(
            "Forks",
            Some(
                note(
                    "how clear-sable-eagle grew, and where you can pick it up",
                    theme,
                )
                .into_any_element(),
            ),
            theme,
        )
        .flex_1()
        .child(
            inner_card(theme).child(self.ribbons(theme, cx)).child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .child(about)
                    .child(turn)
                    .child(branch),
            ),
        );
        div().pb(px(8.0)).child(shell)
    }
}

fn fact(title: &'static str, parts: &[(&'static str, bool)], theme: &Theme) -> Div {
    div()
        .flex_1()
        .min_w_0()
        .rounded(px(10.0))
        .bg(Rgba::new(0.0, 0.0, 0.0, CARD_SHADE))
        .px(px(12.0))
        .py(px(10.0))
        .text_size(px(12.5))
        .line_height(px(18.0))
        .child(caption(title, theme).mb(px(4.0)))
        .child(frame::rich(
            &parts
                .iter()
                .map(|(text, code)| {
                    (
                        *text,
                        if *code {
                            frame::Mark::Mono
                        } else {
                            frame::Mark::Plain
                        },
                    )
                })
                .collect::<Vec<_>>(),
            theme,
        ))
}

fn fact_row(label: &'static str, value: &'static str, theme: &Theme) -> Div {
    div()
        .flex()
        .gap(px(14.0))
        .child(
            div()
                .w(px(120.0))
                .flex_none()
                .text_color(ink(theme, T3))
                .child(label),
        )
        .child(value)
}

fn mark(left: f32, top: f32, name: &'static str, change: &'static str, theme: &Theme) -> Div {
    div()
        .absolute()
        .left(px(left))
        .top(px(top))
        .w(px(90.0))
        .flex()
        .flex_col()
        .items_center()
        .text_size(px(11.5))
        .line_height(px(15.0))
        .child(div().text_color(ink(theme, T2)).child(name))
        .child(
            div()
                .font_family(mono(theme))
                .text_color(ink(theme, T3))
                .child(change),
        )
}

impl Render for Session {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let body = if self.forks {
            self.forks(&theme, cx)
        } else {
            self.overview(&theme, cx)
        };
        let told = told(self.told, &theme, cx, |session: &mut Session| {
            session.told = None
        });
        window(&theme, body).children(told)
    }
}
