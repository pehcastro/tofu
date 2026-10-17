use std::mem;
use std::ops::Range;
use std::time::{Instant, SystemTime, UNIX_EPOCH};

use desk_core::git::{Commit, Ref, RefKind};
use desk_ui::components::chip::mono;
use desk_ui::components::commit_graph::{
    LANE_WIDTH, LaneRow, LaneState, PillKind, REF_BAND, ROW_HEIGHT, graph_cell, lay_out_from,
    ref_pill,
};
use desk_ui::components::paint::ink;
use desk_ui::components::scroll::scrollbar;
use desk_ui::components::size::{FONT_BODY, FONT_SMALL, RADIUS_ROW, T2, T3};
use desk_ui::components::skeleton::{pulse, skeleton_bar};
use desk_ui::components::tooltip::{Edge, hover_card};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, App, ClickEvent, Context, Div, SharedString, Window, div, prelude::*, px,
    uniform_list,
};

use super::model::{GitScreen, Loaded, Panel};
use super::sync::short;

const PAGE: usize = 200;
const AHEAD: usize = 60;
const SKELETON_ROWS: usize = 14;
const PILLS_SHOWN: usize = 2;
const MAX_LANES: u16 = 12;
const SHA_WIDTH: f32 = 64.0;
const TIME_WIDTH: f32 = 40.0;
const ROW_PAD: f32 = 10.0;
const GAP: f32 = 12.0;
const SECONDS: [(i64, &str); 5] = [
    (365 * 24 * 3600, "y"),
    (30 * 24 * 3600, "mo"),
    (7 * 24 * 3600, "w"),
    (24 * 3600, "d"),
    (3600, "h"),
];
const MINUTE: i64 = 60;

#[derive(Default)]
pub struct History {
    commits: Vec<Commit>,
    lanes: Vec<LaneRow>,
    state: LaneState,
    span: u16,
    more: bool,
    paging: bool,
    filled: bool,
    generation: u64,
}

type Laid = (Vec<Commit>, Vec<LaneRow>, LaneState);

fn lay(commits: Vec<Commit>, mut state: LaneState) -> Laid {
    let rows: Vec<(String, Vec<String>)> = commits
        .iter()
        .map(|commit| (commit.sha.clone(), commit.parents.clone()))
        .collect();
    let lanes = lay_out_from(&mut state, &rows);
    (commits, lanes, state)
}

fn span(lanes: &[LaneRow]) -> u16 {
    lanes
        .iter()
        .flat_map(|row| {
            row.lanes_through
                .iter()
                .map(|&(lane, _)| lane)
                .chain(
                    row.edges
                        .iter()
                        .flat_map(|edge| [edge.from_lane, edge.to_lane]),
                )
                .chain([row.node_lane])
        })
        .max()
        .map_or(1, |widest| widest.saturating_add(1))
        .min(MAX_LANES)
}

impl History {
    fn keep(&mut self, (commits, lanes, state): Laid, asked: usize, appended: bool) {
        if !appended {
            self.commits.clear();
            self.lanes.clear();
            self.span = 0;
        }
        self.more = commits.len() == asked;
        self.span = self.span.max(span(&lanes));
        self.commits.extend(commits);
        self.lanes.extend(lanes);
        self.state = state;
    }
}

impl GitScreen {
    pub(super) fn reload_history(&mut self, cx: &mut Context<Self>) {
        let asked = self.history.commits.len().max(PAGE);
        self.history.generation = self.history.generation.wrapping_add(1);
        self.history.paging = true;
        let (generation, started) = (self.history.generation, Instant::now());
        self.fetch(
            move |git| Ok(lay(git.log(0, asked)?, LaneState::default())),
            move |screen, read, _| {
                if screen.history.generation != generation {
                    return;
                }
                screen.history.paging = false;
                match read {
                    Loaded::Ready(laid) => {
                        if !screen.history.filled {
                            eprintln!(
                                "desk: git: history first {} rows {} ms after open, {} ms to read",
                                laid.0.len(),
                                screen.opened.elapsed().as_millis(),
                                started.elapsed().as_millis()
                            );
                        }
                        screen.history.filled = true;
                        screen.history.keep(laid, asked, false);
                    }
                    Loaded::Failed(error) => {
                        eprintln!("desk: git: history was not read: {error}");
                        screen.history.filled = true;
                    }
                    Loaded::Waiting => {}
                }
            },
            cx,
        );
    }

