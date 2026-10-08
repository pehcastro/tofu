use std::time::Duration;

use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{inner_card, shell};
use desk_ui::components::form::{SelectableText, TextInput};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::overlay::{
    Dropdown, DropdownTrigger, MenuButton, MenuItem, Placement, Popover, Side, ToastKind, actions,
    context_menu,
};
use desk_ui::components::sheet::{Drawer, Sheet};
use desk_ui::icon::Icon;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    ClickEvent, Context, Div, ElementId, Entity, SharedString, Stateful, Window, div, prelude::*,
    px,
};

use super::Book;
use super::kit::{TILE_HEIGHT, block, efforts, label, models, named, spread, titled};
use crate::catalog::Page;

const POPOVER_ROOM: f32 = 160.0;
const POPOVER_SIDES: [(Side, &str); 4] = [
    (Side::Top, "Top"),
    (Side::Right, "Right"),
    (Side::Bottom, "Bottom"),
    (Side::Left, "Left"),
];
const EDGE_PAGE_HEIGHT: f32 = 2.0 * TILE_HEIGHT;
const AREA_WIDTH: f32 = 320.0;
const FIELD: &str = "Type here, select it, right click";
const PROSE: &str = "Drag across this sentence, then right click it to copy or select all.";
const MENU_TRIGGERS: [&str; 2] = ["Menu, top", "Menu, bottom: opens upward"];
const MENU: [&str; 5] = ["Rename", "Pin", "Lock", "Reset layout", "Reset to preset"];
const SPAWNS: [&str; 4] = ["Chat", "Sub-agents", "File edits", "Shells"];
const GROUPED_PICKS: [&str; 11] = [
    "",
    "Rename",
    "Pin",
    "Lock",
    "",
    "",
    "Spawn",
    "Chat",
    "Sub-agents",
    "File edits",
    "Shells",
];

fn grouped() -> Vec<MenuItem> {
    vec![
        MenuItem::Caption("Workspace".into()),
        MenuItem::Action {
            label: "Rename".into(),
            keys: Some("F2".into()),
            icon: Some(Glyph::File.into()),
        },
        MenuItem::action("Pin").icon(Glyph::Pin),
        MenuItem::action("Lock").icon(Glyph::Lock),
        MenuItem::Separator,
        MenuItem::Caption("Tiles".into()),
        MenuItem::Submenu {
            label: "Spawn".into(),
            icon: Some(Icon::Plus.into()),
            items: actions(SPAWNS),
        },
    ]
}
const TOASTS: [(&str, &str, ToastKind, &str); 3] = [
    (
        "toast-success",
        "Success",
        ToastKind::Success,
        "go-dev 2 finished: 3 files changed.",
    ),
    (
        "toast-error",
        "Error",
        ToastKind::Error,
        "go test failed: loop_test.go:41",
    ),
    (
        "toast-info",
        "Info",
        ToastKind::Info,
        "Usage is not built yet.",
    ),
];
const BURST: [(ToastKind, &str); 5] = [
    (ToastKind::Info, "Lead picked up the ask."),
    (ToastKind::Success, "go-dev 1 finished: 1 file changed."),
    (ToastKind::Info, "bench queued behind go-dev 2."),
    (ToastKind::Error, "go-dev 3 stopped: exit 126."),
    (ToastKind::Success, "go-dev 2 finished: 3 files changed."),
];
const BURST_GAP: Duration = Duration::from_millis(200);
const LOADING_TAKES: Duration = Duration::from_millis(2000);

#[derive(Clone, Copy)]
struct Docked {
    side: Side,
    open: bool,
}

fn opener(
    id: impl Into<ElementId>,
    name: &'static str,
    open: bool,
    theme: &Theme,
) -> Stateful<Div> {
    let lit = theme.color(ColorToken::StateActive);
    button(id, name, None, ButtonKind::Opener, theme).when(open, |trigger| trigger.bg(lit))
}

fn docked(
    title: &'static str,
    prefix: &'static str,
    dock: fn(&mut ContentState) -> &mut Docked,
    shown: Docked,
    panel: impl IntoElement,
    theme: &Theme,
    cx: &mut Context<Book>,
) -> Div {
    let opens = POPOVER_SIDES.map(|(side, name)| {
        let open = shown.open && shown.side == side;
        opener(
            SharedString::from(format!("{prefix}-{name}")),
            name,
            open,
            theme,
        )
        .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
            *dock(&mut this.content) = Docked { side, open: true };
            cx.notify();
        }))
    });
    shell(titled(title), theme).h(px(EDGE_PAGE_HEIGHT)).child(
        inner_card(theme)
            .relative()
            .flex_1()
            .overflow_hidden()
            .p_3()
            .child(spread(theme).children(opens))
            .child(panel),
    )
}

