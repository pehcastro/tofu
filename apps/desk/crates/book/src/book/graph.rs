use desk_ui::components::fork_chain::{Fork, ForkChain, Generation};
use desk_ui::components::graph::{
    AgentNode, Change, Cross, DelegationMap, FileLeaf, Fit, Leaves, Node, Tone,
};
use desk_ui::theme::Theme;
use gpui::{Context, Div, SharedString, Window, div, prelude::*, px};

use super::Book;
use super::kit::label;

const NARROW_WIDTH: f32 = 320.0;
const QA_SPEC: (usize, usize) = (2, 0);
const GO_STORE: (usize, usize) = (0, 0);
const STATES: [(&str, Option<Node>, bool); 3] = [
    (
        "nothing picked: hover a node to light its edges",
        None,
        false,
    ),
    (
        "qa picked: the cross link to store.go shows",
        Some(Node::Agent(2)),
        false,
    ),
    ("store.go picked and undone", Some(Node::File(0, 0)), true),
];

fn file(path: &str, add: &str, del: &str, change: Change, undone: bool) -> FileLeaf {
    FileLeaf {
        path: SharedString::from(path.to_string()),
        add: SharedString::from(add.to_string()),
        del: SharedString::from(del.to_string()),
        change,
        undone,
    }
}

fn agent(name: &str, meta: &str, tone: Tone, owns: &str, leaves: Leaves) -> AgentNode {
    AgentNode {
        name: SharedString::from(name.to_string()),
        meta: SharedString::from(meta.to_string()),
        tone,
        owns: SharedString::from(owns.to_string()),
        leaves,
    }
}

fn agents(store_undone: bool) -> Vec<AgentNode> {
    vec![
        agent(
            "go-dev",
            "7 of 12",
            Tone::Live,
            "notes/**  migrations/**",
            Leaves::Files(vec![
                file(
                    "notes/store.go",
                    "+48",
                    "-21",
                    Change::Modified,
                    store_undone,
                ),
                file("notes/sqlite.go", "+112", "", Change::New, false),
                file("migrations/0008_sqlite.sql", "+19", "", Change::New, false),
            ]),
        ),
        agent(
            "ts-dev",
            "5 of 7",
            Tone::Live,
            "web/**",
            Leaves::Files(vec![
                file("web/api.ts", "+4", "-1", Change::Modified, false),
                file("web/NoteList.tsx", "+7", "-2", Change::Modified, false),
            ]),
        ),
        agent(
            "qa",
            "1 failed",
            Tone::Failed,
            "e2e/**",
            Leaves::Files(vec![file(
                "e2e/store.spec.ts",
                "+36",
                "",
                Change::New,
                false,
            )]),
        ),
        agent(
            "research",
            "done",
            Tone::Done,
            "nothing, read only",
            Leaves::Reads("14 reads · 3 pages · 0 writes".into()),
        ),
    ]
}

fn map(id: String, theme: &Theme, picked: Option<Node>, undone: bool, fit: Fit) -> DelegationMap {
    let map = DelegationMap::new(
        id,
        theme,
        ("lead".into(), "Opus 5 · waiting".into()),
        agents(undone),
    )
    .cross(Cross {
        from: QA_SPEC,
        to: GO_STORE,
    })
    .fit(fit);
    match picked {
        Some(node) => map.picked(node),
        None => map,
    }
}

fn generation(
    name: &str,
    at: &str,
    place: &str,
    (current, head): (bool, bool),
    facts: &[(&str, &str)],
    into: Option<Fork>,
) -> Generation {
    Generation {
        name: SharedString::from(name.to_owned()),
        handle: Some(SharedString::from(format!("{name}#06r84.{at}"))),
        place: SharedString::from(place.to_owned()),
        current,
        head,
        facts: facts
            .iter()
            .map(|(label, value)| {
                (
                    SharedString::from((*label).to_owned()),
                    SharedString::from((*value).to_owned()),
                )
            })
            .collect(),
        into,
    }
}

