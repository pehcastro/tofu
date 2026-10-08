mod fixture;
pub mod frame;

use desk_ui::components::card::{dots, inner_card};
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
    BOARD, Measure, Outcome, PROJECT, Range, SESSIONS, SIFTED, SIFTED_NOTE, SOURCES, SOURCES_NOTE,
    SPENDERS, Scope, Source, Swatch,
};
use frame::{
    BODY_TEXT, HEADER_PILLS, LINE, SHELL_PILLS, ellipsis, figure, fraction, load_fonts, note,
    panel, panes, pills, title, told, window,
};

const ACTIVITY_LEAST: f32 = 340.0;
const SOURCES_LEAST: f32 = 320.0;
const SIDE_LEAST: f32 = 260.0;

const CELL: f32 = 9.0;
const CELL_RADIUS: f32 = 2.5;
const CELLS: usize = 10;
const CELL_EMPTY: f32 = 0.05;
const CELL_FULL: f32 = 0.55;
const CELL_TOP: f32 = 0.9;
const BAR: f32 = 6.0;
const BAR_TRACK: f32 = 0.08;
const LEAD_BAR: f32 = 0.85;
const RULE: f32 = 0.06;
const FAINT_SWATCH: f32 = 0.45;

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    match board {
        None | Some(BOARD) => {}
        Some(other) => return Err(format!("the usage screen draws {BOARD}, not {other}")),
    }
    load_fonts(cx)?;
    Ok(cx
        .new(|_| Usage {
            measure: Measure::Tokens,
            range: Range::Week,
            scope: Scope::ThisProject,
            filter: None,
            told: None,
        })
        .into())
}

struct Usage {
    measure: Measure,
    range: Range,
    scope: Scope,
    filter: Option<Source>,
    told: Option<&'static str>,
}

impl Usage {
    fn header(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        div()
            .flex()
            .flex_none()
            .flex_wrap()
            .items_center()
            .gap(px(10.0))
            .px(px(4.0))
            .child(title("Usage"))
            .child(
                div()
                    .text_size(px(13.0))
                    .text_color(ink(theme, T3))
                    .child(format!("{PROJECT} · {}", self.range.text())),
            )
            .child(div().flex_1())
            .child(pills(
                "range",
                &Range::ALL,
                self.range,
                &HEADER_PILLS,
                theme,
                cx,
                |usage: &mut Usage, range| usage.range = range,
            ))
            .child(pills(
                "scope",
                &Scope::ALL,
                self.scope,
                &HEADER_PILLS,
                theme,
                cx,
                |usage: &mut Usage, scope| usage.scope = scope,
            ))
    }

    fn activity(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let [headline, unit, delta] = self.measure.headline();
        let seed = self.measure.seed();
        let count = self.range.columns();
        let share = Source::share(self.filter) * self.scope.share();
        let columns = (0..count).map(|at| {
            let base = f32::from(seed.get(at % seed.len()).copied().unwrap_or(1));
            let height = ((base * share).round() as usize).max(1);
            div()
                .flex_1()
                .flex()
                .flex_col_reverse()
                .gap(px(3.0))
                .children((0..CELLS).map(|cell| {
                    let fill = if cell >= height {
                        ink(theme, CELL_EMPTY)
                    } else if at + 1 == count {
                        theme.color(ColorToken::StatusLive)
                    } else if cell + 1 == height {
                        ink(theme, CELL_TOP)
                    } else {
                        ink(theme, CELL_FULL)
                    };
                    div().h(px(CELL)).rounded(px(CELL_RADIUS)).bg(fill)
                }))
        });
        panel(
            "Activity",
            Some(
                pills(
                    "measure",
                    &Measure::ALL,
                    self.measure,
                    &SHELL_PILLS,
                    theme,
                    cx,
                    |usage: &mut Usage, measure| usage.measure = measure,
                )
                .into_any_element(),
            ),
            theme,
        )
        .map(|panel| fraction(panel, 1.6, ACTIVITY_LEAST))
        .child(
            inner_card(theme)
                .px(px(18.0))
                .py(px(16.0))
                .gap(px(12.0))
                .child(dots(theme))
                .child(
                    div()
                        .flex()
                        .items_baseline()
                        .gap(px(10.0))
                        .child(
                            div()
                                .text_size(px(38.0))
                                .line_height(relative(1.0))
                                .font_weight(FontWeight::SEMIBOLD)
                                .letter_spacing(px(-0.38))
                                .child(headline),
                        )
                        .child(
                            div()
                                .text_size(px(13.5))
                                .text_color(ink(theme, T2))
                                .child(unit),
                        )
                        .child(div().flex_1())
                        .child(
                            div()
                                .text_size(px(12.5))
                                .text_color(Tone::Added.color(theme))
                                .child(delta),
                        ),
                )
                .child(
                    div()
                        .flex_1()
                        .flex()
                        .items_end()
                        .gap(px(5.0))
                        .children(columns),
                )
                .child(
                    div()
                        .flex()
                        .justify_between()
                        .font_family(mono(theme))
                        .text_size(px(11.5))
                        .text_color(ink(theme, T3))
                        .child(self.range.start())
                        .child("now"),
                ),
        )
    }

