use std::ops::RangeInclusive;
use std::rc::Rc;

use desk_ui::components::code::{GutterMark, LineMarks, Marks};
use desk_ui::components::empty::empty_state;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::tabs::{Tab, TabEvent, TabMark, connected_tabs};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, Context, Div, FontWeight, SharedString, div, linear_color_stop, linear_gradient,
    prelude::*, px, relative,
};

use crate::modules::chat::Find;

use super::fixture::{COUNT, COUNT_TEST, Git, Icon, Line, MENU, STORE, STORE_DIFF, TOP, WEB};
use super::kit::{
    ADD, AGENT, AGENT_FILL, AGENT_TEXT, CHAT, COLLAPSE, DANGER, DEL, DOWN, MODIFIED, MONO, PLUS,
    POP, PREFS, ROBOT, SEND, SPLIT, T2, T3, TRACE, black, cap, ellipsis, file_icon, glyph, hex,
    inner, medium, pop_shadow, ring, shell, square, text, tint, white,
};
use super::parts::{close_mark, code, dot, kbd, quiet, tab, tree_row};
use super::{Editor, File, Mode, OnDisk, Source};

const CURSOR: ColorToken = ColorToken::StateHover;
const AGENT_LINES: RangeInclusive<usize> = 10..=18;
const REMOVED_LINE: usize = 9;
const MODIFIED_LINE: usize = 21;
const CURSOR_LINE: usize = 13;
const STORE_BLAME_LINE: usize = 4;
const SIDE_SHARE: f32 = 0.3;
const SIDE_LEAST: f32 = 180.0;
const CHAT_SHARE: f32 = 0.3;
const CHAT_LEAST: f32 = 240.0;
const CHAT_MOST: f32 = 360.0;

fn marked(gutter: ColorToken) -> LineMarks {
    LineMarks {
        gutter: Some(GutterMark::Changed(gutter)),
        ..LineMarks::default()
    }
}

pub fn cursor_marks() -> Marks {
    Marks::from([(
        CURSOR_LINE - 1,
        LineMarks {
            background: Some(CURSOR),
            ..LineMarks::default()
        },
    )])
}

pub fn notes_marks(scale: f32) -> Marks {
    let mut marks: Marks = AGENT_LINES
        .map(|line| {
            let agent = LineMarks {
                edge: Some(ColorToken::Trace),
                ..marked(ColorToken::GitAdded)
            };
            (line - 1, agent)
        })
        .collect();
    marks.insert(
        REMOVED_LINE - 1,
        LineMarks {
            gutter: Some(GutterMark::Removed(ColorToken::GitDeleted)),
            ..LineMarks::default()
        },
    );
    marks.insert(MODIFIED_LINE - 1, marked(ColorToken::GitModified));
    if let Some(line) = marks.get_mut(&(CURSOR_LINE - 1)) {
        line.background = Some(CURSOR);
        line.trailing = Some(Rc::new(move |_: &Theme| blame(scale).into_any_element()));
    }
    marks
}

pub fn store_marks() -> Marks {
    Marks::from([(
        STORE_BLAME_LINE - 1,
        LineMarks {
            background: Some(ColorToken::TabsHover),
            trailing: Some(Rc::new(|_: &Theme| person_blame().into_any_element())),
            ..LineMarks::default()
        },
    )])
}

fn side_width(column: Div, most: f32) -> Div {
    column
        .flex_none()
        .w(relative(SIDE_SHARE))
        .min_w(px(SIDE_LEAST))
        .max_w(px(most))
}

fn head(most: f32, tools: Option<Div>) -> Div {
    div()
        .flex()
        .flex_none()
        .items_end()
        .gap(px(2.0))
        .h(px(36.0))
        .pl(px(4.0))
        .pr(px(6.0))
        .child(
            side_width(div(), most)
                .flex()
                .items_center()
                .gap(px(8.0))
                .h(px(24.0))
                .mb(px(6.0))
                .pl(px(8.0))
                .child(cap("Editor").flex_1())
                .children(tools),
        )
}

