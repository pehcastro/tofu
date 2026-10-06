use gpui::{
    App, ClickEvent, Div, ElementId, FontWeight, SharedString, Stateful, Svg, Window, div,
    prelude::*, px, rgba, svg,
};

use crate::icon::Icon;
use crate::metrics::{
    CONTROL, ICON_SMALL, ICON_TINY, RADIUS_CONTROL, RADIUS_STATUS, RADIUS_TOAST,
    STATUS_ITEM_HEIGHT, TEXT, TEXT_SMALL, TEXT_TINY, TOAST_DISMISS, TOAST_MAX_WIDTH,
};
use crate::theme;

pub fn icon(glyph: Icon, size: f32, color: u32) -> Svg {
    svg()
        .path(glyph.path())
        .size(px(size))
        .flex_none()
        .text_color(rgba(color))
}

pub fn control(id: impl Into<ElementId>, label: &'static str) -> Stateful<Div> {
    div()
        .id(id)
        .aria_label(label)
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .cursor_pointer()
        .hover(|style| style.bg(rgba(theme::HOVER)))
}

pub fn icon_button(id: impl Into<ElementId>, glyph: Icon, label: &'static str) -> Stateful<Div> {
    control(id, label)
        .size(px(CONTROL))
        .rounded(px(RADIUS_CONTROL))
        .child(icon(glyph, crate::metrics::ICON, theme::ICON))
}

pub fn status_item(id: impl Into<ElementId>, label: &'static str) -> Stateful<Div> {
    control(id, label)
        .h(px(STATUS_ITEM_HEIGHT))
        .px_2()
        .gap_1p5()
        .rounded(px(RADIUS_STATUS))
        .text_size(px(TEXT_SMALL))
        .font_weight(FontWeight::MEDIUM)
        .text_color(rgba(theme::STATUS))
}

pub fn toast(
    message: SharedString,
    badge: &'static str,
    on_dismiss: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> Div {
    div()
        .flex()
        .items_center()
        .gap_2p5()
        .max_w(px(TOAST_MAX_WIDTH))
        .py_2()
        .pl_3p5()
        .pr_2p5()
        .rounded(px(RADIUS_TOAST))
        .bg(rgba(theme::TOAST))
        .border_1()
        .border_color(rgba(theme::TOAST_EDGE))
        .shadow_lg()
        .text_size(px(TEXT))
        .text_color(rgba(theme::TEXT))
        .child(icon(Icon::Arrow, ICON_SMALL, theme::TEXT_MUTED))
        .child(div().flex_1().child(message))
        .child(
            div()
                .flex_none()
                .px_2()
                .py_0p5()
                .rounded_full()
                .bg(rgba(theme::BADGE))
                .text_size(px(TEXT_TINY))
                .text_color(rgba(theme::TEXT_MUTED))
                .child(badge),
        )
        .child(
            control("toast-dismiss", "Dismiss")
                .size(px(TOAST_DISMISS))
                .rounded(px(RADIUS_CONTROL))
                .child(icon(Icon::Close, ICON_TINY, theme::ICON))
                .on_click(on_dismiss),
        )
}
