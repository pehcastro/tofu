use gpui::{
    AnyElement, ClickEvent, Context, Div, Entity, FontWeight, Rgba, SharedString, Window, div,
    prelude::*, px, rgb,
};

use crate::component::icon;
use crate::components::button::{ButtonKind, button};
use crate::components::card::caption;
use crate::components::chip::{GitStatus, git_name, mono, tabular};
use crate::components::glyph::Glyph;
use crate::components::overlay::MenuButton;
use crate::components::paint::{glyph, ink, tint};
use crate::components::size::{
    CAPTION_TEXT, FONT_BODY, FONT_SMALL, FONT_TREE, HOVER, NUMBER_TEXT, RADIUS_BADGE, RADIUS_ROW,
    ROW_ON, ROW_PAD_X, ROW_PAD_Y, T1, T3,
};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{AVATAR, ICON, ICON_SMALL};
use crate::theme::{ColorToken, Theme};

const PERSON_FILL: u32 = 0x56_63_75;
const PERSON_TEXT: u32 = 0xe6_ea_f0;
const CODE_LINE: f32 = 22.0;
const NUMBER_WIDTH: f32 = 52.0;
const NUMBER_PAD: f32 = 18.0;
const BLAME_WIDTH: f32 = 168.0;
const BLAME_FACE: f32 = 14.0;
const AGENT_BAR: f32 = 3.0;
const DOT: f32 = 6.0;
const CHECKBOX: f32 = 14.0;
const HELD_TINT: f32 = 0.12;
const AGENT_TINT: f32 = 0.08;
const FACE_TEXT_SHARE: f32 = 0.5;

pub struct Commit {
    pub subject: SharedString,
    pub author: SharedString,
    pub age: SharedString,
    pub agent: Option<SharedString>,
}

pub struct Day {
    pub title: SharedString,
    pub commits: Vec<Commit>,
}

pub enum Blame {
    Person {
        name: SharedString,
        commit: SharedString,
    },
    Agent {
        name: SharedString,
        turn: u32,
    },
}

pub struct BlameLine {
    pub number: u32,
    pub text: SharedString,
    pub blame: Blame,
}

pub struct ChangedFile {
    pub path: SharedString,
    pub git: GitStatus,
    pub staged: bool,
    pub agent: bool,
}

fn face(name: &str, side: f32) -> Div {
    div()
        .flex_none()
        .size(px(side))
        .flex()
        .items_center()
        .justify_center()
        .rounded_full()
        .bg(rgb(PERSON_FILL))
        .text_color(rgb(PERSON_TEXT))
        .text_size(px(side * FACE_TEXT_SHARE))
        .font_weight(FontWeight::SEMIBOLD)
        .child(
            name.chars()
                .next()
                .unwrap_or('?')
                .to_uppercase()
                .to_string(),
        )
}

fn dot(color: Rgba) -> Div {
    div().flex_none().size(px(DOT)).rounded_full().bg(color)
}

pub struct History {
    days: Vec<Day>,
    selected: (usize, usize),
    branch: SharedString,
    branches: Vec<SharedString>,
    writer: Option<SharedString>,
    held: Option<SharedString>,
    queued: bool,
    picker: Entity<MenuButton>,
}

impl History {
    pub fn new(
        days: Vec<Day>,
        branches: Vec<SharedString>,
        writer: Option<SharedString>,
        cx: &mut Context<Self>,
    ) -> Self {
        let picker = MenuButton::new("Branch".into(), branches.clone(), cx);
        let history = cx.entity().downgrade();
        picker.update(cx, |picker, _| {
            picker.on_pick(move |at, _, cx| {
                if let Some(history) = history.upgrade() {
                    history.update(cx, |this, cx| this.pick(*at, cx));
                }
            });
        });
        History {
            days,
            selected: (0, 0),
            branch: branches.first().cloned().unwrap_or_default(),
            branches,
            writer,
            held: None,
            queued: false,
            picker,
        }
    }

    pub fn hold(&mut self, branch: impl Into<SharedString>) {
        self.held = Some(branch.into());
    }

    fn pick(&mut self, at: usize, cx: &mut Context<Self>) {
        let Some(branch) = self.branches.get(at).cloned() else {
            return;
        };
        self.queued = false;
        match &self.writer {
            Some(_) if branch != self.branch => self.held = Some(branch),
            _ => {
                self.branch = branch;
                self.held = None;
            }
        }
        cx.notify();
    }

