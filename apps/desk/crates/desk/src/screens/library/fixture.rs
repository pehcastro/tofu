#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Mode {
    Shadow,
    Enforced,
    Off,
}

impl Mode {
    pub const ALL: [Mode; 3] = [Mode::Shadow, Mode::Enforced, Mode::Off];

    pub fn name(self) -> &'static str {
        match self {
            Mode::Shadow => "shadow",
            Mode::Enforced => "enforced",
            Mode::Off => "off",
        }
    }
}

pub struct Rule {
    pub id: &'static str,
    pub text: &'static str,
    pub mode: Mode,
    pub layer: &'static str,
    pub fires: u32,
    pub kind: &'static str,
    pub trigger: &'static str,
    pub file: &'static str,
    pub stale: bool,
    pub week: [u32; 7],
}

pub const BUILT_IN: &str = "built in";

pub const RULES: [Rule; 6] = [
    Rule {
        id: "go/table_tests@1",
        text: "Write Go tests as tables; one case per row.",
        mode: Mode::Shadow,
        layer: BUILT_IN,
        fires: 14,
        kind: "structural",
        trigger: "language go, task write",
        file: "library/dev/go/rules/table_tests@1.yaml",
        stale: false,
        week: [1, 3, 2, 0, 4, 1, 3],
    },
    Rule {
        id: "general/verify_sub_agents@1",
        text: "Check a sub-agent's work before reporting.",
        mode: Mode::Off,
        layer: BUILT_IN,
        fires: 0,
        kind: "decision",
        trigger: "role orchestrator",
        file: "library/general/rules/verify_sub_agents@1.yaml",
        stale: false,
        week: [0, 0, 0, 0, 0, 0, 0],
    },
    Rule {
        id: "notes/blank_titles@1",
        text: "Blank titles are kept but never counted.",
        mode: Mode::Enforced,
        layer: "this project",
        fires: 6,
        kind: "human",
        trigger: "always",
        file: ".tofu/rules/notes@1.yaml",
        stale: false,
        week: [0, 1, 0, 2, 1, 0, 2],
    },
    Rule {
        id: "ts/no_any@2",
        text: "No explicit any in TypeScript.",
        mode: Mode::Enforced,
        layer: "global",
        fires: 9,
        kind: "structural",
        trigger: "language typescript",
        file: "~/.tofu/rules/no_any@1.yaml",
        stale: true,
        week: [2, 1, 3, 0, 1, 2, 0],
    },
    Rule {
        id: "general/no_force_push@1",
        text: "Never force-push.",
        mode: Mode::Enforced,
        layer: BUILT_IN,
        fires: 1,
        kind: "structural",
        trigger: "always",
        file: "library/general/rules/no_force_push@1.yaml",
        stale: false,
        week: [0, 0, 0, 1, 0, 0, 0],
    },
    Rule {
        id: "qa/browser_check@1",
        text: "Check visible changes in a browser.",
        mode: Mode::Shadow,
        layer: BUILT_IN,
        fires: 3,
        kind: "decision",
        trigger: "a web file changed",
        file: "library/qa/rules/browser_check@1.yaml",
        stale: false,
        week: [0, 1, 0, 0, 1, 0, 1],
    },
];

pub const RECENT: [(&str, &str, &str); 2] = [
    ("16:39", "notes/notes_test.go", "1 finding · shadow"),
    ("15:02", "web/count.test.ts", "0 findings"),
];

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Found {
    Offered,
    Shadowed,
    Undescribed,
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Dot {
    Live,
    Grey,
    Warn,
}

pub struct Eval {
    pub followed: &'static str,
    pub cost: &'static str,
    pub passed: &'static str,
}

pub struct Skill {
    pub name: &'static str,
    pub desc: Option<&'static str>,
    pub place: &'static str,
    pub found: Found,
    pub dot: Dot,
    pub eval: Option<Eval>,
}

pub const SKILLS: [Skill; 5] = [
    Skill {
        name: "migrations",
        desc: Some("Write and run a database migration."),
        place: ".tofu/skills/migrations",
        found: Found::Offered,
        dot: Dot::Live,
        eval: Some(Eval {
            followed: "2 of 3",
            cost: "247 tokens",
            passed: "5 vs 3",
        }),
    },
    Skill {
        name: "release",
        desc: Some("Cut a version: changelog, tag, build."),
        place: ".agents/skills/release",
        found: Found::Offered,
        dot: Dot::Live,
        eval: None,
    },
    Skill {
        name: "review",
        desc: Some("Review a diff against the brief."),
        place: "~/.tofu/skills/review",
        found: Found::Offered,
        dot: Dot::Live,
        eval: Some(Eval {
            followed: "3 of 3",
            cost: "190 tokens",
            passed: "4 vs 4",
        }),
    },
    Skill {
        name: "release",
        desc: Some("An older copy of release."),
        place: ".claude/skills/release",
        found: Found::Shadowed,
        dot: Dot::Grey,
        eval: None,
    },
    Skill {
        name: "scratch",
        desc: None,
        place: ".tofu/skills/scratch",
        found: Found::Undescribed,
        dot: Dot::Warn,
        eval: None,
    },
];

pub const EVAL_RAN: Eval = Eval {
    followed: "3 of 3",
    cost: "251 tokens",
    passed: "5 vs 3",
};
pub const EVAL_WHEN: &str = "Sep 26 · bench/skills";
pub const EVAL_JUST_RAN: &str = "just now · 3 tasks, with and without the skill";
pub const EVAL_NEVER: &str = "never run";
pub const STEPS: [&str; 3] = [
    "1. Read the migration folder and the current schema.",
    "2. Write the next numbered migration; never edit an applied one.",
    "3. Run it against the dev database, then the tests.",
];

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Tier {
    Inherit,
    Worker,
    Smart,
    Pinned,
}

impl Tier {
    pub const ALL: [Tier; 4] = [Tier::Inherit, Tier::Worker, Tier::Smart, Tier::Pinned];

    pub fn name(self) -> &'static str {
        match self {
            Tier::Inherit => "inherit",
            Tier::Worker => "@worker",
            Tier::Smart => "@smart",
            Tier::Pinned => "pinned",
        }
    }

    pub fn runs_on(self) -> &'static str {
        match self {
            Tier::Inherit => "claude-sub/claude-opus-5 (the lead's)",
            Tier::Worker => "claude-sub/claude-haiku-5",
            Tier::Smart => "codex-sub/gpt-5.6-sol",
            Tier::Pinned => "claude-sub/claude-sonnet-5",
        }
    }
}

