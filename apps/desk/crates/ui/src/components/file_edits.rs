use std::rc::Rc;

use gpui::{AnyElement, App, Div, ElementId, SharedString, Stateful, Window, div, prelude::*, px};

use crate::components::card::caption;
use crate::components::chip::tabular;
use crate::components::diff::{DiffCard, FileDiff};
use crate::components::list::HoverList;
use crate::components::paint::ink;
use crate::components::size::{
    DIM_TEXT, FONT_SMALL, FONT_TAB, FONT_WHO, GRID_PAD_X, GRID_ROW, GRID_WHO, ROW_GAP, T1, T2, T3,
};
use crate::theme::Theme;

const HEAD_ROW: f32 = 25.0;
const EDITS_COLUMN: f32 = 52.0;
const WHEN_COLUMN: f32 = 60.0;
const HISTORY_GAP: f32 = 10.0;
const EDIT_GAP: f32 = 6.0;
const HISTORY_HEAD_GAP: f32 = 3.0;

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
    pub diff: FileDiff,
}

impl EditedFile {
    fn name_and_dir(&self) -> (SharedString, SharedString) {
        match self.path.rsplit_once('/') {
            Some((dir, name)) => (name.to_owned().into(), format!("{dir}/").into()),
            None => (self.path.clone(), SharedString::default()),
        }
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

fn file_row(ix: usize, file: &EditedFile, theme: &Theme) -> Stateful<Div> {
    let (name, dir) = file.name_and_dir();
    let dim = ink(theme, T3);
    grid_line(("file-row", ix))
        .h(px(GRID_ROW))
        .cursor_pointer()
        .text_color(ink(theme, T1))
        .child(
            file_cell().child(name).child(
                div()
                    .min_w_0()
                    .truncate()
                    .text_size(px(FONT_WHO))
                    .text_color(dim)
                    .child(dir),
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
    let (name, dir) = file.name_and_dir();
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
            .child(DiffCard::new(("file-edit", ix), Rc::new(edit.diff.clone())))
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
