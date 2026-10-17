use desk_core::git::{State, UndoStep};
use desk_ui::components::agents::ago;
use desk_ui::components::button::{self as kit, ButtonKind};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::overlay::{Popover, toggles_on_press};
use desk_ui::components::paint::{glyph, ink, ring, tint};
use desk_ui::components::size::{FONT_SMALL, RADIUS_ROW, T1, T2, T3};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::{ICON, ICON_TINY};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, ClickEvent, Context, Div, Entity, SharedString, Window, div, prelude::*, px,
};

use super::model::{Action, GitScreen, Loaded};
use super::sync::{header_button, short};

const POP_WIDTH: f32 = 420.0;
const LIST_MAX: f32 = 360.0;
const CHECK_BOX: f32 = 14.0;
const SECONDS_PER_MINUTE: i64 = 60;

struct UndoPop {
    open: bool,
    keep: bool,
}

pub fn button(screen: &GitScreen, window: &mut Window, cx: &mut Context<GitScreen>) -> AnyElement {
    let theme = ActiveTheme::theme(cx);
    let pop = window.use_keyed_state("git-undo-state", cx, |_, _| UndoPop {
        open: false,
        keep: true,
    });
    let (was_open, keep) = {
        let state = pop.read(cx);
        (state.open, state.keep)
    };
    let snapshot = screen.snapshot();
    let dirty = snapshot
        .changes
        .iter()
        .any(|entry| entry.state != State::Ignored);
    let branch = snapshot
        .branch
        .as_ref()
        .map_or("HEAD", |branch| branch.name.as_str());
    let face = header_button(
        "git-undo",
        "Undo",
        glyph(Glyph::Undo, ICON, theme.color(ColorToken::TextBase)),
        true,
        &theme,
    );
    let face = toggles_on_press(face, {
        let (pop, screen) = (pop.clone(), cx.entity());
        move |_, cx| {
            pop.update(cx, |pop, cx| {
                pop.open = !was_open;
                cx.notify();
            });
            if !was_open {
                screen.update(cx, |screen, cx| screen.want_undo(cx));
            }
        }
    });
    let tip = format!("Undo: pick an earlier moment of {branch} to go back to");
    let face = tooltip("git-undo-tip", face, Edge::Frame, tip, &theme, window, cx);
    let face = Popover::new("git-undo-pop", face).width(POP_WIDTH);
    if !was_open {
        return face.into_any_element();
    }
    let now = chrono::Utc::now().timestamp();
    let keeps = dirty && keep;
    let rows: Vec<Div> = match screen.undo_steps() {
        Loaded::Waiting => vec![note("Reading what you did recently", &theme)],
        Loaded::Failed(said) => vec![note(said.clone(), &theme)],
        Loaded::Ready(steps) if steps.len() < 2 => vec![note("Nothing to undo yet", &theme)],
        Loaded::Ready(steps) => steps
            .iter()
            .enumerate()
            .map(|(ix, step)| row(ix, step, now, branch, keeps, &pop, &theme, window, cx))
            .collect(),
    };
    let what = match (dirty, keeps) {
        (true, true) => format!(
            "Restore moves {branch} back to that moment. Your uncommitted changes are stashed first and put back after. Later commits stay in this list so you can come back."
        ),
        (true, false) => format!(
            "Restore moves {branch} back to that moment and throws away your uncommitted changes. Later commits stay in this list so you can come back."
        ),
        (false, _) => format!(
            "Restore moves {branch} back to that moment. Later commits stay in this list so you can come back."
        ),
    };
    let body = div()
        .flex()
        .flex_col()
        .gap_2()
        .on_mouse_down_out({
            let pop = pop.clone();
            move |_, _, cx| {
                pop.update(cx, |pop, cx| {
                    pop.open = false;
                    cx.notify();
                })
            }
        })
        .child(
            div()
                .id("git-undo-list")
                .flex()
                .flex_col()
                .max_h(px(LIST_MAX))
                .overflow_y_scroll()
                .children(rows),
        )
        .when(dirty, |body| body.child(keep_box(keep, &pop, &theme)))
        .child(
            div()
                .text_size(px(FONT_SMALL))
                .line_height(px(17.0))
                .text_color(ink(&theme, T2))
                .child(what),
        );
    face.open(true).child(body).into_any_element()
}

fn note(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .py_1p5()
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, T2))
        .child(text.into())
}

#[expect(
    clippy::too_many_arguments,
    reason = "one row reads the step, the clock, the branch, the box and the popover it closes"
)]
fn row(
    ix: usize,
    step: &UndoStep,
    now: i64,
    branch: &str,
    keep: bool,
    pop: &Entity<UndoPop>,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> Div {
    let minutes = now.saturating_sub(step.when).max(0) / SECONDS_PER_MINUTE;
    let when = ago(minutes as f32);
    let line = div()
        .flex()
        .items_center()
        .gap_2()
        .py_1p5()
        .text_size(px(FONT_SMALL))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .flex()
                .flex_col()
                .child(
                    div()
                        .truncate()
                        .text_color(ink(theme, T1))
                        .child(step.action.clone()),
                )
                .child(
                    div()
                        .text_color(ink(theme, T3))
                        .child(format!("{when} · {}", short(&step.sha))),
                ),
        );
    if ix == 0 {
        return line.child(div().flex_none().text_color(ink(theme, T3)).child("Now"));
    }
    let sha = step.sha.clone();
    let pop = pop.clone();
    let restore = kit::button(
        ("git-undo-restore", ix),
        "Restore",
        None,
        ButtonKind::Text,
        theme,
    )
    .on_click(cx.listener(move |screen, _: &ClickEvent, _, cx| {
        pop.update(cx, |pop, cx| {
            pop.open = false;
            cx.notify();
        });
        screen.run(
            Action::Restore {
                sha: sha.clone(),
                keep,
            },
            cx,
        )
    }));
    let stash = if keep {
        ", stashing your uncommitted changes first"
    } else {
        ""
    };
    let tip = format!(
        "Restore: move {branch} back to {} from {when}{stash}",
        short(&step.sha)
    );
    line.child(tooltip(
        ("git-undo-restore-tip", ix),
        restore,
        Edge::Frame,
        tip,
        theme,
        window,
        cx,
    ))
}

fn keep_box(keep: bool, pop: &Entity<UndoPop>, theme: &Theme) -> impl IntoElement {
    let fill = theme.color(ColorToken::ButtonPrimary);
    let tick = theme.color(ColorToken::ButtonPrimaryText);
    let pop = pop.clone();
    div()
        .id("git-undo-keep")
        .flex()
        .items_center()
        .gap_2()
        .py_1()
        .cursor_pointer()
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, T1))
        .on_click(move |_, _, cx| {
            pop.update(cx, |pop, cx| {
                pop.keep = !pop.keep;
                cx.notify();
            })
        })
        .child(
            div()
                .flex()
                .flex_none()
                .items_center()
                .justify_center()
                .size(px(CHECK_BOX))
                .rounded(px(RADIUS_ROW / 2.0))
                .bg(tint(fill, if keep { 1.0 } else { 0.0 }))
                .shadow(vec![ring(ink(theme, if keep { 0.0 } else { 0.3 }))])
                .child(glyph(
                    Glyph::Check,
                    ICON_TINY,
                    tint(tick, if keep { 1.0 } else { 0.0 }),
                )),
        )
        .child("Keep my changes (stash them first)")
}
