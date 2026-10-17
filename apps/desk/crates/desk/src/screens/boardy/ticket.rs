use chrono::Utc;
use desk_core::boardy::{Speaker, report_echoes, spoken};
use desk_core::protocol::boardy::{Event, Row, Ticket};
use desk_core::protocol::{
    BoardyAuthorKind as AuthorKind, BoardyEventKind as Kind, BoardyStatus as Status,
    BoardyTicketType as TicketType,
};
use desk_ui::components::avatar::{Agent, AgentKind, AgentStatus, AvatarSize, avatar};
use desk_ui::components::breadcrumb::{OnCrumb, breadcrumb};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::caption;
use desk_ui::components::chat::{Block, document, markdown};
use desk_ui::components::composer::Composer;
use desk_ui::components::dot_chip::{dot_chip, icon_chip};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::hovercard::{Subject, hover, link_card, link_key};
use desk_ui::components::paint::{glyph, ink};
use desk_ui::components::priority_icon::priority_icon;
use desk_ui::components::properties::properties_panel;
use desk_ui::components::scroll::ScrollArea;
use desk_ui::components::size::{CAPTION_TEXT, FONT_SMALL, FONT_TITLE, HOVER, T1, T2};
use desk_ui::components::status_icon::status_icon;
use desk_ui::theme::{ColorToken, Theme, WordToken};
use gpui::{
    AnyElement, App, Bounds, ClickEvent, ClipboardItem, Context, CursorStyle, Div, Entity,
    FontWeight, MouseButton, MouseMoveEvent, Pixels, Rgba, SharedString, Window, canvas, div,
    prelude::*, px, rgb_to_hsla,
};
use std::cell::Cell;
use std::rc::Rc;
use std::time::Instant;

use super::look::{self, age, day, disc, kit, noun};
use super::refs::{Ref, To};
use super::views::opener;
use super::{Boardy, Opened, Place, Section};
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

pub(super) enum Said {
    Quiet,
    Echo,
    Spoken {
        full: Vec<Block>,
        short: Option<Vec<Block>>,
        refs: Vec<Ref>,
    },
}

pub(super) fn said(events: &[Event], found: impl Fn(&str) -> Vec<Ref>) -> Vec<Said> {
    events
        .iter()
        .zip(report_echoes(events))
        .map(|(event, echo)| {
            let text = event.text.as_deref().unwrap_or_default();
            match (echo, spoken(event)) {
                (true, _) => Said::Echo,
                (false, false) => Said::Quiet,
                (false, true) => Said::Spoken {
                    full: markdown(text),
                    short: folded(text).map(|short| markdown(&short)),
                    refs: found(text),
                },
            }
        })
        .collect()
}

fn folded(text: &str) -> Option<String> {
    let long = text.lines().nth(FOLD_LINES).is_some() || text.chars().nth(FOLD_CHARS).is_some();
    long.then(|| {
        let head = text.lines().take(FOLD_LINES).collect::<Vec<_>>().join("\n");
        let shown: String = head.chars().take(FOLD_CHARS).collect();
        format!("{}…", shown.trim_end())
    })
}

pub(super) fn page(boardy: &Boardy, theme: &Theme, wide: Pixels, cx: &mut Context<Boardy>) -> Div {
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
    let side = (f32::from(wide) * SIDE_SHARE).clamp(SIDE_LEAST, SIDE_MOST);
    div()
        .size_full()
        .flex()
        .flex_col()
        .min_h_0()
        .child(top)
        .child(Split {
            width: boardy.panel.clone(),
            body: ScrollArea::new("boardy-ticket")
                .child(
                    div()
                        .w_full()
                        .flex()
                        .justify_center()
                        .py(px(PAD))
                        .pl(px(side))
                        .pr(px(side.max(DIVIDER_GAP)))
                        .child(body(boardy, opened, theme, cx)),
                )
                .into_any_element(),
            panel: ScrollArea::new("boardy-properties")
                .child(
                    div()
                        .py(px(PAD))
                        .pl(px(PANEL_PAD))
                        .pr(px(PAD))
                        .child(properties(boardy, opened, theme, cx)),
                )
                .into_any_element(),
            line: theme.color(ColorToken::Separator),
            lit: theme.color(ColorToken::FocusRing),
        })
}

