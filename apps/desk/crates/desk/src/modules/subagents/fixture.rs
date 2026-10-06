#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Kind {
    Lead,
    GoDev,
    TsDev,
    Explore,
    Research,
    Qa,
    Browser,
    PyDev,
}

impl Kind {
    pub const fn name(self) -> &'static str {
        match self {
            Kind::Lead => "lead",
            Kind::GoDev => "go-dev",
            Kind::TsDev => "ts-dev",
            Kind::Explore => "explore",
            Kind::Research => "research",
            Kind::Qa => "qa",
            Kind::Browser => "browser",
            Kind::PyDev => "py-dev",
        }
    }

    pub const fn rgb(self) -> u32 {
        match self {
            Kind::Lead => 0xd9d6d0,
            Kind::GoDev => 0x79c0ff,
            Kind::TsDev => 0xb9a6ea,
            Kind::Explore => 0x8fd0aa,
            Kind::Research => 0xe8c98a,
            Kind::Qa => 0xf0a3b5,
            Kind::Browser => 0x86d4d4,
            Kind::PyDev => 0xc7d97a,
        }
    }

    pub fn initial(self) -> String {
        self.name()[..1].to_uppercase()
    }

    const fn now(self) -> &'static str {
        match self {
            Kind::GoDev => "editing store/sqlite.go",
            Kind::TsDev => "editing web/Header.tsx",
            Kind::Explore => "grep Store.All( in notes/",
            Kind::Research => "reading sqlite.org/wal.html",
            Kind::Qa => "running playwright delete.spec.ts",
            Kind::Browser => "clicking Delete on row 2",
            Kind::PyDev => "running scripts/seed.py",
            Kind::Lead => "",
        }
    }

    const fn ask(self) -> Option<&'static str> {
        match self {
            Kind::GoDev => Some("Keep the JSON store as a read only fallback for one release?"),
            Kind::Explore => Some("Which env file wins, .env or .env.local?"),
            Kind::Qa => Some("Run the e2e suite against SQLite or the JSON store?"),
            _ => None,
        }
    }

    const fn failure(self) -> &'static str {
        match self {
            Kind::GoDev => {
                "go test ./notes/ -run TestDelete exited 1: Count after delete, want 2, got 3"
            }
            Kind::PyDev => "python scripts/seed.py exited 1: no such table: notes",
            Kind::Qa => "playwright: undo restores the row timed out after 30s",
            Kind::Explore => "stopped: the search ran past its budget",
            _ => "exited 1",
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum State {
    Run,
    Wait,
    Fail,
    Done,
}

impl State {
    pub const GROUPS: [(State, &'static str); 4] = [
        (State::Run, "Working"),
        (State::Wait, "Asking the lead"),
        (State::Fail, "Failed"),
        (State::Done, "Finished"),
    ];

    pub const fn word(self) -> &'static str {
        match self {
            State::Run => "working",
            State::Wait => "asking the lead",
            State::Done => "finished",
            State::Fail => "failed",
        }
    }
}

const STATES: [State; 12] = [
    State::Run,
    State::Done,
    State::Run,
    State::Done,
    State::Wait,
    State::Done,
    State::Run,
    State::Done,
    State::Fail,
    State::Done,
    State::Run,
    State::Done,
];

const KINDS: [(Kind, &str, &[&str]); 7] = [
    (
        Kind::GoDev,
        "store/**, notes/**",
        &[
            "Port Store.All to SQLite",
            "Add Store.Count with a table test",
            "Move undo snapshots into the store",
            "Index notes by created_at",
            "Fix the flaky TestDelete",
            "Make Search case-insensitive",
            "Add the soft delete migration",
            "Remove the old JSON store",
        ],
    ),
    (
        Kind::TsDev,
        "web/**",
        &[
            "Wire the count into the badge",
            "Hide the badge at zero",
            "Move NoteList to TanStack Query",
            "Add the empty state",
            "Keyboard shortcuts for delete",
        ],
    ),
    (
        Kind::Explore,
        "reads only",
        &[
            "Find every caller of Store.All",
            "Find where notes are sorted",
            "Map the undo code",
            "List the migrations",
            "Find uses of the JSON store",
            "Find tests that touch the clock",
            "Read the delete path end to end",
            "Find every place the badge renders",
            "List env vars the server reads",
            "Find dead exports in web/",
        ],
    ),
    (
        Kind::Research,
        "reads only",
        &[
            "How TanStack refreshes after a delete",
            "SQLite WAL mode for one writer",
            "FTS5 or LIKE for search",
            "Soft delete patterns in SQLite",
        ],
    ),
    (
        Kind::Qa,
        "e2e/**",
        &[
            "Run the delete flow end to end",
            "Check the badge in the browser",
            "Replay the sort order bug",
        ],
    ),
    (
        Kind::Browser,
        "reads only",
        &[
            "Read the badge on :5871 after a delete",
            "Screenshot the empty state",
        ],
    ),
    (
        Kind::PyDev,
        "scripts/**",
        &[
            "Export notes to CSV",
            "Seed the dev database",
            "Script the SQLite backup",
        ],
    ),
];

pub struct Agent {
    pub id: String,
    pub kind: Kind,
    pub task: &'static str,
    pub owns: &'static str,
    pub state: State,
    started: f64,
    worked: f64,
}

impl Agent {
    pub fn line(&self) -> String {
        match self.state {
            State::Run => self.kind.now().to_owned(),
            State::Wait => format!("asked: {}", self.kind.ask().unwrap_or("")),
            State::Fail => self.kind.failure().to_owned(),
            State::Done => format!("worked for {}", dur(self.worked)),
        }
    }

    pub fn time(&self) -> String {
        match self.state {
            State::Done => format!("worked {}", dur(self.worked)),
            State::Run => format!("for {}", dur(self.started)),
            State::Wait | State::Fail => ago((self.started - self.worked).max(0.3)),
        }
    }
}

pub fn ago(minutes: f64) -> String {
    if minutes < 1.0 {
        "just now".to_owned()
    } else if minutes < 60.0 {
        format!("{}m ago", minutes.round())
    } else {
        format!("{}h ago", (minutes / 60.0).round())
    }
}

fn dur(minutes: f64) -> String {
    if minutes < 1.0 {
        "under a minute".to_owned()
    } else if minutes < 60.0 {
        format!("{}m", minutes.round())
    } else {
        format!(
            "{}h {}m",
            (minutes / 60.0).floor(),
            (minutes % 60.0).round()
        )
    }
}

pub fn agents() -> Vec<Agent> {
    let mut all = Vec::new();
    for (kind, owns, tasks) in KINDS {
        for (i, task) in tasks.iter().enumerate() {
            let state = STATES[all.len() % STATES.len()];
            let k = all.len() + 1;
            all.push(Agent {
                id: format!("{} {}", kind.name(), i + 1),
                kind,
                task,
                owns,
                state,
                started: (4 + (k * 7) % 38) as f64,
                worked: (2 + (k * 5) % 17) as f64,
            });
        }
    }
    all
}

#[derive(Clone, Copy)]
pub enum Sign {
    Add,
    Del,
}

pub struct HunkLine {
    pub n: u32,
    pub sign: Sign,
    pub text: &'static str,
}

pub enum Detail {
    Spawn {
        agent: String,
        kind: Kind,
        task: &'static str,
        owns: &'static str,
    },
    Read {
        path: &'static str,
        lines: &'static str,
    },
    Grep,
    Web {
        query: &'static str,
    },
    Fetch,
    Edit {
        path: &'static str,
        add: u32,
        del: u32,
        hunk: Vec<HunkLine>,
    },
    Bash {
        cmd: &'static str,
        failed: bool,
        out: Vec<&'static str>,
    },
    Browse {
        action: &'static str,
        target: &'static str,
        result: &'static str,
    },
    Ask {
        question: &'static str,
    },
    Fail {
        text: &'static str,
    },
    Report {
        text: String,
        worked: String,
    },
}

impl Detail {
    pub const fn label(&self) -> &'static str {
        match self {
            Detail::Read { .. } => "read",
            Detail::Grep => "searched the code",
            Detail::Web { .. } => "searched the web",
            Detail::Fetch => "read a page",
            Detail::Edit { .. } => "edited",
            Detail::Bash { .. } => "ran",
            Detail::Browse { .. } => "",
            Detail::Ask { .. } => "asked the lead",
            Detail::Fail { .. } => "failed",
            Detail::Report { .. } => "finished",
            Detail::Spawn { .. } => "started",
        }
    }
}

