use desk_core::git::{Conflict, Operation, Side, State};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::paint::{ink, tint};
use desk_ui::components::size::{FONT_BODY, FONT_SMALL, T1, T2};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, ClickEvent, Context, Div, FontWeight, SharedString, Window, div, prelude::*, px,
};

use super::model::{Action, GitScreen, Told};
use super::sync::{plural, short};

const DISABLED: f32 = 0.45;
const ROWS_MAX: f32 = 240.0;
const WASH: f32 = 0.1;

struct Sides {
    ours: String,
    theirs: String,
}

pub fn render(
    screen: &GitScreen,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> Option<AnyElement> {
    let theme = ActiveTheme::theme(cx);
    let failed = match screen.told() {
        Some(Told::Failed { said, fix }) => Some(failure(said, fix.as_ref(), &theme, cx)),
        Some(Told::Done(_)) | None => None,
    };
    let operation = screen
        .snapshot()
        .operation
        .as_ref()
        .map(|operation| banner(screen, operation, &theme, window, cx));
    match (operation, failed) {
        (None, None) => None,
        (operation, failed) => Some(
            div()
                .flex()
                .flex_col()
                .flex_none()
                .children(operation)
                .children(failed)
                .into_any_element(),
        ),
    }
}

fn banner(
    screen: &GitScreen,
    operation: &Operation,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> Div {
    let snapshot = screen.snapshot();
    let here = snapshot
        .branch
        .as_ref()
        .map_or("HEAD".to_owned(), |branch| branch.name.clone());
    let conflicts: Vec<(&str, Conflict)> = snapshot
        .changes
        .iter()
        .filter_map(|entry| match entry.state {
            State::Conflicted(kind) => Some((entry.path.as_str(), kind)),
            State::Tracked { .. } | State::Renamed { .. } | State::Untracked | State::Ignored => {
                None
            }
        })
        .collect();
    let left = match conflicts.len() {
        0 => "no conflicts left, press Continue to finish".to_owned(),
        count => format!("{} in conflict", plural(count, "file")),
    };
    let (doing, sides, finish, undo) = match operation {
        Operation::Merge { theirs } => {
            let theirs = if theirs.len() == 40 && theirs.bytes().all(|b| b.is_ascii_hexdigit()) {
                short(theirs)
            } else {
                theirs.as_str()
            };
            (
                format!("Merging {theirs} into {here}: {left}"),
                Sides {
                    ours: here.clone(),
                    theirs: theirs.to_owned(),
                },
                format!("Continue: finish the merge with a merge commit on {here}"),
                format!("Abort: stop merging and put {here} back as it was before"),
            )
        }
        Operation::Rebase { onto, step, of } => (
            format!("Rebasing onto {onto}, step {step} of {of}: {left}"),
            Sides {
                ours: onto.clone(),
                theirs: "your commit".to_owned(),
            },
            format!("Continue: commit this step and replay the next one onto {onto}"),
            "Abort: stop rebasing and put your branch back as it was before".to_owned(),
        ),
        Operation::CherryPick { sha } => (
            format!("Cherry-picking {} onto {here}: {left}", short(sha)),
            Sides {
                ours: here.clone(),
                theirs: format!("commit {}", short(sha)),
            },
            format!("Continue: commit the copy of {} on {here}", short(sha)),
            format!("Abort: stop the cherry-pick and put {here} back as it was before"),
        ),
        Operation::Revert { sha } => (
            format!("Reverting {} on {here}: {left}", short(sha)),
            Sides {
                ours: here.clone(),
                theirs: format!("the revert of {}", short(sha)),
            },
            format!("Continue: commit the revert of {} on {here}", short(sha)),
            format!("Abort: stop the revert and put {here} back as it was before"),
        ),
    };
    let warn = theme.color(ColorToken::StatusWarn);
    let danger = theme.color(ColorToken::StatusDanger);
    let ready = conflicts.is_empty();
    let go_on = button("git-continue", "Continue", None, ButtonKind::Primary, theme);
    let go_on = if ready {
        go_on
            .on_click(cx.listener(|screen, _: &ClickEvent, _, cx| screen.run(Action::Continue, cx)))
    } else {
        go_on.opacity(DISABLED).cursor_default()
    };
    let go_on_tip = if ready {
        finish
    } else {
        format!(
            "Resolve the {} in conflict first",
            plural(conflicts.len(), "file")
        )
    };
    let abort = button("git-abort", "Abort", None, ButtonKind::Plain, theme)
        .bg(tint(danger, 0.16))
        .text_color(danger)
        .on_click(cx.listener(|screen, _: &ClickEvent, _, cx| screen.run(Action::Abort, cx)));
    let head = div()
        .flex()
        .items_center()
        .gap_2()
        .px_3()
        .py_2()
        .child(
            div()
                .flex_1()
                .min_w_0()
                .truncate()
                .text_size(px(FONT_BODY))
                .font_weight(FontWeight::MEDIUM)
                .text_color(ink(theme, T1))
                .child(doing),
        )
        .child(tooltip(
            "git-continue-tip",
            go_on,
            Edge::Frame,
            go_on_tip,
            theme,
            window,
            cx,
        ))
        .child(tooltip(
            "git-abort-tip",
            abort,
            Edge::Frame,
            undo,
            theme,
            window,
            cx,
        ));
    let rows: Vec<Div> = conflicts
        .iter()
        .enumerate()
        .map(|(ix, (path, kind))| conflict_row(ix, path, *kind, &sides, theme, cx))
        .collect();
    div()
        .flex()
        .flex_col()
        .bg(tint(warn, WASH))
        .border_b_1()
        .border_color(tint(warn, 0.3))
        .child(head)
        .when(!rows.is_empty(), |banner| {
            banner.child(
                div()
                    .id("git-conflicts")
                    .flex()
                    .flex_col()
                    .max_h(px(ROWS_MAX))
                    .overflow_y_scroll()
                    .px_3()
                    .pb_2()
                    .children(rows),
            )
        })
}

fn kind_words(kind: Conflict, sides: &Sides) -> String {
    let Sides { ours, theirs } = sides;
    match kind {
        Conflict::BothModified => "both changed".to_owned(),
        Conflict::BothAdded => "both added".to_owned(),
        Conflict::BothDeleted => "both deleted".to_owned(),
        Conflict::AddedByUs => format!("added by {ours}"),
        Conflict::AddedByThem => format!("added by {theirs}"),
        Conflict::DeletedByUs => format!("deleted by {ours}, changed by {theirs}"),
        Conflict::DeletedByThem => format!("deleted by {theirs}, changed by {ours}"),
    }
}

fn conflict_row(
    ix: usize,
    path: &str,
    kind: Conflict,
    sides: &Sides,
    theme: &Theme,
    cx: &Context<GitScreen>,
) -> Div {
    let take = |id: &'static str, label: String, side: Side| {
        let path = path.to_owned();
        button((id, ix), label, None, ButtonKind::Text, theme).on_click(cx.listener(
            move |screen, _: &ClickEvent, _, cx| {
                screen.run(
                    Action::Take {
                        path: path.clone(),
                        side,
                    },
                    cx,
                )
            },
        ))
    };
    let resolved = {
        let path = path.to_owned();
        button(
            ("git-resolve", ix),
            "Mark resolved",
            None,
            ButtonKind::Text,
            theme,
        )
        .on_click(cx.listener(move |screen, _: &ClickEvent, _, cx| {
            screen.run(Action::Resolve(path.clone()), cx)
        }))
    };
    div()
        .flex()
        .items_center()
        .gap_2()
        .py_1()
        .text_size(px(FONT_SMALL))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .flex()
                .gap_2()
                .child(
                    div()
                        .truncate()
                        .text_color(ink(theme, T1))
                        .child(path.to_owned()),
                )
                .child(
                    div()
                        .flex_none()
                        .text_color(ink(theme, T2))
                        .child(kind_words(kind, sides)),
                ),
        )
        .child(take(
            "git-keep-ours",
            format!("Keep {}'s", sides.ours),
            Side::Ours,
        ))
        .child(take(
            "git-keep-theirs",
            format!("Keep {}'s", sides.theirs),
            Side::Theirs,
        ))
        .child(resolved)
}

