use chrono::{DateTime, Local, Utc};
use desk_core::boardy::{Speaker, report_echoes, spoken};
use desk_core::protocol::boardy::{Event, Row, Ticket};
use desk_core::protocol::{
    BoardyAuthorKind as AuthorKind, BoardyEventKind as Kind, BoardyStatus as Status,
    BoardyTicketType as TicketType,
};
use desk_ui::components::avatar::{Agent, AgentKind, AgentStatus, AvatarSize, avatar, kind_avatar};
use desk_ui::components::breadcrumb::{OnCrumb, breadcrumb};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::caption;
use desk_ui::components::chat::{Block, document, markdown};
use desk_ui::components::composer::Composer;
use desk_ui::components::dot_chip::{dot_chip, icon_chip};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::hovercard::{Subject, hover, link_card, link_key};
use desk_ui::components::paint::{glyph, ink};
use desk_ui::components::pill_tabs::PillTabs;
use desk_ui::components::priority_icon::priority_icon;
use desk_ui::components::properties::properties_panel;
use desk_ui::components::scroll::ScrollArea;
use desk_ui::components::size::{CAPTION_TEXT, FONT_SMALL, FONT_TITLE, HOVER, T1, T2};
use desk_ui::components::status_icon::status_icon;
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::theme::{ColorToken, Theme, WordToken};
use gpui::{
    AnyElement, App, Bounds, ClickEvent, ClipboardItem, Context, CursorStyle, Div, Entity,
    FontWeight, MouseButton, MouseMoveEvent, Pixels, Rgba, SharedString, Window, canvas, div,
    prelude::*, px, rgb_to_hsla,
};
use std::cell::Cell;
use std::collections::{BTreeMap, BTreeSet};
use std::rc::Rc;
use std::time::Instant;

use super::look::{self, age, day, kit, noun};
use super::refs::{Ref, To};
use super::views::opener;
use super::{Boardy, Opened, Origin, Place, Section};
use crate::screens::frame::note;

const PAD: f32 = 24.0;
#[cfg(feature = "module-editor")]
const EDITOR: &str = "editor";
const SIDE_SHARE: f32 = 0.025;
const SIDE_LEAST: f32 = 16.0;
const SIDE_MOST: f32 = 40.0;
const DIVIDER_GAP: f32 = 24.0;
const PANEL_PAD: f32 = 16.0;
const CHIP_RADIUS: f32 = 6.0;
const CHIP_HEIGHT: f32 = 22.0;
const COLUMN: f32 = 760.0;
const PANEL: f32 = 280.0;
const PANEL_LEAST: f32 = 220.0;
const PANEL_MOST: f32 = 480.0;
const HANDLE: f32 = 6.0;
const STACK_BELOW: f32 = 720.0;
const TITLE_SCALE: f32 = 1.35;
const GAP: f32 = 18.0;
const BAR: f32 = 44.0;
const ICON: f32 = 14.0;
const BUBBLE_RADIUS: f32 = 10.0;
const BUBBLE_FILL: f32 = 0.035;
const REPLY_INDENT: f32 = 48.0;
const FOLD_LINES: usize = 6;
const FOLD_CHARS: usize = 480;
const PERSON: &str = "you";
const PERSON_ACTOR: &str = "person";
const HOOK_ACTOR: &str = "hook:";
const SERVE_ACTOR: &str = "tofu serve";
const CLAUDE_CODE: &str = "claude-code";
const CLAUDE_CODE_SESSION: &str = "claude-code#";
const ORCHESTRATOR: &str = "orchestrator";
const LEAD: &str = "lead";
const UNKNOWN: &str = "unknown";
const SHORT_SESSION: usize = 8;
const ALIAS_MOST: usize = 24;
const PM: &str = "PM";
const FENCE: &str = "```";
const PARAGRAPH: &str = "\n\n";
const NO_ANSWER: &str = "did not answer";
const EVIDENCE_KINDS: [&str; 2] = ["evidence", "logged"];
const FOLD_SECONDS: i64 = 2;
const EXACT_TIME: &str = "%a %b %-d %Y, %H:%M:%S";
const REPLY_GAP: f32 = 8.0;
const KEPT_GAP: f32 = 8.0;

pub(super) enum Said {
    Quiet,
    Echo,
    Spoken {
        full: Vec<Block>,
        short: Option<Vec<Block>>,
        refs: Vec<Ref>,
        verdict: Option<Verdict>,
    },
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(super) enum Verdict {
    Verified,
    NotVerified,
    Inconclusive,
}

impl Verdict {
    const ALL: [Verdict; 3] = [
        Verdict::NotVerified,
        Verdict::Verified,
        Verdict::Inconclusive,
    ];

    fn word(self) -> &'static str {
        match self {
            Verdict::Verified => "VERIFIED",
            Verdict::NotVerified => "NOT VERIFIED",
            Verdict::Inconclusive => "INCONCLUSIVE",
        }
    }

    fn label(self) -> &'static str {
        match self {
            Verdict::Verified => "Verified",
            Verdict::NotVerified => "Not verified",
            Verdict::Inconclusive => "Inconclusive",
        }
    }

    fn color(self, theme: &Theme) -> Rgba {
        theme.color(match self {
            Verdict::Verified => ColorToken::GitAdded,
            Verdict::NotVerified => ColorToken::StatusDanger,
            Verdict::Inconclusive => ColorToken::StatusWarn,
        })
    }
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub(super) enum Tab {
    #[default]
    Conversation,
    History,
    Evidence,
}

impl Tab {
    const ALL: [Tab; 3] = [Tab::Conversation, Tab::History, Tab::Evidence];

    fn label(self) -> &'static str {
        match self {
            Tab::Conversation => "Conversation",
            Tab::History => "History",
            Tab::Evidence => "Evidence",
        }
    }
}

pub(super) enum Line {
    Said { at: usize, moved: Option<usize> },
    Handoff { at: usize },
    Asked { at: usize, answer: Option<usize> },
    Unanswered { at: usize },
    Kept { at: usize },
    Evidence { at: usize },
}

impl Line {
    fn tab(&self) -> Tab {
        match self {
            Line::Said { .. }
            | Line::Handoff { .. }
            | Line::Asked { .. }
            | Line::Unanswered { .. } => Tab::Conversation,
            Line::Kept { .. } => Tab::History,
            Line::Evidence { .. } => Tab::Evidence,
        }
    }
}

