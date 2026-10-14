pub const TITLE: &str = "tofu";
pub const TAGLINE: &str =
    "Code owns the loop. A lead plans, sub-agents write, a small classifier decides at every gate.";
pub const START: &str = "Get started";
pub const IMPORT: &str = "Already use tofu? Import ~/.tofu";

pub const TELL_IMPORT: &str = "Reads ~/.tofu from another machine or an older install: accounts, settings, themes and the library; keys are asked for again.";
pub const KEY_PLACEHOLDER: &str = "paste the key";

pub const STEPS: [&str; 4] = ["Welcome", "Models", "Classifier", "Project"];
pub const HINT: &str = "first run · everything here changes later in Settings";

pub const MODEL_STEP: &str = "language model";
pub const CLASSIFIER_STEP: &str = "classifier · jev, required";
pub const LLM_ROLE: &str = "llm";
pub const CLAUDE_SOURCE: &str = "claude-sub";
pub const CODEX_SOURCE: &str = "codex-sub";
pub const KEY_PROVIDERS: [(&str, &str); 3] = [
    ("META_MUSE_API_KEY", "meta"),
    ("OPENROUTER_KEY", "openrouter"),
    ("TYPESAFE_API_KEY", "typesafe"),
];