pub struct Event {
    pub at: f64,
    pub who: Option<usize>,
    pub detail: Detail,
}

pub const GREP_QUERY: &str = "Store.All(";
pub const GREP_SCOPE: &str = "**/*.go";
pub const GREP_HITS: &str = "9 matches in 4 files";
pub const GREP_FILES: [&str; 4] = [
    "notes/notes.go",
    "store/store.go",
    "web/api.ts",
    "cmd/notes/main.go",
];
pub const WEB_RESULTS: [&str; 3] = [
    "sqlite.org: Write-Ahead Logging",
    "TanStack Query: Invalidations from mutations",
    "Stack Overflow: soft delete with a deleted_at column",
];
pub const FETCH: [&str; 3] = [
    "Write-Ahead Logging",
    "sqlite.org/wal.html",
    "18 KB, sifted to 2 KB",
];

const fn add(n: u32, text: &'static str) -> HunkLine {
    HunkLine {
        n,
        sign: Sign::Add,
        text,
    }
}

const fn del(n: u32, text: &'static str) -> HunkLine {
    HunkLine {
        n,
        sign: Sign::Del,
        text,
    }
}

fn work(agent: &Agent) -> Vec<Detail> {
    let failed = agent.state == State::Fail;
    match agent.kind {
        Kind::Explore => vec![
            Detail::Grep,
            Detail::Read {
                path: "store/store.go",
                lines: "lines 1 to 80",
            },
        ],
        Kind::Research => vec![Detail::Web { query: agent.task }, Detail::Fetch],
        Kind::GoDev => vec![
            Detail::Read {
                path: "store/store.go",
                lines: "lines 1 to 120",
            },
            Detail::Edit {
                path: "store/store.go",
                add: 3,
                del: 1,
                hunk: vec![
                    del(11, "\tRemove(id int64) error"),
                    add(11, "\tDelete(id int64) error"),
                    add(12, "\tCount() (int, error)"),
                ],
            },
            Detail::Bash {
                cmd: "go test ./store/...",
                failed,
                out: if failed {
                    vec![
                        "--- FAIL: TestDelete/soft (0.02s)",
                        "    notes_test.go:88: want 2, got 3",
                    ]
                } else {
                    vec!["ok  \tnotes-app/store\t0.412s"]
                },
            },
        ],
        Kind::TsDev => vec![
            Detail::Read {
                path: "web/NoteList.tsx",
                lines: "112 lines",
            },
            Detail::Edit {
                path: "web/Header.tsx",
                add: 1,
                del: 1,
                hunk: vec![
                    del(33, "      <Badge count={count} />"),
                    add(33, "      {count > 0 && <Badge count={count} />}"),
                ],
            },
            Detail::Bash {
                cmd: "npx vitest run count",
                failed: false,
                out: vec!["✓ count.test.ts (4 tests) 21ms"],
            },
        ],
        Kind::Qa => vec![Detail::Bash {
            cmd: "npx playwright test delete.spec.ts",
            failed,
            out: vec![
                "✓ delete removes the row (1.2s)",
                "✓ badge goes down (0.9s)",
            ],
        }],
        Kind::Browser => vec![
            Detail::Browse {
                action: "opened",
                target: "localhost:5871/notes",
                result: "5 notes listed",
            },
            Detail::Browse {
                action: "clicked",
                target: "Delete on Call the landlord",
                result: "the row is gone",
            },
            Detail::Browse {
                action: "read",
                target: "span.badge",
                result: "\"4 notes\"",
            },
        ],
        Kind::PyDev => vec![
            Detail::Edit {
                path: "scripts/seed.py",
                add: 2,
                del: 1,
                hunk: vec![
                    del(21, "    rows = load_json(path)"),
                    add(
                        21,
                        "    rows = db.execute(\"SELECT * FROM notes\").fetchall()",
                    ),
                    add(22, "    write_csv(rows)"),
                ],
            },
            Detail::Bash {
                cmd: "python scripts/seed.py",
                failed,
                out: vec![if failed {
                    "sqlite3.OperationalError: no such table: notes"
                } else {
                    "seeded 40 notes"
                }],
            },
        ],
        Kind::Lead => Vec::new(),
    }
}

