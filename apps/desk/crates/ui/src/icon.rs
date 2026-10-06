use std::borrow::Cow;

use gpui::AssetRegistry;

macro_rules! line_icon {
    ($body:literal) => {
        concat!(
            r##"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="#000" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">"##,
            $body,
            "</svg>"
        )
    };
}

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
}

impl Icon {
    const ALL: [Icon; 10] = [
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
        }
    }

    fn svg(self) -> &'static str {
        match self {
            Icon::Sidebar => line_icon!(
                r#"<rect x="2" y="3" width="12" height="10" rx="2"/><path d="M6 3v10"/>"#
            ),
            Icon::Plus => line_icon!(r#"<path d="M8 3.5v9M3.5 8h9"/>"#),
            Icon::Search => line_icon!(r#"<circle cx="7" cy="7" r="4"/><path d="M10 10l3 3"/>"#),
            Icon::Bell => line_icon!(r#"<path d="M4 11V7a4 4 0 0 1 8 0v4l1 1.5H3zM6.5 14h3"/>"#),
            Icon::Minimize => line_icon!(r#"<path d="M4 8.5h8"/>"#),
            Icon::Maximize => line_icon!(r#"<rect x="4" y="4" width="8" height="8" rx="2"/>"#),
            Icon::Restore => line_icon!(
                r#"<rect x="4" y="6" width="6" height="6" rx="1.5"/><path d="M6.5 4h4A1.5 1.5 0 0 1 12 5.5v4"/>"#
            ),
            Icon::Close => line_icon!(r#"<path d="M4.5 4.5l7 7M11.5 4.5l-7 7"/>"#),
            Icon::Branch => line_icon!(
                r#"<circle cx="5" cy="4" r="1.5"/><circle cx="5" cy="12" r="1.5"/><circle cx="11" cy="6" r="1.5"/><path d="M5 5.5v5M11 7.5c0 2-6 1.5-6 3"/>"#
            ),
            Icon::Arrow => line_icon!(r#"<path d="M3 8h9M9 5l3 3-3 3"/>"#),
        }
    }

    pub fn registry() -> AssetRegistry {
        Icon::ALL
            .iter()
            .map(|icon| (icon.path(), Cow::Borrowed(icon.svg().as_bytes())))
            .collect()
    }
}
