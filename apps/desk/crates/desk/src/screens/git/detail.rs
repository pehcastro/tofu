use chrono::{DateTime, Local};
use desk_core::git::{DiffOf, FileStat};
use desk_ui::components::card::caption;
use desk_ui::components::chip::{badge, mono, tabular};
use desk_ui::components::paint::ink;
use desk_ui::components::size::{
    DIM_TEXT, FONT_BODY, FONT_SMALL, FONT_TAB, HOVER, LINE_BODY, RADIUS_ROW, T1, T2, T3,
};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{ClickEvent, Context, Div, FontWeight, SharedString, Stateful, div, prelude::*, px};

use super::changes::{letter, letter_cell};
use super::model::{GitScreen, Loaded, Panel};

const SHORT_SHA: usize = 7;
const PAD: f32 = 14.0;
const GAP: f32 = 10.0;
const ROW: f32 = 26.0;
const ROW_PAD: f32 = 8.0;
const ROW_GAP: f32 = 6.0;
const SUBJECT: f32 = 15.0;

pub(super) fn short(sha: &str) -> &str {
    sha.get(..SHORT_SHA).unwrap_or(sha)
}

pub(super) fn render(
    screen: &GitScreen,
    sha: &str,
    theme: &Theme,
    cx: &mut Context<GitScreen>,
) -> Div {
    let quiet = |said: String| {
        div()
            .flex_1()
            .p(px(PAD))
            .text_size(px(FONT_SMALL))
            .text_color(ink(theme, DIM_TEXT))
            .child(said)
    };
    let detail = match screen.detail() {
        Some(Loaded::Ready(detail)) => detail,
        Some(Loaded::Failed(error)) => {
            return quiet(format!("Commit {} could not be read: {error}", short(sha)));
        }
        None | Some(Loaded::Waiting) => {
            return quiet(format!("Reading commit {}\u{2026}", short(sha)));
        }
    };
    let commit = &detail.commit;
    let when = DateTime::from_timestamp(commit.when, 0).map_or_else(
        || format!("{} seconds after 1970", commit.when),
        |at| {
            at.with_timezone(&Local)
                .format("%-d %b %Y, %H:%M")
                .to_string()
        },
    );
    let body = detail.body.trim();
    let files: Vec<Stateful<Div>> = detail
        .files
        .iter()
        .map(|file| file_row(sha, file, theme, cx))
        .collect();
    div().flex_1().min_h_0().child(
        div()
            .id("git-commit")
            .size_full()
            .overflow_y_scroll()
            .flex()
            .flex_col()
            .gap(px(GAP))
            .px(px(PAD))
            .pb(px(PAD))
            .child(
                div()
                    .text_size(px(SUBJECT))
                    .font_weight(FontWeight::SEMIBOLD)
                    .text_color(ink(theme, T1))
                    .child(commit.subject.clone()),
            )
            .child(
                div()
                    .flex()
                    .flex_col()
                    .text_size(px(FONT_SMALL))
                    .text_color(ink(theme, T2))
                    .child(format!("{} <{}>", commit.author, commit.email))
                    .child(
                        div()
                            .flex()
                            .gap(px(ROW_GAP))
                            .text_color(ink(theme, T3))
                            .child(when)
                            .child(div().font_family(mono(theme)).child(short(sha).to_owned())),
                    ),
            )
            .when(!body.is_empty(), |page| {
                page.child(
                    div()
                        .text_size(px(FONT_BODY))
                        .line_height(px(LINE_BODY))
                        .text_color(ink(theme, T2))
                        .child(body.to_owned()),
                )
            })
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(ROW_GAP))
                    .child(caption("Files", theme))
                    .child(badge(detail.files.len().to_string(), theme)),
            )
            .child(div().flex().flex_col().children(files)),
    )
}

fn file_row(
    sha: &str,
    file: &FileStat,
    theme: &Theme,
    cx: &mut Context<GitScreen>,
) -> Stateful<Div> {
    let (dir, name) = file.path.rsplit_once('/').unwrap_or(("", &file.path));
    let whence = match &file.from {
        Some(from) => format!("from {from}"),
        None => dir.to_owned(),
    };
    let hover = ink(theme, HOVER);
    let open = Panel::Diff {
        path: file.path.clone(),
        of: DiffOf::Commit(sha.to_owned()),
        back: Box::new(Panel::Commit(sha.to_owned())),
    };
    div()
        .id(SharedString::from(format!("git-commit-file-{}", file.path)))
        .flex()
        .items_center()
        .gap(px(ROW_GAP))
        .h(px(ROW))
        .px(px(ROW_PAD))
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .text_size(px(FONT_TAB))
        .hover(move |style| style.bg(hover))
        .on_click(cx.listener(move |screen, _: &ClickEvent, _, cx| screen.show(open.clone(), cx)))
        .child(letter_cell(letter(file.mark), theme))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .flex()
                .items_baseline()
                .gap(px(ROW_GAP))
                .child(
                    div()
                        .flex_none()
                        .text_color(ink(theme, T1))
                        .child(name.to_owned()),
                )
                .child(
                    div()
                        .min_w_0()
                        .truncate()
                        .text_size(px(FONT_SMALL))
                        .text_color(ink(theme, T3))
                        .child(whence),
                ),
        )
        .child(
            div()
                .flex()
                .flex_none()
                .gap(px(ROW_GAP))
                .font_family(mono(theme))
                .font_features(tabular())
                .text_size(px(FONT_SMALL))
                .child(
                    div()
                        .text_color(theme.color(ColorToken::GitAdded))
                        .child(format!("+{}", file.added)),
                )
                .child(
                    div()
                        .text_color(theme.color(ColorToken::GitDeleted))
                        .child(format!("-{}", file.removed)),
                ),
        )
}
