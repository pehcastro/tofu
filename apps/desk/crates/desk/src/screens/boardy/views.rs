use std::collections::{BTreeMap, BTreeSet};
use std::f32::consts::FRAC_PI_2;
use std::ops::Range;
use std::rc::Rc;
use std::time::Instant;

use chrono::{DateTime, Utc};
use desk_core::boardy::{BoardyStore, COLUMNS, running, spoken};
use desk_core::protocol::BoardyStatus as Status;
use desk_core::protocol::boardy::{AskedQuestion, Event, PlanAnswer, PlannedEpic, Row};
use desk_core::protocol::{BoardyEventKind, BoardyPlannedEpicStatus};
use desk_ui::component::{control, icon_button};
use desk_ui::components::avatar::spinner;
use desk_ui::components::board::{ColumnHeader, Moment, board_card, board_rows};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::caption;
use desk_ui::components::chip::tabular;
use desk_ui::components::dot_chip::{dot_chip, icon_chip};
use desk_ui::components::empty::empty_state;
use desk_ui::components::flip::flip;
use desk_ui::components::form::TextArea;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::issue_row::{group_header, issue_row};
use desk_ui::components::paint::{glyph, ink};
use desk_ui::components::scroll::{ScrollArea, scrollbar};
use desk_ui::components::size::{CAPTION_TEXT, FONT_SMALL, HOVER, RADIUS_ROW, T1, T2};
use desk_ui::components::status_icon::RunState;
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::icon::Icon;
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::{CONTROL, ICON_TINY, RADIUS_CONTROL};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, App, Axis, ClickEvent, Context, Div, ElementId, FontWeight, MouseButton,
    MouseDownEvent, Rgba, SharedString, Stateful, Transformation, WeakEntity, Window, canvas, div,
    prelude::*, px, radians, relative, rgb_to_hsla, uniform_list,
};

use super::look::{self, Lane, age, ago, day, kit, noun, past, span, stamp};
use super::{Boardy, Layout, MS_PER_SECOND, Place, SCROLL_WINDOW_MS, Section};
use crate::screens::frame::note;

const LINE: f32 = 44.0;
const PAD_X: f32 = 16.0;
const LIST_INSET: f32 = 4.0;
const BAR: f32 = 44.0;
const TOOLS_GAP: f32 = 6.0;
const MARK: f32 = 12.0;
const MARK_STROKE: f32 = 1.5;
const MARK_GAP: f32 = 2.5;
const LANE: f32 = 3.0;
const LANES: [f32; 3] = [12.0, 9.0, 6.0];
const COLUMN_GAP: f32 = 10.0;
const TOLD: usize = 4;
const GIST_CHARS: usize = 200;
const WAITING_REVIEW: &str = "Waiting for review";
const APPROVED: &str = "approved from the desk";
const REJECTED: &str = "rejected from the desk, do not go ahead";
const RECOMMENDED: &str = "Approve picks";
pub(super) const MILESTONES: &str = "Milestones";
const NO_MILESTONE: &str = "No milestone";
const NOT_PLANNED: &str = "Not in the plan";
const UNPLANNED: &str = "Unplanned";
const ACTIVE: &str = "Active";
const PLANNED: &str = "Planned";
const CLOSED: &str = "Closed";
const SPRINT_PHASES: [&str; 3] = [ACTIVE, PLANNED, CLOSED];
const EPIC_KIND: &str = "epic";
const SPRINT_KIND: &str = "sprint";
const MILESTONE_KIND: &str = "milestone";
const FOLD_INDENT: f32 = 36.0;
const PROGRESS_WIDTH: f32 = 48.0;
const PROGRESS_HEIGHT: f32 = 4.0;
const PROGRESS_TRACK: f32 = 0.08;
const TALLY_WIDTH: f32 = 44.0;
const STALE_REVIEW_DAYS: i64 = 1;
const DIGEST_ROWS: usize = 8;

#[derive(Clone, Copy, PartialEq, Eq)]
enum Need {
    Decide,
    Blocked,
    Stale,
}

impl Need {
    const ALL: [Need; 3] = [Need::Decide, Need::Blocked, Need::Stale];

    fn label(self) -> &'static str {
        match self {
            Need::Decide => "Decide",
            Need::Blocked => "Blocked",
            Need::Stale => "In review over a day",
        }
    }
}

#[derive(Clone, Copy)]
pub(super) struct Spot {
    board: usize,
    index: usize,
}

pub(super) struct Group<'a> {
    pub status: Option<Status>,
    pub title: String,
    pub tickets: Vec<&'a Row>,
    spots: Vec<Spot>,
    pub folded: bool,
    fold: Option<Fold>,
}

pub(super) struct Fold {
    key: String,
    glyph: Glyph,
    chip: Option<String>,
    note: Option<String>,
    phase: Option<&'static str>,
    done: usize,
    total: usize,
}

struct Planned {
    id: String,
    title: String,
    note: Option<String>,
    phase: Option<&'static str>,
    closed: bool,
}

pub(super) enum Line {
    Phase(&'static str),
    Empty(String),
    Head {
        status: Option<Status>,
        title: String,
        count: usize,
        folded: bool,
        fold: Option<Fold>,
    },
    Row {
        spot: Spot,
        at: usize,
    },
}

#[derive(Default)]
pub(super) struct Tail {
    told: Vec<(String, SharedString)>,
    doing: Option<String>,
    moved: Option<String>,
}

impl Tail {
    fn since(&self) -> Option<&str> {
        self.doing.as_deref().or(self.moved.as_deref())
    }

    fn took(&self) -> Option<String> {
        Some(span(
            stamp(self.doing.as_deref()?)?,
            stamp(self.moved.as_deref()?)?,
        ))
    }
}

pub(super) fn heard(tails: &mut BTreeMap<String, Tail>, events: &[Event]) {
    for event in events {
        let tail = tails.entry(event.ticket.clone()).or_default();
        let moved = event.kind == BoardyEventKind::Moved;
        if moved {
            tail.moved = Some(event.at.clone());
        }
        let to = event.to.clone().map(|to| Status::from(String::from(to)));
        if to == Some(Status::Doing) {
            tail.doing = Some(event.at.clone());
        }
        let told = (moved || spoken(event))
            .then(|| event.text.as_deref().and_then(gist))
            .flatten();
        if let Some(text) = told {
            tail.told.push((event.at.clone(), text));
            if tail.told.len() > TOLD {
                tail.told.remove(0);
            }
        }
    }
}

fn gist(text: &str) -> Option<SharedString> {
    text.lines()
        .map(|line| line.trim().trim_matches(['#', '*', '-', '>', ' ']))
        .find(|line| !line.is_empty())
        .map(|line| line.chars().take(GIST_CHARS).collect::<String>().into())
}

struct Slot<'a> {
    spot: Spot,
    ticket: &'a Row,
    state: RunState,
    question: Option<&'a AskedQuestion>,
}

fn lanes<'a>(boardy: &'a Boardy, key: &str) -> Vec<(Lane, Vec<Slot<'a>>)> {
    let mut held = held(&boardy.store, Some(key));
    held.sort_by(|(_, a), (_, b)| b.updated.cmp(&a.updated));
    let mut lanes: Vec<(Lane, Vec<Slot<'a>>)> = Lane::ALL
        .into_iter()
        .map(|lane| (lane, Vec::new()))
        .collect();
    for (spot, ticket) in held {
        let question = boardy
            .questions
            .iter()
            .find(|question| question.ticket == ticket.id);
        let Some((lane, state)) = look::inbox(&ticket.status, question.is_some()) else {
            continue;
        };
        if let Some((_, slots)) = lanes.iter_mut().find(|(held, _)| *held == lane) {
            slots.push(Slot {
                spot,
                ticket,
                state,
                question,
            });
        }
    }
    lanes
}

fn held<'a>(store: &'a BoardyStore, only: Option<&str>) -> Vec<(Spot, &'a Row)> {
    store
        .boards
        .iter()
        .enumerate()
        .filter(|(_, summary)| only.is_none_or(|key| summary.key == key))
        .flat_map(|(board, summary)| {
            store
                .tickets(&summary.key)
                .iter()
                .enumerate()
                .map(move |(index, ticket)| (Spot { board, index }, ticket))
        })
        .collect()
}

