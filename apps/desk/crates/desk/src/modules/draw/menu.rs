use std::fs;
use std::path::PathBuf;

use desk_canvas::{Act, GROUNDS, Tool, ground};
use desk_ui::components::chip::kbd;
use desk_ui::components::form::{TextInput, switch};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::paint::{glyph, ink};
use desk_ui::components::sheet::Sheet;
use desk_ui::components::size::{FIELD_WIDTH, FONT_BODY, FONT_SMALL, RADIUS_ROW, T1, T2};
use desk_ui::theme::{ColorToken, Theme};
use draw_doc::Look;
use gpui::{
    AnyElement, ClickEvent, Context, Div, ElementId, Entity, EntityInputHandler, Focusable,
    KeyDownEvent, MouseDownEvent, Rgba, SharedString, Stateful, Subscription, Window, anchored,
    deferred, div, prelude::*, px,
};

use super::export::Format;
use super::marks::{self, ALL, BACK, FIT, FRONT, GROUP, Mark, UNGROUP};
use super::panel::{caption, swatch};
use super::{
    Draw, INSET, ISLAND_PAD, MENU_WIDTH, Menu, Note, TOOL_BOX, board, face, island, listed, name,
    permit,
};

const MENU_BAR: (f32, f32) = (14.0, 1.5);
const CONTEXT_WIDTH: f32 = 248.0;
const SNAP_MARGIN: f32 = 8.0;
const LIST_TALL: f32 = 280.0;
const RENAME_PAD: f32 = 16.0;

type Row = (Act, &'static str, Mark, &'static str);

const EDITS: [Row; 4] = [
    (Act::Cut, "Cut", Mark::Glyph(Glyph::Cut), "Ctrl+X"),
    (Act::Copy, "Copy", Mark::Glyph(Glyph::Copy), "Ctrl+C"),
    (Act::Paste, "Paste", Mark::Glyph(Glyph::Paste), "Ctrl+V"),
    (
        Act::Duplicate,
        "Duplicate",
        Mark::Glyph(Glyph::Duplicate),
        "Ctrl+D",
    ),
];
const ARRANGE: [Row; 2] = [
    (Act::Front, "Bring to front", FRONT, "Ctrl+Shift+]"),
    (Act::Back, "Send to back", BACK, "Ctrl+Shift+["),
];
const EMPTY: [Row; 3] = [
    (Act::Paste, "Paste", Mark::Glyph(Glyph::Paste), "Ctrl+V"),
    (Act::SelectAll, "Select all", ALL, "Ctrl+A"),
    (Act::Fit, "Zoom to fit", FIT, "Shift+1"),
];

pub(super) struct Picker {
    search: Entity<TextInput>,
    boards: Vec<PathBuf>,
    _watch: Subscription,
}

impl Picker {
    fn matching(&self, cx: &gpui::App) -> Vec<PathBuf> {
        let query = self.search.read(cx).text().trim().to_lowercase();
        self.boards
            .iter()
            .filter(|path| name(path).to_lowercase().contains(&query))
            .cloned()
            .collect()
    }
}

fn item(
    id: impl Into<ElementId>,
    mark: Mark,
    label: impl Into<SharedString>,
    keys: Option<&'static str>,
    color: Rgba,
    theme: &Theme,
) -> Stateful<Div> {
    let hover = theme.color(ColorToken::StateHover);
    div()
        .id(id)
        .flex()
        .items_center()
        .gap_2()
        .px_2()
        .py_1()
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .text_size(px(FONT_BODY))
        .text_color(color)
        .hover(move |style| style.bg(hover))
        .child(marks::paint(mark, color))
        .child(div().flex_1().min_w_0().truncate().child(label.into()))
        .children(keys.map(|keys| kbd(keys, theme)))
}

fn separator(theme: &Theme) -> Div {
    div()
        .h(px(1.0))
        .my_1()
        .bg(theme.color(ColorToken::Separator))
}

