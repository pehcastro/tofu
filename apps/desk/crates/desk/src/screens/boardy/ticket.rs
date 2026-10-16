use chrono::Utc;
use desk_core::boardy::{Speaker, spoken};
use desk_core::protocol::BoardyEventKind as Kind;
use desk_core::protocol::boardy::{Event, Ticket};
use desk_ui::components::breadcrumb::{OnCrumb, breadcrumb};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::caption;
use desk_ui::components::chat::document;
use desk_ui::components::dot_chip::{dot_chip, icon_chip};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::paint::ink;
use desk_ui::components::priority_icon::priority_icon;
use desk_ui::components::properties::properties_panel;
use desk_ui::components::scroll::ScrollArea;
use desk_ui::components::size::{CAPTION_TEXT, FONT_SMALL, FONT_TITLE, T1, T2};
use desk_ui::components::status_icon::status_icon;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, ClickEvent, Context, Div, FontWeight, SharedString, div, prelude::*, px,
    rgb_to_hsla,
};

use super::look::{self, age, day, disc, kit, noun};
use super::views::opener;
use super::{Boardy, Opened, Place};
use crate::screens::frame::note;

const PAD: f32 = 24.0;
const COLUMN: f32 = 800.0;
const TITLE_SCALE: f32 = 1.35;
const GAP: f32 = 18.0;
const BAR: f32 = 44.0;
const ICON: f32 = 14.0;
const BUBBLE_RADIUS: f32 = 10.0;
const BUBBLE_FILL: f32 = 0.035;

pub(super) fn page(boardy: &Boardy, theme: &Theme, cx: &mut Context<Boardy>) -> Div {
    let Place::Ticket(id) = &boardy.place else {
        return div();
    };
    let back = |cx: &mut Context<Boardy>| -> OnCrumb {
        Box::new(cx.listener(|boardy: &mut Boardy, _: &ClickEvent, _, cx| {
            boardy.place = boardy.back.clone();
            boardy.opened = None;
            cx.notify();
        }))
    };
    let opened = boardy
        .opened
        .as_ref()
        .filter(|opened| opened.answer.ticket.id == *id);
    let title = opened.map_or_else(String::new, |opened| opened.answer.ticket.title.clone());
    let board = id.split_once('-').map_or(id.as_str(), |(board, _)| board);
    let top = div()
        .flex()
        .flex_none()
        .items_center()
        .gap_2()
        .h(px(BAR))
        .px(px(PAD / 2.0))
        .child(breadcrumb([
            (board.to_owned(), Some(back(cx))),
            (noun(&boardy.store.words.ticket, true), Some(back(cx))),
            (format!("{id} {title}"), None),
        ]))
        .child(div().flex_1())
        .child(note(
            format!(
                "{} / {}",
                boardy.at.saturating_add(1).min(boardy.trail.len()),
                boardy.trail.len()
            ),
            theme,
        ))
        .child(
            button("boardy-down", "J", None, ButtonKind::Text, theme).on_click(
                cx.listener(|boardy: &mut Boardy, _: &ClickEvent, _, cx| boardy.step(1, cx)),
            ),
        )
        .child(
            button("boardy-up", "K", None, ButtonKind::Text, theme).on_click(
                cx.listener(|boardy: &mut Boardy, _: &ClickEvent, _, cx| boardy.step(-1, cx)),
            ),
        );
    let Some(opened) = opened else {
        let said = boardy
            .failed
            .clone()
            .unwrap_or_else(|| format!("Reading {id}").into());
        return div()
            .size_full()
            .flex()
            .flex_col()
            .child(top)
            .child(div().p(px(PAD)).child(note(said, theme)));
    };
    div()
        .size_full()
        .flex()
        .flex_col()
        .min_h_0()
        .child(top)
        .child(
            div()
                .flex_1()
                .min_h_0()
                .flex()
                .gap(px(PAD))
                .px(px(PAD))
                .child(
                    div().flex_1().min_w_0().h_full().child(
                        ScrollArea::new("boardy-ticket").child(body(boardy, opened, theme, cx)),
                    ),
                )
                .child(
                    ScrollArea::new("boardy-properties")
                        .child(properties(boardy, opened, theme, cx)),
                ),
        )
}