fn ticket_at(store: &BoardyStore, spot: Spot) -> Option<&Row> {
    let summary = store.boards.get(spot.board)?;
    store.tickets(&summary.key).get(spot.index)
}

fn by_status<'a>(boardy: &Boardy, held: &[(Spot, &'a Row)]) -> Vec<Group<'a>> {
    COLUMNS
        .iter()
        .map(|status| {
            let (spots, tickets) = held
                .iter()
                .filter(|(_, ticket)| ticket.status == *status)
                .copied()
                .unzip();
            Group {
                status: Some(status.clone()),
                title: look::label(status).to_owned(),
                tickets,
                spots,
                folded: boardy.layout == Layout::List && boardy.folded.contains(status),
                fold: None,
            }
        })
        .filter(|group| !group.tickets.is_empty())
        .collect()
}

fn in_review(boardy: &Boardy, ticket: &Row) -> Option<i64> {
    let (board, _) = ticket.id.split_once('-')?;
    let report = boardy.reports.get(board)?;
    let aged = report.review.iter().find(|aged| aged.id == ticket.id)?;
    Some(aged.days)
}

fn need(boardy: &Boardy, ticket: &Row) -> Option<Need> {
    let asked = ticket.questions.unwrap_or_default() > 0
        || boardy
            .questions
            .iter()
            .any(|question| question.ticket == ticket.id);
    match ticket.status {
        _ if asked => Some(Need::Decide),
        Status::NeedsDecision => Some(Need::Decide),
        Status::Blocked => Some(Need::Blocked),
        Status::Review => in_review(boardy, ticket)
            .filter(|days| *days >= STALE_REVIEW_DAYS)
            .map(|_| Need::Stale),
        _ => None,
    }
}

fn needed(boardy: &Boardy) -> Vec<(Need, Vec<(Spot, &Row)>)> {
    let mut needed: Vec<(Need, Vec<(Spot, &Row)>)> = Need::ALL
        .into_iter()
        .map(|want| (want, Vec::new()))
        .collect();
    for (spot, ticket) in held(&boardy.store, None) {
        let Some(want) = need(boardy, ticket) else {
            continue;
        };
        if let Some((_, rows)) = needed.iter_mut().find(|(held, _)| *held == want) {
            rows.push((spot, ticket));
        }
    }
    needed
}

fn deciding<'a>(boardy: &'a Boardy, rows: &[(Spot, &'a Row)]) -> Vec<&'a str> {
    let mut ids: Vec<&str> = rows.iter().map(|(_, ticket)| ticket.id.as_str()).collect();
    for question in &boardy.questions {
        if !ids.contains(&question.ticket.as_str()) {
            ids.push(&question.ticket);
        }
    }
    ids
}

pub(super) fn wanting(boardy: &Boardy) -> usize {
    needed(boardy)
        .iter()
        .map(|(want, rows)| match want {
            Need::Decide => deciding(boardy, rows).len(),
            Need::Blocked | Need::Stale => rows.len(),
        })
        .sum()
}

struct Plan {
    key: String,
    glyph: Glyph,
    planned: Vec<Planned>,
    name: fn(&Row) -> Option<&str>,
    none: String,
    loose: Option<&'static str>,
    stray: Option<&'static str>,
}

type Bucket<'a> = (Option<String>, Planned, Vec<Spot>, Vec<&'a Row>);

fn tally<'a>(tickets: impl Iterator<Item = &'a Row>) -> (usize, usize) {
    tickets
        .filter(|ticket| ticket.status != Status::Dropped)
        .fold((0, 0), |(done, total), ticket| {
            (done + usize::from(ticket.status == Status::Done), total + 1)
        })
}

fn by_plan<'a>(
    held: Vec<(Spot, &'a Row)>,
    plan: Plan,
    flipped: &BTreeSet<String>,
) -> Vec<Group<'a>> {
    let mut keyed: Vec<Bucket<'a>> = plan
        .planned
        .into_iter()
        .map(|planned| (Some(planned.id.clone()), planned, Vec::new(), Vec::new()))
        .collect();
    for (spot, ticket) in held {
        let key = (plan.name)(ticket).map(str::to_owned);
        let known = keyed
            .iter_mut()
            .find(|(id, planned, ..)| *id == key || key.as_deref() == Some(planned.title.as_str()));
        match known {
            Some((_, _, spots, tickets)) => {
                spots.push(spot);
                tickets.push(ticket);
            }
            None => {
                let title = match (&key, plan.stray) {
                    (None, _) => plan.none.clone(),
                    (Some(id), Some(_)) => id.clone(),
                    (Some(_), None) => NOT_PLANNED.to_owned(),
                };
                let planned = Planned {
                    id: key.clone().unwrap_or_default(),
                    title,
                    note: None,
                    phase: if key.is_some() {
                        plan.stray
                    } else {
                        plan.loose
                    },
                    closed: key.is_none(),
                };
                keyed.push((key, planned, vec![spot], vec![ticket]));
            }
        }
    }
    keyed.sort_by_key(|(key, planned, ..)| {
        let rank = SPRINT_PHASES
            .iter()
            .position(|phase| Some(*phase) == planned.phase);
        (key.is_none(), rank.unwrap_or(SPRINT_PHASES.len()))
    });
    let mut opened = false;
    keyed
        .into_iter()
        .map(|(chip, planned, spots, tickets)| {
            let (done, total) = tally(tickets.iter().copied());
            let open = !planned.closed && !opened && done < total;
            opened |= open;
            let key = format!("{}/{}", plan.key, planned.id);
            Group {
                status: None,
                title: planned.title,
                tickets,
                spots,
                folded: open == flipped.contains(&key),
                fold: Some(Fold {
                    key,
                    glyph: plan.glyph,
                    chip,
                    note: planned.note,
                    phase: planned.phase,
                    done,
                    total,
                }),
            }
        })
        .collect()
}

fn planned_epics(plan: Option<&PlanAnswer>) -> Vec<Planned> {
    plan.map_or_else(Vec::new, |plan| {
        plan.epics
            .iter()
            .map(|epic| Planned {
                id: epic.id.clone(),
                title: epic.title.clone(),
                note: None,
                phase: None,
                closed: epic.status == BoardyPlannedEpicStatus::Done,
            })
            .collect()
    })
}

fn planned_sprints(plan: Option<&PlanAnswer>) -> Vec<Planned> {
    let now = Utc::now();
    plan.map_or_else(Vec::new, |plan| {
        plan.sprints
            .iter()
            .map(|sprint| {
                let closed =
                    !sprint.active && sprint.end.as_deref().is_some_and(|end| past(end, now));
                let phase = match (sprint.active, closed) {
                    (true, _) => ACTIVE,
                    (false, false) => PLANNED,
                    (false, true) => CLOSED,
                };
                let note = match (sprint.start.as_deref(), sprint.end.as_deref()) {
                    (Some(start), Some(end)) => Some(format!("{} to {}", day(start), day(end))),
                    (Some(start), None) => Some(format!("from {}", day(start))),
                    (None, Some(end)) => Some(format!("until {}", day(end))),
                    (None, None) => None,
                };
                Planned {
                    id: sprint.id.clone(),
                    title: sprint.title.clone(),
                    note,
                    phase: Some(phase),
                    closed,
                }
            })
            .collect()
    })
}

