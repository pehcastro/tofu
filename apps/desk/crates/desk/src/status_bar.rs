use desk_core::control::Control;
use desk_ui::component::{icon, status_item};
use desk_ui::icon::Icon;
use desk_ui::metrics::{ICON_SMALL, STATUS_BAR_HEIGHT, TEXT_SMALL};
use desk_ui::theme::{ColorToken, Theme, ThemeError};
use gpui::{Context, Div, IntoElement, Stateful, div, prelude::*, px};

use crate::desk::Desk;

fn item(control: Control, theme: &Theme, cx: &mut Context<Desk>) -> Stateful<Div> {
    status_item(control.label(), control.label(), theme)
        .when(control == Control::Branch, |item| {
            item.child(icon(
                Icon::Branch,
                ICON_SMALL,
                theme.color(ColorToken::TextStatus),
            ))
        })
        .child(control.label())
        .on_click(Desk::teller(control, cx))
}

fn problem_line(problems: &[ThemeError]) -> Option<String> {
    let first = problems.first()?;
    Some(match problems.len() {
        1 => first.to_string(),
        count => format!("{first} (and {} more)", count - 1),
    })
}

pub fn render(theme: &Theme, problems: &[ThemeError], cx: &mut Context<Desk>) -> impl IntoElement {
    div()
        .h(px(STATUS_BAR_HEIGHT))
        .flex_none()
        .flex()
        .items_center()
        .gap_0p5()
        .px_2p5()
        .children(Control::STATUS_LEFT.map(|control| item(control, theme, cx)))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .px_2()
                .truncate()
                .text_size(px(TEXT_SMALL))
                .text_color(theme.color(ColorToken::StatusDanger))
                .children(problem_line(problems)),
        )
        .children(Control::STATUS_RIGHT.map(|control| item(control, theme, cx)))
}
