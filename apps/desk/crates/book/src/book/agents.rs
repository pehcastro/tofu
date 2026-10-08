use std::rc::Rc;

use desk_ui::components::agents::{
    AgentBoard, AgentEvent, AgentLine, AgentScreen, AgentStep, AgentTile, DiffLine, DiffSign, ago,
};
use desk_ui::components::avatar::{Agent, AgentKind, AgentStatus};
use desk_ui::components::card::{Header, inner_card, shell};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::tabs::{Tab, TabEvent, TabMark, connected_tabs};
use desk_ui::theme::Theme;
use gpui::{App, Context, Div, Entity, SharedString, WeakEntity, Window, div, prelude::*, px};

use super::Book;
use super::kit::label;

const TILE_WIDTH: f32 = 538.0;
const TILE_HEIGHT: f32 = 820.0;
const NARROW_WIDTH: f32 = 190.0;
const NARROW_HEIGHT: f32 = 260.0;
const NARROW_AGENTS: usize = 2;
const SCREEN_HEIGHT: f32 = 720.0;
const FILE_EDITS: u32 = 20;
const SHELLS: u32 = 6;
const START_BASE: u16 = 4;
const START_STEP: u16 = 7;
const START_SPREAD: u16 = 38;
const WORK_BASE: u16 = 2;
const WORK_STEP: u16 = 5;
const WORK_SPREAD: u16 = 17;
const SPAWN_LEAD: f32 = 0.2;
const STEP_FLOOR: f32 = 0.4;
const STEPS_PER_WORK: f32 = 6.0;
const ASK_FLOOR: f32 = 0.3;
const FAIL_FLOOR: f32 = 0.2;

const KINDS: [(AgentKind, &str, &[&str]); 7] = [
    (
        AgentKind::GoDev,
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
        AgentKind::TsDev,
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
        AgentKind::Explore,
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
        AgentKind::Research,
        "reads only",
        &[
            "How TanStack refreshes after a delete",
            "SQLite WAL mode for one writer",
            "FTS5 or LIKE for search",
            "Soft delete patterns in SQLite",
        ],
    ),
    (
        AgentKind::Qa,
        "e2e/**",
        &[
            "Run the delete flow end to end",
            "Check the badge in the browser",
            "Replay the sort order bug",
        ],
    ),
    (
        AgentKind::Browser,
        "reads only",
        &[
            "Read the badge on :5871 after a delete",
            "Screenshot the empty state",
        ],
    ),
    (
        AgentKind::PyDev,
        "scripts/**",
        &[
            "Export notes to CSV",
            "Seed the dev database",
            "Script the SQLite backup",
        ],
    ),
];

const STATES: [AgentStatus; 12] = [
    AgentStatus::Working,
    AgentStatus::Finished,
    AgentStatus::Working,
    AgentStatus::Finished,
    AgentStatus::Asking,
    AgentStatus::Finished,
    AgentStatus::Working,
    AgentStatus::Finished,
    AgentStatus::Failed,
    AgentStatus::Finished,
    AgentStatus::Working,
    AgentStatus::Finished,
];

fn doing_now(kind: AgentKind) -> &'static str {
    match kind {
        AgentKind::GoDev => "editing store/sqlite.go",
        AgentKind::TsDev => "editing web/Header.tsx",
        AgentKind::Explore => "grep Store.All( in notes/",
        AgentKind::Research => "reading sqlite.org/wal.html",
        AgentKind::Qa => "running playwright delete.spec.ts",
        AgentKind::Browser => "clicking Delete on row 2",
        AgentKind::PyDev => "running scripts/seed.py",
    }
}

fn question(kind: AgentKind) -> Option<&'static str> {
    match kind {
        AgentKind::GoDev => Some("Keep the JSON store as a read only fallback for one release?"),
        AgentKind::Explore => Some("Which env file wins, .env or .env.local?"),
        AgentKind::Qa => Some("Run the e2e suite against SQLite or the JSON store?"),
        AgentKind::TsDev | AgentKind::Research | AgentKind::Browser | AgentKind::PyDev => None,
    }
}