fn fix_label(fix: &Action) -> &'static str {
    match fix {
        Action::Stage(_) => "Stage",
        Action::Unstage(_) => "Unstage",
        Action::Discard(_) => "Discard",
        Action::StagePatch(_) => "Stage hunk",
        Action::DiscardPatch(_) => "Discard hunk",
        Action::Commit { .. } => "Commit",
        Action::Switch(_) => "Switch",
        Action::CreateBranch { .. } => "Create branch",
        Action::Fetch => "Fetch",
        Action::Pull(_) => "Pull",
        Action::Push { lease: false } => "Push",
        Action::Push { lease: true } => "Force push",
        Action::StashPush(_) => "Stash",
        Action::StashPop(_) => "Pop stash",
        Action::StashDrop(_) => "Drop stash",
        Action::Restore { .. } => "Restore",
        Action::Take { .. } => "Keep",
        Action::Resolve(_) => "Mark resolved",
        Action::Continue => "Continue",
        Action::Abort => "Abort",
    }
}

fn failure(said: &str, fix: Option<&Action>, theme: &Theme, cx: &Context<GitScreen>) -> Div {
    let danger = theme.color(ColorToken::StatusDanger);
    let said: SharedString = said.to_owned().into();
    div()
        .flex()
        .items_center()
        .gap_2()
        .px_3()
        .py_2()
        .bg(tint(danger, WASH))
        .border_b_1()
        .border_color(tint(danger, 0.3))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .text_size(px(FONT_BODY))
                .text_color(ink(theme, T1))
                .child(said),
        )
        .children(fix.map(|fix| {
            let action = fix.clone();
            button(
                "git-told-fix",
                fix_label(fix),
                None,
                ButtonKind::Primary,
                theme,
            )
            .on_click(
                cx.listener(move |screen, _: &ClickEvent, _, cx| screen.run(action.clone(), cx)),
            )
        }))
}