pub(super) struct Width {
    px: f32,
    dragging: bool,
    right: Rc<Cell<Pixels>>,
    moved: Option<Instant>,
}

impl Default for Width {
    fn default() -> Self {
        Width {
            px: PANEL,
            dragging: false,
            right: Rc::default(),
            moved: None,
        }
    }
}

#[derive(IntoElement)]
struct Split {
    width: Entity<Width>,
    body: AnyElement,
    panel: AnyElement,
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
        let (shown, right) = {
            let held = width.read(cx);
            (held.px, held.right.clone())
        };
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
            .child(
                canvas(
                    move |bounds, _, _| right.set(bounds.right()),
                    |_, (), _, _| {},
                )
                .absolute()
                .size_full(),
            )
            .child(div().flex_1().min_w_0().h_full().child(self.body))
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
                div()
                    .flex_none()
                    .w(px(shown - HANDLE))
                    .h_full()
                    .child(self.panel),
            )
    }
}

fn body(boardy: &Boardy, opened: &Opened, theme: &Theme, cx: &mut Context<Boardy>) -> Div {
    let ticket = &opened.answer.ticket;
    let revisions = ticket.acceptance.len();
    let subs: Vec<&Row> = boardy
        .store
        .tickets(ticket.id.split_once('-').map_or("", |(board, _)| board))
        .iter()
        .filter(|sub| super::look::parent(&sub.id) == Some(ticket.id.as_str()))
        .collect();
    div()
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
        .children(mentions("body", &opened.body_refs, boardy, theme, cx))
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
                .enumerate()
                .filter_map(|(at, event)| activity(at, event, opened, boardy, theme, cx)),
        )
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
        .h(px(CHIP_HEIGHT))
        .px_1p5()
        .rounded(px(CHIP_RADIUS))
        .bg(ink(theme, BUBBLE_FILL))
        .hover(move |style| style.bg(hovered))
        .cursor_pointer()
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, T1))
        .child(mark)
        .child(one.label.clone())
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

