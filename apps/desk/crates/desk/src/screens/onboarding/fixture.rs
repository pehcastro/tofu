pub const TITLE: &str = "tofu";
pub const TAGLINE: &str =
    "Code owns the loop. A lead plans, sub-agents write, a small classifier decides at every gate.";
pub const START: &str = "Get started";
pub const IMPORT: &str = "Already use tofu? Import ~/.tofu";

pub const TELL_START: &str = "Opens the setup: Models, Classifier and Project as connected tabs.";
pub const TELL_IMPORT: &str = "Reads ~/.tofu from another machine or an older install: accounts, settings, themes and the library; keys are asked for again.";
pub const TELL_CHECK: &str =
    "Checks the key with one free call and stores it for the classifier only.";
pub const TELL_FOLDER: &str = "Opens the system folder picker.";
pub const TELL_OPEN: &str = "Opens the home screen on the picked project.";

pub const STEPS: [&str; 4] = ["Welcome", "Models", "Classifier", "Project"];
pub const HINT: &str = "first run · everything here changes later in Settings";
pub const PROVIDERS: [&str; 4] = ["anthropic", "openai", "meta", "a gateway"];

pub struct Choice {
    pub title: &'static str,
    pub sub: &'static str,
    pub needs_key: bool,
}

pub const CLASSIFIERS: [Choice; 4] = [
    Choice {
        title: "jev through OpenRouter",
        sub: "recommended · add an OpenRouter key · about 0.03 USD a day",
        needs_key: true,
    },
    Choice {
        title: "jev through typesafe",
        sub: "the same model, billed by typesafe directly",
        needs_key: true,
    },
    Choice {
        title: "One of your models",
        sub: "runs on your subscription; slower, and every gate costs a request",
        needs_key: false,
    },
    Choice {
        title: "None for now",
        sub: "no gate decides alone: every risky command asks you",
        needs_key: false,
    },
];

pub struct Project {
    pub name: &'static str,
    pub path: &'static str,
    pub found: [&'static str; 3],
}

pub const PROJECTS: [Project; 3] = [
    Project {
        name: "notes-app",
        path: "F:/code/notes-app",
        found: [
            "git main · 3 changed",
            "3 sessions found",
            "rules in .tofu/rules · AGENTS.md",
        ],
    },
    Project {
        name: "tofu",
        path: "F:/localhost/ephem-sh/tofu",
        found: [
            "git develop · clean",
            "123 sessions found",
            "rules in .tofu/rules · CLAUDE.md",
        ],
    },
    Project {
        name: "bob",
        path: "F:/localhost/ephem-sh/bob",
        found: ["git main · clean", "1 session found", "no rules yet"],
    },
];
