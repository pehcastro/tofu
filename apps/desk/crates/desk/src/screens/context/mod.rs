mod fixture;

#[cfg(not(any(feature = "screen-usage", feature = "screen-limits")))]
#[path = "../usage/frame.rs"]
pub mod frame;
#[cfg(all(feature = "screen-limits", not(feature = "screen-usage")))]
use crate::screens::limits::frame;
#[cfg(feature = "screen-usage")]
use crate::screens::usage::frame;

use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{caption, inner_card};
use desk_ui::components::chip::mono;
use desk_ui::components::list::row;
use desk_ui::components::paint::{ink, tint};
use desk_ui::components::size::{T2, T3};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Div, FontWeight, Rgba, Window, div, prelude::*,
    px, relative,
};

use fixture::{BOARD, CAPACITY, CATEGORIES, FORK_AT, FORK_NOW, LEGEND, Part, Swatch, items, size};
use frame::{Ink, Shape, ellipsis, fraction, note, panel, panes, shapes, title, told, window};

const STRONG_SWATCH: f32 = 0.55;
const FAINT_SWATCH: f32 = 0.28;
const DIMMED: f32 = 0.35;
const EMPTY_CELL: f32 = 0.05;
const FORK_CELL: f32 = 0.5;
const RULE: f32 = 0.07;
const COLUMNS: usize = 25;
const AREA: f32 = 0.06;
const LINE_INK: f32 = 0.85;
const CHART: (f32, f32) = (700.0, 150.0);
const CHART_LEAST: f32 = 120.0;
const WINDOW_LEAST: f32 = 340.0;
const PARTS_LEAST: f32 = 300.0;
const GROWTH: [(f32, f32); 15] = [
    (0.0, 140.0),
    (60.0, 130.0),
    (120.0, 118.0),
    (180.0, 100.0),
    (240.0, 78.0),
    (280.0, 58.0),
    (290.0, 58.0),
    (290.0, 124.0),
    (360.0, 118.0),
    (420.0, 104.0),
    (470.0, 92.0),
    (520.0, 86.0),
    (580.0, 80.0),
    (640.0, 72.0),
    (700.0, 66.0),
];

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    match board {
        None | Some(BOARD) => {}
        Some(other) => return Err(format!("the context screen draws {BOARD}, not {other}")),
    }
    frame::load_fonts(cx)?;
    Ok(cx
        .new(|_| ContextScreen {
            part: Part::Tools,
            compacted: false,
            told: None,
        })
        .into())
}

struct ContextScreen {
    part: Part,
    compacted: bool,
    told: Option<&'static str>,
}

fn swatch(swatch: Swatch, theme: &Theme) -> Rgba {
    match swatch {
        Swatch::Strong => ink(theme, STRONG_SWATCH),
        Swatch::Faint => ink(theme, FAINT_SWATCH),
        Swatch::Accent => theme.color(ColorToken::StatusAccent),
        Swatch::Live => theme.color(ColorToken::StatusLive),
        Swatch::Trace => theme.color(ColorToken::Trace),
        Swatch::Warn => theme.color(ColorToken::StatusWarn),
    }
}

impl ContextScreen {
    fn header(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let (label, kind) = if self.compacted {
            ("Back to now", ButtonKind::Primary)
        } else {
            ("Preview compact", ButtonKind::Plain)
        };
        div()
            .flex()
            .flex_none()
            .flex_wrap()
            .items_center()
            .gap(px(10.0))
            .px(px(4.0))
            .child(title("Context"))
            .child(
                div()
                    .text_size(px(13.0))
                    .text_color(ink(theme, T3))
                    .child("clear-sable-eagle · what the lead reads at its next step"),
            )
            .child(div().flex_1())
            .child(
                button("compact", label, None, kind, theme).on_click(cx.listener(
                    |screen, _: &ClickEvent, _, cx| {
                        screen.compacted = !screen.compacted;
                        cx.notify();
                    },
                )),
            )
            .child(
                button("fork", "Fork now", None, ButtonKind::Plain, theme).on_click(cx.listener(
                    |screen, _: &ClickEvent, _, cx| {
                        screen.told = Some(FORK_NOW);
                        cx.notify();
                    },
                )),
            )
    }

    fn cells(&self, theme: &Theme) -> Vec<Rgba> {
        let mut cells = Vec::with_capacity(CAPACITY);
        for category in &CATEGORIES {
            let color = swatch(category.swatch, theme);
            let color = if category.part == self.part {
                color
            } else {
                tint(color, color.alpha * DIMMED)
            };
            cells.extend(std::iter::repeat_n(
                color,
                size(category.part, self.compacted).0,
            ));
        }
        while cells.len() < CAPACITY {
            cells.push(if cells.len() == FORK_AT {
                tint(theme.color(ColorToken::StatusDanger), FORK_CELL)
            } else {
                ink(theme, EMPTY_CELL)
            });
        }
        cells
    }

