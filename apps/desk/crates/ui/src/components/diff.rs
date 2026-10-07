use std::collections::HashSet;
use std::f32::consts::FRAC_PI_2;
use std::fmt;
use std::ops::Range;
use std::rc::Rc;
use std::time::Instant;

use desk_motion::reduced_motion;
use desk_motion::tokens::{EASE_OUT, HOVER, HOVER_MS};
use gpui::{
    Animation, AnimationExt, AnyElement, App, Div, ElementId, Entity, FontWeight, HighlightStyle,
    Motion, Rgba, SharedString, SpringState, StyledText, Transformation, Window, div, prelude::*,
    px, radians, rgb_to_hsla,
};

use crate::components::chip::{Tone, badge, mono, tabular};
use crate::components::glyph::Glyph;
use crate::components::paint::{glyph, ink, tint};
use crate::components::scroll::ScrollArea;
use crate::components::size::{
    CAPTION_TEXT, CODE_LINE, DIM_TEXT, FONT_BODY, FONT_SMALL, HEADER, HEADER_PAD_LEFT,
    HOVER as HOVER_FILL, NUMBER_COLUMN, NUMBER_PAD, NUMBER_TEXT, RADIUS_LIST, T1, T3,
};
use crate::live::ActiveTheme;
use crate::metrics::{HAIRLINE, ICON_SMALL};
use crate::theme::{ColorToken, Theme};

const KEPT_CONTEXT: usize = 3;
const FOLD_AT_LEAST: usize = 4;
const SIGN: f32 = 14.0;
const VISIBLE_LINES: f32 = 16.0;
const HUNK_GAP: f32 = 6.0;
const TAG_FILL: f32 = 0.14;
const FOLD_FILL: f32 = 0.03;
const TURN_SETTLED: f32 = 0.001;

pub type Highlight = Vec<(Range<usize>, ColorToken)>;

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum FileChange {
    Modified,
    Added,
    Deleted,
    Renamed { from: SharedString },
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum PatchError {
    HunkHeader { line: usize },
    OutsideHunk { line: usize },
    UnknownLine { line: usize },
    CountMismatch { hunk: usize },
}

impl fmt::Display for PatchError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            PatchError::HunkHeader { line } => write!(f, "line {line}: not a hunk header"),
            PatchError::OutsideHunk { line } => write!(f, "line {line}: before the first hunk"),
            PatchError::UnknownLine { line } => {
                write!(f, "line {line}: starts with none of space, + or -")
            }
            PatchError::CountMismatch { hunk } => {
                write!(f, "hunk {hunk}: line counts disagree with its header")
            }
        }
    }
}

impl std::error::Error for PatchError {}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum LineKind {
    Context,
    Added,
    Removed,
}

#[derive(Clone, Debug)]
struct Segment {
    range: Range<usize>,
    syntax: Option<ColorToken>,
    changed: bool,
}

#[derive(Clone, Debug)]
struct DiffLine {
    kind: LineKind,
    number: u32,
    text: SharedString,
    segments: Vec<Segment>,
}

#[derive(Clone, Debug)]
struct Hunk {
    header: SharedString,
    scope: SharedString,
    lines: Vec<DiffLine>,
}

#[derive(Clone, Debug)]
pub struct FileDiff {
    path: SharedString,
    change: FileChange,
    hunks: Vec<Hunk>,
    added: usize,
    removed: usize,
}

struct Parsed {
    kind: LineKind,
    number: u32,
    text: SharedString,
    syntax: Highlight,
}

#[derive(Default)]
struct Cursor {
    old: u32,
    new: u32,
    old_left: u32,
    new_left: u32,
}