fn icon_button(id: &'static str, body: &str, size: f32, scale: f32) -> gpui::Stateful<Div> {
    square(size, 7.0)
        .id(id)
        .cursor_pointer()
        .child(glyph(body, 13.0, white(0.45), scale))
}

fn tree(most: f32) -> Div {
    side_width(div(), most)
        .flex()
        .flex_col()
        .px(px(4.0))
        .py(px(8.0))
        .border_r_1()
        .border_color(black(0.35))
}

fn strip(when: &'static str, scale: f32) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(8.0))
        .mx(px(12.0))
        .mb(px(6.0))
        .px(px(10.0))
        .py(px(6.0))
        .rounded(px(8.0))
        .bg(AGENT_FILL)
        .text_size(px(12.5))
        .line_height(px(23.0))
        .child(glyph(ROBOT, 13.0, AGENT, scale))
        .child(ellipsis(text(
            12.5,
            23.0,
            AGENT_TEXT,
            "Recently edited by go-dev",
        )))
        .child(ellipsis(quiet(12.5, when)))
}

fn blame(scale: f32) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(6.0))
        .ml(px(28.0))
        .font_family(super::kit::SANS)
        .text_size(px(12.5))
        .text_color(tint(0xb9a6ea, 0.62))
        .child(glyph(ROBOT, 12.0, tint(0xb9a6ea, 0.62), scale))
        .child("go-dev · not committed · turn 4")
}

fn person_blame() -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(6.0))
        .ml(px(28.0))
        .font_family(super::kit::SANS)
        .text_size(px(12.5))
        .text_color(white(0.34))
        .child(
            div()
                .flex_none()
                .size(px(12.0))
                .rounded(px(6.0))
                .bg(linear_gradient(
                    135.0,
                    linear_color_stop(hex(0x6b7a8f), 0.0),
                    linear_color_stop(hex(0x3a4250), 1.0),
                )),
        )
        .child("pehcastro, 3 weeks ago · a88092d")
}

const POP_LINE: f32 = 22.75;
const POP_ROW: f32 = POP_LINE + 14.0;

fn pop_row(label: &'static str, color: gpui::Rgba, key: Option<&'static str>) -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(10.0))
        .h(px(33.0))
        .px(px(9.0))
        .rounded(px(8.0))
        .text_size(px(13.0))
        .line_height(px(23.0))
        .text_color(color)
        .child(div().flex_1().child(label))
        .children(key.map(kbd))
}

impl Editor {
    pub fn edit_board(&mut self, scale: f32, cx: &mut Context<Self>) -> Div {
        let notes = self.file == File::Notes;
        let tools = div()
            .flex()
            .flex_none()
            .gap(px(8.0))
            .child(
                icon_button("new-file", PLUS, 24.0, scale).on_click(cx.listener(Self::tell(
                    "Creates a file in the selected folder and opens it in a new tab.",
                ))),
            )
            .child(
                icon_button("collapse", COLLAPSE, 24.0, scale)
                    .on_click(cx.listener(Self::update(|this| this.notes_open = false))),
            );
        let header = head(290.0, Some(tools));
        let header = match &self.source {
            Source::Disk { file, .. } => {
                header.child(self.file_tab(file.as_ref().map(|(_, disk)| disk), cx))
            }
            Source::Fixture { .. } => self.fixture_tabs(header, notes, scale, cx),
        }
        .child(
            icon_button("split", SPLIT, 26.0, scale)
                .mb(px(5.0))
                .on_click(cx.listener(Self::tell(
                    "Splits the editor in two side by side; the current tab opens on the right.",
                ))),
        )
        .child(
            icon_button("prefs", PREFS, 26.0, scale)
                .mb(px(5.0))
                .on_click(cx.listener(Self::update(|this| this.prefs = !this.prefs))),
        );
        let side = match &self.source {
            Source::Disk {
                folder: Some(folder),
                ..
            } => tree(291.0).child(folder.tree.clone()),
            _ => self.fixture_tree(scale, cx),
        };
        self.shell_around(header, side, scale, cx)
    }