    fn next_page(&mut self, cx: &mut Context<Self>) {
        let history = &mut self.history;
        if history.paging || !history.more {
            return;
        }
        history.paging = true;
        let (skip, generation) = (history.commits.len(), history.generation);
        let state = mem::take(&mut history.state);
        self.fetch(
            move |git| Ok(lay(git.log(skip, PAGE)?, state)),
            move |screen, read, _| {
                if screen.history.generation != generation {
                    return;
                }
                screen.history.paging = false;
                match read {
                    Loaded::Ready(laid) => screen.history.keep(laid, PAGE, true),
                    Loaded::Failed(error) => {
                        screen.history.more = false;
                        eprintln!("desk: git: the next history page was not read: {error}");
                    }
                    Loaded::Waiting => {}
                }
            },
            cx,
        );
    }
}

fn ago(when: i64) -> String {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .ok()
        .and_then(|since| i64::try_from(since.as_secs()).ok())
        .unwrap_or(when);
    let past = now.saturating_sub(when).max(0);
    SECONDS.iter().find(|(unit, _)| past >= *unit).map_or_else(
        || format!("{}m", past / MINUTE),
        |(unit, name)| format!("{}{name}", past / unit),
    )
}

fn initials(author: &str) -> SharedString {
    author
        .split_whitespace()
        .filter_map(|word| word.chars().next())
        .take(2)
        .flat_map(char::to_uppercase)
        .collect::<String>()
        .into()
}

fn pills(screen: &GitScreen, commit: &Commit) -> Vec<(SharedString, PillKind)> {
    let branches = &screen.snapshot().branches;
    let merged = |remote: &str| {
        branches.iter().any(|branch| {
            branch.upstream.as_deref() == Some(remote)
                && commit
                    .refs
                    .iter()
                    .any(|other| other.kind == RefKind::Local && other.name == branch.name)
        })
    };
    let head = commit.refs.iter().any(|one| one.kind == RefKind::Head);
    let current = screen.snapshot().branch.as_ref().map(|branch| &branch.name);
    commit
        .refs
        .iter()
        .filter_map(|Ref { name, kind }| {
            let upstream = || {
                branches
                    .iter()
                    .find(|branch| &branch.name == name)
                    .and_then(|branch| branch.upstream.as_deref())
                    .is_some_and(|remote| commit.refs.iter().any(|other| other.name == remote))
            };
            let kind = match kind {
                RefKind::Head => return None,
                RefKind::Local if head && current == Some(name) => PillKind::Head,
                RefKind::Local if upstream() => PillKind::LocalWithUpstream,
                RefKind::Local => PillKind::Local,
                RefKind::Remote if merged(name) => return None,
                RefKind::Remote => PillKind::Remote,
                RefKind::Tag => PillKind::Tag,
                RefKind::Stash => PillKind::Stash,
                RefKind::Detached => PillKind::Detached,
            };
            Some((SharedString::from(name.clone()), kind))
        })
        .collect()
}