pub(super) fn groups(boardy: &Boardy) -> Vec<Group<'_>> {
    let store = &boardy.store;
    let words = &store.words;
    match &boardy.place {
        Place::Board(key, Section::Tickets) if boardy.layout == Layout::Board => lanes(boardy, key)
            .into_iter()
            .map(|(lane, slots)| {
                let (spots, tickets) = slots.iter().map(|slot| (slot.spot, slot.ticket)).unzip();
                Group {
                    status: None,
                    title: lane.label().to_owned(),
                    tickets,
                    spots,
                    folded: false,
                    fold: None,
                }
            })
            .collect(),
        Place::Board(key, Section::Tickets) => by_status(boardy, &held(store, Some(key))),
        Place::Board(key, Section::Epics) => by_plan(
            held(store, Some(key)),
            Plan {
                key: format!("{key}/{EPIC_KIND}"),
                glyph: Glyph::Epic,
                planned: planned_epics(boardy.plans.get(key)),
                name: |ticket| ticket.epic.as_deref(),
                none: format!("No {}", words.epic),
                loose: None,
                stray: None,
            },
            &boardy.flipped,
        ),
        Place::Board(key, Section::Sprints) => by_plan(
            held(store, Some(key)),
            Plan {
                key: format!("{key}/{SPRINT_KIND}"),
                glyph: Glyph::Sprint,
                planned: planned_sprints(boardy.plans.get(key)),
                name: |ticket| ticket.sprint.as_deref(),
                none: format!("No {}", words.sprint),
                loose: Some(UNPLANNED),
                stray: Some(PLANNED),
            },
            &boardy.flipped,
        ),
        Place::Attention => needed(boardy)
            .into_iter()
            .filter(|(_, rows)| !rows.is_empty())
            .map(|(want, rows)| {
                let (spots, tickets) = rows.into_iter().unzip();
                Group {
                    status: None,
                    title: want.label().to_owned(),
                    tickets,
                    spots,
                    folded: false,
                    fold: None,
                }
            })
            .collect(),
        Place::CatchUp => {
            let (spots, tickets) = held(store, None)
                .into_iter()
                .filter(|(_, ticket)| ticket.questions.unwrap_or_default() > 0)
                .unzip();
            vec![Group {
                status: None,
                title: "Open questions".to_owned(),
                tickets,
                spots,
                folded: false,
                fold: None,
            }]
        }
        Place::Board(_, Section::Triage | Section::Milestones) | Place::Ticket(_) => Vec::new(),
    }
}

fn phases(place: &Place) -> &'static [&'static str] {
    match place {
        Place::Board(_, Section::Sprints) => &SPRINT_PHASES,
        _ => &[],
    }
}

fn lines(boardy: &Boardy) -> Vec<Line> {
    let mut lines = Vec::new();
    let mut at = 0;
    let mut phase = None;
    let wanted = phases(&boardy.place);
    let mut next = 0;
    let sprint = &boardy.store.words.sprint;
    for group in groups(boardy) {
        let now = group.fold.as_ref().and_then(|fold| fold.phase);
        if now != phase {
            let rest = wanted.get(next..).unwrap_or_default();
            let reached = now.and_then(|now| rest.iter().position(|held| *held == now));
            let skip = reached.unwrap_or(rest.len());
            lines.extend(empty_phases(rest.get(..skip).unwrap_or_default(), sprint));
            next += skip + usize::from(reached.is_some());
            if let Some(starts) = now {
                lines.push(Line::Phase(starts));
            }
        }
        phase = now;
        lines.push(Line::Head {
            status: group.status,
            title: group.title,
            count: group.tickets.len(),
            folded: group.folded,
            fold: group.fold,
        });
        if group.folded {
            continue;
        }
        for spot in group.spots {
            lines.push(Line::Row { spot, at });
            at += 1;
        }
    }
    lines.extend(empty_phases(wanted.get(next..).unwrap_or_default(), sprint));
    lines
}

fn empty_phases<'a>(phases: &'a [&'static str], noun: &'a str) -> impl Iterator<Item = Line> + 'a {
    phases.iter().flat_map(move |phase| {
        [
            Line::Phase(phase),
            Line::Empty(format!("No {} {noun}", phase.to_lowercase())),
        ]
    })
}

#[derive(IntoElement)]
struct Tipped {
    target: Stateful<Div>,
    tip: &'static str,
}

impl RenderOnce for Tipped {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        tooltip(
            SharedString::from(format!("boardy-tip-{}", self.tip)),
            self.target,
            Edge::Frame,
            self.tip,
            &theme,
            window,
            cx,
        )
    }
}

pub(super) fn reread(boardy: &Boardy, theme: &Theme, cx: &mut Context<Boardy>) -> AnyElement {
    let again = icon_button("boardy-reread", Icon::Reset, "Read again", theme)
        .on_click(cx.listener(|boardy: &mut Boardy, _: &ClickEvent, _, cx| boardy.refetch(cx)));
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(TOOLS_GAP))
        .when(boardy.asking > 0, |strip| {
            strip.child(spinner("boardy-asking", theme))
        })
        .child(Tipped {
            target: again,
            tip: "Read again",
        })
        .into_any_element()
}

fn layout_mark(layout: Layout, color: Rgba) -> Div {
    match layout {
        Layout::List => div()
            .flex()
            .flex_col()
            .gap(px(MARK_GAP))
            .children((0..3).map(|_| div().w(px(MARK)).h(px(MARK_STROKE)).bg(color))),
        Layout::Board => div()
            .flex()
            .items_start()
            .gap(px(MARK_GAP))
            .children(LANES.map(|tall| {
                div()
                    .w(px(LANE))
                    .h(px(tall))
                    .rounded(px(MARK_STROKE))
                    .bg(color)
            })),
    }
}

fn layout_toggle(boardy: &Boardy, theme: &Theme, cx: &mut Context<Boardy>) -> Div {
    let shown = |to: Layout, label: &'static str| {
        let on = boardy.layout == to;
        let color = if on {
            theme.color(ColorToken::TextStrong)
        } else {
            theme.color(ColorToken::TextIcon)
        };
        let face = control(
            SharedString::from(format!("boardy-layout-{label}")),
            label,
            theme,
        )
        .size(px(CONTROL))
        .rounded(px(RADIUS_CONTROL))
        .when(on, |face| face.bg(theme.color(ColorToken::StateActive)))
        .child(layout_mark(to, color))
        .on_click(
            cx.listener(move |boardy: &mut Boardy, _: &ClickEvent, _, cx| {
                eprintln!("desk: boardy layout {to:?}");
                boardy.layout = to;
                boardy.at = 0;
                cx.notify();
            }),
        );
        Tipped {
            target: face,
            tip: label,
        }
    };
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap_0p5()
        .child(shown(Layout::List, "List"))
        .child(shown(Layout::Board, "Board"))
}

const SKELETON_LINES: usize = 8;