pub(super) fn said(events: &[Event], found: impl Fn(&str) -> Vec<Ref>) -> Vec<Said> {
    events
        .iter()
        .zip(report_echoes(events))
        .map(|(event, echo)| {
            let (verdict, text) = verdict(event.text.as_deref().unwrap_or_default());
            let text = plain_headings(text);
            match (echo, spoken(event)) {
                (true, _) => Said::Echo,
                (false, false) => Said::Quiet,
                (false, true) => Said::Spoken {
                    full: markdown(&text),
                    short: folded(&text).map(|short| markdown(&short)),
                    refs: found(&text),
                    verdict,
                },
            }
        })
        .collect()
}

fn verdict(text: &str) -> (Option<Verdict>, &str) {
    let bare = text.trim_start().trim_start_matches(['#', '*', ' ']);
    Verdict::ALL
        .into_iter()
        .find_map(|verdict| {
            bare.strip_prefix(verdict.word()).map(|rest| {
                let rest = rest.trim_start_matches([':', '.', ',', '*', ' ']);
                (Some(verdict), rest.trim_start())
            })
        })
        .unwrap_or((None, text))
}

fn plain_headings(text: &str) -> String {
    let mut fenced = false;
    text.lines()
        .map(|line| {
            let lead = line.trim_start();
            fenced ^= lead.starts_with(FENCE);
            let title = lead
                .strip_prefix('#')
                .map(|rest| rest.trim_start_matches('#'))
                .and_then(|rest| rest.strip_prefix(' '))
                .map(str::trim)
                .filter(|title| !fenced && !title.is_empty());
            title.map_or_else(|| line.to_owned(), |title| format!("**{title}**"))
        })
        .collect::<Vec<_>>()
        .join("\n")
}

fn folded(text: &str) -> Option<String> {
    let text = text.trim();
    let paragraph = text.split(PARAGRAPH).next().unwrap_or_default();
    let head = paragraph
        .lines()
        .take(FOLD_LINES)
        .collect::<Vec<_>>()
        .join("\n");
    let shown: String = head.chars().take(FOLD_CHARS).collect();
    (shown.len() < text.len()).then(|| format!("{}…", shown.trim_end()))
}

pub(super) fn thread(events: &[Event], said: &[Said]) -> Vec<Line> {
    let mut answered = BTreeSet::new();
    let mut lines: Vec<Line> = Vec::new();
    for (at, (event, spoken)) in events.iter().zip(said).enumerate() {
        let quiet = matches!(spoken, Said::Quiet);
        let line = match &event.kind {
            _ if matches!(spoken, Said::Echo) => continue,
            Kind::Spawned => Line::Handoff { at },
            Kind::Asked => {
                let answer = events
                    .iter()
                    .enumerate()
                    .skip(at.saturating_add(1))
                    .find(|(later, reply)| {
                        reply.kind == Kind::Answered && !answered.contains(later)
                    })
                    .map(|(later, _)| later);
                answered.extend(answer);
                Line::Asked { at, answer }
            }
            Kind::Answered if answered.contains(&at) => continue,
            Kind::Answered if unanswered(event) => Line::Unanswered { at },
            Kind::Commented | Kind::Unknown(_) if evidence(event) => match quiet {
                true => continue,
                false => Line::Evidence { at },
            },
            Kind::Commented | Kind::Messaged | Kind::Reported | Kind::Answered => match quiet {
                true => continue,
                false => Line::Said { at, moved: None },
            },
            Kind::Moved => {
                let folded = lines.iter_mut().rev().find_map(|line| match line {
                    Line::Said { at: by, moved } if moved.is_none() => events
                        .get(*by)
                        .filter(|entry| voice(entry) == voice(event) && soon(entry, event))
                        .map(|_| moved),
                    _ => None,
                });
                if let Some(moved) = folded {
                    *moved = Some(at);
                }
                Line::Kept { at }
            }
            Kind::Created
            | Kind::Rule
            | Kind::Waiting
            | Kind::Requested
            | Kind::Accepted
            | Kind::RequestDropped
            | Kind::Handed
            | Kind::Widened
            | Kind::Managers
            | Kind::Assigned
            | Kind::Hook
            | Kind::HookFailed
            | Kind::Unknown(_) => Line::Kept { at },
        };
        lines.push(line);
    }
    lines
}

fn unanswered(event: &Event) -> bool {
    event
        .text
        .as_deref()
        .is_some_and(|text| text.contains(NO_ANSWER))
}

fn evidence(event: &Event) -> bool {
    match &event.kind {
        Kind::Unknown(raw) => EVIDENCE_KINDS.contains(&raw.as_str()),
        Kind::Commented => match &event.author {
            Some(author) => author.kind == AuthorKind::SubAgent,
            None => Speaker::of(event.actor.as_deref().unwrap_or_default()) == Speaker::Agent,
        },
        _ => false,
    }
}

fn voice(event: &Event) -> &str {
    event
        .author
        .as_ref()
        .map(|author| author.name.as_str())
        .or(event.actor.as_deref())
        .unwrap_or_default()
}

fn soon(entry: &Event, then: &Event) -> bool {
    let parsed = |event: &Event| DateTime::parse_from_rfc3339(&event.at).ok();
    parsed(entry)
        .zip(parsed(then))
        .is_some_and(|(entry, then)| {
            (0..=FOLD_SECONDS).contains(&then.signed_duration_since(entry).num_seconds())
        })
}

pub(super) fn page(
    boardy: &Boardy,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<Boardy>,
) -> Div {
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
        .filter(|opened| opened.ticket.id == *id);
    let title = opened.map_or_else(String::new, |opened| opened.ticket.title.clone());
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
    shown(boardy, opened, top, theme, window, cx)
}

pub(super) fn request_page(
    boardy: &Boardy,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<Boardy>,
) -> Div {
    let Some(opened) = boardy.opened.as_ref() else {
        return div();
    };
    let Origin::Request { board, id, by, at } = &opened.origin else {
        return div();
    };
    let chip = asker(
        "boardy-request-by",
        by,
        &format!("· {}", age(at, Utc::now())),
        boardy,
        theme,
        window,
        cx,
    );
    let back = || -> OnCrumb {
        Box::new(cx.listener(|boardy: &mut Boardy, _: &ClickEvent, _, cx| {
            boardy.opened = None;
            cx.notify();
        }))
    };
    let act = |accept: bool| {
        let (board, id) = (board.clone(), id.clone());
        cx.listener(move |boardy: &mut Boardy, _: &ClickEvent, _, cx| {
            boardy.triage_act(board.clone(), id.clone(), accept, cx);
        })
    };
    let top = div()
        .flex()
        .flex_none()
        .items_center()
        .gap_2()
        .h(px(BAR))
        .px(px(PAD / 2.0))
        .child(breadcrumb([
            (board.clone(), Some(back())),
            ("Triage".to_owned(), Some(back())),
            (format!("{id} {}", opened.ticket.title), None),
        ]))
        .child(div().flex_1())
        .child(chip)
        .child(
            button(
                "boardy-request-accept",
                "Accept",
                Some(Glyph::Check),
                ButtonKind::Plain,
                theme,
            )
            .on_click(act(true)),
        )
        .child(
            button("boardy-request-drop", "Drop", None, ButtonKind::Text, theme)
                .on_click(act(false)),
        );
    shown(boardy, opened, top, theme, window, cx)
}

