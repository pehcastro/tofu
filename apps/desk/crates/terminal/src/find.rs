use crate::Events;
use crate::paint::FAMILY;
use crate::palette::Palette;
use alacritty_terminal::grid::Dimensions;
use alacritty_terminal::index::{Column, Direction, Line, Point};
use alacritty_terminal::term::Term;
use alacritty_terminal::term::search::{Match, RegexIter, RegexSearch};
use gpui::{ColorExt, IntoElement, Pixels, div, prelude::*, px};

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

#[derive(Default)]
pub(crate) struct Find {
    pub query: String,
    matches: Option<Vec<Match>>,
    at: Option<usize>,
}

impl Find {
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
        let pattern: String = self
            .query
            .to_lowercase()
            .chars()
            .flat_map(|ch| META.contains(&ch).then_some('\\').into_iter().chain([ch]))
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

    pub(crate) fn bar(&self, palette: &Palette, font: Pixels) -> impl IntoElement {
        let status = match (&self.matches, self.at) {
            (None, _) => "Enter to find".to_owned(),
            (Some(matches), Some(at)) => format!("{} of {}", at + 1, matches.len()),
            (Some(_), None) => "no matches".to_owned(),
        };
        div()
            .absolute()
            .top(px(INSET))
            .right(px(RIGHT))
            .block_mouse_except_scroll()
            .flex()
            .items_center()
            .gap(px(GAP))
            .px(px(PAD))
            .py(px(PAD / 2.))
            .rounded(px(RADIUS))
            .border_1()
            .border_color(palette.foreground.opacity(EDGE))
            .bg(palette.background)
            .font_family(FAMILY)
            .text_size(font)
            .text_color(palette.foreground)
            .child(
                div()
                    .min_w(px(FIELD))
                    .child(format!("{}\u{258f}", self.query)),
            )
            .child(div().opacity(QUIET).child(status))
    }
}
