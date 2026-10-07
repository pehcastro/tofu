use std::time::Instant;

use desk_motion::{Presence, reduced_motion};
use desk_ui::component::icon_button;
use desk_ui::components::avatar::{
    Agent, AgentKind, AgentStatus, AvatarSize, Person, avatar, avatar_stack, spinner,
};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{caption, inner_card};
use desk_ui::components::chip::{
    Chip, GitStatus, Tone, agent_pill, badge, code, file_chip, flat_chip, git_name, kbd, mention,
    trace,
};
use desk_ui::components::form::{SelectableText, TextArea, TextInput, segmented, switch};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::overlay::{Dropdown, MenuItem, menu};
use desk_ui::components::paint::{glyph, ink};
use desk_ui::components::size::CAPTION_TEXT;
use desk_ui::icon::Icon;
use desk_ui::metrics::ICON_SMALL;
use desk_ui::theme::Theme;
use gpui::{
    ClickEvent, Context, Div, Entity, SharedString, Window, deferred, div, prelude::*, px, rems,
};

use super::Book;
use super::kit::{block, label, named, present, spread, toggle};
use crate::catalog::Page;

const SIZES: [(AvatarSize, &str); 4] = [
    (AvatarSize::Feed, "feed"),
    (AvatarSize::List, "list"),
    (AvatarSize::Row, "row"),
    (AvatarSize::Large, "large"),
];
const KIND_WIDTH: f32 = 80.0;
const GIT: [GitStatus; 4] = [
    GitStatus::Modified,
    GitStatus::Added,
    GitStatus::Deleted,
    GitStatus::Untracked,
];
const FIELDS: [&str; 2] = ["Filter agents", "Branch name"];
const BRANCHES: [&str; 3] = ["main", "develop", "DESK-041"];
const WORKSPACES: [&str; 4] = ["work", "editor", "data", "review DESK-003"];
const FLAT_MENU_DROP: f32 = 30.0;
const PROSE: &str = "Drag across this sentence to select part of it, Ctrl C copies it, and Ctrl V pastes it into a field above. Double click selects a word.";
const SEGMENTS: [&[&str]; 3] = [
    &["Table", "Feed", "Map"],
    &["Light", "Dark"],
    &["All", "Read", "Edit", "Run", "Ask"],
];
const SEGMENT_IDS: [&str; 3] = ["segmented-1", "segmented-2", "segmented-3"];
const SWITCHES: [&str; 3] = ["Auto mode", "Notifications", "Reduce noise"];
const SPINNER_NOTE: &str = "Work in flight, turned by the frame clock.";
const PANEL_NOTE: &str = "A panel. Its close button closes it.";
const BUTTONS_NOTE: &str = "Every button below counts and says so in a toast.";
const WORK_NOTE: &str = "Body text sits right under the work chip. Open its menu: the menu covers these words and none of them show through it, and it is drawn above the card instead of being cut at its edge.";
const AREA_PLACEHOLDER: &str = "Write a message. Enter sends, Shift Enter breaks the line.";
const CLOSABLE: [&str; 3] = ["go-dev 2", "rust-dev 3", "bench"];
const STACK_SHOWN: usize = 4;

pub(super) struct ControlsState {
    fields: Vec<Entity<TextInput>>,
    area: Entity<TextArea>,
    submitted: Vec<SharedString>,
    closed: [bool; CLOSABLE.len()],
    restores: usize,
    work_note: Entity<SelectableText>,
    prose: Entity<SelectableText>,
    spinner_note: Entity<SelectableText>,
    panel_note: Entity<SelectableText>,
    buttons_note: Entity<SelectableText>,
    branch: Entity<Dropdown>,
    segments: [usize; 3],
    switches: [bool; 3],
    pub(super) clicks: usize,
    flat_menu: Presence,
    flat_pick: usize,
    mention_shown: bool,
    panel_shown: bool,
}