    fn held_strip(&self, theme: &Theme, cx: &mut Context<Self>) -> Option<Div> {
        let branch = self.held.clone()?;
        let writer = self.writer.clone().unwrap_or_default();
        let warn = theme.color(ColorToken::StatusWarn);
        let note: SharedString = if self.queued {
            format!("Switches to {branch} when the turn ends").into()
        } else {
            format!("{branch} is held: three changed files would be overwritten and {writer} is writing web/ right now").into()
        };
        Some(
            div()
                .flex()
                .items_center()
                .gap_2()
                .min_w_0()
                .px(px(ROW_PAD_X))
                .py(px(ROW_PAD_Y))
                .rounded(px(RADIUS_ROW))
                .bg(tint(warn, HELD_TINT))
                .text_size(px(FONT_SMALL))
                .child(div().flex_1().min_w_0().truncate().child(note))
                .when(!self.queued, |strip| {
                    strip.child(
                        button(
                            "history-stash",
                            "Stash and switch after the turn",
                            None,
                            ButtonKind::Plain,
                            theme,
                        )
                        .on_click(cx.listener(
                            |this, _: &ClickEvent, _, cx| {
                                this.queued = true;
                                cx.notify();
                            },
                        )),
                    )
                }),
        )
    }

    fn commit_row(
        &self,
        day: usize,
        at: usize,
        commit: &Commit,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let picked = self.selected == (day, at);
        let hover = ink(theme, HOVER);
        div()
            .id(SharedString::from(format!("commit-{day}-{at}")))
            .flex()
            .items_center()
            .gap(px(ROW_PAD_X))
            .min_w_0()
            .px(px(ROW_PAD_X))
            .py(px(ROW_PAD_Y))
            .rounded(px(RADIUS_ROW))
            .cursor_pointer()
            .when(picked, |row| row.bg(ink(theme, ROW_ON)))
            .when(!picked, |row| row.hover(move |style| style.bg(hover)))
            .child(face(&commit.author, AVATAR))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .flex_1()
                    .min_w_0()
                    .child(
                        div()
                            .truncate()
                            .text_size(px(FONT_TREE))
                            .text_color(ink(theme, T1))
                            .child(commit.subject.clone()),
                    )
                    .child(
                        div()
                            .flex()
                            .min_w_0()
                            .gap_1()
                            .text_size(px(FONT_SMALL))
                            .text_color(ink(theme, CAPTION_TEXT))
                            .child(
                                div()
                                    .flex_none()
                                    .child(format!("{} \u{b7} {}", commit.author, commit.age)),
                            )
                            .children(commit.agent.clone().map(|agent| {
                                div()
                                    .min_w_0()
                                    .truncate()
                                    .text_color(theme.color(ColorToken::Trace))
                                    .child(format!("\u{b7} with {agent}"))
                            })),
                    ),
            )
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                this.selected = (day, at);
                cx.notify();
            }))
            .into_any_element()
    }
}

impl Render for History {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let header = div()
            .flex()
            .items_center()
            .gap_2()
            .min_w_0()
            .px(px(ROW_PAD_X))
            .child(icon(Icon::Branch, ICON, ink(&theme, CAPTION_TEXT)))
            .child(
                div()
                    .flex_none()
                    .font_weight(FontWeight::MEDIUM)
                    .child("History"),
            )
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .truncate()
                    .font_family(mono(&theme))
                    .text_size(px(FONT_SMALL))
                    .text_color(ink(&theme, CAPTION_TEXT))
                    .child(self.branch.clone()),
            )
            .child(self.picker.clone());
        let mut list = div().flex().flex_col().gap_0p5().min_w_0();
        for (day, group) in self.days.iter().enumerate() {
            list = list.child(
                div()
                    .px(px(ROW_PAD_X))
                    .pt_2()
                    .pb_1()
                    .child(caption(group.title.clone(), &theme)),
            );
            for (at, commit) in group.commits.iter().enumerate() {
                list = list.child(self.commit_row(day, at, commit, &theme, cx));
            }
        }
        div()
            .flex()
            .flex_col()
            .gap_2()
            .w_full()
            .min_w_0()
            .text_size(px(FONT_BODY))
            .child(header)
            .children(self.held_strip(&theme, cx))
            .child(list)
    }
}

