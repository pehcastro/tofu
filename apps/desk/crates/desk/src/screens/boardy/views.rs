use std::ops::Range;
use std::rc::Rc;

use chrono::Utc;
use desk_core::boardy::{COLUMNS, Flow, running};
use desk_core::protocol::BoardyPlannedEpicStatus;
use desk_core::protocol::BoardyTicketStatus as Status;
use desk_core::protocol::boardy::{Aging, AskedQuestion, PlanAnswer, PlannedEpic, Ticket};
use desk_ui::components::board::{ColumnHeader, board_card, board_column};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::caption;
use desk_ui::components::dot_chip::{dot_chip, icon_chip};
use desk_ui::components::empty::empty_state;
use desk_ui::components::flip::flip;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::issue_row::{group_header, issue_row};
use desk_ui::components::paint::ink;
use desk_ui::components::pill_tabs::PillTabs;
use desk_ui::components::priority_icon::priority_icon;
use desk_ui::components::scroll::{ScrollArea, scrollbar};
use desk_ui::components::size::{CAPTION_TEXT, FONT_SMALL, T1, T2};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, App, Axis, ClickEvent, Context, Div, ElementId, FontWeight, MouseButton,
    MouseDownEvent, SharedString, Window, div, prelude::*, px, rgb_to_hsla, uniform_list,
};

use super::look::{self, age, day, disc, kit, noun};
use super::{Boardy, Layout, Place, Section};
use crate::screens::frame::note;

const LINE: f32 = 40.0;
const PAD_X: f32 = 16.0;
const BAR: f32 = 44.0;
const CLOSED_SHOWN: usize = 30;
const CARD_GAP: f32 = 6.0;
const COLUMN_GAP: f32 = 10.0;
pub(super) const MILESTONES: &str = "Milestones";
const NO_MILESTONE: &str = "No milestone";

pub(super) struct Group<'a> {
    pub status: Option<Status>,
    pub title: String,
    pub tickets: Vec<&'a Ticket>,
    pub folded: bool,
}

pub(super) enum Line {
    Head {
        status: Option<Status>,
        title: String,
        count: usize,
        folded: bool,
    },
    Row {
        id: String,
        at: usize,
    },
}

fn by_status<'a>(boardy: &Boardy, tickets: &[&'a Ticket]) -> Vec<Group<'a>> {
    COLUMNS
        .iter()
        .map(|status| Group {
            status: Some(status.clone()),
            title: look::label(status).to_owned(),
            tickets: tickets
                .iter()
                .copied()
                .filter(|ticket| ticket.status == *status)
                .collect(),
            folded: boardy.layout == Layout::List && boardy.folded.contains(status),
        })
        .filter(|group| !group.tickets.is_empty())
        .collect()
}

fn by_plan<'a>(
    tickets: &'a [Ticket],
    planned: Vec<(String, String)>,
    name: fn(&Ticket) -> Option<&str>,
    none: &str,
) -> Vec<Group<'a>> {
    let mut keyed: Vec<(Option<String>, Group<'a>)> = planned
        .into_iter()
        .map(|(id, title)| {
            let group = Group {
                status: None,
                title,
                tickets: Vec::new(),
                folded: false,
            };
            (Some(id), group)
        })
        .collect();
    for ticket in tickets {
        let key = name(ticket).map(str::to_owned);
        match keyed.iter_mut().find(|(held, _)| *held == key) {
            Some((_, group)) => group.tickets.push(ticket),
            None => {
                let title = key.clone().unwrap_or_else(|| none.to_owned());
                let group = Group {
                    status: None,
                    title,
                    tickets: vec![ticket],
                    folded: false,
                };
                keyed.push((key, group));
            }
        }
    }
    keyed.into_iter().map(|(_, group)| group).collect()
}

fn planned_epics(plan: Option<&PlanAnswer>) -> Vec<(String, String)> {
    plan.map_or_else(Vec::new, |plan| {
        plan.epics
            .iter()
            .map(|epic| {
                let done = if epic.status == BoardyPlannedEpicStatus::Done {
                    " · done"
                } else {
                    ""
                };
                (epic.id.clone(), format!("{} {}{done}", epic.id, epic.title))
            })
            .collect()
    })
}