fn report(agent: &Agent) -> String {
    match agent.kind {
        Kind::Explore => {
            "Store.All is called from 4 places; only cmd/notes reads the JSON path directly."
                .to_owned()
        }
        Kind::Research => {
            "WAL mode fits one writer and many readers; set it once at open.".to_owned()
        }
        _ => {
            let mut task = agent.task.chars();
            let first = task
                .next()
                .map(|c| c.to_lowercase().to_string())
                .unwrap_or_default();
            format!("Done: {first}{}.", task.as_str())
        }
    }
}

pub fn events(agents: &[Agent]) -> Vec<Event> {
    let mut all = Vec::new();
    for (index, agent) in agents.iter().enumerate() {
        let t0 = agent.started;
        let end = (t0 - agent.worked).max(0.0);
        all.push(Event {
            at: t0 + 0.2,
            who: None,
            detail: Detail::Spawn {
                agent: agent.id.clone(),
                kind: agent.kind,
                task: agent.task,
                owns: agent.owns,
            },
        });
        let step = (agent.worked / 6.0).max(0.4);
        for (j, detail) in work(agent).into_iter().enumerate() {
            all.push(Event {
                at: t0 - (j as f64 + 1.0) * step,
                who: Some(index),
                detail,
            });
        }
        let last = match agent.state {
            State::Run => None,
            State::Wait => Some((
                end.max(0.3),
                Detail::Ask {
                    question: agent.kind.ask().unwrap_or("Which env file wins?"),
                },
            )),
            State::Fail => Some((
                end.max(0.2),
                Detail::Fail {
                    text: agent.kind.failure(),
                },
            )),
            State::Done => Some((
                end,
                Detail::Report {
                    text: report(agent),
                    worked: format!("worked for {}", dur(agent.worked)),
                },
            )),
        };
        if let Some((at, detail)) = last {
            all.push(Event {
                at,
                who: Some(index),
                detail,
            });
        }
    }
    all.sort_by(|x, y| x.at.total_cmp(&y.at));
    all
}

pub fn bucket(minutes: f64) -> &'static str {
    if minutes < 3.0 {
        "Just now"
    } else if minutes < 8.0 {
        "A few minutes ago"
    } else if minutes < 20.0 {
        "In the last twenty minutes"
    } else {
        "Earlier"
    }
}

pub const FEED_LIMIT: usize = 70;