fn failure(kind: AgentKind) -> &'static str {
    match kind {
        AgentKind::GoDev => {
            "go test ./notes/ -run TestDelete exited 1: Count after delete, want 2, got 3"
        }
        AgentKind::PyDev => "python scripts/seed.py exited 1: no such table: notes",
        AgentKind::Qa => "playwright: undo restores the row timed out after 30s",
        AgentKind::Explore => "stopped: the search ran past its budget",
        AgentKind::TsDev | AgentKind::Research | AgentKind::Browser => "exited 1",
    }
}

fn thinking(kind: AgentKind) -> &'static str {
    match kind {
        AgentKind::GoDev => {
            "The interface has Remove; callers use it in three places. Renaming to Delete keeps the soft delete meaning clear. Count must skip deleted rows or the badge lies again, so it reads deleted_at, not a separate flag."
        }
        AgentKind::TsDev => {
            "The badge reads useCount, which caches for 30s. Invalidating the count key in the delete mutation is the smallest change; hiding at zero is one condition in Header."
        }
        AgentKind::Explore => {
            "Store.All is the entry point. Search callers first, then read the two that open the store directly."
        }
        AgentKind::Research => {
            "One writer, many readers: WAL is the fit. Check whether the driver sets it per connection or per database."
        }
        AgentKind::Qa => "Run the delete flow first; the badge check depends on it.",
        AgentKind::Browser => {
            "Open the page, delete one row, read the badge, compare with the list length."
        }
        AgentKind::PyDev => "The seed reads the JSON file; point it at the SQLite schema instead.",
    }
}

fn report(kind: AgentKind, task: &str) -> String {
    match kind {
        AgentKind::Explore => {
            "Store.All is called from 4 places; only cmd/notes reads the JSON path directly.".into()
        }
        AgentKind::Research => {
            "WAL mode fits one writer and many readers; set it once at open.".into()
        }
        _ => {
            let mut chars = task.chars();
            let first = chars.next().map(|c| c.to_lowercase().to_string());
            format!("Done: {}{}.", first.unwrap_or_default(), chars.as_str())
        }
    }
}

fn texts(lines: &[&str]) -> Vec<SharedString> {
    lines.iter().map(|line| SharedString::from(*line)).collect()
}

fn diff(lines: &[(DiffSign, u32, &str)]) -> Vec<DiffLine> {
    lines
        .iter()
        .map(|&(sign, number, text)| DiffLine {
            number,
            sign,
            text: text.into(),
        })
        .collect()
}

struct Seed {
    agent: Agent,
    task: &'static str,
    owns: &'static str,
    started: u16,
    worked: u16,
}

fn seeds() -> Vec<Seed> {
    let mut states = STATES.iter().copied().cycle();
    let mut k: u16 = 0;
    let mut seeds = Vec::new();
    for (kind, owns, tasks) in KINDS {
        for (instance, task) in (1u32..).zip(tasks.iter().copied()) {
            let Some(status) = states.next() else {
                break;
            };
            k += 1;
            seeds.push(Seed {
                agent: Agent {
                    kind,
                    instance,
                    status,
                },
                task,
                owns,
                started: START_BASE + (k * START_STEP) % START_SPREAD,
                worked: WORK_BASE + (k * WORK_STEP) % WORK_SPREAD,
            });
        }
    }
    seeds
}

