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
use desk_ui::components::tooltip::{Edge, hover_card, tooltip};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, App, ClickEvent, Context, Div, Entity, Render, ScrollWheelEvent, SharedString,
    StyleRefinement, Window, div, prelude::*, px, uniform_list,
};

use super::model::{GitScreen, Loaded, Panel, Shown};
use super::sync::short;

const PAGE: usize = 200;
const AHEAD: usize = 60;
const SKELETON_ROWS: usize = 14;
const PILLS_SHOWN: usize = 2;
const MAX_LANES: u16 = 8;
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

pub struct History {
    commits: Vec<Commit>,
    authors: Vec<SharedString>,
    lanes: Vec<LaneRow>,
    state: LaneState,
    hidden: u16,
    shift: u16,
    drift: f32,
    more: bool,
    paging: bool,
    filled: bool,
    generation: u64,
    pointer: Entity<Pointer>,
}

#[derive(Clone, Copy)]
struct Spot {
    at: usize,
    top: f32,
}

#[derive(Default)]
struct Pointer {
    lit: Option<Spot>,
    tip: Option<(Spot, SharedString)>,
}

type Laid = (Vec<Commit>, Vec<SharedString>, Vec<LaneRow>, LaneState);

fn lay(commits: Vec<Commit>, mut state: LaneState) -> Laid {
    let rows: Vec<(String, Vec<String>)> = commits
        .iter()
        .map(|commit| (commit.sha.clone(), commit.parents.clone()))
        .collect();
    let lanes = lay_out_from(&mut state, &rows);
    let authors = commits
        .iter()
        .map(|commit| format!("{} <{}>", commit.author, commit.email).into())
        .collect();
    (commits, authors, lanes, state)
}

impl History {
    pub(super) fn new(cx: &mut Context<GitScreen>) -> History {
        History {
            commits: Vec::new(),
            authors: Vec::new(),
            lanes: Vec::new(),
            state: LaneState::default(),
            hidden: 0,
            shift: 0,
            drift: 0.0,
            more: false,
            paging: false,
            filled: false,
            generation: 0,
            pointer: cx.new(|_| Pointer::default()),
        }
    }

    fn keep(&mut self, (commits, authors, lanes, state): Laid, asked: usize, appended: bool) {
        if !appended {
            self.commits.clear();
            self.authors.clear();
            self.lanes.clear();
        }
        self.more = commits.len() == asked;
        self.commits.extend(commits);
        self.authors.extend(authors);
        self.lanes.extend(lanes);
        self.state = state;
    }

    fn frame(&mut self, rows: impl Iterator<Item = usize>) -> (u16, u16) {
        let wide = rows
            .filter_map(|at| self.lanes.get(at))
            .map(LaneRow::span)
            .max()
            .unwrap_or(1)
            .max(1);
        let shown = wide.min(MAX_LANES);
        self.hidden = wide.saturating_sub(shown);
        self.shift = self.shift.min(self.hidden);
        (shown, self.shift)
    }

    fn slide(&mut self, by: f32) -> bool {
        let before = self.shift;
        self.drift += by;
        while self.drift >= LANE_WIDTH && self.shift < self.hidden {
            self.drift -= LANE_WIDTH;
            self.shift += 1;
        }
        while self.drift <= -LANE_WIDTH && self.shift > 0 {
            self.drift += LANE_WIDTH;
            self.shift -= 1;
        }
        if (self.shift == self.hidden && self.drift > 0.0) || (self.shift == 0 && self.drift < 0.0)
        {
            self.drift = 0.0;
        }
        self.shift != before
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
                    return Shown::Same;
                }
                screen.history.paging = false;
                match read {
                    Loaded::Ready(laid)
                        if screen.history.filled && laid.0 == screen.history.commits =>
                    {
                        screen.history.keep(laid, asked, false);
                        Shown::Same
                    }
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
                        Shown::Changed
                    }
                    Loaded::Failed(error) => {
                        eprintln!("desk: git: history was not read: {error}");
                        screen.history.filled = true;
                        Shown::Changed
                    }
                    Loaded::Waiting => Shown::Same,
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
                    return Shown::Same;
                }
                screen.history.paging = false;
                match read {
                    Loaded::Ready(laid) => {
                        screen.history.keep(laid, PAGE, true);
                        Shown::Changed
                    }
                    Loaded::Failed(error) => {
                        screen.history.more = false;
                        eprintln!("desk: git: the next history page was not read: {error}");
                        Shown::Same
                    }
                    Loaded::Waiting => Shown::Same,
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

fn row_base(
    id: impl Into<gpui::ElementId>,
    screen: &GitScreen,
    at: usize,
    author: Option<SharedString>,
    picked: bool,
    theme: &Theme,
) -> gpui::Stateful<Div> {
    let (pointer, list) = (screen.history.pointer.clone(), screen.list.clone());
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
        .when(picked, |row| row.bg(theme.color(ColorToken::StateActive)))
        .on_hover(move |inside, _, cx| {
            let scrolled = list.0.borrow().base_handle.offset().y.as_f32();
            let spot = Spot {
                at,
                top: at as f32 * ROW_HEIGHT + scrolled,
            };
            pointer.update(cx, |pointer, cx| {
                pointer.hover(spot, author.clone(), *inside, cx)
            });
        })
}

impl Pointer {
    fn hover(
        &mut self,
        spot: Spot,
        author: Option<SharedString>,
        inside: bool,
        cx: &mut Context<Self>,
    ) {
        match inside {
            true => {
                self.lit = Some(spot);
                if let Some(author) = author {
                    self.tip = Some((spot, author));
                }
            }
            false if self.lit.is_some_and(|lit| lit.at == spot.at) => self.lit = None,
            false => return,
        }
        cx.notify();
    }
}

impl Render for Pointer {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        desk_perf::span!("git-hover");
        let theme = ActiveTheme::theme(cx);
        let lit = self.lit.map(|spot| {
            div()
                .absolute()
                .left_0()
                .top(px(spot.top))
                .w_full()
                .h(px(ROW_HEIGHT))
                .bg(theme.color(ColorToken::StateActive))
        });
        let tip = self.tip.clone().map(|(spot, author)| {
            let target = div()
                .id("git-hover")
                .top(px(spot.top))
                .w_full()
                .h(px(ROW_HEIGHT));
            tooltip(
                "git-commit-author",
                target,
                Edge::FrameStart,
                author,
                &theme,
                window,
                cx,
            )
        });
        div()
            .size_full()
            .overflow_hidden()
            .children(lit)
            .children(tip)
    }
}