pub(super) fn main(
    boardy: &mut Boardy,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<Boardy>,
) -> Div {
    let started = Instant::now();
    let words = boardy.store.words.clone();
    let (crumbs, tickets) = match &boardy.place {
        Place::Board(key, section) => {
            let part = match section {
                Section::Tickets => noun(&words.ticket, true),
                Section::Milestones => MILESTONES.to_owned(),
                Section::Epics => noun(&words.epic, true),
                Section::Sprints => noun(&words.sprint, true),
                Section::Triage => "Triage".to_owned(),
            };
            (vec![key.clone(), part], *section == Section::Tickets)
        }
        _ => (vec!["Need attention".to_owned()], false),
    };
    let tabs = tickets.then(|| layout_toggle(boardy, theme, cx));
    let top = div()
        .flex()
        .flex_none()
        .items_center()
        .gap_2()
        .h(px(BAR))
        .px(px(PAD_X))
        .child(desk_ui::components::breadcrumb::breadcrumb(
            crumbs.into_iter().map(|crumb| (crumb, None)),
        ))
        .child(div().flex_1())
        .children(tabs)
        .child(reread(boardy, theme, cx));
    let failed = boardy
        .failed
        .clone()
        .map(|failed| div().px(px(PAD_X)).pb_2().child(note(failed, theme)));
    let waiting = match &boardy.place {
        Place::Board(key, Section::Triage) => !boardy.triage.contains_key(key),
        Place::Board(key, Section::Milestones) => !boardy.plans.contains_key(key),
        Place::Board(key, _) => !boardy.store.read(key),
        _ => false,
    };
    let stale =
        matches!(&boardy.place, Place::Board(key, Section::Tickets) if boardy.inbox.key != *key);
    if stale && boardy.layout == Layout::Board {
        boardy.set_board();
    }
    let body = match (&boardy.place, boardy.layout) {
        _ if waiting => div()
            .p(px(PAD_X))
            .child(desk_ui::components::skeleton::skeleton_lines(
                "boardy-loading",
                SKELETON_LINES,
                theme,
                cx,
            ))
            .into_any_element(),
        (Place::Attention, _) => attention(boardy, theme, cx),
        (Place::Board(key, Section::Triage), _) => triage(boardy, &key.clone(), theme, window, cx),
        (Place::Board(key, Section::Milestones), _) => milestones(boardy, key, theme, cx),
        (Place::Board(_, Section::Tickets), Layout::Board) => {
            columns(boardy, cx).into_any_element()
        }
        _ => list(boardy, started, theme, cx),
    };
    div()
        .size_full()
        .flex()
        .flex_col()
        .min_h_0()
        .child(top)
        .children(failed)
        .child(div().flex_1().min_h_0().px(px(LIST_INSET)).child(body))
}

fn list(
    boardy: &mut Boardy,
    started: Instant,
    theme: &Theme,
    cx: &mut Context<Boardy>,
) -> AnyElement {
    let lines = lines(boardy);
    if lines.is_empty() {
        return empty_state(
            "boardy-empty",
            "Nothing here",
            None,
            &[],
            &[],
            theme,
            |_, _, _| {},
        )
        .into_any_element();
    }
    if boardy.follow {
        boardy.follow = false;
        let selected = lines
            .iter()
            .position(|line| matches!(line, Line::Row { at, .. } if *at == boardy.at));
        if let Some(line) = selected {
            boardy
                .list
                .scroll_to_item(line, gpui::ScrollStrategy::Center);
        }
    }
    let count = lines.len();
    boardy.lines = Rc::new(lines);
    let base = boardy.list.0.borrow().base_handle.clone();
    let scrolling = boardy.layout == Layout::List
        && boardy
            .wheel
            .is_some_and(|wheel| wheel.elapsed().as_millis() < SCROLL_WINDOW_MS);
    div()
        .relative()
        .size_full()
        .child(
            uniform_list(
                "boardy-list",
                count,
                cx.processor(|boardy: &mut Boardy, range: Range<usize>, _, cx| {
                    let theme = ActiveTheme::theme(cx);
                    let lines = boardy.lines.clone();
                    lines
                        .get(range)
                        .unwrap_or_default()
                        .iter()
                        .map(|line| line_row(boardy, line, &theme, cx))
                        .collect::<Vec<_>>()
                }),
            )
            .track_scroll(&boardy.list)
            .size_full(),
        )
        .child(scrollbar("boardy-list-bar", &base))
        .when(scrolling, |list| {
            list.child(
                canvas(
                    |_, _, _| {},
                    move |_, (), _, _| {
                        eprintln!(
                            "desk: boardy: frame {:.2}ms",
                            started.elapsed().as_secs_f64() * MS_PER_SECOND
                        );
                    },
                )
                .absolute()
                .size_0(),
            )
        })
        .into_any_element()
}

fn line_row(boardy: &Boardy, line: &Line, theme: &Theme, cx: &mut Context<Boardy>) -> AnyElement {
    match line {
        Line::Phase(phase) => div()
            .h(px(LINE))
            .flex()
            .items_end()
            .px(px(PAD_X / 2.0))
            .pb_2()
            .child(caption(*phase, theme))
            .into_any_element(),
        Line::Empty(said) => div()
            .h(px(LINE))
            .flex()
            .items_center()
            .pl(px(FOLD_INDENT))
            .child(note(said.clone(), theme))
            .into_any_element(),
        Line::Head {
            title,
            folded,
            fold: Some(fold),
            ..
        } => fold_head(fold, title, *folded, theme)
            .on_click(toggler(&fold.key, cx))
            .into_any_element(),
        Line::Head {
            status: Some(status),
            title,
            count,
            folded,
            ..
        } => {
            let (toggled, added) = (status.clone(), status.clone());
            let Some(shown) = kit(status) else {
                return div().into_any_element();
            };
            div()
                .w_full()
                .h(px(LINE))
                .flex()
                .items_end()
                .child(
                    group_header(
                        shown,
                        title.clone(),
                        *count,
                        *folded,
                        cx.listener(move |boardy, _: &ClickEvent, _, cx| {
                            boardy.open_create(Some(String::from(added.clone())), cx)
                        }),
                    )
                    .on_toggle(cx.listener(
                        move |boardy, _: &ClickEvent, _, cx| {
                            match boardy.folded.iter().position(|held| *held == toggled) {
                                Some(at) => {
                                    boardy.folded.remove(at);
                                }
                                None => boardy.folded.push(toggled.clone()),
                            }
                            cx.notify();
                        },
                    )),
                )
                .into_any_element()
        }
        Line::Head {
            status: None,
            title,
            count,
            ..
        } => head(title.clone(), *count, theme).into_any_element(),
        Line::Row { spot, at } => match ticket_at(&boardy.store, *spot) {
            Some(ticket) => ticket_row(boardy, ticket, *at == boardy.at, theme, cx),
            None => div().h(px(LINE)).into_any_element(),
        },
    }
}

fn fold_head(fold: &Fold, title: &str, folded: bool, theme: &Theme) -> Stateful<Div> {
    let hover = ink(theme, HOVER);
    let angle = if folded { -FRAC_PI_2 } else { 0.0 };
    div()
        .id(SharedString::from(format!("fold-{}", fold.key)))
        .w_full()
        .h(px(LINE))
        .flex()
        .items_center()
        .gap_3()
        .px(px(PAD_X / 2.0))
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .hover(move |style| style.bg(hover))
        .child(
            glyph(Glyph::Chevron, ICON_TINY, ink(theme, CAPTION_TEXT))
                .with_transformation(Transformation::rotate(radians(angle))),
        )
        .children(fold.chip.clone().map(|chip| icon_chip(fold.glyph, chip)))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .truncate()
                .font_weight(FontWeight::MEDIUM)
                .text_color(ink(theme, T1))
                .when(fold.chip.as_deref() != Some(title), |text| {
                    text.child(title.to_owned())
                }),
        )
        .children(fold.note.clone().map(|said| note(said, theme).flex_none()))
        .child(progress(fold.done, fold.total, theme))
}

fn progress(done: usize, total: usize, theme: &Theme) -> Div {
    let share = match total {
        0 => 0.0,
        total => done as f32 / total as f32,
    };
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap_2()
        .child(
            div()
                .w(px(PROGRESS_WIDTH))
                .h(px(PROGRESS_HEIGHT))
                .rounded_full()
                .bg(ink(theme, PROGRESS_TRACK))
                .child(
                    div()
                        .h_full()
                        .w(relative(share))
                        .rounded_full()
                        .bg(theme.color(ColorToken::StatusAccent)),
                ),
        )
        .child(
            div()
                .flex_none()
                .min_w(px(TALLY_WIDTH))
                .flex()
                .justify_end()
                .whitespace_nowrap()
                .text_size(px(FONT_SMALL))
                .font_features(tabular())
                .text_color(ink(theme, CAPTION_TEXT))
                .child(format!("{done}/{total}")),
        )
}

fn turn(flipped: &mut BTreeSet<String>, key: &str) {
    if !flipped.remove(key) {
        flipped.insert(key.to_owned());
    }
}

