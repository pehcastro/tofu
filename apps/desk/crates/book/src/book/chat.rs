use std::cell::{Cell, RefCell};
use std::rc::Rc;
use std::time::{Duration, Instant};

use desk_motion::tokens::{EASE_OUT, PANEL_IN_MS, PANEL_OUT_MS, TOGGLE_MS};
use desk_motion::{Phase, Presence, reduced_motion};
use desk_ui::components::ask::{
    Act, Ask, Asking, Choice, Pick, Question, Shape, ask_bar, phase_line,
};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::chat::{
    self, Agent, AgentMark, Block, Call, FIND_RESERVE, Face, Hit, Marks, Span, Verdict, agent_list,
    agent_row, agents, calls, command, cron_row, fail, find_hits, folding, foot, hit_marks, lead,
    note, queued, tools, you,
};
use desk_ui::components::chip::kbd;
use desk_ui::components::find::FindBar;
use desk_ui::components::form::TextArea;
use desk_ui::components::size::MENU_PAD;
use desk_ui::components::transcript::{Transcript, transcript};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, App, ClickEvent, Context, Div, Entity, EntityId, Focusable, FollowMode,
    KeyDownEvent, ListAlignment, ListState, MouseButton, SharedString, Window, div, list,
    prelude::*, px,
};

use super::Book;
use super::kit::{block, label, toggle};

const GATE_ANSWER: &str = "The gate reads the policy first so that a verdict is never invented.

## What the policy carries

- the mode it was declared in
- the lock that resolved it
- the thresholds

None of the three is knowable from the wire alone, so `tool_gate` waits for `policy.Resolve` before it answers.";

const MARKDOWN_SAMPLE: &str = r#"# Markdown sample

