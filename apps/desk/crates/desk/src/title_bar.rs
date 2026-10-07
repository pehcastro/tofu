use desk_core::control::Control;
use desk_ui::components::title_bar::{Title, TitleBar, TitlePick, WindowKeys};
use gpui::{AnyElement, Context, SharedString};

use crate::desk::Desk;

pub fn title_bar(
    sidebar_open: bool,
    account: Option<SharedString>,
    tabs: Option<AnyElement>,
    cx: &mut Context<Desk>,
) -> TitleBar {
    let title = Title {
        sidebar_open,
        palette_keys: Control::Palette.label().into(),
        unread: false,
        account,
        keys: WindowKeys::Live,
    };
    TitleBar::new(
        "title-bar",
        title,
        tabs,
        cx.listener(|desk, pick: &TitlePick, window, cx| match pick {
            TitlePick::Sidebar => desk.toggle_sidebar(cx),
            TitlePick::Palette => desk.open_palette(window, cx),
            TitlePick::NewWorkspace => desk.tell(Control::NewWorkspace, cx),
            TitlePick::Notifications => desk.tell(Control::Notifications, cx),
            TitlePick::Account => desk.tell(Control::Account, cx),
        }),
    )
}
