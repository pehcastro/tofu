use desk_ui::components::card::{Header, inner_card, shell};
use desk_ui::components::chip::badge;
use desk_ui::components::empty::empty_state;
use desk_ui::components::file_edits::{ChangeAction, changed_files, commit_box};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::paint::ink;
use desk_ui::theme::{Theme, WordToken};
use gpui::{
    ClickEvent, Context, Div, HighlightStyle, Rgba, StyledText, Window, div, prelude::*, px, rgb,
    rgb_to_hsla, rgba,
};

use super::fixture::{HUNK, HUNK_EDIT, Kind, LINES, Tone};
use super::fixture::{TELL_DISCARD_HUNK, TELL_STAGE_HUNK};
use super::kit::{self, ADD, AGENT_TEXT, BASE, DANGER, DEL, T3};
use super::{Changes, Git};

const SIDE: f32 = 440.0;
const CODE_LINE: f32 = 22.0;
const GUTTER: f32 = 70.0;
const FIXTURE_BRANCH: &str = "main \u{2191}2";

impl Git {
    pub(super) fn changes(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let branch = match &self.changes {
            Changes::Refused(error) => {
                return div().child(empty_state(
                    "git-refused",
                    "Not a git repository",
                    Some(error.clone()),
                    &[],
                    &[],
                    theme,
                    |_, _, _| {},
                ));
            }
            Changes::Fixture(_) => FIXTURE_BRANCH.into(),
            Changes::Repo(repo) => repo.branch.clone(),
        };
        let files = self.files();
        let staged = files.iter().filter(|file| file.staged).count();
        let act = cx.listener(|this, (ix, action): &(usize, ChangeAction), _, cx| {
            this.act(*ix, *action, cx)
        });
        let side = shell(
            Header::Title(
                Some(Glyph::File),
                "Source control".into(),
                Some(badge(branch, theme).into_any_element()),
            ),
            theme,
        )
        .flex_none()
        .w(px(SIDE))
        .child(
            inner_card(theme)
                .child(commit_box(
                    self.message.clone(),
                    staged,
                    theme,
                    cx.listener(|this, _: &ClickEvent, _, cx| this.commit(cx)),
                ))
                .child(changed_files(
                    "changes",
                    &files,
                    theme,
                    move |ix, action, window: &mut Window, cx| act(&(ix, action), window, cx),
                )),
        );
        div()
            .child(side)
            .children(matches!(self.changes, Changes::Fixture(_)).then(|| {
                kit::shell()
                    .flex_1()
                    .child(diff_head(theme))
                    .child(self.diff(theme, cx))
            }))
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
