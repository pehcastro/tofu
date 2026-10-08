use desk_ui::components::paint::ink;
use desk_ui::theme::{Theme, WordToken};
use gpui::{
    BoxShadow, ClickEvent, Context, Div, ElementId, FontWeight, Stateful, div, point, prelude::*,
    px, rgb, rgba,
};

use super::Git;
use super::fixture::{
    Avatar, BLOCKED, BRANCHES, COMMITS, CREATE_TELL, Day, HISTORY_NOTE, TELL_MENTION, TELL_STASH,
    TOUCHED, TYPED,
};
use super::kit::{self, ADD, AGENT, ANA, BASE, DEL, T2, T3, WARN, YOU, edge};

const ROW: f32 = 28.0;

fn file_row(id: impl Into<ElementId>, left: f32) -> Stateful<Div> {
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

fn author_dot(agent: bool) -> Div {
    if agent {
        kit::dot(rgb(AGENT))
    } else {
        kit::person(YOU, 6.0)
    }
}

impl Git {
    pub(super) fn history(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        div()
            .child(
                kit::side(kit::shell())
                    .child(
                        kit::shell_head(theme)
                            .child(kit::cap("History", theme).pl(px(19.0)))
                            .child(div().flex_1())
                            .child(
                                kit::button("branch", 24.0, 12.0, theme)
                                    .px(px(9.0))
                                    .rounded(px(7.0))
                                    .font_family(theme.word(WordToken::ShapeMono))
                                    .text_color(ink(theme, 0.85))
                                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                                        this.branches = !this.branches;
                                        cx.notify();
                                    }))
                                    .child("main")
                                    .child(div().text_color(ink(theme, T3)).child("\u{2304}")),
                            ),
                    )
                    .child(self.commits(theme, cx))
                    .children(self.branches.then(|| self.branch_menu(theme, cx))),
            )
            .child(kit::shell().flex_1().child(self.detail(theme, cx)))
    }

    fn commits(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let mono = theme.word(WordToken::ShapeMono);
        let mut list = kit::inner(theme).p(px(8.0));
        for (index, commit) in COMMITS.iter().enumerate() {
            if index == 0 || COMMITS[index - 1].day != commit.day {
                let (label, top) = match commit.day {
                    Day::Today => ("Today", 6.0),
                    Day::Yesterday => ("Yesterday", 10.0),
                };
                list = list.child(kit::cap(label, theme).px(px(8.0)).pt(px(top)).pb(px(6.0)));
            }
            let colors = match commit.avatar {
                Avatar::Pehcastro => YOU,
                Avatar::Ana => ANA,
            };
            let line = |text: String| div().child(text);
            let meta = div()
                .flex()
                .text_size(px(12.0))
                .text_color(ink(theme, T3))
                .child(line(format!(
                    "{} \u{b7} {} \u{b7} ",
                    commit.author, commit.age
                )))
                .child(div().font_family(mono.clone()).child(commit.hash))
                .children(commit.with.map(|with| line(format!(" \u{b7} {with}"))));
            list = list.child(
                div()
                    .id(("commit", index))
                    .flex()
                    .items_start()
                    .gap(px(12.0))
                    .py(px(7.0))
                    .px(px(10.0))
                    .rounded(px(8.0))
                    .cursor_pointer()
                    .line_height(px(18.0))
                    .when(commit.tell.is_none() && self.commit == index, |row| {
                        row.bg(ink(theme, 0.08))
                    })
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        match commit.tell {
                            Some(message) => this.told = Some(message.into()),
                            None => this.commit = index,
                        }
                        cx.notify();
                    }))
                    .child(kit::person(colors, 22.0).mt(px(1.0)))
                    .child(
                        div()
                            .flex_1()
                            .flex()
                            .flex_col()
                            .pt(px(1.0))
                            .child(
                                div()
                                    .text_size(px(13.5))
                                    .when(commit.tell.is_some(), |text| {
                                        text.text_color(ink(theme, T2))
                                    })
                                    .child(commit.summary),
                            )
                            .child(meta),
                    ),
            );
        }
        list.child(div().flex_1()).child(
            div()
                .px(px(8.0))
                .py(px(6.0))
                .text_size(px(12.0))
                .line_height(px(17.0))
                .text_color(ink(theme, T3))
                .child(HISTORY_NOTE),
        )
    }

    fn branch_menu(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let mono = theme.word(WordToken::ShapeMono);
        let item = |id: ElementId| {
            div()
                .id(id)
                .flex()
                .items_center()
                .gap(px(10.0))
                .py(px(7.0))
                .px(px(10.0))
                .rounded(px(8.0))
                .cursor_pointer()
        };
        div()
            .absolute()
            .right(px(8.0))
            .top(px(40.0))
            .w(px(300.0))
            .p(px(6.0))
            .rounded(px(12.0))
            .bg(rgba(0x1e1d24f0))
            .text_size(px(13.0))
            .shadow(vec![
                edge(ink(theme, 0.12), 1.0),
                BoxShadow {
                    color: rgba(0x00000099).into(),
                    offset: point(px(0.0), px(22.0)),
                    blur_radius: px(50.0),
                    spread_radius: px(0.0),
                    inset: false,
                },
            ])
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(8.0))
                    .pt(px(6.0))
                    .px(px(10.0))
                    .pb(px(8.0))
                    .border_b_1()
                    .border_color(ink(theme, 0.07))
                    .child(div().font_family(mono.clone()).child(TYPED))
                    .child(div().w(px(1.5)).h(px(14.0)).bg(ink(theme, 1.0))),
            )
            .child(
                item("create".into())
                    .mt(px(4.0))
                    .on_click(cx.listener(Self::tell(CREATE_TELL)))
                    .child(div().text_color(ink(theme, T2)).child("+"))
                    .child(
                        div()
                            .flex_1()
                            .flex()
                            .child("Create ")
                            .child(div().font_family(mono.clone()).child(TYPED))
                            .child(" from main"),
                    ),
            )
            .children(BRANCHES.iter().enumerate().map(|(index, branch)| {
                item(("branch", index).into())
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        if branch.switches {
                            this.branches = false;
                            this.blocked = true;
                        } else {
                            this.told = Some(branch.tell.into());
                        }
                        cx.notify();
                    }))
                    .child(
                        div()
                            .flex_1()
                            .font_family(mono.clone())
                            .when(!branch.switches, |name| name.text_color(ink(theme, T2)))
                            .child(branch.name),
                    )
                    .child(
                        div()
                            .text_size(px(12.0))
                            .text_color(ink(theme, T3))
                            .child(branch.age),
                    )
            }))
    }

    fn detail(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let commit = &COMMITS[self.commit];
        let head = kit::shell_head(theme)
            .child(
                div()
                    .font_family(theme.word(WordToken::ShapeMono))
                    .text_color(ink(theme, 0.8))
                    .child(commit.hash),
            )
            .child(div().flex_1().text_color(ink(theme, T3)).child(commit.when))
            .child(
                kit::button("mention", 24.0, 12.0, theme)
                    .text_color(ink(theme, BASE))
                    .on_click(cx.listener(Self::tell(TELL_MENTION)))
                    .child("Mention in chat"),
            );
        let body =
            kit::inner(theme)
                .py(px(16.0))
                .px(px(18.0))
                .gap(px(14.0))
                .child(
                    div()
                        .text_size(px(17.0))
                        .line_height(px(22.0))
                        .letter_spacing(px(-0.2))
                        .font_weight(FontWeight::SEMIBOLD)
                        .child(commit.title),
                )
                .child(
                    div()
                        .text_size(px(13.5))
                        .line_height(px(21.0))
                        .letter_spacing(px(-0.25))
                        .text_color(ink(theme, T2))
                        .child(commit.body),
                )
                .child(kit::cap("Files", theme))
                .child(div().flex().flex_col().gap(px(2.0)).children(
                    TOUCHED.iter().enumerate().map(|(index, file)| {
                        file_row(("file", index), 6.0)
                            .w(px(652.0))
                            .text_size(px(13.5))
                            .child(kit::icon(file.kind, 16.0))
                            .child(div().flex_1().child(file.path))
                            .child(author_dot(file.agent))
                            .child(kit::count(file.added, '+', ADD, theme))
                            .children(
                                file.removed
                                    .map(|removed| kit::count(removed, '-', DEL, theme)),
                            )
                    }),
                ))
                .child(div().flex_1())
                .children(self.blocked.then(|| {
                    div()
                        .flex()
                        .items_center()
                        .gap(px(10.0))
                        .py(px(10.0))
                        .px(px(14.0))
                        .rounded(px(11.0))
                        .bg(rgba(0xe8c98a12))
                        .shadow(vec![edge(rgba(0xe8c98a38), 1.0)])
                        .text_size(px(13.0))
                        .line_height(px(23.0))
                        .child(
                            div()
                                .flex_1()
                                .min_w_0()
                                .text_color(rgb(WARN))
                                .child(BLOCKED),
                        )
                        .child(
                            kit::button("stash", 26.0, 13.0, theme)
                                .on_click(cx.listener(Self::tell(TELL_STASH)))
                                .child("Stash and switch after the turn"),
                        )
                        .child(
                            kit::button("cancel", 26.0, 13.0, theme)
                                .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                                    this.blocked = false;
                                    cx.notify();
                                }))
                                .child("Cancel"),
                        )
                }));
        div()
            .flex()
            .flex_col()
            .flex_1()
            .min_h_0()
            .child(head)
            .child(body)
    }
}
