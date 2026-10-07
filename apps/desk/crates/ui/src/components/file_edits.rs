use std::rc::Rc;

use gpui::{
    AnyElement, App, ClickEvent, Div, ElementId, Rgba, SharedString, Stateful, Window, div,
    prelude::*, px,
};

use crate::components::button::{ButtonKind, button};
use crate::components::card::caption;
use crate::components::chip::{GitStatus, Tone, badge, git_name, mono, tabular};
use crate::components::diff::{DiffCard, FileChange, FileDiff};
use crate::components::glyph::Glyph;
use crate::components::list::{HoverList, group_header};
use crate::components::paint::ink;
use crate::components::size::{
    DIM_TEXT, FONT_SMALL, FONT_TAB, FONT_WHO, GRID_PAD_X, GRID_ROW, GRID_WHO, ROW_GAP, T1, T2, T3,
};
use crate::theme::{ColorToken, Theme};

const HEAD_ROW: f32 = 25.0;
const EDITS_COLUMN: f32 = 52.0;
const COUNTS_COLUMN: f32 = 56.0;
const WHEN_COLUMN: f32 = 60.0;
const HISTORY_GAP: f32 = 10.0;
const EDIT_GAP: f32 = 6.0;
const HISTORY_HEAD_GAP: f32 = 3.0;
const COMMIT_PAD_Y: f32 = 10.0;

#[derive(Clone)]
pub struct EditedFile {
    pub path: SharedString,
    pub edits: Vec<FileEdit>,
    pub by: Vec<SharedString>,
}

#[derive(Clone)]
pub struct FileEdit {
    pub agent: Option<SharedString>,
    pub at: SharedString,
    pub diff: Rc<FileDiff>,
}

impl EditedFile {
    fn added(&self) -> usize {
        self.edits.iter().map(|edit| edit.diff.added()).sum()
    }

    fn removed(&self) -> usize {
        self.edits.iter().map(|edit| edit.diff.removed()).sum()
    }

    fn name_color(&self, theme: &Theme) -> Rgba {
        let token = match self.edits.last().map(|edit| edit.diff.change()) {
            None => return ink(theme, T1),
            Some(FileChange::Added) => ColorToken::GitAdded,
            Some(FileChange::Deleted) => ColorToken::GitDeleted,
            Some(FileChange::Modified | FileChange::Renamed { .. }) => ColorToken::GitModified,
        };
        theme.color(token)
    }

    fn edit_count(&self) -> SharedString {
        match self.edits.len() {
            1 => "1 edit".into(),
            count => format!("{count} edits").into(),
        }
    }

    fn editors(&self) -> SharedString {
        match self.by.as_slice() {
            [] => SharedString::default(),
            [one] => one.clone(),
            [first, rest @ ..] => format!("{first} +{}", rest.len()).into(),
        }
    }
}

fn name_and_dir(path: &SharedString) -> (SharedString, SharedString) {
    match path.rsplit_once('/') {
        Some((dir, name)) => (name.to_owned().into(), format!("{dir}/").into()),
        None => (path.clone(), SharedString::default()),
    }
}

fn grid_line(id: impl Into<ElementId>) -> Stateful<Div> {
    div()
        .id(id)
        .w_full()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(ROW_GAP))
        .px(px(GRID_PAD_X))
        .text_size(px(FONT_TAB))
}

fn file_cell() -> Div {
    div()
        .flex_1()
        .min_w_0()
        .flex()
        .items_baseline()
        .gap_1p5()
        .whitespace_nowrap()
        .overflow_hidden()
}

fn fixed(width: f32) -> Div {
    div().w(px(width)).flex_none().min_w_0().truncate()
}

fn path_cell(name: Div, dir: SharedString, theme: &Theme) -> Div {
    file_cell().child(name).child(
        div()
            .min_w_0()
            .truncate()
            .text_size(px(FONT_WHO))
            .text_color(ink(theme, T3))
            .child(dir),
    )
}