impl Cursor {
    fn take(&mut self, kind: LineKind) -> Option<u32> {
        let take_old = |cursor: &mut Cursor| {
            cursor.old_left = cursor.old_left.checked_sub(1)?;
            let number = cursor.old;
            cursor.old = cursor.old.checked_add(1)?;
            Some(number)
        };
        let take_new = |cursor: &mut Cursor| {
            cursor.new_left = cursor.new_left.checked_sub(1)?;
            let number = cursor.new;
            cursor.new = cursor.new.checked_add(1)?;
            Some(number)
        };
        match kind {
            LineKind::Context => take_old(self).and_then(|_| take_new(self)),
            LineKind::Added => take_new(self),
            LineKind::Removed => take_old(self),
        }
    }

    fn spent(&self) -> bool {
        self.old_left == 0 && self.new_left == 0
    }
}

fn hunk_header(rest: &str) -> Option<([u32; 4], &str)> {
    let span = |text: &str| -> Option<(u32, u32)> {
        match text.split_once(',') {
            Some((start, count)) => Some((start.parse().ok()?, count.parse().ok()?)),
            None => Some((text.parse().ok()?, 1)),
        }
    };
    let (old, rest) = rest.strip_prefix('-')?.split_once(" +")?;
    let (new, scope) = rest.split_once(" @@")?;
    let (old_start, old_count) = span(old)?;
    let (new_start, new_count) = span(new)?;
    Some(([old_start, old_count, new_start, new_count], scope.trim()))
}

fn changed_ranges(old: &str, new: &str) -> Option<(Range<usize>, Range<usize>)> {
    let prefix: usize = old
        .chars()
        .zip(new.chars())
        .take_while(|(a, b)| a == b)
        .map(|(a, _)| a.len_utf8())
        .sum();
    let suffix: usize = old
        .get(prefix..)?
        .chars()
        .rev()
        .zip(new.get(prefix..)?.chars().rev())
        .take_while(|(a, b)| a == b)
        .map(|(a, _)| a.len_utf8())
        .sum();
    if prefix + suffix == 0 {
        return None;
    }
    Some((
        prefix..old.len().saturating_sub(suffix),
        prefix..new.len().saturating_sub(suffix),
    ))
}

fn run_len<T>(lines: &[T], kind: LineKind, kind_of: impl Fn(&T) -> LineKind) -> usize {
    lines
        .iter()
        .take_while(|line| kind_of(line) == kind)
        .count()
}

fn changes(lines: &[Parsed]) -> Vec<Option<Range<usize>>> {
    let mut changed = vec![None; lines.len()];
    let mut at = 0;
    while at < lines.len() {
        let rest = lines.get(at..).unwrap_or_default();
        let removed = run_len(rest, LineKind::Removed, |line| line.kind);
        let after = rest.get(removed..).unwrap_or_default();
        let added = run_len(after, LineKind::Added, |line| line.kind);
        for (pair, (old, new)) in rest.iter().zip(after).take(removed.min(added)).enumerate() {
            let (old_ix, new_ix) = (at + pair, at + removed + pair);
            if let Some((old_range, new_range)) = changed_ranges(&old.text, &new.text) {
                if let Some(slot) = changed.get_mut(old_ix) {
                    *slot = Some(old_range);
                }
                if let Some(slot) = changed.get_mut(new_ix) {
                    *slot = Some(new_range);
                }
            }
        }
        at += (removed + added).max(1);
    }
    changed
}

fn segments(text: &str, syntax: &Highlight, changed: Option<&Range<usize>>) -> Vec<Segment> {
    let mut cuts: Vec<usize> = syntax
        .iter()
        .map(|(range, _)| range)
        .chain(changed)
        .flat_map(|range| [range.start, range.end])
        .chain([0, text.len()])
        .filter(|&at| text.is_char_boundary(at))
        .collect();
    cuts.sort_unstable();
    cuts.dedup();
    cuts.windows(2)
        .filter_map(|pair| {
            let &[start, end] = pair else { return None };
            let inside = |range: &Range<usize>| range.start <= start && end <= range.end;
            let syntax = syntax
                .iter()
                .find(|(range, _)| inside(range))
                .map(|(_, token)| *token);
            let changed = changed.is_some_and(inside);
            (syntax.is_some() || changed).then_some(Segment {
                range: start..end,
                syntax,
                changed,
            })
        })
        .collect()
}

