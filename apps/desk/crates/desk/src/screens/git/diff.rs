use std::f32::consts::PI;
use std::iter;

use desk_core::git::DiffOf;
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::chip::mono;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::paint::{glyph, ink};
use desk_ui::components::size::{
    CODE_LINE, DIFF_FOLD_LINES, DIM_TEXT, FONT_SMALL, FONT_TAB, HOVER, NUMBER_TEXT, RADIUS_BADGE,
    T1, T3,
};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::{HAIRLINE, ICON_SMALL};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{ClickEvent, Context, Div, Rgba, Transformation, Window, div, prelude::*, px, radians};

use super::confirm::{self, Discarded};
use super::detail::short;
use super::model::{Action, GitScreen, Loaded, Panel};

const SPLIT_FROM: f32 = 560.0;
const HEAD: f32 = 40.0;
const HUNK_HEAD: f32 = 30.0;
const HUNK_GAP: f32 = 8.0;
const PAD: f32 = 12.0;
const GAP: f32 = 8.0;
const BACK: f32 = 26.0;
const NUMBER: f32 = 40.0;
const NUMBER_PAD: f32 = 8.0;
const SIGN: f32 = 14.0;
const HUNK_FILL: f32 = 0.03;
const EMPTY_FILL: f32 = 0.02;

#[derive(Clone, Copy, PartialEq, Eq)]
enum Kind {
    Context,
    Added,
    Removed,
}

struct Line<'a> {
    kind: Kind,
    old: u32,
    new: u32,
    text: &'a str,
}

struct Hunk<'a> {
    header: &'a str,
    raw: Vec<&'a str>,
    lines: Vec<Line<'a>>,
}

struct Patch<'a> {
    head: Vec<&'a str>,
    hunks: Vec<Hunk<'a>>,
}

type Pair<'p, 'a> = (Option<&'p Line<'a>>, Option<&'p Line<'a>>);

pub(super) fn render(
    screen: &GitScreen,
    path: &str,
    of: &DiffOf,
    back: &Panel,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> Div {
    let body = match screen.diff() {
        None | Some(Loaded::Waiting) => note("Reading the diff\u{2026}".to_owned(), theme),
        Some(Loaded::Failed(error)) => note(format!("The diff could not be read: {error}"), theme),
        Some(Loaded::Ready(text)) => {
            let patch = parse(text);
            match patch.hunks.is_empty() {
                true => note(
                    "No lines changed. The file is binary, or only its mode changed.".to_owned(),
                    theme,
                ),
                false => {
                    let split = screen.right > SPLIT_FROM;
                    hunks(screen, &patch, of, split, window, cx)
                }
            }
        }
    };
    div()
        .flex()
        .flex_col()
        .flex_1()
        .min_h_0()
        .child(head(path, of, back, theme, window, cx))
        .child(
            div().flex_1().min_h_0().child(
                div()
                    .id("git-diff")
                    .size_full()
                    .overflow_y_scroll()
                    .child(body),
            ),
        )
}

fn parse(text: &str) -> Patch<'_> {
    let mut patch = Patch {
        head: Vec::new(),
        hunks: Vec::new(),
    };
    let (mut old, mut new) = (0, 0);
    for piece in text.split_inclusive('\n') {
        let raw = piece.strip_suffix('\n').unwrap_or(piece);
        if let Some(rest) = raw.strip_prefix("@@ ") {
            (old, new) = starts(rest);
            patch.hunks.push(Hunk {
                header: raw,
                raw: Vec::new(),
                lines: Vec::new(),
            });
            continue;
        }
        let Some(hunk) = patch.hunks.last_mut() else {
            patch.head.push(raw);
            continue;
        };
        if raw.starts_with("diff --git ") {
            break;
        }
        hunk.raw.push(raw);
        let (kind, body) = match raw.split_at_checked(1) {
            Some((" ", body)) => (Kind::Context, body),
            Some(("+", body)) => (Kind::Added, body),
            Some(("-", body)) => (Kind::Removed, body),
            Some(_) | None => continue,
        };
        hunk.lines.push(Line {
            kind,
            old,
            new,
            text: body.strip_suffix('\r').unwrap_or(body),
        });
        if kind != Kind::Added {
            old = old.saturating_add(1);
        }
        if kind != Kind::Removed {
            new = new.saturating_add(1);
        }
    }
    patch
}

fn starts(rest: &str) -> (u32, u32) {
    let start = |part: Option<&str>, sign: char| {
        part.and_then(|part| part.strip_prefix(sign))
            .and_then(|part| part.split(',').next())
            .and_then(|start| start.parse().ok())
            .unwrap_or(0)
    };
    let mut parts = rest.split(' ');
    let old = start(parts.next(), '-');
    (old, start(parts.next(), '+'))
}

