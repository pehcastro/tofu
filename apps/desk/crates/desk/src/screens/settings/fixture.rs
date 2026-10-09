use desk_core::settings::Scope as WireScope;
use gpui::SharedString;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum DeskPage {
    Keys,
    Editor,
    Git,
}

impl DeskPage {
    pub fn name(self) -> &'static str {
        match self {
            DeskPage::Keys => "Keyboard",
            DeskPage::Editor => "Editor",
            DeskPage::Git => "Git",
        }
    }

    pub fn page(self) -> &'static Page {
        match self {
            DeskPage::Keys => &KEYS,
            DeskPage::Editor => &EDITOR,
            DeskPage::Git => &GIT,
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum PageId {
    Desk(DeskPage),
    Tofu(SharedString),
}

pub const DESK_NAV: (&str, [DeskPage; 3]) =
    ("Desk", [DeskPage::Keys, DeskPage::Editor, DeskPage::Git]);
pub const TOFU_NAV: &str = "tofu";
pub const FIRST_TOFU_PAGE: &str = "Turn";

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Scope {
    Everywhere,
    Project,
}

impl Scope {
    pub fn wire(self) -> WireScope {
        match self {
            Scope::Everywhere => WireScope::Global,
            Scope::Project => WireScope::Project,
        }
    }
}

pub enum Control {
    Switch(bool),
    Pack,
}

pub struct Setting {
    pub key: &'static str,
    pub name: &'static str,
    pub desc: &'static str,
    pub control: Control,
}

pub struct Page {
    pub title: &'static str,
    pub desc: &'static str,
    pub groups: &'static [(&'static str, &'static [Setting])],
}

const fn switch(key: &'static str, name: &'static str, desc: &'static str) -> Setting {
    Setting {
        key,
        name,
        desc,
        control: Control::Switch(true),
    }
}

pub const FILE_ICONS: &str = "file-icons";

const EDITOR: Page = Page {
    title: "Editor",
    desc: "How the editor draws your files.",
    groups: &[(
        "Files",
        &[Setting {
            key: FILE_ICONS,
            name: "File icons",
            desc: "The icon pack the file tree and the file tabs draw.",
            control: Control::Pack,
        }],
    )],
};

const GIT: Page = Page {
    title: "Git",
    desc: "How the desk reads and shows your repository. tofu itself never commits.",
    groups: &[
        (
            "Changes",
            &[
                switch("autoRefresh", "Refresh on file and git events", ""),
                switch("untracked", "Show untracked files", ""),
                switch(
                    "agentMarks",
                    "Mark agent changes",
                    "Violet marks on lines and rows an agent wrote this session.",
                ),
                switch("confirmDiscard", "Confirm before discarding", ""),
            ],
        ),
        (
            "Blame",
            &[
                switch("gitBlame", "Git blame on the current line", ""),
                switch("agentBlame", "Agent blame on the current line", ""),
            ],
        ),
    ],
};

const KEYS: Page = Page {
    title: "Keyboard",
    desc: "The keys the desk answers to. Rebinding comes later.",
    groups: &[],
};

pub const KEYS_GROUP: &str = "Tiles and tabs";

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Effect {
    Dither,
    Fade,
    Duotone,
    Halftone,
    Grain,
    Scan,
    Glow,
}

impl Effect {
    pub const ALL: [Effect; 7] = [
        Effect::Dither,
        Effect::Fade,
        Effect::Duotone,
        Effect::Halftone,
        Effect::Grain,
        Effect::Scan,
        Effect::Glow,
    ];

    pub fn name(self) -> &'static str {
        match self {
            Effect::Dither => "Dithered fade",
            Effect::Fade => "Soft fade",
            Effect::Duotone => "Duotone",
            Effect::Halftone => "Halftone",
            Effect::Grain => "Film grain",
            Effect::Scan => "Scanlines",
            Effect::Glow => "Glow",
        }
    }

    pub fn sub(self) -> &'static str {
        match self {
            Effect::Dither => "the image dissolves into the window through ordered dots",
            Effect::Fade => "a plain gradient into the window color",
            Effect::Duotone => "grey, mapped between two theme colors",
            Effect::Halftone => "a dot screen rising from the middle",
            Effect::Grain => "fine noise over everything",
            Effect::Scan => "thin lines, strongest at the top",
            Effect::Glow => "a soft light rising from the bottom",
        }
    }

    pub fn on_at_start(self) -> bool {
        matches!(self, Effect::Dither | Effect::Scan)
    }
}

pub const IMAGES: [&str; 3] = ["street.jpg", "room.jpg", "dusk.png"];
pub const IMAGE_DIR: &str = "../../../../.local/desk-app/mockups/canvas/bg";
pub const ADD_IMAGE_TELL: &str = "Picks an image or a short video from disk; it is copied to ~/.tofu/intro/ and never leaves the machine.";
pub const OPEN_INTRO_TELL: &str = "Opens the intro screen in a new tab.";
pub const PROJECT: &str = "notes-app";
pub const LATEST_SESSION: &str = "clear-sable-eagle";
pub const MODEL: &str = "Opus 5";
pub const EFFORT: &str = "High";