impl Draw {
    pub(super) fn menu(&self, theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
        let open = !matches!(self.menu, Menu::Closed);
        let bar = || {
            div()
                .w(px(MENU_BAR.0))
                .h(px(MENU_BAR.1))
                .rounded_full()
                .bg(ink(theme, T1))
        };
        let trigger = face("draw-menu", open, theme)
            .child(
                div()
                    .flex()
                    .flex_col()
                    .gap(px(3.0))
                    .child(bar())
                    .child(bar())
                    .child(bar()),
            )
            .on_click(cx.listener(|draw, _: &ClickEvent, window, cx| {
                draw.menu = match draw.menu {
                    Menu::Closed => Menu::Open,
                    Menu::Open | Menu::Export | Menu::Rename(_) => Menu::Closed,
                };
                draw.canvas
                    .update(cx, |canvas, cx| canvas.focus_handle(cx).focus(window, cx));
                cx.notify();
            }));
        let dock = div()
            .id("draw-menu-dock")
            .absolute()
            .left(px(INSET))
            .top(px(INSET))
            .flex()
            .flex_col()
            .items_start()
            .gap(px(ISLAND_PAD))
            .on_mouse_down_out(cx.listener(|draw, _: &MouseDownEvent, _, cx| {
                if !matches!(draw.menu, Menu::Closed) {
                    draw.menu = Menu::Closed;
                    eprintln!("desk: draw: popover closed by click outside");
                    cx.notify();
                }
            }))
            .child(
                island(theme)
                    .id("draw-menu-island")
                    .occlude()
                    .p(px(ISLAND_PAD))
                    .child(trigger),
            );
        if !open {
            return dock.into_any_element();
        }
        let plain = ink(theme, T1);
        let heading = match &self.menu {
            Menu::Rename(input) => div()
                .on_key_down(cx.listener(Self::rename_key))
                .child(input.clone()),
            Menu::Closed | Menu::Open | Menu::Export => div()
                .px_2()
                .py_1()
                .truncate()
                .text_size(px(FONT_SMALL))
                .text_color(ink(theme, T2))
                .child(self.board.as_deref().map(name).unwrap_or_default()),
        };
        let wide = match self.menu {
            Menu::Rename(_) => FIELD_WIDTH + RENAME_PAD,
            Menu::Closed | Menu::Open | Menu::Export => MENU_WIDTH,
        };
        let export = item(
            "draw-export",
            Mark::Glyph(Glyph::Image),
            "Export As",
            None,
            plain,
            theme,
        )
        .child(glyph(Glyph::Chevron, marks::ICON_BOX, ink(theme, T2)))
        .on_click(cx.listener(|draw, _: &ClickEvent, _, cx| {
            draw.menu = match draw.menu {
                Menu::Export => Menu::Open,
                Menu::Closed | Menu::Open | Menu::Rename(_) => Menu::Export,
            };
            cx.notify();
        }));
        let panel = island(theme)
            .id("draw-menu-panel")
            .occlude()
            .w(px(wide))
            .p_1()
            .flex()
            .flex_col()
            .child(heading)
            .child(
                item(
                    "draw-rename",
                    Mark::Glyph(Glyph::Pencil),
                    "Rename board",
                    None,
                    plain,
                    theme,
                )
                .on_click(
                    cx.listener(|draw, _: &ClickEvent, window, cx| draw.start_rename(window, cx)),
                ),
            )
            .child(
                item(
                    "draw-open",
                    Mark::Glyph(Glyph::Folder),
                    "Open board...",
                    None,
                    plain,
                    theme,
                )
                .on_click(
                    cx.listener(|draw, _: &ClickEvent, window, cx| draw.open_picker(window, cx)),
                ),
            )
            .child(
                item(
                    "draw-new",
                    Mark::Glyph(Glyph::FileAdd),
                    "New board",
                    None,
                    plain,
                    theme,
                )
                .on_click(cx.listener(|draw, _: &ClickEvent, _, cx| {
                    draw.fresh(cx);
                    cx.notify();
                })),
            )
            .child(export)
            .child(separator(theme))
            .child(self.theme_rows(theme, cx));
        let flyout = matches!(self.menu, Menu::Export).then(|| {
            island(theme)
                .id("draw-export-flyout")
                .occlude()
                .w(px(MENU_WIDTH / 2.0))
                .p_1()
                .flex()
                .flex_col()
                .children(Format::ALL.into_iter().map(|format| {
                    item(
                        ("draw-export-as", format as usize),
                        Mark::Glyph(Glyph::File),
                        format.label(),
                        None,
                        plain,
                        theme,
                    )
                    .on_click(
                        cx.listener(move |draw, _: &ClickEvent, _, cx| draw.export_as(format, cx)),
                    )
                }))
        });
        dock.child(
            div()
                .flex()
                .items_start()
                .gap(px(ISLAND_PAD))
                .child(panel)
                .children(flyout),
        )
        .into_any_element()
    }