fn close(
    hunks: &mut Vec<Hunk>,
    open: Option<(SharedString, SharedString, Vec<Parsed>)>,
    cursor: &Cursor,
) -> Result<(), PatchError> {
    let Some((header, scope, parsed)) = open else {
        return Ok(());
    };
    if !cursor.spent() {
        return Err(PatchError::CountMismatch { hunk: hunks.len() });
    }
    let changed = changes(&parsed);
    let lines = parsed
        .into_iter()
        .zip(changed)
        .map(|(line, changed)| DiffLine {
            segments: segments(&line.text, &line.syntax, changed.as_ref()),
            kind: line.kind,
            number: line.number,
            text: line.text,
        })
        .collect();
    hunks.push(Hunk {
        header,
        scope,
        lines,
    });
    Ok(())
}

impl FileDiff {
    pub fn path(&self) -> &SharedString {
        &self.path
    }

    pub fn change(&self) -> &FileChange {
        &self.change
    }

    pub fn added(&self) -> usize {
        self.added
    }

    pub fn removed(&self) -> usize {
        self.removed
    }

    pub fn parse(
        path: impl Into<SharedString>,
        change: FileChange,
        patch: &str,
        highlight: impl Fn(&str) -> Highlight,
    ) -> Result<Self, PatchError> {
        let mut hunks = Vec::new();
        let mut open: Option<(SharedString, SharedString, Vec<Parsed>)> = None;
        let mut cursor = Cursor::default();
        for (ix, raw) in patch.lines().enumerate() {
            let line = ix + 1;
            if let Some(rest) = raw.strip_prefix("@@ ") {
                close(&mut hunks, open.take(), &cursor)?;
                let ([old, old_left, new, new_left], scope) =
                    hunk_header(rest).ok_or(PatchError::HunkHeader { line })?;
                cursor = Cursor {
                    old,
                    new,
                    old_left,
                    new_left,
                };
                let header = format!("@@ -{old},{old_left} +{new},{new_left} @@");
                open = Some((header.into(), scope.to_owned().into(), Vec::new()));
                continue;
            }
            if raw.starts_with('\\') {
                continue;
            }
            let Some((_, _, body)) = open.as_mut() else {
                return Err(PatchError::OutsideHunk { line });
            };
            let (kind, text) = match raw.split_at_checked(1) {
                Some((" ", text)) => (LineKind::Context, text),
                Some(("+", text)) => (LineKind::Added, text),
                Some(("-", text)) => (LineKind::Removed, text),
                None => (LineKind::Context, ""),
                Some(_) => return Err(PatchError::UnknownLine { line }),
            };
            let number = cursor
                .take(kind)
                .ok_or(PatchError::CountMismatch { hunk: hunks.len() })?;
            body.push(Parsed {
                kind,
                number,
                syntax: highlight(text),
                text: text.to_owned().into(),
            });
        }
        close(&mut hunks, open, &cursor)?;
        let count = |kind| {
            hunks
                .iter()
                .flat_map(|hunk| &hunk.lines)
                .filter(|line| line.kind == kind)
                .count()
        };
        let (added, removed) = (count(LineKind::Added), count(LineKind::Removed));
        Ok(FileDiff {
            path: path.into(),
            change,
            hunks,
            added,
            removed,
        })
    }
}

#[derive(Clone, Copy)]
enum Row {
    Line { line: usize, revealed: bool },
    Fold { from: usize, count: usize },
}

