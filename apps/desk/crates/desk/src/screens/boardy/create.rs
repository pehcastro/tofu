use std::collections::BTreeSet;
use std::time::Instant;

use desk_core::boardy::{BoardyStore, COLUMNS};
use desk_core::protocol::boardy::{CreateRequest, Ticket};
use desk_core::protocol::request::BoardyCreate;
use desk_core::protocol::{BoardyPriority, BoardyStatus as Status, BoardyTicketType};
use desk_motion::Phase;
use desk_ui::component::icon_button;
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::chip::chip;
use desk_ui::components::form::{TextArea, switch};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::overlay::{MenuButton, MenuItem};
use desk_ui::components::paint::{drop, glyph, ink, ring, tint};
use desk_ui::components::priority_icon::{Priority as Bars, priority_icon};
use desk_ui::components::size::{
    BADGE_FILL, CAPTION_TEXT, CHIP, CHIP_FILL, CHIP_PAD, CHIP_TEXT, FONT_BODY, FONT_SMALL,
    RADIUS_CHIP, RADIUS_POP, SHEET_RING, T1, T2,
};
use desk_ui::components::status_icon::{TicketStatus, status_icon};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::icon::Icon;
use desk_ui::metrics::ICON_SMALL;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    App, ClickEvent, Context, Div, Entity, FocusHandle, Focusable, FontWeight, MouseButton,
    MouseDownEvent, SharedString, Stateful, WeakEntity, Window, div, prelude::*, px, relative,
};

use super::look::{self, kit, noun};
use super::{Boardy, MS_PER_SECOND};

const CARD_WIDTH: f32 = 640.0;
const CARD_WIDE: f32 = 880.0;
const CARD_SHARE: f32 = 0.9;
const CARD_TOP: f32 = 96.0;
const PAD_X: f32 = 16.0;
const PAD_TOP: f32 = 14.0;
const ROW_Y: f32 = 12.0;
const GAP: f32 = 6.0;
const ABOUT_MIN: f32 = 72.0;
const TITLE_LINES: usize = 3;
const ABOUT_LINES: usize = 10;
const SCRIM: f32 = 0.35;
const SHADOW_Y: f32 = 16.0;
const SHADOW_BLUR: f32 = 24.0;
const PRIORITIES: [BoardyPriority; 5] = [
    BoardyPriority::P0,
    BoardyPriority::P1,
    BoardyPriority::P2,
    BoardyPriority::P3,
    BoardyPriority::P4,
];

#[derive(Clone, Copy)]
enum Lead {
    Status(TicketStatus),
    Priority(Bars),
    Glyph(Glyph),
}

struct Known {
    assignees: Vec<String>,
    epics: Vec<String>,
    labels: Vec<String>,
}

struct Pickers {
    status: Entity<MenuButton>,
    priority: Entity<MenuButton>,
    assignee: Entity<MenuButton>,
    epic: Entity<MenuButton>,
    labels: Entity<MenuButton>,
}

pub(super) struct Draft {
    board: String,
    epic_word: String,
    status: Status,
    priority: BoardyPriority,
    assignee: Option<String>,
    epic: Option<String>,
    labels: Vec<String>,
    known: Known,
    title: Entity<TextArea>,
    about: Entity<TextArea>,
    pickers: Pickers,
    more: bool,
    wide: bool,
}

