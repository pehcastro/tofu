pub struct Palette {
    pub key: &'static str,
    pub name: &'static str,
    pub mode: &'static str,
    pub note: &'static str,
    pub window: &'static str,
    pub shell: &'static str,
    pub inner: &'static str,
    pub text: &'static str,
    pub muted: &'static str,
    pub accent: &'static str,
    pub live: &'static str,
    pub warn: &'static str,
    pub danger: &'static str,
    pub radius: u32,
    pub blur: u32,
    pub double: bool,
    pub file: Option<&'static str>,
}

pub const GLASS: &str = include_str!("../../../../../themes/tofu-glass.json");
const FLAT: &str = include_str!("../../../../../themes/tofu-flat.json");

pub const PALETTES: [Palette; 5] = [
    Palette {
        key: "glass",
        name: "Tofu Glass",
        mode: "dark",
        note: "the default: frosted window, a dark shell, a raised inner card",
        window: "rgba(19,18,24,.80)",
        shell: "rgba(7,6,10,.72)",
        inner: "rgba(255,255,255,.05)",
        text: "rgba(255,255,255,.92)",
        muted: "rgba(255,255,255,.38)",
        accent: "#9db8f0",
        live: "#86e0b3",
        warn: "#e8c98a",
        danger: "#f1737d",
        radius: 14,
        blur: 44,
        double: true,
        file: None,
    },
    Palette {
        key: "flat",
        name: "Tofu Flat",
        mode: "dark",
        note: "one surface: the inner card takes the shell's color",
        window: "rgba(19,18,24,.80)",
        shell: "rgba(24,23,30,.92)",
        inner: "rgba(24,23,30,.92)",
        text: "rgba(255,255,255,.92)",
        muted: "rgba(255,255,255,.38)",
        accent: "#9db8f0",
        live: "#86e0b3",
        warn: "#e8c98a",
        danger: "#f1737d",
        radius: 10,
        blur: 44,
        double: false,
        file: Some(FLAT),
    },
    Palette {
        key: "light",
        name: "Paper",
        mode: "light",
        note: "a light mode on warm paper; images keep their colors",
        window: "rgba(243,241,236,.88)",
        shell: "rgba(233,230,223,.9)",
        inner: "#ffffff",
        text: "#1d1c1a",
        muted: "rgba(29,28,26,.45)",
        accent: "#3b63c4",
        live: "#1f8a5a",
        warn: "#a86a12",
        danger: "#c23a44",
        radius: 14,
        blur: 30,
        double: true,
        file: None,
    },
    Palette {
        key: "midnight",
        name: "Midnight",
        mode: "dark",
        note: "a blue night tint over the same shapes",
        window: "rgba(8,11,26,.86)",
        shell: "rgba(4,7,20,.75)",
        inner: "rgba(120,150,255,.08)",
        text: "rgba(235,240,255,.92)",
        muted: "rgba(200,210,255,.4)",
        accent: "#8fb0ff",
        live: "#7fe0c3",
        warn: "#f0c98a",
        danger: "#ff8a9a",
        radius: 16,
        blur: 50,
        double: true,
        file: None,
    },
    Palette {
        key: "contrast",
        name: "High contrast",
        mode: "dark",
        note: "pure black, strong borders, brighter muted text",
        window: "#000000",
        shell: "#000000",
        inner: "#0b0b0b",
        text: "#ffffff",
        muted: "rgba(255,255,255,.62)",
        accent: "#a8c4ff",
        live: "#7dffb6",
        warn: "#ffd27a",
        danger: "#ff7a86",
        radius: 8,
        blur: 0,
        double: true,
        file: None,
    },
];

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Knob {
    OuterFill,
    OuterBorder,
    OuterRadius,
    Gap,
    InnerFill,
    InnerBorder,
    InnerRadius,
}

impl Knob {
    pub const ALL: [Knob; 7] = [
        Knob::OuterFill,
        Knob::OuterBorder,
        Knob::OuterRadius,
        Knob::Gap,
        Knob::InnerFill,
        Knob::InnerBorder,
        Knob::InnerRadius,
    ];