fn line(seed: &Seed, events: &[AgentEvent]) -> AgentLine {
    let edits = events.iter().filter_map(|event| match &event.step {
        AgentStep::Edit {
            path,
            added,
            removed,
            ..
        } => Some((path, *added, *removed)),
        _ => None,
    });
    let mut paths = edits.clone().map(|(path, _, _)| path).collect::<Vec<_>>();
    paths.sort();
    paths.dedup();
    let sum = |pick: fn((&SharedString, u32, u32)) -> u32| {
        edits.clone().map(pick).map(|n| n as usize).sum()
    };
    let kind = seed.agent.kind;
    let (started, worked) = (f32::from(seed.started), f32::from(seed.worked));
    let (now, time) = match seed.agent.status {
        AgentStatus::Working => (doing_now(kind).to_owned(), format!("for {}m", seed.started)),
        AgentStatus::Asking => (
            format!("asked: {}", question(kind).unwrap_or_default()),
            ago((started - worked).max(ASK_FLOOR)).to_string(),
        ),
        AgentStatus::Failed => (
            failure(kind).to_owned(),
            ago((started - worked).max(ASK_FLOOR)).to_string(),
        ),
        AgentStatus::Finished => (
            format!("worked for {}m", seed.worked),
            format!("worked {}m", seed.worked),
        ),
    };
    AgentLine {
        agent: seed.agent,
        task: seed.task.into(),
        now: now.into(),
        time: time.into(),
        owns: seed.owns.into(),
        thinking: thinking(kind).into(),
        added: sum(|(_, added, _)| added),
        removed: sum(|(_, _, removed)| removed),
        files: paths.len(),
        tools: events
            .iter()
            .filter(|event| {
                !matches!(
                    event.step,
                    AgentStep::Spawn { .. }
                        | AgentStep::Report { .. }
                        | AgentStep::Fail { .. }
                        | AgentStep::Ask { .. }
                )
            })
            .count(),
    }
}