fn band(
    screen: &GitScreen,
    at: usize,
    commit: &Commit,
    lane: u8,
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> Div {
    let all = pills(screen, commit);
    let shown = all
        .iter()
        .take(PILLS_SHOWN)
        .map(|(name, kind)| ref_pill(name.clone(), *kind, lane, theme));
    let hidden = all.len().saturating_sub(PILLS_SHOWN);
    let more = (hidden > 0).then(|| {
        let names = div().flex().flex_col().gap_1().children(
            all.iter()
                .skip(PILLS_SHOWN)
                .map(|(name, kind)| ref_pill(name.clone(), *kind, lane, theme)),
        );
        let count = div()
            .id(("git-more-refs", at))
            .flex_none()
            .px_1p5()
            .rounded(px(RADIUS_ROW))
            .text_size(px(FONT_SMALL))
            .text_color(ink(theme, T2))
            .child(format!("+{hidden}"));
        hover_card(
            ("git-more-refs-card", at),
            count,
            Edge::Frame,
            names,
            theme,
            window,
            cx,
        )
    });
    div()
        .flex()
        .flex_none()
        .w(px(REF_BAND))
        .justify_end()
        .items_center()
        .gap_1()
        .overflow_hidden()
        .children(shown)
        .children(more)
}

fn row_base(id: impl Into<gpui::ElementId>, picked: bool, theme: &Theme) -> gpui::Stateful<Div> {
    let hover = theme.color(ColorToken::StateActive);
    div()
        .id(id)
        .flex()
        .items_center()
        .w_full()
        .overflow_hidden()
        .gap(px(GAP))
        .h(px(ROW_HEIGHT))
        .px(px(ROW_PAD))
        .cursor_pointer()
        .text_size(px(FONT_BODY))
        .when(picked, |row| row.bg(hover))
        .hover(move |row| row.bg(hover))
}

fn graph_width(span: u16) -> f32 {
    f32::from(span) * LANE_WIDTH
}

fn uncommitted(screen: &GitScreen, theme: &Theme, cx: &mut Context<GitScreen>) -> AnyElement {
    let count = screen.snapshot().changes.len();
    let picked = matches!(screen.panel(), Panel::Changes);
    let files = match count {
        1 => "1 file".to_owned(),
        count => format!("{count} files"),
    };
    row_base("git-uncommitted", picked, theme)
        .on_click(cx.listener(|screen, _: &ClickEvent, _, cx| screen.show(Panel::Changes, cx)))
        .child(div().flex_none().w(px(REF_BAND)))
        .child(div().flex_none().w(px(graph_width(screen.history.span))))
        .child(
            div()
                .flex()
                .flex_1()
                .min_w_0()
                .gap_2()
                .italic()
                .child("Uncommitted changes")
                .child(div().text_color(ink(theme, T3)).child(format!("({files})"))),
        )
        .into_any_element()
}

fn commit_row(
    screen: &GitScreen,
    at: usize,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> Option<AnyElement> {
    let commit = screen.history.commits.get(at)?;
    let lanes = screen.history.lanes.get(at)?;
    let head = commit.refs.iter().any(|one| one.kind == RefKind::Head);
    let picked = matches!(screen.panel(), Panel::Commit(sha) if *sha == commit.sha);
    let lane = lanes.node_color();
    let sha = commit.sha.clone();
    Some(
        row_base(("git-commit", at), picked, theme)
            .on_click(cx.listener(move |screen, _: &ClickEvent, _, cx| {
                screen.show(Panel::Commit(sha.clone()), cx)
            }))
            .child(band(screen, at, commit, lane, theme, window, cx))
            .child(
                div()
                    .flex_none()
                    .w(px(graph_width(screen.history.span)))
                    .overflow_hidden()
                    .child(graph_cell(lanes, initials(&commit.author), head, theme)),
            )
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .truncate()
                    .child(commit.subject.clone()),
            )
            .child(
                div()
                    .flex_none()
                    .w(px(SHA_WIDTH))
                    .font_family(mono(theme))
                    .text_size(px(FONT_SMALL))
                    .text_color(ink(theme, T2))
                    .child(short(&commit.sha).to_owned()),
            )
            .child(
                div()
                    .flex_none()
                    .w(px(TIME_WIDTH))
                    .text_right()
                    .text_size(px(FONT_SMALL))
                    .text_color(ink(theme, T3))
                    .child(ago(commit.when)),
            )
            .into_any_element(),
    )
}

fn skeleton(theme: &Theme, cx: &App) -> AnyElement {
    div()
        .flex()
        .flex_col()
        .children((0..SKELETON_ROWS).map(|at| {
            div()
                .flex()
                .items_center()
                .gap(px(GAP))
                .h(px(ROW_HEIGHT))
                .px(px(ROW_PAD))
                .child(div().flex_none().w(px(REF_BAND)))
                .child(pulse(
                    "git-skeleton-dot",
                    at,
                    skeleton_bar(0.03, 20.0, theme),
                    cx,
                ))
                .child(pulse(
                    "git-skeleton-line",
                    at,
                    skeleton_bar(0.5, 10.0, theme),
                    cx,
                ))
        }))
        .into_any_element()
}

fn rows(
    screen: &mut GitScreen,
    range: Range<usize>,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> Vec<AnyElement> {
    let dirty = usize::from(!screen.snapshot().changes.is_empty());
    if range.end.saturating_add(AHEAD) >= screen.history.commits.len().saturating_add(dirty) {
        screen.next_page(cx);
    }
    let theme = ActiveTheme::theme(cx);
    range
        .filter_map(|at| match at.checked_sub(dirty) {
            None => Some(uncommitted(screen, &theme, cx)),
            Some(at) => commit_row(screen, at, &theme, window, cx),
        })
        .collect()
}

pub fn render(screen: &GitScreen, theme: &Theme, cx: &mut Context<GitScreen>) -> AnyElement {
    if !screen.history.filled {
        return skeleton(theme, cx);
    }
    let dirty = usize::from(!screen.snapshot().changes.is_empty());
    let count = screen.history.commits.len().saturating_add(dirty);
    let base = screen.list.0.borrow().base_handle.clone();
    div()
        .relative()
        .size_full()
        .overflow_hidden()
        .child(
            uniform_list("git-history", count, cx.processor(rows))
                .track_scroll(&screen.list)
                .size_full(),
        )
        .child(scrollbar("git-history-bar", &base))
        .into_any_element()
}