    pub fn name(self) -> &'static str {
        match self {
            Knob::OuterFill | Knob::InnerFill => "fill",
            Knob::OuterBorder | Knob::InnerBorder => "border",
            Knob::OuterRadius | Knob::InnerRadius => "radius",
            Knob::Gap => "gap to inner",
        }
    }

    pub fn options(self) -> &'static [&'static str] {
        match self {
            Knob::OuterFill => &["theme", "deep", "none"],
            Knob::OuterBorder => &["theme", "none", "hairline", "strong"],
            Knob::OuterRadius => &["theme", "8", "12", "18"],
            Knob::Gap => &["theme", "0", "3", "6"],
            Knob::InnerFill => &["theme", "raised", "same as outer", "none"],
            Knob::InnerBorder => &["theme", "none", "top light", "hairline", "strong"],
            Knob::InnerRadius => &["theme", "4", "8", "11", "16"],
        }
    }

    pub fn tokens(self, option: &str) -> &'static [(&'static str, &'static str)] {
        match (self, option) {
            (Knob::OuterFill, "deep") => &[("cards.outer.fill", "\"#040307e6\"")],
            (Knob::OuterFill, "none") => &[("cards.outer.fill", "\"#00000000\"")],
            (Knob::OuterBorder, "none") => &[("cards.outer.border", "\"none\"")],
            (Knob::OuterBorder, "hairline") => &[("cards.outer.border", "\"#ffffff14\"")],
            (Knob::OuterBorder, "strong") => &[("cards.outer.border", "\"#ffffff4d\"")],
            (Knob::OuterRadius, "8") => &[("cards.outer.radius", "8")],
            (Knob::OuterRadius, "12") => &[("cards.outer.radius", "12")],
            (Knob::OuterRadius, "18") => &[("cards.outer.radius", "18")],
            (Knob::Gap, "0") => &[("cards.outer.gap", "0")],
            (Knob::Gap, "3") => &[("cards.outer.gap", "3")],
            (Knob::Gap, "6") => &[("cards.outer.gap", "6")],
            (Knob::InnerFill, "raised") => &[
                ("cards.inner.fill", "\"#ffffff0f\""),
                ("cards.inner.fill_end", "\"#ffffff09\""),
            ],
            (Knob::InnerFill, "same as outer" | "none") => &[
                ("cards.inner.fill", "\"#00000000\""),
                ("cards.inner.fill_end", "\"#00000000\""),
            ],
            (Knob::InnerBorder, "none") => &[
                ("cards.inner.border", "\"none\""),
                ("cards.inner.highlight", "false"),
            ],
            (Knob::InnerBorder, "top light") => &[
                ("cards.inner.border", "\"none\""),
                ("cards.inner.highlight", "true"),
                ("cards.inner.sheen", "\"#ffffff14\""),
            ],
            (Knob::InnerBorder, "hairline") => &[("cards.inner.border", "\"#ffffff14\"")],
            (Knob::InnerBorder, "strong") => &[("cards.inner.border", "\"#ffffff47\"")],
            (Knob::InnerRadius, "4") => &[("cards.inner.radius", "4")],
            (Knob::InnerRadius, "8") => &[("cards.inner.radius", "8")],
            (Knob::InnerRadius, "11") => &[("cards.inner.radius", "11")],
            (Knob::InnerRadius, "16") => &[("cards.inner.radius", "16")],
            _ => &[],
        }
    }

    pub fn word(self, option: &str) -> &'static str {
        match option {
            "hairline" => "hair",
            "same as outer" => "same",
            "top light" => "top",
            _ => self
                .options()
                .iter()
                .copied()
                .find(|known| *known == option)
                .unwrap_or("theme"),
        }
    }
}

pub const AGENTS: [(&str, &str); 6] = [
    ("go-dev", "#79c0ff"),
    ("ts-dev", "#b9a6ea"),
    ("explore", "#8fd0aa"),
    ("research", "#e8c98a"),
    ("qa", "#f0a3b5"),
    ("browser", "#86d4d4"),
];

pub const GIT: [(&str, &str); 4] = [
    ("modified", "#deb04e"),
    ("added", "#52c68e"),
    ("deleted", "#f1737d"),
    ("untracked", "#66b0ec"),
];

pub struct Worker {
    pub letter: &'static str,
    pub name: &'static str,
    pub color: &'static str,
    pub task: &'static str,
    pub when: &'static str,
    pub waiting: bool,
}

pub const WORKERS: [Worker; 2] = [
    Worker {
        letter: "G",
        name: "go-dev 1",
        color: "#79c0ff",
        task: "Port Store.All to SQLite",
        when: "for 11m",
        waiting: false,
    },
    Worker {
        letter: "T",
        name: "ts-dev 2",
        color: "#b9a6ea",
        task: "Hide the badge at zero",
        when: "2m ago",
        waiting: true,
    },
];

pub const DUPLICATE_TELL: &str = "Copies this theme to ~/.tofu/themes/mine.json and opens it in the editor; saving the file restyles the desk live.";
pub const IMPORT_TELL: &str = "Imports a theme file: a .json with the same keys, or a VS Code or Zed theme converted on import.";
pub const TOKEN_TELL: &str = "Opens a color picker for this token; the window updates as you drag, and the change is written to your theme file.";
pub const THEMES_NOTE: &str = "Your files in ~/.tofu/themes/ appear here too. A theme can extend another and change only the tokens it names.";
