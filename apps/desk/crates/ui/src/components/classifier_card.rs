use std::rc::Rc;

use desk_core::protocol::Verdict;
use gpui::{
    App, Context, Div, FocusHandle, Focusable, Rgba, SharedString, Window, div, prelude::*, px,
};

use crate::components::chip::tabular;
use crate::components::context_card::{
    FIGURE_ROW, FOOTER_ROW, GAP, HEADER_ROW, LEGEND_ROW, OnOpen, card_frame, card_header,
    figure_row, footer_row, header_chip, note, open_link, slug_brand, swatch,
};
use crate::components::list::{bare_row, separator};
use crate::components::paint::ink;
use crate::components::size::{T2, T3};
use crate::live::ActiveTheme;
use crate::metrics::HAIRLINE;
use crate::theme::{ColorToken, Theme};

pub const CLASSIFIER_CARD_WIDTH: f32 = 360.0;
const TITLE: &str = "Classifier";
const OPEN_CLASSIFIER: &str = "Open classifier";
const UNBOUND: &str = "no classifier bound";
const READING: &str = "reading the binding";
const NO_DECISION: &str = "No decision yet in this session";
const NO_LATENCY: &str = "Latency is not reported by tofu yet";
pub const LATEST: usize = 4;
const VERDICT_WIDTH: f32 = 56.0;

#[derive(Clone, PartialEq)]
pub struct Ruling {
    pub verdict: Verdict,
    pub point: SharedString,
    pub tool: SharedString,
    pub at: SharedString,
}

#[derive(Clone, PartialEq)]
pub enum Bound {
    Reading,
    Unbound,
    Model(SharedString),
}

#[derive(Clone, PartialEq)]
pub struct Rulings {
    pub bound: Bound,
    pub total: usize,
    pub capped: bool,
    pub kinds: Vec<(Verdict, usize)>,
    pub latest: Vec<Ruling>,
}

pub struct ClassifierCard {
    shown: Rulings,
    figure: SharedString,
    label: SharedString,
    counts: Vec<SharedString>,
    lines: Vec<SharedString>,
    focus: FocusHandle,
    on_open: OnOpen,
}

impl ClassifierCard {
    pub fn new(
        shown: Rulings,
        on_open: impl Fn(&mut Window, &mut App) + 'static,
        cx: &mut Context<Self>,
    ) -> Self {
        let (figure, label) = said(&shown);
        ClassifierCard {
            figure,
            label,
            counts: counts(&shown.kinds),
            lines: lines(&shown.latest),
            shown,
            focus: cx.focus_handle(),
            on_open: Rc::new(on_open),
        }
    }

    pub fn set(&mut self, shown: Rulings, cx: &mut Context<Self>) {
        if shown == self.shown {
            return;
        }
        (self.figure, self.label) = said(&shown);
        self.counts = counts(&shown.kinds);
        self.lines = lines(&shown.latest);
        self.shown = shown;
        cx.notify();
    }

    pub fn height(&self) -> f32 {
        let (rows, sections) = match self.lines.len() {
            0 => (1, 5.0),
            latest => (self.shown.kinds.len() + latest, 6.0),
        };
        HEADER_ROW
            + FIGURE_ROW
            + rows as f32 * LEGEND_ROW
            + HAIRLINE
            + FOOTER_ROW
            + (sections - 1.0) * GAP
    }

    fn figure(&self, theme: &Theme) -> Div {
        figure_row(
            self.figure.clone(),
            div().text_color(ink(theme, T2)).child(self.label.clone()),
            theme,
        )
    }

    fn kinds(&self, theme: &Theme) -> Div {
        div()
            .flex()
            .flex_col()
            .children(self.shown.kinds.iter().zip(&self.counts).enumerate().map(
                |(at, ((verdict, _), count))| {
                    bare_row(("classifier-card-kind", at), false, false, theme)
                        .h(px(LEGEND_ROW))
                        .py_0()
                        .cursor_default()
                        .child(swatch(verdict_color(verdict, theme)))
                        .child(
                            div()
                                .flex_1()
                                .min_w_0()
                                .truncate()
                                .child(verdict_word(verdict)),
                        )
                        .child(
                            div()
                                .flex_none()
                                .font_features(tabular())
                                .child(count.clone()),
                        )
                },
            ))
    }

