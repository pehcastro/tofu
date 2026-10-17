use std::collections::{BTreeMap, HashMap};
use std::ops::Range;
use std::rc::Rc;

use gpui::{
    AnyElement, App, Axis, Bounds, ContentMask, Div, ElementId, Font, FontStyle, FontWeight,
    HighlightStyle, LineFragment, LineWrapperHandle, ListHorizontalSizingBehavior, PathBuilder,
    Pixels, Point, Rgba, ScrollHandle, ScrollStrategy, ShapedLine, SharedString, StyledText,
    TextAlign, TextRun, UniformListScrollHandle, Window, canvas, combine_highlights, div, fill,
    font, point, prelude::*, px, rgb_to_hsla, size, uniform_list,
};

use crate::components::chip::{mono, tabular};
use crate::components::diff::Highlight;
use crate::components::find::match_highlights;
use crate::components::paint::{ink, tint};
use crate::components::scroll::scrollbar;
use crate::components::size::{
    CARET_HEIGHT, CARET_WIDTH, CODE_LINE, FONT_BODY, NUMBER_COLUMN, NUMBER_PAD, NUMBER_TEXT,
    SELECTION_FILL, T1, T3,
};
use crate::live::ActiveTheme;
use crate::theme::{ColorToken, Theme};

const CODE_PAD_TOP: f32 = 2.0;
const MARK_INSET: f32 = 6.0;
const MARK_WIDTH: f32 = 3.0;
const STRIPE_FILL: f32 = 0.08;
const MARK_RADIUS: f32 = 2.0;
const MARK_TOP: f32 = 3.0;
const REMOVED_SIZE: f32 = 6.0;
const EDGE_WIDTH: f32 = 2.0;
const WRAP_MARGIN: f32 = 12.0;
const FOLD_X: f32 = NUMBER_COLUMN - NUMBER_PAD + 4.5;
const FOLD_HALF: f32 = 3.0;
const FOLD_STROKE: f32 = 1.5;
pub const TAB_STOP: usize = 4;
const GUIDE_WIDTH: f32 = 1.0;
const GUIDE_SHARE: f32 = 4.4;
const ACTIVE_GUIDE_SHARE: f32 = 7.2;
const BLANK_DOT: f32 = 2.0;
const BLANK_INSET: f32 = 2.0;
const BLANK_HEAD: f32 = 3.0;
const BLANK_STROKE: f32 = 1.0;
const HEADING_ONE: f32 = 1.4;
const HEADING_TWO: f32 = 1.25;
const HEADING_THREE: f32 = 1.12;
const HEADING_ONE_ROWS: f32 = 1.6;
const HEADING_TWO_ROWS: f32 = 1.4;
const HEADING_THREE_ROWS: f32 = 1.2;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Blank {
    Space,
    Tab,
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Guides {
    pub levels: usize,
    pub active: Option<usize>,
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub enum Heading {
    #[default]
    Body,
    One,
    Two,
    Three,
    Minor,
}

impl Heading {
    pub fn level(hashes: usize) -> Self {
        match hashes {
            0 => Heading::Body,
            1 => Heading::One,
            2 => Heading::Two,
            3 => Heading::Three,
            _ => Heading::Minor,
        }
    }

    pub fn size(self) -> Pixels {
        let scale = match self {
            Heading::Body | Heading::Minor => 1.0,
            Heading::One => HEADING_ONE,
            Heading::Two => HEADING_TWO,
            Heading::Three => HEADING_THREE,
        };
        px(FONT_BODY * scale)
    }

    pub fn height(self) -> Pixels {
        let rows = match self {
            Heading::Body | Heading::Minor => 1.0,
            Heading::One => HEADING_ONE_ROWS,
            Heading::Two => HEADING_TWO_ROWS,
            Heading::Three => HEADING_THREE_ROWS,
        };
        px((CODE_LINE * rows).round())
    }

    fn font(self, theme: &Theme) -> Font {
        match self {
            Heading::Body => font(mono(theme)),
            _ => font(mono(theme)).bold(),
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Face {
    Bold,
    Italic,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct CodeLine {
    pub text: SharedString,
    pub runs: Highlight,
    pub heading: Heading,
    pub faces: Vec<(Range<usize>, Face)>,
    pub selected: Vec<Range<usize>>,
    pub carets: Vec<usize>,
    pub matches: Vec<Range<usize>>,
    pub current_match: Option<usize>,
    pub guides: Guides,
    pub blanks: Vec<(Range<usize>, Blank)>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum GutterMark {
    Changed(ColorToken),
    Removed(ColorToken),
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum FoldMark {
    Open,
    Folded,
}

pub type Trailing = Rc<dyn Fn(&Theme) -> AnyElement>;

#[derive(Clone, Default)]
pub struct LineMarks {
    pub gutter: Option<GutterMark>,
    pub edge: Option<ColorToken>,
    pub background: Option<ColorToken>,
    pub trailing: Option<Trailing>,
    pub fold: Option<FoldMark>,
}

pub type Marks = BTreeMap<usize, LineMarks>;

#[derive(Clone)]
pub struct Stripe {
    pub lines: Range<usize>,
    pub token: ColorToken,
    pub opacity: f32,
}

type LineSource = Rc<dyn Fn(Range<usize>, &mut App) -> Vec<CodeLine>>;

#[derive(Clone)]
struct WrappedLine {
    key: u64,
    breaks: Rc<[usize]>,
}

#[derive(Clone, Default)]
pub struct Wrap {
    width: Pixels,
    lines: Vec<WrappedLine>,
    starts: Vec<usize>,
}

fn wrap_breaks(
    wrappers: &mut HashMap<Heading, LineWrapperHandle>,
    (text, heading): (String, Heading),
    width: Pixels,
    theme: &Theme,
    cx: &App,
) -> Rc<[usize]> {
    wrappers
        .entry(heading)
        .or_insert_with(|| {
            cx.text_system()
                .line_wrapper(heading.font(theme), heading.size())
        })
        .wrap_line(&[LineFragment::text(&text)], width)
        .map(|boundary| boundary.ix)
        .collect()
}

pub fn wrap_width(bounds: Bounds<Pixels>) -> Pixels {
    bounds.size.width - px(NUMBER_COLUMN + WRAP_MARGIN)
}

impl Wrap {
    pub fn new(
        previous: &Wrap,
        keys: impl Iterator<Item = u64>,
        text_of: impl Fn(usize) -> (String, Heading),
        width: Pixels,
        theme: &Theme,
        cx: &App,
    ) -> Self {
        let known: HashMap<u64, Rc<[usize]>> = match previous.width == width {
            true => previous
                .lines
                .iter()
                .map(|line| (line.key, line.breaks.clone()))
                .collect(),
            false => HashMap::new(),
        };
        let mut wrappers = HashMap::new();
        let lines =
            keys.enumerate()
                .map(|(line, key)| {
                    let breaks = known.get(&key).cloned().unwrap_or_else(|| {
                        wrap_breaks(&mut wrappers, text_of(line), width, theme, cx)
                    });
                    WrappedLine { key, breaks }
                })
                .collect();
        let mut wrap = Wrap {
            width,
            lines,
            starts: vec![0],
        };
        wrap.restart(0);
        wrap
    }

    pub fn splice(
        &mut self,
        old: Range<usize>,
        fresh: Vec<(u64, String, Heading)>,
        theme: &Theme,
        cx: &App,
    ) -> (Range<usize>, Range<usize>) {
        let len = self.lines.len();
        let old = old.start.min(len)..old.end.min(len);
        let row = |wrap: &Wrap, line: usize| wrap.starts.get(line).copied().unwrap_or(0);
        let gone = row(self, old.start)..row(self, old.end);
        let came = old.start + fresh.len();
        let mut wrappers = HashMap::new();
        let lines: Vec<WrappedLine> = fresh
            .into_iter()
            .map(|(key, text, heading)| WrappedLine {
                key,
                breaks: wrap_breaks(&mut wrappers, (text, heading), self.width, theme, cx),
            })
            .collect();
        self.lines.splice(old.clone(), lines);
        self.restart(old.start);
        (gone, row(self, old.start)..row(self, came))
    }

    fn restart(&mut self, from: usize) {
        self.starts.truncate(from + 1);
        let mut next = self.starts.last().copied().unwrap_or(0);
        for line in self.lines.iter().skip(from) {
            next += line.breaks.len() + 1;
            self.starts.push(next);
        }
    }

    pub fn width(&self) -> Pixels {
        self.width
    }

    pub fn rows(&self) -> usize {
        self.starts.last().copied().unwrap_or(0)
    }

    pub fn locate(&self, row: usize) -> (usize, usize) {
        let line = self
            .starts
            .partition_point(|start| *start <= row)
            .saturating_sub(1);
        let first = self.starts.get(line).copied().unwrap_or(0);
        (line, row.saturating_sub(first))
    }

    pub fn pieces(&self, line: usize) -> usize {
        self.lines
            .get(line)
            .map_or(1, |wrapped| wrapped.breaks.len() + 1)
    }

    pub fn piece(&self, line: usize, piece: usize, len: usize) -> Range<usize> {
        let Some(breaks) = self.lines.get(line).map(|wrapped| &wrapped.breaks) else {
            return 0..len;
        };
        let start = piece
            .checked_sub(1)
            .and_then(|before| breaks.get(before))
            .copied()
            .unwrap_or(0);
        let end = breaks.get(piece).copied().unwrap_or(len);
        start.min(len)..end.clamp(start.min(len), len)
    }

    pub fn row_of(&self, line: usize, byte: usize) -> usize {
        let first = self.starts.get(line).copied().unwrap_or(0);
        let within = self.lines.get(line).map_or(0, |wrapped| {
            wrapped.breaks.partition_point(|at| *at <= byte)
        });
        first + within
    }
}

#[derive(Clone, Default)]
pub struct Heights(Option<Rc<[Pixels]>>);

impl Heights {
    pub fn measured(rows: impl Iterator<Item = Heading>) -> Self {
        let tops = rows.scan(px(0.0), |top, heading| {
            *top += heading.height();
            Some(*top)
        });
        Heights(Some(std::iter::once(px(0.0)).chain(tops).collect()))
    }

    pub fn spliced(&self, old: Range<usize>, fresh: impl Iterator<Item = Heading>) -> Option<Self> {
        let tops = self.0.as_ref()?;
        let (first, end) = (*tops.get(old.start)?, *tops.get(old.end)?);
        let mut spliced = Vec::with_capacity(tops.len());
        spliced.extend_from_slice(tops.get(..=old.start)?);
        let mut top = first;
        for heading in fresh {
            top += heading.height();
            spliced.push(top);
        }
        let moved = top - end;
        spliced.extend(tops.get(old.end + 1..)?.iter().map(|after| *after + moved));
        Some(Heights(Some(spliced.into())))
    }

    fn is_measured(&self) -> bool {
        self.0.is_some()
    }

    pub fn top(&self, row: usize) -> Pixels {
        let line = px(CODE_LINE);
        let Some(tops) = &self.0 else {
            return line * row as f32;
        };
        let last = tops.len().saturating_sub(1);
        tops.get(row).copied().unwrap_or_else(|| {
            tops.get(last).copied().unwrap_or_default() + line * row.saturating_sub(last) as f32
        })
    }

    pub fn height(&self, row: usize) -> Pixels {
        self.top(row + 1) - self.top(row)
    }

    fn row_at(&self, y: Pixels) -> usize {
        let line = px(CODE_LINE);
        let Some(tops) = &self.0 else {
            return (y / line).floor().max(0.0) as usize;
        };
        let below = tops.partition_point(|top| *top <= y);
        if below < tops.len() {
            return below.saturating_sub(1);
        }
        let last = tops.len().saturating_sub(1);
        let rest = y - tops.get(last).copied().unwrap_or_default();
        last + (rest / line).floor().max(0.0) as usize
    }

    fn shown(&self, scrolled: Pixels, height: Pixels, count: usize) -> Range<usize> {
        let first = self.row_at(scrolled);
        let end = match self.0 {
            Some(_) => self.row_at(scrolled + height) + 1,
            None => first + (height / px(CODE_LINE)).ceil() as usize + 1,
        };
        first.min(count)..count.min(end)
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Reveal {
    Minimal,
    Centre,
}

pub fn reveal_row(scroll: &UniformListScrollHandle, heights: &Heights, row: usize, how: Reveal) {
    if !heights.is_measured() {
        let strategy = match how {
            Reveal::Minimal => ScrollStrategy::Nearest,
            Reveal::Centre => ScrollStrategy::Center,
        };
        scroll.scroll_to_item(row, strategy);
        return;
    }
    let base = scroll.0.borrow().base_handle.clone();
    let offset = base.offset();
    let seen = -offset.y;
    let view = base.bounds().size.height;
    let (top, bottom) = (heights.top(row), heights.top(row + 1) + px(CODE_PAD_TOP));
    let to = match how {
        Reveal::Centre => (top + px(CODE_PAD_TOP) + bottom - view) / 2.0,
        Reveal::Minimal if top < seen => top,
        Reveal::Minimal if bottom > seen + view => bottom - view,
        Reveal::Minimal => return,
    };
    base.set_offset(point(offset.x, -to.max(px(0.0))));
}

#[derive(Clone, Default)]
struct RowMap {
    wrap: Option<Rc<Wrap>>,
    shown: Option<Rc<[usize]>>,
}

impl RowMap {
    fn line_at(&self, row: usize) -> (usize, bool) {
        match (&self.wrap, &self.shown) {
            (Some(wrap), _) => {
                let (line, piece) = wrap.locate(row);
                (line, piece == 0)
            }
            (None, Some(shown)) => (shown.get(row).copied().unwrap_or(row), true),
            (None, None) => (row, true),
        }
    }

    fn count(&self, lines: usize) -> usize {
        match (&self.wrap, &self.shown) {
            (Some(wrap), _) => wrap.rows(),
            (None, Some(shown)) => shown.len(),
            (None, None) => lines,
        }
    }
}

fn clip(range: &Range<usize>, within: &Range<usize>) -> Option<Range<usize>> {
    let (start, end) = (range.start.max(within.start), range.end.min(within.end));
    (start < end).then(|| start - within.start..end - within.start)
}

fn piece_of(line: &CodeLine, within: Range<usize>, last: bool) -> CodeLine {
    let inside = |range: &Range<usize>| clip(range, &within);
    let current_match = line
        .current_match
        .filter(|current| line.matches.get(*current).and_then(inside).is_some())
        .map(|current| line.matches.iter().take(current).filter_map(inside).count());
    CodeLine {
        text: line
            .text
            .get(within.clone())
            .unwrap_or_default()
            .to_owned()
            .into(),
        runs: line
            .runs
            .iter()
            .filter_map(|(range, token)| Some((inside(range)?, *token)))
            .collect(),
        heading: line.heading,
        faces: line
            .faces
            .iter()
            .filter_map(|(range, face)| Some((inside(range)?, *face)))
            .collect(),
        selected: line.selected.iter().filter_map(inside).collect(),
        carets: line
            .carets
            .iter()
            .filter(|at| within.contains(at) || (last && **at == within.end))
            .map(|at| at - within.start)
            .collect(),
        matches: line.matches.iter().filter_map(inside).collect(),
        current_match,
        guides: match within.start {
            0 => line.guides,
            _ => Guides::default(),
        },
        blanks: line
            .blanks
            .iter()
            .filter_map(|(range, blank)| Some((inside(range)?, *blank)))
            .collect(),
    }
}

#[derive(IntoElement)]
pub struct CodeView {
    id: ElementId,
    count: usize,
    widest: usize,
    scroll: UniformListScrollHandle,
    lines: LineSource,
    marks: Rc<Marks>,
    stripe: Option<Stripe>,
    rows: RowMap,
    heights: Heights,
    centre: Option<usize>,
}

pub fn code_view(
    id: impl Into<ElementId>,
    count: usize,
    scroll: &UniformListScrollHandle,
    lines: impl Fn(Range<usize>, &mut App) -> Vec<CodeLine> + 'static,
) -> CodeView {
    CodeView {
        id: id.into(),
        count,
        widest: 0,
        scroll: scroll.clone(),
        lines: Rc::new(lines),
        marks: Rc::default(),
        stripe: None,
        rows: RowMap::default(),
        heights: Heights::default(),
        centre: None,
    }
}

impl CodeView {
    pub fn widest(mut self, line: usize) -> Self {
        self.widest = line;
        self
    }

    pub fn wrap(mut self, wrap: Option<Rc<Wrap>>) -> Self {
        self.rows.wrap = wrap;
        self
    }

    pub fn shown(mut self, shown: Option<Rc<[usize]>>) -> Self {
        self.rows.shown = shown;
        self
    }

    pub fn marks(mut self, marks: Rc<Marks>) -> Self {
        self.marks = marks;
        self
    }

    pub fn stripe(mut self, stripe: Option<Stripe>) -> Self {
        self.stripe = stripe;
        self
    }

    pub fn heights(mut self, heights: Heights) -> Self {
        self.heights = heights;
        self
    }

    pub fn centre(mut self, row: Option<usize>) -> Self {
        self.centre = row;
        self
    }
}

fn backgrounds(
    marks: Rc<Marks>,
    stripe: Option<Stripe>,
    count: usize,
    rows: RowMap,
    heights: Heights,
    base: ScrollHandle,
) -> impl IntoElement {
    canvas(
        |_, _, _| {},
        move |bounds, _, window, cx| {
            let theme = ActiveTheme::theme(cx);
            let top = px(CODE_PAD_TOP) + base.offset().y;
            for row in heights.shown(-top, bounds.size.height, count) {
                let (line, _) = rows.line_at(row);
                let area = Bounds::new(
                    point(bounds.origin.x, bounds.origin.y + top + heights.top(row)),
                    size(bounds.size.width, heights.height(row)),
                );
                if let Some(token) = marks.get(&line).and_then(|mark| mark.background) {
                    window.paint_quad(fill(area, theme.color(token)));
                }
                if let Some(stripe) = stripe.as_ref().filter(|s| s.lines.contains(&line)) {
                    let mut colour = theme.color(stripe.token);
                    colour.alpha = STRIPE_FILL * stripe.opacity;
                    window.paint_quad(fill(area, colour));
                }
            }
        },
    )
    .absolute()
    .top_0()
    .left_0()
    .size_full()
}

fn paint_marks(
    mark: &LineMarks,
    first: bool,
    left: Pixels,
    (y, height): (Pixels, Pixels),
    theme: &Theme,
    window: &mut Window,
) {
    if let Some(token) = mark.edge {
        let edge = Bounds::new(point(left, y), size(px(EDGE_WIDTH), height));
        window.paint_quad(fill(edge, theme.color(token)));
    }
    match mark.gutter.filter(|_| first) {
        Some(GutterMark::Changed(token)) => {
            let x = left + px(NUMBER_COLUMN - MARK_INSET - MARK_WIDTH);
            let bar = Bounds::new(
                point(x, y + px(MARK_TOP)),
                size(px(MARK_WIDTH), height - px(2.0 * MARK_TOP)),
            );
            window.paint_quad(fill(bar, theme.color(token)).corner_radii(px(MARK_RADIUS)));
        }
        Some(GutterMark::Removed(token)) => {
            let x = left + px(NUMBER_COLUMN - MARK_INSET - REMOVED_SIZE);
            let top = y + height - px(REMOVED_SIZE / 2.0);
            let mut shape = PathBuilder::fill();
            shape.move_to(point(x, top));
            shape.line_to(point(x + px(REMOVED_SIZE), top + px(REMOVED_SIZE / 2.0)));
            shape.line_to(point(x, top + px(REMOVED_SIZE)));
            shape.close();
            if let Ok(path) = shape.build() {
                window.paint_path(path, theme.color(token));
            }
        }
        None => {}
    }
    if let Some(fold) = mark.fold.filter(|_| first) {
        let (x, mid, half) = (left + px(FOLD_X), y + height / 2.0, px(FOLD_HALF));
        let mut shape = PathBuilder::stroke(px(FOLD_STROKE));
        match fold {
            FoldMark::Open => {
                shape.move_to(point(x - half, mid - half / 2.0));
                shape.line_to(point(x, mid + half / 2.0));
                shape.line_to(point(x + half, mid - half / 2.0));
            }
            FoldMark::Folded => {
                shape.move_to(point(x - half / 2.0, mid - half));
                shape.line_to(point(x + half / 2.0, mid));
                shape.line_to(point(x - half / 2.0, mid + half));
            }
        }
        if let Ok(path) = shape.build() {
            window.paint_path(path, ink(theme, T3));
        }
    }
}

pub fn code_origin(
    bounds: Bounds<Pixels>,
    scroll: &UniformListScrollHandle,
    heights: &Heights,
    row: usize,
) -> Point<Pixels> {
    let offset = scroll.0.borrow().base_handle.offset();
    point(
        bounds.left() + px(NUMBER_COLUMN) + offset.x,
        bounds.top() + px(CODE_PAD_TOP) + offset.y + heights.top(row),
    )
}

pub fn code_place(
    bounds: Bounds<Pixels>,
    scroll: &UniformListScrollHandle,
    heights: &Heights,
    position: Point<Pixels>,
) -> (usize, Pixels) {
    let origin = code_origin(bounds, scroll, heights, 0);
    (heights.row_at(position.y - origin.y), position.x - origin.x)
}

pub fn code_shape(
    text: SharedString,
    heading: Heading,
    window: &Window,
    theme: &Theme,
) -> ShapedLine {
    let run = TextRun {
        len: text.len(),
        font: heading.font(theme),
        color: rgb_to_hsla(ink(theme, T1)),
        background_color: None,
        underline: None,
        strikethrough: None,
        letter_spacing: None,
    };
    window
        .text_system()
        .shape_line(text, heading.size(), &[run], None)
}

fn paint_blank(
    blank: Blank,
    from: Pixels,
    to: Pixels,
    mid: Pixels,
    shade: Rgba,
    window: &mut Window,
) {
    match blank {
        Blank::Space => {
            let half = px(BLANK_DOT / 2.0);
            let centre = (from + to) / 2.0;
            let dot = Bounds::new(
                point(centre - half, mid - half),
                size(half * 2.0, half * 2.0),
            );
            window.paint_quad(fill(dot, shade).corner_radii(half));
        }
        Blank::Tab => {
            let (left, right) = (from + px(BLANK_INSET), to - px(BLANK_INSET));
            let head = px(BLANK_HEAD);
            let mut arrow = PathBuilder::stroke(px(BLANK_STROKE));
            arrow.move_to(point(left, mid));
            arrow.line_to(point(right, mid));
            arrow.move_to(point(right - head, mid - head));
            arrow.line_to(point(right, mid));
            arrow.line_to(point(right - head, mid + head));
            if let Ok(path) = arrow.build() {
                window.paint_path(path, shade);
            }
        }
    }
}

fn code_row(
    line: &CodeLine,
    height: Pixels,
    trailing: Option<&Trailing>,
    cell: Pixels,
    theme: &Theme,
) -> Div {
    let runs = line.runs.iter().map(|(range, token)| {
        (
            range.clone(),
            HighlightStyle {
                color: Some(rgb_to_hsla(theme.color(*token))),
                ..HighlightStyle::default()
            },
        )
    });
    let selection = HighlightStyle {
        background_color: Some(rgb_to_hsla(tint(
            theme.color(ColorToken::FocusRing),
            SELECTION_FILL,
        ))),
        ..HighlightStyle::default()
    };
    let faces = line.faces.iter().map(|(range, face)| {
        let style = match face {
            Face::Bold => HighlightStyle {
                font_weight: Some(FontWeight::BOLD),
                ..HighlightStyle::default()
            },
            Face::Italic => HighlightStyle {
                font_style: Some(FontStyle::Italic),
                ..HighlightStyle::default()
            },
        };
        (range.clone(), style)
    });
    let selected = line.selected.iter().map(|range| (range.clone(), selection));
    let found = match_highlights(&line.matches, line.current_match, theme);
    let text = StyledText::new(line.text.clone())
        .with_highlights(combine_highlights(runs.chain(faces), selected.chain(found)));
    let layout = text.layout().clone();
    let carets = line.carets.clone();
    let blanks = line.blanks.clone();
    let guides = line.guides;
    let color = ink(theme, T1);
    let blank_ink = ink(theme, T3);
    let border = theme.color(ColorToken::Separator);
    let guide_ink = |share: f32| tint(border, (border.alpha * share).min(1.0));
    let (guide, active_guide) = (guide_ink(GUIDE_SHARE), guide_ink(ACTIVE_GUIDE_SHARE));
    let drawn = !carets.is_empty() || !blanks.is_empty() || guides.levels > 0;
    let caret_height = px(CARET_HEIGHT) * (line.heading.size() / px(FONT_BODY));
    div()
        .flex()
        .items_center()
        .h(height)
        .whitespace_nowrap()
        .font_family(mono(theme))
        .text_size(line.heading.size())
        .when(line.heading != Heading::Body, |row| {
            row.font_weight(FontWeight::BOLD)
        })
        .line_height(height)
        .child(
            div()
                .flex_none()
                .relative()
                .text_color(theme.color(ColorToken::SyntaxVariable))
                .child(text)
                .when(drawn, |row| {
                    row.child(
                        canvas(
                            |_, _, _| {},
                            move |bounds, _, window, _| {
                                for level in 0..guides.levels {
                                    let x = bounds.origin.x + cell * (level * TAB_STOP) as f32;
                                    let line = Bounds::new(
                                        point(x, bounds.origin.y),
                                        size(px(GUIDE_WIDTH), height),
                                    );
                                    let shade = match guides.active == Some(level) {
                                        true => active_guide,
                                        false => guide,
                                    };
                                    window.paint_quad(fill(line, shade));
                                }
                                for (range, blank) in &blanks {
                                    let (Some(from), Some(to)) = (
                                        layout.position_for_index(range.start),
                                        layout.position_for_index(range.end),
                                    ) else {
                                        continue;
                                    };
                                    let mid = from.y + height / 2.0;
                                    paint_blank(*blank, from.x, to.x, mid, blank_ink, window);
                                }
                                for at in &carets {
                                    let Some(origin) = layout.position_for_index(*at) else {
                                        continue;
                                    };
                                    let top = origin.y + (height - caret_height) / 2.0;
                                    let caret = Bounds::new(
                                        point(origin.x, top),
                                        size(px(CARET_WIDTH), caret_height),
                                    );
                                    window.paint_quad(fill(caret, color));
                                }
                            },
                        )
                        .absolute()
                        .top_0()
                        .left_0()
                        .size_full(),
                    )
                }),
        )
        .children(trailing.map(|build| build(theme)))
}

struct RowSource {
    lines: LineSource,
    marks: Rc<Marks>,
    rows: RowMap,
    heights: Heights,
}

impl RowSource {
    fn wrapped(
        &self,
        wrap: &Wrap,
        rows: Range<usize>,
        cell: Pixels,
        theme: &Theme,
        cx: &mut App,
    ) -> Vec<Div> {
        let (Some(first), Some(last)) = (
            rows.clone().next().map(|row| wrap.locate(row).0),
            rows.clone().last().map(|row| wrap.locate(row).0),
        ) else {
            return Vec::new();
        };
        let drawn = (self.lines)(first..last + 1, cx);
        rows.filter_map(|row| {
            let (line, piece) = wrap.locate(row);
            let code = drawn.get(line.checked_sub(first)?)?;
            let last = piece + 1 == wrap.pieces(line);
            let within = wrap.piece(line, piece, code.text.len());
            let trailing = self
                .marks
                .get(&line)
                .and_then(|mark| mark.trailing.as_ref())
                .filter(|_| last);
            Some(code_row(
                &piece_of(code, within, last),
                self.heights.height(row),
                trailing,
                cell,
                theme,
            ))
        })
        .collect()
    }

    fn draw(&self, range: Range<usize>, window: &mut Window, cx: &mut App) -> Vec<Div> {
        let theme = ActiveTheme::theme(cx);
        let cell = code_shape(" ".into(), Heading::Body, window, &theme).width();
        let (marks, heights) = (&self.marks, &self.heights);
        if let Some(wrap) = &self.rows.wrap {
            return self.wrapped(wrap, range, cell, &theme, cx);
        }
        (self.lines)(range.clone(), cx)
            .iter()
            .zip(range)
            .map(|(line, row)| {
                let trailing = marks
                    .get(&self.rows.line_at(row).0)
                    .and_then(|mark| mark.trailing.as_ref());
                code_row(line, heights.height(row), trailing, cell, &theme)
            })
            .collect()
    }
}

impl RenderOnce for CodeView {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let count = self.rows.count(self.count);
        let base = self.scroll.0.borrow().base_handle.clone();
        let placed = !self.heights.is_measured() || base.bounds().size.height > px(0.0);
        let unplaced = match (self.centre, placed) {
            (Some(row), true) => {
                reveal_row(&self.scroll, &self.heights, row, Reveal::Centre);
                None
            }
            (centre, _) => centre,
        };
        let view = window.current_view();
        let centre_once_placed = unplaced.map(|row| {
            let (scroll, heights) = (self.scroll.clone(), self.heights.clone());
            canvas(
                |_, _, _| {},
                move |_, _, _, cx| {
                    reveal_row(&scroll, &heights, row, Reveal::Centre);
                    cx.notify(view);
                },
            )
            .absolute()
            .size_0()
        });
        let source = RowSource {
            lines: self.lines,
            marks: self.marks.clone(),
            rows: self.rows.clone(),
            heights: self.heights.clone(),
        };
        let list = match self.heights.is_measured() {
            false => uniform_list(self.id.clone(), count, move |range, window, cx| {
                source.draw(range, window, cx)
            })
            .map(|list| match self.rows.wrap {
                Some(_) => {
                    list.with_horizontal_sizing_behavior(ListHorizontalSizingBehavior::FitList)
                }
                None => list
                    .with_width_from_item(Some(self.widest))
                    .with_horizontal_sizing_behavior(ListHorizontalSizingBehavior::Unconstrained),
            })
            .size_full()
            .pt(px(CODE_PAD_TOP))
            .track_scroll(&self.scroll)
            .into_any_element(),
            true => {
                let heights = &self.heights;
                let total = heights.top(count);
                let most = (total + px(CODE_PAD_TOP) - base.bounds().size.height).max(px(0.0));
                let seen = (-base.offset().y).min(most) - px(CODE_PAD_TOP);
                let shown = heights.shown(seen, window.viewport_size().height, count);
                let (above, below) = (heights.top(shown.start), total - heights.top(shown.end));
                let drawn = source.draw(shown, window, cx);
                div()
                    .id(self.id.clone())
                    .size_full()
                    .pt(px(CODE_PAD_TOP))
                    .flex()
                    .flex_col()
                    .items_start()
                    .map(|list| match self.rows.wrap {
                        Some(_) => list.overflow_y_scroll(),
                        None => list.overflow_scroll(),
                    })
                    .track_scroll(&base)
                    .child(div().flex_none().h(above))
                    .children(drawn.into_iter().map(|row| row.flex_none()))
                    .child(div().flex_none().h(below))
                    .into_any_element()
            }
        };
        let bar = |axis: Axis| {
            scrollbar(SharedString::from(format!("{}-{axis:?}", self.id)), &base).axis(axis)
        };
        div()
            .relative()
            .size_full()
            .when(!self.marks.is_empty() || self.stripe.is_some(), |view| {
                view.child(backgrounds(
                    self.marks.clone(),
                    self.stripe.clone(),
                    count,
                    self.rows.clone(),
                    self.heights.clone(),
                    base.clone(),
                ))
            })
            .child(div().size_full().pl(px(NUMBER_COLUMN)).child(list))
            .child(gutter(
                count,
                self.marks.clone(),
                self.stripe,
                (self.rows.clone(), self.heights.clone()),
                base.clone(),
                window,
                cx,
            ))
            .child(bar(Axis::Vertical))
            .when(self.rows.wrap.is_none(), |view| {
                view.child(bar(Axis::Horizontal))
            })
            .children(centre_once_placed)
    }
}

fn gutter(
    count: usize,
    marks: Rc<Marks>,
    stripe: Option<Stripe>,
    (rows, heights): (RowMap, Heights),
    base: ScrollHandle,
    window: &mut Window,
    cx: &mut App,
) -> Div {
    let theme = ActiveTheme::theme(cx);
    let run = TextRun {
        len: 0,
        font: Font {
            features: tabular(),
            ..font(mono(&theme))
        },
        color: rgb_to_hsla(ink(&theme, NUMBER_TEXT)),
        background_color: None,
        underline: None,
        strikethrough: None,
        letter_spacing: None,
    };
    let view = window.current_view();
    let wheel = base.clone();
    let numbers = canvas(
        move |bounds, window, _| {
            let top = px(CODE_PAD_TOP) + base.offset().y;
            heights
                .shown(-top, bounds.size.height, count)
                .map(|row| {
                    let (line, first) = rows.line_at(row);
                    let shaped = first.then(|| {
                        let text = SharedString::from((line + 1).to_string());
                        let run = TextRun {
                            len: text.len(),
                            ..run.clone()
                        };
                        window
                            .text_system()
                            .shape_line(text, px(FONT_BODY), &[run], None)
                    });
                    let y = bounds.origin.y + top + heights.top(row);
                    (line, first, (y, heights.height(row)), shaped)
                })
                .collect::<Vec<_>>()
        },
        move |bounds, rows, window, cx| {
            let theme = ActiveTheme::theme(cx);
            window.with_content_mask(
                Some(ContentMask {
                    bounds,
                    ..ContentMask::default()
                }),
                |window| {
                    for (line, first, (y, height), shaped) in rows {
                        if let Some(mark) = marks.get(&line) {
                            paint_marks(mark, first, bounds.origin.x, (y, height), &theme, window);
                        }
                        if let Some(stripe) = stripe.as_ref().filter(|s| s.lines.contains(&line)) {
                            let bar = Bounds::new(
                                point(bounds.origin.x, y),
                                size(px(MARK_WIDTH), height),
                            );
                            let mut colour = theme.color(stripe.token);
                            colour.alpha = stripe.opacity;
                            window.paint_quad(fill(bar, colour));
                        }
                        let Some(shaped) = shaped else {
                            continue;
                        };
                        let origin = point(bounds.origin.x, y);
                        let painted = shaped.paint(
                            origin,
                            height,
                            TextAlign::Right,
                            Some(px(NUMBER_COLUMN - NUMBER_PAD)),
                            window,
                            cx,
                        );
                        if painted.is_err() {
                            break;
                        }
                    }
                },
            );
        },
    )
    .size_full();
    div()
        .absolute()
        .top_0()
        .left_0()
        .h_full()
        .w(px(NUMBER_COLUMN))
        .on_scroll_wheel(move |event, window, cx| {
            let delta = event.delta.pixel_delta(window.line_height());
            let offset = wheel.offset();
            let x = (offset.x + delta.x).max(-wheel.max_offset().x).min(px(0.0));
            let y = (offset.y + delta.y).min(px(0.0));
            if point(x, y) != offset {
                wheel.set_offset(point(x, y));
                cx.notify(view);
            }
        })
        .child(numbers)
}
