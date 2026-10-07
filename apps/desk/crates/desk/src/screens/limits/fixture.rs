pub const BOARD: &str = "ILIMITS-1";
pub const HOURS: f32 = 12.0;
pub const PROJECTED_UNTIL: f32 = 5.83;
pub const SWITCHING_ORDER: &str = "Opens the order tofu picks accounts in, per role: lead, each tier, the classifier. Saved in Settings, Accounts and models.";
pub const OPEN_USAGE: &str = "Opens Usage: tokens by source, session and who spent them.";
pub const AWAY: &str = "after 23:30 you are usually away, so no projection";
pub const NOW: &str = "now 17:40";
pub const AXIS: [(f32, &str); 5] = [
    (2.0, "20:00"),
    (4.0, "22:00"),
    (6.0, "00:00"),
    (8.0, "02:00"),
    (10.0, "04:00"),
];

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Tone {
    Live,
    Warn,
    Accent,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Lane {
    Serve,
    Spent,
    Idle,
    Light,
}

pub struct Quota {
    pub percent: Option<u8>,
    pub reset: &'static str,
    pub projection: &'static str,
    pub warn: bool,
}

pub struct Segment {
    pub lane: Lane,
    pub from: f32,
    pub to: f32,
    pub text: &'static str,
}

pub struct Account {
    pub source: &'static str,
    pub name: &'static str,
    pub plan: &'static str,
    pub state: &'static str,
    pub tone: Tone,
    pub windows: [Quota; 3],
    pub spend: &'static [(&'static str, u8)],
    pub pace: &'static str,
    pub lane: &'static [Segment],
    pub ticks: &'static [f32],
    pub away: bool,
}

const fn window(percent: u8, reset: &'static str, projection: &'static str, warn: bool) -> Quota {
    Quota {
        percent: Some(percent),
        reset,
        projection,
        warn,
    }
}

const fn segment(lane: Lane, from: f32, to: f32, text: &'static str) -> Segment {
    Segment {
        lane,
        from,
        to,
        text,
    }
}

pub const ACCOUNTS: [Account; 3] = [
    Account {
        source: "claude-sub",
        name: "personal",
        plan: "Max",
        state: "serving the lead",
        tone: Tone::Live,
        windows: [
            window(34, "resets 19:50", "full at 19:30", true),
            window(12, "resets Fri", "fine", false),
            window(9, "Opus · resets Fri", "fine", false),
        ],
        spend: &[
            ("lead", 41),
            ("go-dev", 33),
            ("ts-dev", 18),
            ("research", 6),
            ("compaction", 2),
        ],
        pace: "36% an hour, 3 sub-agents running",
        lane: &[
            segment(
                Lane::Serve,
                0.0,
                1.83,
                "serving · 34% now, full about 19:30",
            ),
            segment(Lane::Spent, 1.83, 2.17, ""),
            segment(Lane::Idle, 2.17, 4.62, "standby"),
            segment(Lane::Serve, 4.62, 5.83, "serving"),
        ],
        ticks: &[2.17],
        away: true,
    },
    Account {
        source: "claude-sub",
        name: "work",
        plan: "Max",
        state: "back at 18:20",
        tone: Tone::Warn,
        windows: [
            window(100, "resets 18:20", "spent", true),
            window(57, "resets Tue", "fine", false),
            window(40, "Opus · resets Tue", "fine", false),
        ],
        spend: &[("lead", 52), ("go-dev", 30), ("qa", 12), ("compaction", 6)],
        pace: "idle until tofu moves here",
        lane: &[
            segment(Lane::Spent, 0.0, 0.67, "spent"),
            segment(Lane::Idle, 0.67, 1.83, "standby"),
            segment(Lane::Serve, 1.83, 4.62, "serving · full about 22:17"),
            segment(Lane::Spent, 4.62, 5.83, ""),
        ],
        ticks: &[0.67],
        away: false,
    },
    Account {
        source: "codex-sub",
        name: "personal",
        plan: "Pro",
        state: "serving @smart",
        tone: Tone::Accent,
        windows: [
            window(8, "5h · resets 21:42", "fine", false),
            window(3, "week · resets Mon", "fine", false),
            Quota {
                percent: None,
                reset: "no per model window",
                projection: "",
                warn: false,
            },
        ],
        spend: &[("ts-dev", 71), ("research", 29)],
        pace: "5% an hour, sub-agents on @smart",
        lane: &[segment(
            Lane::Light,
            0.0,
            5.83,
            "serving @smart sub-agents · 8%",
        )],
        ticks: &[4.03],
        away: false,
    },
];