fn toggler(
    key: &str,
    cx: &Context<Boardy>,
) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static {
    let key = key.to_owned();
    cx.listener(move |boardy: &mut Boardy, _: &ClickEvent, _, cx| {
        turn(&mut boardy.flipped, &key);
        cx.notify();
    })
}

fn chips(ticket: &Row, theme: &Theme) -> Vec<AnyElement> {
    let label = rgb_to_hsla(theme.color(ColorToken::StatusAccent));
    ticket
        .epic
        .iter()
        .map(|epic| icon_chip(Glyph::Epic, epic.clone()).into_any_element())
        .chain(
            ticket
                .labels
                .iter()
                .map(|name| dot_chip(name.clone(), label).into_any_element()),
        )
        .chain(
            ticket
                .questions
                .filter(|open| *open > 0)
                .map(|open| icon_chip(Glyph::Attention, format!("{open} open")))
                .map(IntoElement::into_any_element),
        )
        .collect()
}

fn who(ticket: &Row, boardy: &Boardy, theme: &Theme) -> Option<Div> {
    let assignee = ticket.assignee.as_deref().filter(|who| !who.is_empty())?;
    let (writer, _) = super::ticket::assigned(assignee, &boardy.requesters);
    Some(
        div()
            .flex()
            .items_center()
            .gap_1p5()
            .min_w_0()
            .text_size(px(FONT_SMALL))
            .text_color(ink(theme, CAPTION_TEXT))
            .child(writer.face(theme).when(running(ticket), |face| {
                face.child(
                    div()
                        .absolute()
                        .right_0()
                        .bottom_0()
                        .size(px(7.0))
                        .rounded_full()
                        .bg(theme.color(ColorToken::StatusLive)),
                )
            }))
            .child(div().min_w_0().truncate().child(writer.name().to_owned())),
    )
}

fn aim(
    id: &str,
    cx: &Context<Boardy>,
) -> impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static {
    let id = id.to_owned();
    cx.listener(move |boardy: &mut Boardy, _: &MouseDownEvent, _, cx| boardy.aim(id.clone(), cx))
}

pub(super) fn opener(
    id: &str,
    cx: &Context<Boardy>,
) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static {
    let id = id.to_owned();
    cx.listener(move |boardy: &mut Boardy, _: &ClickEvent, _, cx| {
        boardy.open_ticket(id.clone(), cx);
        cx.notify();
    })
}

fn ticket_row(
    boardy: &Boardy,
    ticket: &Row,
    selected: bool,
    theme: &Theme,
    cx: &mut Context<Boardy>,
) -> AnyElement {
    let mut row = issue_row(ElementId::Name(ticket.id.clone().into()))
        .ticket(ticket.id.clone())
        .age(age(&ticket.created, Utc::now()))
        .title(ticket.title.clone())
        .chips(chips(ticket, theme))
        .date(day(&ticket.updated))
        .selected(selected)
        .on_click(opener(&ticket.id, cx));
    if let Some(priority) = look::priority(&ticket.priority) {
        row = row.priority(priority);
    }
    if let Some(status) = kit(&ticket.status) {
        row = row.status(status);
    }
    if let Some(who) = who(ticket, boardy, theme) {
        row = row.avatar(who);
    }
    div()
        .w_full()
        .h(px(LINE))
        .flex()
        .items_center()
        .on_mouse_down(MouseButton::Right, aim(&ticket.id, cx))
        .child(row)
        .into_any_element()
}

#[derive(Clone)]
enum Said {
    Fixed(SharedString),
    Since(DateTime<Utc>),
}

pub(super) struct Card {
    id: SharedString,
    title: SharedString,
    summary: Option<SharedString>,
    state: RunState,
    said: Said,
    question: Option<AskedQuestion>,
    review: bool,
}

#[derive(Default)]
pub(super) struct Inbox {
    pub(super) key: String,
    lanes: Vec<(Lane, Rc<[Card]>)>,
}

pub(super) fn inbox(boardy: &Boardy, key: &str) -> Inbox {
    let lanes = lanes(boardy, key)
        .into_iter()
        .map(|(lane, slots)| {
            let cards: Rc<[Card]> = slots
                .iter()
                .map(|slot| filed(slot, boardy.tails.get(&slot.ticket.id)))
                .collect();
            (lane, cards)
        })
        .collect();
    Inbox {
        key: key.to_owned(),
        lanes,
    }
}

fn filed(slot: &Slot, tail: Option<&Tail>) -> Card {
    let ticket = slot.ticket;
    let fixed = |text: &str| Said::Fixed(SharedString::from(text.to_owned()));
    let said = match (slot.state, slot.question) {
        (RunState::Waiting, Some(question)) => fixed(&question.tldr),
        (RunState::Waiting, None) if ticket.status == Status::Review => fixed(WAITING_REVIEW),
        (RunState::Waiting | RunState::Failed, _) => fixed(look::label(&ticket.status)),
        (RunState::Running, _) => {
            let since = tail.and_then(Tail::since).unwrap_or(&ticket.updated);
            stamp(since).map_or_else(|| fixed(""), Said::Since)
        }
        (RunState::Done, _) => fixed(
            &tail
                .and_then(Tail::took)
                .unwrap_or_else(|| day(&ticket.updated)),
        ),
    };
    Card {
        id: ticket.id.clone().into(),
        title: ticket.title.clone().into(),
        summary: tail
            .and_then(|tail| tail.told.last())
            .map(|(_, said)| said.clone()),
        state: slot.state,
        said,
        question: slot.question.cloned(),
        review: ticket.status == Status::Review,
    }
}

fn columns(boardy: &Boardy, cx: &mut Context<Boardy>) -> impl IntoElement {
    let theme = ActiveTheme::theme(cx);
    let tint = theme.color(ColorToken::StatusAccent);
    let mut first = 0;
    let mut built = Vec::new();
    for (lane, cards) in &boardy.inbox.lanes {
        let count = cards.len();
        let tall = boardy
            .unfolded
            .as_deref()
            .and_then(|open| cards.iter().position(|card| card.id.as_ref() == open));
        let (cards, from) = (cards.clone(), first);
        first += count;
        let draw = cx.processor(move |boardy: &mut Boardy, range: Range<usize>, _, cx| {
            let theme = ActiveTheme::theme(cx);
            let now = Utc::now();
            let shown = cards.get(range.clone()).unwrap_or_default();
            if shown.iter().any(|card| card.state == RunState::Running) {
                boardy.tick(cx);
            }
            shown
                .iter()
                .zip(range)
                .map(|(card, at)| {
                    let selected = from + at == boardy.at;
                    drawn(boardy, card, selected, now, &theme, cx)
                })
                .collect()
        });
        built.push(
            board_rows(
                ElementId::Name(format!("boardy-lane-{}", lane.label()).into()),
                ColumnHeader {
                    status: lane.status(),
                    title: lane.label().into(),
                    count,
                },
                tall,
                draw,
            )
            .inbox(tint),
        );
    }
    div()
        .relative()
        .size_full()
        .min_w_0()
        .child(
            div()
                .id("boardy-board")
                .size_full()
                .overflow_x_scroll()
                .restrict_scroll_to_axis()
                .track_scroll(&boardy.board_scroll)
                .flex()
                .gap(px(COLUMN_GAP))
                .children(built),
        )
        .child(scrollbar("boardy-board-bar", &boardy.board_scroll).axis(Axis::Horizontal))
}