fn compact(tokens: &str, fill: (f32, f32)) -> Option<Fork> {
    Some(Fork {
        kind: "compact".into(),
        tokens: Some(SharedString::from(tokens.to_owned())),
        fill: Some(fill),
    })
}

fn chains() -> [(&'static str, Vec<Generation>); 2] {
    let ended = [
        ("turns", "9"),
        ("steps", "31"),
        ("outcome", "stopped"),
        ("end reason", "fork"),
        ("started", "2026-10-08 05:37:49"),
        ("ended", "2026-10-08 10:27:49"),
    ];
    [
        (
            "three generations, the second open, the third the head",
            vec![
                generation(
                    "crisp-azure-swift",
                    "1",
                    "generation 1 of 3",
                    (false, false),
                    &ended,
                    None,
                ),
                generation(
                    "crisp-azure-swift",
                    "2",
                    "generation 2 of 3",
                    (true, false),
                    &ended,
                    compact("238,000 → 31,000 tokens, 207,000 dropped", (0.95, 0.12)),
                ),
                generation(
                    "crisp-azure-swift",
                    "3",
                    "generation 3 of 3",
                    (false, true),
                    &[
                        ("turns", "0"),
                        ("steps", "0"),
                        ("started", "2026-10-08 10:27:49"),
                    ],
                    compact("181,000 → 12,000 tokens, 169,000 dropped", (0.72, 0.05)),
                ),
            ],
        ),
        (
            "never forked: one generation",
            vec![generation(
                "rapid-linen-swift",
                "1",
                "generation 1",
                (true, true),
                &[
                    ("turns", "1"),
                    ("steps", "2"),
                    ("outcome", "stopped"),
                    ("started", "2026-10-08 05:37:49"),
                ],
                None,
            )],
        ),
    ]
}

pub(super) struct GraphPage;

impl GraphPage {
    pub(super) fn new(_: &mut Context<Book>) -> Self {
        GraphPage
    }

    pub(super) fn render(&mut self, theme: &Theme, _: &mut Window, _: &mut Context<Book>) -> Div {
        let column = |caption: &'static str, fit: Fit, width: Option<f32>| {
            let maps = STATES
                .iter()
                .enumerate()
                .map(|(ix, (state, picked, undone))| {
                    div()
                        .flex()
                        .flex_col()
                        .gap_2()
                        .min_w_0()
                        .child(label(*state, theme))
                        .child(map(
                            format!("delegation-{caption}-{ix}"),
                            theme,
                            *picked,
                            *undone,
                            fit,
                        ))
                });
            div()
                .flex()
                .flex_col()
                .gap_4()
                .min_w_0()
                .flex_none()
                .when_some(width, |frame, width| frame.w(px(width)))
                .child(label(caption, theme))
                .children(maps)
        };
        div()
            .flex()
            .flex_col()
            .gap_3()
            .child(label(
                "Hover a node to light its edges. Click a node to pick it; click again to let go.",
                theme,
            ))
            .child(
                div()
                    .flex()
                    .items_start()
                    .gap_6()
                    .child(column("board width", Fit::Board, None))
                    .child(column("320 px", Fit::Outline, Some(NARROW_WIDTH))),
            )
            .child(label(
                "Fork chain: one card per generation, root first. The ribbon between two is the context carried across the fork, before above and after below, against the ceiling.",
                theme,
            ))
            .child(div().flex().items_start().gap_6().children(
                [None, Some(NARROW_WIDTH)].into_iter().map(|width| {
                    div()
                        .flex()
                        .flex_col()
                        .gap_4()
                        .min_w_0()
                        .map(|frame| match width {
                            Some(width) => frame.flex_none().w(px(width)),
                            None => frame.flex_1(),
                        })
                        .children(chains().into_iter().map(|(state, generations)| {
                            div()
                                .flex()
                                .flex_col()
                                .gap_2()
                                .min_w_0()
                                .child(label(state, theme))
                                .child(ForkChain::new(theme, generations))
                        }))
                }),
            ))
    }

    pub(super) fn key(&mut self, _: &str) -> bool {
        false
    }
}