fn shown(
    boardy: &Boardy,
    opened: &Opened,
    top: Div,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<Boardy>,
) -> Div {
    let side = (f32::from(window.viewport_size().width) * SIDE_SHARE).clamp(SIDE_LEAST, SIDE_MOST);
    let panel = properties(boardy, opened, theme, window, cx).into_any_element();
    div()
        .size_full()
        .flex()
        .flex_col()
        .min_h_0()
        .child(top)
        .child(Split {
            width: boardy.panel.clone(),
            body: div()
                .w_full()
                .flex()
                .justify_center()
                .py(px(PAD))
                .pl(px(side))
                .pr(px(side.max(DIVIDER_GAP)))
                .child(body(boardy, opened, theme, window, cx))
                .into_any_element(),
            panel,
            side,
            line: theme.color(ColorToken::Separator),
            lit: theme.color(ColorToken::FocusRing),
        })
}

pub(super) struct Width {
    px: f32,
    dragging: bool,
    right: Rc<Cell<Pixels>>,
    stacked: Rc<Cell<bool>>,
    moved: Option<Instant>,
}

impl Default for Width {
    fn default() -> Self {
        Width {
            px: PANEL,
            dragging: false,
            right: Rc::default(),
            stacked: Rc::default(),
            moved: None,
        }
    }
}

#[derive(IntoElement)]
struct Split {
    width: Entity<Width>,
    body: AnyElement,
    panel: AnyElement,
    side: f32,
    line: Rgba,
    lit: Rgba,
}

impl RenderOnce for Split {
    fn render(self, _: &mut Window, cx: &mut App) -> impl IntoElement {
        let width = self.width;
        if let Some(moved) = width.update(cx, |width, _| width.moved.take()) {
            eprintln!(
                "desk: boardy: panel drag frame {:.2}",
                moved.elapsed().as_secs_f64() * 1000.0
            );
        }
        let (shown, right, stacked) = {
            let held = width.read(cx);
            (held.px, held.right.clone(), held.stacked.clone())
        };
        let was = stacked.get();
        let measure = canvas(
            move |bounds, window, _| {
                right.set(bounds.right());
                let narrow = f32::from(bounds.size.width) < STACK_BELOW;
                if narrow != stacked.get() {
                    stacked.set(narrow);
                    window.refresh();
                }
            },
            |_, (), _, _| {},
        )
        .absolute()
        .size_full();
        if was {
            return div()
                .flex_1()
                .min_h_0()
                .relative()
                .child(measure)
                .child(
                    ScrollArea::new("boardy-ticket").child(self.body).child(
                        div()
                            .pb(px(PAD))
                            .pl(px(self.side))
                            .pr(px(PAD))
                            .child(self.panel),
                    ),
                )
                .into_any_element();
        }
        let (moving, lifting, pressing) = (width.clone(), width.clone(), width);
        div()
            .flex_1()
            .min_h_0()
            .flex()
            .relative()
            .on_mouse_move(move |event: &MouseMoveEvent, _, cx| {
                moving.update(cx, |width, cx| {
                    if !width.dragging || event.pressed_button != Some(MouseButton::Left) {
                        width.dragging = false;
                        return;
                    }
                    let to = f32::from(width.right.get() - event.position.x);
                    width.px = to.clamp(PANEL_LEAST, PANEL_MOST);
                    width.moved = Some(Instant::now());
                    cx.notify();
                });
            })
            .on_mouse_up(MouseButton::Left, move |_, _, cx| {
                lifting.update(cx, |width, _| width.dragging = false);
            })
            .child(measure)
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .h_full()
                    .child(ScrollArea::new("boardy-ticket").child(self.body)),
            )
            .child(
                div()
                    .id("boardy-panel-edge")
                    .group("boardy-panel-edge")
                    .flex()
                    .flex_none()
                    .justify_center()
                    .w(px(HANDLE))
                    .h_full()
                    .cursor(CursorStyle::ResizeLeftRight)
                    .child(
                        div()
                            .w(px(1.0))
                            .h_full()
                            .bg(self.line)
                            .group_hover("boardy-panel-edge", move |mark| mark.bg(self.lit)),
                    )
                    .on_mouse_down(MouseButton::Left, move |_, _, cx| {
                        cx.stop_propagation();
                        pressing.update(cx, |width, _| width.dragging = true);
                    }),
            )
            .child(
                div().flex_none().w(px(shown - HANDLE)).h_full().child(
                    ScrollArea::new("boardy-properties").child(
                        div()
                            .py(px(PAD))
                            .pl(px(PANEL_PAD))
                            .pr(px(PAD))
                            .child(self.panel),
                    ),
                ),
            )
            .into_any_element()
    }
}