fn hunk_patch(head: &[&str], hunk: &Hunk) -> String {
    let mut patch = String::new();
    for line in head.iter().chain(iter::once(&hunk.header)).chain(&hunk.raw) {
        patch.push_str(line);
        patch.push('\n');
    }
    patch
}

fn note(text: String, theme: &Theme) -> Div {
    div()
        .p(px(PAD))
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, DIM_TEXT))
        .child(text)
}

fn head(
    path: &str,
    of: &DiffOf,
    back: &Panel,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> Div {
    let (dir, name) = path.rsplit_once('/').unwrap_or(("", path));
    let target = match back {
        Panel::Commit(_) => "Back to the commit",
        Panel::Changes | Panel::Diff { .. } => "Back to the changes",
    };
    let state = match of {
        DiffOf::Worktree => "Not staged".to_owned(),
        DiffOf::Index => "Staged".to_owned(),
        DiffOf::Commit(sha) => format!("In {}", short(sha)),
    };
    let back = back.clone();
    let hover = ink(theme, HOVER);
    let arrow = div()
        .id("git-diff-back")
        .aria_label(target)
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(BACK))
        .rounded(px(RADIUS_BADGE))
        .cursor_pointer()
        .hover(move |style| style.bg(hover))
        .on_click(cx.listener(move |screen, _: &ClickEvent, _, cx| screen.show(back.clone(), cx)))
        .child(
            glyph(Glyph::Chevron, ICON_SMALL, ink(theme, T1))
                .with_transformation(Transformation::rotate(radians(PI))),
        );
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(GAP))
        .h(px(HEAD))
        .px(px(GAP))
        .border_b(px(HAIRLINE))
        .border_color(theme.color(ColorToken::Separator))
        .text_size(px(FONT_TAB))
        .child(tooltip(
            "git-diff-back-tip",
            arrow,
            Edge::Frame,
            target,
            theme,
            window,
            cx,
        ))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .flex()
                .items_baseline()
                .gap(px(GAP))
                .child(
                    div()
                        .flex_none()
                        .text_color(ink(theme, T1))
                        .child(name.to_owned()),
                )
                .child(
                    div()
                        .min_w_0()
                        .truncate()
                        .text_size(px(FONT_SMALL))
                        .text_color(ink(theme, T3))
                        .child(dir.to_owned()),
                ),
        )
        .child(
            div()
                .flex_none()
                .text_size(px(FONT_SMALL))
                .text_color(ink(theme, T3))
                .child(state),
        )
}

fn hunks(
    screen: &GitScreen,
    patch: &Patch,
    of: &DiffOf,
    split: bool,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> Div {
    let shared = ActiveTheme::theme(cx);
    let theme = &*shared;
    let editable = matches!(of, DiffOf::Worktree) && !patch.head.is_empty();
    let mut room = DIFF_FOLD_LINES;
    let mut hidden = 0usize;
    let mut out = div().flex().flex_col().pb(px(PAD));
    for (ix, hunk) in patch.hunks.iter().enumerate() {
        let paired = split.then(|| pairs(&hunk.lines));
        let total = paired.as_ref().map_or(hunk.lines.len(), Vec::len);
        let shown: Vec<Div> = match paired {
            Some(paired) => paired
                .into_iter()
                .take(room)
                .map(|pair| split_row(pair, theme))
                .collect(),
            None => hunk
                .lines
                .iter()
                .take(room)
                .map(|line| unified_row(line, theme))
                .collect(),
        };
        room = room.saturating_sub(shown.len());
        hidden = hidden.saturating_add(total.saturating_sub(shown.len()));
        if shown.is_empty() {
            continue;
        }
        let patch_text = editable.then(|| hunk_patch(&patch.head, hunk));
        out = out
            .child(hunk_head(ix, hunk, patch_text, screen, window, cx))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .font_family(mono(theme))
                    .text_size(px(FONT_SMALL))
                    .line_height(px(CODE_LINE))
                    .children(shown),
            );
    }
    out.when(hidden > 0, |out| {
        out.child(
            div()
                .p(px(PAD))
                .text_size(px(FONT_SMALL))
                .text_color(ink(theme, DIM_TEXT))
                .child(format!(
                    "{hidden} more lines are not drawn. Open the file in the editor to read them."
                )),
        )
    })
}