    fn fixture_tree(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        let selected = |file: File| self.file == file;
        let mut rows: Vec<AnyElement> = Vec::new();
        if self.filtering {
            rows.push(
                div()
                    .flex()
                    .items_center()
                    .gap(px(8.0))
                    .pt(px(2.0))
                    .px(px(10.0))
                    .pb(px(8.0))
                    .text_size(px(13.0))
                    .child(quiet(13.0, "filter"))
                    .child("no")
                    .child(div().w(px(1.5)).h(px(14.0)).bg(white(1.0)))
                    .child(div().flex_1())
                    .child(quiet(11.5, "esc"))
                    .into_any_element(),
            );
        }
        for (index, entry) in TOP.iter().enumerate() {
            rows.push(
                tree_row(entry.name, entry.icon, entry.git, false, false, None, scale)
                    .id(("top", index))
                    .cursor_pointer()
                    .on_click(cx.listener(Self::tell(entry.tell)))
                    .into_any_element(),
            );
        }
        rows.push(
            tree_row(
                "notes",
                Icon::FolderSrcOpen,
                Git::Modified,
                false,
                false,
                Some(dot(MODIFIED).into_any_element()),
                scale,
            )
            .id("notes")
            .cursor_pointer()
            .on_click(cx.listener(Self::update(|this| this.notes_open = !this.notes_open)))
            .into_any_element(),
        );
        if self.notes_open {
            for (id, name, git, file) in [
                ("notes-go", "notes.go", Git::Modified, File::Notes),
                ("notes-test", "notes_test.go", Git::Untracked, File::Test),
                ("store-go", "store.go", Git::Clean, File::Store),
            ] {
                rows.push(
                    tree_row(name, Icon::Go, git, true, selected(file), None, scale)
                        .id(id)
                        .cursor_pointer()
                        .on_click(cx.listener(Self::update(move |this| {
                            this.file = file;
                            if file == File::Test {
                                this.test_open = true;
                            }
                        })))
                        .into_any_element(),
                );
            }
        }
        for (index, entry) in WEB.iter().enumerate() {
            let tail = match (entry.git, index) {
                (Git::Modified, 0) => Some(dot(DANGER).into_any_element()),
                (Git::Conflict, _) => Some(
                    text(11.0, 23.0, super::parts::git_ink(Git::Conflict), "conflict")
                        .into_any_element(),
                ),
                _ => None,
            };
            rows.push(
                tree_row(
                    entry.name,
                    entry.icon,
                    entry.git,
                    entry.nested,
                    false,
                    tail,
                    scale,
                )
                .id(("web", index))
                .cursor_pointer()
                .on_click(cx.listener(Self::tell(entry.tell)))
                .into_any_element(),
            );
        }
        tree(291.0).children(rows).child(div().flex_1()).child(
            div()
                .id("filter")
                .cursor_pointer()
                .px(px(10.0))
                .py(px(6.0))
                .child(quiet(11.5, "type to filter"))
                .on_click(cx.listener(Self::update(|this| this.filtering = !this.filtering))),
        )
    }

