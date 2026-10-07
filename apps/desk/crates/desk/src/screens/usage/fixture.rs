#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Measure {
    Tokens,
    Requests,
    TurnTime,
}

impl Measure {
    pub const ALL: [(Measure, &'static str); 3] = [
        (Measure::Tokens, "Tokens"),
        (Measure::Requests, "Requests"),
        (Measure::TurnTime, "Turn time"),
    ];

    pub fn seed(self) -> [u8; 14] {
        match self {
            Measure::Tokens => [3, 4, 5, 4, 6, 7, 5, 8, 9, 7, 6, 8, 10, 9],
            Measure::Requests => [2, 3, 3, 4, 5, 4, 4, 6, 7, 6, 5, 7, 8, 7],
            Measure::TurnTime => [1, 2, 3, 2, 4, 5, 3, 5, 6, 4, 4, 5, 6, 5],
        }
    }

    pub fn headline(self) -> [&'static str; 3] {
        match self {
            Measure::Tokens => ["6.8M", "tokens", "+18% on last week"],
            Measure::Requests => ["1,284", "requests", "+9%"],
            Measure::TurnTime => ["11h 20m", "of turns, 4h waiting on providers", "-6%"],
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Range {
    Today,
    Week,
    Month,
}

impl Range {
    pub const ALL: [(Range, &'static str); 3] = [
        (Range::Today, "Today"),
        (Range::Week, "7 days"),
        (Range::Month, "30 days"),
    ];

    pub fn columns(self) -> usize {
        match self {
            Range::Today => 24,
            Range::Week => 14,
            Range::Month => 30,
        }
    }

    pub fn text(self) -> &'static str {
        match self {
            Range::Today => "today, by hour",
            Range::Week => "last 7 days, twice a day",
            Range::Month => "last 30 days",
        }
    }

    pub fn start(self) -> &'static str {
        match self {
            Range::Today => "00:00",
            Range::Week => "Sep 29",
            Range::Month => "Sep 6",
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Scope {
    ThisProject,
    AllProjects,
}

impl Scope {
    pub const ALL: [(Scope, &'static str); 2] = [
        (Scope::ThisProject, "This project"),
        (Scope::AllProjects, "All projects"),
    ];

    pub fn share(self) -> f32 {
        match self {
            Scope::ThisProject => 0.8,
            Scope::AllProjects => 1.0,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Source {
    Claude,
    Codex,
    Meta,
}

impl Source {
    pub fn share(filter: Option<Source>) -> f32 {
        match filter {
            None | Some(Source::Claude) => 1.0,
            Some(Source::Codex) => 0.3,
            Some(Source::Meta) => 0.1,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Swatch {
    White,
    Lead,
    Faint,
    Accent,
    Trace,
    Live,
}

pub struct SourceRow {
    pub source: Source,
    pub swatch: Swatch,
    pub name: &'static str,
    pub paid: &'static str,
    pub tokens: &'static str,
}

pub const SOURCES: [SourceRow; 3] = [
    SourceRow {
        source: Source::Claude,
        swatch: Swatch::White,
        name: "claude-sub",
        paid: "quota",
        tokens: "5.6M",
    },
    SourceRow {
        source: Source::Codex,
        swatch: Swatch::Faint,
        name: "codex-sub",
        paid: "quota",
        tokens: "1.0M",
    },
    SourceRow {
        source: Source::Meta,
        swatch: Swatch::Accent,
        name: "meta · your key",
        paid: "0.42 USD",
        tokens: "0.2M",
    },
];

pub const SOURCES_NOTE: &str = "Subscriptions are quota, not money; only key-paid sources cost.";

pub struct Spender {
    pub who: &'static str,
    pub tokens: &'static str,
    pub share: f32,
    pub swatch: Swatch,
    pub note: Option<&'static str>,
}

pub const SPENDERS: [Spender; 3] = [
    Spender {
        who: "Lead",
        tokens: "3.1M",
        share: 0.46,
        swatch: Swatch::Lead,
        note: Some("planning, checking sub-agents, answering their questions"),
    },
    Spender {
        who: "Sub-agents",
        tokens: "3.0M",
        share: 0.44,
        swatch: Swatch::Trace,
        note: Some("go-dev 41%, ts-dev 38%, scout 21%"),
    },
    Spender {
        who: "Classifier",
        tokens: "0.6M",
        share: 0.10,
        swatch: Swatch::Live,
        note: None,
    },
];

pub const SIFTED: &str = "1.4M";
pub const SIFTED_NOTE: &str = "tokens of shell output the model never read, cut by sift";

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Outcome {
    Live,
    Idle,
    LoopGuard,
}

pub struct SessionRow {
    pub name: &'static str,
    pub outcome: Outcome,
    pub turns: &'static str,
    pub tokens: &'static str,
    pub tell: &'static str,
}

pub const SESSIONS: [SessionRow; 3] = [
    SessionRow {
        name: "clear-sable-eagle",
        outcome: Outcome::Live,
        turns: "14 turns",
        tokens: "4.1M",
        tell: "Opens clear-sable-eagle on the Session screen.",
    },
    SessionRow {
        name: "fond-sandy-mink",
        outcome: Outcome::Idle,
        turns: "6 turns",
        tokens: "1.6M",
        tell: "Opens fond-sandy-mink on the Session screen, read only.",
    },
    SessionRow {
        name: "quiet-amber-heron",
        outcome: Outcome::LoopGuard,
        turns: "loop guard",
        tokens: "1.1M",
        tell: "Opens quiet-amber-heron on the Session screen; the loop guard stopped a run that repeated itself.",
    },
];

pub const PROJECT: &str = "notes-app";
pub const BOARD: &str = "IUSAGE-1";
