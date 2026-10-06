use desk_core::control::Control;
use desk_ui::component::{icon, status_item};
use desk_ui::icon::Icon;
use desk_ui::metrics::{ICON_SMALL, STATUS_BAR_HEIGHT};
use desk_ui::theme;
use gpui::{Context, Div, IntoElement, Stateful, div, prelude::*, px};

use crate::desk::Desk;

fn item(control: Control, cx: &mut Context<Desk>) -> Stateful<Div> {
    status_item(control.label(), control.label())
        .when(control == Control::Branch, |item| {
            item.child(icon(Icon::Branch, ICON_SMALL, theme::STATUS))
        })
        .child(control.label())
        .on_click(Desk::teller(control, cx))
}

pub fn render(cx: &mut Context<Desk>) -> impl IntoElement {
    div()
        .h(px(STATUS_BAR_HEIGHT))
        .flex_none()
        .flex()
        .items_center()
        .gap_0p5()
        .px_2p5()
        .children(Control::STATUS_LEFT.map(|control| item(control, cx)))
        .child(div().flex_1())
        .children(Control::STATUS_RIGHT.map(|control| item(control, cx)))
}
