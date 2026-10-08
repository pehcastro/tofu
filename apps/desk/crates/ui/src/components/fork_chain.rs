use gpui::{
    Bounds, Div, FontWeight, PathBuilder, Pixels, Rgba, SharedString, Window, canvas, div, point,
    prelude::*, px,
};

use crate::components::card::inner_card;
use crate::components::chip::{badge, mono, tabular};
use crate::components::paint::{ink, ring};
use crate::components::size::{FONT_BODY, FONT_CHAT, FONT_SMALL, FONT_WHO, LINE_BODY, T1, T2, T3};
use crate::theme::{ColorToken, Theme};

const RAIL: f32 = 22.0;
const DOT: f32 = 9.0;
const DOT_TOP: f32 = 16.0;
const LINE: f32 = 1.5;
const LINE_INK: f32 = 0.16;
const RIBBON_INK: f32 = 0.2;
const RIBBON_LEAST: f32 = 2.0;
const RIBBON_MOST: f32 = 18.0;
const FORK_HEIGHT: f32 = 46.0;
const FACT_LEAST: f32 = 104.0;
const DOT_RING: f32 = 0.45;

#[derive(Clone)]
pub struct Fork {
    pub kind: SharedString,
    pub tokens: Option<SharedString>,
    pub fill: Option<(f32, f32)>,
}

#[derive(Clone)]
pub struct Generation {
    pub name: SharedString,
    pub handle: Option<SharedString>,
    pub place: SharedString,
    pub current: bool,
    pub head: bool,
    pub facts: Vec<(SharedString, SharedString)>,
    pub into: Option<Fork>,
}

#[derive(IntoElement)]
pub struct ForkChain {
    theme: Theme,
    generations: Vec<Generation>,
}

impl ForkChain {
    pub fn new(theme: &Theme, generations: Vec<Generation>) -> Self {
        ForkChain {
            theme: theme.clone(),
            generations,
        }
    }
}

fn rail_line(theme: &Theme, shown: bool) -> Div {
    div()
        .w(px(LINE))
        .flex_1()
        .when(shown, |line| line.bg(ink(theme, LINE_INK)))
}

fn ribbon(fill: Option<(f32, f32)>, color: Rgba) -> impl IntoElement {
    let width = |share: f32| RIBBON_LEAST + (RIBBON_MOST - RIBBON_LEAST) * share.clamp(0.0, 1.0);
    let (top, bottom) = fill.map_or((LINE, LINE), |(before, after)| {
        (width(before), width(after))
    });
    canvas(
        |_, _, _| (),
        move |bounds: Bounds<Pixels>, (), window: &mut Window, _| {
            let middle = f32::from(bounds.size.width) / 2.0;
            let height = f32::from(bounds.size.height);
            let at = |x: f32, y: f32| bounds.origin + point(px(x), px(y));
            let mut path = PathBuilder::fill();
            path.move_to(at(middle - top / 2.0, 0.0));
            path.line_to(at(middle + top / 2.0, 0.0));
            path.line_to(at(middle + bottom / 2.0, height));
            path.line_to(at(middle - bottom / 2.0, height));
            path.close();
            if let Ok(path) = path.build() {
                window.paint_path(path, color);
            }
        },
    )
    .size_full()
}

fn fork_row(fork: &Fork, theme: &Theme) -> Div {
    div()
        .flex()
        .h(px(FORK_HEIGHT))
        .gap(px(10.0))
        .child(
            div()
                .w(px(RAIL))
                .flex_none()
                .child(ribbon(fork.fill, ink(theme, RIBBON_INK))),
        )
        .child(
            div()
                .flex()
                .flex_wrap()
                .items_center()
                .gap_x(px(8.0))
                .min_w_0()
                .text_size(px(FONT_SMALL))
                .text_color(ink(theme, T2))
                .child(badge(fork.kind.clone(), theme))
                .children(
                    fork.tokens
                        .clone()
                        .map(|tokens| div().font_features(tabular()).child(tokens)),
                ),
        )
}

fn dot(theme: &Theme, current: bool) -> Div {
    let dot = div().flex_none().size(px(DOT)).rounded_full();
    if current {
        dot.bg(theme.color(ColorToken::StatusLive))
    } else {
        dot.shadow(vec![ring(ink(theme, DOT_RING))])
    }
}

fn generation_row(generation: &Generation, last: bool, theme: &Theme) -> Div {
    let marks = [
        generation
            .current
            .then_some(("current", ColorToken::StatusLive)),
        generation.head.then_some(("head", ColorToken::TextStrong)),
    ];
    let head = div()
        .flex()
        .flex_wrap()
        .items_center()
        .gap_x(px(8.0))
        .gap_y(px(4.0))
        .min_w_0()
        .child(
            div()
                .text_size(px(FONT_CHAT))
                .font_weight(FontWeight::SEMIBOLD)
                .text_color(ink(theme, T1))
                .child(generation.name.clone()),
        )
        .children(generation.handle.clone().map(|handle| {
            div()
                .font_family(mono(theme))
                .text_size(px(FONT_WHO))
                .text_color(ink(theme, T3))
                .child(handle)
        }))
        .children(
            marks
                .into_iter()
                .flatten()
                .map(|(mark, color)| badge(mark, theme).text_color(theme.color(color))),
        )
        .child(div().flex_1())
        .child(
            div()
                .text_size(px(FONT_SMALL))
                .text_color(ink(theme, T3))
                .child(generation.place.clone()),
        );
    let facts = div()
        .flex()
        .flex_wrap()
        .gap_x(px(20.0))
        .gap_y(px(8.0))
        .children(generation.facts.iter().map(|(label, value)| {
            div()
                .flex()
                .flex_col()
                .min_w(px(FACT_LEAST))
                .child(
                    div()
                        .text_size(px(FONT_WHO))
                        .text_color(ink(theme, T3))
                        .child(label.clone()),
                )
                .child(
                    div()
                        .text_size(px(FONT_BODY))
                        .line_height(px(LINE_BODY))
                        .font_features(tabular())
                        .whitespace_nowrap()
                        .text_color(ink(theme, T1))
                        .child(value.clone()),
                )
        }));
    div()
        .flex()
        .gap(px(10.0))
        .child(
            div()
                .w(px(RAIL))
                .flex_none()
                .flex()
                .flex_col()
                .items_center()
                .child(
                    rail_line(theme, generation.into.is_some())
                        .flex_none()
                        .h(px(DOT_TOP)),
                )
                .child(dot(theme, generation.current))
                .child(rail_line(theme, !last)),
        )
        .child(
            inner_card(theme)
                .px(px(14.0))
                .py(px(10.0))
                .gap(px(8.0))
                .child(head)
                .when(!generation.facts.is_empty(), |card| card.child(facts)),
        )
}

impl RenderOnce for ForkChain {
    fn render(self, _: &mut Window, _: &mut gpui::App) -> impl IntoElement {
        let count = self.generations.len();
        div().flex().flex_col().min_w_0().children(
            self.generations
                .iter()
                .enumerate()
                .flat_map(|(at, generation)| {
                    let fork = generation
                        .into
                        .as_ref()
                        .map(|fork| fork_row(fork, &self.theme));
                    let row = generation_row(generation, at + 1 == count, &self.theme);
                    fork.into_iter().chain([row])
                })
                .collect::<Vec<_>>(),
        )
    }
}