    fn shell_around(&self, header: Div, side: Div, scale: f32, cx: &mut Context<Self>) -> Div {
        let notes = self.file == File::Notes;
        let fixture = matches!(self.source, Source::Fixture { .. });
        let (crumb, symbol, state) = match self.file {
            File::Notes => ("notes.go", " › Count", "modified · unsaved"),
            File::Test => ("notes_test.go", " › TestCount", "untracked"),
            File::Store => ("store.go", " › Store", "saved"),
        };
        let crumbs = div()
            .flex()
            .flex_none()
            .items_center()
            .gap(px(6.0))
            .pt(px(9.0))
            .px(px(16.0))
            .pb(px(6.0));
        let crumbs = match &self.source {
            Source::Disk { file: None, .. } => crumbs,
            Source::Disk {
                file: Some((_, disk)),
                ..
            } => crumbs
                .children(disk.folders.iter().flat_map(|folder| {
                    [
                        text(12.5, 23.0, white(T3), folder.clone()),
                        quiet(12.5, "›"),
                    ]
                }))
                .child(ellipsis(text(12.5, 23.0, white(T2), disk.name.clone())))
                .child(div().flex_1())
                .child(ellipsis(quiet(
                    11.5,
                    if disk.dirty {
                        "modified · unsaved"
                    } else {
                        "saved"
                    },
                ))),
            Source::Fixture { .. } => crumbs
                .child(quiet(12.5, "notes"))
                .child(quiet(12.5, "›"))
                .child(file_icon(Icon::Go.bytes(), 13.0, scale))
                .child(ellipsis(text(12.5, 23.0, white(T2), crumb)))
                .child(ellipsis(quiet(12.5, symbol)))
                .child(div().flex_1())
                .child(ellipsis(quiet(11.5, state))),
        };
        let agent = strip("this session · turn 4 · 2 min ago", scale)
            .child(div().flex_1())
            .child(
                square(22.0, 7.0)
                    .id("trace")
                    .cursor_pointer()
                    .child(glyph(TRACE, 13.0, AGENT, scale))
                    .on_click(cx.listener(Self::update(|this| this.trace = !this.trace))),
            );

        let hint = match &self.source {
            Source::Disk {
                folder: Some(_), ..
            } => "Click a file in the tree to open it.",
            _ => "Run desk with --file to open one.",
        };
        let code_area = match (self.who_view(&ActiveTheme::theme(cx)), self.opened()) {
            (Some(who), _) => who,
            (None, None) => empty_state(
                "editor-shut",
                "No file open",
                Some(hint.into()),
                &[],
                &[],
                &ActiveTheme::theme(cx),
                |_, _, _| {},
            )
            .into_any_element(),
            (None, Some(Ok(editor))) => {
                let finder = editor.clone();
                div()
                    .size_full()
                    .on_action(move |_: &Find, window, cx| {
                        eprintln!("desk: find editor open");
                        finder.update(cx, |editor, cx| editor.find(window, cx));
                    })
                    .child(editor.clone())
                    .into_any_element()
            }
            (None, Some(Err(error))) => empty_state(
                "editor-failure",
                "Could not open the file",
                Some(error.clone()),
                &[],
                &[],
                &ActiveTheme::theme(cx),
                |_, _, _| {},
            )
            .into_any_element(),
        };
        let editor = div()
            .relative()
            .flex()
            .flex_col()
            .flex_1()
            .min_w_0()
            .child(crumbs)
            .when(notes && fixture, |editor| editor.child(agent))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .flex_1()
                    .min_h_0()
                    .overflow_hidden()
                    .pt(px(2.0))
                    .child(code_area),
            )
            .when(self.trace, |editor| editor.child(self.trace_pop(cx)));