impl Draft {
    fn new(
        board: String,
        status: Status,
        store: &BoardyStore,
        window: &mut Window,
        cx: &mut Context<Boardy>,
    ) -> Draft {
        let words = &store.words;
        let tickets = store.tickets(&board);
        let known = Known {
            assignees: distinct(
                tickets
                    .iter()
                    .filter_map(|ticket| ticket.assignee.as_deref())
                    .filter(|who| !who.contains('/')),
            ),
            epics: distinct(tickets.iter().filter_map(|ticket| ticket.epic.as_deref())),
            labels: distinct(
                tickets
                    .iter()
                    .flat_map(|ticket| ticket.labels.iter().map(String::as_str)),
            ),
        };
        let weak = cx.weak_entity();
        let next = weak.clone();
        let title = cx.new(|cx| {
            TextArea::new(
                format!("{} title", noun(&words.ticket, false)).into(),
                window,
                cx,
            )
            .bare()
            .max_lines(TITLE_LINES)
            .on_submit(move |_, window, cx| {
                let moved = next.update(cx, |boardy, cx| {
                    if let Some(draft) = &boardy.draft {
                        draft.about.focus_handle(cx).focus(window, cx);
                    }
                });
                if let Err(error) = moved {
                    eprintln!("desk: boardy closed before the description took focus: {error}");
                }
            })
        });
        let about = cx.new(|cx| {
            TextArea::new("Add description...".into(), window, cx)
                .bare()
                .max_lines(ABOUT_LINES)
        });
        let named = |names: &[String], none: String| {
            std::iter::once(MenuItem::action(none))
                .chain(names.iter().map(|name| MenuItem::action(name.clone())))
                .collect()
        };
        let pickers = Pickers {
            status: picker(
                COLUMNS
                    .iter()
                    .map(|status| MenuItem::action(look::label(status).to_owned()))
                    .collect(),
                &weak,
                |draft, at| {
                    if let Some(status) = COLUMNS.get(at) {
                        draft.status = status.clone();
                    }
                },
                cx,
            ),
            priority: picker(
                PRIORITIES
                    .iter()
                    .filter_map(look::priority)
                    .map(|bars| MenuItem::action(bars.label()))
                    .collect(),
                &weak,
                |draft, at| {
                    if let Some(priority) = PRIORITIES.get(at) {
                        draft.priority = priority.clone();
                    }
                },
                cx,
            ),
            assignee: picker(
                named(&known.assignees, "Nobody".to_owned()),
                &weak,
                |draft, at| {
                    draft.assignee = picked(&draft.known.assignees, at);
                },
                cx,
            ),
            epic: picker(
                named(&known.epics, format!("No {}", words.epic)),
                &weak,
                |draft, at| {
                    draft.epic = picked(&draft.known.epics, at);
                },
                cx,
            ),
            labels: picker(Vec::new(), &weak, toggle_label, cx),
        };
        Draft {
            board,
            epic_word: words.epic.clone(),
            status,
            priority: BoardyPriority::P2,
            assignee: None,
            epic: None,
            labels: Vec::new(),
            known,
            title,
            about,
            pickers,
            more: false,
            wide: false,
        }
    }

    fn dress(&self, cx: &mut App) {
        let status = kit(&self.status).map_or(Lead::Glyph(Glyph::Inbox), Lead::Status);
        dressed(
            &self.pickers.status,
            "boardy-create-status",
            status,
            look::label(&self.status).to_owned(),
            cx,
        );
        let bars = look::priority(&self.priority);
        dressed(
            &self.pickers.priority,
            "boardy-create-priority",
            bars.map_or(Lead::Glyph(Glyph::BarChart), Lead::Priority),
            bars.map_or("Priority", Bars::label).to_owned(),
            cx,
        );
        dressed(
            &self.pickers.assignee,
            "boardy-create-assignee",
            Lead::Glyph(Glyph::Person),
            self.assignee
                .clone()
                .unwrap_or_else(|| "Assignee".to_owned()),
            cx,
        );
        dressed(
            &self.pickers.epic,
            "boardy-create-epic",
            Lead::Glyph(Glyph::Epic),
            self.epic
                .clone()
                .unwrap_or_else(|| noun(&self.epic_word, false)),
            cx,
        );
        let labels = match self.labels.as_slice() {
            [] => "Labels".to_owned(),
            [one] => one.clone(),
            many => format!("{} labels", many.len()),
        };
        dressed(
            &self.pickers.labels,
            "boardy-create-labels",
            Lead::Glyph(Glyph::Label),
            labels,
            cx,
        );
        let items = match self.known.labels.is_empty() {
            true => vec![MenuItem::action("No labels on this board yet").enabled(false)],
            false => self
                .known
                .labels
                .iter()
                .map(|name| {
                    let item = MenuItem::action(name.clone());
                    match self.labels.contains(name) {
                        true => item.icon(Glyph::Check),
                        false => item,
                    }
                })
                .collect(),
        };
        self.pickers
            .labels
            .update(cx, |button, cx| button.items(items, cx));
    }

