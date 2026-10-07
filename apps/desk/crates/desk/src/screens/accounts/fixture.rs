#[derive(Clone, Copy, PartialEq, Eq)]
pub enum State {
    InUse,
    Away,
    Standby,
}

impl State {
    pub fn label(self) -> &'static str {
        match self {
            State::InUse => "in use",
            State::Away => "back in 40m",
            State::Standby => "standby",
        }
    }
}

pub struct Account {
    pub source: &'static str,
    pub name: &'static str,
    pub kind: &'static str,
    pub state: State,
}

pub const ACCOUNTS: [Account; 6] = [
    Account {
        source: "claude-sub",
        name: "personal",
        kind: "subscription \u{b7} Max",
        state: State::InUse,
    },
    Account {
        source: "claude-sub",
        name: "work",
        kind: "subscription \u{b7} Max",
        state: State::Away,
    },
    Account {
        source: "codex-sub",
        name: "personal",
        kind: "subscription \u{b7} Pro",
        state: State::Standby,
    },
    Account {
        source: "meta",
        name: "key \u{b7}\u{b7}\u{b7}\u{b7}3f1a",
        kind: "key, priced per token",
        state: State::Standby,
    },
    Account {
        source: "openrouter",
        name: "key \u{b7}\u{b7}\u{b7}\u{b7}88c0",
        kind: "classifier only",
        state: State::InUse,
    },
    Account {
        source: "github",
        name: "pehcastro",
        kind: "from gh \u{b7} read only",
        state: State::InUse,
    },
];

pub const MODELS: [&str; 5] = [
    "claude-sub/claude-opus-5",
    "claude-sub/claude-sonnet-5",
    "claude-sub/claude-haiku-5",
    "codex-sub/gpt-5.6-sol",
    "meta/muse-2",
];

pub const CLASSIFIERS: [&str; 4] = [
    "openrouter/~typesafe/jev-latest",
    "openrouter/~typesafe/jev-mini",
    "meta/muse-2-lite",
    "claude-sub/claude-haiku-5",
];

pub struct Role {
    pub role: &'static str,
    pub desc: &'static str,
    pub options: &'static [&'static str],
    pub start: usize,
}

pub const ROLES: [Role; 6] = [
    Role {
        role: "Lead",
        desc: "the orchestrator, the one you talk to",
        options: &MODELS,
        start: 0,
    },
    Role {
        role: "@genius tier",
        desc: "sub-agents that need the most",
        options: &MODELS,
        start: 0,
    },
    Role {
        role: "@smart tier",
        desc: "most sub-agent work",
        options: &MODELS,
        start: 3,
    },
    Role {
        role: "@worker tier",
        desc: "reading, searching, routine edits",
        options: &MODELS,
        start: 2,
    },
    Role {
        role: "@dumb tier",
        desc: "trivial jobs",
        options: &MODELS,
        start: 2,
    },
    Role {
        role: "Classifier",
        desc: "decides at every gate; jev is one choice",
        options: &CLASSIFIERS,
        start: 0,
    },
];

pub const ACCOUNTS_NOTE: &str = "The GitHub account came from gh, the commit identity from git config; neither is stored by tofu.";
pub const ROLES_NOTE: &str = "A model is always source/model: the source is what pays. The same model on a subscription and on a key are two different rows.";
pub const MORE_TELL: &str = "Rename, set aside, sign in again, or remove this account.";

pub struct AddGroup {
    pub caption: &'static str,
    pub sources: &'static [(&'static str, &'static str)],
}

pub const ADD_GROUPS: [AddGroup; 3] = [
    AddGroup {
        caption: "Subscription, signs in through the browser",
        sources: &[
            (
                "claude-sub",
                "Signs in with your Claude subscription in the browser; tofu keeps the token in agent.db.",
            ),
            (
                "codex-sub",
                "Signs in with your ChatGPT subscription in the browser; tofu keeps the token in agent.db.",
            ),
        ],
    },
    AddGroup {
        caption: "Key, paid per token",
        sources: &[
            (
                "meta",
                "Asks for a Meta API key; paid per token, priced from the catalog.",
            ),
            (
                "anthropic",
                "Asks for an Anthropic API key; paid per token.",
            ),
            ("openai", "Asks for an OpenAI API key; paid per token."),
            (
                "a gateway",
                "Asks for a base URL and a key for any OpenAI compatible gateway.",
            ),
        ],
    },
    AddGroup {
        caption: "For the classifier and search",
        sources: &[
            (
                "openrouter",
                "Asks for an OpenRouter key; used by the classifier unless you pick another source.",
            ),
            (
                "typesafe",
                "Asks for a typesafe key to reach the jev classifier models directly.",
            ),
            (
                "brave search",
                "Asks for a Brave Search key for the web search tool.",
            ),
        ],
    },
];