fn body(
    boardy: &Boardy,
    opened: &Opened,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<Boardy>,
) -> Div {
    let ticket = &opened.ticket;
    let revisions = ticket.acceptance.len();
    let read = div()
        .w_full()
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
        .children(mentions("body", &opened.body_refs, boardy, theme, cx));
    let Origin::Ticket(_) = opened.origin else {
        return read;
    };
    let subs: Vec<&Row> = boardy
        .store
        .tickets(ticket.id.split_once('-').map_or("", |(board, _)| board))
        .iter()
        .filter(|sub| super::look::parent(&sub.id) == Some(ticket.id.as_str()))
        .collect();
    read.children((!ticket.questions.is_empty()).then(|| {
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
    .child(tabs(opened, cx))
    .children({
        let seen = Seen {
            opened,
            boardy,
            theme,
        };
        opened
            .lines
            .iter()
            .filter(|line| line.tab() == opened.tab)
            .filter_map(|line| entry(line, &seen, window, cx))
            .collect::<Vec<_>>()
    })
    .child(
        Composer::new("boardy-comment", boardy.composer.clone()).on_send(cx.listener(
            |boardy: &mut Boardy, text: &str, _, cx| boardy.comment(text.trim().to_owned(), cx),
        )),
    )
}

fn mentions(
    at: &str,
    refs: &[Ref],
    boardy: &Boardy,
    theme: &Theme,
    cx: &Context<Boardy>,
) -> Option<Div> {
    (!refs.is_empty()).then(|| {
        div().flex().flex_wrap().gap_1p5().children(
            refs.iter()
                .enumerate()
                .map(|(n, one)| mention(at, n, one, boardy, theme, cx)),
        )
    })
}

fn mention(
    at: &str,
    n: usize,
    one: &Ref,
    boardy: &Boardy,
    theme: &Theme,
    cx: &Context<Boardy>,
) -> AnyElement {
    let faint = ink(theme, T2);
    let mark = match &one.to {
        To::Ticket(id) => match boardy.store.find(id).and_then(|ticket| kit(&ticket.status)) {
            Some(status) => status_icon(status, px(ICON)).into_any_element(),
            None => glyph(Glyph::Link, ICON, faint).into_any_element(),
        },
        To::File(_) => glyph(Glyph::File, ICON, faint).into_any_element(),
        To::Folder(_) => glyph(Glyph::Folder, ICON, faint).into_any_element(),
        To::Image(_) => glyph(Glyph::Image, ICON, faint).into_any_element(),
    };
    let hovered = ink(theme, HOVER);
    let to = one.to.clone();
    let chip = div()
        .id(SharedString::from(format!("ref-{at}-{n}")))
        .relative()
        .flex()
        .items_center()
        .gap_1()
        .max_w_full()
        .min_w_0()
        .h(px(CHIP_HEIGHT))
        .px_1p5()
        .rounded(px(CHIP_RADIUS))
        .bg(ink(theme, BUBBLE_FILL))
        .hover(move |style| style.bg(hovered))
        .cursor_pointer()
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, T1))
        .child(mark)
        .child(div().min_w_0().truncate().child(one.label.clone()))
        .on_click(
            cx.listener(move |boardy: &mut Boardy, _: &ClickEvent, window, cx| {
                boardy.follow(to.clone(), window, cx);
            }),
        );
    let To::Image(path) = &one.to else {
        return chip.into_any_element();
    };
    let key = link_key(&one.label, n);
    let held = Rc::new(Cell::new(Bounds::default()));
    let measured = held.clone();
    let subject = Subject::File(path.clone());
    chip.child(
        canvas(move |bounds, _, _| measured.set(bounds), |_, (), _, _| {})
            .absolute()
            .size_full(),
    )
    .on_hover(move |over: &bool, window, cx| {
        hover(key, over.then(|| (subject.clone(), held.get())), window, cx);
    })
    .child(link_card(key))
    .into_any_element()
}

impl Boardy {
    #[cfg(feature = "module-editor")]
    fn follow(&mut self, to: To, window: &mut Window, cx: &mut Context<Self>) {
        let (path, folder) = match to {
            To::Ticket(id) => {
                self.open_ticket(id, cx);
                return cx.notify();
            }
            To::File(path) => (path, false),
            To::Image(path) => (path.to_path_buf(), false),
            To::Folder(path) => (path, true),
        };
        let Some(Some(desk)) = window.root::<crate::desk::Desk>() else {
            return self.say(
                "boardy is not inside the desk, so it cannot open the editor".to_owned(),
                cx,
            );
        };
        eprintln!("desk: boardy opens {} in the editor", path.display());
        let me = cx.weak_entity();
        window.defer(cx, move |window, cx| {
            let opened = desk.update(cx, |desk, cx| {
                desk.open_screen(EDITOR, window, cx)?;
                let editor = desk
                    .editor()
                    .ok_or_else(|| "the editor screen holds no editor".to_owned())?;
                editor.update(cx, |editor, cx| match folder {
                    true => editor
                        .reveal_in_tree(&path, cx)
                        .then_some(())
                        .ok_or_else(|| {
                            format!("{} is outside the editor's folder", path.display())
                        }),
                    false => {
                        editor.land(&path, 0, 0, window, cx);
                        Ok(())
                    }
                })
            });
            if let Err(error) = opened
                && let Err(gone) = me.update(cx, |boardy, cx| boardy.say(error, cx))
            {
                eprintln!("desk: boardy closed before the editor refused: {gone}");
            }
        });
    }

    #[cfg(not(feature = "module-editor"))]
    fn follow(&mut self, to: To, _: &mut Window, cx: &mut Context<Self>) {
        match to {
            To::Ticket(id) => {
                self.open_ticket(id, cx);
                cx.notify();
            }
            To::File(_) | To::Folder(_) | To::Image(_) => {
                self.say("this desk was built without the editor".to_owned(), cx);
            }
        }
    }
}

fn section(title: String, theme: &Theme) -> Div {
    div().pt_2().child(caption(title, theme))
}

fn linked(ticket: &Row, theme: &Theme, cx: &Context<Boardy>) -> AnyElement {
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

pub(super) struct Writer {
    name: String,
    kind: AgentKind,
}

pub(super) fn runners(events: &[Event]) -> Vec<Option<String>> {
    let named = |event: &Event| {
        matches!(event.kind, Kind::Spawned | Kind::Reported)
            .then(|| event.agent.clone())
            .flatten()
    };
    let mut later = None;
    let mut after: Vec<Option<String>> = events
        .iter()
        .rev()
        .map(|event| {
            later = named(event).or(later.take());
            later.clone()
        })
        .collect();
    after.reverse();
    let mut before = None;
    events
        .iter()
        .zip(after)
        .map(|(event, after)| {
            before = named(event).or(before.take());
            before.clone().or(after)
        })
        .collect()
}

fn sub_agent_name(name: &str, ticket: &str, ran: Option<&str>) -> String {
    match Speaker::of(name) {
        Speaker::Named if name != ticket => name.to_owned(),
        Speaker::Named | Speaker::Agent | Speaker::Lead | Speaker::Unknown => {
            ran.map_or_else(|| format!("sub-agent on {ticket}"), str::to_owned)
        }
    }
}

fn named(name: &str, kind: AgentKind) -> Writer {
    Writer {
        name: name.to_owned(),
        kind,
    }
}

fn lettered(name: &str) -> AgentKind {
    AgentKind::Other(name.chars().next().map_or('?', |c| c.to_ascii_uppercase()))
}

fn engineer(name: &str) -> Writer {
    let role = name.trim_end_matches(|c: char| c.is_ascii_digit() || c == '-');
    let number = name
        .get(role.len()..)
        .unwrap_or_default()
        .trim_start_matches('-');
    let kind = AgentKind::named(role);
    if role.contains(char::is_whitespace) || role.is_empty() {
        return named(name, lettered(name));
    }
    let mut words = role.replace('-', " ");
    if let Some(head) = words.get_mut(..1) {
        head.make_ascii_uppercase();
    }
    named(format!("{words} {number}").trim_end(), kind)
}

fn authored(kind: &AuthorKind, name: &str, ticket: &str, ran: Option<&str>) -> Writer {
    match kind {
        AuthorKind::Person => named(PERSON, lettered(PERSON)),
        AuthorKind::ClaudeCode => named(name, lettered(name)),
        AuthorKind::Lead => {
            let session = name
                .strip_prefix(ORCHESTRATOR)
                .or_else(|| name.strip_prefix(LEAD))
                .unwrap_or(name)
                .trim_start_matches('-');
            named(format!("{PM} {session}").trim_end(), lettered(PM))
        }
        AuthorKind::SubAgent => engineer(&sub_agent_name(name, ticket, ran)),
        AuthorKind::Hook | AuthorKind::Unknown(_) => named(name, lettered(name)),
    }
}

pub(super) fn assigned(
    by: &str,
    spawned: &BTreeMap<String, Option<String>>,
) -> (Writer, Option<String>) {
    let requester = requester(by, spawned);
    let mut writer = authored(&requester.kind, &requester.alias, "", None);
    if writer.name.chars().count() > ALIAS_MOST {
        let kept: String = writer.name.chars().take(ALIAS_MOST - 1).collect();
        writer.name = format!("{kept}…");
    }
    (writer, requester.id)
}

pub(super) struct Requester {
    kind: AuthorKind,
    alias: String,
    id: Option<String>,
}

pub(super) fn requester(by: &str, spawned: &BTreeMap<String, Option<String>>) -> Requester {
    let made = |kind: AuthorKind, alias: &str, session: Option<&str>| Requester {
        id: session
            .filter(|session| !session.is_empty())
            .map(|session| {
                format!(
                    "{alias}#{}",
                    session.get(..SHORT_SESSION).unwrap_or(session)
                )
            }),
        alias: alias.to_owned(),
        kind,
    };
    let (session, agent) = by
        .split_once('/')
        .map_or((by, None), |(session, agent)| (session, Some(agent)));
    let claude_session = session.strip_prefix(CLAUDE_CODE_SESSION);
    match agent {
        _ if by.is_empty() => made(AuthorKind::Unknown(String::new()), UNKNOWN, None),
        _ if by == PERSON_ACTOR => made(AuthorKind::Person, PERSON, None),
        _ if by == ORCHESTRATOR => made(AuthorKind::Lead, LEAD, None),
        _ if by.starts_with(HOOK_ACTOR) || by == SERVE_ACTOR => made(AuthorKind::Hook, by, None),
        Some(agent) if by.starts_with(CLAUDE_CODE) => {
            made(AuthorKind::SubAgent, agent, claude_session)
        }
        None if by.starts_with(CLAUDE_CODE) => {
            made(AuthorKind::ClaudeCode, CLAUDE_CODE, claude_session)
        }
        Some(ticket) => match spawned.get(ticket).and_then(Option::as_deref) {
            Some(name) => made(AuthorKind::SubAgent, name, Some(session)),
            None => made(
                AuthorKind::SubAgent,
                &format!("sub-agent on {ticket}"),
                Some(session),
            ),
        },
        None => match Speaker::of(by) {
            Speaker::Lead => made(AuthorKind::Lead, LEAD, Some(by)),
            Speaker::Named => made(AuthorKind::SubAgent, by, None),
            Speaker::Unknown | Speaker::Agent => made(AuthorKind::Unknown(by.to_owned()), by, None),
        },
    }
}

pub(super) fn asker(
    key: &str,
    by: &str,
    said: &str,
    boardy: &Boardy,
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> AnyElement {
    let (writer, id) = assigned(by, &boardy.requesters);
    let chip = byline(&writer, said, theme);
    let Some(id) = id else {
        return chip.into_any_element();
    };
    tooltip(
        SharedString::from(format!("{key}-tip")),
        div().id(SharedString::from(key.to_owned())).child(chip),
        Edge::Frame,
        id,
        theme,
        window,
        cx,
    )
    .into_any_element()
}

pub(super) fn byline(writer: &Writer, said: &str, theme: &Theme) -> Div {
    div()
        .flex()
        .items_center()
        .gap_2()
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, CAPTION_TEXT))
        .child(writer.face(theme))
        .child(format!("{} {said}", writer.name).trim_end().to_owned())
}

impl Writer {
    pub(super) fn face(&self, theme: &Theme) -> Div {
        kind_avatar(self.kind, AvatarSize::Row, theme)
    }

    pub(super) fn name(&self) -> &str {
        &self.name
    }
}

fn writer(event: &Event, ran: Option<&str>) -> Writer {
    if let Some(author) = &event.author {
        return authored(&author.kind, &author.name, &event.ticket, ran);
    }
    let actor = event.actor.as_deref().unwrap_or_default();
    match Speaker::of(actor) {
        Speaker::Unknown if actor.is_empty() => {
            named(event.agent.as_deref().unwrap_or(UNKNOWN), lettered(UNKNOWN))
        }
        Speaker::Unknown => named(actor, lettered(actor)),
        Speaker::Lead => authored(
            &AuthorKind::Lead,
            actor.get(..SHORT_SESSION).unwrap_or(actor),
            &event.ticket,
            ran,
        ),
        Speaker::Agent => authored(
            &AuthorKind::SubAgent,
            event.agent.as_deref().unwrap_or(actor),
            &event.ticket,
            ran,
        ),
        Speaker::Named => engineer(actor),
    }
}

struct Seen<'a> {
    opened: &'a Opened,
    boardy: &'a Boardy,
    theme: &'a Theme,
}