    fn ticket(&self, title: String, cx: &App) -> Ticket {
        let now = chrono::Utc::now().to_rfc3339_opts(chrono::SecondsFormat::Secs, true);
        Ticket {
            acceptance: Vec::new(),
            assignee: self.assignee.clone(),
            blocks: Vec::new(),
            component: None,
            created: now.clone(),
            depends: Vec::new(),
            duplicates: Vec::new(),
            epic: self.epic.clone(),
            id: String::new(),
            labels: self.labels.clone(),
            log: String::new(),
            owns: Vec::new(),
            parent: None,
            points: None,
            priority: self.priority.clone(),
            problem: self.about.read(cx).text().trim().to_owned(),
            questions: Vec::new(),
            reason: None,
            relates: Vec::new(),
            scope: String::new(),
            size: None,
            spec: None,
            sprint: None,
            status: self.status.clone(),
            title,
            r#type: BoardyTicketType::Task,
            updated: now,
        }
    }

    fn title_focus(&self, cx: &App) -> FocusHandle {
        self.title.focus_handle(cx)
    }
}

fn distinct<'a>(names: impl Iterator<Item = &'a str>) -> Vec<String> {
    names
        .filter(|name| !name.is_empty())
        .collect::<BTreeSet<_>>()
        .into_iter()
        .map(str::to_owned)
        .collect()
}

fn picked(names: &[String], at: usize) -> Option<String> {
    at.checked_sub(1).and_then(|at| names.get(at)).cloned()
}

fn toggle_label(draft: &mut Draft, at: usize) {
    let Some(name) = draft.known.labels.get(at) else {
        return;
    };
    match draft.labels.iter().position(|held| held == name) {
        Some(held) => {
            draft.labels.remove(held);
        }
        None => draft.labels.push(name.clone()),
    }
}

fn picker(
    items: Vec<MenuItem>,
    weak: &WeakEntity<Boardy>,
    set: fn(&mut Draft, usize),
    cx: &mut App,
) -> Entity<MenuButton> {
    let button = MenuButton::new(SharedString::default(), items, cx);
    let weak = weak.clone();
    button.update(cx, |button, _| {
        button.on_pick(move |at, _, cx| {
            let landed = weak.update(cx, |boardy, cx| {
                if let Some(draft) = boardy.draft.as_mut() {
                    set(draft, *at);
                    draft.dress(cx);
                }
                cx.notify();
            });
            if let Err(error) = landed {
                eprintln!("desk: boardy closed before a pick landed: {error}");
            }
        });
    });
    button
}

fn dressed(button: &Entity<MenuButton>, id: &'static str, lead: Lead, text: String, cx: &mut App) {
    let text = SharedString::from(text);
    button.update(cx, |button, cx| {
        button.trigger(move |open, theme| face(id, lead, text.clone(), open, theme));
        cx.notify();
    });
}