impl ControlsState {
    pub(super) fn new(presence: Presence, window: &mut Window, cx: &mut Context<Book>) -> Self {
        let fields = FIELDS
            .map(|name| {
                let field = TextInput::new(name.into(), window, cx);
                cx.observe(&field, |_, _, cx| cx.notify()).detach();
                field
            })
            .to_vec();
        let book = cx.weak_entity();
        let area = cx.new(|cx| {
            TextArea::new(AREA_PLACEHOLDER.into(), window, cx).on_submit(move |text, _, cx| {
                let text = SharedString::from(text.to_owned());
                let book = book.clone();
                cx.defer(move |cx| {
                    if let Some(book) = book.upgrade() {
                        book.update(cx, |this, cx| {
                            this.controls.submitted.push(text);
                            this.controls.area.update(cx, |area, cx| area.clear(cx));
                            cx.notify();
                        });
                    }
                });
            })
        });
        ControlsState {
            fields,
            area,
            submitted: Vec::new(),
            closed: [false; CLOSABLE.len()],
            restores: 0,
            work_note: SelectableText::new(WORK_NOTE.into(), cx),
            prose: SelectableText::new(PROSE.into(), cx),
            spinner_note: SelectableText::new(SPINNER_NOTE.into(), cx),
            panel_note: SelectableText::new(PANEL_NOTE.into(), cx),
            buttons_note: SelectableText::new(BUTTONS_NOTE.into(), cx),
            branch: Dropdown::new(BRANCHES.map(SharedString::from).to_vec(), cx),
            segments: [0, 1, 2],
            switches: [true, true, false],
            clicks: 0,
            flat_menu: presence,
            flat_pick: 0,
            mention_shown: true,
            panel_shown: true,
        }
    }

    pub(super) fn render(
        &mut self,
        page: Page,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Book>,
    ) -> Div {
        match page {
            Page::Avatar => avatars(&self.spinner_note, theme),
            Page::Chip => self.chips(theme, window, cx),
            Page::Button => self.buttons(theme, cx),
            Page::Input => self.inputs(theme, cx),
            Page::Segmented => self.segmented(theme, cx),
            Page::Switch => self.switches(theme, cx),
            _ => div(),
        }
    }

    pub(super) fn key(&mut self, page: Page, key: &str) -> bool {
        match (page, key) {
            (Page::Chip, "escape") => self.flat_menu.set_open(false, Instant::now()),
            _ => return false,
        }
        true
    }
}

fn avatars(spinner_note: &Entity<SelectableText>, theme: &Theme) -> Div {
    let grid = AgentKind::ALL.into_iter().enumerate().map(|(at, kind)| {
        spread(theme)
            .child(div().w(px(KIND_WIDTH)).child(kind.name()))
            .children(
                AgentStatus::ALL
                    .into_iter()
                    .enumerate()
                    .map(move |(column, status)| {
                        let agent = Agent {
                            kind,
                            instance: 1,
                            status,
                        };
                        avatar(
                            ("avatar", at * AgentStatus::ALL.len() + column),
                            &agent,
                            AvatarSize::Row,
                            theme,
                        )
                    }),
            )
    });
    let sizes = AgentStatus::ALL
        .into_iter()
        .enumerate()
        .map(|(at, status)| {
            spread(theme)
                .gap_4()
                .child(div().w(px(KIND_WIDTH)).child(status.group()))
                .children(
                    SIZES
                        .into_iter()
                        .enumerate()
                        .map(move |(column, (size, name))| {
                            let agent = Agent {
                                kind: AgentKind::ALL[at % AgentKind::ALL.len()],
                                instance: 2,
                                status,
                            };
                            named(
                                name,
                                theme,
                                avatar(("sized", at * SIZES.len() + column), &agent, size, theme),
                            )
                        }),
                )
        });
    let people = AgentKind::ALL.map(|kind| Person {
        name: kind.name().into(),
        kind,
    });
    div()
        .flex()
        .flex_col()
        .gap_3()
        .child(block(
            "Stack: 7 people, 4 shown. Hover it to fan it out, hover one for its name",
            theme,
            spread(theme).child(avatar_stack(
                &people,
                STACK_SHOWN,
                "avatar-stack",
                AvatarSize::Row,
                theme,
            )),
        ))
        .child(block(
            "Every status at every size",
            theme,
            div().flex().flex_col().gap_3().children(sizes),
        ))
        .child(block(
            "Every kind: working, asking, failed, finished",
            theme,
            div().flex().flex_col().gap_2().children(grid),
        ))
        .child(block(
            "Spinner",
            theme,
            spread(theme)
                .child(spinner("spinner", theme))
                .child(note(spinner_note, theme)),
        ))
}

