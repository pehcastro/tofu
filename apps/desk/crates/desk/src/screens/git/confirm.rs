use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::overlay::{Popover, toggles_on_press};
use desk_ui::components::paint::{ink, tint};
use desk_ui::components::size::{FONT_SMALL, T1, T2};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::ColorToken;
use gpui::{
    AnyElement, App, ClickEvent, Context, Div, ElementId, Entity, Stateful, Window, div,
    prelude::*, px,
};

use super::model::{Action, GitScreen};
use super::sync::plural;

pub enum Discarded {
    Files(Vec<String>),
    Hunk(String),
}

fn set(open: &Entity<bool>, to: bool, cx: &mut App) {
    open.update(cx, |open, cx| {
        *open = to;
        cx.notify();
    });
}

pub fn discard(
    id: impl Into<ElementId>,
    trigger: Stateful<Div>,
    what: Discarded,
    _screen: &GitScreen,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> AnyElement {
    let id: ElementId = id.into();
    let theme = ActiveTheme::theme(cx);
    let open = window.use_keyed_state((id.clone(), "state"), cx, |_, _| false);
    let was_open = *open.read(cx);
    let trigger = toggles_on_press(trigger, {
        let open = open.clone();
        move |_, cx| set(&open, !was_open, cx)
    });
    if !was_open {
        return Popover::new(id, trigger).into_any_element();
    }
    let (question, label, action) = match what {
        Discarded::Files(paths) => {
            let (question, label) = match paths.as_slice() {
                [one] => (
                    format!("Discard the changes to {one} permanently?"),
                    "Discard".to_owned(),
                ),
                many => {
                    let files = plural(many.len(), "file");
                    (
                        format!("Discard the changes to {files} permanently?"),
                        format!("Discard {files}"),
                    )
                }
            };
            (question, label, Action::Discard(paths))
        }
        Discarded::Hunk(patch) => (
            "Discard this hunk permanently?".to_owned(),
            "Discard hunk".to_owned(),
            Action::DiscardPatch(patch),
        ),
    };
    let danger = theme.color(ColorToken::StatusDanger);
    let cancel = {
        let open = open.clone();
        button(
            "git-discard-cancel",
            "Cancel",
            None,
            ButtonKind::Plain,
            &theme,
        )
        .on_click(move |_, _, cx| set(&open, false, cx))
    };
    let confirm = {
        let open = open.clone();
        button("git-discard-go", label, None, ButtonKind::Plain, &theme)
            .bg(tint(danger, 0.16))
            .text_color(danger)
            .on_click(cx.listener(move |screen, _: &ClickEvent, _, cx| {
                set(&open, false, cx);
                screen.run(action.clone(), cx)
            }))
    };
    let body = div()
        .flex()
        .flex_col()
        .gap_2()
        .text_size(px(FONT_SMALL))
        .line_height(px(17.0))
        .on_mouse_down_out({
            let open = open.clone();
            move |_, _, cx| set(&open, false, cx)
        })
        .child(div().text_color(ink(&theme, T1)).child(question))
        .child(
            div()
                .text_color(ink(&theme, T2))
                .child("It is gone for good: Undo cannot bring it back."),
        )
        .child(
            div()
                .flex()
                .justify_end()
                .gap_2()
                .pt_1()
                .child(cancel)
                .child(confirm),
        );
    Popover::new(id, trigger)
        .open(true)
        .child(body)
        .into_any_element()
}
