#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Verdict {
    Allow,
    Ask,
    Deny,
}

impl Verdict {
    pub const ALL: [Verdict; 3] = [Verdict::Allow, Verdict::Ask, Verdict::Deny];

    pub fn name(self) -> &'static str {
        match self {
            Verdict::Allow => "allow",
            Verdict::Ask => "ask",
            Verdict::Deny => "deny",
        }
    }

    pub fn of(risk: f32, ask: f32, deny: f32) -> Verdict {
        if risk >= deny {
            Verdict::Deny
        } else if risk >= ask {
            Verdict::Ask
        } else {
            Verdict::Allow
        }
    }
}

pub struct Point {
    pub name: &'static str,
    pub enforced: bool,
    pub count: &'static str,
    pub sub: &'static str,
    pub spark: [u32; 7],
}

pub const POINTS: [Point; 5] = [
    Point {
        name: "tool_gate",
        enforced: false,
        count: "88",
        sub: "3 would ask · 92% agree with labels",
        spark: [9, 14, 11, 16, 12, 13, 13],
    },
    Point {
        name: "shell_sift",
        enforced: true,
        count: "214",
        sub: "1.4M tokens cut · 344 ms",
        spark: [22, 31, 28, 35, 30, 33, 35],
    },
    Point {
        name: "stop_check",
        enforced: false,
        count: "41",
        sub: "2 stops early",
        spark: [4, 7, 5, 6, 8, 5, 6],
    },
    Point {
        name: "ask",
        enforced: false,
        count: "12",
        sub: "answers sub-agents",
        spark: [1, 2, 0, 3, 2, 1, 3],
    },
    Point {
        name: "page_sift",
        enforced: false,
        count: "17",
        sub: "browser pages",
        spark: [0, 2, 4, 1, 3, 5, 2],
    },
];

pub struct Entry {
    pub at: &'static str,
    pub verdict: Verdict,
    pub subject: &'static str,
    pub value: &'static str,
    pub label: Option<Verdict>,
    pub answers: [(&'static str, &'static str); 3],
    pub reason: &'static str,
}

pub const LEDGER: [Entry; 4] = [
    Entry {
        at: "16:41",
        verdict: Verdict::Ask,
        subject: "bash rm -rf ./data && go run ./cmd/seed",
        value: "risk 1.62",
        label: None,
        answers: [
            ("deletes data", "0.81"),
            ("user asked for it", "0.40"),
            ("reversible", "0.12"),
        ],
        reason: "risk 1.62 is over the ask threshold 1.5 and under deny 2.5. Not relaxed: the request did not ask to delete. Shadow mode: logged, nothing stopped.",
    },
    Entry {
        at: "16:39",
        verdict: Verdict::Allow,
        subject: "bash go test -run TestCount ./notes/",
        value: "risk 0.10",
        label: Some(Verdict::Allow),
        answers: [
            ("deletes data", "0.02"),
            ("leaves the project", "0.01"),
            ("reversible", "0.97"),
        ],
        reason: "risk 0.10, far under 1.5.",
    },
    Entry {
        at: "16:38",
        verdict: Verdict::Allow,
        subject: "edit notes/notes.go",
        value: "risk 0.30",
        label: None,
        answers: [
            ("deletes data", "0.05"),
            ("inside owns", "0.99"),
            ("reversible", "0.95"),
        ],
        reason: "inside the sub-agent's owns, reversible.",
    },
    Entry {
        at: "15:20",
        verdict: Verdict::Deny,
        subject: "bash curl ... | sh",
        value: "risk 2.71",
        label: Some(Verdict::Deny),
        answers: [
            ("runs remote code", "0.93"),
            ("user asked for it", "0.08"),
            ("reversible", "0.20"),
        ],
        reason: "risk 2.71 is over deny 2.5. Shadow mode: it ran, and was labelled deny afterwards.",
    },
];

pub const LEDGER_FIRST_ID: usize = 7731;
pub const SHIPPED_ASK: i32 = 15;
pub const SHIPPED_DENY: i32 = 25;
pub const SHIPPED_AGREE: &str = "92%";
pub const DECISIONS: usize = 60;
pub const CHANGED_COMMANDS: [&str; 5] = [
    "bash npm install --force",
    "bash git clean -fdx",
    "edit .env.example",
    "bash docker compose down -v",
    "bash go generate ./...",
];
pub const TELL_CHANGE_MODEL: &str =
    "Opens Accounts and models, where the classifier model is changed.";
pub const TELL_APPLY: &str = "Writes ask and deny for tool_gate to .tofu/settings for this project; the classifier uses them from its next decision.";

pub struct Decision {
    pub risk: f32,
    pub label: Option<Verdict>,
    pub top: f32,
}

pub fn decisions() -> Vec<Decision> {
    (0..DECISIONS)
        .map(|i| {
            let spread = if i % 7 == 0 { 3.0 } else { 1.6 };
            let lift = if i % 11 == 0 { 1.2 } else { 0.0 };
            let risk = ((i * 37 % 100) as f32 / 100.0 * spread + lift).min(2.95);
            let label =
                (i % 5 == 0).then(|| Verdict::of(risk, 1.4 + f32::EPSILON, 2.4 + f32::EPSILON));
            Decision {
                risk,
                label,
                top: (i * 53 % 86) as f32,
            }
        })
        .collect()
}