fn rows(lines: &[DiffLine], unfolded: impl Fn(usize) -> bool) -> Vec<Row> {
    let mut rows = Vec::new();
    let total = lines.len();
    let mut at = 0;
    while at < total {
        let rest = lines.get(at..).unwrap_or_default();
        let end = at + run_len(rest, LineKind::Context, |line| line.kind).max(1);
        let head = if at == 0 { 0 } else { KEPT_CONTEXT };
        let tail = if end == total { 0 } else { KEPT_CONTEXT };
        let hidden = (end - at).saturating_sub(head + tail);
        let shown =
            |range: Range<usize>, revealed| range.map(move |line| Row::Line { line, revealed });
        if hidden < FOLD_AT_LEAST {
            rows.extend(shown(at..end, false));
        } else {
            let (fold, keep) = (at + head, end - tail);
            rows.extend(shown(at..fold, false));
            if unfolded(at) {
                rows.extend(shown(fold..keep, true));
            } else {
                rows.push(Row::Fold {
                    from: at,
                    count: hidden,
                });
            }
            rows.extend(shown(keep..end, false));
        }
        at = end;
    }
    rows
}

#[derive(Clone, Copy)]
struct Turn {
    from: SpringState,
    target: f32,
    moved: Instant,
}

impl Turn {
    fn at(&self, now: Instant) -> SpringState {
        let elapsed = now.saturating_duration_since(self.moved).as_secs_f32();
        HOVER.step(self.from, self.target, elapsed)
    }

    fn toward(&mut self, target: f32, now: Instant) {
        *self = Turn {
            from: self.at(now),
            target,
            moved: now,
        };
    }
}

struct CardState {
    open: bool,
    turn: Turn,
    opened: usize,
    unfolded: HashSet<(usize, usize)>,
}

#[derive(IntoElement)]
pub struct DiffCard {
    id: ElementId,
    diff: Rc<FileDiff>,
    max_h: f32,
}

impl DiffCard {
    pub fn new(id: impl Into<ElementId>, diff: Rc<FileDiff>) -> Self {
        DiffCard {
            id: id.into(),
            diff,
            max_h: CODE_LINE * VISIBLE_LINES,
        }
    }

    pub fn max_h(mut self, height: f32) -> Self {
        self.max_h = height;
        self
    }
}

fn path_text(diff: &FileDiff, theme: &Theme) -> StyledText {
    let dim = HighlightStyle {
        color: Some(rgb_to_hsla(ink(theme, CAPTION_TEXT))),
        ..HighlightStyle::default()
    };
    let lead = match &diff.change {
        FileChange::Renamed { from } => format!("{from} \u{2192} "),
        _ => String::new(),
    };
    let dir = diff.path.rfind('/').map_or(0, |slash| slash + 1);
    let text = format!("{lead}{}", diff.path);
    StyledText::new(text).with_highlights([(0..lead.len() + dir, dim)])
}

fn change_tag(change: &FileChange, theme: &Theme) -> Option<Div> {
    let (label, token) = match change {
        FileChange::Modified => return None,
        FileChange::Added => ("new", ColorToken::GitAdded),
        FileChange::Deleted => ("deleted", ColorToken::GitDeleted),
        FileChange::Renamed { .. } => ("renamed", ColorToken::GitModified),
    };
    let color = theme.color(token);
    Some(
        badge(label, theme)
            .bg(tint(color, TAG_FILL))
            .text_color(color),
    )
}

fn counts(diff: &FileDiff, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_none()
        .gap_1p5()
        .font_family(mono(theme))
        .font_features(tabular())
        .when(diff.added > 0, |counts| {
            counts.child(
                div()
                    .text_color(Tone::Added.color(theme))
                    .child(format!("+{}", diff.added)),
            )
        })
        .when(diff.removed > 0, |counts| {
            counts.child(
                div()
                    .text_color(Tone::Deleted.color(theme))
                    .child(format!("-{}", diff.removed)),
            )
        })
}