impl Seen<'_> {
    fn event(&self, at: usize) -> Option<&Event> {
        self.opened.events.get(at)
    }

    fn writer(&self, at: usize, event: &Event) -> Writer {
        writer(event, self.opened.ran.get(at).and_then(Option::as_deref))
    }

    fn open(&self, at: usize) -> bool {
        self.opened.unfolded.contains(&at)
    }
}

fn fold(at: usize, label: &'static str, cx: &Context<Boardy>, theme: &Theme) -> impl IntoElement {
    button(
        SharedString::from(format!("boardy-fold-{at}")),
        label,
        None,
        ButtonKind::Text,
        theme,
    )
    .on_click(
        cx.listener(move |boardy: &mut Boardy, _: &ClickEvent, _, cx| {
            if let Some(opened) = boardy.opened.as_mut()
                && !opened.unfolded.remove(&at)
            {
                opened.unfolded.insert(at);
            }
            cx.notify();
        }),
    )
}

fn when(at: usize, event: &Event, theme: &Theme, window: &mut Window, cx: &mut App) -> AnyElement {
    let exact = DateTime::parse_from_rfc3339(&event.at).map_or_else(
        |_| event.at.clone(),
        |then| then.with_timezone(&Local).format(EXACT_TIME).to_string(),
    );
    tooltip(
        SharedString::from(format!("boardy-at-{at}-tip")),
        div()
            .id(SharedString::from(format!("boardy-at-{at}")))
            .flex_none()
            .text_size(px(FONT_SMALL))
            .text_color(ink(theme, CAPTION_TEXT))
            .child(age(&event.at, Utc::now())),
        Edge::Text,
        exact,
        theme,
        window,
        cx,
    )
    .into_any_element()
}