fn face(
    id: &'static str,
    lead: Lead,
    text: SharedString,
    open: bool,
    theme: &Theme,
) -> Stateful<Div> {
    let lit = ink(theme, CHIP_FILL + BADGE_FILL);
    let lead = match lead {
        Lead::Status(status) => status_icon(status, px(ICON_SMALL)).into_any_element(),
        Lead::Priority(bars) => priority_icon(bars).into_any_element(),
        Lead::Glyph(mark) => glyph(mark, ICON_SMALL, ink(theme, CAPTION_TEXT)).into_any_element(),
    };
    div()
        .id(id)
        .flex()
        .flex_none()
        .items_center()
        .gap_1p5()
        .h(px(CHIP))
        .px(px(CHIP_PAD))
        .rounded(px(RADIUS_CHIP))
        .border_1()
        .border_color(ink(theme, CHIP_FILL))
        .cursor_pointer()
        .text_size(px(FONT_SMALL))
        .font_weight(FontWeight::MEDIUM)
        .text_color(ink(theme, CHIP_TEXT))
        .when(open, |face| face.bg(lit))
        .hover(move |style| style.bg(lit))
        .child(lead)
        .child(text)
}

impl Boardy {
    pub(super) fn drafting(&self) -> bool {
        self.draft.is_some()
            && matches!(
                self.shown.phase(Instant::now()),
                Phase::Opening | Phase::Open
            )
    }

    pub(super) fn begin_draft(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some((board, status)) = self.wanted.take() else {
            return;
        };
        let draft = Draft::new(board, status, &self.store, window, cx);
        draft.dress(cx);
        draft.title_focus(cx).focus(window, cx);
        self.draft = Some(draft);
        self.shown.set_open(true, Instant::now());
    }

    pub(super) fn close_draft(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.shown.set_open(false, Instant::now());
        self.focus.focus(window, cx);
        cx.notify();
    }

    pub(super) fn submit(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if !self.drafting() {
            return;
        }
        let Some(draft) = self.draft.as_ref() else {
            return;
        };
        let title = draft
            .title
            .read(cx)
            .text()
            .split_whitespace()
            .collect::<Vec<_>>()
            .join(" ");
        if title.is_empty() {
            return draft.title_focus(cx).focus(window, cx);
        }
        let params = CreateRequest {
            board: Some(draft.board.clone()),
            ticket: draft.ticket(title, cx),
        };
        match draft.more {
            true => {
                draft.title.update(cx, |area, cx| area.clear(cx));
                draft.about.update(cx, |area, cx| area.clear(cx));
                draft.title_focus(cx).focus(window, cx);
            }
            false => self.close_draft(window, cx),
        }
        let sent = Instant::now();
        self.send::<BoardyCreate>(params, cx, move |boardy, reply, cx| {
            if let Ok(answer) = &reply {
                eprintln!(
                    "desk: boardy: created {} in {:.1}ms",
                    answer.ticket.id,
                    sent.elapsed().as_secs_f64() * MS_PER_SECOND
                );
            }
            boardy.landed(reply, cx);
        });
    }

    fn reshape(&mut self, cx: &mut Context<Self>, change: fn(&mut Draft)) {
        if let Some(draft) = self.draft.as_mut() {
            change(draft);
        }
        cx.notify();
    }
}

