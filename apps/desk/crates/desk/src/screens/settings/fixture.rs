use desk_ui::components::settings::Source;
use desk_ui::components::tree::IconPack;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum PageId {
    Appearance,
    Keys,
    Notify,
    Workspaces,
    Editor,
    Git,
    Turn,
    Context,
    Shell,
    Browser,
    Accounts,
}

impl PageId {
    pub fn name(self) -> &'static str {
        match self {
            PageId::Appearance => "Appearance",
            PageId::Keys => "Keyboard",
            PageId::Notify => "Notifications",
            PageId::Workspaces => "Workspaces",
            PageId::Editor => "Editor",
            PageId::Git => "Git",
            PageId::Turn => "Turn and sub-agents",
            PageId::Context => "Context",
            PageId::Shell => "Shell",
            PageId::Browser => "Browser",
            PageId::Accounts => "Accounts and models",
        }
    }

    pub fn page(self) -> &'static Page {
        match self {
            PageId::Git => &GIT,
            PageId::Appearance => &APPEARANCE,
            PageId::Keys => &KEYS,
            PageId::Editor => &EDITOR,
            PageId::Turn
            | PageId::Notify
            | PageId::Workspaces
            | PageId::Context
            | PageId::Shell
            | PageId::Browser
            | PageId::Accounts => &TURN,
        }
    }
}

pub const NAV: [(&str, &[PageId]); 4] = [
    (
        "General",
        &[PageId::Appearance, PageId::Keys, PageId::Notify],
    ),
    ("Desk", &[PageId::Workspaces, PageId::Editor, PageId::Git]),
    (
        "tofu",
        &[
            PageId::Turn,
            PageId::Context,
            PageId::Shell,
            PageId::Browser,
        ],
    ),
    ("Models", &[PageId::Accounts]),
];

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Scope {
    Everywhere,
    Project,
}

impl Scope {
    pub const ALL: [Scope; 2] = [Scope::Everywhere, Scope::Project];
    pub const LABELS: [&'static str; 2] = ["Everywhere", "This project"];

    pub fn source(self) -> Source {
        match self {
            Scope::Everywhere => Source::Global,
            Scope::Project => Source::Project,
        }
    }
}

pub enum Control {
    Switch(bool),
    Value(&'static str),
    Pack(IconPack),
}

pub struct Setting {
    pub key: &'static str,
    pub name: &'static str,
    pub desc: &'static str,
    pub control: Control,
    pub source: Source,
}

pub struct Page {
    pub title: &'static str,
    pub desc: &'static str,
    pub groups: &'static [(&'static str, &'static [Setting])],
}

const fn switch(
    key: &'static str,
    name: &'static str,
    desc: &'static str,
    on: bool,
    source: Source,
) -> Setting {
    Setting {
        key,
        name,
        desc,
        control: Control::Switch(on),
        source,
    }
}

const fn value(
    key: &'static str,
    name: &'static str,
    desc: &'static str,
    value: &'static str,
    source: Source,
) -> Setting {
    Setting {
        key,
        name,
        desc,
        control: Control::Value(value),
        source,
    }
}

const fn pack(pack: IconPack, desc: &'static str) -> Setting {
    Setting {
        key: pack.key(),
        name: pack.label(),
        desc,
        control: Control::Pack(pack),
        source: Source::Default,
    }
}

const EDITOR: Page = Page {
    title: "Editor",
    desc: "How the editor draws your files.",
    groups: &[(
        "File icons",
        &[
            pack(
                IconPack::Material,
                "The icons the desk shipped with. 25 icons.",
            ),
            pack(
                IconPack::Catppuccin,
                "Mocha. 619 icons by name, extension, language and folder.",
            ),
            pack(
                IconPack::Github,
                "One file icon and one folder icon, nothing else.",
            ),
            pack(
                IconPack::Jetbrains,
                "New UI, dark. 131 icons, open folders keep their icon.",
            ),
            pack(
                IconPack::Makinda,
                "Stroke. 111 icons by name, extension, language and folder.",
            ),
            pack(
                IconPack::Phosphor,
                "26 glyphs by extension. Folders are plain.",
            ),
            pack(
                IconPack::Pierre,
                "Complete, coloured. 56 icons by name and extension.",
            ),
            pack(
                IconPack::Symbols,
                "320 icons by name, extension, language and folder.",
            ),
        ],
    )],
};

const TURN: Page = Page {
    title: "Turn and sub-agents",
    desc: "How the lead works, how many sub-agents it may run, and when it must stop and ask you.",
    groups: &[
        (
            "Approvals",
            &[
                switch(
                    "gatePrompt",
                    "Ask before risky commands",
                    "The lead stops for your answer when the classifier says ask. Off: it only logs.",
                    false,
                    Source::Default,
                ),
                switch(
                    "turnMaySpawn",
                    "Lead may spawn sub-agents",
                    "Off keeps every change in the lead.",
                    true,
                    Source::Default,
                ),
                switch(
                    "verifySubAgents",
                    "Lead checks sub-agent work",
                    "Re-runs checks and a browser check after a report. Costs requests.",
                    false,
                    Source::Project,
                ),
            ],
        ),
        (
            "Limits",
            &[
                value(
                    "subAgentsPerTurn",
                    "Sub-agents per turn",
                    "",
                    "10",
                    Source::Default,
                ),
                value(
                    "subAgentDepth",
                    "Sub-agent depth",
                    "Sub-agents spawning sub-agents.",
                    "2",
                    Source::Default,
                ),
                value(
                    "decisionCap",
                    "Classifier decisions per turn",
                    "",
                    "200",
                    Source::Global,
                ),
            ],
        ),
    ],
};

const GIT: Page = Page {
    title: "Git",
    desc: "How the desk reads and shows your repository. tofu itself never commits.",
    groups: &[
        (
            "Changes",
            &[
                switch(
                    "autoRefresh",
                    "Refresh on file and git events",
                    "",
                    true,
                    Source::Default,
                ),
                switch(
                    "untracked",
                    "Show untracked files",
                    "",
                    true,
                    Source::Default,
                ),
                switch(
                    "agentMarks",
                    "Mark agent changes",
                    "Violet marks on lines and rows an agent wrote this session.",
                    true,
                    Source::Default,
                ),
                switch(
                    "confirmDiscard",
                    "Confirm before discarding",
                    "",
                    true,
                    Source::Default,
                ),
            ],
        ),
        (
            "Blame",
            &[
                switch(
                    "gitBlame",
                    "Git blame on the current line",
                    "",
                    true,
                    Source::Default,
                ),
                switch(
                    "agentBlame",
                    "Agent blame on the current line",
                    "",
                    true,
                    Source::Default,
                ),
            ],
        ),
    ],
};

const APPEARANCE: Page = Page {
    title: "Appearance",
    desc: "",
    groups: &[(
        "Look",
        &[
            value("theme", "Theme", "", "tofu", Source::Default),
            value("density", "Density", "", "comfortable", Source::Default),
            value("animations", "Animations", "", "subtle", Source::Global),
            value(
                "glass",
                "Glass",
                "Frost everywhere; refraction where the GPU path allows it.",
                "frost",
                Source::Default,
            ),
        ],
    )],
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
