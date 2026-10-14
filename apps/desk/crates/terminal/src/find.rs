use crate::Events;
use crate::paint::FAMILY;
use crate::palette::Palette;
use alacritty_terminal::grid::Dimensions;
use alacritty_terminal::index::{Column, Direction, Line, Point};
use alacritty_terminal::term::Term;
use alacritty_terminal::term::search::{Match, RegexIter, RegexSearch};
use gpui::{
    App, ClickEvent, ColorExt, Hsla, IntoElement, Pixels, Rgba, Window, div, hsla_to_rgba,
    prelude::*, px, rgb, rgb_to_hsla,
};

const META: &[char] = &[
    '\\', '.', '+', '*', '?', '(', ')', '|', '[', ']', '{', '}', '^', '$',
];
const INSET: f32 = 8.;
const RIGHT: f32 = 16.;
const PAD: f32 = 8.;
const GAP: f32 = 12.;
const RADIUS: f32 = 6.;
const FIELD: f32 = 160.;
const QUIET: f32 = 0.6;
const EDGE: f32 = 0.25;

const COUNT: f32 = 72.;
const TOGGLE: f32 = 24.;
const LIFT: f32 = 0.08;
const DARK_GROUND: u32 = 0x161719;
const LIGHT_GROUND: u32 = 0xffffff;

#[derive(Default)]
pub(crate) struct Find {
    pub query: String,
    matches: Option<Vec<Match>>,
    at: Option<usize>,
    case: bool,
}

fn mix(from: Rgba, to: Rgba, amount: f32) -> Rgba {
    let step = |a: f32, b: f32| a + (b - a) * amount;
    Rgba::new(
        step(from.red, to.red),
        step(from.green, to.green),
        step(from.blue, to.blue),
        1.,
    )
}

fn surface(palette: &Palette) -> Hsla {
    let ink = hsla_to_rgba(palette.foreground);
    let card = hsla_to_rgba(palette.background);
    let ground = rgb(match palette.foreground.lightness > 0.5 {
        true => DARK_GROUND,
        false => LIGHT_GROUND,
    });
    rgb_to_hsla(mix(mix(ground, card, card.alpha), ink, LIFT))
}

impl Find {
    pub(crate) fn toggle_case(&mut self) {
        self.case = !self.case;
        self.forget();
    }

    pub(crate) fn edit(&mut self, query: impl FnOnce(&mut String)) {
        query(&mut self.query);
        self.forget();
    }

    pub(crate) fn forget(&mut self) {
        self.matches = None;
        self.at = None;
    }

    pub(crate) fn run(
        &mut self,
        term: &Term<Events>,
        older: bool,
    ) -> Result<Option<Match>, String> {
        let flag = match self.case {
            true => "(?-i)",
            false => "(?i)",
        };
        let pattern: String = flag
            .chars()
            .chain(
                self.query
                    .chars()
                    .flat_map(|ch| META.contains(&ch).then_some('\\').into_iter().chain([ch])),
            )
            .collect();
        let mut regex = RegexSearch::new(&pattern).map_err(|error| error.to_string())?;
        let grid = term.grid();
        let start = Point::new(grid.topmost_line(), Column(0));
        let end = Point::new(grid.bottommost_line(), grid.last_column());
        let matches: Vec<Match> =
            RegexIter::new(start, end, Direction::Right, term, &mut regex).collect();
        let origin = term
            .selection
            .as_ref()
            .and_then(|selection| selection.to_range(term))
            .map(|range| range.start);
        let last = matches.len().checked_sub(1);
        self.at = match origin {
            None => last,
            Some(origin) if older => matches
                .iter()
                .rposition(|found| *found.start() < origin)
                .or(last),
            Some(origin) => matches
                .iter()
                .position(|found| *found.start() > origin)
                .or(last.map(|_| 0)),
        };
        let picked = self.at.and_then(|at| matches.get(at)).cloned();
        self.matches = Some(matches);
        Ok(picked)
    }

    pub(crate) fn count(&self) -> usize {
        self.matches.as_ref().map_or(0, Vec::len)
    }

    pub(crate) fn visible(&self, top: Line, bottom: Line) -> &[Match] {
        let matches = self.matches.as_deref().unwrap_or_default();
        let first = matches.partition_point(|found| found.end().line < top);
        let past = matches.partition_point(|found| found.start().line <= bottom);
        matches.get(first..past.max(first)).unwrap_or_default()
    }

    pub(crate) fn bar(
        &self,
        palette: &Palette,
        font: Pixels,
        on_case: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
    ) -> impl IntoElement {
        let status = match (&self.matches, self.at) {
            (None, _) => "Enter".to_owned(),
            (Some(matches), Some(at)) => format!("{} of {}", at + 1, matches.len()),
            (Some(_), None) => "none".to_owned(),
        };
        let edge = palette.foreground.opacity(EDGE);
        div()
            .absolute()
            .top(px(INSET))
            .right(px(RIGHT))
            .block_mouse_except_scroll()
            .flex()
            .flex_row()
            .items_center()
            .gap(px(GAP))
            .px(px(PAD))
            .py(px(PAD / 2.))
            .rounded(px(RADIUS))
            .border_1()
            .border_color(edge)
            .bg(surface(palette))
            .font_family(FAMILY)
            .text_size(font)
            .text_color(palette.foreground)
            .whitespace_nowrap()
            .child(
                div()
                    .w(px(FIELD))
                    .flex_none()
                    .overflow_hidden()
                    .child(format!("{}\u{258f}", self.query)),
            )
            .child(
                div()
                    .w(px(COUNT))
                    .flex_none()
                    .flex()
                    .justify_end()
                    .opacity(QUIET)
                    .child(status),
            )
            .child(
                div()
                    .id("terminal-find-case")
                    .w(px(TOGGLE))
                    .flex_none()
                    .flex()
                    .justify_center()
                    .rounded(px(RADIUS / 2.))
                    .border_1()
                    .border_color(match self.case {
                        true => palette.foreground,
                        false => edge,
                    })
                    .when(!self.case, |toggle| toggle.opacity(QUIET))
                    .cursor_pointer()
                    .on_click(on_case)
                    .child("Aa"),
            )
    }
}
