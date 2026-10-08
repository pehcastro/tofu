use desk_core::control::Control;
use desk_ui::components::status_bar::Quota;
use desk_ui::components::title_bar::{Account, Title, TitleBar, TitlePick, WindowKeys};
use gpui::{AnyElement, Context};

use crate::desk::Desk;

pub fn title_bar(
    sidebar_open: bool,
    tabs_x: f32,
    quota: Option<&Quota>,
    tabs: Option<AnyElement>,
    cx: &mut Context<Desk>,
) -> TitleBar {
    let title = Title {
        sidebar_open,
        palette_keys: Control::Palette.label().into(),
        notices: Vec::new(),
        letter: quota.and_then(|quota| {
            let first = quota.account.chars().find(|c| c.is_alphanumeric())?;
            Some(first.to_lowercase().collect::<String>().into())
        }),
        account: quota.map(|quota| Account {
            name: quota.account.clone(),
            found: format!("{} window at {}%", quota.window, quota.percent).into(),
            accounts: None,
        }),
        whats_new: None,
        keys: WindowKeys::Live,
        open: None,
        tabs_x,
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