fn hunk_head(
    ix: usize,
    hunk: &Hunk,
    patch: Option<String>,
    screen: &GitScreen,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> Div {
    let shared = ActiveTheme::theme(cx);
    let theme = &*shared;
    let row = div()
        .flex()
        .items_center()
        .gap(px(GAP))
        .h(px(HUNK_HEAD))
        .when(ix > 0, |row| row.mt(px(HUNK_GAP)))
        .px(px(PAD))
        .bg(ink(theme, HUNK_FILL))
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, T3))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .truncate()
                .font_family(mono(theme))
                .child(hunk.header.to_owned()),
        );
    let Some(patch) = patch else {
        return row;
    };
    let changed = hunk
        .lines
        .iter()
        .filter(|line| line.kind != Kind::Context)
        .count();
    let lines = match changed {
        1 => "this line".to_owned(),
        count => format!("these {count} lines"),
    };
    let staged = patch.clone();
    let stage = button(
        ("git-stage-hunk", ix),
        "Stage hunk",
        None,
        ButtonKind::Text,
        theme,
    )
    .on_click(cx.listener(move |screen, _: &ClickEvent, _, cx| {
        screen.run(Action::StagePatch(staged.clone()), cx)
    }));
    let discard = button(
        ("git-discard-hunk", ix),
        "Discard hunk",
        None,
        ButtonKind::Text,
        theme,
    )
    .text_color(theme.color(ColorToken::StatusDanger));
    let discard = tooltip(
        ("git-discard-hunk-tip", ix),
        discard,
        Edge::Frame,
        format!("Discard hunk: throw {lines} away permanently"),
        theme,
        window,
        cx,
    );
    row.child(tooltip(
        ("git-stage-hunk-tip", ix),
        stage,
        Edge::Frame,
        format!("Stage hunk: add {lines} to the next commit"),
        theme,
        window,
        cx,
    ))
    .child(confirm::discard(
        ("git-discard-hunk-confirm", ix),
        discard,
        Discarded::Hunk(patch),
        screen,
        window,
        cx,
    ))
}

fn look(kind: Kind, theme: &Theme) -> (Option<Rgba>, &'static str, Rgba) {
    match kind {
        Kind::Context => (None, " ", ink(theme, NUMBER_TEXT)),
        Kind::Added => (
            Some(theme.color(ColorToken::DiffAddedLine)),
            "+",
            theme.color(ColorToken::GitAdded),
        ),
        Kind::Removed => (
            Some(theme.color(ColorToken::DiffRemovedLine)),
            "-",
            theme.color(ColorToken::GitDeleted),
        ),
    }
}

fn cells(line: &Line, number: u32, theme: &Theme) -> Div {
    let (fill, sign, sign_color) = look(line.kind, theme);
    div()
        .flex_1()
        .min_w_0()
        .flex()
        .h(px(CODE_LINE))
        .when_some(fill, |row, fill| row.bg(fill))
        .child(
            div()
                .w(px(NUMBER))
                .flex_none()
                .pr(px(NUMBER_PAD))
                .text_right()
                .text_color(ink(theme, NUMBER_TEXT))
                .child(number.to_string()),
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
                .text_color(ink(theme, T1))
                .child(line.text.to_owned()),
        )
}

fn unified_row(line: &Line, theme: &Theme) -> Div {
    let number = match line.kind {
        Kind::Removed => line.old,
        Kind::Context | Kind::Added => line.new,
    };
    div().flex().child(cells(line, number, theme))
}

fn split_row((left, right): Pair, theme: &Theme) -> Div {
    let empty = || {
        div()
            .flex_1()
            .min_w_0()
            .h(px(CODE_LINE))
            .bg(ink(theme, EMPTY_FILL))
    };
    div()
        .flex()
        .child(left.map_or_else(empty, |line| cells(line, line.old, theme)))
        .child(
            div()
                .w(px(HAIRLINE))
                .flex_none()
                .bg(theme.color(ColorToken::Separator)),
        )
        .child(right.map_or_else(empty, |line| cells(line, line.new, theme)))
}

fn pairs<'p, 'a>(lines: &'p [Line<'a>]) -> Vec<Pair<'p, 'a>> {
    let mut out = Vec::new();
    let mut at = 0;
    while let Some(line) = lines.get(at) {
        if line.kind == Kind::Context {
            out.push((Some(line), Some(line)));
            at += 1;
            continue;
        }
        let run = |from: usize, kind: Kind| -> Vec<&'p Line<'a>> {
            lines
                .get(from..)
                .unwrap_or_default()
                .iter()
                .take_while(|line| line.kind == kind)
                .collect()
        };
        let removed = run(at, Kind::Removed);
        let added = run(at + removed.len(), Kind::Added);
        let longest = removed.len().max(added.len());
        out.extend((0..longest).map(|ix| (removed.get(ix).copied(), added.get(ix).copied())));
        at += removed.len() + added.len();
    }
    out
}