fn graph_width(span: u16) -> f32 {
    f32::from(span) * LANE_WIDTH
}

fn uncommitted(
    screen: &GitScreen,
    lanes: u16,
    theme: &Theme,
    cx: &mut Context<GitScreen>,
) -> AnyElement {
    let count = screen.snapshot().changes.len();
    let picked = matches!(screen.panel(), Panel::Changes);
    let files = match count {
        1 => "1 file".to_owned(),
        count => format!("{count} files"),
    };
    row_base("git-uncommitted", screen, 0, None, picked, theme)
        .on_click(cx.listener(|screen, _: &ClickEvent, _, cx| screen.show(Panel::Changes, cx)))
        .child(div().flex_none().w(px(REF_BAND)))
        .child(div().flex_none().w(px(graph_width(lanes))))
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
    (ix, at): (usize, usize),
    graph: (u16, u16),
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
    let slides = screen.history.hidden > 0;
    let author = screen.history.authors.get(at).cloned();
    let row = row_base(("git-commit", at), screen, ix, author, picked, theme)
        .on_click(cx.listener(move |screen, _: &ClickEvent, _, cx| {
            screen.show(Panel::Commit(sha.clone()), cx)
        }))
        .child(band(screen, at, commit, lane, theme, window, cx))
        .child(
            div()
                .flex_none()
                .when(slides, |column| {
                    column.on_scroll_wheel(cx.listener(slide_graph))
                })
                .child(graph_cell(lanes, head, graph, theme)),
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
        );
    Some(row.into_any_element())
}

fn slide_graph(
    screen: &mut GitScreen,
    event: &ScrollWheelEvent,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) {
    let delta = event.delta.pixel_delta(window.line_height());
    let across = match event.modifiers.shift && delta.x == px(0.0) {
        true => delta.y,
        false => delta.x,
    };
    if across == px(0.0) {
        return;
    }
    cx.stop_propagation();
    if screen.history.slide(-f32::from(across)) {
        cx.notify();
    }
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
    desk_perf::span!("git-rows");
    let dirty = usize::from(!screen.snapshot().changes.is_empty());
    if range.end.saturating_add(AHEAD) >= screen.history.commits.len().saturating_add(dirty) {
        screen.next_page(cx);
    }
    let theme = ActiveTheme::theme(cx);
    let graph = screen
        .history
        .frame(range.clone().filter_map(|at| at.checked_sub(dirty)));
    range
        .filter_map(|ix| match ix.checked_sub(dirty) {
            None => Some(uncommitted(screen, graph.0, &theme, cx)),
            Some(at) => commit_row(screen, (ix, at), graph, &theme, window, cx),
        })
        .collect()
}

pub fn pointer(screen: &GitScreen) -> AnyElement {
    screen
        .history
        .pointer
        .clone()
        .cached(StyleRefinement::default().absolute().inset_0())
        .into_any_element()
}

pub fn list(screen: &GitScreen, cx: &mut Context<GitScreen>) -> AnyElement {
    let theme = ActiveTheme::theme(cx);
    if !screen.history.filled {
        return skeleton(&theme, cx);
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