pub(super) fn modal(
    boardy: &Boardy,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<Boardy>,
) -> Option<Div> {
    let draft = boardy.draft.as_ref()?;
    let now = Instant::now();
    let phase = boardy.shown.phase(now);
    if matches!(phase, Phase::Opening | Phase::Closing) {
        window.request_animation_frame();
    }
    let shown = boardy.shown.progress(now);
    let open = matches!(phase, Phase::Opening | Phase::Open);
    let words = &boardy.store.words;
    let shade = theme.color(ColorToken::Shadow);
    let wide = tooltip(
        "boardy-create-wide-tip",
        icon_button("boardy-create-wide", Icon::Expand, "Expand", theme).on_click(cx.listener(
            |boardy, _: &ClickEvent, _, cx| boardy.reshape(cx, |draft| draft.wide = !draft.wide),
        )),
        Edge::Frame,
        if draft.wide { "Shrink" } else { "Expand" },
        theme,
        window,
        cx,
    );
    let close = tooltip(
        "boardy-create-close-tip",
        icon_button("boardy-create-close", Icon::Close, "Close", theme).on_click(
            cx.listener(|boardy, _: &ClickEvent, window, cx| boardy.close_draft(window, cx)),
        ),
        Edge::Frame,
        "Close  Esc",
        theme,
        window,
        cx,
    );
    let header = div()
        .flex()
        .items_center()
        .gap_2()
        .px(px(PAD_X))
        .pt(px(PAD_TOP))
        .child(chip(draft.board.clone(), None, theme))
        .child(div().text_color(ink(theme, CAPTION_TEXT)).child("›"))
        .child(
            div()
                .text_color(ink(theme, T2))
                .child(format!("New {}", words.ticket)),
        )
        .child(div().flex_1())
        .child(wide)
        .child(close);
    let chips = div()
        .flex()
        .flex_wrap()
        .gap(px(GAP))
        .px(px(PAD_X))
        .py(px(ROW_Y))
        .child(draft.pickers.status.clone())
        .child(draft.pickers.priority.clone())
        .child(draft.pickers.assignee.clone())
        .child(draft.pickers.epic.clone())
        .child(draft.pickers.labels.clone());
    let create = button(
        "boardy-create-go",
        format!("Create {}", words.ticket),
        None,
        ButtonKind::Primary,
        theme,
    )
    .on_click(cx.listener(|boardy, _: &ClickEvent, window, cx| {
        boardy.submit(window, cx);
    }));
    let footer = div()
        .flex()
        .items_center()
        .justify_end()
        .gap(px(ROW_Y))
        .px(px(PAD_X))
        .py(px(ROW_Y))
        .border_t_1()
        .border_color(ink(theme, SHEET_RING))
        .child(
            switch("boardy-create-more", "Create more", draft.more, theme).on_click(cx.listener(
                |boardy, _: &ClickEvent, _, cx| {
                    boardy.reshape(cx, |draft| draft.more = !draft.more)
                },
            )),
        )
        .child(tooltip(
            "boardy-create-go-tip",
            create,
            Edge::Frame,
            "Ctrl+Enter",
            theme,
            window,
            cx,
        ));
    let card = div()
        .id("boardy-create")
        .occlude()
        .flex()
        .flex_col()
        .w(px(if draft.wide { CARD_WIDE } else { CARD_WIDTH }))
        .max_w(relative(CARD_SHARE))
        .bg(theme.color(ColorToken::ToastFill))
        .rounded(px(RADIUS_POP))
        .shadow(vec![
            ring(ink(theme, SHEET_RING)),
            drop(tint(shade, shade.alpha * shown), SHADOW_Y, SHADOW_BLUR),
        ])
        .text_size(px(FONT_BODY))
        .text_color(ink(theme, T1))
        .opacity(shown)
        .child(header)
        .child(
            div()
                .px(px(PAD_X))
                .pt(px(ROW_Y))
                .font_weight(FontWeight::SEMIBOLD)
                .child(draft.title.clone()),
        )
        .child(
            div()
                .px(px(PAD_X))
                .pt_1()
                .min_h(px(ABOUT_MIN))
                .child(draft.about.clone()),
        )
        .child(chips)
        .child(footer);
    let scrim = div()
        .id("boardy-create-scrim")
        .absolute()
        .top_0()
        .left_0()
        .size_full()
        .bg(tint(shade, SCRIM * shown))
        .occlude()
        .when(open, |scrim| {
            scrim.on_mouse_down(
                MouseButton::Left,
                cx.listener(|boardy, _: &MouseDownEvent, window, cx| {
                    boardy.close_draft(window, cx);
                }),
            )
        });
    Some(
        div()
            .absolute()
            .top_0()
            .left_0()
            .size_full()
            .flex()
            .flex_col()
            .items_center()
            .pt(px(CARD_TOP))
            .child(scrim)
            .child(card),
    )
}
