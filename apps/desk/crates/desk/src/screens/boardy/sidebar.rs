use std::time::Instant;

use desk_core::boardy::running;
use desk_core::protocol::boardy::Ticket;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::nav_sidebar::NavSidebar;
use desk_ui::components::paint::tint;
use desk_ui::components::scroll::ScrollArea;
use desk_ui::components::size::SEPARATOR_INSET;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::ColorToken;
use gpui::{ClickEvent, Context, Div, div, prelude::*, px};

use super::look::{kit, noun};
use super::views::opener;
use super::{Boardy, MS_PER_SECOND, NEWEST, Place, Section};

const RULE: f32 = 0.35;

pub(super) fn sidebar(boardy: &Boardy, cx: &mut Context<Boardy>) -> Div {
    let theme = ActiveTheme::theme(cx);
    let store = &boardy.store;
    let words = &store.words;
    let go = |place: Place| {
        cx.listener(move |boardy: &mut Boardy, _: &ClickEvent, window, cx| {
            let clicked = Instant::now();
            let shown = format!("{place:?}");
            boardy.go(place.clone(), cx);
            window.on_next_frame(move |window, _| {
                window.on_next_frame(move |_, _| {
                    eprintln!(
                        "desk: boardy: shows {shown} in {:.2}ms",
                        clicked.elapsed().as_secs_f64() * MS_PER_SECOND
                    );
                });
            });
        })
    };
    let attention = store.attention();
    let questions = boardy.questions.len();
    let nav = NavSidebar::new("boardy-nav")
        .item(
            Glyph::Catchup,
            "Catch up",
            (questions > 0).then_some(questions),
            false,
            boardy.place == Place::CatchUp,
            go(Place::CatchUp),
        )
        .item(
            Glyph::Attention,
            "Need attention",
            Some(attention.len()),
            false,
            boardy.place == Place::Attention,
            go(Place::Attention),
        );
    let mut boards = NavSidebar::new("boardy-boards");
    for summary in &store.boards {
        let key = summary.key.clone();
        let tickets = store.tickets(&key);
        let count =
            |pick: fn(&Ticket) -> bool| tickets.iter().filter(|ticket| pick(ticket)).count();
        let at = |section: Section| Place::Board(key.clone(), section);
        let on = |section: Section| boardy.place == at(section);
        let triage = boardy.triage.get(&key).map(Vec::len);
        let children = NavSidebar::new(format!("boardy-nav-{key}"))
            .item(
                Glyph::Inbox,
                noun(&words.ticket, true),
                Some(if store.read(&key) {
                    tickets.len()
                } else {
                    usize::try_from(summary.tickets).unwrap_or_default()
                }),
                summary.live > 0,
                on(Section::Tickets),
                go(at(Section::Tickets)),
            )
            .item(
                Glyph::Milestone,
                super::views::MILESTONES,
                boardy.plans.get(&key).map(|plan| plan.milestones.len()),
                false,
                on(Section::Milestones),
                go(at(Section::Milestones)),
            )
            .item(
                Glyph::Epic,
                noun(&words.epic, true),
                Some(count(|ticket| ticket.epic.is_some())),
                false,
                on(Section::Epics),
                go(at(Section::Epics)),
            )
            .item(
                Glyph::Sprint,
                noun(&words.sprint, true),
                Some(count(|ticket| ticket.sprint.is_some())),
                false,
                on(Section::Sprints),
                go(at(Section::Sprints)),
            )
            .item(
                Glyph::Filter,
                "Triage",
                triage,
                false,
                on(Section::Triage),
                go(at(Section::Triage)),
            );
        boards = boards.group(key.clone(), false, children);
    }
    let nav = nav.group(format!("Your {}s", words.board), false, boards);
    let mut newest: Vec<&Ticket> = store
        .boards
        .iter()
        .flat_map(|summary| store.tickets(&summary.key).iter().take(NEWEST))
        .collect();
    newest.sort_by(|a, b| b.created.cmp(&a.created));
    newest.truncate(NEWEST);
    let mut recent = NavSidebar::new("boardy-recent");
    for ticket in newest {
        let Some(status) = kit(&ticket.status) else {
            continue;
        };
        recent = recent.item(
            status,
            format!("{} {}", ticket.id, ticket.title),
            None,
            running(ticket),
            boardy.place == Place::Ticket(ticket.id.clone()),
            opener(&ticket.id, cx),
        );
    }
    let nav = nav.group(format!("Newest {}", words.ticket), false, recent);
    div()
        .flex_none()
        .h_full()
        .border_r_1()
        .border_color(tint(theme.color(ColorToken::Shadow), RULE))
        .px(px(SEPARATOR_INSET / 2.0))
        .py_2()
        .child(ScrollArea::new("boardy-sidebar").child(nav))
}