    fn latest(&self, theme: &Theme) -> Div {
        div()
            .flex()
            .flex_col()
            .children(self.shown.latest.iter().zip(&self.lines).enumerate().map(
                |(at, (ruling, line))| {
                    bare_row(("classifier-card-latest", at), false, false, theme)
                        .h(px(LEGEND_ROW))
                        .py_0()
                        .cursor_default()
                        .child(
                            div()
                                .flex_none()
                                .w(px(VERDICT_WIDTH))
                                .text_color(verdict_color(&ruling.verdict, theme))
                                .child(verdict_word(&ruling.verdict)),
                        )
                        .child(
                            div()
                                .flex_1()
                                .min_w_0()
                                .truncate()
                                .text_color(ink(theme, T2))
                                .child(line.clone()),
                        )
                        .child(
                            div()
                                .flex_none()
                                .font_features(tabular())
                                .text_color(ink(theme, T3))
                                .child(ruling.at.clone()),
                        )
                },
            ))
    }
}

impl Focusable for ClassifierCard {
    fn focus_handle(&self, _: &App) -> FocusHandle {
        self.focus.clone()
    }
}

impl Render for ClassifierCard {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let chip = match &self.shown.bound {
            Bound::Model(slug) => header_chip(slug.clone(), slug_brand(slug), &theme),
            Bound::Unbound => header_chip(UNBOUND.into(), None, &theme),
            Bound::Reading => header_chip(READING.into(), None, &theme),
        };
        let footer =
            footer_row(div().text_color(ink(&theme, T3)).child(NO_LATENCY)).child(open_link(
                "classifier-card-open",
                OPEN_CLASSIFIER,
                self.on_open.clone(),
                &theme,
            ));
        let frame = card_frame(&self.focus)
            .child(card_header(TITLE, Some(chip), &theme))
            .child(self.figure(&theme));
        let frame = if self.lines.is_empty() {
            frame.child(note(NO_DECISION, ink(&theme, T2)))
        } else {
            frame.child(self.kinds(&theme)).child(self.latest(&theme))
        };
        frame.child(separator(&theme).mx_0()).child(footer)
    }
}

pub fn count_kinds<'a>(verdicts: impl Iterator<Item = &'a Verdict>) -> Vec<(Verdict, usize)> {
    let mut kinds: Vec<(Verdict, usize)> =
        vec![(Verdict::Allow, 0), (Verdict::Ask, 0), (Verdict::Deny, 0)];
    for verdict in verdicts {
        match kinds.iter_mut().find(|(kind, _)| kind == verdict) {
            Some((_, count)) => *count = count.saturating_add(1),
            None => kinds.push((verdict.clone(), 1)),
        }
    }
    kinds
}

fn said(shown: &Rulings) -> (SharedString, SharedString) {
    let more = if shown.capped { "+" } else { "" };
    let counted = shown
        .kinds
        .iter()
        .fold(0, |sum: usize, (_, count)| sum.saturating_add(*count));
    let label = if shown.capped {
        format!("decisions this session, kinds of the last {counted}")
    } else {
        "decisions this session".to_owned()
    };
    (format!("{}{more}", shown.total).into(), label.into())
}

fn counts(kinds: &[(Verdict, usize)]) -> Vec<SharedString> {
    kinds
        .iter()
        .map(|(_, count)| count.to_string().into())
        .collect()
}

fn lines(latest: &[Ruling]) -> Vec<SharedString> {
    latest
        .iter()
        .take(LATEST)
        .map(|ruling| format!("{} · {}", ruling.point, ruling.tool).into())
        .collect()
}

fn verdict_word(verdict: &Verdict) -> SharedString {
    match verdict {
        Verdict::Allow => "allowed".into(),
        Verdict::Ask => "asked".into(),
        Verdict::Deny => "denied".into(),
        Verdict::Unknown(raw) => raw.clone().into(),
    }
}

fn verdict_color(verdict: &Verdict, theme: &Theme) -> Rgba {
    match verdict {
        Verdict::Allow => theme.color(ColorToken::StatusLive),
        Verdict::Ask => theme.color(ColorToken::StatusWarn),
        Verdict::Deny => theme.color(ColorToken::StatusDanger),
        Verdict::Unknown(_) => ink(theme, T3),
    }
}