fn tabs(opened: &Opened, cx: &Context<Boardy>) -> PillTabs {
    Tab::ALL
        .into_iter()
        .fold(PillTabs::new("boardy-activity-tabs"), |tabs, tab| {
            let count = opened.lines.iter().filter(|line| line.tab() == tab).count();
            tabs.tab(
                format!("{} {count}", tab.label()),
                tab == opened.tab,
                cx.listener(move |boardy: &mut Boardy, _: &ClickEvent, _, cx| {
                    if let Some(opened) = boardy.opened.as_mut() {
                        opened.tab = tab;
                    }
                    cx.notify();
                }),
            )
        })
}

fn entry(line: &Line, seen: &Seen, window: &mut Window, cx: &mut Context<Boardy>) -> Option<Div> {
    match *line {
        Line::Said { at, moved } => message(at, moved, seen, window, cx),
        Line::Evidence { at } => message(at, None, seen, window, cx),
        Line::Handoff { at } => handoff(at, seen, window, cx),
        Line::Asked { at, answer } => {
            let reply =
                answer.and_then(|answer| match seen.event(answer).is_some_and(unanswered) {
                    true => muted(answer, seen, window, cx),
                    false => message(answer, None, seen, window, cx),
                });
            Some(
                div()
                    .flex()
                    .flex_col()
                    .gap(px(REPLY_GAP))
                    .children(message(at, None, seen, window, cx))
                    .children(reply.map(|reply| reply.pl(px(REPLY_INDENT)))),
            )
        }
        Line::Unanswered { at } => muted(at, seen, window, cx),
        Line::Kept { at } => kept(at, seen, window, cx),
    }
}

fn header(writer: &Writer, said: String, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_wrap()
        .items_center()
        .gap_1p5()
        .min_w_0()
        .text_size(px(FONT_SMALL))
        .child(
            div()
                .font_weight(FontWeight::SEMIBOLD)
                .text_color(ink(theme, T1))
                .child(writer.name.clone()),
        )
        .child(div().text_color(ink(theme, CAPTION_TEXT)).child(said))
}

fn message(
    at: usize,
    moved: Option<usize>,
    seen: &Seen,
    window: &mut Window,
    cx: &mut Context<Boardy>,
) -> Option<Div> {
    let theme = seen.theme;
    let event = seen.event(at)?;
    let Said::Spoken {
        full,
        short,
        refs,
        verdict,
    } = seen.opened.said.get(at)?
    else {
        return None;
    };
    let writer = seen.writer(at, event);
    let open = seen.open(at);
    let shown = match short {
        Some(short) if !open => short,
        Some(_) | None => full,
    };
    let status = moved
        .and_then(|moved| seen.event(moved))
        .and_then(|moved| moved.to.clone())
        .map(|to| Status::from(String::from(to)));
    let chip = status.map(|status| {
        div()
            .flex()
            .items_center()
            .gap_1()
            .h(px(CHIP_HEIGHT))
            .px_1p5()
            .rounded(px(CHIP_RADIUS))
            .bg(ink(theme, BUBBLE_FILL))
            .text_color(ink(theme, T1))
            .children(kit(&status).map(|kit| status_icon(kit, px(ICON))))
            .child(look::label(&status).to_owned())
    });
    let head = header(&writer, did(event), theme)
        .children(
            verdict.map(|verdict| dot_chip(verdict.label(), rgb_to_hsla(verdict.color(theme)))),
        )
        .children(chip)
        .child(when(at, event, theme, window, cx));
    let trace = event
        .author
        .as_ref()
        .and_then(|author| author.trace.clone())
        .map(|trace| command(at, trace, theme));
    let bubble = div()
        .flex_1()
        .min_w_0()
        .flex()
        .flex_col()
        .gap_1()
        .p_3()
        .rounded(px(BUBBLE_RADIUS))
        .bg(ink(theme, BUBBLE_FILL))
        .child(head)
        .child(document(
            SharedString::from(format!("said-{at}")),
            shown,
            &[],
            theme,
        ))
        .children(
            short
                .as_ref()
                .map(|_| fold(at, if open { "Show less" } else { "Show all" }, cx, theme)),
        )
        .children(mentions(
            &format!("said-{at}"),
            refs,
            seen.boardy,
            theme,
            cx,
        ))
        .children(trace);
    Some(div().flex().gap_2().child(writer.face(theme)).child(bubble))
}

