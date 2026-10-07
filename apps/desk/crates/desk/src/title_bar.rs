use desk_core::control::Control;
use desk_ui::components::title_bar::{Title, TitleBar, TitlePick, WindowKeys};
use gpui::{AnyElement, Context, SharedString};

use crate::desk::Desk;

pub fn title_bar(
    sidebar_open: bool,
    letter: Option<SharedString>,
    tabs: Option<AnyElement>,
    cx: &mut Context<Desk>,
) -> TitleBar {
    let title = Title {
        sidebar_open,
        palette_keys: Control::Palette.label().into(),
        notices: Vec::new(),
        letter,
        account: None,
        whats_new: None,
        keys: WindowKeys::Live,
        open: None,
    };
    TitleBar::new(
        "title-bar",
        title,
        tabs,
        cx.listener(|desk, pick: &TitlePick, window, cx| match pick {
            TitlePick::Sidebar => desk.toggle_sidebar(cx),
            TitlePick::Palette => desk.open_palette(window, cx),
            TitlePick::NewWorkspace => desk.tell(Control::NewWorkspace, cx),
            TitlePick::Notice(_) => desk.tell(Control::Notifications, cx),
            TitlePick::Accounts | TitlePick::Settings | TitlePick::WhatsNew | TitlePick::Docs => {
                desk.tell(Control::Account, cx)
            }
        }),
    )
}