fn file_row(ix: usize, file: &EditedFile, theme: &Theme) -> Stateful<Div> {
    let (name, dir) = name_and_dir(&file.path);
    let dim = ink(theme, T3);
    grid_line(("file-row", ix))
        .h(px(GRID_ROW))
        .cursor_pointer()
        .text_color(ink(theme, T1))
        .child(path_cell(
            div().text_color(file.name_color(theme)).child(name),
            dir,
            theme,
        ))
        .child(
            fixed(COUNTS_COLUMN)
                .flex()
                .gap_1p5()
                .text_size(px(FONT_WHO))
                .font_family(mono(theme))
                .font_features(tabular())
                .child(
                    div()
                        .text_color(Tone::Added.color(theme))
                        .child(format!("+{}", file.added())),
                )
                .child(
                    div()
                        .text_color(Tone::Deleted.color(theme))
                        .child(format!("-{}", file.removed())),
                ),
        )
        .child(
            fixed(GRID_WHO)
                .text_color(ink(theme, T2))
                .child(file.editors()),
        )
        .child(
            fixed(EDITS_COLUMN)
                .text_size(px(FONT_WHO))
                .text_color(dim)
                .child(file.edit_count()),
        )
        .child(
            fixed(WHEN_COLUMN)
                .text_right()
                .text_size(px(FONT_WHO))
                .font_features(tabular())
                .text_color(dim)
                .children(file.edits.last().map(|edit| edit.at.clone())),
        )
}

fn head_row(files: &[EditedFile], theme: &Theme) -> Stateful<Div> {
    let noun = if files.len() == 1 { "file" } else { "files" };
    grid_line("file-edits-head")
        .h(px(HEAD_ROW))
        .child(
            file_cell().child(caption("file", theme)).child(
                div()
                    .text_size(px(FONT_SMALL))
                    .text_color(ink(theme, T3))
                    .child(format!("{} {noun}", files.len())),
            ),
        )
        .child(fixed(COUNTS_COLUMN).child(caption("lines", theme)))
        .child(fixed(GRID_WHO).child(caption("by", theme)))
        .child(fixed(EDITS_COLUMN).child(caption("edits", theme)))
        .child(
            fixed(WHEN_COLUMN)
                .flex()
                .justify_end()
                .child(caption("when", theme)),
        )
}

pub fn file_edits(
    id: impl Into<ElementId>,
    files: &[EditedFile],
    theme: &Theme,
    on_open: impl Fn(usize, &mut Window, &mut App) + 'static,
) -> AnyElement {
    let on_open = Rc::new(on_open);
    let rows = files.iter().enumerate().map(|(ix, file)| {
        let on_open = on_open.clone();
        file_row(ix, file, theme).on_click(move |_, window, cx| on_open(ix, window, cx))
    });
    HoverList::new(id, theme)
        .inert(head_row(files, theme))
        .items(rows)
        .when(files.is_empty(), |list| {
            list.inert(
                grid_line("file-edits-empty")
                    .h(px(GRID_ROW))
                    .text_color(ink(theme, DIM_TEXT))
                    .child("Files the agents edit appear here."),
            )
        })
        .into_any_element()
}

fn edit_line(edit: &FileEdit, theme: &Theme) -> Div {
    div()
        .flex()
        .items_baseline()
        .gap_1p5()
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, T3))
        .children(
            edit.agent
                .clone()
                .map(|agent| div().text_color(ink(theme, T2)).child(agent)),
        )
        .child(format!("edited \u{b7} {}", edit.at))
}

pub fn file_history(id: impl Into<ElementId>, file: &EditedFile, theme: &Theme) -> AnyElement {
    let (name, dir) = name_and_dir(&file.path);
    let head = div()
        .flex()
        .flex_col()
        .gap(px(HISTORY_HEAD_GAP))
        .child(
            div()
                .flex()
                .items_baseline()
                .gap_1p5()
                .text_size(px(FONT_TAB))
                .text_color(ink(theme, T1))
                .child(name)
                .child(
                    div()
                        .text_size(px(FONT_WHO))
                        .text_color(ink(theme, T3))
                        .child(dir),
                ),
        )
        .child(
            div()
                .text_size(px(FONT_SMALL))
                .text_color(ink(theme, T3))
                .child(file.edit_count()),
        );
    let edits = file.edits.iter().enumerate().rev().map(|(ix, edit)| {
        div()
            .flex()
            .flex_col()
            .gap(px(EDIT_GAP))
            .child(edit_line(edit, theme))
            .child(DiffCard::new(("file-edit", ix), edit.diff.clone()))
    });
    div()
        .id(id)
        .flex()
        .flex_col()
        .gap(px(HISTORY_GAP))
        .child(head)
        .children(edits)
        .into_any_element()
}
#[derive(Clone)]
pub struct ChangedFile {
    pub path: SharedString,
    pub status: GitStatus,
    pub staged: bool,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ChangeAction {
    Stage,
    Unstage,
    Discard,
}

impl ChangeAction {
    pub fn verb(self) -> &'static str {
        match self {
            ChangeAction::Stage => "stage",
            ChangeAction::Unstage => "unstage",
            ChangeAction::Discard => "discard",
        }
    }