fn line_row(line: &DiffLine, theme: &Theme) -> Div {
    let (fill, word, sign, sign_color) = match line.kind {
        LineKind::Context => (None, None, " ", ink(theme, NUMBER_TEXT)),
        LineKind::Added => (
            Some(theme.color(ColorToken::DiffAddedLine)),
            Some(theme.color(ColorToken::DiffAddedWord)),
            "+",
            Tone::Added.color(theme),
        ),
        LineKind::Removed => (
            Some(theme.color(ColorToken::DiffRemovedLine)),
            Some(theme.color(ColorToken::DiffRemovedWord)),
            "-",
            Tone::Deleted.color(theme),
        ),
    };
    let highlights = line.segments.iter().map(|segment| {
        (
            segment.range.clone(),
            HighlightStyle {
                color: segment.syntax.map(|token| rgb_to_hsla(theme.color(token))),
                background_color: word.filter(|_| segment.changed).map(rgb_to_hsla),
                ..HighlightStyle::default()
            },
        )
    });
    div()
        .h(px(CODE_LINE))
        .flex()
        .items_center()
        .when_some(fill, |row, fill: Rgba| row.bg(fill))
        .child(
            div()
                .w(px(NUMBER_COLUMN))
                .flex_none()
                .pr(px(NUMBER_PAD))
                .text_right()
                .text_color(ink(theme, NUMBER_TEXT))
                .child(line.number.to_string()),
        )
        .child(
            div()
                .w(px(SIGN))
                .flex_none()
                .text_color(sign_color)
                .child(sign),
        )
        .child(
            div()
                .flex_1()
                .min_w_0()
                .overflow_hidden()
                .whitespace_nowrap()
                .text_color(theme.color(ColorToken::SyntaxVariable))
                .child(StyledText::new(line.text.clone()).with_highlights(highlights)),
        )
}

fn hunk_row(hunk: &Hunk, first: bool, theme: &Theme) -> Div {
    div()
        .h(px(CODE_LINE))
        .when(!first, |row| row.mt(px(HUNK_GAP)))
        .flex()
        .items_center()
        .gap_2()
        .pl(px(NUMBER_COLUMN + SIGN))
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, T3))
        .child(hunk.header.clone())
        .child(div().truncate().child(hunk.scope.clone()))
}

fn fold_row(
    key: (usize, usize),
    count: usize,
    state: &Entity<CardState>,
    theme: &Theme,
) -> AnyElement {
    let state = state.clone();
    let rest = ink(theme, FOLD_FILL);
    let hover = ink(theme, HOVER_FILL);
    let noun = if count == 1 { "line" } else { "lines" };
    div()
        .id(("diff-fold", key.1))
        .h(px(CODE_LINE))
        .flex()
        .items_center()
        .gap_2()
        .pl(px(NUMBER_COLUMN + SIGN))
        .bg(rest)
        .cursor_pointer()
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, DIM_TEXT))
        .transitions(|transitions| transitions.bg(Motion::new(HOVER_MS).with_easing(EASE_OUT)))
        .hover(move |row| row.bg(hover))
        .on_click(move |_, _, cx| {
            state.update(cx, |state, cx| {
                state.unfolded.insert(key);
                cx.notify();
            })
        })
        .child(format!("\u{22ef} {count} unchanged {noun}"))
        .into_any_element()
}

fn fade_in(element: Div, id: impl Into<ElementId>) -> AnyElement {
    element
        .with_animation(
            id,
            Animation::new(HOVER_MS).with_easing(EASE_OUT),
            |element, t| element.opacity(t),
        )
        .into_any_element()
}