fn events(seed: &Seed) -> Vec<AgentEvent> {
    let agent = seed.agent;
    let failed = agent.status == AgentStatus::Failed;
    let exit = i32::from(failed);
    let (started, worked) = (f32::from(seed.started), f32::from(seed.worked));
    let end = (started - worked).max(0.0);
    let gap = STEP_FLOOR.max(worked / STEPS_PER_WORK);
    let mut at = 0.0;
    let step = |j: f32| started - (j + 1.0) * gap;
    let mut steps = vec![(
        started + SPAWN_LEAD,
        AgentStep::Spawn {
            task: seed.task.into(),
            owns: seed.owns.into(),
        },
    )];
    let mut then = |step: AgentStep, steps: &mut Vec<(f32, AgentStep)>, when: f32| {
        steps.push((when, step));
        at += 1.0;
    };
    use DiffSign::{Added, Removed};
    match agent.kind {
        AgentKind::Explore => {
            then(
                AgentStep::Grep {
                    query: "Store.All(".into(),
                    scope: "**/*.go".into(),
                    hits: "9 matches in 4 files".into(),
                    files: texts(&[
                        "notes/notes.go",
                        "store/store.go",
                        "web/api.ts",
                        "cmd/notes/main.go",
                    ]),
                },
                &mut steps,
                step(0.0),
            );
            then(
                AgentStep::Read {
                    path: "store/store.go".into(),
                    lines: "lines 1 to 80".into(),
                },
                &mut steps,
                step(1.0),
            );
        }
        AgentKind::Research => {
            then(
                AgentStep::Web {
                    query: seed.task.into(),
                    results: texts(&[
                        "sqlite.org: Write-Ahead Logging",
                        "TanStack Query: Invalidations from mutations",
                        "Stack Overflow: soft delete with a deleted_at column",
                    ]),
                },
                &mut steps,
                step(0.0),
            );
            then(
                AgentStep::Fetch {
                    url: "sqlite.org/wal.html".into(),
                    title: "Write-Ahead Logging".into(),
                    size: "18 KB, sifted to 2 KB".into(),
                },
                &mut steps,
                step(1.0),
            );
        }
        AgentKind::GoDev => {
            then(
                AgentStep::Read {
                    path: "store/store.go".into(),
                    lines: "lines 1 to 120".into(),
                },
                &mut steps,
                step(0.0),
            );
            then(
                AgentStep::Edit {
                    path: "store/store.go".into(),
                    added: 3,
                    removed: 1,
                    hunk: diff(&[
                        (Removed, 11, "\tRemove(id int64) error"),
                        (Added, 11, "\tDelete(id int64) error"),
                        (Added, 12, "\tCount() (int, error)"),
                    ]),
                },
                &mut steps,
                step(1.0),
            );
            let output: &[&str] = if failed {
                &[
                    "--- FAIL: TestDelete/soft (0.02s)",
                    "    notes_test.go:88: want 2, got 3",
                ]
            } else {
                &["ok  \tnotes-app/store\t0.412s"]
            };
            then(
                AgentStep::Bash {
                    command: "go test ./store/...".into(),
                    exit,
                    output: texts(output),
                },
                &mut steps,
                step(2.0),
            );
        }
        AgentKind::TsDev => {
            then(
                AgentStep::Read {
                    path: "web/NoteList.tsx".into(),
                    lines: "112 lines".into(),
                },
                &mut steps,
                step(0.0),
            );
            then(
                AgentStep::Edit {
                    path: "web/Header.tsx".into(),
                    added: 1,
                    removed: 1,
                    hunk: diff(&[
                        (Removed, 33, "      <Badge count={count} />"),
                        (Added, 33, "      {count > 0 && <Badge count={count} />}"),
                    ]),
                },
                &mut steps,
                step(1.0),
            );
            then(
                AgentStep::Bash {
                    command: "npx vitest run count".into(),
                    exit: 0,
                    output: texts(&["\u{2713} count.test.ts (4 tests) 21ms"]),
                },
                &mut steps,
                step(2.0),
            );
        }
        AgentKind::Qa => then(
            AgentStep::Bash {
                command: "npx playwright test delete.spec.ts".into(),
                exit,
                output: texts(&[
                    "\u{2713} delete removes the row (1.2s)",
                    "\u{2713} badge goes down (0.9s)",
                ]),
            },
            &mut steps,
            step(0.0),
        ),
        AgentKind::Browser => {
            for (j, (action, target, result)) in [
                ("opened", "localhost:5871/notes", "5 notes listed"),
                ("clicked", "Delete on Call the landlord", "the row is gone"),
                ("read", "span.badge", "\"4 notes\""),
            ]
            .into_iter()
            .enumerate()
            {
                let j = if j == 0 {
                    0.0
                } else if j == 1 {
                    1.0
                } else {
                    2.0
                };
                then(
                    AgentStep::Browse {
                        action: action.into(),
                        target: target.into(),
                        result: result.into(),
                    },
                    &mut steps,
                    step(j),
                );
            }
        }
        AgentKind::PyDev => {
            then(
                AgentStep::Edit {
                    path: "scripts/seed.py".into(),
                    added: 2,
                    removed: 1,
                    hunk: diff(&[
                        (Removed, 21, "    rows = load_json(path)"),
                        (
                            Added,
                            21,
                            "    rows = db.execute(\"SELECT * FROM notes\").fetchall()",
                        ),
                        (Added, 22, "    write_csv(rows)"),
                    ]),
                },
                &mut steps,
                step(0.0),
            );
            let output: &[&str] = if failed {
                &["sqlite3.OperationalError: no such table: notes"]
            } else {
                &["seeded 40 notes"]
            };
            then(
                AgentStep::Bash {
                    command: "python scripts/seed.py".into(),
                    exit,
                    output: texts(output),
                },
                &mut steps,
                step(1.0),
            );
        }
    }
    match agent.status {
        AgentStatus::Working => {}
        AgentStatus::Asking => steps.push((
            end.max(ASK_FLOOR),
            AgentStep::Ask {
                question: question(agent.kind)
                    .unwrap_or("Which env file wins?")
                    .into(),
            },
        )),
        AgentStatus::Failed => steps.push((
            end.max(FAIL_FLOOR),
            AgentStep::Fail {
                text: failure(agent.kind).into(),
            },
        )),
        AgentStatus::Finished => steps.push((
            end,
            AgentStep::Report {
                text: report(agent.kind, seed.task).into(),
                worked: format!("worked for {}m", seed.worked).into(),
            },
        )),
    }
    steps
        .into_iter()
        .map(|(minutes, step)| AgentEvent {
            agent,
            minutes,
            step,
        })
        .collect()
}

