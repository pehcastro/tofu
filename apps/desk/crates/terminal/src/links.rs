use crate::Events;
use alacritty_terminal::grid::Dimensions;
use alacritty_terminal::index::{Column, Line, Point};
use alacritty_terminal::term::Term;
use std::ops::RangeInclusive;

const SCHEMES: [&str; 2] = ["https://", "http://"];
const BREAKS: &[char] = &['"', '\'', '<', '>', '`', '(', ')', '[', ']', '{', '}', '|'];
const TRAILING: &[char] = &['.', ',', ';', ':', '!', '?', '\'', '"'];

#[derive(Clone, PartialEq, Eq, Debug)]
pub(crate) struct Link {
    pub line: Line,
    pub columns: RangeInclusive<usize>,
    pub url: String,
}

fn openable(url: &str) -> bool {
    SCHEMES
        .iter()
        .any(|scheme| url.len() > scheme.len() && url.starts_with(scheme))
}

pub(crate) fn link_at(term: &Term<Events>, at: Point) -> Option<Link> {
    let grid = term.grid();
    let columns = grid.columns();
    if at.line < grid.topmost_line() || at.line > grid.bottommost_line() || at.column.0 >= columns {
        return None;
    }
    let row = &grid[at.line];
    let cells: Vec<_> = (0..columns).map(|col| &row[Column(col)]).collect();
    if let Some(hyperlink) = cells.get(at.column.0)?.hyperlink() {
        let same = |col: &usize| {
            cells.get(*col).and_then(|cell| cell.hyperlink()).as_ref() == Some(&hyperlink)
        };
        let start = (0..=at.column.0).rev().take_while(same).last()?;
        let end = (at.column.0..columns).take_while(same).last()?;
        let url = hyperlink.uri().to_owned();
        return openable(&url).then_some(Link {
            line: at.line,
            columns: start..=end,
            url,
        });
    }
    let chars: Vec<char> = cells.iter().map(|cell| cell.c).collect();
    let (columns, url) = url_around(&chars, at.column.0)?;
    Some(Link {
        line: at.line,
        columns,
        url,
    })
}

fn url_around(chars: &[char], column: usize) -> Option<(RangeInclusive<usize>, String)> {
    let breaks = |ch: &char| ch.is_whitespace() || BREAKS.contains(ch);
    if breaks(chars.get(column)?) {
        return None;
    }
    let start = chars
        .get(..column)?
        .iter()
        .rposition(breaks)
        .map_or(0, |at| at + 1);
    let end = chars
        .get(column..)?
        .iter()
        .position(breaks)
        .map_or(chars.len(), |at| column + at);
    let token: String = chars.get(start..end)?.iter().collect();
    let begin = SCHEMES
        .iter()
        .filter_map(|scheme| token.find(scheme))
        .min()?;
    let url = token.get(begin..)?.trim_end_matches(TRAILING);
    let first = start + token.get(..begin)?.chars().count();
    let last = (first + url.chars().count()).checked_sub(1)?;
    (openable(url) && (first..=last).contains(&column)).then(|| (first..=last, url.to_owned()))
}