Text can be **bold**, *italic*, ***both***, ~~struck~~ or `inline code`, and a [link to the docs](https://github.com/gpui-ce/gpui-ce).

| Tool | Calls | Share |
|:-----|:-----:|------:|
| read | 12 | 48% |
| edit | 9 | 36% |
| shell | 4 | 16% |

```rust
fn main() {
    let count: u32 = 3;
    println!("{count} tools");
}
```

- [x] parse the reply
- [ ] draw the chart
- a plain bullet

1. first
2. second

> A quote keeps its **emphasis**
> across two lines.

---

<Chart kind="bar" values={[1, 2, 3]} />"#;

const BOARD_WIDTH: f32 = 760.0;
const CONTENT_WIDTH: f32 = 672.0;
const NARROW_WIDTH: f32 = 320.0;
const COLUMN_HEIGHT: f32 = 560.0;
const COLUMN_TOP: f32 = 22.0;
const COLUMN_BOTTOM: f32 = 30.0;
const NARROW_PAD: f32 = 14.0;
const INNER_RADIUS: f32 = 11.0;
const ASK_MARGIN: f32 = 12.0;
const PAGE_GAP: f32 = 16.0;
const OVERDRAW: f32 = 200.0;
const REVEAL: Duration = Duration::from_millis(420);
const SETTLED_OPEN: Duration = Duration::from_secs(1);
const STREAM_ROWS: usize = 2000;
const STREAM_APPEND: usize = 500;

enum Row {
    You {
        time: &'static str,
        text: &'static str,
        traced: bool,
        attached: Option<&'static str>,
    },
    Queued(&'static str),
    Lead {
        time: &'static str,
        body: Vec<Block>,
    },
    Tools {
        count: &'static str,
        summary: &'static str,
        calls: Vec<Call>,
        footnote: Option<&'static str>,
    },
    Agents {
        count: &'static str,
        summary: &'static str,
        agents: Vec<Agent>,
    },
    Agent(Agent),
    Cron {
        job: &'static str,
        schedule: &'static str,
        prompt: &'static str,
    },
    Command {
        busy: bool,
        text: &'static str,
        verdict: Option<Verdict>,
        tail: Option<&'static str>,
    },
    Fail {
        text: &'static str,
        detail: &'static str,
        link: &'static str,
    },
    Note(&'static str),
    Foot(&'static str),
}

impl Row {
    fn folds(&self) -> bool {
        matches!(self, Row::Tools { .. } | Row::Agents { .. })
    }

    fn pieces(&self) -> Vec<String> {
        match self {
            Row::You { text, .. } => vec![(*text).to_owned()],
            Row::Lead { body, .. } => chat::pieces(body),
            Row::Queued(_)
            | Row::Tools { .. }
            | Row::Agents { .. }
            | Row::Agent(_)
            | Row::Cron { .. }
            | Row::Command { .. }
            | Row::Fail { .. }
            | Row::Note(_)
            | Row::Foot(_) => Vec::new(),
        }
    }
}

fn call(tool: &'static str, arg: &'static str, out: &'static str) -> Call {
    Call {
        tool: tool.into(),
        arg: arg.into(),
        out: out.into(),
    }
}

fn said(time: &'static str, body: Vec<Block>) -> Row {
    Row::Lead { time, body }
}

fn asked(time: &'static str, text: &'static str) -> Row {
    Row::You {
        time,
        text,
        traced: false,
        attached: None,
    }
}

fn plain(text: &'static str) -> Span {
    Span::new(Face::Plain, text)
}

fn code(text: &'static str) -> Span {
    Span::new(Face::Code, text)
}

fn spoke(spans: Vec<Span>) -> Vec<Block> {
    vec![Block::Para(spans)]
}

fn member(
    mark: AgentMark,
    name: &'static str,
    summary: &'static str,
    link: Option<&'static str>,
) -> Agent {
    Agent {
        mark,
        name: name.into(),
        summary: summary.into(),
        link: link.map(Into::into),
    }
}

fn gate_calls() -> Vec<Call> {
    vec![
        call("read", "internal/judge/gate.go", "212 lines"),
        call("read", "internal/policy/policy.go", "96 lines"),
        call("grep", "Resolve( in internal/", "6 matches"),
        call("jev", "tool_gate", "allow"),
        call(
            "bash",
            "go test ./internal/judge/",
            "ok · sifted 2 of 31 lines",
        ),
        call("read", "internal/policy/lock.go", "58 lines"),
        call("read", "go.mod", "24 lines"),
    ]
}

fn port_calls() -> Vec<Call> {
    vec![
        call("read", "notes/store.go", "188 lines"),
        call("read", "notes/notes.go", "140 lines"),
        call("read", "notes/store_test.go", "203 lines"),
        call("read", "web/NoteList.tsx", "112 lines"),
        call("grep", "Store( in notes/ web/", "14 hits"),
        call("jev", "tool_gate", "allow"),
        call("read", "go.mod", "24 lines"),
        call("read", "notes/search.go", "88 lines"),
        call("bash", "go build ./...", "exit 0"),
    ]
}

fn count_calls() -> Vec<Call> {
    vec![
        call("read", "notes/notes.go", "140 lines"),
        call("read", "web/NoteList.tsx", "112 lines"),
        call("grep", "Count in notes/ web/", "no matches"),
        call("bash", "go test ./notes/", "ok · sifted 3 of 48 lines"),
        call("read", "web/api.ts", "64 lines"),
    ]
}

fn swarm() -> Vec<Agent> {
    vec![
        member(
            AgentMark::Working,
            "go-dev 2",
            "porting Store.Search · step 5 of 12 · 4m 02s",
            None,
        ),
        member(
            AgentMark::Working,
            "ts-dev",
            "note count badge · step 4 of 9 · 2m 31s",
            None,
        ),
        member(
            AgentMark::Asking,
            "research",
            "asked the lead which driver · 40s",
            None,
        ),
        member(
            AgentMark::Failed,
            "go-dev 5",
            "TestDelete failed · sent back with the case",
            None,
        ),
        member(AgentMark::Done, "explore", "mapped 6 callers · 52s", None),
        member(
            AgentMark::Done,
            "qa",
            "wrote the end to end test · 1m 40s",
            None,
        ),
    ]
}

fn conversation() -> Vec<Row> {
    vec![
        Row::You {
            time: "14:32",
            text: "why does the gate read the policy first?",
            traced: true,
            attached: None,
        },
        Row::Tools {
            count: "7 tools",
            summary: "· classifier 1 · shell 1 · 16s",
            calls: gate_calls(),
            footnote: Some("The outputs are in Sub-agents under lead, each with its trace."),
        },
        said("14:32", chat::markdown(GATE_ANSWER)),
        Row::Foot("cooked for 16s · waited 0s"),
        asked("14:34", "reply with a markdown sample"),
        said("14:34", chat::markdown(MARKDOWN_SAMPLE)),
        asked(
            "14:36",
            "check the loader with a sub-agent, I want a second read",
        ),
        said(
            "14:36",
            spoke(vec![plain("Handing the policy loader to qa.")]),
        ),
        Row::Agent(member(
            AgentMark::Done,
            "qa",
            "read 4 files, reported · 1m 18s",
            Some("open in Sub-agents"),
        )),
        said(
            "14:38",
            spoke(vec![plain(
                "qa read it the same way: the loader reads the lock before the mode, and a missing lock is an error, never a default.",
            )]),
        ),
        Row::Foot("cooked for 1m 24s · waited 0s"),
        Row::Cron {
            job: "c1",
            schedule: "every 30m",
            prompt: "say hi",
        },
        said("14:40", spoke(vec![plain("hi")])),
        asked("14:41", "push it"),
        Row::Command {
            busy: false,
            text: "bash git push origin develop",
            verdict: Some(Verdict::Exit("exit 128".into())),
            tail: None,
        },
        Row::Fail {
            text: "git push origin develop",
            detail: "fatal: could not read Username for 'https://github.com'",
            link: "whole error in work",
        },
        said(
            "14:41",
            spoke(vec![
                plain("There is no credential for that remote here. Run "),
                code("gh auth login"),
                plain(" once and I will push again."),
            ]),
        ),
        Row::Foot("cooked for 6s · waited 0s"),
        asked("14:44", "read the whole loop file and summarize it"),
        Row::Command {
            busy: false,
            text: "read internal/turn/loop.go",
            verdict: None,
            tail: Some("84 lines, 2.1 KB"),
        },
        Row::Note("· you stopped the turn"),
        Row::Note("· 64 characters were written and kept in sub-agents"),
        Row::Foot("stopped after 3s"),
        Row::You {
            time: "15:02",
            text: "Port the store to SQLite and keep the API the same. The web client should show the note count. Use as many agents as it takes.",
            traced: true,
            attached: None,
        },
        Row::Tools {
            count: "9 tools",
            summary: "· 12s",
            calls: port_calls(),
            footnote: None,
        },
        said(
            "15:03",
            spoke(vec![plain(
                "A big one, so I split it: explore maps the code first, go-dev ports the store in parallel slices, ts-dev takes the web client, research answers the open questions, qa and browser check the result, py-dev updates the scripts.",
            )]),
        ),
        Row::Agents {
            count: "35 sub-agents",
            summary: "12 working · 3 asking me · 3 failed · 17 done",
            agents: swarm(),
        },
        said(
            "15:21",
            spoke(vec![
                plain("go-dev 5 failed "),
                code("TestDelete"),
                plain(
                    ": a deleted note still counts. I sent it the failing case and it is on it again. Three agents asked me things; I answered two and kept one for you.",
                ),
            ]),
        ),
        Row::Fail {
            text: "go test ./notes/ -run TestDelete",
            detail: "exit 1, want 2, got 3",
            link: "in Shells",
        },
        said(
            "15:24",
            spoke(vec![plain(
                "I want to rebuild the web client before qa runs the browser checks.",
            )]),
        ),
        Row::Command {
            busy: false,
            text: "bash npm run build",
            verdict: Some(Verdict::Ask),
            tail: None,
        },
        Row::Note("· allowed once · building"),
        Row::Foot("cooked for 9m 12s · waited 22s"),
        Row::You {
            time: "16:31",
            text: "Why ORDER BY id and not created_at?",
            traced: false,
            attached: Some("store.go 25 to 26"),
        },
        said(
            "16:31",
            spoke(vec![
                plain(
                    "Two callers binary-search by id, so insertion order is part of the API. The lead decided it when go-dev asked at 16:12 ",
                ),
                Span::new(Face::Mention, "ask#c41e09"),
                plain("."),
            ]),
        ),
        Row::Foot("cooked for 4s · waited 0s"),
        asked(
            "16:40",
            "Add Count with a table test, then a badge on the web client. Check it in the browser.",
        ),
        Row::Tools {
            count: "5 tools",
            summary: "· 4s",
            calls: count_calls(),
            footnote: None,
        },
        said(
            "16:40",
            spoke(vec![
                plain("Two sub-agents, one after the other: go-dev writes "),
                code("Count"),
                plain(
                    " with its table test, then ts-dev reads it for the badge and the browser check.",
                ),
            ]),
        ),
        Row::Agent(member(
            AgentMark::Done,
            "go-dev",
            "added Count, checks passed · 2m 10s",
            Some("open in Sub-agents"),
        )),
        Row::Agent(member(
            AgentMark::Working,
            "ts-dev",
            "badge · step 4 of 9 · 1m 02s",
            Some("open in Sub-agents"),
        )),
        Row::Command {
            busy: true,
            text: "bash rm -rf web/.cache && npm run build",
            verdict: Some(Verdict::Ask),
            tail: Some("12s"),
        },
        Row::Queued("also show the count in the page title"),
        Row::Note("· the queue keeps 1 you typed"),
    ]
}

const REPLAY_FROM: usize = 33;

fn build_question() -> Question {
    Question {
        header: "build".into(),
        text: "".into(),
        shape: Shape::Approval(Ask {
            tool: "bash".into(),
            command: "npm run build".into(),
            writes: "web/dist".into(),
            hint: "the chat still takes what you type".into(),
        }),
    }
}

fn store_question() -> Question {
    Question {
        header: "Store".into(),
        text: "Which store should the port use?".into(),
        shape: Shape::Pick(Pick {
            choices: vec![
                Choice {
                    label: "SQLite".into(),
                    about: Some("one file, the API stays the same".into()),
                },
                Choice {
                    label: "BoltDB".into(),
                    about: Some("key value only, every query becomes a scan".into()),
                },
                Choice {
                    label: "Keep JSON".into(),
                    about: Some("no port, add an index beside it".into()),
                },
            ],
            many: false,
            other: None,
        }),
    }
}

fn check_choices() -> Vec<Choice> {
    vec![
        Choice {
            label: "Unit tests".into(),
            about: Some("go test ./notes/".into()),
        },
        Choice {
            label: "Browser".into(),
            about: Some("the badge, in a real page".into()),
        },
        Choice {
            label: "Lint".into(),
            about: Some("golangci-lint run".into()),
        },
        Choice {
            label: "Bench".into(),
            about: Some("store read and write, 3 runs".into()),
        },
    ]
}

fn checks_question() -> Question {
    Question {
        header: "Checks".into(),
        text: "Which checks should qa run before the merge?".into(),
        shape: Shape::Pick(Pick {
            choices: check_choices(),
            many: true,
            other: None,
        }),
    }
}

fn checks_or_other() -> Question {
    Question {
        header: "Checks".into(),
        text: "Which checks should qa run before the merge?".into(),
        shape: Shape::Pick(Pick {
            choices: check_choices(),
            many: true,
            other: Some("name another check".into()),
        }),
    }
}

fn release_question() -> Question {
    Question {
        header: "Release".into(),
        text: "What should this release be called in the changelog?".into(),
        shape: Shape::Typed("a short name, like note count badge".into()),
    }
}

fn driver_question() -> Question {
    Question {
        header: "Driver".into(),
        text: "Which SQLite driver should go-dev use?".into(),
        shape: Shape::Pick(Pick {
            choices: vec![
                Choice {
                    label: "modernc.org/sqlite".into(),
                    about: Some("pure Go, slower writes".into()),
                },
                Choice {
                    label: "mattn/go-sqlite3".into(),
                    about: Some("fastest, needs cgo and a C compiler".into()),
                },
            ],
            many: false,
            other: Some("name another driver".into()),
        }),
    }
}

fn delete_question() -> Question {
    Question {
    header: "Delete".into(),
    text: "TestDelete fails because a deleted note still counts. There are two ways to fix it and they disagree about what a delete means. A soft delete keeps the row with a deleted_at stamp, so undo and the sync log keep working, but every query that counts or lists notes has to filter it out, and the web client's badge reads Count directly. A hard delete removes the row and the count is right for free, but undo goes away and the sync log has to learn a tombstone record instead. The old JSON store did a hard delete and nothing in the web client ever offered undo. Which way should go-dev take?".into(),
    shape: Shape::Pick(Pick {
        choices: vec![
            Choice {
                label: "Soft delete".into(),
                about: Some("keep the row, filter it everywhere".into()),
            },
            Choice {
                label: "Hard delete".into(),
                about: Some("drop the row, add a tombstone to the sync log".into()),
            },
        ],
        many: false,
        other: None,
    }),
    }
}

fn build() -> Vec<Question> {
    vec![build_question()]
}

fn sequence() -> Vec<Question> {
    vec![store_question(), checks_or_other(), release_question()]
}

const FIELD_LINES: usize = 4;

struct Fold {
    presence: Presence,
    natural: Rc<Cell<f32>>,
    opened: f32,
    measured: f32,
}

struct Shown {
    folds: Vec<Option<Fold>>,
    entered: Vec<Option<Instant>>,
    faded: Vec<f32>,
}

struct Column {
    tag: &'static str,
    width: f32,
    rows: Rc<[Row]>,
    list: ListState,
    shown: Rc<RefCell<Shown>>,
    replay: Option<Instant>,
}

impl Column {
    fn new(tag: &'static str, width: f32, open: bool, rows: Rc<[Row]>) -> Self {
        let since = Instant::now()
            .checked_sub(SETTLED_OPEN)
            .unwrap_or_else(Instant::now);
        let count = rows.len();
        let folds = rows
            .iter()
            .map(|row| {
                row.folds().then(|| {
                    let mut presence = Presence::new(PANEL_IN_MS, PANEL_OUT_MS);
                    presence.set_open(open, since);
                    Fold {
                        presence,
                        natural: Rc::default(),
                        opened: 0.0,
                        measured: 0.0,
                    }
                })
            })
            .collect();
        Column {
            tag,
            width,
            rows,
            list: ListState::new(count, ListAlignment::Top, px(OVERDRAW)),
            shown: Rc::new(RefCell::new(Shown {
                folds,
                entered: vec![None; count],
                faded: vec![1.0; count],
            })),
            replay: None,
        }
    }

    fn replay(&mut self, now: Instant) {
        let count = self.list.item_count();
        self.list.splice(REPLAY_FROM.min(count)..count, 0);
        self.list.set_follow_mode(FollowMode::Tail);
        self.replay = Some(now);
    }

    fn advance(&mut self, now: Instant, reduced: bool, window: &mut Window) {
        let mut shown = self.shown.borrow_mut();
        let mut moving = false;
        if let Some(started) = self.replay {
            let due = REPLAY_FROM
                .saturating_add(
                    usize::try_from(now.duration_since(started).as_millis() / REVEAL.as_millis())
                        .unwrap_or(usize::MAX),
                )
                .min(self.rows.len());
            let count = self.list.item_count();
            if due > count {
                self.list.splice(count..count, due - count);
                for entered in shown.entered.iter_mut().take(due).skip(count) {
                    *entered = Some(now);
                }
            }
            if due == self.rows.len() {
                self.replay = None;
            }
            moving = true;
        }
        let Shown {
            folds,
            entered,
            faded,
        } = &mut *shown;
        for (at, (since, fade)) in entered.iter_mut().zip(faded.iter_mut()).enumerate() {
            let Some(at_time) = *since else { continue };
            let progress = now.duration_since(at_time).as_secs_f32() / PANEL_IN_MS.as_secs_f32();
            *fade = EASE_OUT(progress.min(1.0));
            if progress >= 1.0 {
                *since = None;
            }
            moving |= since.is_some();
            if *fade < 1.0 {
                self.list.remeasure_items(at..at + 1);
            }
        }
        for (at, fold) in folds.iter_mut().enumerate() {
            let Some(fold) = fold else { continue };
            let phase = fold.presence.phase(now);
            let opened = match (reduced, phase) {
                (true, Phase::Opening | Phase::Open) => 1.0,
                (true, Phase::Closing | Phase::Closed) => 0.0,
                (false, _) => fold.presence.progress(now),
            };
            let natural = fold.natural.get();
            if opened != fold.opened || natural != fold.measured {
                self.list.remeasure_items(at..at + 1);
                fold.opened = opened;
                fold.measured = natural;
            }
            moving |= !reduced && matches!(phase, Phase::Opening | Phase::Closing);
        }
        if moving {
            window.request_animation_frame();
        }
    }

    fn frame(
        &self,
        book: EntityId,
        theme: &Theme,
        below: Option<Div>,
        found: (Rc<Vec<Hit>>, usize, bool),
        over: Option<AnyElement>,
    ) -> Div {
        let (shown, conversation) = (self.shown.clone(), self.rows.clone());
        let tag = self.tag;
        let (hits, current, finding) = found;
        let reserve = if finding {
            FIND_RESERVE - COLUMN_TOP
        } else {
            0.0
        };
        let rows = list(self.list.clone(), move |at, _, cx| {
            let theme = ActiveTheme::theme(cx);
            let marks = hit_marks(&hits, current, at, &theme);
            paint(tag, conversation.get(at), at, &marks, &shown, book, &theme)
        })
        .mt(px(reserve))
        .h(px(COLUMN_HEIGHT - reserve))
        .w_full();
        let narrow = self.width < BOARD_WIDTH;
        div()
            .w(px(self.width))
            .flex()
            .flex_col()
            .rounded(px(INNER_RADIUS))
            .overflow_hidden()
            .bg(theme.color(ColorToken::CardsInnerFill))
            .child(
                div()
                    .pt(px(COLUMN_TOP))
                    .pb(px(COLUMN_BOTTOM))
                    .when(narrow, |column| column.px(px(NARROW_PAD)))
                    .relative()
                    .child(rows)
                    .children(over),
            )
            .child(phase_line((self.tag, u64::MAX), false, "3m 20s", theme))
            .children(below.map(|ask| div().px(px(ASK_MARGIN)).pb(px(ASK_MARGIN)).child(ask)))
    }
}

fn opened_of(shown: &Shown, at: usize) -> (f32, Rc<Cell<f32>>) {
    match shown.folds.get(at) {
        Some(Some(fold)) => (fold.opened, fold.natural.clone()),
        _ => (0.0, Rc::default()),
    }
}

fn folded(
    header: gpui::Stateful<Div>,
    content: Div,
    at: usize,
    shown: &Rc<RefCell<Shown>>,
    book: EntityId,
) -> Div {
    let (opened, natural) = opened_of(&shown.borrow(), at);
    let flip = shown.clone();
    div()
        .flex()
        .flex_col()
        .child(header.on_click(move |_, _, cx: &mut App| {
            if let Some(Some(fold)) = flip.borrow_mut().folds.get_mut(at) {
                toggle(&mut fold.presence);
            }
            cx.notify(book);
        }))
        .child(folding(natural, opened, content))
}

fn paint(
    tag: &'static str,
    row: Option<&Row>,
    at: usize,
    marks: &[Marks],
    shown: &Rc<RefCell<Shown>>,
    book: EntityId,
    theme: &Theme,
) -> AnyElement {
    let id = (tag, at);
    let (opened, _) = opened_of(&shown.borrow(), at);
    let fade = shown.borrow().faded.get(at).copied().unwrap_or(1.0);
    let row = match row {
        Some(Row::You {
            time,
            text,
            traced,
            attached,
        }) => you(
            *time,
            *text,
            *traced,
            attached.map(Into::into),
            at == 0,
            marks.first().map_or(&[][..], Vec::as_slice),
            theme,
        ),
        Some(Row::Queued(text)) => queued(*text, theme),
        Some(Row::Lead { time, body }) => lead(id, *time, body, marks, theme),
        Some(Row::Tools {
            count,
            summary,
            calls: rows,
            footnote,
        }) => folded(
            tools(id, *count, *summary, opened, theme),
            calls(rows, footnote.map(Into::into), theme),
            at,
            shown,
            book,
        ),
        Some(Row::Agents {
            count,
            summary,
            agents: lines,
        }) => folded(
            agents(id, *count, *summary, opened, theme),
            agent_list(tag, lines, theme),
            at,
            shown,
            book,
        ),
        Some(Row::Agent(line)) => agent_row(id, line, theme),
        Some(Row::Cron {
            job,
            schedule,
            prompt,
        }) => cron_row(*job, *schedule, *prompt, theme),
        Some(Row::Command {
            busy,
            text,
            verdict,
            tail,
        }) => command(
            id,
            *busy,
            *text,
            verdict.clone(),
            tail.map(Into::into),
            theme,
        ),
        Some(Row::Fail { text, detail, link }) => fail(*text, *detail, *link, theme),
        Some(Row::Note(text)) => note(*text, theme),
        Some(Row::Foot(text)) => foot(*text, theme),
        None => div(),
    };
    row.opacity(fade).into_any_element()
}

fn faded(moved: Option<Instant>, now: Instant, window: &mut Window) -> f32 {
    let Some(at) = moved else { return 1.0 };
    let progress = now.duration_since(at).as_secs_f32() / TOGGLE_MS.as_secs_f32();
    if progress < 1.0 {
        window.request_animation_frame();
    }
    EASE_OUT(progress.min(1.0))
}

struct Stream {
    transcript: Transcript,
    count: usize,
    said: String,
}

impl Stream {
    fn append(&mut self) {
        self.transcript
            .splice(self.count..self.count, STREAM_APPEND);
        self.count += STREAM_APPEND;
    }

    fn readout(&self) -> String {
        let shown = self.transcript.visible();
        let pin = if self.transcript.pinned() {
            "pinned"
        } else {
            "not pinned"
        };
        format!(
            "{} rows · {pin} · rows {} to {} visible",
            self.count, shown.start, shown.end
        )
    }
}

fn streamed(at: usize, theme: &Theme) -> AnyElement {
    let (minute, second) = ((at / 60) % 60, at % 60);
    let time = format!("15:{minute:02}:{second:02}");
    match at % 5 {
        0 => you(
            time,
            format!("row {at}: keep going"),
            false,
            None,
            at == 0,
            &[],
            theme,
        ),
        1 => command(
            ("chat-stream", at),
            false,
            format!("read internal/turn/part_{at}.go"),
            None,
            Some(format!("{} lines", 40 + at % 200).into()),
            theme,
        ),
        2 => lead(
            ("chat-stream-lead", at),
            time,
            &[Block::Para(vec![
                Span::new(Face::Plain, format!("row {at}: the lead read ")),
                Span::new(Face::Code, format!("part_{at}.go")),
                Span::new(Face::Plain, " and moved on."),
            ])],
            &[],
            theme,
        ),
        3 => note(format!("· row {at} kept in sub-agents"), theme),
        _ => foot(format!("row {at} · cooked for {second}s"), theme),
    }
    .into_any_element()
}

pub(super) struct ChatPage {
    book: EntityId,
    wide: Column,
    narrow: Column,
    asking: Rc<RefCell<Asking>>,
    stream: Rc<RefCell<Stream>>,
    find: Entity<FindBar>,
    hits: Rc<Vec<Hit>>,
    current: usize,
    finding: bool,
}

impl ChatPage {
    pub(super) fn new(window: &mut Window, cx: &mut Context<Book>) -> Self {
        let rows: Rc<[Row]> = conversation().into();
        let find = FindBar::new(window, cx);
        let changed = cx.listener(|book, (query, current): &(String, usize), _, cx| {
            book.chat.found(query, *current, cx);
            cx.notify();
        });
        let closed = cx.listener(|book, _: &(), _, cx| {
            book.chat.finding = false;
            book.chat.hits = Rc::default();
            cx.notify();
        });
        find.update(cx, |bar, _| {
            bar.on_change(move |query, current, window, cx| {
                changed(&(query.to_owned(), current), window, cx);
            });
            bar.on_close(move |window, cx| closed(&(), window, cx));
        });
        ChatPage {
            book: cx.entity_id(),
            wide: Column::new("chat-board", BOARD_WIDTH, false, rows.clone()),
            narrow: Column::new("chat-narrow", NARROW_WIDTH, true, rows),
            asking: Rc::new(RefCell::new(Asking::new(build()))),
            stream: Rc::new(RefCell::new(Stream {
                transcript: Transcript::new(STREAM_ROWS).content_width(px(CONTENT_WIDTH)),
                count: STREAM_ROWS,
                said: String::new(),
            })),
            find,
            hits: Rc::default(),
            current: 0,
            finding: false,
        }
    }

    fn open_find(&mut self, window: &mut Window, cx: &mut Context<Book>) {
        self.finding = true;
        self.find.update(cx, |bar, cx| bar.open(window, cx));
    }

    fn found(&mut self, query: &str, current: usize, cx: &mut Context<Book>) {
        self.hits = Rc::new(find_hits(self.wide.rows.iter().map(Row::pieces), query));
        self.current = current;
        let total = self.hits.len();
        self.find.update(cx, |bar, cx| bar.set_total(total, cx));
        if let Some(hit) = self.hits.get(current) {
            self.wide.list.scroll_to_reveal_item(hit.item);
        }
    }

    fn find_control(&self, theme: &Theme, cx: &mut Context<Book>) -> AnyElement {
        if self.finding {
            return self.find.clone().into_any_element();
        }
        div()
            .absolute()
            .top(px(MENU_PAD))
            .right(px(MENU_PAD))
            .child(
                button("chat-find", "Find", None, ButtonKind::Text, theme)
                    .child(kbd("Ctrl F", theme))
                    .on_click(cx.listener(|book, _: &ClickEvent, window, cx| {
                        book.chat.open_find(window, cx);
                    })),
            )
            .into_any_element()
    }

    fn stream(&self, theme: &Theme, window: &mut Window) -> Div {
        let (stream, book) = (self.stream.clone(), self.book);
        let mut held = self.stream.borrow_mut();
        let readout = held.readout();
        if held.said != readout {
            held.said.clone_from(&readout);
            window.request_animation_frame();
        }
        div()
            .w(px(BOARD_WIDTH))
            .flex()
            .flex_col()
            .gap_3()
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_3()
                    .child(
                        button(
                            "chat-stream-append",
                            format!("Append {STREAM_APPEND}"),
                            None,
                            ButtonKind::Plain,
                            theme,
                        )
                        .on_click(move |_, _, cx| {
                            stream.borrow_mut().append();
                            cx.notify(book);
                        }),
                    )
                    .child(label(readout, theme)),
            )
            .child(
                div()
                    .h(px(COLUMN_HEIGHT))
                    .flex()
                    .flex_col()
                    .rounded(px(INNER_RADIUS))
                    .overflow_hidden()
                    .bg(theme.color(ColorToken::CardsInnerFill))
                    .pt(px(COLUMN_TOP))
                    .pb(px(COLUMN_BOTTOM))
                    .child(transcript(
                        "chat-stream",
                        &held.transcript,
                        theme,
                        |at, _, cx| streamed(at, &ActiveTheme::theme(cx)),
                    )),
            )
    }

    pub(super) fn render(
        &mut self,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Book>,
    ) -> Div {
        let now = Instant::now();
        let reduced = reduced_motion(cx);
        self.wide.advance(now, reduced, window);
        self.narrow.advance(now, reduced, window);
        let (asking, book) = (self.asking.clone(), self.book);
        let held = self.asking.borrow();
        let ask = ask_bar(
            "chat-ask",
            &held,
            None,
            faded(held.moved(), now, window),
            true,
            move |act, _, cx| {
                asking.borrow_mut().act(act, "", Instant::now());
                cx.notify(book);
            },
            theme,
        );
        drop(held);
        let found = (self.hits.clone(), self.current, self.finding);
        let control = self.find_control(theme, cx);
        div()
            .flex()
            .flex_wrap()
            .items_start()
            .gap(px(PAGE_GAP))
            .child(block(
                "board width: click 7 tools or 35 sub-agents; r replays the last turn; 1, 2, 3 answer",
                theme,
                self.wide
                    .frame(self.book, theme, Some(ask), found, Some(control)),
            ))
            .child(block(
                "320 px, folds held open",
                theme,
                self.narrow
                    .frame(self.book, theme, None, (Rc::default(), 0, false), None),
            ))
            .child(block(
                "transcript: 2000 generated rows; a appends 500; scroll up to unpin, back to the bottom to pin",
                theme,
                self.stream(theme, window),
            ))
    }

    pub(super) fn key(
        &mut self,
        event: &KeyDownEvent,
        window: &mut Window,
        cx: &mut Context<Book>,
    ) -> bool {
        let keys = &event.keystroke;
        let modifiers = keys.modifiers;
        let ctrl_f = keys.key == "f"
            && modifiers.control
            && !modifiers.alt
            && !modifiers.shift
            && !modifiers.platform;
        if ctrl_f {
            self.open_find(window, cx);
            return true;
        }
        if self.finding {
            return false;
        }
        let key = keys.key.as_str();
        if key == "a" {
            self.stream.borrow_mut().append();
            return true;
        }
        if key == "r" {
            let now = Instant::now();
            self.wide.replay(now);
            self.narrow.replay(now);
            return true;
        }
        let mut asking = self.asking.borrow_mut();
        if key == "0" {
            asking.reset(Instant::now());
            return true;
        }
        let Some(act) = asking.key(key) else {
            return false;
        };
        asking.act(act, "", Instant::now());
        true
    }
}

struct Card {
    id: &'static str,
    title: &'static str,
    width: f32,
    phased: bool,
    asking: Asking,
    field: Option<Entity<TextArea>>,
    typed: String,
}

impl Card {
    fn new(id: &'static str, title: &'static str, width: f32, questions: Vec<Question>) -> Self {
        Card {
            id,
            title,
            width,
            phased: false,
            asking: Asking::new(questions),
            field: None,
            typed: String::new(),
        }
    }

    fn phased(self) -> Self {
        Card {
            phased: true,
            ..self
        }
    }
}

struct Board {
    cards: Vec<Card>,
    active: usize,
    aimed: Option<(usize, Option<EntityId>)>,
}

impl Board {
    fn act(&mut self, at: usize, act: Act) {
        self.active = at;
        let Some(card) = self.cards.get_mut(at) else {
            return;
        };
        if card.asking.act(act, &card.typed, Instant::now()) {
            card.field = None;
            card.typed.clear();
        }
    }

    fn key(&mut self, key: &str) -> bool {
        if key == "tab" {
            self.active = (self.active + 1)
                .checked_rem(self.cards.len())
                .unwrap_or_default();
            return true;
        }
        let at = self.active;
        let Some(card) = self.cards.get_mut(at) else {
            return false;
        };
        if key == "0" && !card.asking.typing() {
            card.asking.reset(Instant::now());
            return true;
        }
        let Some(act) = card.asking.key(key) else {
            return false;
        };
        self.act(at, act);
        true
    }
}

fn field(
    board: &Rc<RefCell<Board>>,
    book: EntityId,
    at: usize,
    placeholder: SharedString,
    window: &mut Window,
    cx: &mut Context<Book>,
) -> Entity<TextArea> {
    let board = Rc::downgrade(board);
    let field = cx.new(|cx| {
        TextArea::new(placeholder, window, cx)
            .bare()
            .max_lines(FIELD_LINES)
            .on_submit(move |text, _, cx| {
                let Some(board) = board.upgrade() else {
                    return;
                };
                let mut board = board.borrow_mut();
                if let Some(card) = board.cards.get_mut(at) {
                    card.typed = text.to_owned();
                }
                board.act(at, Act::Send);
                cx.notify(book);
            })
    });
    cx.observe(&field, |_, _, cx| cx.notify()).detach();
    field
}

pub(super) struct AskPage {
    book: EntityId,
    board: Rc<RefCell<Board>>,
}

impl AskPage {
    pub(super) fn new(cx: &mut Context<Book>) -> Self {
        let cards = vec![
            Card::new(
                "ask-approval",
                "Approval: 1, 2 or 3, or click",
                BOARD_WIDTH,
                vec![build_question()],
            )
            .phased(),
            Card::new(
                "ask-pick",
                "Single pick: a number, or up, down and enter, or click",
                BOARD_WIDTH,
                vec![store_question()],
            ),
            Card::new(
                "ask-many",
                "Multi pick: a number or space ticks, enter or Submit sends",
                BOARD_WIDTH,
                vec![checks_question()],
            ),
            Card::new(
                "ask-typed",
                "Typed answer: type, enter sends, shift enter starts a new line",
                BOARD_WIDTH,
                vec![release_question()],
            ),
            Card::new(
                "ask-other",
                "Other: 3 or a click on Other opens the field, esc goes back",
                BOARD_WIDTH,
                vec![driver_question()],
            ),
            Card::new(
                "ask-long",
                "Long question: clamped to 3 lines, e or more opens it",
                BOARD_WIDTH,
                vec![delete_question()],
            ),
            Card::new(
                "ask-sequence",
                "1 of 3: each answer moves to the next question",
                BOARD_WIDTH,
                sequence(),
            )
            .phased(),
            Card::new(
                "ask-approval-narrow",
                "Approval at 320 px",
                NARROW_WIDTH,
                vec![build_question()],
            ),
            Card::new(
                "ask-pick-narrow",
                "Single pick at 320 px",
                NARROW_WIDTH,
                vec![store_question()],
            ),
            Card::new(
                "ask-long-narrow",
                "Long question at 320 px",
                NARROW_WIDTH,
                vec![delete_question()],
            ),
        ];
        AskPage {
            book: cx.entity_id(),
            board: Rc::new(RefCell::new(Board {
                cards,
                active: 0,
                aimed: None,
            })),
        }
    }

    fn aim(&self, window: &mut Window, cx: &mut Context<Book>) {
        let mut board = self.board.borrow_mut();
        let field = board
            .cards
            .get(board.active)
            .and_then(|card| card.field.clone());
        let aim = (board.active, field.as_ref().map(Entity::entity_id));
        if board.aimed == Some(aim) {
            return;
        }
        board.aimed = Some(aim);
        match field {
            Some(field) => window.focus(&field.focus_handle(cx), cx),
            None => {
                let home = cx.entity();
                window.defer(cx, move |window, cx| {
                    let focus = home.read(cx).focus.clone();
                    window.focus(&focus, cx);
                });
            }
        }
    }

    pub(super) fn render(
        &mut self,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Book>,
    ) -> Div {
        let now = Instant::now();
        let book = self.book;
        let mut board = self.board.borrow_mut();
        for (at, card) in board.cards.iter_mut().enumerate() {
            match (card.asking.placeholder(), &card.field) {
                (None, Some(_)) => {
                    card.field = None;
                    card.typed.clear();
                }
                (Some(placeholder), None) => {
                    card.field = Some(field(&self.board, book, at, placeholder, window, cx));
                }
                _ => {}
            }
            if let Some(field) = &card.field {
                card.typed = field.read(cx).text();
            }
        }
        drop(board);
        self.aim(window, cx);
        let board = self.board.borrow();
        let mut page = div().flex().flex_col().gap_3().child(label(
            "tab moves the keyboard to the next bar and a click picks one; 0 asks the active bar again",
            theme,
        ));
        for (at, card) in board.cards.iter().enumerate() {
            let (acting, clicked) = (self.board.clone(), self.board.clone());
            let bar = ask_bar(
                card.id,
                &card.asking,
                card.field.clone().map(Entity::into_any_element),
                faded(card.asking.moved(), now, window),
                at == board.active,
                move |act, _, cx| {
                    acting.borrow_mut().act(at, act);
                    cx.notify(book);
                },
                theme,
            );
            let lined = div()
                .flex()
                .flex_col()
                .gap_2()
                .min_w_0()
                .when(card.phased, |lined| {
                    lined.child(phase_line(
                        (card.id, 0u64),
                        card.asking.waiting(),
                        "22m",
                        theme,
                    ))
                })
                .child(bar);
            page = page.child(block(
                card.title,
                theme,
                div()
                    .w(px(card.width))
                    .min_w_0()
                    .on_mouse_down(MouseButton::Left, move |_, _, cx| {
                        clicked.borrow_mut().active = at;
                        cx.notify(book);
                    })
                    .child(lined),
            ));
        }
        page
    }

    pub(super) fn key(&mut self, key: &str) -> bool {
        self.board.borrow_mut().key(key)
    }
}