fn planned_sprints(plan: Option<&PlanAnswer>) -> Vec<(String, String)> {
    plan.map_or_else(Vec::new, |plan| {
        plan.sprints
            .iter()
            .map(|sprint| {
                let active = if sprint.active { " · active" } else { "" };
                let dates = match (&sprint.start, &sprint.end) {
                    (Some(start), Some(end)) => format!(" · {} to {}", day(start), day(end)),
                    _ => String::new(),
                };
                let title = format!("{} {}{active}{dates}", sprint.id, sprint.title);
                (sprint.id.clone(), title)
            })
            .collect()
    })
}

pub(super) fn groups(boardy: &Boardy) -> Vec<Group<'_>> {
    let store = &boardy.store;
    let words = &store.words;
    match &boardy.place {
        Place::Board(key, Section::Tickets) => {
            by_status(boardy, &store.tickets(key).iter().collect::<Vec<_>>())
        }
        Place::Board(key, Section::Epics) => by_plan(
            store.tickets(key),
            planned_epics(boardy.plans.get(key)),
            |ticket| ticket.epic.as_deref(),
            &format!("No {}", words.epic),
        ),
        Place::Board(key, Section::Sprints) => by_plan(
            store.tickets(key),
            planned_sprints(boardy.plans.get(key)),
            |ticket| ticket.sprint.as_deref(),
            &format!("No {}", words.sprint),
        ),
        Place::Attention => by_status(boardy, &store.attention()),
        Place::CatchUp => vec![Group {
            status: None,
            title: "Open questions".to_owned(),
            tickets: store
                .attention()
                .into_iter()
                .filter(|ticket| !ticket.questions.is_empty())
                .collect(),
            folded: false,
        }],
        Place::Board(_, Section::Triage | Section::Milestones) | Place::Ticket(_) => Vec::new(),
    }
}

fn lines(boardy: &Boardy) -> Vec<Line> {
    let mut lines = Vec::new();
    let mut at = 0;
    for group in groups(boardy) {
        lines.push(Line::Head {
            status: group.status,
            title: group.title,
            count: group.tickets.len(),
            folded: group.folded,
        });
        if group.folded {
            continue;
        }
        for ticket in group.tickets {
            lines.push(Line::Row {
                id: ticket.id.clone(),
                at,
            });
            at += 1;
        }
    }
    lines
}

pub(super) fn main(boardy: &mut Boardy, theme: &Theme, cx: &mut Context<Boardy>) -> Div {
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
    let tabs = tickets.then(|| {
        let layout = boardy.layout;
        let pick = |to: Layout| {
            cx.listener(move |boardy: &mut Boardy, _: &ClickEvent, _, cx| {
                eprintln!("desk: boardy layout {to:?}");
                boardy.layout = to;
                boardy.at = 0;
                cx.notify();
            })
        };
        PillTabs::new("boardy-layout")
            .tab("List", layout == Layout::List, pick(Layout::List))
            .tab("Board", layout == Layout::Board, pick(Layout::Board))
    });
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
        .children(tabs);
    let creating = boardy.creating.as_ref().map(|(status, input)| {
        div()
            .flex()
            .items_center()
            .gap_2()
            .px(px(PAD_X))
            .pb_2()
            .child(note(format!("New in {}", look::label(status)), theme))
            .child(input.clone())
    });
    let failed = boardy
        .failed
        .clone()
        .map(|failed| div().px(px(PAD_X)).pb_2().child(note(failed, theme)));
    let body = match (&boardy.place, boardy.layout) {
        (Place::Board(key, Section::Triage), _) => triage(boardy, &key.clone(), theme, cx),
        (Place::Board(key, Section::Milestones), _) => milestones(boardy, key, theme),
        (Place::Board(key, Section::Tickets), Layout::Board) => {
            columns(boardy, &key.clone(), cx).into_any_element()
        }
        _ => list(boardy, theme, cx),
    };
    let questions = (boardy.place == Place::Attention && !boardy.questions.is_empty()).then(|| {
        div()
            .flex()
            .flex_none()
            .flex_col()
            .gap_2()
            .px(px(PAD_X))
            .pb_2()
            .child(caption("Open questions", theme))
            .children(
                boardy
                    .questions
                    .iter()
                    .map(|question| asked(boardy, question, theme, cx))
                    .collect::<Vec<_>>(),
            )
    });
    div()
        .size_full()
        .flex()
        .flex_col()
        .min_h_0()
        .child(top)
        .children(creating)
        .children(failed)
        .children(questions)
        .child(div().flex_1().min_h_0().px(px(PAD_X / 2.0)).child(body))
}

