use desk_ui::components::paint::ink;
use desk_ui::theme::{Theme, WordToken};
use gpui::{
    ClickEvent, Context, Div, HighlightStyle, Rgba, Stateful, StyledText, div, prelude::*, px, rgb,
    rgb_to_hsla, rgba,
};

use super::fixture::{
    Author, CHANGES, COMMITTED, Change, HUNK, HUNK_EDIT, Kind, LINES, MESSAGE, Tone,
};
use super::fixture::{TELL_DISCARD_HUNK, TELL_STAGE_HUNK};
use super::kit::{
    self, ADD, AGENT, AGENT_TEXT, BASE, DANGER, DEL, MODIFIED, T2, T3, UNTRACKED, YOU, edge,
};
use super::{Git, Group};

const ROW: f32 = 28.0;
const LIST_GAP: f32 = 10.0;
const SIDE: f32 = 400.0;
const CODE_LINE: f32 = 22.0;
const GUTTER: f32 = 70.0;

impl Git {
    pub(super) fn changes(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        div()
            .child(
                kit::shell()
                    .flex_none()
                    .w(px(SIDE))
                    .child(
                        kit::shell_head(theme)
                            .child(kit::cap("Source control", theme).flex_1().pl(px(19.0)))
                            .child(
                                div()
                                    .text_size(px(12.0))
                                    .text_color(ink(theme, T3))
                                    .child("main \u{2191}2"),
                            ),
                    )
                    .child(self.side(theme, cx)),
            )
            .child(
                kit::shell()
                    .flex_1()
                    .child(diff_head(theme))
                    .child(self.diff(theme, cx)),
            )
    }

    fn side(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let list = kit::inner(theme)
            .p(px(LIST_GAP))
            .gap(px(LIST_GAP))
            .child(self.composer(theme, cx))
            .children(self.committed.then(|| {
                div()
                    .px(px(4.0))
                    .py(px(2.0))
                    .text_size(px(12.5))
                    .text_color(ink(theme, T2))
                    .child(format!(
                        "Committed {COMMITTED} \u{b7} {MESSAGE}. tofu never commits on its own; this was you."
                    ))
            }))
            .child(self.grouping(theme, cx));
        match self.group {
            Group::Status => self.by_status(list, theme, cx),
            Group::Author => by_author(list, theme),
        }
    }

