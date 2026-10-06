use desk_ui::components::paint::ink;
use desk_ui::theme::Theme;
use gpui::{ClickEvent, Context, Div, FontWeight, div, prelude::*, px, relative, rgb, rgba};

use super::fixture::{self, BLANK_ID, NEW_ID, USERS};
use super::kit::{self, DANGER, LINE, LINK, T2, T3, WARN};
use super::{Edit, Studio, Tab};

const NOTE_COLUMNS: [f32; 5] = [44.0, 64.0, 0.0, 120.0, 170.0];
const LAST_COLUMN: f32 = 96.0;
const USER_COLUMNS: [f32; 3] = [44.0, 64.0, 0.0];
const USER_LAST: f32 = 200.0;

fn cells(widths: &[f32], last: f32, cells: Vec<Div>) -> Div {
    let mut row = div().flex().items_center().w_full();
    for (index, cell) in cells.into_iter().enumerate() {
        let width = widths.get(index).copied().unwrap_or(last);
        let padded = if index >= 2 { cell.pl(px(12.0)) } else { cell };
        row = row.child(if width == 0.0 {
            padded.flex_1().min_w_0().overflow_hidden()
        } else {
            padded.w(px(width)).flex_none()
        });
    }
    row
}

fn head(name: &'static str, kind: &'static str, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_col()
        .line_height(px(14.0))
        .child(name)
        .child(
            kit::mono(theme, 10.5)
                .text_color(ink(theme, T3))
                .child(kind),
        )
}