pub fn blame_gutter(lines: &[BlameLine], theme: &Theme) -> Div {
    let violet = theme.color(ColorToken::Trace);
    let grey = ink(theme, CAPTION_TEXT);
    let mut previous: Option<&Blame> = None;
    let mut gutter = div()
        .flex()
        .flex_col()
        .w_full()
        .min_w_0()
        .font_family(mono(theme))
        .text_size(px(FONT_BODY))
        .font_features(tabular());
    for line in lines {
        let fresh = previous.is_none_or(|known| !same_blame(known, &line.blame));
        previous = Some(&line.blame);
        let (bar, label) = match &line.blame {
            Blame::Person { name, commit } => (
                None,
                div()
                    .flex()
                    .items_center()
                    .gap_1p5()
                    .min_w_0()
                    .text_color(grey)
                    .child(face(name, BLAME_FACE))
                    .child(
                        div()
                            .min_w_0()
                            .truncate()
                            .child(format!("{name} \u{b7} {commit}")),
                    ),
            ),
            Blame::Agent { name, turn } => (
                Some(violet),
                div()
                    .flex()
                    .items_center()
                    .gap_1p5()
                    .min_w_0()
                    .text_color(violet)
                    .child(glyph(Glyph::Trace, ICON_SMALL, violet))
                    .child(
                        div()
                            .min_w_0()
                            .truncate()
                            .child(format!("{name} \u{b7} turn {turn}")),
                    ),
            ),
        };
        gutter = gutter.child(
            div()
                .flex()
                .items_center()
                .h(px(CODE_LINE))
                .min_w_0()
                .when_some(bar, |row, violet| row.bg(tint(violet, AGENT_TINT)))
                .child(
                    div()
                        .flex_none()
                        .w(px(AGENT_BAR))
                        .h_full()
                        .when_some(bar, |mark, violet| mark.bg(violet)),
                )
                .child(
                    div()
                        .flex_none()
                        .w(px(BLAME_WIDTH))
                        .pl_2()
                        .text_size(px(FONT_SMALL))
                        .when(fresh, |cell| cell.child(label)),
                )
                .child(
                    div()
                        .flex_none()
                        .w(px(NUMBER_WIDTH))
                        .pr(px(NUMBER_PAD))
                        .text_right()
                        .text_color(ink(theme, NUMBER_TEXT))
                        .child(line.number.to_string()),
                )
                .child(
                    div()
                        .flex_1()
                        .min_w_0()
                        .truncate()
                        .text_color(ink(theme, T1))
                        .child(line.text.clone()),
                ),
        );
    }
    gutter
}

fn same_blame(a: &Blame, b: &Blame) -> bool {
    match (a, b) {
        (Blame::Person { commit: x, .. }, Blame::Person { commit: y, .. }) => x == y,
        (Blame::Agent { name: x, turn: s }, Blame::Agent { name: y, turn: t }) => x == y && s == t,
        _ => false,
    }
}

pub struct Changes {
    files: Vec<ChangedFile>,
}

impl Changes {
    pub fn new(files: Vec<ChangedFile>) -> Self {
        Changes { files }
    }

    fn file_row(
        &self,
        at: usize,
        file: &ChangedFile,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let hover = ink(theme, HOVER);
        let accent = theme.color(ColorToken::StatusAccent);
        let check = div()
            .flex_none()
            .size(px(CHECKBOX))
            .flex()
            .items_center()
            .justify_center()
            .rounded(px(RADIUS_BADGE))
            .when(file.staged, |check| {
                check.bg(accent).child(glyph(
                    Glyph::Check,
                    CHECKBOX - 4.0,
                    theme.color(ColorToken::TextStrong),
                ))
            })
            .when(!file.staged, |check| {
                check.border_1().border_color(ink(theme, T3))
            });
        let who = if file.agent {
            theme.color(ColorToken::Trace)
        } else {
            ink(theme, T3)
        };
        div()
            .id(("change", at))
            .flex()
            .items_center()
            .gap(px(ROW_PAD_X))
            .min_w_0()
            .px(px(ROW_PAD_X))
            .py(px(ROW_PAD_Y))
            .rounded(px(RADIUS_ROW))
            .cursor_pointer()
            .hover(move |style| style.bg(hover))
            .child(check)
            .child(glyph(Glyph::File, ICON_SMALL, ink(theme, CAPTION_TEXT)))
            .child(git_name(file.path.clone(), Some(file.git), theme).flex_1())
            .child(dot(who))
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                if let Some(file) = this.files.get_mut(at) {
                    file.staged = !file.staged;
                    cx.notify();
                }
            }))
            .into_any_element()
    }
}

impl Render for Changes {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let mut list = div()
            .flex()
            .flex_col()
            .gap_0p5()
            .w_full()
            .min_w_0()
            .text_size(px(FONT_BODY));
        for (title, staged) in [("Staged", true), ("Changes", false)] {
            let count = self
                .files
                .iter()
                .filter(|file| file.staged == staged)
                .count();
            list = list.child(
                div()
                    .px(px(ROW_PAD_X))
                    .pt_2()
                    .pb_1()
                    .child(caption(format!("{title}  {count}"), &theme)),
            );
            for (at, file) in self.files.iter().enumerate() {
                if file.staged == staged {
                    list = list.child(self.file_row(at, file, &theme, cx));
                }
            }
        }
        list
    }
}