fn note(text: &Entity<SelectableText>, theme: &Theme) -> Div {
    div()
        .text_color(ink(theme, CAPTION_TEXT))
        .child(text.clone())
}

impl ControlsState {
    fn chips(&self, theme: &Theme, window: &mut Window, cx: &mut Context<Book>) -> Div {
        let reduced = reduced_motion(cx);
        let files = GIT.map(|status| {
            file_chip(
                glyph(Glyph::File, ICON_SMALL, ink(theme, CAPTION_TEXT)),
                git_name("loop.go", Some(status), theme),
                theme,
            )
        });
        let picked = WORKSPACES.get(self.flat_pick).copied().unwrap_or_default();
        let items = WORKSPACES.map(|label| MenuItem::Action { label, keys: None });
        let flat_menu = div()
            .absolute()
            .top(px(FLAT_MENU_DROP))
            .left_0()
            .child(menu(
                "flat-menu",
                &items,
                theme,
                cx.listener(|this, at: &usize, _, cx| {
                    this.controls.flat_pick = *at;
                    this.controls.flat_menu.set_open(false, Instant::now());
                    cx.notify();
                }),
            ));
        let flat = div()
            .relative()
            .child(flat_chip("flat-chip", picked, theme).on_click(cx.listener(
                |this, _: &ClickEvent, _, cx| {
                    toggle(&mut this.controls.flat_menu);
                    cx.notify();
                },
            )))
            .children(
                present(&self.flat_menu, reduced, window, flat_menu)
                    .map(|menu| deferred(menu).priority(1)),
            );
        let book = cx.weak_entity();
        let spread_gap = rems(0.5).to_pixels(window.rem_size());
        let closable = CLOSABLE
            .iter()
            .zip(self.closed)
            .enumerate()
            .filter(|(_, (_, closed))| !closed)
            .map(|(at, (name, _))| {
                let book = book.clone();
                Chip::new(("closable", self.restores * CLOSABLE.len() + at), *name)
                    .gap_after(spread_gap)
                    .on_close(move |_, cx| {
                        if let Some(book) = book.upgrade() {
                            book.update(cx, |this, cx| {
                                if let Some(slot) = this.controls.closed.get_mut(at) {
                                    *slot = true;
                                    cx.notify();
                                }
                            });
                        }
                    })
            });
        let closable = spread(theme).children(closable).child(
            button("chips-restore", "Restore", None, ButtonKind::Plain, theme).on_click(
                cx.listener(|this, _: &ClickEvent, _, cx| {
                    this.controls.closed = [false; CLOSABLE.len()];
                    this.controls.restores += 1;
                    cx.notify();
                }),
            ),
        );
        let removable = if self.mention_shown {
            mention(
                "mention",
                "go-dev 2",
                theme,
                cx.listener(|this, _: &ClickEvent, _, cx| {
                    this.controls.mention_shown = false;
                    cx.notify();
                }),
            )
        } else {
            div().child(
                button(
                    "mention-reset",
                    "Bring the mention back",
                    None,
                    ButtonKind::Plain,
                    theme,
                )
                .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                    this.controls.mention_shown = true;
                    cx.notify();
                })),
            )
        };
        let chips = spread(theme)
            .items_start()
            .child(named(
                "chip with a dropdown: click it, arrows, Enter, Escape",
                theme,
                self.branch.clone(),
            ))
            .child(named("mention: click its \u{d7}", theme, removable))
            .child(named("code", theme, code("go test ./internal/turn", theme)))
            .child(named("kbd", theme, kbd("Ctrl K", theme)));
        let pills = spread(theme).children(
            AgentKind::ALL
                .into_iter()
                .map(|kind| agent_pill(kind, kind.name(), theme)),
        );
        let badges = spread(theme)
            .child(badge("36", theme))
            .children(
                [
                    (Tone::Link, "running"),
                    (Tone::Live, "live"),
                    (Tone::Warn, "asking"),
                    (Tone::Added, "+12"),
                    (Tone::Deleted, "\u{2212}3"),
                ]
                .map(|(tone, text)| badge(text, theme).text_color(tone.color(theme))),
            )
            .child(named(
                "trace",
                theme,
                trace("trace", "go-dev 2".into(), theme).on_click(Book::clicked("trace", cx)),
            ))
            .child(named("caption", theme, caption("caption", theme)));
        div()
            .flex()
            .flex_col()
            .gap_3()
            .child(block(
                "Work chip over body text: open it and pick",
                theme,
                div()
                    .flex()
                    .flex_col()
                    .gap_2()
                    .child(named(format!("{picked} picked"), theme, flat))
                    .child(note(&self.work_note, theme)),
            ))
            .child(block(
                "Closable chips: click an \u{d7}, Restore brings them back",
                theme,
                closable,
            ))
            .child(block("Chip", theme, chips))
            .child(block(
                "File chip: modified, added, deleted, untracked",
                theme,
                spread(theme).children(files),
            ))
            .child(block("Agent pill", theme, pills))
            .child(block("Badge, tone and trace", theme, badges))
    }

    fn buttons(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let plain = spread(theme)
            .gap_4()
            .child(named(
                "label",
                theme,
                button("plain", "Reset layout", None, ButtonKind::Plain, theme)
                    .on_click(Book::clicked("Reset layout", cx)),
            ))
            .child(named(
                "lead glyph",
                theme,
                button(
                    "plain-lead",
                    "New chat",
                    Some(Glyph::Chat),
                    ButtonKind::Plain,
                    theme,
                )
                .on_click(Book::clicked("New chat", cx)),
            ));
        let primary = spread(theme)
            .gap_4()
            .child(named(
                "label",
                theme,
                button("primary", "Allow once", None, ButtonKind::Primary, theme)
                    .on_click(Book::clicked("Allow once", cx)),
            ))
            .child(named(
                "lead glyph",
                theme,
                button(
                    "primary-lead",
                    "Send",
                    Some(Glyph::Send),
                    ButtonKind::Primary,
                    theme,
                )
                .on_click(Book::clicked("Send", cx)),
            ));
        let icons = spread(theme).gap_4().children(
            [
                (Icon::Plus, "New workspace"),
                (Icon::Search, "Search"),
                (Icon::Sidebar, "Sidebar"),
            ]
            .map(|(shown, name)| {
                named(
                    name,
                    theme,
                    icon_button(name, shown, name, theme).on_click(Book::clicked(name, cx)),
                )
            }),
        );
        let closable = if self.panel_shown {
            inner_card(theme)
                .flex_none()
                .flex()
                .flex_row()
                .items_center()
                .gap_3()
                .p_2()
                .child(note(&self.panel_note, theme))
                .child(
                    icon_button("Close", Icon::Close, "Close", theme).on_click(cx.listener(
                        |this, _: &ClickEvent, _, cx| {
                            this.controls.panel_shown = false;
                            cx.notify();
                        },
                    )),
                )
        } else {
            div().child(
                button(
                    "panel-reset",
                    "Bring the panel back",
                    None,
                    ButtonKind::Plain,
                    theme,
                )
                .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                    this.controls.panel_shown = true;
                    cx.notify();
                })),
            )
        };
        let icons = icons.child(named("Close", theme, closable));
        let together = spread(theme)
            .child(
                button("row-cancel", "Deny", None, ButtonKind::Plain, theme)
                    .on_click(Book::clicked("Deny", cx)),
            )
            .child(
                button("row-allow", "Allow once", None, ButtonKind::Primary, theme)
                    .on_click(Book::clicked("Allow once", cx)),
            );
        div()
            .flex()
            .flex_col()
            .gap_3()
            .child(
                note(&self.buttons_note, theme)
                    .flex()
                    .gap_1()
                    .child(format!("{} clicks so far.", self.clicks)),
            )
            .child(block("Plain", theme, plain))
            .child(block("Primary", theme, primary))
            .child(block("Icon button", theme, icons))
            .child(block("Side by side, as in an ask bar", theme, together))
    }

    fn inputs(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let typed = self
            .fields
            .first()
            .map(|field| field.read(cx).text().to_string())
            .unwrap_or_default();
        let fields = spread(theme).gap_4().items_start().children(
            self.fields
                .iter()
                .zip(FIELDS)
                .map(|(field, name)| named(name, theme, field.clone())),
        );
        div()
            .flex()
            .flex_col()
            .gap_3()
            .child(block(
                "Click a field and type. Shift and arrows or a drag select, Ctrl X C V cut copy paste, Ctrl Z undoes.",
                theme,
                div()
                    .flex()
                    .flex_col()
                    .gap_2()
                    .child(fields)
                    .child(label(format!("The first field holds: {typed:?}"), theme)),
            ))
            .child(block(
                "Text area: grows to 8 lines, then scrolls. Enter sends, Shift Enter breaks the line.",
                theme,
                div()
                    .flex()
                    .flex_col()
                    .gap_2()
                    .child(self.area.clone())
                    .child(label(
                        format!("{} sent", self.submitted.len()),
                        theme,
                    ))
                    .children(
                        self.submitted
                            .iter()
                            .map(|sent| inner_card(theme).p_2().child(sent.clone())),
                    ),
            ))
            .child(block(
                "Selectable text",
                theme,
                self.prose.clone(),
            ))
    }

    fn segmented(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let live = SEGMENTS
            .iter()
            .zip(SEGMENT_IDS)
            .enumerate()
            .map(|(at, (options, id))| {
                let chosen = self.segments.get(at).copied().unwrap_or_default();
                let picked = options.get(chosen).copied().unwrap_or_default();
                named(
                    format!("{} options, {picked} chosen", options.len()),
                    theme,
                    segmented(
                        id,
                        options,
                        chosen,
                        theme,
                        cx.listener(move |this, ix: &usize, _, cx| {
                            if let Some(slot) = this.controls.segments.get_mut(at) {
                                *slot = *ix;
                                cx.notify();
                            }
                        }),
                    ),
                )
            });
        div().flex().flex_col().gap_3().child(block(
            "Click to choose",
            theme,
            spread(theme).gap_6().items_start().children(live),
        ))
    }

    fn switches(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let live = SWITCHES.iter().enumerate().map(|(at, name)| {
            let on = self.switches.get(at).copied().unwrap_or_default();
            named(
                if on { "on" } else { "off" },
                theme,
                switch(("switch", at), name, on, theme).on_click(cx.listener(
                    move |this, _: &ClickEvent, _, cx| {
                        if let Some(slot) = this.controls.switches.get_mut(at) {
                            *slot = !*slot;
                            cx.notify();
                        }
                    },
                )),
            )
        });
        div().flex().flex_col().gap_3().child(block(
            "Click to flip",
            theme,
            spread(theme).gap_6().items_start().children(live),
        ))
    }
}