pub struct Agent {
    pub name: &'static str,
    pub desc: &'static str,
    pub origin: &'static str,
    pub tier: Tier,
    pub gate: &'static str,
    pub tools: &'static str,
    pub refs: &'static str,
    pub path: &'static str,
    pub runs: &'static str,
    pub first: &'static str,
    pub steps: &'static str,
}

pub const AGENTS: [Agent; 8] = [
    Agent {
        name: "go-dev",
        desc: "Go work: write, edit, test.",
        origin: BUILT_IN,
        tier: Tier::Inherit,
        gate: "vet, test",
        tools: "read, glob, search, symbols, edit, write, bash",
        refs: "4 Go references",
        path: "library/dev/agents/go-dev.md",
        runs: "12",
        first: "83%",
        steps: "9",
    },
    Agent {
        name: "ts-dev",
        desc: "TypeScript and frontend work.",
        origin: BUILT_IN,
        tier: Tier::Inherit,
        gate: "tsc, eslint",
        tools: "read, glob, search, edit, write, bash",
        refs: "5 references",
        path: "library/dev/agents/ts-dev.md",
        runs: "9",
        first: "71%",
        steps: "11",
    },
    Agent {
        name: "py-dev",
        desc: "Python work.",
        origin: BUILT_IN,
        tier: Tier::Inherit,
        gate: "ruff, pytest",
        tools: "read, glob, search, edit, write, bash",
        refs: "3 references",
        path: "library/dev/agents/py-dev.md",
        runs: "0",
        first: "-",
        steps: "-",
    },
    Agent {
        name: "rust-dev",
        desc: "Rust work.",
        origin: BUILT_IN,
        tier: Tier::Inherit,
        gate: "check, test",
        tools: "read, glob, search, edit, write, bash",
        refs: "3 references",
        path: "library/dev/agents/rust-dev.md",
        runs: "0",
        first: "-",
        steps: "-",
    },
    Agent {
        name: "research",
        desc: "Reads and reports, never edits.",
        origin: BUILT_IN,
        tier: Tier::Worker,
        gate: "none",
        tools: "read, glob, search, fetch",
        refs: "contract, research report",
        path: "library/general/agents/research.md",
        runs: "6",
        first: "-",
        steps: "7",
    },
    Agent {
        name: "browser",
        desc: "Drives the browser through the relay.",
        origin: BUILT_IN,
        tier: Tier::Inherit,
        gate: "none",
        tools: "browser_do",
        refs: "recipes",
        path: "library/general/agents/browser.md",
        runs: "3",
        first: "-",
        steps: "6",
    },
    Agent {
        name: "qa",
        desc: "Checks a change as a person would.",
        origin: BUILT_IN,
        tier: Tier::Smart,
        gate: "none",
        tools: "read, bash, browser_do",
        refs: "2 references",
        path: "library/qa/agents/qa.md",
        runs: "1",
        first: "-",
        steps: "8",
    },
    Agent {
        name: "scout",
        desc: "Finds every caller before a change.",
        origin: "this project",
        tier: Tier::Worker,
        gate: "none",
        tools: "read, glob, search, symbols",
        refs: "none",
        path: ".tofu/agents/scout.md",
        runs: "4",
        first: "-",
        steps: "4",
    },
];

pub const TELL_NEW_RULE: &str = "Opens a new rule in the editor at .tofu/rules/, from a template: id, text, kind, when it fires. It counts once saved.";
pub const TELL_SAVE_OVERRIDE: &str = "Writes the override with its reason to .tofu/rules/overrides.yaml; the rule shows as off here and in tofu doctor.";
pub const TELL_EDIT_RULE: &str = "Copies the rule into .tofu/rules/ and opens it in the editor tab; your copy wins over the built in one.";
pub const TELL_RUN_CHECK: &str = "Runs this rule over the files changed in this session now, without a model call, and lists the findings under Recent fires.";
pub const TELL_WHICH_RULES: &str = "Asks for a task in one line and lists the rules that would reach the writer for it, with the reason each one matched.";
pub const TELL_NEW_AGENT: &str = "Opens a new agent file in the editor at .tofu/agents/, from a template: name, tier, tools, the rules it reads.";
pub const TELL_OPEN_FILE: &str = "Opens this agent's file in the editor tab.";
pub const TELL_DISABLE: &str = "Stops the lead from spawning this agent in this project; written to .tofu/settings, undone the same way.";