fn drawn(
    boardy: &Boardy,
    shown: &Card,
    selected: bool,
    now: DateTime<Utc>,
    theme: &Theme,
    cx: &mut Context<Boardy>,
) -> AnyElement {
    let id = ElementId::Name(shown.id.clone());
    let said = match &shown.said {
        Said::Fixed(said) => said.clone(),
        Said::Since(since) => span(*since, now).into(),
    };
    let mut card = board_card(id.clone())
        .title(shown.title.clone())
        .run(shown.state, said)
        .selected(selected)
        .on_click(unfolder(&shown.id, cx));
    if let Some(summary) = &shown.summary {
        card = card.summary(summary.clone());
    }
    if boardy.unfolded.as_deref() == Some(shown.id.as_ref()) {
        let tail = boardy.tails.get(shown.id.as_ref());
        let told = tail.map_or(&[][..], |tail| tail.told.as_slice());
        let picks = shown.question.as_ref().and_then(|question| {
            let option = recommended(question)?;
            question.options.get(option.checked_sub(1)?)
        });
        let moments = told
            .iter()
            .map(|(at, text)| Moment {
                text: text.clone(),
                when: ago(at, now).into(),
            })
            .chain(picks.map(|option| Moment {
                text: format!("{RECOMMENDED}: {option}").into(),
                when: SharedString::default(),
            }))
            .collect();
        card = card.unfolded(moments, actions(shown, theme, cx));
    }
    flip(
        id,
        &boardy.flips,
        div()
            .flex_none()
            .on_mouse_down(MouseButton::Right, aim(&shown.id, cx))
            .child(card),
    )
    .into_any_element()
}

fn recommended(question: &AskedQuestion) -> Option<usize> {
    usize::try_from(question.recommended)
        .ok()
        .filter(|option| (1..=question.options.len()).contains(option))
}

enum Decision {
    Answer(i64, String),
    Move(Status),
}

fn actions(shown: &Card, theme: &Theme, cx: &mut Context<Boardy>) -> Vec<AnyElement> {
    let id = shown.id.as_ref();
    let (reject, approve) = match (&shown.question, shown.review) {
        (Some(question), _) => (
            Decision::Answer(question.id, REJECTED.to_owned()),
            Decision::Answer(
                question.id,
                recommended(question).map_or_else(|| APPROVED.to_owned(), |n| n.to_string()),
            ),
        ),
        (None, true) => (Decision::Move(Status::Doing), Decision::Move(Status::Done)),
        (None, false) => {
            let ticket = id.to_owned();
            let open = button(
                SharedString::from(format!("open-{id}")),
                "Open",
                None,
                ButtonKind::Text,
                theme,
            )
            .on_click(cx.listener(
                move |boardy: &mut Boardy, _: &ClickEvent, _, cx| {
                    cx.stop_propagation();
                    boardy.open_ticket(ticket.clone(), cx);
                    cx.notify();
                },
            ));
            return vec![open.into_any_element()];
        }
    };
    vec![
        decide(id, "Reject", ButtonKind::Text, reject, theme, cx),
        decide(id, "Approve", ButtonKind::Primary, approve, theme, cx),
    ]
}

fn decide(
    id: &str,
    label: &'static str,
    kind: ButtonKind,
    decision: Decision,
    theme: &Theme,
    cx: &mut Context<Boardy>,
) -> AnyElement {
    let ticket = id.to_owned();
    button(
        SharedString::from(format!("{label}-{id}")),
        label,
        None,
        kind,
        theme,
    )
    .on_click(
        cx.listener(move |boardy: &mut Boardy, _: &ClickEvent, _, cx| {
            cx.stop_propagation();
            eprintln!("desk: boardy {label} {ticket} from its card");
            boardy.unfolded = None;
            match &decision {
                Decision::Answer(question, answer) => {
                    boardy.answer_question(ticket.clone(), *question, answer.clone(), cx)
                }
                Decision::Move(to) => boardy.move_to(ticket.clone(), to.clone(), cx),
            }
            cx.notify();
        }),
    )
    .into_any_element()
}

fn unfolder(
    id: &str,
    cx: &Context<Boardy>,
) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static {
    let id = id.to_owned();
    cx.listener(move |boardy: &mut Boardy, _: &ClickEvent, _, cx| {
        boardy.unfolded = match boardy.unfolded.as_deref() == Some(id.as_str()) {
            true => None,
            false => Some(id.clone()),
        };
        cx.notify();
    })
}

fn triage(
    boardy: &Boardy,
    key: &str,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<Boardy>,
) -> AnyElement {
    let Some(requests) = boardy.triage.get(key) else {
        return note("Reading triage", theme).into_any_element();
    };
    if requests.is_empty() {
        return empty_state(
            "boardy-triage",
            "Triage is empty",
            None,
            &[],
            &[],
            theme,
            |_, _, _| {},
        )
        .into_any_element();
    }
    let now = Utc::now();
    ScrollArea::new("boardy-triage")
        .child(
            div()
                .flex()
                .flex_col()
                .gap_1()
                .children(requests.iter().map(|request| {
                    let chip = super::ticket::asker(
                        &format!("request-by-{}", request.id),
                        &request.by.id,
                        &format!("· {}", age(&request.at, now)),
                        boardy,
                        theme,
                        window,
                        cx,
                    );
                    let act = |accept: bool| {
                        let (board, id) = (key.to_owned(), request.id.clone());
                        cx.listener(move |boardy: &mut Boardy, _: &ClickEvent, _, cx| {
                            cx.stop_propagation();
                            boardy.triage_act(board.clone(), id.clone(), accept, cx)
                        })
                    };
                    let (board, id) = (key.to_owned(), request.id.clone());
                    div()
                        .id(SharedString::from(format!("request-{}", request.id)))
                        .cursor_pointer()
                        .on_click(
                            cx.listener(move |boardy: &mut Boardy, _: &ClickEvent, _, cx| {
                                boardy.open_request(board.clone(), &id, cx)
                            }),
                        )
                        .flex()
                        .items_center()
                        .gap_3()
                        .h(px(LINE))
                        .px(px(PAD_X / 2.0))
                        .child(note(request.id.clone(), theme))
                        .child(
                            div()
                                .flex_1()
                                .min_w_0()
                                .truncate()
                                .font_weight(FontWeight::MEDIUM)
                                .text_color(ink(theme, T1))
                                .child(request.ticket.title.clone()),
                        )
                        .child(chip)
                        .child(
                            button(
                                SharedString::from(format!("accept-{}", request.id)),
                                "Accept",
                                Some(Glyph::Check),
                                ButtonKind::Plain,
                                theme,
                            )
                            .on_click(act(true)),
                        )
                        .child(
                            button(
                                SharedString::from(format!("drop-{}", request.id)),
                                "Drop",
                                None,
                                ButtonKind::Text,
                                theme,
                            )
                            .on_click(act(false)),
                        )
                })),
        )
        .into_any_element()
}