struct Writer {
    name: String,
    color: Rgba,
    sub_agent: bool,
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

fn sub_agent_name(name: &str, event: &Event, ran: Option<&str>) -> String {
    match Speaker::of(name) {
        Speaker::Named if name != event.ticket => name.to_owned(),
        Speaker::Named | Speaker::Agent | Speaker::Lead | Speaker::Unknown => {
            ran.map_or_else(|| format!("sub-agent on {}", event.ticket), str::to_owned)
        }
    }
}

fn writer(event: &Event, ran: Option<&str>, theme: &Theme) -> Writer {
    let named = |name: &str, color: Rgba, sub_agent: bool| Writer {
        name: name.to_owned(),
        color,
        sub_agent,
    };
    let sub_agent = |name: &str| {
        let name = sub_agent_name(name, event, ran);
        named(&name, tone(&name, theme), true)
    };
    if let Some(author) = &event.author {
        let name = author.name.as_str();
        return match &author.kind {
            AuthorKind::Person => named(PERSON, theme.color(ColorToken::StatusAccent), false),
            AuthorKind::ClaudeCode => named(name, theme.color(ColorToken::StatusWarn), false),
            AuthorKind::Lead => named(name, theme.color(ColorToken::Trace), false),
            AuthorKind::SubAgent => sub_agent(name),
            AuthorKind::Hook | AuthorKind::Unknown(_) => {
                named(name, ink(theme, CAPTION_TEXT), false)
            }
        };
    }
    let actor = event.actor.as_deref().unwrap_or_default();
    match Speaker::of(actor) {
        Speaker::Unknown if actor.is_empty() => named(
            event.agent.as_deref().unwrap_or("unknown"),
            ink(theme, CAPTION_TEXT),
            false,
        ),
        Speaker::Unknown => named(actor, ink(theme, CAPTION_TEXT), false),
        Speaker::Lead => named("orchestrator", theme.color(ColorToken::StatusWarn), false),
        Speaker::Agent => sub_agent(event.agent.as_deref().unwrap_or(actor)),
        Speaker::Named => named(actor, tone(actor, theme), false),
    }
}

fn conversation(event: &Event) -> bool {
    match event.kind {
        Kind::Spawned | Kind::Messaged | Kind::Asked | Kind::Answered | Kind::Reported => true,
        Kind::Created
        | Kind::Moved
        | Kind::Commented
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
        | Kind::Unknown(_) => false,
    }
}

fn activity(
    at: usize,
    event: &Event,
    opened: &Opened,
    boardy: &Boardy,
    theme: &Theme,
    cx: &Context<Boardy>,
) -> Option<Div> {
    let ran = opened.ran.get(at).and_then(Option::as_deref);
    let open = opened.unfolded.contains(&at);
    let writer = writer(event, ran, theme);
    let name = &writer.name;
    let when = age(&event.at, Utc::now());
    let face = disc(
        &name.get(..1).unwrap_or_default().to_uppercase(),
        writer.color,
    );
    let (full, short, refs) = match opened.said.get(at)? {
        Said::Echo => return None,
        Said::Quiet => {
            return Some(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .text_size(px(FONT_SMALL))
                    .text_color(ink(theme, CAPTION_TEXT))
                    .child(face)
                    .child(format!("{name} {} · {when}", did(event))),
            );
        }
        Said::Spoken { full, short, refs } => (full, short, refs),
    };
    let talking = conversation(event);
    let heading = match event
        .agent
        .as_deref()
        .filter(|agent| talking && agent != name)
    {
        Some(agent) => format!("{name} → {agent} · {} · {when}", did(event)),
        None => format!("{name} {} · {when}", did(event)),
    };
    let shown = match short {
        Some(short) if !open => short,
        Some(_) | None => full,
    };
    let fold = short.as_ref().map(|_| {
        let label = if open { "Show less" } else { "Show all" };
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
    });
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
        .child(note(heading, theme))
        .child(document(
            SharedString::from(format!("said-{at}")),
            shown,
            &[],
            theme,
        ))
        .children(fold)
        .children(mentions(&format!("said-{at}"), refs, boardy, theme, cx))
        .children(trace);
    let row = div().flex().gap_2().child(face).child(bubble);
    Some(match (talking, writer.sub_agent) {
        (true, true) => row.flex_row_reverse().pl(px(REPLY_INDENT)),
        (true, false) => row.pr(px(REPLY_INDENT)),
        (false, _) => row,
    })
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

fn tone(name: &str, theme: &Theme) -> Rgba {
    AgentKind::named(name.trim_end_matches(|c: char| c.is_ascii_digit() || c == '-')).color(theme)
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
    cx: &Context<Boardy>,
) -> impl IntoElement {
    let ticket = &opened.answer.ticket;
    let actual = &opened.answer.actual;
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
                (
                    "Type".into(),
                    marked(
                        kind_glyph(&ticket.r#type),
                        String::from(ticket.r#type.clone()),
                    ),
                ),
                ("Assignee".into(), assignee(ticket, theme, line)),
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
                    (SharedString::default(), shown)
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
    theme: &Theme,
    line: impl Fn(AnyElement, String) -> AnyElement,
) -> AnyElement {
    let Some(who) = ticket.assignee.clone() else {
        return line(
            glyph(Glyph::Person, ICON, ink(theme, T2)).into_any_element(),
            "nobody".to_owned(),
        );
    };
    if who.contains('/') {
        return line(
            glyph(Glyph::Agents, ICON, ink(theme, T2)).into_any_element(),
            "sub-agent".to_owned(),
        );
    }
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
                kind: AgentKind::named(
                    who.trim_end_matches(|c: char| c.is_ascii_digit() || c == '-'),
                ),
                instance: 0,
                status,
            };
            avatar("boardy-assignee", &agent, AvatarSize::Feed, theme).into_any_element()
        }
        None => disc(
            &who.get(..1).unwrap_or_default().to_uppercase(),
            tone(&who, theme),
        )
        .into_any_element(),
    };
    line(mark, who)
}