    fn theme_rows(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let backdrop = self.canvas.read(cx).backdrop().clone();
        let grounds: Vec<_> = GROUNDS
            .into_iter()
            .enumerate()
            .map(|(at, pick)| {
                swatch(
                    ("draw-ground", at),
                    Some(ground(pick, theme)),
                    backdrop.color.as_deref() == pick,
                    theme,
                )
                .aria_label(pick.unwrap_or("theme"))
                .on_click(cx.listener(move |draw, _: &ClickEvent, _, cx| {
                    draw.restyle(|backdrop| backdrop.color = pick.map(str::to_owned), cx)
                }))
            })
            .collect();
        div()
            .flex()
            .flex_col()
            .gap_1()
            .px_2()
            .py_1()
            .child(caption("Theme", theme))
            .child(div().flex().gap_1().children(grounds))
            .child(
                switch("draw-dots", "Dot grid", backdrop.dots, theme).on_click(cx.listener(
                    |draw, _: &ClickEvent, _, cx| {
                        draw.restyle(|backdrop| backdrop.dots = !backdrop.dots, cx)
                    },
                )),
            )
            .child(
                switch("draw-look", "UI nodes", backdrop.look == Look::Ui, theme).on_click(
                    cx.listener(|draw, _: &ClickEvent, _, cx| {
                        draw.restyle(
                            |backdrop| {
                                backdrop.look = match backdrop.look {
                                    Look::Sketch => Look::Ui,
                                    Look::Ui => Look::Sketch,
                                };
                                eprintln!("desk: draw: board look {:?}", backdrop.look);
                            },
                            cx,
                        )
                    }),
                ),
            )
    }

    fn start_rename(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let current = self.board.as_deref().map(name).unwrap_or_default();
        let input = TextInput::new("Board name".into(), window, cx);
        input.update(cx, |input, cx| {
            input.replace_text_in_range(None, &current, window, cx)
        });
        window.focus(&input.focus_handle(cx), cx);
        eprintln!("desk: draw: rename {current}");
        self.menu = Menu::Rename(input);
        cx.notify();
    }