    fn composer(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let staged = self.staged.iter().filter(|on| **on).count();
        let ready = staged > 0 && self.written;
        let commit = kit::button("commit", 26.0, 12.5, theme)
            .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                if this.written && this.staged.iter().any(|on| *on) {
                    this.committed = true;
                    this.written = false;
                    this.staged = [false; CHANGES.len()];
                    cx.notify();
                }
            }))
            .child(format!("Commit {staged}"));
        let commit = if ready {
            commit.bg(ink(theme, BASE)).text_color(rgb(0x111111))
        } else {
            commit
        };
        div()
            .flex()
            .flex_col()
            .gap(px(8.0))
            .py(px(10.0))
            .px(px(12.0))
            .rounded(px(11.0))
            .bg(rgba(0x00000040))
            .shadow(vec![edge(ink(theme, 0.08), 1.0)])
            .child(
                div()
                    .min_h(px(40.0))
                    .text_size(px(13.5))
                    .line_height(px(20.0))
                    .when(!self.written, |text| text.text_color(ink(theme, T3)))
                    .child(if self.written {
                        MESSAGE
                    } else {
                        "Commit message"
                    }),
            )
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(6.0))
                    .child(
                        kit::button("write", 26.0, 12.5, theme)
                            .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                                this.written = true;
                                cx.notify();
                            }))
                            .child(div().w(px(13.0)))
                            .child("Ask the lead to write it"),
                    )
                    .child(div().flex_1())
                    .child(commit),
            )
    }

    fn grouping(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let choice = |id: &'static str, label: &'static str, group: Group| {
            let on = self.group == group;
            div()
                .id(id)
                .px(px(8.0))
                .py(px(2.0))
                .rounded(px(5.0))
                .cursor_pointer()
                .text_color(ink(theme, if on { 1.0 } else { 0.5 }))
                .when(on, |item| item.bg(ink(theme, 0.12)))
                .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                    this.group = group;
                    cx.notify();
                }))
                .child(label)
        };
        div()
            .flex()
            .items_center()
            .gap(px(6.0))
            .px(px(2.0))
            .text_size(px(12.0))
            .child(div().flex_1().text_color(ink(theme, T3)).child("group by"))
            .child(
                div()
                    .flex()
                    .gap(px(2.0))
                    .p(px(2.0))
                    .rounded(px(7.0))
                    .bg(ink(theme, 0.05))
                    .child(choice("by-status", "status", Group::Status))
                    .child(choice("by-author", "who changed it", Group::Author)),
            )
    }

    fn by_status(&self, list: Div, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let staged = self.staged.iter().filter(|on| **on).count();
        let open = |tracked: bool| {
            CHANGES
                .iter()
                .enumerate()
                .filter(move |(index, change)| change.tracked == tracked && !self.staged[*index])
        };
        list.child(heading("Staged", staged, 4.0, theme))
            .children(
                CHANGES
                    .iter()
                    .enumerate()
                    .filter(|(index, _)| self.staged[*index])
                    .map(|(index, change)| self.row(index, change, theme, cx)),
            )
            .child(heading("Changes", open(true).count(), 10.0, theme))
            .children(open(true).map(|(index, change)| self.row(index, change, theme, cx)))
            .child(heading("Untracked", open(false).count(), 10.0, theme))
            .children(open(false).map(|(index, change)| self.row(index, change, theme, cx)))
    }

    fn row(
        &self,
        index: usize,
        change: &'static Change,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        let staged = self.staged[index];
        let tone = if staged {
            ink(theme, BASE)
        } else if change.tracked {
            rgb(MODIFIED)
        } else {
            rgb(UNTRACKED)
        };
        let check = div()
            .flex()
            .flex_none()
            .items_center()
            .justify_center()
            .size(px(14.0))
            .rounded(px(4.0));
        let check = if staged {
            check
                .bg(rgb(0xe1e1e6))
                .text_color(rgb(0x111111))
                .text_size(px(10.0))
                .child("\u{2713}")
        } else {
            check.shadow(vec![edge(ink(theme, 0.3), 1.5)])
        };
        file_row(("change", index), 6.0)
            .text_size(px(13.5))
            .when(index == 0 && !staged, |row| row.bg(ink(theme, 0.08)))
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                if change.stageable {
                    this.staged[index] = !this.staged[index];
                    this.committed = false;
                } else {
                    this.told = Some(change.tell.into());
                }
                cx.notify();
            }))
            .child(check)
            .child(kit::icon(change.kind, 16.0))
            .child(name(change.name, change.dir, tone, theme))
            .children((!staged).then(|| author_dot(change.author != Author::You)))
            .child(kit::count(change.added, '+', ADD, theme))
            .children(
                change
                    .removed
                    .map(|removed| kit::count(removed, '-', DEL, theme)),
            )
    }

    fn diff(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let mono = theme.word(WordToken::ShapeMono);
        kit::inner(theme)
            .pt(px(6.0))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(8.0))
                    .py(px(6.0))
                    .px(px(14.0))
                    .text_size(px(12.0))
                    .text_color(ink(theme, T3))
                    .child(div().font_family(mono.clone()).child(HUNK))
                    .child(div().flex_1())
                    .child(
                        kit::button("stage-hunk", 22.0, 11.5, theme)
                            .text_color(ink(theme, BASE))
                            .on_click(cx.listener(Self::tell(TELL_STAGE_HUNK)))
                            .child("Stage hunk"),
                    )
                    .child(
                        kit::button("discard-hunk", 22.0, 11.5, theme)
                            .text_color(rgb(DANGER))
                            .on_click(cx.listener(Self::tell(TELL_DISCARD_HUNK)))
                            .child("Discard hunk"),
                    ),
            )
            .children(LINES.iter().map(|line| {
                let (fill, sign, color) = if line.added {
                    (rgba(0x52c68e17), "+", ADD)
                } else {
                    (rgba(0xf1737d17), "-", DEL)
                };
                div()
                    .flex()
                    .h(px(CODE_LINE))
                    .bg(fill)
                    .font_family(mono.clone())
                    .text_size(px(13.0))
                    .line_height(px(CODE_LINE))
                    .child(
                        div()
                            .flex_none()
                            .w(px(GUTTER))
                            .pr(px(18.0))
                            .flex()
                            .justify_end()
                            .text_color(ink(theme, 0.24))
                            .child(line.number.to_string()),
                    )
                    .child(div().text_color(rgb(color)).child(sign))
                    .child(div().whitespace_nowrap().child(code(line.parts, theme)))
            }))
            .child(div().flex_1())
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(8.0))
                    .mt(px(10.0))
                    .mx(px(12.0))
                    .mb(px(12.0))
                    .py(px(8.0))
                    .px(px(12.0))
                    .rounded(px(9.0))
                    .bg(rgba(0xb5a1e812))
                    .text_size(px(12.5))
                    .child(div().size(px(13.0)))
                    .child(
                        div()
                            .text_color(rgb(AGENT_TEXT))
                            .child("go-dev wrote this hunk in turn 4"),
                    )
                    .child(div().flex_1())
                    .child(
                        div()
                            .font_family(mono)
                            .text_size(px(11.5))
                            .text_color(ink(theme, T3))
                            .child(HUNK_EDIT),
                    ),
            )
    }
}