fn milestones(boardy: &Boardy, key: &str, theme: &Theme, cx: &mut Context<Boardy>) -> AnyElement {
    let Some(plan) = boardy.plans.get(key) else {
        return empty_state(
            "boardy-plan",
            "Reading the plan",
            None,
            &[],
            &[],
            theme,
            |_, _, _| {},
        )
        .into_any_element();
    };
    let tickets = boardy.store.tickets(key);
    let in_epic = |epic: &str| {
        tally(
            tickets
                .iter()
                .filter(move |ticket| ticket.epic.as_deref() == Some(epic)),
        )
    };
    let section = |id: Option<&str>, due: Option<&str>, epics: &[&PlannedEpic]| {
        let (done, total) = epics
            .iter()
            .map(|epic| in_epic(&epic.id))
            .fold((0, 0), |(done, total), (more, all)| {
                (done + more, total + all)
            });
        Fold {
            key: format!("{key}/{MILESTONE_KIND}/{}", id.unwrap_or_default()),
            glyph: Glyph::Milestone,
            chip: id.map(str::to_owned),
            note: due.map(|due| format!("due {}", day(due))),
            phase: None,
            done,
            total,
        }
    };
    let mut sections: Vec<(Fold, String, Vec<&PlannedEpic>)> = plan
        .milestones
        .iter()
        .map(|milestone| {
            let epics: Vec<&PlannedEpic> = plan
                .epics
                .iter()
                .filter(|epic| epic.milestone.as_deref() == Some(milestone.id.as_str()))
                .collect();
            let fold = section(Some(&milestone.id), milestone.due.as_deref(), &epics);
            (fold, milestone.title.clone(), epics)
        })
        .collect();
    let loose: Vec<&PlannedEpic> = plan
        .epics
        .iter()
        .filter(|epic| {
            epic.milestone
                .as_deref()
                .is_none_or(|id| plan.milestones.iter().all(|milestone| milestone.id != id))
        })
        .collect();
    if !loose.is_empty() {
        let fold = section(None, None, &loose);
        sections.push((fold, NO_MILESTONE.to_owned(), loose));
    }
    let hover = ink(theme, HOVER);
    let mut cards: Vec<AnyElement> = Vec::new();
    for (fold, title, epics) in sections {
        let folded = !boardy.flipped.contains(&fold.key);
        cards.push(
            fold_head(&fold, &title, folded, theme)
                .on_click(toggler(&fold.key, cx))
                .into_any_element(),
        );
        if folded {
            continue;
        }
        if epics.is_empty() {
            cards.push(
                div()
                    .h(px(LINE))
                    .flex()
                    .items_center()
                    .pl(px(FOLD_INDENT))
                    .child(note(
                        format!("No {} yet", noun(&plan.words.epic, true)),
                        theme,
                    ))
                    .into_any_element(),
            );
        }
        for epic in epics {
            let (done, total) = in_epic(&epic.id);
            let closed = epic.status == BoardyPlannedEpicStatus::Done;
            let (board, wanted) = (key.to_owned(), format!("{key}/{EPIC_KIND}/{}", epic.id));
            cards.push(
                div()
                    .id(SharedString::from(format!("milestone-epic-{}", epic.id)))
                    .h(px(LINE))
                    .flex()
                    .items_center()
                    .gap_3()
                    .pl(px(FOLD_INDENT))
                    .pr(px(PAD_X / 2.0))
                    .rounded(px(RADIUS_ROW))
                    .cursor_pointer()
                    .hover(move |style| style.bg(hover))
                    .on_click(
                        cx.listener(move |boardy: &mut Boardy, _: &ClickEvent, _, cx| {
                            boardy.go(Place::Board(board.clone(), Section::Epics), cx);
                            let folded = groups(boardy).iter().any(|group| {
                                group.folded
                                    && group.fold.as_ref().is_some_and(|fold| fold.key == wanted)
                            });
                            if folded {
                                turn(&mut boardy.flipped, &wanted);
                            }
                            cx.notify();
                        }),
                    )
                    .child(icon_chip(
                        if closed { Glyph::Check } else { Glyph::Epic },
                        epic.id.clone(),
                    ))
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .truncate()
                            .text_color(ink(theme, if closed { T2 } else { T1 }))
                            .when(epic.title != epic.id, |text| text.child(epic.title.clone())),
                    )
                    .child(progress(done, total, theme))
                    .into_any_element(),
            );
        }
    }
    if cards.is_empty() {
        return empty_state(
            "boardy-plan",
            "No milestones or epics yet",
            None,
            &[],
            &[],
            theme,
            |_, _, _| {},
        )
        .into_any_element();
    }
    ScrollArea::new("boardy-milestones")
        .child(div().flex().flex_col().gap_1().children(cards))
        .into_any_element()
}

fn head(title: impl Into<SharedString>, count: usize, theme: &Theme) -> Div {
    div()
        .h(px(LINE))
        .flex()
        .items_end()
        .gap_2()
        .px(px(PAD_X / 2.0))
        .pb_2()
        .child(caption(title, theme))
        .child(
            div()
                .text_size(px(FONT_SMALL))
                .text_color(ink(theme, CAPTION_TEXT))
                .child(count.to_string()),
        )
}

