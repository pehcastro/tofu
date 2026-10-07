pub const PROJECT: &str = "notes-app";
pub const PROJECT_INITIAL: &str = "n";
pub const PROJECT_LINE: &str = "main · 3 changed";
pub const SESSION: &str = "clear-sable-eagle";
pub const ACCOUNT_INITIAL: &str = "p";

pub struct Session {
    pub name: &'static str,
    pub state: &'static str,
    pub on: bool,
}

pub const RUNNING: [Session; 2] = [
    Session {
        name: "clear-sable-eagle",
        state: "working",
        on: true,
    },
    Session {
        name: "quiet-amber-heron",
        state: "loop 10m",
        on: false,
    },
];
pub const INACTIVE: &str = "3";

pub const TABS: [&str; 3] = ["work", "editor", "data"];

pub enum Kind {
    Go,
    Ts,
    Explore,
    Research,
    Browser,
    Py,
    Qa,
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum State {
    Working,
    Asking,
    Failed,
}

pub struct Agent {
    pub kind: Kind,
    pub name: &'static str,
    pub doing: &'static str,
    pub now: &'static str,
    pub time: &'static str,
    pub state: State,
}

const fn agent(
    kind: Kind,
    name: &'static str,
    doing: &'static str,
    now: &'static str,
    time: &'static str,
    state: State,
) -> Agent {
    Agent {
        kind,
        name,
        doing,
        now,
        time,
        state,
    }
}

pub const GROUPS: [(State, &str, &str); 3] = [
    (State::Working, "Working", "12"),
    (State::Asking, "Asking the lead", "3"),
    (State::Failed, "Failed", "3"),
];

pub const AGENTS: [Agent; 18] = [
    agent(
        Kind::Go,
        "go-dev 1",
        "Port Store.All to SQLite",
        "editing store/sqlite.go",
        "for 11m",
        State::Working,
    ),
    agent(
        Kind::Go,
        "go-dev 3",
        "Move undo snapshots into the store",
        "editing store/sqlite.go",
        "for 25m",
        State::Working,
    ),
    agent(
        Kind::Go,
        "go-dev 7",
        "Add the soft delete migration",
        "editing store/sqlite.go",
        "for 15m",
        State::Working,
    ),
    agent(
        Kind::Ts,
        "ts-dev 3",
        "Move NoteList to TanStack Query",
        "editing web/Header.tsx",
        "for 5m",
        State::Working,
    ),
    agent(
        Kind::Ts,
        "ts-dev 5",
        "Keyboard shortcuts for delete",
        "editing web/Header.tsx",
        "for 19m",
        State::Working,
    ),
    agent(
        Kind::Explore,
        "explore 2",
        "Find where notes are sorted",
        "grep Store.All( in notes/",
        "for 33m",
        State::Working,
    ),
    agent(
        Kind::Explore,
        "explore 6",
        "Find tests that touch the clock",
        "grep Store.All( in notes/",
        "for 23m",
        State::Working,
    ),
    agent(
        Kind::Explore,
        "explore 10",
        "Find dead exports in web/",
        "grep Store.All( in notes/",
        "for 13m",
        State::Working,
    ),
    agent(
        Kind::Research,
        "research 2",
        "SQLite WAL mode for one writer",
        "reading sqlite.org/wal.html",
        "for 27m",
        State::Working,
    ),
    agent(
        Kind::Research,
        "research 4",
        "Soft delete patterns in SQLite",
        "reading sqlite.org/wal.html",
        "for 41m",
        State::Working,
    ),
    agent(
        Kind::Browser,
        "browser 1",
        "Read the badge on :5871 after a delete",
        "clicking Delete on row 2",
        "for 31m",
        State::Working,
    ),
    agent(
        Kind::Py,
        "py-dev 3",
        "Script the SQLite backup",
        "running scripts/seed.py",
        "for 21m",
        State::Working,
    ),
    agent(
        Kind::Go,
        "go-dev 5",
        "Fix the flaky TestDelete",
        "asked: Keep the JSON store as a read only fallback for one release?",
        "29m ago",
        State::Asking,
    ),
    agent(
        Kind::Explore,
        "explore 4",
        "List the migrations",
        "asked: Which env file wins, .env or .env.local?",
        "7m ago",
        State::Asking,
    ),
    agent(
        Kind::Qa,
        "qa 2",
        "Check the badge in the browser",
        "asked: Run the e2e suite against SQLite or the JSON store?",
        "6m ago",
        State::Asking,
    ),
    agent(
        Kind::Ts,
        "ts-dev 1",
        "Wire the count into the badge",
        "exited 1",
        "16m ago",
        State::Failed,
    ),
    agent(
        Kind::Explore,
        "explore 8",
        "Find every place the badge renders",
        "stopped: the search ran past its budget",
        "32m ago",
        State::Failed,
    ),
    agent(
        Kind::Py,
        "py-dev 1",
        "Export notes to CSV",
        "exited 2",
        "35m ago",
        State::Failed,
    ),
];

pub const TABLE_HEAD: [&str; 3] = ["agent", "doing", "time"];
pub const STACK_BADGES: [&str; 3] = ["18 live", "20", "6"];

pub const YOU_TIME: &str = "15:02";
pub const YOU_SAID: &str = "Port the store to SQLite and keep the API the same. The web client should show the note count. Use as many agents as it takes.";
pub const TOOLS: (&str, &str) = ("9 tools", "· 12s");
pub const LEAD_FIRST: (&str, &str) = (
    "15:03",
    "A big one, so I split it: explore maps the code first, go-dev ports the store in parallel slices, ts-dev takes the web client, research answers the open questions, qa and browser check the result, py-dev updates the scripts.",
);
pub const FLEET: (&str, &str) = (
    "35 sub-agents",
    "12 working · 3 asking me · 3 failed · 17 done",
);
pub const LEAD_FAIL_TIME: &str = "15:21";
pub const LEAD_FAIL: [&str; 3] = [
    "go-dev 5 failed ",
    "TestDelete",
    ": a deleted note still counts. I sent it the failing case and it is on it again. Three agents asked me things; I answered two and kept one for you.",
];
pub const FAIL_ROW: (&str, &str, &str) = (
    "go test ./notes/ -run TestDelete: ",
    "exit 1, want 2, got 3",
    "in Shells",
);
pub const LEAD_LAST: (&str, &str) = (
    "15:24",
    "I want to rebuild the web client before qa runs the browser checks.",
);
pub const RUNNING_TOOL: (&str, &str) = ("bash npm run build", "ask");
pub const WAITING: (&str, &str) = ("waiting on you", "· 22m");
pub const ASK: (&str, &str, &str) = ("bash wants", "npm run build", "writes web/dist");
pub const ASK_BUTTONS: [(&str, &str); 3] =
    [("Allow once", "1"), ("Deny", "2"), ("Always here", "3")];
pub const COMPOSER: &str = "Ask, steer, or delegate";
pub const MODEL: &str = "Opus 5";
pub const EFFORT: &str = "Medium";

pub const STATUS_BRANCH: (&str, &str) = ("main", "↑2");
pub const STATUS_CONTEXT: (&str, &str, f32) = ("ctx", "20k", 0.08);
pub const STATUS_QUOTA: (&str, &str, &str) = ("claude-sub", "5h", "34%");
pub const STATUS_RIGHT: [&str; 2] = ["classifier 4", "cron 1"];
pub const PALETTE_KEY: &str = "Alt K";

pub const SHEET_AGENT: usize = 0;
pub const SHEET_STATE: &str = "working";
pub const THINKING: &str = "The interface has Remove; callers use it in three places. Renaming to Delete keeps the soft delete meaning clear. Count must skip deleted rows or the badge lies again, so it reads deleted_at, not a separate flag.";
pub const RAN: (&str, &str, &str, &str, &str) = (
    "ran",
    "8m ago",
    "go test ./store/...",
    "exit 0",
    "ok      notes-app/store 0.412s",
);
pub const EDITED: (&str, &str, &str, &str, &str) =
    ("edited", "9m ago", "store/store.go", "+3", "-1");
pub const DIFF: [(bool, &str, &str); 3] = [
    (false, "11", "Remove(id int64) error"),
    (true, "11", "Delete(id int64) error"),
    (true, "12", "Count() (int, error)"),
];
pub const READ: (&str, &str, &str, &str) = ("read", "10m ago", "store/store.go", "lines 1 to 120");
