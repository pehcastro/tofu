use super::paint::{DEL, T3, WARN, white};
use super::rows::{Block, Mark, Outcome, Row, code, plain};

pub fn four_turns() -> Vec<Row> {
    vec![
        Row::You {
            time: "14:32".into(),
            text: "why does the gate read the policy first?".into(),
            trace: true,
        },
        Row::Tools {
            count: "7 tools".into(),
            detail: "· classifier 1 · shell 1 · 16s".into(),
        },
        Row::Lead {
            time: "14:32".into(),
            body: vec![
                Block::Para(vec![plain(
                    "The gate reads the policy first so that a verdict is never invented.",
                )]),
                Block::Heading("What the policy carries".into()),
                Block::List(vec![
                    "the mode it was declared in".into(),
                    "the lock that resolved it".into(),
                    "the thresholds".into(),
                ]),
                Block::Para(vec![
                    plain("None of the three is knowable from the wire alone, so "),
                    code("tool_gate"),
                    plain(" waits for "),
                    code("policy.Resolve"),
                    plain(" before it answers."),
                ]),
            ],
        },
        Row::Foot("cooked for 16s · waited 0s".into()),
        Row::You {
            time: "14:36".into(),
            text: "check the loader with a sub-agent, I want a second read".into(),
            trace: false,
        },
        Row::Lead {
            time: "14:36".into(),
            body: vec![Block::Para(vec![plain("Handing the policy loader to qa.")])],
        },
        Row::Agent {
            outcome: Outcome::Done,
            name: "qa".into(),
            summary: "read 4 files, reported · 1m 18s".into(),
            link: true,
        },
        Row::Lead {
            time: "14:38".into(),
            body: vec![Block::Para(vec![plain(
                "qa read it the same way: the loader reads the lock before the mode, and a missing lock is an error, never a default.",
            )])],
        },
        Row::Foot("cooked for 1m 24s · waited 0s".into()),
        Row::You {
            time: "14:41".into(),
            text: "push it".into(),
            trace: false,
        },
        Row::Command {
            mark: Mark::Prompt,
            command: "bash git push origin develop".into(),
            tail: vec![("exit 128".into(), DEL)],
        },
        Row::Failure {
            head: "git push origin develop:".into(),
            detail: "fatal: could not read Username for 'https://github.com'".into(),
            link: "whole error in work".into(),
        },
        Row::Lead {
            time: "14:41".into(),
            body: vec![Block::Para(vec![
                plain("There is no credential for that remote here. Run "),
                code("gh auth login"),
                plain(" once and I will push again."),
            ])],
        },
        Row::Foot("cooked for 6s · waited 0s".into()),
        Row::You {
            time: "14:44".into(),
            text: "read the whole loop file and summarize it".into(),
            trace: false,
        },
        Row::Command {
            mark: Mark::Prompt,
            command: "read internal/turn/loop.go".into(),
            tail: vec![("84 lines, 2.1 KB".into(), white(T3))],
        },
        Row::Note("· you stopped the turn".into()),
        Row::Note("· 64 characters were written and kept in sub-agents".into()),
        Row::Foot("stopped after 3s".into()),
    ]
}

pub fn running_turn() -> Vec<Row> {
    vec![
        Row::You {
            time: "16:40".into(),
            text: "Add Count with a table test, then a badge on the web client. Check it in the browser."
                .into(),
            trace: false,
        },
        Row::Tools {
            count: "5 tools".into(),
            detail: "· 4s".into(),
        },
        Row::Lead {
            time: "16:40".into(),
            body: vec![Block::Para(vec![
                plain("Two sub-agents, one after the other: go-dev writes "),
                code("Count"),
                plain(" with its table test, then ts-dev reads it for the badge and the browser check."),
            ])],
        },
        Row::Agent {
            outcome: Outcome::Done,
            name: "go-dev".into(),
            summary: "added Count, checks passed · 2m 10s".into(),
            link: false,
        },
        Row::Agent {
            outcome: Outcome::Running,
            name: "ts-dev".into(),
            summary: "badge · step 4 of 9 · 1m 02s".into(),
            link: false,
        },
        Row::Command {
            mark: Mark::Running,
            command: "bash rm -rf web/.cache && npm run build".into(),
            tail: vec![("ask".into(), WARN), ("12s".into(), white(T3))],
        },
        Row::Queued {
            text: "also show the count in the page title".into(),
        },
    ]
}