fn diff_head(theme: &Theme) -> Div {
    kit::shell_head(theme)
        .child(kit::icon(Kind::Go, 14.0))
        .child(div().text_color(ink(theme, 0.85)).child("notes/notes.go"))
        .child(
            div()
                .flex_1()
                .text_color(ink(theme, T3))
                .child("working tree against HEAD"),
        )
        .child(
            div()
                .flex()
                .gap(px(2.0))
                .p(px(2.0))
                .rounded(px(7.0))
                .bg(ink(theme, 0.05))
                .child(
                    div()
                        .px(px(8.0))
                        .py(px(2.0))
                        .rounded(px(5.0))
                        .bg(ink(theme, 0.12))
                        .child("Unified"),
                )
                .child(
                    div()
                        .px(px(8.0))
                        .py(px(2.0))
                        .text_color(ink(theme, T3))
                        .child("Split"),
                ),
        )
}

fn by_author(list: Div, theme: &Theme) -> Div {
    [Author::GoDev, Author::TsDev, Author::You]
        .into_iter()
        .enumerate()
        .fold(list, |list, (place, author)| {
            let files: Vec<&Change> = CHANGES
                .iter()
                .filter(|change| change.author == author)
                .collect();
            let label = match author {
                Author::GoDev => "go-dev \u{b7} this session",
                Author::TsDev => "ts-dev \u{b7} this session",
                Author::You => "You, by hand",
            };
            let top = if place == 0 { 4.0 } else { 10.0 };
            let head = match author {
                Author::You => heading(label, files.len(), top, theme),
                Author::GoDev | Author::TsDev => kit::cap("", theme)
                    .items_center()
                    .gap(px(6.0))
                    .pt(px(top))
                    .pb(px(4.0))
                    .px(px(6.0))
                    .text_color(rgb(AGENT_TEXT))
                    .child(kit::dot(rgb(AGENT)))
                    .child(div().flex_1().child(label.to_uppercase()))
                    .child(files.len().to_string()),
            };
            list.child(head).children(files.into_iter().map(|change| {
                div()
                    .flex()
                    .items_center()
                    .gap(px(8.0))
                    .h(px(ROW))
                    .pl(px(20.0))
                    .pr(px(10.0))
                    .text_size(px(13.5))
                    .child(kit::icon(change.kind, 16.0))
                    .child(
                        div()
                            .flex_1()
                            .text_color(rgb(if change.tracked { MODIFIED } else { UNTRACKED }))
                            .child(change.name),
                    )
                    .children(change.edit.map(|edit| {
                        div()
                            .font_family(theme.word(WordToken::ShapeMono))
                            .text_size(px(11.0))
                            .text_color(ink(theme, T3))
                            .child(edit)
                    }))
            }))
        })
}

fn heading(label: &'static str, count: usize, top: f32, theme: &Theme) -> Div {
    kit::cap(label, theme)
        .pt(px(top))
        .pb(px(4.0))
        .px(px(6.0))
        .child(div().flex_1())
        .child(count.to_string())
}

pub(super) fn file_row(id: impl Into<gpui::ElementId>, left: f32) -> Stateful<Div> {
    div()
        .id(id)
        .flex()
        .flex_none()
        .items_center()
        .gap(px(8.0))
        .h(px(ROW))
        .pl(px(left))
        .pr(px(10.0))
        .rounded(px(7.0))
        .cursor_pointer()
}

fn name(file: &'static str, dir: Option<&'static str>, tone: Rgba, theme: &Theme) -> Div {
    div()
        .flex_1()
        .flex()
        .items_baseline()
        .gap(px(4.0))
        .text_color(tone)
        .child(file)
        .children(dir.map(|dir| {
            div()
                .text_size(px(12.0))
                .text_color(ink(theme, T3))
                .child(dir)
        }))
}

pub(super) fn author_dot(agent: bool) -> Div {
    if agent {
        kit::dot(rgb(AGENT))
    } else {
        kit::person(YOU, 6.0)
    }
}

fn code(parts: &[(Tone, &str)], theme: &Theme) -> StyledText {
    let mut text = String::new();
    let mut looks = Vec::new();
    for (tone, part) in parts {
        let range = text.len()..text.len() + part.len();
        text.push_str(part);
        let style = HighlightStyle {
            color: Some(rgb_to_hsla(paint(*tone, theme))),
            ..HighlightStyle::default()
        };
        looks.push((range, style));
    }
    StyledText::new(text).with_highlights(looks)
}

fn paint(tone: Tone, theme: &Theme) -> Rgba {
    match tone {
        Tone::Plain => ink(theme, BASE),
        Tone::Keyword => rgb(0xc9a7f5),
        Tone::Function => rgb(0x8fd0e8),
        Tone::Literal => rgb(0xe6d38f),
        Tone::Comment => ink(theme, 0.35),
        Tone::Type => rgb(0x9fdcc3),
    }
}
