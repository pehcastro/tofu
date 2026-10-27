use gpui::SharedString;

use super::stored::{GUIDES, VIM};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum DeskPage {
    Appearance,
    Keys,
    Editor,
    Terminal,
}

impl DeskPage {
    pub fn name(self) -> &'static str {
        match self {
            DeskPage::Appearance => "Appearance",
            DeskPage::Keys => "Keys",
            DeskPage::Editor => "Editor",
            DeskPage::Terminal => "Terminal",
        }
    }

    pub fn page(self) -> &'static Page {
        match self {
            DeskPage::Appearance => &APPEARANCE,
            DeskPage::Keys => &KEYS,
            DeskPage::Editor => &EDITOR,
            DeskPage::Terminal => &TERMINAL,
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum PageId {
    Desk(DeskPage),
    Tofu(SharedString),
    Plugins(Option<SharedString>),
}

pub const PLUGINS_NAV: &str = "Plugins";
pub const PLUGINS_GENERAL: &str = "General";

pub const DESK_NAV: (&str, [DeskPage; 4]) = (
    "Desk",
    [
        DeskPage::Appearance,
        DeskPage::Keys,
        DeskPage::Editor,
        DeskPage::Terminal,
    ],
);
pub const TOFU_NAV: &str = "Tofu";
pub const TOFU_EVERYWHERE: &str =
    "Applies to tofu everywhere: this desk's sessions, the terminal and every other window.";
pub const TERMINAL_ONLY: [&str; 1] = ["Appearance"];

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Models {
    Any,
    Keyed,
    Subscriptions,
    OnePerSubscription,
}

pub const MODEL_SETTINGS: [(&str, Models); 8] = [
    ("continueOnSubscription", Models::OnePerSubscription),
    ("continueOnAPIModel", Models::Keyed),
    ("memoryModel", Models::Any),
    ("modelTier.genius", Models::Any),
    ("modelTier.smart", Models::Any),
    ("modelTier.worker", Models::Any),
    ("modelTier.dumb", Models::Any),
    ("browserModel", Models::Subscriptions),
];

pub struct Fixed {
    pub values: &'static [(&'static str, &'static str)],
    pub custom: &'static str,
}

pub const FIXED_SETTINGS: [(&str, Fixed); 2] = [
    (
        "sideChatAccess",
        Fixed {
            values: &[("read", "read"), ("notes", "notes"), ("files", "files")],
            custom: "Custom globs\u{2026}",
        },
    ),
    (
        "shell",
        Fixed {
            values: &[("", "Your own shell"), ("wsl", "wsl")],
            custom: "Custom path\u{2026}",
        },
    ),
];

pub struct Setting {
    pub key: &'static str,
    pub name: &'static str,
    pub desc: &'static str,
}

pub struct Page {
    pub title: &'static str,
    pub desc: &'static str,
    pub groups: &'static [Group],
}

pub struct Group {
    pub title: &'static str,
    pub about: &'static str,
    pub settings: &'static [Setting],
}

const APPEARANCE: Page = Page {
    title: "Appearance",
    desc: "How this desk looks. Changes apply at once and go to ~/.tofu/desk/config/theme.",
    groups: &[],
};

const EDITOR: Page = Page {
    title: "Editor",
    desc: "How the editor draws your files. Changes go to ~/.tofu/desk/config/settings.json.",
    groups: &[Group {
        title: "Editing",
        about: "How the editor behaves while you type.",
        settings: &[
            Setting {
                key: VIM,
                name: "Vim",
                desc: "Normal, insert and visual modes, and :w to save. A file opened after the change uses it.",
            },
            Setting {
                key: GUIDES,
                name: "Whitespace guides",
                desc: "A thin line at each indent level.",
            },
        ],
    }],
};

const TERMINAL: Page = Page {
    title: "Terminal",
    desc: "The shell a new terminal starts. A terminal already open keeps its own.",
    groups: &[],
};

const KEYS: Page = Page {
    title: "Keys",
    desc: "Click a binding and press the new chord. Changes go to ~/.tofu/desk/config/keymap.json.",
    groups: &[],
};