fn edges(top: impl IntoElement, bottom: impl IntoElement) -> Div {
    div()
        .h(px(EDGE_PAGE_HEIGHT))
        .flex()
        .flex_col()
        .justify_between()
        .items_start()
        .child(top)
        .child(bottom)
}

pub(super) struct ContentState {
    drawer: Docked,
    sheet: Docked,
    popover: Option<Side>,
    menus: [Entity<MenuButton>; 2],
    dropdowns: [Entity<Dropdown>; 2],
    badges: [Entity<Dropdown>; 2],
    field: Option<Entity<TextInput>>,
    prose: Entity<SelectableText>,
}

impl ContentState {
    pub(super) fn new(cx: &mut Context<Book>) -> Self {
        ContentState {
            drawer: Docked {
                side: Side::Bottom,
                open: false,
            },
            sheet: Docked {
                side: Side::Right,
                open: false,
            },
            popover: None,
            menus: MENU_TRIGGERS.map(|name| {
                let menu = MenuButton::new(name.into(), actions(MENU), cx);
                let picked = cx.listener(|this, row: &usize, _, cx| {
                    if let Some(label) = MENU.get(*row) {
                        this.tell(format!("Menu: {label}"), cx);
                    }
                });
                menu.update(cx, |menu, _| {
                    menu.placement(Placement::below());
                    menu.on_pick(picked);
                });
                menu
            }),
            dropdowns: [(); 2].map(|_| Dropdown::new(actions(models()), cx)),
            badges: [models(), efforts()].map(|items| {
                let badge = Dropdown::new(actions(items), cx);
                badge.update(cx, |badge, _| badge.trigger(DropdownTrigger::Chip));
                badge
            }),
            field: None,
            prose: SelectableText::new(PROSE.into(), cx),
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
            Page::Popover => self.popovers(theme, cx),
            Page::Menu => self.menus(),
            Page::Dropdown => self.dropdowns(theme),
            Page::ContextMenu => self.context_menus(theme, window, cx),
            Page::Toast => toasts(theme, cx),
            Page::Sheet => self.sheet(theme, cx),
            Page::Drawer => self.drawer(theme, cx),
            _ => div(),
        }
    }

    pub(super) fn key(&mut self, page: Page, key: &str) -> bool {
        match (page, key) {
            (Page::Drawer, "escape") => self.drawer.open = false,
            (Page::Sheet, "escape") => self.sheet.open = false,
            _ => return false,
        }
        true
    }

    fn popovers(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let triggers = POPOVER_SIDES.map(|(side, name)| {
            let open = self.popover == Some(side);
            let trigger = opener(("popover", side as usize), name, open, theme).on_click(
                cx.listener(move |this, _: &ClickEvent, _, cx| {
                    this.content.popover = (this.content.popover != Some(side)).then_some(side);
                    cx.notify();
                }),
            );
            Popover::new(("popover-panel", side as usize), trigger)
                .open(open)
                .placement(Placement {
                    side,
                    ..Placement::below()
                })
                .child(format!("Opens on the {name} side."))
                .child(label("Click the same button to close it.", theme))
        });
        block(
            "Popover: one trigger per side, each opens on its side",
            theme,
            div()
                .flex()
                .justify_center()
                .gap_6()
                .py(px(POPOVER_ROOM))
                .children(triggers),
        )
    }

    fn menus(&self) -> Div {
        let [top, bottom] = self.menus.clone();
        edges(top, bottom)
    }

    fn dropdowns(&self, theme: &Theme) -> Div {
        let [top, bottom] = self.dropdowns.clone();
        edges(
            spread(theme)
                .items_start()
                .gap_6()
                .child(named(
                    "dropdown near the top: click, arrows, Enter, Escape",
                    theme,
                    top,
                ))
                .children(
                    self.badges
                        .iter()
                        .map(|badge| named("badge: click, pick a value", theme, badge.clone())),
                ),
            named("dropdown near the bottom: opens upward", theme, bottom),
        )
    }

