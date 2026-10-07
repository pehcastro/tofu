pub const OVERVIEW: &str = "ISESSION-1";
pub const FORKS: &str = "ISESSION-2";
pub const NAME: &str = "clear-sable-eagle";
pub const STARTED: &str = "started 13:10 · opus 5 on claude-sub personal · head of 3 sessions";
pub const RENAME: &str = "Renames the session. The three word name stays as its id.";
pub const COMPACT: &str =
    "Runs /compact: old tool results become handles the lead can reopen. No model call.";
pub const MENTION: &str = "Adds this turn to the chat as a reference the lead can read.";
pub const UNDO: &str = "Puts back the files this turn changed, from tofu's snapshot. Later turns that touched the same files are named first.";
pub const FORK_HERE: &str = "Starts a new session from this turn, with the context it had then. This session keeps running.";
pub const OPEN_WORK: &str = "Opens this turn in the work screen.";
pub const RIBBON_NOTE: &str = "the ribbon is the context: thicker means fuller, 250k at most";
pub const BRANCH_NOTE: &str =
    "3 uncommitted files, all from this session. tofu never commits; undo keeps its own snapshots.";
pub const PICK_NOTE: &str = "Click any dot on the ribbons to read that turn. Forking copies the context the session had at that turn into a new session.";

pub const STATS: [(&str, &str); 5] = [
    ("Turns", "14"),
    ("Steps", "212"),
    ("Sub-agents", "9"),
    ("Files changed", "17"),
    ("Tokens", "4.1M"),
];

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Outcome {
    Running,
    Stopped,
    LoopGuard,
}

pub struct Agent {
    pub name: &'static str,
    pub meta: &'static str,
    pub trace: &'static str,
}

pub struct Turn {
    pub at: &'static str,
    pub title: &'static str,
    pub meta: &'static str,
    pub outcome: Outcome,
    pub when: &'static str,
    pub summary: &'static str,
    pub agents: &'static [Agent],
    pub files: &'static [&'static str],
}

pub const TURNS: [Turn; 4] = [
    Turn {
        at: "16:38",
        title: "Add Count and the note badge",
        meta: "2 sub-agents · 4 files",
        outcome: Outcome::Running,
        when: "16:38 · 4m so far",
        summary: "go-dev added Count with a table test; ts-dev is writing the badge. The lead checked go-dev's work itself.",
        agents: &[
            Agent {
                name: "go-dev",
                meta: "done in 41s · checks passed",
                trace: "spawn#91be20",
            },
            Agent {
                name: "ts-dev",
                meta: "step 4 of 9",
                trace: "spawn#a3c1f4",
            },
        ],
        files: &["notes.go", "notes_test.go", "NoteList.tsx", "count.ts"],
    },
    Turn {
        at: "15:02",
        title: "Why does the list flicker on save",
        meta: "1 sub-agent · 1 file",
        outcome: Outcome::Stopped,
        when: "15:02 · 3m 10s",
        summary: "The list re-rendered on every keystroke because the store emitted on draft changes; now it emits only on save.",
        agents: &[Agent {
            name: "ts-dev",
            meta: "done · 1 file",
            trace: "spawn#77aa01",
        }],
        files: &["store.ts"],
    },
    Turn {
        at: "13:55",
        title: "Run the browser check on Save",
        meta: "browser · 2 checks held",
        outcome: Outcome::LoopGuard,
        when: "13:55 · stopped by the loop guard",
        summary: "Two checks held, then the lead repeated the same step and the loop guard stopped the turn.",
        agents: &[],
        files: &[],
    },
    Turn {
        at: "13:10",
        title: "Read the repo and plan",
        meta: "no edits",
        outcome: Outcome::Stopped,
        when: "13:10 · 1m 40s",
        summary: "Read the Go package, the web client and the migrations; wrote a three-step plan.",
        agents: &[Agent {
            name: "scout",
            meta: "done · 6 callers",
            trace: "spawn#12ce90",
        }],
        files: &[],
    },
];

pub struct Segment {
    pub name: &'static str,
    pub kind: &'static str,
    pub lane: usize,
    pub x0: f32,
    pub context: &'static [u16],
    pub turns: &'static [&'static str],
    pub what: &'static str,
    pub carried: &'static str,
    pub ended: &'static str,
    pub act: &'static str,
}

pub const LANES: [f32; 2] = [104.0, 206.0];
pub const STEP: f32 = 30.0;
pub const HEAD: usize = 2;

pub const SEGMENTS: [Segment; 4] = [
    Segment {
        name: "crisp-azure-swift",
        kind: "root",
        lane: 0,
        x0: 70.0,
        context: &[18, 30, 41, 52, 63, 74, 120, 180, 238],
        turns: &[
            "Plan the port of the store to SQLite",
            "Read every caller of Store",
            "Sketch the schema",
            "Ask about soft deletes",
            "Write the migration",
            "Try WAL mode",
            "Design the undo store",
            "Read the old tests",
            "Hand the plan over",
        ],
        what: "The first session. It planned the port and the undo store; two big test logs pushed the context up fast in the last three turns.",
        carried: "nothing, it is the root",
        ended: "auto fork at 238k, the fork target",
        act: "Resume read only",
    },
    Segment {
        name: "tidy-ochre-wren",
        kind: "auto fork",
        lane: 0,
        x0: 380.0,
        context: &[31, 78, 130, 181],
        turns: &[
            "Pick up the plan",
            "go-dev ports Store.All",
            "ts-dev updates the count badge",
            "Fix the sort order",
        ],
        what: "tofu forked when the context reached 238k. The summary and the open plan came across; raw tool output stayed behind as handles.",
        carried: "238k became 31k, a summary and the plan",
        ended: "/compact by you at 181k",
        act: "Resume read only",
    },
    Segment {
        name: "clear-sable-eagle",
        kind: "compact · head",
        lane: 0,
        x0: 540.0,
        context: &[12, 13, 13, 14, 15, 15, 16, 16, 17, 18, 18, 19, 19, 20],
        turns: &[
            "Continue after /compact",
            "Add the blank titles rule",
            "go-dev: Store.Search",
            "Review the diff",
            "Run the store tests",
            "Fix a flaky test",
            "ts-dev: the list view",
            "Ask about the deposit row",
            "Add the index",
            "Check the migration",
            "Port Store.Delete",
            "Check the undo snapshots",
            "Keep the API the same",
            "Store on SQLite, step 5 of 12",
        ],
        what: "You ran /compact: old tool results became handles the lead can reopen. It stays small because the sub-agents carry the work in their own context.",
        carried: "181k became 12k, no model call",
        ended: "running now",
        act: "Fork from here",
    },
    Segment {
        name: "brave-umber-lynx",
        kind: "your fork",
        lane: 1,
        x0: 270.0,
        context: &[74, 88, 97],
        turns: &[
            "Try SQLite in WAL mode",
            "Measure the writes",
            "Dropped: no gain",
        ],
        what: "You forked at turn 6 to try WAL mode on the side. It measured no gain and ended without changing a file.",
        carried: "a copy of turn 6, 74k",
        ended: "closed by you, no changes",
        act: "Resume",
    },
];