    fn window_panel(&self, theme: &Theme) -> Div {
        let used: usize = CATEGORIES
            .iter()
            .map(|category| size(category.part, self.compacted).0)
            .sum();
        let forecast = if self.compacted {
            "after compact · forks in about 40 steps"
        } else {
            "at this pace, forks in about 11 steps"
        };
        let cells = self.cells(theme);
        panel(
            "The window",
            Some(note("one dot = 1k tokens", theme).into_any_element()),
            theme,
        )
        .flex_grow(1.2)
        .flex_basis(relative(0.0))
        .min_h_0()
        .child(
            inner_card(theme)
                .px(px(16.0))
                .py(px(14.0))
                .gap(px(12.0))
                .child(
                    div()
                        .flex()
                        .flex_wrap()
                        .items_baseline()
                        .gap(px(10.0))
                        .child(
                            div()
                                .text_size(px(34.0))
                                .line_height(relative(1.0))
                                .font_weight(FontWeight::SEMIBOLD)
                                .child(format!("{used}k")),
                        )
                        .child(div().text_color(ink(theme, T2)).child("of 250k"))
                        .child(div().flex_1())
                        .child(
                            div()
                                .text_size(px(12.5))
                                .text_color(ink(theme, T3))
                                .child(forecast),
                        ),
                )
                .child(div().mt(px(2.0)).flex().flex_col().gap(px(3.0)).children(
                    cells.chunks(COLUMNS).map(|line| {
                        div().flex().gap(px(3.0)).children(
                            line.iter().map(|color| {
                                div().flex_1().h(px(12.0)).rounded(px(2.5)).bg(*color)
                            }),
                        )
                    }),
                ))
                .child(
                    div()
                        .flex()
                        .flex_wrap()
                        .gap(px(14.0))
                        .text_size(px(12.0))
                        .text_color(ink(theme, T2))
                        .children(LEGEND.iter().map(|(color, label)| {
                            div()
                                .flex()
                                .items_center()
                                .gap(px(6.0))
                                .child(
                                    div()
                                        .size(px(8.0))
                                        .rounded(px(2.0))
                                        .bg(swatch(*color, theme)),
                                )
                                .child(*label)
                        }))
                        .child(
                            div()
                                .flex()
                                .items_center()
                                .gap(px(6.0))
                                .child(
                                    div()
                                        .w(px(2.0))
                                        .h(px(10.0))
                                        .bg(theme.color(ColorToken::StatusDanger)),
                                )
                                .child("fork at 200k"),
                        ),
                ),
        )
    }

    fn parts_panel(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let picked = CATEGORIES
            .iter()
            .find(|category| category.part == self.part)
            .map_or("", |category| category.title);
        panel(
            "What is in it",
            Some(note("click a part", theme).into_any_element()),
            theme,
        )
        .map(|panel| fraction(panel, 1.0, PARTS_LEAST))
        .child(
            inner_card(theme)
                .p(px(8.0))
                .gap(px(2.0))
                .text_size(px(13.5))
                .children(CATEGORIES.iter().enumerate().map(|(at, category)| {
                    let part = category.part;
                    let (tokens, count) = size(part, self.compacted);
                    row(("part", at), part == self.part, false, theme)
                        .on_click(cx.listener(move |screen, _: &ClickEvent, _, cx| {
                            screen.part = part;
                            cx.notify();
                        }))
                        .child(
                            div()
                                .size(px(9.0))
                                .rounded(px(2.0))
                                .bg(swatch(category.swatch, theme)),
                        )
                        .child(ellipsis(div().flex_1()).child(category.name))
                        .child(note(count, theme))
                        .child(
                            div()
                                .min_w(px(44.0))
                                .flex()
                                .justify_end()
                                .font_family(mono(theme))
                                .child(format!("{tokens}k")),
                        )
                }))
                .child(
                    div()
                        .h(px(1.0))
                        .mx(px(6.0))
                        .my(px(8.0))
                        .bg(ink(theme, RULE)),
                )
                .child(
                    div()
                        .pt(px(2.0))
                        .px(px(10.0))
                        .pb(px(6.0))
                        .child(caption(picked, theme)),
                )
                .children(
                    items(self.part, self.compacted)
                        .into_iter()
                        .flatten()
                        .enumerate()
                        .map(|(at, item)| {
                            row(("item", at), false, false, theme)
                                .items_start()
                                .text_size(px(13.0))
                                .child(
                                    div()
                                        .flex_1()
                                        .min_w_0()
                                        .flex()
                                        .flex_col()
                                        .line_height(px(18.0))
                                        .child(
                                            div()
                                                .truncate()
                                                .font_family(mono(theme))
                                                .text_size(px(12.5))
                                                .child(item.name),
                                        )
                                        .child(note(item.meta, theme)),
                                )
                                .child(
                                    div()
                                        .min_w(px(40.0))
                                        .flex()
                                        .justify_end()
                                        .font_family(mono(theme))
                                        .child(item.tokens),
                                )
                                .child(
                                    div()
                                        .min_w(px(96.0))
                                        .flex()
                                        .justify_end()
                                        .text_size(px(11.5))
                                        .text_color(ink(theme, T3))
                                        .child(item.fate),
                                )
                        }),
                ),
        )
    }
}