fn handoff(at: usize, seen: &Seen, window: &mut Window, cx: &mut Context<Boardy>) -> Option<Div> {
    let theme = seen.theme;
    let event = seen.event(at)?;
    let writer = seen.writer(at, event);
    let to = event
        .agent
        .as_deref()
        .filter(|agent| !agent.is_empty())
        .map_or_else(|| "an engineer".to_owned(), |agent| engineer(agent).name);
    let brief = match seen.opened.said.get(at) {
        Some(Said::Spoken { full, refs, .. }) => Some((full, refs)),
        Some(Said::Quiet | Said::Echo) | None => None,
    };
    let open = seen.open(at);
    let line = div()
        .flex()
        .flex_wrap()
        .items_center()
        .gap_2()
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, CAPTION_TEXT))
        .child(writer.face(theme))
        .child(format!("{} handed {} to {to}", writer.name, event.ticket))
        .child(when(at, event, theme, window, cx))
        .children(brief.map(|_| {
            fold(
                at,
                if open { "Hide brief" } else { "Show brief" },
                cx,
                theme,
            )
        }));
    let shown = brief.filter(|_| open).map(|(full, refs)| {
        div()
            .ml(px(REPLY_INDENT))
            .flex()
            .flex_col()
            .gap_1()
            .p_3()
            .rounded(px(BUBBLE_RADIUS))
            .bg(ink(theme, BUBBLE_FILL))
            .child(document(
                SharedString::from(format!("brief-{at}")),
                full,
                &[],
                theme,
            ))
            .children(mentions(
                &format!("brief-{at}"),
                refs,
                seen.boardy,
                theme,
                cx,
            ))
    });
    Some(div().flex().flex_col().gap_2().child(line).children(shown))
}

fn muted(at: usize, seen: &Seen, window: &mut Window, cx: &mut Context<Boardy>) -> Option<Div> {
    let theme = seen.theme;
    let event = seen.event(at)?;
    let said = event
        .text
        .as_deref()
        .and_then(|text| text.lines().next())
        .unwrap_or_default()
        .to_owned();
    Some(
        div()
            .flex()
            .items_center()
            .gap_2()
            .text_size(px(FONT_SMALL))
            .text_color(ink(theme, CAPTION_TEXT))
            .child(glyph(Glyph::Cron, ICON, ink(theme, T2)))
            .child(div().min_w_0().child(said))
            .child(when(at, event, theme, window, cx)),
    )
}

fn kept(at: usize, seen: &Seen, window: &mut Window, cx: &mut Context<Boardy>) -> Option<Div> {
    let theme = seen.theme;
    let event = seen.event(at)?;
    let writer = seen.writer(at, event);
    let faint = ink(theme, T2);
    let to = event.to.clone().map(|to| Status::from(String::from(to)));
    let mark = match (&event.kind, to.as_ref().and_then(kit)) {
        (Kind::Moved, Some(status)) => status_icon(status, px(ICON)).into_any_element(),
        (kind, _) => glyph(kept_glyph(kind), ICON, faint).into_any_element(),
    };
    let said = match &event.kind {
        Kind::Assigned => {
            let by = event.text.as_deref().unwrap_or_default();
            format!(
                "assigned it to {}",
                assigned(by, &seen.boardy.requesters).0.name
            )
        }
        _ => did(event),
    };
    let reason = match event.kind {
        Kind::Moved => event
            .text
            .as_deref()
            .map(str::trim)
            .filter(|text| !text.is_empty()),
        _ => None,
    };
    let open = seen.open(at);
    let short = reason.and_then(folded);
    let shown = reason.map(|reason| match &short {
        Some(short) if !open => short.clone(),
        Some(_) | None => reason.to_owned(),
    });
    Some(
        div()
            .flex()
            .flex_col()
            .gap_1()
            .child(
                div()
                    .flex()
                    .flex_wrap()
                    .items_center()
                    .gap_2()
                    .text_size(px(FONT_SMALL))
                    .text_color(ink(theme, CAPTION_TEXT))
                    .child(mark)
                    .child(format!("{} {said}", writer.name))
                    .child(when(at, event, theme, window, cx)),
            )
            .children(shown.map(|shown| {
                div()
                    .pl(px(ICON + KEPT_GAP))
                    .text_size(px(FONT_SMALL))
                    .text_color(faint)
                    .child(shown)
            }))
            .children(short.map(|_| {
                div().pl(px(ICON + KEPT_GAP)).child(fold(
                    at,
                    if open { "Show less" } else { "Show all" },
                    cx,
                    theme,
                ))
            })),
    )
}

fn kept_glyph(kind: &Kind) -> Glyph {
    match kind {
        Kind::Created => Glyph::FileAdd,
        Kind::Moved => Glyph::Relative,
        Kind::Assigned | Kind::Managers => Glyph::Person,
        Kind::Widened => Glyph::Folder,
        Kind::Handed | Kind::Spawned => Glyph::Agents,
        Kind::Rule | Kind::Waiting | Kind::Hook | Kind::HookFailed => Glyph::Settings,
        Kind::Requested | Kind::Accepted | Kind::RequestDropped => Glyph::Inbox,
        Kind::Commented | Kind::Asked | Kind::Answered | Kind::Messaged | Kind::Reported => {
            Glyph::Talk
        }
        Kind::Unknown(_) => Glyph::Ellipsis,
    }
}

fn command(at: usize, trace: String, theme: &Theme) -> Div {
    div()
        .flex()
        .items_center()
        .gap_1()
        .min_w_0()
        .child(
            div()
                .flex_1()
                .min_w_0()
                .truncate()
                .font_family(theme.word(WordToken::ShapeMono))
                .text_size(px(FONT_SMALL))
                .text_color(ink(theme, T2))
                .child(trace.clone()),
        )
        .child(
            button(
                SharedString::from(format!("boardy-trace-{at}")),
                "Copy",
                Some(Glyph::Copy),
                ButtonKind::Text,
                theme,
            )
            .on_click(move |_, _, cx| {
                eprintln!("desk: boardy copied the trace command {trace}");
                cx.write_to_clipboard(ClipboardItem::new_string(trace.clone()));
            }),
        )
}