fn hunk_block(
    ix: usize,
    hunk: &Hunk,
    state: &Entity<CardState>,
    cx: &App,
    theme: &Theme,
) -> AnyElement {
    let unfolded = &state.read(cx).unfolded;
    let rows = rows(&hunk.lines, |from| unfolded.contains(&(ix, from)));
    let shown = rows.into_iter().filter_map(|row| match row {
        Row::Line { line, revealed } => {
            let shown = line_row(hunk.lines.get(line)?, theme);
            Some(if revealed {
                fade_in(shown, ("diff-reveal", line))
            } else {
                shown.into_any_element()
            })
        }
        Row::Fold { from, count } => Some(fold_row((ix, from), count, state, theme)),
    });
    div()
        .id(("diff-hunk", ix))
        .flex()
        .flex_col()
        .child(hunk_row(hunk, ix == 0, theme))
        .children(shown.collect::<Vec<_>>())
        .into_any_element()
}

impl RenderOnce for DiffCard {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let reduced = reduced_motion(cx);
        let now = Instant::now();
        let state = window.use_keyed_state(self.id.clone(), cx, |_, _| CardState {
            open: true,
            turn: Turn {
                from: SpringState {
                    position: 1.0,
                    velocity: 0.0,
                },
                target: 1.0,
                moved: now,
            },
            opened: 0,
            unfolded: HashSet::new(),
        });
        let (open, opened, turn) = {
            let card = state.read(cx);
            (card.open, card.opened, card.turn)
        };
        let spun = turn.at(now);
        let settled = reduced || HOVER.is_settled(spun, turn.target, TURN_SETTLED);
        if !settled {
            window.request_animation_frame();
        }
        let position = if settled { turn.target } else { spun.position };
        let hover = ink(&theme, HOVER_FILL);
        let toggle = state.clone();
        let header = div()
            .id("diff-head")
            .h(px(HEADER))
            .flex()
            .flex_none()
            .items_center()
            .gap_2()
            .pl(px(HEADER_PAD_LEFT))
            .pr(px(HEADER_PAD_LEFT))
            .cursor_pointer()
            .aria_label(if open { "Collapse diff" } else { "Expand diff" })
            .bg(tint(hover, 0.0))
            .transitions(|transitions| transitions.bg(Motion::new(HOVER_MS).with_easing(EASE_OUT)))
            .hover(move |head| head.bg(hover))
            .on_click(move |_, _, cx| {
                toggle.update(cx, |card, cx| {
                    card.open = !card.open;
                    card.opened = card.opened.wrapping_add(1);
                    card.turn
                        .toward(if card.open { 1.0 } else { 0.0 }, Instant::now());
                    cx.notify();
                })
            })
            .child(
                glyph(Glyph::Chevron, ICON_SMALL, ink(&theme, CAPTION_TEXT)).with_transformation(
                    Transformation::rotate(radians((position - 1.0) * FRAC_PI_2)),
                ),
            )
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .truncate()
                    .text_color(ink(&theme, T1))
                    .child(path_text(&self.diff, &theme)),
            )
            .children(change_tag(&self.diff.change, &theme))
            .child(counts(&self.diff, &theme));
        let card = div()
            .id(self.id)
            .flex()
            .flex_col()
            .overflow_hidden()
            .rounded(px(RADIUS_LIST))
            .bg(theme.color(ColorToken::ChatCalls))
            .text_size(px(FONT_SMALL))
            .font_weight(FontWeight::MEDIUM)
            .child(header);
        if !open {
            return card;
        }
        let lines = div()
            .flex()
            .flex_col()
            .py_1()
            .border_t(px(HAIRLINE))
            .border_color(theme.color(ColorToken::Separator))
            .font_family(mono(&theme))
            .font_weight(FontWeight::NORMAL)
            .text_size(px(FONT_BODY))
            .line_height(px(CODE_LINE))
            .child(
                ScrollArea::new("diff-scroll").max_h(self.max_h).children(
                    self.diff
                        .hunks
                        .iter()
                        .enumerate()
                        .map(|(ix, hunk)| hunk_block(ix, hunk, &state, cx, &theme)),
                ),
            );
        card.child(if opened == 0 {
            lines.into_any_element()
        } else {
            fade_in(lines, ("diff-body", opened))
        })
    }
}