impl Studio {
    fn toolbar(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let (name, rows) = match self.tab {
            Tab::Notes => {
                let all = self.all_notes().len();
                let text = if self.filtered {
                    format!("2 of {all} rows")
                } else {
                    format!("{all} rows")
                };
                ("notes", text)
            }
            Tab::Users => ("users", "2 rows".to_string()),
        };
        div()
            .h(px(53.0))
            .flex_none()
            .flex()
            .items_center()
            .gap(px(8.0))
            .px(px(16.0))
            .border_b_1()
            .border_color(rgba(LINE))
            .child(
                div()
                    .text_size(px(16.0))
                    .font_weight(FontWeight::SEMIBOLD)
                    .line_height(relative(1.0))
                    .child(name),
            )
            .child(
                div()
                    .text_size(px(13.0))
                    .text_color(ink(theme, T3))
                    .child(rows),
            )
            .child(div().flex_1())
            .child(
                kit::button("filter", 28.0, theme)
                    .when(self.filtered, |button| button.bg(ink(theme, 0.14)))
                    .child(div().text_size(px(11.0)).child("⏷"))
                    .child(if self.filtered {
                        "title is blank · 1"
                    } else {
                        "Filter"
                    })
                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                        this.filtered = !this.filtered;
                        cx.notify();
                    })),
            )
            .child(
                kit::button("sort", 28.0, theme)
                    .child("Sort")
                    .on_click(cx.listener(Self::tell(fixture::TELL_SORT))),
            )
            .child(
                kit::button("columns", 28.0, theme)
                    .child("Columns")
                    .on_click(cx.listener(Self::tell(fixture::TELL_COLUMNS))),
            )
            .child(div().w(px(1.0)).h(px(18.0)).mx(px(2.0)).bg(ink(theme, 0.1)))
            .child(
                kit::primary("add-row", 28.0, theme)
                    .child("+ Add row")
                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                        this.edit = Edit::Insert;
                        this.selected = None;
                        this.tab = Tab::Notes;
                        this.filtered = false;
                        cx.notify();
                    })),
            )
    }

    fn notes_grid(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let header = cells(
            &NOTE_COLUMNS,
            LAST_COLUMN,
            vec![
                div(),
                head("id 🔑", "int4", theme),
                head("title", "text", theme),
                head("user_id", "int4 → users", theme),
                head("created_at", "timestamptz", theme),
                head("deleted", "bool", theme),
            ],
        )
        .h(px(41.0))
        .bg(rgba(0x0000_002e))
        .border_b_1()
        .border_color(ink(theme, 0.06))
        .text_size(px(12.5));
        let rows = self.shown_notes().into_iter().map(|note| {
            let on = self.selected == Some(note.id);
            let title = self.title_of(note);
            let text = match (note.id, title) {
                (NEW_ID, _) => "new row, type a title",
                (_, Some(title)) => title,
                (BLANK_ID, None) => "'   '  three spaces",
                (_, None) => "NULL",
            };
            let title_cell = div()
                .whitespace_nowrap()
                .overflow_hidden()
                .text_ellipsis()
                .child(text);
            let title_cell = if self.pending(note.id) {
                title_cell.text_color(rgb(WARN))
            } else if title.is_none() {
                title_cell.text_color(ink(theme, 0.32)).italic()
            } else {
                title_cell
            };
            let id = note.id;
            cells(
                &NOTE_COLUMNS,
                LAST_COLUMN,
                vec![
                    div().flex().justify_center().child(
                        div()
                            .size(px(13.0))
                            .rounded(px(4.0))
                            .when(on, |box_| box_.bg(ink(theme, 0.88)))
                            .when(!on, |box_| {
                                box_.shadow(vec![kit::edge(ink(theme, 0.22), 1.5)])
                            }),
                    ),
                    kit::mono(theme, 12.5)
                        .text_color(ink(theme, T3))
                        .child(note.id.to_string()),
                    title_cell,
                    kit::mono(theme, 12.5)
                        .text_color(rgb(LINK))
                        .child(note.user),
                    kit::mono(theme, 12.5)
                        .text_color(ink(theme, T2))
                        .child(note.at),
                    div().flex().child(
                        div()
                            .px(px(7.0))
                            .rounded(px(999.0))
                            .text_size(px(11.5))
                            .line_height(px(17.0))
                            .when(note.deleted, |badge| {
                                badge.bg(rgba(0xf1737d24)).text_color(rgb(DANGER))
                            })
                            .when(!note.deleted, |badge| {
                                badge.bg(ink(theme, 0.06)).text_color(ink(theme, 0.5))
                            })
                            .child(if note.deleted { "true" } else { "false" }),
                    ),
                ],
            )
            .id(("note", id as usize))
            .h(px(38.0))
            .cursor_pointer()
            .border_b_1()
            .border_color(ink(theme, 0.04))
            .text_size(px(13.0))
            .when(on, |row| row.bg(ink(theme, 0.06)))
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                this.selected = Some(id);
                cx.notify();
            }))
        });
        div()
            .flex_1()
            .min_h_0()
            .overflow_hidden()
            .child(header)
            .children(rows)
    }

    fn users_grid(&self, theme: &Theme) -> Div {
        let header = cells(
            &USER_COLUMNS,
            USER_LAST,
            vec![
                div(),
                head("id", "int4", theme),
                head("name", "text", theme),
                head("notes", "← notes.user_id", theme),
            ],
        )
        .h(px(41.0))
        .bg(rgba(0x0000_002e))
        .border_b_1()
        .border_color(ink(theme, 0.06))
        .text_size(px(12.5));
        let rows = USERS.iter().map(|(id, name, notes)| {
            cells(
                &USER_COLUMNS,
                USER_LAST,
                vec![
                    div(),
                    kit::mono(theme, 13.0)
                        .text_color(ink(theme, T3))
                        .child(id.to_string()),
                    div().child(*name),
                    div().text_color(rgb(LINK)).child(*notes),
                ],
            )
            .h(px(39.0))
            .border_b_1()
            .border_color(ink(theme, 0.04))
            .text_size(px(13.0))
        });
        div().flex_1().min_h_0().child(header).children(rows)
    }

    fn status(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let bar = div()
            .flex_none()
            .flex()
            .items_center()
            .px(px(16.0))
            .border_t_1();
        match self.edit {
            Edit::Pending | Edit::Insert => {
                let sql = if self.edit == Edit::Insert {
                    "INSERT INTO notes (user_id, created_at) VALUES (1, now())"
                } else {
                    "UPDATE notes SET title = 'Ask about the deposit' WHERE id = 3"
                };
                bar.h(px(47.0))
                    .gap(px(10.0))
                    .bg(rgba(0xe8c98a12))
                    .border_color(rgba(0xe8c98a40))
                    .text_size(px(13.0))
                    .child(div().text_color(rgb(WARN)).child("1 change"))
                    .child(
                        kit::mono(theme, 12.0)
                            .flex_1()
                            .min_w_0()
                            .overflow_hidden()
                            .whitespace_nowrap()
                            .text_color(ink(theme, T3))
                            .child(sql),
                    )
                    .child(
                        kit::button("discard", 28.0, theme)
                            .text_size(px(13.0))
                            .child("Discard")
                            .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                                this.edit = Edit::Clean;
                                this.selected = this.selected.filter(|id| *id != NEW_ID);
                                cx.notify();
                            })),
                    )
                    .child(
                        kit::primary("save", 28.0, theme)
                            .text_size(px(13.0))
                            .child("Save changes")
                            .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                                this.edit = if this.edit == Edit::Insert {
                                    Edit::Inserted
                                } else {
                                    Edit::Saved
                                };
                                cx.notify();
                            })),
                    )
            }
            Edit::Clean | Edit::Saved | Edit::Inserted => {
                let text = match self.edit {
                    Edit::Inserted => "Saved · 1 row inserted",
                    Edit::Saved => "Saved · 1 row updated",
                    _ => "rows 1 to 6 of 6",
                };
                bar.h(px(41.0))
                    .border_color(rgba(LINE))
                    .text_size(px(12.5))
                    .text_color(ink(theme, T3))
                    .child(text)
                    .child(div().flex_1())
                    .child("query 4 ms")
            }
        }
    }

    fn drawer(&self, theme: &Theme, cx: &mut Context<Self>) -> Option<Div> {
        let note = self.drawer_note()?;
        let blank = note.id == BLANK_ID;
        let field_text = match (blank, self.edit) {
            (true, Edit::Clean) => "'   '  click to edit",
            (true, _) => fixture::EDITED_TITLE,
            (false, _) if note.title.is_empty() => "NULL",
            (false, _) => note.title,
        };
        let label = |name: &'static str, kind: &'static str| {
            div()
                .flex()
                .gap(px(4.0))
                .text_size(px(12.0))
                .text_color(ink(theme, T3))
                .child(name)
                .child(kit::mono(theme, 11.0).child(kind))
        };
        let field = || {
            div()
                .h(px(34.0))
                .flex()
                .items_center()
                .px(px(10.0))
                .rounded(px(8.0))
                .bg(rgba(0x0000_0040))
        };
        Some(
            div()
                .absolute()
                .top_0()
                .right_0()
                .bottom_0()
                .w(px(331.0))
                .occlude()
                .flex()
                .flex_col()
                .bg(rgba(0x1817_1ef7))
                .border_l_1()
                .border_color(ink(theme, 0.08))
                .shadow(vec![kit::drop(0.0, 40.0, 0x0000_0066)])
                .child(
                    div()
                        .h(px(53.0))
                        .flex_none()
                        .flex()
                        .items_center()
                        .gap(px(8.0))
                        .px(px(16.0))
                        .border_b_1()
                        .border_color(rgba(LINE))
                        .child(div().font_weight(FontWeight::SEMIBOLD).child("notes"))
                        .child(kit::mono(theme, 12.5).text_color(ink(theme, T3)).child(format!("id {}", self.selected.unwrap_or(note.id))))
                        .child(div().flex_1())
                        .child(
                            div()
                                .id("drawer-close")
                                .size(px(26.0))
                                .flex()
                                .items_center()
                                .justify_center()
                                .cursor_pointer()
                                .text_color(ink(theme, 0.45))
                                .child("×")
                                .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                                    this.selected = None;
                                    cx.notify();
                                })),
                        ),
                )
                .child(
                    div()
                        .flex_1()
                        .flex()
                        .flex_col()
                        .gap(px(14.0))
                        .px(px(16.0))
                        .py(px(14.0))
                        .text_size(px(13.0))
                        .child(
                            div().flex().flex_col().gap(px(5.0)).child(label("title", "text")).child(
                                field()
                                    .id("edit-title")
                                    .cursor_pointer()
                                    .when(blank && self.edit == Edit::Pending, |f| f.shadow(vec![kit::edge(rgb(WARN), 1.0)]))
                                    .child(field_text)
                                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                                        if this.selected == Some(BLANK_ID) {
                                            this.edit = Edit::Pending;
                                            cx.notify();
                                        }
                                    })),
                            ),
                        )
                        .child(
                            div().flex().flex_col().gap(px(5.0)).child(label("user_id", "→ users")).child(
                                field()
                                    .id("drawer-user")
                                    .cursor_pointer()
                                    .text_color(rgb(LINK))
                                    .child(note.user)
                                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                                        this.tab = Tab::Users;
                                        cx.notify();
                                    })),
                            ),
                        )
                        .child(
                            div()
                                .flex()
                                .flex_col()
                                .gap(px(5.0))
                                .child(label("created_at", ""))
                                .child(field().child(kit::mono(theme, 13.0).text_color(ink(theme, T2)).child("2026-10-05 11:02:14+01"))),
                        )
                        .child(
                            div()
                                .flex()
                                .flex_col()
                                .child(div().text_size(px(12.0)).mb(px(6.0)).text_color(ink(theme, T3)).child("Related · note_tags"))
                                .child(div().flex().gap(px(6.0)).child(kit::chip("home", theme)).child(kit::chip("money", theme))),
                        ),
                )
                .child(
                    div()
                        .flex()
                        .flex_col()
                        .gap(px(6.0))
                        .px(px(16.0))
                        .py(px(12.0))
                        .border_t_1()
                        .border_color(rgba(LINE))
                        .child(
                            kit::button("ask-lead", 30.0, theme)
                                .text_size(px(13.0))
                                .child(if self.asked { "Sent to the lead" } else { "Ask the lead about this row" })
                                .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                                    this.asked = true;
                                    cx.notify();
                                })),
                        )
                        .child(
                            div()
                                .text_size(px(11.5))
                                .line_height(px(16.0))
                                .text_color(ink(theme, T3))
                                .child("The row goes to the chat as a reference; the lead reads its values."),
                        ),
                ),
        )
    }

    pub(super) fn main(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let grid = match self.tab {
            Tab::Notes => self.notes_grid(theme, cx),
            Tab::Users => self.users_grid(theme),
        };
        div()
            .flex_1()
            .min_w_0()
            .flex()
            .flex_col()
            .relative()
            .child(self.toolbar(theme, cx))
            .child(grid)
            .child(self.status(theme, cx))
            .children(self.drawer(theme, cx))
    }
}