fn did(event: &Event) -> String {
    let text = event.text.as_deref().unwrap_or_default();
    let to = event.to.clone().map(String::from).unwrap_or_default();
    let from = event.from.clone().map(String::from).unwrap_or_default();
    match &event.kind {
        Kind::Created => format!("created it in {to}"),
        Kind::Moved => format!("moved it {from} → {to}"),
        Kind::Commented => "commented".to_owned(),
        Kind::Asked => "asked".to_owned(),
        Kind::Answered => "answered".to_owned(),
        Kind::Spawned => "spawned an agent".to_owned(),
        Kind::Reported => "reported".to_owned(),
        Kind::Messaged => "sent a message".to_owned(),
        Kind::Hook => format!("ran a hook: {text}"),
        Kind::HookFailed => format!("a hook failed: {text}"),
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
    window: &mut Window,
    cx: &mut Context<Boardy>,
) -> impl IntoElement {
    let ticket = &opened.ticket;
    let words = &boardy.store.words;
    let faint = ink(theme, T2);
    let line = |mark: AnyElement, value: String| {
        div()
            .flex()
            .items_center()
            .gap_1p5()
            .min_w_0()
            .child(mark)
            .child(div().min_w_0().truncate().child(value))
            .into_any_element()
    };
    let marked = |of: Glyph, value: String| line(glyph(of, ICON, faint).into_any_element(), value);
    let aimed = ticket.id.clone();
    let status = div()
        .id("boardy-status")
        .flex()
        .items_center()
        .gap_1p5()
        .children(kit(&ticket.status).map(|status| status_icon(status, px(ICON))))
        .child(look::label(&ticket.status).to_owned())
        .when(matches!(opened.origin, Origin::Ticket(_)), |status| {
            status.cursor_pointer().on_click(cx.listener(
                move |boardy: &mut Boardy, event: &ClickEvent, _, cx| {
                    boardy.aimed = Some(aimed.clone());
                    boardy.menu_at = Some(event.position());
                    cx.notify();
                },
            ))
        })
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
                (
                    "Type".into(),
                    marked(
                        kind_glyph(&ticket.r#type),
                        String::from(ticket.r#type.clone()),
                    ),
                ),
                ("Assignee".into(), assignee(ticket, boardy, theme, line)),
                (
                    noun(&words.epic, false).into(),
                    match &ticket.epic {
                        Some(epic) => {
                            let board = ticket
                                .id
                                .split_once('-')
                                .map_or("", |(board, _)| board)
                                .to_owned();
                            div()
                                .id("boardy-epic")
                                .cursor_pointer()
                                .child(icon_chip(Glyph::Epic, epic.clone()))
                                .on_click(cx.listener(
                                    move |boardy: &mut Boardy, _: &ClickEvent, _, cx| {
                                        boardy.go(Place::Board(board.clone(), Section::Epics), cx);
                                    },
                                ))
                                .into_any_element()
                        }
                        None => marked(Glyph::Epic, "none".to_owned()),
                    },
                ),
                (
                    noun(&words.sprint, false).into(),
                    marked(
                        Glyph::Sprint,
                        ticket.sprint.clone().unwrap_or_else(|| "none".to_owned()),
                    ),
                ),
                (
                    noun(&words.points, false).into(),
                    marked(
                        Glyph::Gauge,
                        ticket
                            .points
                            .map_or_else(|| "none".to_owned(), |points| points.to_string()),
                    ),
                ),
            ]
            .into_iter()
            .chain(match &opened.origin {
                Origin::Ticket(actual) => vec![
                    (
                        "Created".into(),
                        marked(Glyph::FileAdd, day(&ticket.created)),
                    ),
                    (
                        "Updated".into(),
                        marked(Glyph::Pencil, day(&ticket.updated)),
                    ),
                    (
                        "Worked".into(),
                        marked(
                            Glyph::Cron,
                            format!("{} rounds, {} min", actual.rounds, actual.minutes),
                        ),
                    ),
                ],
                Origin::Request { by, at, .. } => vec![
                    (
                        "Requested by".into(),
                        asker("boardy-requested-by", by, "", boardy, theme, window, cx),
                    ),
                    ("Requested".into(), marked(Glyph::FileAdd, day(at))),
                ],
            })
            .collect::<Vec<(SharedString, AnyElement)>>(),
        )
        .section(
            "Labels",
            [(
                SharedString::default(),
                div()
                    .flex()
                    .flex_wrap()
                    .items_center()
                    .gap_1()
                    .child(glyph(Glyph::Label, ICON, faint))
                    .children(ticket.labels.iter().enumerate().map(|(n, name)| {
                        tooltip(
                            SharedString::from(format!("boardy-label-tip-{n}")),
                            div()
                                .id(SharedString::from(format!("boardy-label-{n}")))
                                .min_w_0()
                                .max_w_full()
                                .child(dot_chip(name.clone(), label)),
                            Edge::Frame,
                            name.clone(),
                            theme,
                            window,
                            cx,
                        )
                    }))
                    .when(ticket.labels.is_empty(), |row| {
                        row.child(note("none", theme))
                    })
                    .into_any_element(),
            )],
        )
        .section("Related", relations)
        .section(
            "Owns",
            opened
                .owned
                .iter()
                .enumerate()
                .map(|(n, (glob, to))| {
                    let shown = match to {
                        Some(to) => mention(
                            "owns",
                            n,
                            &Ref {
                                label: glob.clone(),
                                to: to.clone(),
                            },
                            boardy,
                            theme,
                            cx,
                        ),
                        None => marked(Glyph::File, glob.clone()),
                    };
                    let tipped = tooltip(
                        SharedString::from(format!("boardy-owns-tip-{n}")),
                        div()
                            .id(SharedString::from(format!("boardy-owns-{n}")))
                            .min_w_0()
                            .max_w_full()
                            .child(shown),
                        Edge::Frame,
                        glob.clone(),
                        theme,
                        window,
                        cx,
                    );
                    (SharedString::default(), tipped.into_any_element())
                })
                .collect::<Vec<_>>(),
        )
}

fn kind_glyph(kind: &TicketType) -> Glyph {
    match kind {
        TicketType::Story => Glyph::Book,
        TicketType::Task => Glyph::Check,
        TicketType::Bug => Glyph::Bug,
        TicketType::Subtask => Glyph::Branch,
        TicketType::Unknown(_) => Glyph::File,
    }
}

fn assignee(
    ticket: &Ticket,
    boardy: &Boardy,
    theme: &Theme,
    line: impl Fn(AnyElement, String) -> AnyElement,
) -> AnyElement {
    let Some(who) = ticket.assignee.as_deref() else {
        return line(
            glyph(Glyph::Person, ICON, ink(theme, T2)).into_any_element(),
            "nobody".to_owned(),
        );
    };
    let (writer, _) = assigned(who, &boardy.requesters);
    let status = match ticket.status {
        Status::Doing => Some(AgentStatus::Working),
        Status::Done => Some(AgentStatus::Finished),
        Status::NeedsDecision => Some(AgentStatus::Asking),
        Status::Blocked => Some(AgentStatus::Failed),
        Status::Triage
        | Status::Backlog
        | Status::Todo
        | Status::Review
        | Status::Dropped
        | Status::Unknown(_) => None,
    };
    let mark = match status {
        Some(status) => {
            let agent = Agent {
                kind: writer.kind,
                instance: 0,
                status,
            };
            avatar("boardy-assignee", &agent, AvatarSize::Feed, theme).into_any_element()
        }
        None => kind_avatar(writer.kind, AvatarSize::Feed, theme).into_any_element(),
    };
    line(mark, writer.name)
}