fn board(swapped: bool) -> AgentBoard {
    seeded(seeds(), swapped)
}

fn seeded(mut seeds: Vec<Seed>, swapped: bool) -> AgentBoard {
    if let Some(first) = seeds.first_mut().filter(|_| swapped) {
        first.agent.status = AgentStatus::Failed;
    }
    let mut lines = Vec::new();
    let mut all = Vec::new();
    for seed in &seeds {
        let made = events(seed);
        lines.push(line(seed, &made));
        all.extend(made);
    }
    all.sort_by(|a, b| a.minutes.total_cmp(&b.minutes));
    AgentBoard {
        lines,
        events: all,
        attributed: true,
    }
}

fn mention(book: WeakEntity<Book>) -> impl Fn(&Agent, &mut Window, &mut App) + 'static {
    move |agent, _, cx| {
        if let Some(book) = book.upgrade() {
            book.update(cx, |book, cx| {
                book.tell(format!("{} mentioned in the chat", agent.name()), cx)
            });
        }
    }
}

pub(super) struct AgentsPage {
    board: Rc<AgentBoard>,
    tile: Entity<AgentTile>,
    narrow: Entity<AgentTile>,
    wide: Entity<AgentTile>,
    screen: Entity<AgentScreen>,
    picked: Entity<AgentScreen>,
    swapped: bool,
    pending: bool,
}

impl AgentsPage {
    pub(super) fn new(cx: &mut Context<Book>) -> Self {
        let board = Rc::new(board(false));
        let book = cx.weak_entity();
        let screen = cx.new(|_| AgentScreen::new(board.clone(), mention(book.clone())));
        let first = board.lines.first().map(|line| line.agent);
        let picked = cx.new(|cx| {
            let mut picked = AgentScreen::new(board.clone(), mention(book.clone()));
            picked.show(first, cx);
            picked
        });
        let shown = picked.clone();
        let told = book.clone();
        let expand = move |agent: &Agent, _: &mut Window, cx: &mut App| {
            shown.update(cx, |screen, cx| screen.show(Some(*agent), cx));
            if let Some(book) = told.upgrade() {
                book.update(cx, |book, cx| {
                    book.tell(
                        format!("{} expanded: the screen below shows it", agent.name()),
                        cx,
                    )
                });
            }
        };
        let few = seeds().into_iter().take(NARROW_AGENTS).collect();
        let narrow = Rc::new(seeded(few, false));
        let narrow = cx.new(|cx| AgentTile::new(narrow, mention(book.clone()), |_, _, _| {}, cx));
        let wide =
            cx.new(|cx| AgentTile::new(board.clone(), mention(book.clone()), |_, _, _| {}, cx));
        let tile = cx.new(|cx| AgentTile::new(board.clone(), mention(book), expand, cx));
        AgentsPage {
            board,
            tile,
            narrow,
            wide,
            screen,
            picked,
            swapped: false,
            pending: false,
        }
    }

    fn swap(&mut self, cx: &mut Context<Book>) {
        self.pending = false;
        self.swapped = !self.swapped;
        self.board = Rc::new(board(self.swapped));
        self.tile
            .update(cx, |tile, cx| tile.set_board(self.board.clone(), cx));
        self.wide
            .update(cx, |tile, cx| tile.set_board(self.board.clone(), cx));
        for screen in [&self.screen, &self.picked] {
            screen.update(cx, |screen, cx| screen.set_board(self.board.clone(), cx));
        }
    }