fn list(boardy: &mut Boardy, theme: &Theme, cx: &mut Context<Boardy>) -> AnyElement {
    let lines = lines(boardy);
    if lines.is_empty() {
        let said = match &boardy.place {
            Place::Board(key, _) if !boardy.store.read(key) => "Reading the board",
            _ => "Nothing here",
        };
        return empty_state("boardy-empty", said, None, &[], &[], theme, |_, _, _| {})
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
        .into_any_element()
}

fn line_row(boardy: &Boardy, line: &Line, theme: &Theme, cx: &mut Context<Boardy>) -> AnyElement {
    match line {
        Line::Head {
            status: Some(status),
            title,
            count,
            folded,
        } => {
            let (toggled, added) = (status.clone(), status.clone());
            let Some(shown) = kit(status) else {
                return div().into_any_element();
            };
            div()
                .h(px(LINE))
                .flex()
                .items_end()
                .child(
                    group_header(
                        shown,
                        title.clone(),
                        *count,
                        *folded,
                        cx.listener(move |boardy, _: &ClickEvent, window, cx| {
                            boardy.start_creating(added.clone(), window, cx)
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
        } => div()
            .h(px(LINE))
            .flex()
            .items_end()
            .gap_2()
            .px(px(PAD_X / 2.0))
            .pb_2()
            .child(caption(title.clone(), theme))
            .child(
                div()
                    .text_size(px(FONT_SMALL))
                    .text_color(ink(theme, CAPTION_TEXT))
                    .child(count.to_string()),
            )
            .into_any_element(),
        Line::Row { id, at } => match boardy.store.find(id) {
            Some(ticket) => ticket_row(ticket, *at == boardy.at, theme, cx),
            None => div().h(px(LINE)).into_any_element(),
        },
    }
}

fn chips(ticket: &Ticket, theme: &Theme) -> Vec<AnyElement> {
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
            (!ticket.questions.is_empty())
                .then(|| icon_chip(Glyph::Attention, format!("{} open", ticket.questions.len())))
                .map(IntoElement::into_any_element),
        )
        .collect()
}

fn who(ticket: &Ticket, theme: &Theme) -> Option<Div> {
    let assignee = ticket.assignee.as_deref().filter(|who| !who.is_empty())?;
    let letter = assignee
        .chars()
        .next()
        .map(String::from)
        .unwrap_or_default();
    Some(
        div()
            .relative()
            .child(disc(&letter.to_uppercase(), ink(theme, T2)))
            .when(running(ticket), |disc| {
                disc.child(
                    div()
                        .absolute()
                        .right_0()
                        .bottom_0()
                        .size(px(7.0))
                        .rounded_full()
                        .bg(theme.color(ColorToken::StatusLive)),
                )
            }),
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
    ticket: &Ticket,
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
    if let Some(who) = who(ticket, theme) {
        row = row.avatar(who);
    }
    div()
        .h(px(LINE))
        .on_mouse_down(MouseButton::Right, aim(&ticket.id, cx))
        .child(row)
        .into_any_element()
}

fn columns(boardy: &Boardy, key: &str, cx: &mut Context<Boardy>) -> impl IntoElement {
    let shown: Vec<(Status, Vec<&Ticket>)> = boardy
        .store
        .columns(key)
        .into_iter()
        .filter(|(status, tickets)| {
            !tickets.is_empty()
                || matches!(
                    status,
                    Status::Backlog | Status::Todo | Status::Doing | Status::Review | Status::Done
                )
                || (*status == Status::Triage && boardy.flow == Flow::Linear)
        })
        .collect();
    let theme = ActiveTheme::theme(cx);
    let mut at = 0;
    let mut built = Vec::new();
    for (status, tickets) in shown {
        let Some(header) = kit(&status) else {
            continue;
        };
        let closed = matches!(status, Status::Done | Status::Dropped);
        let count = tickets.len();
        let mut cards = Vec::new();
        for (shown_at, ticket) in tickets.into_iter().enumerate() {
            let selected = at == boardy.at;
            at += 1;
            if closed && shown_at >= CLOSED_SHOWN {
                continue;
            }
            cards.push(card(boardy, ticket, selected, &theme, cx));
        }
        if closed && count > CLOSED_SHOWN {
            cards.push(
                note(
                    format!("{} more, open the list to see them", count - CLOSED_SHOWN),
                    &theme,
                )
                .px_2()
                .into_any_element(),
            );
        }
        let added = status.clone();
        built.push(
            board_column(
                ElementId::Name(format!("boardy-column-{}", look::label(&status)).into()),
                ColumnHeader {
                    status: header,
                    title: look::label(&status).to_owned().into(),
                    count,
                },
                cards,
            )
            .on_add(cx.listener(move |boardy, _: &ClickEvent, window, cx| {
                boardy.start_creating(added.clone(), window, cx)
            })),
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

fn card(
    boardy: &Boardy,
    ticket: &Ticket,
    selected: bool,
    theme: &Theme,
    cx: &mut Context<Boardy>,
) -> AnyElement {
    let id = ElementId::Name(ticket.id.clone().into());
    let mut card = board_card(id.clone())
        .id_line(
            div()
                .flex()
                .gap_1()
                .child(ticket.id.clone())
                .children(ticket.parent.clone().map(|parent| format!("› {parent}"))),
        )
        .title(ticket.title.clone())
        .chips(
            look::priority(&ticket.priority)
                .map(|priority| priority_icon(priority).into_any_element())
                .into_iter()
                .chain(chips(ticket, theme)),
        )
        .footer(format!("Created {}", day(&ticket.created)))
        .selected(selected)
        .on_click(opener(&ticket.id, cx));
    if let Some(status) = kit(&ticket.status) {
        card = card.status(status);
    }
    if let Some(who) = who(ticket, theme) {
        card = card.avatar(who);
    }
    flip(
        id,
        &boardy.flips,
        div()
            .flex_none()
            .mb(px(CARD_GAP))
            .on_mouse_down(MouseButton::Right, aim(&ticket.id, cx))
            .child(card),
    )
    .into_any_element()
}

fn triage(boardy: &Boardy, key: &str, theme: &Theme, cx: &mut Context<Boardy>) -> AnyElement {
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
                    let act = |accept: bool| {
                        let (board, id) = (key.to_owned(), request.id.clone());
                        cx.listener(move |boardy: &mut Boardy, _: &ClickEvent, _, cx| {
                            boardy.triage_act(board.clone(), id.clone(), accept, cx)
                        })
                    };
                    div()
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
                        .child(note(
                            format!("{} · {}", request.by, age(&request.at, now)),
                            theme,
                        ))
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

fn milestones(boardy: &Boardy, key: &str, theme: &Theme) -> AnyElement {
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
    let epic_row = |epic: &PlannedEpic| {
        let count = tickets
            .iter()
            .filter(|ticket| ticket.epic.as_deref() == Some(epic.id.as_str()))
            .count();
        let done = epic.status == BoardyPlannedEpicStatus::Done;
        div()
            .flex()
            .gap_2()
            .items_center()
            .child(icon_chip(
                if done { Glyph::Check } else { Glyph::Epic },
                epic.id.clone(),
            ))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .truncate()
                    .text_color(ink(theme, if done { T2 } else { T1 }))
                    .child(epic.title.clone()),
            )
            .child(note(format!("{count} {}", plan.words.ticket), theme))
    };
    let group = |title: String, detail: Option<String>, epics: Vec<&PlannedEpic>| {
        div()
            .flex()
            .flex_col()
            .gap_2()
            .p_3()
            .rounded(px(10.0))
            .bg(ink(theme, 0.03))
            .child(
                div()
                    .flex()
                    .gap_2()
                    .items_center()
                    .child(
                        div()
                            .font_weight(FontWeight::SEMIBOLD)
                            .text_color(ink(theme, T1))
                            .child(title),
                    )
                    .children(detail.map(|detail| note(detail, theme))),
            )
            .children(epics.into_iter().map(epic_row))
    };
    let mut cards: Vec<Div> = plan
        .milestones
        .iter()
        .map(|milestone| {
            let epics = plan
                .epics
                .iter()
                .filter(|epic| epic.milestone.as_deref() == Some(milestone.id.as_str()))
                .collect();
            group(
                format!("{} {}", milestone.id, milestone.title),
                milestone
                    .due
                    .as_deref()
                    .map(|due| format!("due {}", day(due))),
                epics,
            )
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
        cards.push(group(NO_MILESTONE.to_owned(), None, loose));
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
        .child(
            div()
                .flex()
                .flex_col()
                .gap_3()
                .p(px(PAD_X / 2.0))
                .children(cards),
        )
        .into_any_element()
}

fn aging(title: &str, rows: &[Aging], theme: &Theme) -> Option<Div> {
    (!rows.is_empty()).then(|| {
        div()
            .flex()
            .flex_col()
            .gap_1()
            .child(caption(title.to_owned(), theme))
            .children(rows.iter().map(|row| {
                div()
                    .flex()
                    .gap_2()
                    .child(note(row.id.clone(), theme))
                    .child(div().flex_1().min_w_0().truncate().child(row.title.clone()))
                    .child(note(format!("{}d", row.days), theme))
            }))
    })
}

pub(super) fn catch_up(boardy: &Boardy, theme: &Theme, cx: &mut Context<Boardy>) -> Div {
    let reports = boardy.reports.values().map(|report| {
        div()
            .flex()
            .flex_col()
            .gap_2()
            .p_3()
            .rounded(px(10.0))
            .bg(ink(theme, 0.03))
            .child(
                div()
                    .flex()
                    .gap_2()
                    .items_center()
                    .child(
                        div()
                            .font_weight(FontWeight::SEMIBOLD)
                            .child(report.board.clone()),
                    )
                    .child(note(
                        format!(
                            "{} moves, {} finished, {} rounds since {}",
                            report.moves,
                            report.finished.len(),
                            report.rounds,
                            day(&report.since)
                        ),
                        theme,
                    )),
            )
            .children(aging("In review", &report.review, theme))
            .children(aging("Blocked", &report.blocked, theme))
    });
    let questions: Vec<AnyElement> = boardy
        .questions
        .iter()
        .map(|question| asked(boardy, question, theme, cx))
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
                .gap_3()
                .p(px(PAD_X))
                .children(start)
                .child(caption("Since your last look", theme))
                .children(reports)
                .child(caption("Open questions", theme))
                .children(questions),
        ),
    )
}

fn asked(
    boardy: &Boardy,
    question: &AskedQuestion,
    theme: &Theme,
    cx: &mut Context<Boardy>,
) -> AnyElement {
    let title = boardy
        .store
        .find(&question.ticket)
        .map_or_else(String::new, |ticket| ticket.title.clone());
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
        .p_3()
        .rounded(px(10.0))
        .bg(ink(theme, 0.03))
        .child(
            div()
                .id(SharedString::from(format!(
                    "q-{}-{}",
                    question.ticket, question.id
                )))
                .cursor_pointer()
                .on_click(opener(&question.ticket, cx))
                .child(note(
                    format!("{} · Q{} · {title}", question.ticket, question.id),
                    theme,
                )),
        )
        .child(
            div()
                .font_weight(FontWeight::SEMIBOLD)
                .text_color(ink(theme, T1))
                .child(question.tldr.clone()),
        )
        .child(
            div()
                .text_color(ink(theme, T2))
                .child(question.eli5.clone()),
        )
        .child(div().flex().flex_wrap().gap_2().children(options))
        .into_any_element()
}