    fn context_menus(&mut self, theme: &Theme, window: &mut Window, cx: &mut Context<Book>) -> Div {
        let field = self
            .field
            .get_or_insert_with(|| TextInput::new(FIELD.into(), window, cx))
            .clone();
        div()
            .flex()
            .flex_col()
            .gap_3()
            .child(block(
                "Right click anywhere in the area, pick a model",
                theme,
                inner_card(theme).w(px(AREA_WIDTH)).p_3().child(
                    context_menu(actions(models()))
                        .id("context-area")
                        .on_pick(Book::picked_model(cx))
                        .child(label("Right click anywhere in here.", theme)),
                ),
            ))
            .child(block(
                "Grouped: a caption heads each group, Spawn opens a submenu on hover or Right",
                theme,
                inner_card(theme).w(px(AREA_WIDTH)).p_3().child(
                    context_menu(grouped())
                        .id("context-grouped")
                        .on_pick(cx.listener(|this, at: &usize, _, cx| {
                            if let Some(label) = GROUPED_PICKS.get(*at).filter(|l| !l.is_empty()) {
                                this.tell(format!("Menu: {label}"), cx);
                            }
                        }))
                        .child(label("Right click for the grouped menu.", theme)),
                ),
            ))
            .child(block(
                "Edit menu: select text, right click, Cut, Copy, Paste or Select all",
                theme,
                spread(theme)
                    .items_start()
                    .gap_6()
                    .child(field)
                    .child(div().w(px(AREA_WIDTH)).child(self.prose.clone())),
            ))
    }

    fn drawer(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let close = cx.listener(|this, _: &(), _, cx| {
            this.content.drawer.open = false;
            cx.notify();
        });
        let drawn = Drawer::new("page-drawer")
            .side(self.drawer.side)
            .open(self.drawer.open)
            .on_close(move |window, cx| close(&(), window, cx))
            .child(
                div()
                    .p_3()
                    .flex()
                    .flex_col()
                    .gap_2()
                    .child("internal/turn/loop.go")
                    .child(label("Every edit to this file.", theme)),
            );
        docked(
            "Drawer: a side button opens it there, drag the knob or Escape closes it",
            "drawer",
            |content| &mut content.drawer,
            self.drawer,
            drawn,
            theme,
            cx,
        )
    }

    fn sheet(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let [close, confirm] = [(); 2].map(|_| {
            cx.listener(|this, said: &&'static str, _, cx| {
                this.content.sheet.open = false;
                this.tell(*said, cx);
            })
        });
        let drawn = Sheet::new("page-sheet", "Discard the draft?")
            .side(self.sheet.side)
            .open(self.sheet.open)
            .on_cancel(move |window, cx| close(&"Sheet: cancelled", window, cx))
            .on_confirm(move |window, cx| confirm(&"Sheet: confirmed", window, cx))
            .child(label(
                "The message you were writing to go-dev 2 goes away.",
                theme,
            ));
        docked(
            "Sheet: a side button opens it there, Cancel, Confirm, a click outside or Escape closes it",
            "sheet",
            |content| &mut content.sheet,
            self.sheet,
            drawn,
            theme,
            cx,
        )
    }
}

fn toasts(theme: &Theme, cx: &mut Context<Book>) -> Div {
    let kinds = TOASTS.map(|(id, name, kind, said)| {
        button(id, name, None, ButtonKind::Plain, theme).on_click(cx.listener(
            move |this, _: &ClickEvent, _, cx| {
                this.toaster
                    .update(cx, |toaster, cx| toaster.show(kind, said, cx));
            },
        ))
    });
    let loading = button(
        "toast-loading",
        "Loading, then success",
        None,
        ButtonKind::Plain,
        theme,
    )
    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
        let toaster = this.toaster.clone();
        let id = toaster.update(cx, |toaster, cx| {
            toaster.show(ToastKind::Loading, "Running go test ./internal/turn", cx)
        });
        cx.spawn(async move |_, cx| {
            cx.background_executor().timer(LOADING_TAKES).await;
            toaster.update(cx, |toaster, cx| {
                toaster.resolve(id, ToastKind::Success, "Tests passed: 42 of 42.", cx);
            });
        })
        .detach();
    }));
    let burst = button("toast-burst", "Fire five", None, ButtonKind::Plain, theme).on_click(
        cx.listener(|this, _: &ClickEvent, _, cx| {
            let toaster = this.toaster.clone();
            cx.spawn(async move |_, cx| {
                for (kind, said) in BURST {
                    toaster.update(cx, |toaster, cx| toaster.show(kind, said, cx));
                    cx.background_executor().timer(BURST_GAP).await;
                }
            })
            .detach();
        }),
    );
    block(
        "Toast: newest in front, hover spreads the stack and holds every timer, \u{d7} or a swipe right dismisses",
        theme,
        spread(theme).children(kinds).child(loading).child(burst),
    )
}