fn chart(theme: &Theme) -> impl IntoElement {
    let line = GROWTH.to_vec();
    let mut area = line.clone();
    area.extend([(CHART.0, CHART.1), (0.0, CHART.1)]);
    let danger = theme.color(ColorToken::StatusDanger);
    let trace = theme.color(ColorToken::Trace);
    let flat = |points: Vec<(f32, f32)>, ink: Ink| Shape {
        points,
        closed: false,
        ink,
    };
    let mut drawn = vec![
        flat(
            vec![(0.0, 30.0), (CHART.0, 30.0)],
            Ink {
                fill: None,
                stroke: Some((tint(danger, 0.6), 1.0)),
                dash: Some([3.0, 4.0]),
            },
        ),
        Shape {
            points: area,
            closed: true,
            ink: Ink {
                fill: Some(ink(theme, AREA)),
                stroke: None,
                dash: None,
            },
        },
        flat(
            line,
            Ink {
                fill: None,
                stroke: Some((ink(theme, LINE_INK), 2.0)),
                dash: None,
            },
        ),
        flat(
            vec![(290.0, 8.0), (290.0, 148.0)],
            Ink {
                fill: None,
                stroke: Some((ink(theme, 0.25), 1.0)),
                dash: Some([2.0, 3.0]),
            },
        ),
    ];
    for (cx, cy) in [(420.0, 104.0), (580.0, 80.0)] {
        drawn.push(Shape {
            points: (0..16)
                .map(|step| {
                    let angle = step as f32 * std::f32::consts::TAU / 16.0;
                    (cx + 4.0 * angle.cos(), cy + 4.0 * angle.sin())
                })
                .collect(),
            closed: true,
            ink: Ink {
                fill: Some(trace),
                stroke: None,
                dash: None,
            },
        });
    }
    div().flex_1().min_h(px(CHART_LEAST)).relative().child(
        div()
            .absolute()
            .inset_0()
            .child(shapes(CHART, drawn).size_full()),
    )
}

fn growth(theme: &Theme) -> Div {
    panel(
        "How it grew",
        Some(note("per step", theme).into_any_element()),
        theme,
    )
    .flex_grow(1.0)
    .flex_basis(relative(0.0))
    .min_h_0()
    .child(
        inner_card(theme)
            .px(px(16.0))
            .py(px(12.0))
            .child(chart(theme))
            .child(
                div()
                    .flex()
                    .flex_wrap()
                    .gap(px(16.0))
                    .text_size(px(11.5))
                    .text_color(ink(theme, T3))
                    .child("step 1")
                    .child(
                        div()
                            .flex_1()
                            .flex()
                            .justify_center()
                            .child("compacted at step 22 · 182k to 31k"),
                    )
                    .child(
                        div()
                            .text_color(theme.color(ColorToken::Trace))
                            .child("● sub-agent reports"),
                    )
                    .child("step 41"),
            ),
    )
}

impl Render for ContextScreen {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let body = div()
            .gap(px(10.0))
            .pt(px(6.0))
            .px(px(6.0))
            .pb(px(8.0))
            .child(self.header(&theme, cx))
            .child(
                panes()
                    .flex_1()
                    .child(
                        fraction(div(), 1.375, WINDOW_LEAST)
                            .flex()
                            .flex_col()
                            .gap(px(10.0))
                            .child(self.window_panel(&theme))
                            .child(growth(&theme)),
                    )
                    .child(self.parts_panel(&theme, cx)),
            );
        let told = told(self.told, &theme, cx, |screen: &mut ContextScreen| {
            screen.told = None
        });
        window(&theme, body).children(told)
    }
}