    fn rename_key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        if event.keystroke.key != "enter" {
            return;
        }
        cx.stop_propagation();
        let Menu::Rename(input) = std::mem::replace(&mut self.menu, Menu::Closed) else {
            return;
        };
        let to = input.read(cx).text().trim().to_owned();
        match self.rename(&to) {
            Ok(path) => self.note = Some(Note::Done(format!("Renamed to {}", name(&path)).into())),
            Err(error) => self.fail(error),
        }
        self.canvas
            .update(cx, |canvas, cx| canvas.focus_handle(cx).focus(window, cx));
        cx.notify();
    }

    fn rename(&mut self, to: &str) -> Result<PathBuf, String> {
        let from = self.board.clone().ok_or("no board is open to rename")?;
        let named = board(to)?;
        let file = named.file_name().ok_or("the new name is empty")?;
        let path = from.with_file_name(file);
        if path == from {
            return Ok(path);
        }
        if path.exists() {
            return Err(format!("a board named {to} already exists"));
        }
        if from.exists() {
            permit(self.plugin, &path)?;
            fs::rename(&from, &path).map_err(|error| format!("{}: {error}", from.display()))?;
        }
        eprintln!(
            "desk: draw renamed {} to {}",
            from.display(),
            path.display()
        );
        self.board = Some(path.clone());
        Ok(path)
    }

    fn open_picker(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.menu = Menu::Closed;
        let listing = match &self.folder {
            Ok(folder) => listed(folder),
            Err(error) => Err(error.clone()),
        };
        let boards = match listing {
            Ok(boards) => boards,
            Err(error) => return self.fail(error),
        };
        let search = TextInput::new("Search boards".into(), window, cx);
        window.focus(&search.focus_handle(cx), cx);
        let watch = cx.observe(&search, |_, _, cx| cx.notify());
        eprintln!("desk: draw: open picker lists {} boards", boards.len());
        self.picker = Some(Picker {
            search,
            boards,
            _watch: watch,
        });
        cx.notify();
    }

    fn pick_first(&mut self, cx: &mut Context<Self>) {
        let Some(first) = self
            .picker
            .take()
            .and_then(|picker| picker.matching(cx).into_iter().next())
        else {
            return eprintln!("desk: draw: open picker closed with no match");
        };
        self.open(first, cx);
        cx.notify();
    }

    fn picker_key(&mut self, event: &KeyDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        if event.keystroke.key == "enter" {
            cx.stop_propagation();
            self.pick_first(cx);
        }
    }

    pub(super) fn picker_sheet(&self, theme: &Theme, cx: &mut Context<Self>) -> Option<AnyElement> {
        let picker = self.picker.as_ref()?;
        let found = picker.matching(cx);
        let plain = ink(theme, T1);
        let list: AnyElement = match found.is_empty() {
            true => caption(
                match picker.boards.is_empty() {
                    true => "No boards yet",
                    false => "No board matches",
                },
                theme,
            )
            .px_2()
            .py_1()
            .into_any_element(),
            false => div()
                .id("draw-picker-list")
                .flex()
                .flex_col()
                .max_h(px(LIST_TALL))
                .overflow_y_scroll()
                .children(found.into_iter().enumerate().map(|(at, path)| {
                    item(
                        ("draw-picker-board", at),
                        Mark::Glyph(Glyph::File),
                        name(&path),
                        None,
                        plain,
                        theme,
                    )
                    .on_click(cx.listener(
                        move |draw, _: &ClickEvent, _, cx| {
                            draw.picker = None;
                            draw.open(path.clone(), cx);
                            cx.notify();
                        },
                    ))
                }))
                .into_any_element(),
        };
        let cancel = cx.weak_entity();
        let confirm = cx.weak_entity();
        Some(
            Sheet::new("draw-picker", "Open board")
                .modal()
                .open(true)
                .confirm("Open")
                .on_cancel(move |_, cx| {
                    if let Err(error) = cancel.update(cx, |draw, cx| {
                        draw.picker = None;
                        cx.notify();
                    }) {
                        eprintln!("desk: draw: picker: the draw tile is gone: {error}");
                    }
                })
                .on_confirm(move |_, cx| {
                    if let Err(error) = confirm.update(cx, |draw, cx| draw.pick_first(cx)) {
                        eprintln!("desk: draw: picker: the draw tile is gone: {error}");
                    }
                })
                .child(
                    div()
                        .pb_2()
                        .on_key_down(cx.listener(Self::picker_key))
                        .child(picker.search.clone()),
                )
                .child(list)
                .into_any_element(),
        )
    }

    pub(super) fn context_menu(&self, theme: &Theme, cx: &mut Context<Self>) -> Option<AnyElement> {
        let (at, shape) = self.context?;
        let plain = ink(theme, T1);
        let canvas = self.canvas.read(cx);
        let act_row =
            |id: usize, (act, label, mark, keys): Row, color: Rgba, cx: &mut Context<Self>| {
                item(("draw-context", id), mark, label, Some(keys), color, theme).on_click(
                    cx.listener(move |draw, _: &ClickEvent, window, cx| {
                        draw.context = None;
                        draw.canvas
                            .update(cx, |canvas, cx| canvas.act(act, window, cx));
                        cx.notify();
                    }),
                )
            };
        let mut rows: Vec<AnyElement> = Vec::new();
        if shape {
            let grouped = canvas.selection_grouped();
            let lock = match canvas.selection_locked() {
                true => "Unlock",
                false => "Lock",
            };
            let group = match grouped {
                true => (Act::Ungroup, "Ungroup", UNGROUP, "Ctrl+Shift+G"),
                false => (Act::Group, "Group", GROUP, "Ctrl+G"),
            };
            let arrange = ARRANGE.into_iter().chain([
                group,
                (Act::Lock, lock, Mark::Glyph(Glyph::Lock), "Ctrl+Shift+L"),
            ]);
            for (id, row) in EDITS.into_iter().chain(arrange).enumerate() {
                if id == EDITS.len() {
                    rows.push(separator(theme).into_any_element());
                }
                rows.push(act_row(id, row, plain, cx).into_any_element());
            }
            rows.push(separator(theme).into_any_element());
            rows.push(
                div()
                    .px_2()
                    .py_1()
                    .child(self.hue_grid(theme, cx))
                    .into_any_element(),
            );
            rows.push(separator(theme).into_any_element());
            let danger = theme.color(ColorToken::StatusDanger);
            let delete = (Act::Delete, "Delete", Mark::Glyph(Glyph::Trash), "Del");
            let last = EDITS.len() + ARRANGE.len() + 2;
            rows.push(act_row(last, delete, danger, cx).into_any_element());
        } else {
            for (id, row) in EMPTY.into_iter().enumerate() {
                rows.push(act_row(id, row, plain, cx).into_any_element());
            }
        }
        let panel = island(theme)
            .id("draw-context-menu")
            .occlude()
            .w(px(CONTEXT_WIDTH))
            .p_1()
            .flex()
            .flex_col()
            .on_mouse_down_out(cx.listener(|draw, _: &MouseDownEvent, _, cx| {
                if draw.context.take().is_some() {
                    eprintln!("desk: draw: context menu closed by click outside");
                    cx.notify();
                }
            }))
            .children(rows);
        Some(
            deferred(
                anchored()
                    .position(at)
                    .snap_to_window_with_margin(px(SNAP_MARGIN))
                    .child(panel),
            )
            .priority_auto()
            .into_any_element(),
        )
    }

    pub(super) fn more(&self, theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
        let active = self.canvas.read(cx).tool().0;
        let accent = theme.color(ColorToken::FocusRing);
        let plain = ink(theme, T1);
        let on = self.more_open || Tool::MORE.contains(&active);
        let button = face("draw-more", on, theme)
            .aria_label("More tools")
            .child(glyph(
                Glyph::Ellipsis,
                marks::ICON_BOX,
                if on { accent } else { plain },
            ))
            .on_click(cx.listener(|draw, _: &ClickEvent, _, cx| {
                draw.more_open = !draw.more_open;
                cx.notify();
            }));
        let tools = Tool::MORE.into_iter().enumerate().map(|(at, tool)| {
            let color = if tool == active { accent } else { plain };
            let keys = tool.key();
            item(
                ("draw-more-tool", at),
                marks::of_tool(tool),
                tool.name(),
                None,
                color,
                theme,
            )
            .child(kbd(keys.to_uppercase(), theme))
            .on_click(cx.listener(move |draw, _: &ClickEvent, window, cx| {
                draw.more_open = false;
                draw.use_tool(Some(tool), window, cx);
                cx.notify();
            }))
        });
        let popover = self.more_open.then(|| {
            deferred(
                island(theme)
                    .id("draw-more-popover")
                    .occlude()
                    .w(px(MENU_WIDTH * 0.75))
                    .mb(px(ISLAND_PAD * 2.0))
                    .p_1()
                    .flex()
                    .flex_col()
                    .child(
                        item(
                            "draw-insert-image",
                            Mark::Glyph(Glyph::Image),
                            "Insert image",
                            None,
                            plain,
                            theme,
                        )
                        .on_click(cx.listener(
                            |draw, _: &ClickEvent, window, cx| {
                                draw.more_open = false;
                                draw.insert_image_from_file(window, cx);
                                cx.notify();
                            },
                        )),
                    )
                    .child(separator(theme))
                    .children(tools),
            )
            .priority_auto()
        });
        div()
            .relative()
            .flex_none()
            .size(px(TOOL_BOX))
            .child(
                div()
                    .id("draw-more-anchor")
                    .absolute()
                    .right_0()
                    .bottom_0()
                    .flex()
                    .flex_col()
                    .items_end()
                    .children(popover)
                    .child(button)
                    .on_mouse_down_out(cx.listener(|draw, _: &MouseDownEvent, _, cx| {
                        if draw.more_open {
                            draw.more_open = false;
                            eprintln!("desk: draw: popover closed by click outside");
                            cx.notify();
                        }
                    })),
            )
            .into_any_element()
    }
}