    fn tabs(&self, theme: &Theme, cx: &mut Context<Book>) -> impl IntoElement {
        let tab = |label: &str, icon, count| Tab {
            label: SharedString::from(label.to_owned()),
            icon: Some(icon),
            count,
            mark: TabMark::Close,
        };
        let tabs = [
            tab(
                "Sub-agents",
                Glyph::Agents,
                u32::try_from(self.board.live()).ok(),
            ),
            tab("File edits", Glyph::File, Some(FILE_EDITS)),
            tab("Shells", Glyph::Terminal, Some(SHELLS)),
        ];
        let book = cx.weak_entity();
        connected_tabs(
            "agents-tabs",
            &tabs,
            0,
            usize::MAX,
            theme,
            move |event, _, cx| {
                if matches!(event, TabEvent::Select(0)) {
                    return;
                }
                if let Some(book) = book.upgrade() {
                    book.update(cx, |book, cx| {
                        book.tell("Only the Sub-agents tab is built on this page", cx)
                    });
                }
            },
        )
    }

    pub(super) fn render(&mut self, theme: &Theme, _: &mut Window, cx: &mut Context<Book>) -> Div {
        if self.pending {
            self.swap(cx);
        }
        let column = |caption: &'static str, body: Div| {
            div()
                .flex()
                .flex_col()
                .gap_2()
                .min_w_0()
                .child(label(caption, theme))
                .child(body)
        };
        let tile = shell(
            Header::Tabs(self.tabs(theme, cx).into_any_element(), None),
            theme,
        )
        .w(px(TILE_WIDTH))
        .h(px(TILE_HEIGHT))
        .child(inner_card(theme).child(self.tile.clone()));
        let narrow = shell(
            Header::Title(Some(Glyph::Agents), "Sub-agents".into(), None),
            theme,
        )
        .w(px(NARROW_WIDTH))
        .h(px(NARROW_HEIGHT))
        .child(inner_card(theme).child(self.narrow.clone()));
        let wide = shell(
            Header::Title(Some(Glyph::Agents), "Sub-agents".into(), None),
            theme,
        )
        .w_full()
        .h(px(NARROW_HEIGHT))
        .child(inner_card(theme).child(self.wide.clone()));
        let screen = |caption: &'static str, screen: &Entity<AgentScreen>| {
            column(
                caption,
                div().h(px(SCREEN_HEIGHT)).flex().child(screen.clone()),
            )
            .flex_1()
            .flex_basis(px(0.0))
        };
        div()
            .flex()
            .flex_col()
            .gap_4()
            .child(column(
                "Tile, IWY-4: the Sub-agents tab of the work card, 538 x 820 as in the 1440 x 900 board. A row opens the drawer over the bottom of the card; click outside it, Escape or Close shuts it, Expand opens the agent in the screen below.",
                tile,
            ))
            .child(column(
                "Narrow, DESK-208: the same tile 190 px wide with two agents, as in a 720 x 420 board with the sidebar open. Under 48 px for doing, the doing column drops and agent and time stay.",
                narrow,
            ))
            .child(column(
                "Wide, DESK-216: the same tile across the page. A row keeps the narrow row's height and two lines; width adds LINES from 560 px, FILES from 640 px and TOOLS from 760 px. Under 48 px for doing, TIME shrinks to its text and the agent name keeps the rest.",
                wide,
            ))
            .child(
                div()
                    .flex()
                    .gap_4()
                    .child(screen(
                        "Screen, IWY-7: the Sub-agents screen tab on All activity, the header counting each state. A row on the left shows that agent on the right in place.",
                        &self.screen,
                    ))
                    .child(screen(
                        "Screen, S-WORK-5: the same screen with one agent picked, as the drawer's Expand opens it: its task and owns on top, then only its events; All activity goes back.",
                        &self.picked,
                    )),
            )
    }

    pub(super) fn key(&mut self, key: &str) -> bool {
        self.pending = key == "b";
        self.pending
    }
}
