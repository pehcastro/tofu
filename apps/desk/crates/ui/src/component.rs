use gpui::{Div, ElementId, FontWeight, Rgba, Stateful, Svg, div, prelude::*, px, svg};

use crate::components::paint::pressed;
use crate::icon::Icon;
use crate::metrics::{CONTROL, RADIUS_CONTROL, RADIUS_STATUS, STATUS_ITEM_HEIGHT, TEXT_SMALL};
use crate::theme::{ColorToken, Theme};

pub fn icon(glyph: Icon, size: f32, color: Rgba) -> Svg {
    svg()
        .path(glyph.path())
        .size(px(size))
        .flex_none()
        .text_color(color)
}

pub fn control(id: impl Into<ElementId>, label: &'static str, theme: &Theme) -> Stateful<Div> {
    let hover = theme.color(ColorToken::StateHover);
    pressed(
        div()
            .id(id)
            .aria_label(label)
            .flex()
            .flex_none()
            .items_center()
            .justify_center()
            .cursor_pointer()
            .hover(move |style| style.bg(hover)),
        theme.color(ColorToken::CardsInnerFill),
    )
}

pub fn icon_button(
    id: impl Into<ElementId>,
    glyph: Icon,
    label: &'static str,
    theme: &Theme,
) -> Stateful<Div> {
    control(id, label, theme)
        .size(px(CONTROL))
        .rounded(px(RADIUS_CONTROL))
        .child(icon(
            glyph,
            crate::metrics::ICON,
            theme.color(ColorToken::TextIcon),
        ))
}

pub fn status_item(id: impl Into<ElementId>, label: &'static str, theme: &Theme) -> Stateful<Div> {
    control(id, label, theme)
        .h(px(STATUS_ITEM_HEIGHT))
        .px_2()
        .gap_1p5()
        .rounded(px(RADIUS_STATUS))
        .text_size(px(TEXT_SMALL))
        .font_weight(FontWeight::MEDIUM)
        .text_color(theme.color(ColorToken::TextStatus))
}
