use std::rc::Rc;

use desk_core::control::Control;
use gpui::{
    AnyElement, App, ClickEvent, ElementId, FontWeight, SharedString, Window, WindowControlArea,
    div, linear_color_stop, linear_gradient, prelude::*, px,
};

use crate::component::{control, icon, icon_button};
use crate::components::paint::ink;
use crate::components::size::{FONT_AVATAR_LIST, T1};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{
    ACCOUNT_GRADIENT_ANGLE, AVATAR, CAPTION_WIDTH, CONTROL, ICON_SMALL, RADIUS_CAPTION, RADIUS_TAB,
    SIDEBAR_CLOSED_WIDTH, SIDEBAR_WIDTH, TAB_HEIGHT, TEXT, TITLE_BAR_HEIGHT,
};
use crate::theme::{ColorToken, Theme};

const PRODUCT_NAME: &str = "tofu";
const BELL_DOT: f32 = 6.0;
const BELL_DOT_INSET: f32 = 6.0;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TitlePick {
    Sidebar,
    NewWorkspace,
    Palette,
    Notifications,
    Account,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum WindowKeys {
    Live,
    Shown,
}

#[derive(Clone)]
pub struct Title {
    pub sidebar_open: bool,
    pub palette_keys: SharedString,
    pub unread: bool,
    pub account: Option<SharedString>,
    pub keys: WindowKeys,
}

type OnPick = Rc<dyn Fn(&TitlePick, &mut Window, &mut App)>;

#[derive(IntoElement)]
pub struct TitleBar {
    id: ElementId,
    title: Title,
    tabs: Option<AnyElement>,
    on_pick: OnPick,
}

impl TitleBar {
    pub fn new(
        id: impl Into<ElementId>,
        title: Title,
        tabs: Option<AnyElement>,
        on_pick: impl Fn(&TitlePick, &mut Window, &mut App) + 'static,
    ) -> Self {
        Self {
            id: id.into(),
            title,
            tabs,
            on_pick: Rc::new(on_pick),
        }
    }

    fn picked(
        &self,
        pick: TitlePick,
    ) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static + use<> {
        let on_pick = self.on_pick.clone();
        move |_, window, cx| on_pick(&pick, window, cx)
    }
}

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

    fn button(self, keys: WindowKeys, theme: &Theme) -> impl IntoElement {
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
            .when(keys == WindowKeys::Live, |key| {
                key.window_control_area(self.area())
            })
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

fn captions(keys: WindowKeys, maximized: bool, theme: &Theme) -> impl IntoElement {
    let middle = if maximized {
        Caption::Restore
    } else {
        Caption::Maximize
    };
    div()
        .flex()
        .items_center()
        .gap_0p5()
        .mx_1p5()
        .child(Caption::Minimize.button(keys, theme))
        .child(middle.button(keys, theme))
        .child(Caption::Close.button(keys, theme))
}

fn account(letter: Option<SharedString>, theme: &Theme) -> impl IntoElement {
    div()
        .size(px(AVATAR))
        .flex()
        .items_center()
        .justify_center()
        .rounded_full()
        .bg(linear_gradient(
            ACCOUNT_GRADIENT_ANGLE,
            linear_color_stop(theme.color(ColorToken::AccountFrom), 0.0),
            linear_color_stop(theme.color(ColorToken::AccountTo), 1.0),
        ))
        .text_size(px(FONT_AVATAR_LIST))
        .line_height(px(AVATAR))
        .font_weight(FontWeight::SEMIBOLD)
        .text_color(ink(theme, T1))
        .children(letter)
}

impl RenderOnce for TitleBar {
    fn render(mut self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let tab = theme.color(ColorToken::TextTab);
        let tabs = match self.tabs.take() {
            Some(tabs) => div().flex_1().min_w_0().child(tabs).into_any_element(),
            None => control("new-workspace", Control::NewWorkspace.label(), &theme)
                .h(px(TAB_HEIGHT))
                .px_2p5()
                .rounded(px(RADIUS_TAB))
                .child(icon(Icon::Plus, ICON_SMALL, tab))
                .on_click(self.picked(TitlePick::NewWorkspace))
                .into_any_element(),
        };
        let title = &self.title;
        let column = if title.sidebar_open {
            SIDEBAR_WIDTH
        } else {
            SIDEBAR_CLOSED_WIDTH
        };
        let warn = theme.color(ColorToken::StatusWarn);
        div()
            .id(self.id.clone())
            .w_full()
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
                        icon_button("sidebar-toggle", Icon::Sidebar, "Toggle sidebar", &theme)
                            .on_click(self.picked(TitlePick::Sidebar)),
                    )
                    .when(title.sidebar_open, |name| {
                        name.child(
                            div()
                                .pl_1p5()
                                .font_weight(FontWeight::BOLD)
                                .text_color(theme.color(ColorToken::TextName))
                                .child(PRODUCT_NAME),
                        )
                    }),
            )
            .child(tabs)
            .child(
                div()
                    .id("drag")
                    .flex_1()
                    .h_full()
                    .when(title.keys == WindowKeys::Live, |drag| {
                        drag.window_control_area(WindowControlArea::Drag)
                    }),
            )
            .child(
                control("palette", "Command palette", &theme)
                    .h(px(CONTROL))
                    .px_2p5()
                    .gap_2()
                    .rounded(px(RADIUS_TAB))
                    .child(icon(Icon::Search, ICON_SMALL, tab))
                    .child(
                        div()
                            .text_size(px(TEXT))
                            .text_color(theme.color(ColorToken::TextMuted))
                            .child(title.palette_keys.clone()),
                    )
                    .on_click(self.picked(TitlePick::Palette)),
            )
            .child(
                icon_button(
                    "notifications",
                    Icon::Bell,
                    Control::Notifications.label(),
                    &theme,
                )
                .relative()
                .when(title.unread, |bell| {
                    bell.child(
                        div()
                            .absolute()
                            .top(px(BELL_DOT_INSET))
                            .right(px(BELL_DOT_INSET))
                            .size(px(BELL_DOT))
                            .rounded_full()
                            .bg(warn),
                    )
                })
                .on_click(self.picked(TitlePick::Notifications)),
            )
            .child(
                control("account", Control::Account.label(), &theme)
                    .size(px(CONTROL))
                    .rounded_full()
                    .child(account(title.account.clone(), &theme))
                    .on_click(self.picked(TitlePick::Account)),
            )
            .when(!cfg!(target_os = "macos"), |bar| {
                bar.child(captions(title.keys, window.is_maximized(), &theme))
            })
    }
}