fn facts(ticket: &Row, points: &str) -> String {
    [
        Some(String::from(ticket.r#type.clone())),
        ticket
            .points
            .filter(|points| *points > 0)
            .map(|n| format!("{points} {n}")),
        ticket.model.clone().filter(|model| !model.is_empty()),
        ticket
            .tools
            .filter(|tools| *tools > 0)
            .map(|tools| format!("{tools} tools")),
    ]
    .into_iter()
    .flatten()
    .collect::<Vec<_>>()
    .join(" · ")
}

fn entry(
    boardy: &Boardy,
    scope: &str,
    id: &str,
    said: Option<String>,
    tail: String,
    theme: &Theme,
    cx: &Context<Boardy>,
) -> AnyElement {
    let ticket = boardy.store.find(id);
    let hover = ink(theme, HOVER);
    div()
        .id(SharedString::from(format!("{scope}-{id}")))
        .h(px(LINE))
        .flex()
        .items_center()
        .gap_3()
        .px(px(PAD_X / 2.0))
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .hover(move |style| style.bg(hover))
        .on_click(opener(id, cx))
        .child(note(id.to_owned(), theme).flex_none())
        .child(
            div()
                .min_w_0()
                .truncate()
                .font_weight(FontWeight::MEDIUM)
                .text_color(ink(theme, T1))
                .child(ticket.map_or_else(String::new, |ticket| ticket.title.clone())),
        )
        .child(
            div()
                .flex_1()
                .min_w_0()
                .truncate()
                .text_color(ink(theme, T2))
                .child(said.unwrap_or_default()),
        )
        .children(
            ticket
                .map(|ticket| facts(ticket, &boardy.store.words.points))
                .map(|facts| note(facts, theme).flex_none()),
        )
        .child(note(tail, theme).flex_none())
        .into_any_element()
}

fn reason(ticket: Option<&Row>) -> Option<String> {
    ticket?.reason.clone().filter(|reason| !reason.is_empty())
}

fn attention(boardy: &Boardy, theme: &Theme, cx: &mut Context<Boardy>) -> AnyElement {
    let now = Utc::now();
    let mut shown: Vec<AnyElement> = Vec::new();
    for (want, rows) in needed(boardy) {
        let ids = match want {
            Need::Decide => deciding(boardy, &rows),
            Need::Blocked | Need::Stale => rows.iter().map(|(_, row)| row.id.as_str()).collect(),
        };
        if ids.is_empty() {
            continue;
        }
        shown.push(head(want.label(), ids.len(), theme).into_any_element());
        for id in ids {
            let ticket = boardy.store.find(id);
            let tail = match (want, ticket) {
                (Need::Stale, Some(ticket)) => in_review(boardy, ticket)
                    .map_or_else(String::new, |days| format!("in review {days}d")),
                (_, Some(ticket)) => ago(&ticket.updated, now),
                (_, None) => String::new(),
            };
            shown.push(entry(
                boardy,
                "attention",
                id,
                reason(ticket),
                tail,
                theme,
                cx,
            ));
            shown.extend(
                boardy
                    .questions
                    .iter()
                    .filter(|question| question.ticket == id)
                    .map(|question| asked(question, theme, cx)),
            );
        }
    }
    if shown.is_empty() {
        return empty_state(
            "boardy-attention",
            "Nothing needs you",
            None,
            &[],
            &[],
            theme,
            |_, _, _| {},
        )
        .into_any_element();
    }
    ScrollArea::new("boardy-attention")
        .child(div().flex().flex_col().children(shown))
        .into_any_element()
}

fn digest(
    boardy: &Boardy,
    board: usize,
    decide: &[&str],
    theme: &Theme,
    cx: &Context<Boardy>,
) -> Vec<AnyElement> {
    let Some(summary) = boardy.store.boards.get(board) else {
        return Vec::new();
    };
    let key = summary.key.as_str();
    let now = Utc::now();
    let report = boardy.reports.get(key);
    let mut shown = vec![
        div()
            .h(px(LINE))
            .flex()
            .items_end()
            .gap_2()
            .px(px(PAD_X / 2.0))
            .pb_2()
            .child(
                div()
                    .flex_none()
                    .font_weight(FontWeight::SEMIBOLD)
                    .text_color(ink(theme, T1))
                    .child(key.to_owned()),
            )
            .child(note(
                report.map_or_else(
                    || "Reading what moved".to_owned(),
                    |report| {
                        format!(
                            "{} moves, {} finished, {} rounds since {}",
                            report.moves,
                            report.finished.len(),
                            report.rounds,
                            day(&report.since)
                        )
                    },
                ),
                theme,
            ))
            .into_any_element(),
    ];
    let Some(report) = report else {
        return shown;
    };
    let on_board = |id: &&str| id.split_once('-').is_some_and(|(held, _)| held == key);
    let decide: Vec<&str> = decide.iter().copied().filter(on_board).collect();
    let since = stamp(&report.since);
    let mut moved: Vec<&Row> = boardy
        .store
        .tickets(key)
        .iter()
        .filter(|ticket| {
            since.is_some_and(|since| stamp(&ticket.updated).is_some_and(|at| at > since))
                && !report.finished.contains(&ticket.id)
                && !decide.contains(&ticket.id.as_str())
        })
        .collect();
    moved.sort_by(|a, b| b.updated.cmp(&a.updated));
    let more = |count: usize, theme: &Theme| {
        let board = key.to_owned();
        count
            .checked_sub(DIGEST_ROWS)
            .filter(|left| *left > 0)
            .map(|left| {
                div()
                    .id(SharedString::from(format!("digest-more-{board}-{count}")))
                    .h(px(LINE))
                    .flex()
                    .items_center()
                    .px(px(PAD_X / 2.0))
                    .cursor_pointer()
                    .on_click(
                        cx.listener(move |boardy: &mut Boardy, _: &ClickEvent, _, cx| {
                            boardy.go(Place::Board(board.clone(), Section::Tickets), cx)
                        }),
                    )
                    .child(note(format!("{left} more on {key}"), theme))
                    .into_any_element()
            })
    };
    if !moved.is_empty() {
        shown.push(head("Moved", moved.len(), theme).into_any_element());
        shown.extend(moved.iter().take(DIGEST_ROWS).map(|ticket| {
            let said = Some(look::label(&ticket.status).to_owned());
            let tail = ago(&ticket.updated, now);
            entry(boardy, "moved", &ticket.id, said, tail, theme, cx)
        }));
        shown.extend(more(moved.len(), theme));
    }
    if !decide.is_empty() {
        shown.push(head("Decide", decide.len(), theme).into_any_element());
        shown.extend(decide.iter().map(|id| {
            let said = boardy
                .questions
                .iter()
                .find(|question| question.ticket == *id)
                .map(|question| question.tldr.clone())
                .or_else(|| reason(boardy.store.find(id)));
            entry(boardy, "decide", id, said, String::new(), theme, cx)
        }));
    }
    if !report.finished.is_empty() {
        shown.push(head("Finished", report.finished.len(), theme).into_any_element());
        shown.extend(report.finished.iter().take(DIGEST_ROWS).map(|id| {
            let tail = boardy
                .store
                .find(id)
                .map_or_else(String::new, |ticket| ago(&ticket.updated, now));
            entry(boardy, "finished", id, None, tail, theme, cx)
        }));
        shown.extend(more(report.finished.len(), theme));
    }
    if moved.is_empty() && decide.is_empty() && report.finished.is_empty() {
        shown.push(
            div()
                .h(px(LINE))
                .flex()
                .items_center()
                .px(px(PAD_X / 2.0))
                .child(note(
                    format!("Nothing moved since {}", day(&report.since)),
                    theme,
                ))
                .into_any_element(),
        );
    }
    shown
}

pub(super) fn catch_up(boardy: &Boardy, theme: &Theme, cx: &mut Context<Boardy>) -> Div {
    let decide = needed(boardy)
        .into_iter()
        .find(|(want, _)| *want == Need::Decide)
        .map_or_else(Vec::new, |(_, rows)| deciding(boardy, &rows));
    let digests: Vec<AnyElement> = (0..boardy.store.boards.len())
        .flat_map(|board| digest(boardy, board, &decide, theme, cx))
        .collect();
    let board = &boardy.store.words.board;
    let start = boardy.no_boards.then(|| {
        let make = match &boardy.naming {
            Some(input) => input.clone().into_any_element(),
            None => button(
                "boardy-init",
                format!("Create a {board}"),
                Some(Glyph::FolderAdd),
                ButtonKind::Primary,
                theme,
            )
            .on_click(
                cx.listener(|boardy: &mut Boardy, _: &ClickEvent, window, cx| {
                    boardy.start_naming(window, cx)
                }),
            )
            .into_any_element(),
        };
        div()
            .flex()
            .flex_col()
            .gap_2()
            .p_3()
            .rounded(px(10.0))
            .bg(ink(theme, 0.03))
            .child(note(
                format!("tofu has no {board}s for {}", boardy.project),
                theme,
            ))
            .child(make)
    });
    div().size_full().child(
        ScrollArea::new("boardy-catch-up").child(
            div()
                .flex()
                .flex_col()
                .px(px(PAD_X / 2.0))
                .pb(px(PAD_X))
                .children(start)
                .children(digests),
        ),
    )
}

fn asked(question: &AskedQuestion, theme: &Theme, cx: &mut Context<Boardy>) -> AnyElement {
    let options: Vec<AnyElement> = question
        .options
        .iter()
        .enumerate()
        .map(|(at, option)| {
            let number = at.saturating_add(1);
            let recommended = i64::try_from(number).is_ok_and(|n| n == question.recommended);
            let chosen = question.answer.as_deref() == Some(number.to_string().as_str());
            let (ticket, id) = (question.ticket.clone(), question.id);
            button(
                SharedString::from(format!("answer-{ticket}-{id}-{number}")),
                format!("{number}. {option}"),
                recommended.then_some(Glyph::Check),
                if chosen {
                    ButtonKind::Primary
                } else {
                    ButtonKind::Plain
                },
                theme,
            )
            .on_click(
                cx.listener(move |boardy: &mut Boardy, _: &ClickEvent, _, cx| {
                    boardy.answer_question(ticket.clone(), id, number.to_string(), cx)
                }),
            )
            .into_any_element()
        })
        .collect();
    div()
        .flex()
        .flex_col()
        .gap_1()
        .pl(px(FOLD_INDENT))
        .pr(px(PAD_X / 2.0))
        .pb_3()
        .child(
            div()
                .flex()
                .items_center()
                .gap_2()
                .child(note(format!("Q{}", question.id), theme).flex_none())
                .child(
                    div()
                        .font_weight(FontWeight::SEMIBOLD)
                        .text_color(ink(theme, T1))
                        .child(question.tldr.clone()),
                ),
        )
        .child(
            div()
                .text_color(ink(theme, T2))
                .child(question.eli5.clone()),
        )
        .child(div().flex().flex_wrap().gap_2().children(options))
        .child(Answer {
            ticket: question.ticket.clone(),
            id: question.id,
            boardy: cx.weak_entity(),
        })
        .into_any_element()
}

#[derive(IntoElement)]
struct Answer {
    ticket: String,
    id: i64,
    boardy: WeakEntity<Boardy>,
}

impl RenderOnce for Answer {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let key = SharedString::from(format!("boardy-answer-{}-{}", self.ticket, self.id));
        let Answer { ticket, id, boardy } = self;
        let field = window.use_keyed_state(key, cx, move |window, cx| {
            TextArea::new("Answer in your own words".into(), window, cx)
                .max_lines(1)
                .on_submit(move |text, _, cx| {
                    let text = text.trim().to_owned();
                    if text.is_empty() {
                        return;
                    }
                    let sent = boardy.update(cx, |boardy, cx| {
                        boardy.answer_question(ticket.clone(), id, text, cx)
                    });
                    if let Err(error) = sent {
                        eprintln!("desk: boardy closed before the answer was sent: {error}");
                    }
                })
        });
        div().pt_1().child(field)
    }
}