fn body(boardy: &Boardy, opened: &Opened, theme: &Theme, cx: &mut Context<Boardy>) -> Div {
    let ticket = &opened.answer.ticket;
    let revisions = ticket.acceptance.len();
    let subs: Vec<&Ticket> = boardy
        .store
        .tickets(ticket.id.split_once('-').map_or("", |(board, _)| board))
        .iter()
        .filter(|sub| sub.parent.as_deref() == Some(ticket.id.as_str()))
        .collect();
    div()
        .max_w(px(COLUMN))
        .flex()
        .flex_col()
        .gap(px(GAP))
        .pb(px(PAD))
        .child(
            div()
                .text_size(px(FONT_TITLE * TITLE_SCALE))
                .font_weight(FontWeight::BOLD)
                .text_color(ink(theme, T1))
                .child(ticket.title.clone()),
        )
        .child(document(
            SharedString::from(format!("{}-body", ticket.id)),
            &opened.body,
            &[],
            theme,
        ))
        .child(section(
            format!(
                "Acceptance{}",
                if revisions > 1 {
                    format!(", revision {revisions} in force")
                } else {
                    String::new()
                }
            ),
            theme,
        ))
        .child(document(
            SharedString::from(format!("{}-acceptance", ticket.id)),
            &opened.acceptance,
            &[],
            theme,
        ))
        .children((!ticket.questions.is_empty()).then(|| {
            div()
                .flex()
                .flex_col()
                .gap_2()
                .child(section("Questions".to_owned(), theme))
                .children(ticket.questions.iter().map(|question| {
                    div()
                        .flex()
                        .flex_col()
                        .gap_1()
                        .child(
                            div()
                                .font_weight(FontWeight::SEMIBOLD)
                                .child(format!("Q{} {}", question.id, question.tldr)),
                        )
                        .child(
                            div()
                                .text_color(ink(theme, T2))
                                .child(question.eli5.clone()),
                        )
                        .children(
                            question
                                .options
                                .iter()
                                .enumerate()
                                .map(|(at, option)| note(format!("{}. {option}", at + 1), theme)),
                        )
                }))
        }))
        .children((!subs.is_empty()).then(|| {
            div()
                .flex()
                .flex_col()
                .gap_1()
                .child(section(format!("Sub-{}", boardy.store.words.ticket), theme))
                .children(subs.into_iter().map(|sub| linked(sub, theme, cx)))
        }))
        .child(section("Activity".to_owned(), theme))
        .children(
            opened
                .answer
                .events
                .iter()
                .zip(&opened.said)
                .enumerate()
                .map(|(at, (event, said))| activity(at, event, said.as_deref(), theme)),
        )
        .child(
            div()
                .flex()
                .items_end()
                .gap_2()
                .p_2()
                .rounded(px(BUBBLE_RADIUS))
                .bg(ink(theme, BUBBLE_FILL))
                .child(div().flex_1().min_w_0().child(boardy.composer.clone()))
                .child(note("Enter sends to boardy log", theme)),
        )
}

fn section(title: String, theme: &Theme) -> Div {
    div().pt_2().child(caption(title, theme))
}

fn linked(ticket: &Ticket, theme: &Theme, cx: &Context<Boardy>) -> AnyElement {
    div()
        .id(SharedString::from(format!("link-{}", ticket.id)))
        .flex()
        .items_center()
        .gap_2()
        .cursor_pointer()
        .on_click(opener(&ticket.id, cx))
        .children(kit(&ticket.status).map(|status| status_icon(status, px(ICON))))
        .child(note(ticket.id.clone(), theme))
        .child(div().min_w_0().truncate().child(ticket.title.clone()))
        .into_any_element()
}

fn activity(
    at: usize,
    event: &Event,
    said: Option<&[desk_ui::components::chat::Block]>,
    theme: &Theme,
) -> Div {
    let actor = event.actor.as_deref().unwrap_or_default();
    let speaker = Speaker::of(actor);
    let (name, color) = match speaker {
        Speaker::You => ("you".to_owned(), theme.color(ColorToken::StatusAccent)),
        Speaker::Lead => (
            "orchestrator".to_owned(),
            theme.color(ColorToken::StatusWarn),
        ),
        Speaker::Agent => (
            event.agent.clone().unwrap_or_else(|| "agent".to_owned()),
            theme.color(ColorToken::GitAdded),
        ),
    };
    let when = age(&event.at, Utc::now());
    let face = disc(&name.get(..1).unwrap_or_default().to_uppercase(), color);
    let Some(said) = said.filter(|_| spoken(event)) else {
        return div()
            .flex()
            .items_center()
            .gap_2()
            .text_size(px(FONT_SMALL))
            .text_color(ink(theme, CAPTION_TEXT))
            .child(face)
            .child(format!("{name} {} · {when}", did(event)));
    };
    div().flex().gap_2().child(face).child(
        div()
            .flex_1()
            .min_w_0()
            .flex()
            .flex_col()
            .gap_1()
            .p_3()
            .rounded(px(BUBBLE_RADIUS))
            .bg(ink(theme, BUBBLE_FILL))
            .child(note(format!("{name} {} · {when}", did(event)), theme))
            .child(document(
                SharedString::from(format!("said-{at}")),
                said,
                &[],
                theme,
            )),
    )
}

