use desk_core::control::Control;
use desk_ui::component::{control, icon, icon_button};
use desk_ui::icon::Icon;
use desk_ui::metrics::{
    ACCOUNT_GRADIENT_ANGLE, AVATAR, CAPTION_WIDTH, CONTROL, ICON_SMALL, RADIUS_CAPTION, RADIUS_TAB,
    SIDEBAR_CLOSED_WIDTH, SIDEBAR_WIDTH, TAB_HEIGHT, TEXT, TITLE_BAR_HEIGHT,
};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    Context, FontWeight, IntoElement, Window, WindowControlArea, div, linear_color_stop,
    linear_gradient, prelude::*, px,
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

    fn hover(self) -> ColorToken {
        match self {
            Caption::Minimize | Caption::Maximize | Caption::Restore => {
                ColorToken::StateCaptionHover
            }
            Caption::Close => ColorToken::StateCloseHover,
        }
    }

    fn button(self, theme: &Theme) -> impl IntoElement {
        let label = self.label();
        let hover = theme.color(self.hover());
        let strong = theme.color(ColorToken::TextStrong);
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
            .hover(move |style| style.bg(hover))
            .child(
                icon(
                    self.icon(),
                    ICON_SMALL,
                    theme.color(ColorToken::TextCaption),
                )
                .group_hover(label, move |style| style.text_color(strong)),
            )
    }
}

fn captions(window: &Window, theme: &Theme) -> impl IntoElement {
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
        .child(Caption::Minimize.button(theme))
        .child(middle.button(theme))
        .child(Caption::Close.button(theme))
}

pub fn render(
    sidebar_open: bool,
    theme: &Theme,
    window: &Window,
    cx: &mut Context<Desk>,
) -> impl IntoElement {
    let tab = theme.color(ColorToken::TextTab);
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
                    icon_button("sidebar-toggle", Icon::Sidebar, "Toggle sidebar", theme)
                        .on_click(cx.listener(|desk, _, _, cx| desk.toggle_sidebar(cx))),
                )
                .when(sidebar_open, |name| {
                    name.child(
                        div()
                            .pl_1p5()
                            .font_weight(FontWeight::BOLD)
                            .text_color(theme.color(ColorToken::TextName))
                            .child(PRODUCT_NAME),
                    )
                }),
        )
        .child(
            control("new-workspace", Control::NewWorkspace.label(), theme)
                .h(px(TAB_HEIGHT))
                .px_2p5()
                .rounded(px(RADIUS_TAB))
                .child(icon(Icon::Plus, ICON_SMALL, tab))
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
            control("palette", "Command palette", theme)
                .h(px(CONTROL))
                .px_2p5()
                .gap_2()
                .rounded(px(RADIUS_TAB))
                .child(icon(Icon::Search, ICON_SMALL, tab))
                .child(
                    div()
                        .text_size(px(TEXT))
                        .text_color(theme.color(ColorToken::TextMuted))
                        .child(Control::Palette.label()),
                )
                .on_click(Desk::teller(Control::Palette, cx)),
        )
        .child(
            icon_button(
                "notifications",
                Icon::Bell,
                Control::Notifications.label(),
                theme,
            )
            .on_click(Desk::teller(Control::Notifications, cx)),
        )
        .child(
            control("account", Control::Account.label(), theme)
                .size(px(CONTROL))
                .rounded_full()
                .child(div().size(px(AVATAR)).rounded_full().bg(linear_gradient(
                    ACCOUNT_GRADIENT_ANGLE,
                    linear_color_stop(theme.color(ColorToken::AccountFrom), 0.0),
                    linear_color_stop(theme.color(ColorToken::AccountTo), 1.0),
                )))
                .on_click(Desk::teller(Control::Account, cx)),
        )
        .when(!cfg!(target_os = "macos"), |bar| {
            bar.child(captions(window, theme))
        })
}
