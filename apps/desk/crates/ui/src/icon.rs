use std::borrow::Cow;

use gpui::AssetRegistry;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Icon {
    Sidebar,
    Plus,
    Search,
    Bell,
    Minimize,
    Maximize,
    Restore,
    Close,
    Branch,
    Arrow,
    Expand,
}

impl Icon {
    const ALL: [Icon; 11] = [
        Icon::Sidebar,
        Icon::Plus,
        Icon::Search,
        Icon::Bell,
        Icon::Minimize,
        Icon::Maximize,
        Icon::Restore,
        Icon::Close,
        Icon::Branch,
        Icon::Arrow,
        Icon::Expand,
    ];

    pub fn path(self) -> &'static str {
        match self {
            Icon::Sidebar => "icons/sidebar.svg",
            Icon::Plus => "icons/plus.svg",
            Icon::Search => "icons/search.svg",
            Icon::Bell => "icons/bell.svg",
            Icon::Minimize => "icons/minimize.svg",
            Icon::Maximize => "icons/maximize.svg",
            Icon::Restore => "icons/restore.svg",
            Icon::Close => "icons/close.svg",
            Icon::Branch => "icons/branch.svg",
            Icon::Arrow => "icons/arrow.svg",
            Icon::Expand => "icons/expand.svg",
        }
    }

    fn svg(self) -> &'static str {
        match self {
            Icon::Sidebar => include_str!("../assets/icons/panel-left.svg"),
            Icon::Plus => include_str!("../assets/icons/add.svg"),
            Icon::Search => include_str!("../assets/icons/search.svg"),
            Icon::Bell => include_str!("../assets/icons/bell.svg"),
            Icon::Minimize => include_str!("../assets/icons/subtract.svg"),
            Icon::Maximize => include_str!("../assets/icons/square.svg"),
            Icon::Restore => include_str!("../assets/icons/chrome-restore.svg"),
            Icon::Close => include_str!("../assets/icons/cancel.svg"),
            Icon::Branch => include_str!("../assets/icons/branch.svg"),
            Icon::Arrow => include_str!("../assets/icons/arrow-right.svg"),
            Icon::Expand => include_str!("../assets/icons/full-screen-maximize.svg"),
        }
    }

    pub fn registry() -> AssetRegistry {
        Icon::ALL
            .iter()
            .map(|icon| (icon.path(), Cow::Borrowed(icon.svg().as_bytes())))
            .collect()
    }
}