fn did(event: &Event) -> String {
    let text = event.text.as_deref().unwrap_or_default();
    let to = event.to.clone().map(String::from).unwrap_or_default();
    let from = event.from.clone().map(String::from).unwrap_or_default();
    match &event.kind {
        Kind::Created => format!("created it in {to}"),
        Kind::Moved if text.is_empty() => format!("moved it {from} → {to}"),
        Kind::Moved => format!("moved it {from} → {to}: {text}"),
        Kind::Commented => "commented".to_owned(),
        Kind::Asked => "asked".to_owned(),
        Kind::Answered => "answered".to_owned(),
        Kind::Spawned => "spawned an agent".to_owned(),
        Kind::Reported => "reported".to_owned(),
        Kind::Assigned => format!("assigned it to {text}"),
        Kind::Rule | Kind::Waiting => format!("ran a rule: {text}"),
        Kind::Requested => format!("requested {text}"),
        Kind::Accepted => format!("accepted {text}"),
        Kind::RequestDropped => format!("dropped the request {text}"),
        Kind::Handed => format!("handed it {text}"),
        Kind::Widened => format!("widened its owns {text}"),
        Kind::Managers => format!("changed managers {text}"),
        Kind::Unknown(raw) => format!("{raw} {text}"),
    }
}

fn properties(
    boardy: &Boardy,
    opened: &Opened,
    theme: &Theme,
    cx: &Context<Boardy>,
) -> impl IntoElement {
    let ticket = &opened.answer.ticket;
    let actual = &opened.answer.actual;
    let words = &boardy.store.words;
    let text = |value: String| div().child(value).into_any_element();
    let aimed = ticket.id.clone();
    let status = div()
        .id("boardy-status")
        .flex()
        .items_center()
        .gap_1p5()
        .cursor_pointer()
        .children(kit(&ticket.status).map(|status| status_icon(status, px(ICON))))
        .child(look::label(&ticket.status).to_owned())
        .on_click(
            cx.listener(move |boardy: &mut Boardy, event: &ClickEvent, _, cx| {
                boardy.aimed = Some(aimed.clone());
                boardy.menu_at = Some(event.position());
                cx.notify();
            }),
        )
        .into_any_element();
    let priority = div()
        .flex()
        .items_center()
        .gap_1p5()
        .children(look::priority(&ticket.priority).map(|priority| {
            div()
                .flex()
                .gap_1p5()
                .child(priority_icon(priority))
                .child(priority.label())
        }))
        .into_any_element();
    let label = rgb_to_hsla(theme.color(ColorToken::StatusAccent));
    let ids = |title: &str, ids: &[String]| {
        ids.iter()
            .map(|id| {
                let shown = boardy.store.find(id);
                (
                    SharedString::from(title.to_owned()),
                    match shown {
                        Some(found) => linked(found, theme, cx),
                        None => note(id.clone(), theme).into_any_element(),
                    },
                )
            })
            .collect::<Vec<_>>()
    };
    let mut relations = ids("Depends on", &ticket.depends);
    relations.extend(ids("Blocks", &ticket.blocks));
    relations.extend(ids("Relates", &ticket.relates));
    relations.extend(ids("Duplicates", &ticket.duplicates));
    relations.extend(ids("Parent", ticket.parent.as_slice()));
    properties_panel()
        .section(
            "Properties",
            [
                ("Status".into(), status),
                ("Priority".into(), priority),
                ("Type".into(), text(String::from(ticket.r#type.clone()))),
                (
                    "Assignee".into(),
                    text(
                        ticket
                            .assignee
                            .clone()
                            .unwrap_or_else(|| "nobody".to_owned()),
                    ),
                ),
                (
                    noun(&words.epic, false).into(),
                    match &ticket.epic {
                        Some(epic) => icon_chip(Glyph::Epic, epic.clone()).into_any_element(),
                        None => note("none", theme).into_any_element(),
                    },
                ),
                (
                    noun(&words.sprint, false).into(),
                    text(ticket.sprint.clone().unwrap_or_else(|| "none".to_owned())),
                ),
                (
                    noun(&words.points, false).into(),
                    text(
                        ticket
                            .points
                            .map_or_else(|| "none".to_owned(), |points| points.to_string()),
                    ),
                ),
                ("Created".into(), text(day(&ticket.created))),
                ("Updated".into(), text(day(&ticket.updated))),
                (
                    "Worked".into(),
                    text(format!("{} rounds, {} min", actual.rounds, actual.minutes)),
                ),
            ],
        )
        .section(
            "Labels",
            [(
                SharedString::default(),
                div()
                    .flex()
                    .flex_wrap()
                    .gap_1()
                    .children(
                        ticket
                            .labels
                            .iter()
                            .map(|name| dot_chip(name.clone(), label)),
                    )
                    .when(ticket.labels.is_empty(), |row| {
                        row.child(note("none", theme))
                    })
                    .into_any_element(),
            )],
        )
        .section("Related", relations)
        .section(
            "Owns",
            ticket
                .owns
                .iter()
                .map(|glob| {
                    (
                        SharedString::default(),
                        note(glob.clone(), theme).into_any_element(),
                    )
                })
                .collect::<Vec<_>>(),
        )
}
