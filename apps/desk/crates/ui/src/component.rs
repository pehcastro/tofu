use gpui::{
    App, ClickEvent, Div, ElementId, FontWeight, Rgba, SharedString, Stateful, Svg, Window, div,
    prelude::*, px, svg,
};

use crate::icon::Icon;
use crate::metrics::{
    CONTROL, HAIRLINE, ICON_SMALL, ICON_TINY, RADIUS_CONTROL, RADIUS_STATUS, RADIUS_TOAST,
    STATUS_ITEM_HEIGHT, TEXT, TEXT_SMALL, TEXT_TINY, TOAST_DISMISS, TOAST_MAX_WIDTH,
};
use crate::theme::{BorderToken, ColorToken, FlagToken, NumberToken, Theme};

pub fn icon(glyph: Icon, size: f32, color: Rgba) -> Svg {
    svg()
        .path(glyph.path())
        .size(px(size))
        .flex_none()
        .text_color(color)
}

pub fn control(id: impl Into<ElementId>, label: &'static str, theme: &Theme) -> Stateful<Div> {
    let hover = theme.color(ColorToken::StateHover);
    div()
        .id(id)
        .aria_label(label)
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .cursor_pointer()
        .hover(move |style| style.bg(hover))
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

fn edge(element: Div, border: Option<Rgba>) -> Div {
    match border {
        Some(color) => element.border_1().border_color(color),
        None => element,
    }
}

pub fn outer_card(theme: &Theme) -> Div {
    edge(
        div()
            .flex()
            .bg(theme.color(ColorToken::CardsOuterFill))
            .rounded(px(theme.number(NumberToken::CardsOuterRadius)))
            .p(px(theme.number(NumberToken::CardsOuterGap))),
        theme.border(BorderToken::CardsOuter),
    )
}

pub fn inner_card(theme: &Theme) -> Div {
    let radius = theme.number(NumberToken::CardsInnerRadius);
    edge(
        div()
            .relative()
            .flex_1()
            .bg(theme.color(ColorToken::CardsInnerFill))
            .rounded(px(radius)),
        theme.border(BorderToken::CardsInner),
    )
    .when(theme.flag(FlagToken::CardsInnerHighlight), |card| {
        card.child(
            div()
                .absolute()
                .top_0()
                .left(px(radius))
                .right(px(radius))
                .h(px(HAIRLINE))
                .bg(theme.color(ColorToken::CardsInnerSheen)),
        )
    })
}

pub fn toast(
    message: SharedString,
    badge: &'static str,
    theme: &Theme,
    on_dismiss: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> Div {
    edge(
        div()
            .flex()
            .items_center()
            .gap_2p5()
            .max_w(px(TOAST_MAX_WIDTH))
            .py_2()
            .pl_3p5()
            .pr_2p5()
            .rounded(px(RADIUS_TOAST))
            .bg(theme.color(ColorToken::ToastFill)),
        theme.border(BorderToken::Toast),
    )
    .shadow_lg()
    .text_size(px(TEXT))
    .text_color(theme.color(ColorToken::TextBase))
    .child(icon(
        Icon::Arrow,
        ICON_SMALL,
        theme.color(ColorToken::TextMuted),
    ))
    .child(div().flex_1().child(message))
    .child(
        div()
            .flex_none()
            .px_2()
            .py_0p5()
            .rounded_full()
            .bg(theme.color(ColorToken::ToastBadge))
            .text_size(px(TEXT_TINY))
            .text_color(theme.color(ColorToken::TextMuted))
            .child(badge),
    )
    .child(
        control("toast-dismiss", "Dismiss", theme)
            .size(px(TOAST_DISMISS))
            .rounded(px(RADIUS_CONTROL))
            .child(icon(
                Icon::Close,
                ICON_TINY,
                theme.color(ColorToken::TextIcon),
            ))
            .on_click(on_dismiss),
    )
}