    fn label(self) -> &'static str {
        match self {
            ChangeAction::Stage => "Stage",
            ChangeAction::Unstage => "Unstage",
            ChangeAction::Discard => "Discard",
        }
    }
}

impl ChangedFile {
    fn toggle(&self) -> ChangeAction {
        if self.staged {
            ChangeAction::Unstage
        } else {
            ChangeAction::Stage
        }
    }

    fn actions(&self) -> &'static [ChangeAction] {
        match (self.staged, self.status) {
            (true, _) => &[ChangeAction::Unstage],
            (false, GitStatus::Untracked | GitStatus::Ignored | GitStatus::Conflict) => {
                &[ChangeAction::Stage]
            }
            (false, GitStatus::Modified | GitStatus::Added | GitStatus::Deleted) => {
                &[ChangeAction::Discard, ChangeAction::Stage]
            }
        }
    }

    fn letter(&self) -> &'static str {
        match self.status {
            GitStatus::Modified => "M",
            GitStatus::Added => "A",
            GitStatus::Deleted => "D",
            GitStatus::Untracked => "U",
            GitStatus::Ignored => "I",
            GitStatus::Conflict => "C",
        }
    }
}

type OnChange = Rc<dyn Fn(usize, ChangeAction, &mut Window, &mut App)>;

fn change_row(ix: usize, file: &ChangedFile, theme: &Theme, on: &OnChange) -> Stateful<Div> {
    let (name, dir) = name_and_dir(&file.path);
    let (on_row, toggle) = (on.clone(), file.toggle());
    let buttons = file.actions().iter().map(|&action| {
        let on = on.clone();
        button(
            action.label(),
            action.label(),
            None,
            ButtonKind::Text,
            theme,
        )
        .on_click(move |_, window, cx| {
            cx.stop_propagation();
            on(ix, action, window, cx);
        })
    });
    grid_line(("change-row", ix))
        .h(px(GRID_ROW))
        .cursor_pointer()
        .text_color(ink(theme, T1))
        .child(path_cell(
            git_name(name, Some(file.status), theme),
            dir,
            theme,
        ))
        .child(badge(file.letter(), theme))
        .children(buttons)
        .on_click(move |_, window, cx| on_row(ix, toggle, window, cx))
}

type Belongs = fn(&ChangedFile) -> bool;

const CHANGE_GROUPS: [(&str, Belongs); 3] = [
    ("Staged", |file| file.staged),
    ("Changes", |file| {
        !file.staged && file.status != GitStatus::Untracked
    }),
    ("Untracked", |file| {
        !file.staged && file.status == GitStatus::Untracked
    }),
];

pub fn changed_files(
    id: impl Into<ElementId>,
    files: &[ChangedFile],
    theme: &Theme,
    on: impl Fn(usize, ChangeAction, &mut Window, &mut App) + 'static,
) -> AnyElement {
    let on: OnChange = Rc::new(on);
    CHANGE_GROUPS
        .iter()
        .fold(HoverList::new(id, theme), |list, (title, belongs)| {
            let members: Vec<(usize, &ChangedFile)> = files
                .iter()
                .enumerate()
                .filter(|(_, file)| belongs(file))
                .collect();
            list.inert(group_header(*title, title, members.len(), true, theme))
                .items(
                    members
                        .into_iter()
                        .map(|(ix, file)| change_row(ix, file, theme, &on)),
                )
        })
        .when(files.is_empty(), |list| {
            list.inert(
                grid_line("changes-empty")
                    .h(px(GRID_ROW))
                    .text_color(ink(theme, DIM_TEXT))
                    .child("Nothing to commit: the working tree is clean."),
            )
        })
        .into_any_element()
}

pub fn commit_box(
    message: impl IntoElement,
    staged: usize,
    theme: &Theme,
    on_commit: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> Div {
    div()
        .flex()
        .items_start()
        .gap(px(ROW_GAP))
        .px(px(GRID_PAD_X))
        .py(px(COMMIT_PAD_Y))
        .child(message)
        .child(
            button(
                "commit",
                format!("Commit {staged}"),
                Some(Glyph::Check),
                ButtonKind::Primary,
                theme,
            )
            .on_click(on_commit),
        )
}