        div().child(
            shell()
                .flex_1()
                .child(header)
                .child(inner().flex_row().child(side).child(editor))
                .when(self.prefs, |shell| shell.child(self.prefs_pop(cx))),
        )
    }

    fn file_tab(&self, disk: Option<&OnDisk>, cx: &mut Context<Self>) -> Div {
        let tabs: Vec<Tab> = disk
            .map(|disk| Tab {
                label: disk.name.clone(),
                icon: Some(Glyph::File),
                count: None,
                mark: if disk.dirty {
                    TabMark::Dirty
                } else {
                    TabMark::Close
                },
                flag: None,
            })
            .into_iter()
            .collect();
        let editor = cx.weak_entity();
        div().flex_1().min_w_0().child(connected_tabs(
            "editor-file-tabs",
            &tabs,
            0,
            1,
            &ActiveTheme::theme(cx),
            move |event, _, cx| {
                if let TabEvent::Close(_) = event
                    && let Err(error) = editor.update(cx, |editor, cx| editor.shut_file(cx))
                {
                    eprintln!("desk: editor is gone before its tab closed: {error}");
                }
            },
        ))
    }

    fn fixture_tabs(&self, header: Div, notes: bool, scale: f32, cx: &mut Context<Self>) -> Div {
        header
            .child(
                tab(Icon::Go, "notes.go", notes, scale)
                    .id("tab-notes")
                    .cursor_pointer()
                    .on_click(cx.listener(Self::update(|this| this.file = File::Notes)))
                    .child(
                        div()
                            .flex_none()
                            .size(px(7.0))
                            .mx(px(4.0))
                            .rounded(px(4.0))
                            .bg(white(0.85)),
                    ),
            )
            .when(self.test_open, |head| {
                head.child(
                    tab(Icon::Go, "notes_test.go", self.file == File::Test, scale)
                        .id("tab-test")
                        .cursor_pointer()
                        .on_click(cx.listener(Self::update(|this| this.file = File::Test)))
                        .child(close_mark().id("close-test").on_click(cx.listener(
                            |this, _, _, cx| {
                                cx.stop_propagation();
                                this.test_open = false;
                                if this.file == File::Test {
                                    this.file = File::Notes;
                                }
                                cx.notify();
                            },
                        ))),
                )
            })
            .child(div().flex_1())
    }

    fn prefs_pop(&self, cx: &mut Context<Self>) -> Div {
        let rows: [(&'static str, &'static str, bool); 4] = [
            (
                "Git blame",
                "Turns git blame on the current line off.",
                true,
            ),
            (
                "Agent blame",
                "Turns agent blame on the current line off.",
                true,
            ),
            (
                "Change marks",
                "Turns the gutter marks for changed lines off.",
                true,
            ),
            ("Word wrap", "Turns word wrap on for this file.", false),
        ];
        div()
            .absolute()
            .right(px(10.0))
            .top(px(40.0))
            .w(px(240.0))
            .p(px(6.0))
            .rounded(px(12.0))
            .bg(POP)
            .shadow(pop_shadow())
            .children(
                rows.into_iter()
                    .enumerate()
                    .map(|(index, (label, tell, on))| {
                        pop_row(label, white(0.9), None)
                            .h(px(POP_ROW))
                            .child(if on {
                                div().text_color(ADD).child("on")
                            } else {
                                div().text_color(white(T3)).child("off")
                            })
                            .id(("prefs-row", index))
                            .cursor_pointer()
                            .on_click(cx.listener(Self::tell(tell)))
                    }),
            )
    }

    fn trace_pop(&self, cx: &mut Context<Self>) -> Div {
        let rows: [(&'static str, &'static str, Option<&'static str>, gpui::Rgba); 4] = [
            (
                "Mention in chat",
                "Adds this to the chat as a reference the lead can read.",
                Some("@"),
                white(0.9),
            ),
            (
                "Open in File edits",
                "Opens this edit in File edits: the diff, the turn and the agent's reason.",
                None,
                white(0.9),
            ),
            (
                "Copy trace",
                "Copies the trace id, to paste into any chat or a ticket.",
                None,
                white(0.9),
            ),
            (
                "Undo this edit",
                "Puts back the lines this edit changed, from tofu's snapshot, after a confirm.",
                None,
                DANGER,
            ),
        ];
        div()
            .absolute()
            .right(px(16.0))
            .top(px(74.0))
            .w(px(250.0))
            .p(px(6.0))
            .rounded(px(12.0))
            .bg(POP)
            .shadow(pop_shadow())
            .child(
                div()
                    .px(px(10.0))
                    .py(px(6.0))
                    .font_family(MONO)
                    .text_size(px(13.0))
                    .line_height(px(POP_LINE))
                    .text_color(white(T3))
                    .child("tr#4e19a2"),
            )
            .children(
                rows.into_iter()
                    .enumerate()
                    .map(|(index, (label, tell, key, color))| {
                        pop_row(label, color, key)
                            .h(px(POP_ROW))
                            .id(("trace-row", index))
                            .cursor_pointer()
                            .on_click(cx.listener(Self::tell(tell)))
                    }),
            )
    }

    pub fn changes_board(&mut self, scale: f32, cx: &mut Context<Self>) -> Div {
        let segment = |id: &'static str, label: &'static str, mode: Mode| {
            let on = self.mode == mode;
            div()
                .id(id)
                .flex()
                .items_center()
                .gap(px(4.0))
                .h(px(19.0))
                .px(px(10.0))
                .rounded(px(6.0))
                .cursor_pointer()
                .text_size(px(12.5))
                .line_height(px(17.0))
                .font_weight(FontWeight::MEDIUM)
                .text_color(if on { white(1.0) } else { white(0.5) })
                .when(on, |segment| segment.bg(white(0.12)))
                .child(label)
        };
        let header = head(228.0, None)
            .child(tab(Icon::Go, "store.go", true, scale).child(
                close_mark().id("close-store").on_click(
                    cx.listener(Self::tell("Closes store.go. It has no unsaved changes.")),
                ),
            ))
            .child(div().flex_1())
            .child(
                div()
                    .flex()
                    .flex_none()
                    .gap(px(2.0))
                    .p(px(2.0))
                    .mb(px(6.0))
                    .mr(px(4.0))
                    .rounded(px(8.0))
                    .bg(white(0.05))
                    .child(
                        segment("mode-edit", "Edit", Mode::Edit)
                            .on_click(cx.listener(Self::update(|this| this.mode = Mode::Edit))),
                    )
                    .child(
                        segment("mode-changes", "Changes", Mode::Changes)
                            .child(div().text_color(ADD).child("+31"))
                            .child(div().text_color(DEL).child("-18"))
                            .on_click(cx.listener(Self::update(|this| this.mode = Mode::Changes))),
                    )
                    .child(
                        segment("mode-who", "Who wrote it", Mode::Who)
                            .on_click(cx.listener(Self::update(|this| this.mode = Mode::Who))),
                    ),
            );
        let entries: [(&'static str, Icon, Git, bool, bool, &'static str); 5] = [
            (
                "notes",
                Icon::FolderSrcOpen,
                Git::Modified,
                false,
                false,
                "Collapses notes.",
            ),
            (
                "notes.go",
                Icon::Go,
                Git::Modified,
                true,
                false,
                "Opens notes/notes.go in a preview tab.",
            ),
            (
                "store.go",
                Icon::Go,
                Git::Modified,
                true,
                true,
                "Shows notes/store.go, the tab already open.",
            ),
            (
                "notes_test.go",
                Icon::Go,
                Git::Untracked,
                true,
                false,
                "Opens notes/notes_test.go in a preview tab.",
            ),
            (
                "web",
                Icon::FolderPublic,
                Git::Clean,
                false,
                false,
                "Expands web.",
            ),
        ];
        let side = tree(229.0).children(entries.into_iter().enumerate().map(
            |(index, (name, icon, git, nested, on, tell))| {
                let tail = (index == 0).then(|| dot(MODIFIED).into_any_element());
                tree_row(name, icon, git, nested, on, tail, scale)
                    .when(nested, |row| row.children(None::<Div>))
                    .id(("tree", index))
                    .cursor_pointer()
                    .on_click(cx.listener(Self::tell(tell)))
            },
        ));
        let body: Vec<AnyElement> = match self.mode {
            Mode::Changes => STORE_DIFF
                .iter()
                .map(|line| code(line, 70.0).into_any_element())
                .chain(std::iter::once(
                    div()
                        .flex()
                        .items_center()
                        .gap(px(8.0))
                        .px(px(16.0))
                        .py(px(12.0))
                        .child(
                            div()
                                .flex_1()
                                .min_w_0()
                                .text_size(px(12.5))
                                .line_height(px(23.0))
                                .text_color(white(T3))
                                .child("31 added, 18 removed · against HEAD · go-dev wrote all of it this session"),
                        )
                        .child(
                            medium(13.0, 26.0, white(T3), "Undo this file")
                                .id("undo-file")
                                .cursor_pointer()
                                .h(px(26.0))
                                .px(px(12.0))
                                .rounded(px(8.0))
                                .bg(white(0.07))
                                .on_click(cx.listener(Self::tell(
                                    "Puts back notes/store.go as it was before this session, from tofu's undo snapshot. Other files stay.",
                                ))),
                        )
                        .into_any_element(),
                ))
                .collect(),
            Mode::Edit | Mode::Who => STORE
                .iter()
                .map(|line| {
                    code(line, 70.0)
                        .when(self.mode == Mode::Who && line.number == "4", |row| {
                            row.child(person_blame())
                        })
                        .into_any_element()
                })
                .collect(),
        };
        let editor = div()
            .flex()
            .flex_col()
            .flex_1()
            .min_w_0()
            .child(strip("this session · turn 7 · now", scale).mt(px(10.0)))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .flex_1()
                    .overflow_hidden()
                    .pt(px(2.0))
                    .children(body),
            );
        div()
            .child(
                shell()
                    .flex_1()
                    .child(header)
                    .child(inner().flex_row().child(side).child(editor)),
            )
            .child(self.child_chat(scale, cx))
    }

    fn child_chat(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        let chip = |id: &'static str, label: &'static str, chevron: bool, tell: &'static str| {
            div()
                .id(id)
                .flex()
                .flex_none()
                .items_center()
                .gap(px(6.0))
                .h(px(24.0))
                .px(px(9.0))
                .rounded(px(7.0))
                .cursor_pointer()
                .child(medium(12.5, 14.0, white(0.85), label))
                .when(chevron, |chip| {
                    chip.child(glyph(DOWN, 13.0, white(0.85), scale))
                })
                .on_click(cx.listener(Self::tell(tell)))
        };
        shell()
            .flex_none()
            .w(relative(CHAT_SHARE))
            .min_w(px(CHAT_LEAST))
            .max_w(px(CHAT_MOST))
            .child(
                div()
                    .flex()
                    .flex_none()
                    .items_center()
                    .gap(px(6.0))
                    .h(px(28.0))
                    .pl(px(9.0))
                    .pr(px(6.0))
                    .child(glyph(CHAT, 13.0, white(0.55), scale))
                    .child(cap("Chat · child").flex_1())
                    .child(text(12.0, 12.0, white(T3), "reads only")),
            )
            .child(
                inner()
                    .flex_col()
                    .child(
                        div()
                            .flex_1()
                            .px(px(18.0))
                            .py(px(14.0))
                            .text_size(px(12.5))
                            .line_height(px(21.0))
                            .text_color(white(T3))
                            .child("Child of clear-sable-eagle. It knows what the lead knows, and never edits or spawns."),
                    )
                    .child(
                        div().pt(px(8.0)).px(px(10.0)).pb(px(10.0)).child(
                            div()
                                .flex()
                                .items_center()
                                .gap(px(4.0))
                                .p(px(6.0))
                                .rounded(px(16.0))
                                .bg(black(0.22))
                                .shadow(vec![ring(white(0.09))])
                                .child(
                                    square(28.0, 7.0)
                                        .id("attach")
                                        .cursor_pointer()
                                        .child(glyph(PLUS, 16.0, white(0.45), scale))
                                        .on_click(cx.listener(Self::tell(
                                            "Attaches a file or an image; it goes to the lead as a reference, like an @ mention.",
                                        ))),
                                )
                                .child(text(14.0, 23.0, white(T3), "Ask about this file").flex_1())
                                .child(chip("model", "Opus 5", true, "Picks the child chat's model; it reads only, whatever the model."))
                                .child(chip("effort", "Medium", false, "Sets how hard the child thinks before it answers."))
                                .child(
                                    square(28.0, 14.0)
                                        .id("send")
                                        .cursor_pointer()
                                        .bg(white(0.18))
                                        .child(glyph(SEND, 16.0, white(0.9), scale))
                                        .on_click(cx.listener(Self::tell(
                                            "Sends the question to the child chat with store.go as a reference.",
                                        ))),
                                ),
                        ),
                    ),
            )
    }

    pub fn split_board(&mut self, scale: f32, cx: &mut Context<Self>) -> Div {
        let header = head(258.0, None)
            .child(
                tab(Icon::Go, "notes.go", true, scale).child(
                    close_mark()
                        .id("close-left")
                        .on_click(cx.listener(Self::tell(
                            "Closes notes.go on the left; the right pane takes the whole editor.",
                        ))),
                ),
            )
            .child(div().flex_1())
            .child(
                tab(Icon::Go, "notes_test.go", true, scale).child(
                    close_mark()
                        .id("close-right")
                        .on_click(cx.listener(Self::tell(
                            "Closes notes_test.go on the right and joins the split.",
                        ))),
                ),
            )
            .child(
                icon_button("split", SPLIT, 26.0, scale)
                    .mb(px(5.0))
                    .on_click(cx.listener(Self::tell("Joins the two panes back into one."))),
            );
        let entries: [(&'static str, Icon, Git, bool, bool); 7] = [
            ("notes", Icon::FolderSrcOpen, Git::Modified, false, false),
            ("notes.go", Icon::Go, Git::Modified, true, true),
            ("notes_test.go", Icon::Go, Git::Untracked, true, false),
            ("store.go", Icon::Go, Git::Clean, true, false),
            ("web", Icon::FolderPublic, Git::Modified, false, true),
            ("node_modules", Icon::Folder, Git::Ignored, false, false),
            ("README.md", Icon::Readme, Git::Clean, false, false),
        ];
        let side = tree(259.0).children(entries.into_iter().enumerate().map(
            |(index, (name, icon, git, nested, on))| {
                let tail = match name {
                    "web" => Some(quiet(12.0, "⋯").into_any_element()),
                    "node_modules" => {
                        Some(text(11.0, 23.0, white(0.9), "312 MB").into_any_element())
                    }
                    _ => None,
                };
                let on = on && (name != "web" || self.menu);
                tree_row(name, icon, git, nested, on, tail, scale)
                    .id(("tree", index))
                    .cursor_pointer()
                    .on_click(cx.listener(Self::update(move |this| {
                        if name == "web" {
                            this.menu = !this.menu;
                        } else {
                            this.menu = false;
                            this.told = Some(SharedString::from(format!(
                                "Opens {name} in a preview tab."
                            )));
                        }
                    })))
            },
        ));
        let pane = |lines: &'static [Line], border: bool| {
            div()
                .flex()
                .flex_col()
                .flex_1()
                .min_w_0()
                .overflow_hidden()
                .pt(px(10.0))
                .when(border, |pane| pane.border_l_1().border_color(black(0.35)))
                .children(lines.iter().map(|line| code(line, 70.0)))
        };
        let body = inner()
            .flex_row()
            .child(side)
            .child(pane(COUNT, false))
            .child(pane(COUNT_TEST, true))
            .when(self.menu, |body| body.child(self.menu_pop(cx)));
        div().child(shell().flex_1().child(header).child(body))
    }

    fn menu_pop(&self, cx: &mut Context<Self>) -> Div {
        let mut rows: Vec<AnyElement> = Vec::new();
        for (group, items) in MENU.iter().enumerate() {
            if group > 0 {
                rows.push(
                    div()
                        .h(px(1.0))
                        .mx(px(6.0))
                        .my(px(4.0))
                        .bg(white(0.07))
                        .into_any_element(),
                );
            }
            for (index, (label, key, tell)) in items.iter().enumerate() {
                let color = if *label == "Delete" {
                    DANGER
                } else {
                    white(0.9)
                };
                let tell = *tell;
                rows.push(
                    pop_row(label, color, *key)
                        .id(("menu", group * 10 + index))
                        .cursor_pointer()
                        .on_click(cx.listener(Self::update(move |this| {
                            this.menu = false;
                            this.told = Some(tell.into());
                        })))
                        .into_any_element(),
                );
            }
        }
        div()
            .absolute()
            .left(px(150.0))
            .top(px(126.0))
            .w(px(250.0))
            .p(px(5.0))
            .rounded(px(12.0))
            .bg(POP)
            .shadow(vec![ring(white(0.12))])
            .children(rows)
    }
}
