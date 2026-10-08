use gpui::{AnyElement, Div, FontWeight, Rgba, div, prelude::*, px};

use super::fixture::{Change, Git, Icon, Ink, Line};
use super::kit::{
    ADD, CONFLICT, DANGER, DEL, FN, KW, MODIFIED, MONO, ST, T3, TY, UNTRACKED, ellipsis, file_icon,
    medium, text, tint, white,
};

pub const ROW: f32 = 22.0;
const NUMBER_PAD: f32 = 18.0;

pub fn ink(ink: Ink) -> Rgba {
    match ink {
        Ink::Plain | Ink::Word => white(0.9),
        Ink::Kw => KW,
        Ink::Fn => FN,
        Ink::Ty => TY,
        Ink::St => ST,
        Ink::Dim => white(T3),
        Ink::Add => ADD,
        Ink::Del => DEL,
    }
}

pub fn git_ink(git: Git) -> Rgba {
    match git {
        Git::Clean | Git::Ignored => white(0.9),
        Git::Modified => MODIFIED,
        Git::Untracked => UNTRACKED,
        Git::Deleted => DANGER,
        Git::Conflict => CONFLICT,
    }
}

pub fn row(line: &Line) -> Div {
    let fill = match line.change {
        Change::Same | Change::Hunk => white(0.0),
        Change::Add => tint(0x52c68e, 0.09),
        Change::Del => tint(0xf1737d, 0.09),
    };
    div()
        .relative()
        .flex()
        .flex_none()
        .h(px(ROW))
        .bg(fill)
        .font_family(MONO)
        .text_size(px(13.0))
        .line_height(px(ROW))
        .whitespace_nowrap()
}

pub fn number(label: &'static str, width: f32) -> Div {
    div()
        .relative()
        .flex()
        .flex_none()
        .justify_end()
        .w(px(width))
        .pr(px(NUMBER_PAD))
        .text_color(white(0.24))
        .child(label)
}

pub fn runs(line: &Line) -> Vec<AnyElement> {
    line.runs
        .iter()
        .enumerate()
        .filter(|(_, (_, body))| !body.trim().is_empty())
        .map(|(index, (kind, body))| {
            let run = div().flex_none().text_color(ink(*kind)).child(*body);
            match (kind, line.change, index) {
                (Ink::Word, _, _) => run.rounded(px(3.0)).bg(tint(0x52c68e, 0.25)),
                (_, Change::Hunk, 1) => run.ml(px(8.0)),
                _ => run,
            }
            .into_any_element()
        })
        .collect()
}

pub fn code(line: &'static Line, width: f32) -> Div {
    let indent = if line.change == Change::Hunk {
        div().flex_none().w(px(width - NUMBER_PAD))
    } else {
        number(line.number, width)
    };
    row(line).child(indent).children(runs(line))
}

pub fn dot(color: Rgba) -> Div {
    div().flex_none().size(px(6.0)).rounded(px(3.0)).bg(color)
}

pub fn tree_row(
    name: &'static str,
    icon: Icon,
    git: Git,
    nested: bool,
    selected: bool,
    tail: Option<AnyElement>,
    scale: f32,
) -> Div {
    let label = div()
        .flex_1()
        .min_w_0()
        .truncate()
        .text_color(git_ink(git))
        .child(name);
    div()
        .relative()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(7.0))
        .h(px(28.0))
        .pl(px(if nested { 30.0 } else { 10.0 }))
        .pr(px(10.0))
        .rounded(px(7.0))
        .text_size(px(13.5))
        .line_height(px(23.0))
        .when(selected, |row| row.bg(white(0.08)))
        .when(git == Git::Ignored, |row| row.opacity(0.42).italic())
        .when(nested, |row| {
            row.child(
                div()
                    .absolute()
                    .top_0()
                    .bottom_0()
                    .left(px(17.0))
                    .w(px(1.0))
                    .bg(white(0.06)),
            )
        })
        .child(file_icon(icon.bytes(), 16.0, scale))
        .child(if git == Git::Deleted {
            label.line_through()
        } else {
            label
        })
        .children(tail)
}

pub fn tab(icon: Icon, label: &'static str, on: bool, scale: f32) -> Div {
    div()
        .flex()
        .min_w_0()
        .items_center()
        .gap(px(7.0))
        .h(px(31.0))
        .pl(px(10.0))
        .pr(px(4.0))
        .rounded_t(px(9.0))
        .when(on, |tab| tab.bg(white(0.05)))
        .child(file_icon(icon.bytes(), 14.0, scale))
        .child(ellipsis(medium(
            12.5,
            14.0,
            if on { white(1.0) } else { white(0.5) },
            label,
        )))
}

pub fn close_mark() -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(20.0))
        .rounded(px(7.0))
        .text_size(px(14.0))
        .line_height(px(20.0))
        .font_weight(FontWeight::MEDIUM)
        .text_color(white(0.45))
        .child("×")
}

pub fn quiet(size: f32, body: &'static str) -> Div {
    text(size, 23.0, white(T3), body)
}

pub fn kbd(body: &'static str) -> Div {
    div()
        .flex_none()
        .h(px(18.0))
        .px(px(6.0))
        .rounded(px(5.0))
        .bg(white(0.08))
        .font_family(MONO)
        .text_size(px(11.0))
        .line_height(px(18.0))
        .font_weight(FontWeight::MEDIUM)
        .text_color(white(0.5))
        .child(body)
}