    fn sources(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        panel(
            "By source",
            Some(note("click to filter", theme).into_any_element()),
            theme,
        )
        .map(|panel| fraction(panel, 1.6, SOURCES_LEAST))
        .child(
            inner_card(theme)
                .p(px(8.0))
                .gap(px(2.0))
                .text_size(px(13.5))
                .children(SOURCES.iter().enumerate().map(|(at, source)| {
                    let picked = source.source;
                    row(("source", at), self.filter == Some(picked), false, theme)
                        .text_size(px(13.5))
                        .line_height(px(LINE))
                        .text_color(ink(theme, BODY_TEXT))
                        .on_click(cx.listener(move |usage, _: &ClickEvent, _, cx| {
                            usage.filter = (usage.filter != Some(picked)).then_some(picked);
                            cx.notify();
                        }))
                        .child(
                            div()
                                .size(px(8.0))
                                .rounded(px(2.0))
                                .bg(swatch(source.swatch, theme)),
                        )
                        .child(ellipsis(div().flex_1()).child(source.name))
                        .child(note(source.paid, theme))
                        .child(figure(source.tokens, 56.0, theme))
                }))
                .child(
                    div()
                        .mt_auto()
                        .px(px(10.0))
                        .py(px(8.0))
                        .text_size(px(12.0))
                        .line_height(px(17.0))
                        .text_color(ink(theme, T3))
                        .child(SOURCES_NOTE),
                ),
        )
    }

    fn sessions(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        panel(
            "Sessions",
            Some(note("all sessions", theme).into_any_element()),
            theme,
        )
        .map(|panel| fraction(panel, 1.0, SIDE_LEAST))
        .child(
            inner_card(theme)
                .px(px(8.0))
                .py(px(6.0))
                .children(SESSIONS.iter().enumerate().map(|(at, session)| {
                    let (dot, name, turns) = match session.outcome {
                        Outcome::Live => (
                            theme.color(ColorToken::StatusLive),
                            ink(theme, BODY_TEXT),
                            ink(theme, T3),
                        ),
                        Outcome::Idle => (ink(theme, T3), ink(theme, T2), ink(theme, T3)),
                        Outcome::LoopGuard => (
                            theme.color(ColorToken::StatusWarn),
                            ink(theme, T2),
                            theme.color(ColorToken::StatusWarn),
                        ),
                    };
                    let tell = session.tell;
                    row(("session", at), false, false, theme)
                        .line_height(px(LINE))
                        .on_click(cx.listener(move |usage, _: &ClickEvent, _, cx| {
                            usage.told = Some(tell);
                            cx.notify();
                        }))
                        .child(div().text_size(px(8.0)).text_color(dot).child("●"))
                        .child(
                            ellipsis(div().flex_1())
                                .text_color(name)
                                .child(session.name),
                        )
                        .child(
                            div()
                                .when(session.outcome == Outcome::LoopGuard, |turns| {
                                    turns.text_size(px(12.0))
                                })
                                .text_color(turns)
                                .child(session.turns),
                        )
                        .child(figure(session.tokens, 50.0, theme))
                })),
        )
    }
}

fn where_it_went(theme: &Theme) -> Div {
    panel(
        "Where it went",
        Some(note("by who spent it", theme).into_any_element()),
        theme,
    )
    .map(|panel| fraction(panel, 1.0, SIDE_LEAST))
    .child(
        inner_card(theme)
            .px(px(16.0))
            .py(px(14.0))
            .gap(px(12.0))
            .text_size(px(13.0))
            .children(SPENDERS.iter().map(|spender| {
                div()
                    .flex()
                    .flex_col()
                    .child(
                        div()
                            .flex()
                            .child(div().flex_1().child(spender.who))
                            .child(div().font_family(mono(theme)).child(spender.tokens)),
                    )
                    .child(
                        div()
                            .mt(px(6.0))
                            .h(px(BAR))
                            .rounded(px(3.0))
                            .overflow_hidden()
                            .bg(ink(theme, BAR_TRACK))
                            .child(
                                div()
                                    .h(px(BAR))
                                    .rounded(px(3.0))
                                    .w(relative(spender.share))
                                    .bg(swatch(spender.swatch, theme)),
                            ),
                    )
                    .children(spender.note.map(|text| {
                        div()
                            .mt(px(4.0))
                            .text_size(px(11.5))
                            .text_color(ink(theme, T3))
                            .child(text)
                    }))
            }))
            .child(
                div()
                    .mt_auto()
                    .pt(px(10.0))
                    .border_t_1()
                    .border_color(ink(theme, RULE))
                    .flex()
                    .items_baseline()
                    .gap(px(8.0))
                    .child(
                        div()
                            .text_size(px(20.0))
                            .line_height(relative(1.0))
                            .font_weight(FontWeight::SEMIBOLD)
                            .text_color(theme.color(ColorToken::StatusLive))
                            .child(SIFTED),
                    )
                    .child(
                        div()
                            .text_size(px(12.5))
                            .text_color(ink(theme, T2))
                            .child(SIFTED_NOTE),
                    ),
            ),
    )
}

fn swatch(swatch: Swatch, theme: &Theme) -> Rgba {
    match swatch {
        Swatch::White => theme.color(ColorToken::TextStrong),
        Swatch::Lead => ink(theme, LEAD_BAR),
        Swatch::Faint => ink(theme, FAINT_SWATCH),
        Swatch::Accent => theme.color(ColorToken::StatusAccent),
        Swatch::Trace => theme.color(ColorToken::Trace),
        Swatch::Live => theme.color(ColorToken::StatusLive),
    }
}

impl Render for Usage {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let body = div().gap(px(10.0)).child(self.header(&theme, cx)).child(
            div()
                .flex_1()
                .min_h_0()
                .flex()
                .flex_col()
                .gap(px(10.0))
                .child(
                    panes()
                        .flex_grow(1.25)
                        .child(self.activity(&theme, cx))
                        .child(where_it_went(&theme)),
                )
                .child(
                    panes()
                        .flex_grow(1.0)
                        .child(self.sources(&theme, cx))
                        .child(self.sessions(&theme, cx)),
                ),
        );
        let told = told(self.told, &theme, cx, |usage: &mut Usage| usage.told = None);
        window(&theme, body).children(told)
    }
}
