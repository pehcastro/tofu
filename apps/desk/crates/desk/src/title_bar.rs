use desk_core::control::Control;
use desk_ui::component::{control, icon, icon_button};
use desk_ui::icon::Icon;
use desk_ui::metrics::{
    ACCOUNT_GRADIENT_ANGLE, AVATAR, CAPTION_WIDTH, CONTROL, ICON_SMALL, RADIUS_CAPTION, RADIUS_TAB,
    SIDEBAR_CLOSED_WIDTH, SIDEBAR_WIDTH, TAB_HEIGHT, TEXT, TITLE_BAR_HEIGHT,
};
use desk_ui::theme;
use gpui::{
    Context, FontWeight, IntoElement, Window, WindowControlArea, div, linear_color_stop,
    linear_gradient, prelude::*, px, rgba,
};

use crate::desk::Desk;

const PRODUCT_NAME: &str = "tofu";

#[derive(Clone, Copy)]
enum Caption {
    Minimize,
    Maximize,
    Restore,
    Close,
}

impl Caption {
    fn area(self) -> WindowControlArea {
        match self {
            Caption::Minimize => WindowControlArea::Min,
            Caption::Maximize | Caption::Restore => WindowControlArea::Max,
            Caption::Close => WindowControlArea::Close,
        }
    }

    fn icon(self) -> Icon {
        match self {
            Caption::Minimize => Icon::Minimize,
            Caption::Maximize => Icon::Maximize,
            Caption::Restore => Icon::Restore,
            Caption::Close => Icon::Close,
        }
    }

    fn label(self) -> &'static str {
        match self {
            Caption::Minimize => "Minimize",
            Caption::Maximize => "Maximize",
            Caption::Restore => "Restore",
            Caption::Close => "Close",
        }
    }

    fn hover(self) -> u32 {
        match self {
            Caption::Minimize | Caption::Maximize | Caption::Restore => theme::CAPTION_HOVER,
            Caption::Close => theme::CLOSE_HOVER,
        }
    }

    fn button(self) -> impl IntoElement {
        let label = self.label();
        div()
            .id(label)
            .aria_label(label)
            .group(label)
            .w(px(CAPTION_WIDTH))
            .h(px(CONTROL))
            .flex()
            .items_center()
            .justify_center()
            .rounded(px(RADIUS_CAPTION))
            .window_control_area(self.area())
            .hover(|style| style.bg(rgba(self.hover())))
            .child(
                icon(self.icon(), ICON_SMALL, theme::CAPTION)
                    .group_hover(label, |style| style.text_color(rgba(theme::STRONG))),
            )
    }
}

fn captions(window: &Window) -> impl IntoElement {
    let middle = if window.is_maximized() {
        Caption::Restore
    } else {
        Caption::Maximize
    };
    div()
        .flex()
        .items_center()
        .gap_0p5()
        .mx_1p5()
        .child(Caption::Minimize.button())
        .child(middle.button())
        .child(Caption::Close.button())
}

pub fn render(sidebar_open: bool, window: &Window, cx: &mut Context<Desk>) -> impl IntoElement {
    let column = if sidebar_open {
        SIDEBAR_WIDTH
    } else {
        SIDEBAR_CLOSED_WIDTH
    };
    div()
        .h(px(TITLE_BAR_HEIGHT))
        .flex_none()
        .flex()
        .items_center()
        .gap_1()
        .child(
            div()
                .w(px(column))
                .h_full()
                .flex_none()
                .flex()
                .items_center()
                .gap_1()
                .pl_2p5()
                .child(
                    icon_button("sidebar-toggle", Icon::Sidebar, "Toggle sidebar")
                        .on_click(cx.listener(|desk, _, _, cx| desk.toggle_sidebar(cx))),
                )
                .when(sidebar_open, |name| {
                    name.child(
                        div()
                            .pl_1p5()
                            .font_weight(FontWeight::BOLD)
                            .text_color(rgba(theme::NAME))
                            .child(PRODUCT_NAME),
                    )
                }),
        )
        .child(
            control("new-workspace", Control::NewWorkspace.label())
                .h(px(TAB_HEIGHT))
                .px_2p5()
                .rounded(px(RADIUS_TAB))
                .child(icon(Icon::Plus, ICON_SMALL, theme::TAB))
                .on_click(Desk::teller(Control::NewWorkspace, cx)),
        )
        .child(
            div()
                .id("drag")
                .flex_1()
                .h_full()
                .window_control_area(WindowControlArea::Drag),
        )
        .child(
            control("palette", "Command palette")
                .h(px(CONTROL))
                .px_2p5()
                .gap_2()
                .rounded(px(RADIUS_TAB))
                .child(icon(Icon::Search, ICON_SMALL, theme::TAB))
                .child(
                    div()
                        .text_size(px(TEXT))
                        .text_color(rgba(theme::TEXT_MUTED))
                        .child(Control::Palette.label()),
                )
                .on_click(Desk::teller(Control::Palette, cx)),
        )
        .child(
            icon_button("notifications", Icon::Bell, Control::Notifications.label())
                .on_click(Desk::teller(Control::Notifications, cx)),
        )
        .child(
            control("account", Control::Account.label())
                .size(px(CONTROL))
                .rounded_full()
                .child(div().size(px(AVATAR)).rounded_full().bg(linear_gradient(
                    ACCOUNT_GRADIENT_ANGLE,
                    linear_color_stop(rgba(theme::ACCOUNT_FROM), 0.0),
                    linear_color_stop(rgba(theme::ACCOUNT_TO), 1.0),
                )))
                .on_click(Desk::teller(Control::Account, cx)),
        )
        .when(!cfg!(target_os = "macos"), |bar| {
            bar.child(captions(window))
        })
}
