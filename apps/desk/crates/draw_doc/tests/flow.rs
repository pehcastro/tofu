use std::collections::HashMap;

use draw_doc::{Direction, DocError, Element, Line, Look, NodeKind, Scene, flow, flow_scene};

const OWNER: &str = "flowchart TD
    start([:play: Start]) --> license[:file: Get license]
    subgraph attempt [Try/Catch]
        research[:search: Research license]
        review[:eye: Review manually]
    end
    license -->|Try| research
    license -->|Catch| review
    research --> eligible{:help: Eligible?}
    review --> eligible
    eligible -->|Yes| issue[Issue license]
    eligible -->|No| refuse[Explain refusal]
    issue --> approval[:mail: Send approval email]
    refuse --> rejection[:mail: Send rejection email]
    approval & rejection --> followup[:mail: Send follow-up email]
    followup --> record[Record outcome] --> close[Close case] --> done([End])
";

fn line_of(text: &str) -> Option<usize> {
    match flow(text) {
        Err(DocError::Flow { line, .. }) => Some(line),
        _ => None,
    }
}

#[test]
fn the_owner_example_parses_to_thirteen_nodes() -> Result<(), DocError> {
    let parsed = flow(OWNER)?;
    let graph = &parsed.graph;
    assert_eq!(parsed.direction, Direction::Down);
    assert_eq!(graph.nodes.len(), 13, "{:?}", graph.nodes);
    assert_eq!(graph.edges.len(), 14);
    let node = |key: &str| graph.nodes.iter().find(|node| node.key == key);
    let start = node("start").ok_or(DocError::UnknownKey("start".into()))?;
    assert_eq!(
        (start.label.as_str(), start.kind, start.icon.as_deref()),
        ("Start", NodeKind::Terminal, Some("play"))
    );
    let eligible = node("eligible").ok_or(DocError::UnknownKey("eligible".into()))?;
    assert_eq!(eligible.kind, NodeKind::Decision);
    assert_eq!(eligible.label, "Eligible?");
    for key in ["research", "review"] {
        assert_eq!(
            node(key).and_then(|node| node.group.as_deref()),
            Some("Try/Catch"),
            "{key}"
        );
    }
    assert_eq!(node("license").and_then(|node| node.group.clone()), None);
    let labels: Vec<_> = graph
        .edges
        .iter()
        .filter_map(|edge| edge.label.as_deref())
        .collect();
    assert_eq!(labels, ["Try", "Catch", "Yes", "No"]);
    assert_eq!(graph.nodes[0].key, "start", "nodes keep first-seen order");
    Ok(())
}

#[test]
fn a_label_set_after_a_bare_use_wins_and_keeps_first_seen_order() -> Result<(), DocError> {
    let graph = flow("a --> b\nb[Bee]\nb{Bee?}")?.graph;
    let keys: Vec<_> = graph.nodes.iter().map(|node| node.key.as_str()).collect();
    assert_eq!(keys, ["a", "b"]);
    assert_eq!(graph.nodes[0].label, "a");
    assert_eq!(graph.nodes[1].label, "Bee?");
    assert_eq!(graph.nodes[1].kind, NodeKind::Decision);
    Ok(())
}

#[test]
fn quoted_labels_keep_brackets_and_semicolons() -> Result<(), DocError> {
    let graph = flow("a[\"x [y]; z\"] --> b(Run (fast)); b --> c")?.graph;
    assert_eq!(graph.nodes[0].label, "x [y]; z");
    assert_eq!(graph.nodes[1].label, "Run (fast)");
    assert_eq!(graph.nodes[1].kind, NodeKind::Step);
    assert_eq!(graph.edges.len(), 2);
    Ok(())
}

#[test]
fn both_link_label_forms_chains_and_fans() -> Result<(), DocError> {
    let graph = flow("a -- go on --> b -->|then| c\nx & y --> z & w")?.graph;
    let pairs: Vec<_> = graph
        .edges
        .iter()
        .map(|edge| (edge.from.as_str(), edge.to.as_str(), edge.label.as_deref()))
        .collect();
    assert_eq!(
        pairs,
        [
            ("a", "b", Some("go on")),
            ("b", "c", Some("then")),
            ("x", "z", None),
            ("x", "w", None),
            ("y", "z", None),
            ("y", "w", None),
        ]
    );
    Ok(())
}

#[test]
fn dotted_links_are_dashed_and_long_arrows_are_arrows() -> Result<(), DocError> {
    let graph = flow("a -.-> b\nb ---> c\nc -.label.-> d")?.graph;
    let lines: Vec<_> = graph.edges.iter().map(|edge| edge.line).collect();
    assert_eq!(lines, [Line::Dashed, Line::Solid, Line::Dashed]);
    assert_eq!(graph.edges[2].label.as_deref(), Some("label"));
    Ok(())
}

#[test]
fn an_icon_needs_both_colons_and_a_space() -> Result<(), DocError> {
    let graph = flow("a[Ratio: 3] --> b[:bad] --> c[:file: Doc]")?.graph;
    let seen: Vec<_> = graph
        .nodes
        .iter()
        .map(|node| (node.label.as_str(), node.icon.as_deref()))
        .collect();
    assert_eq!(
        seen,
        [("Ratio: 3", None), (":bad", None), ("Doc", Some("file"))]
    );
    Ok(())
}

#[test]
fn the_mermaid_export_header_reads_back_left_to_right() -> Result<(), DocError> {
    let parsed = flow("graph LR\n%% a comment\n  n1[\"A\"] --> n2((\"B\"))\n")?;
    assert_eq!(parsed.direction, Direction::Right);
    assert_eq!(parsed.graph.nodes[1].kind, NodeKind::Terminal);
    Ok(())
}

#[test]
fn every_broken_input_names_its_line() {
    let cases: [(&str, &str, usize); 16] = [
        ("empty", "", 0),
        ("only a header", "flowchart TD\n", 0),
        ("unknown direction", "flowchart XY\na --> b", 1),
        ("bottom to top", "flowchart BT\na --> b", 1),
        ("unclosed bracket", "a --> b\nc[Get license", 2),
        ("mismatched bracket", "a[Label}", 1),
        (
            "nested subgraph",
            "subgraph one\nsubgraph two\na\nend\nend",
            2,
        ),
        ("end with no subgraph", "a --> b\nend", 2),
        ("subgraph left open", "subgraph one\na --> b", 1),
        ("styling", "a --> b\nclassDef hot fill:#f00", 2),
        ("click", "a --> b\nclick a call", 2),
        ("open line", "a --- b", 1),
        ("thick link", "a ==> b", 1),
        ("dangling link", "a -->", 1),
        ("link with no source", "--> b", 1),
        ("two ids with no link", "a b --> c", 1),
    ];
    for (name, text, line) in cases {
        assert_eq!(line_of(text), Some(line), "{name}: {:?}", flow(text).err());
    }
    assert_eq!(line_of("a --> b\nflowchart TD"), Some(2), "a late header");
}

fn scene_of(text: &str, look: Look) -> Result<Scene, DocError> {
    flow_scene(&flow(text)?, look)
}

fn by_key(scene: &Scene) -> HashMap<String, Element> {
    scene
        .elements()
        .iter()
        .filter_map(|element| {
            let key = element.extra.get("customData")?.get("key")?.as_str()?;
            Some((key.to_owned(), element.clone()))
        })
        .collect()
}

fn word<'a>(element: &'a Element, field: &str) -> Option<&'a str> {
    element.extra.get(field).and_then(|value| value.as_str())
}

#[test]
fn the_ui_look_draws_cards_that_follow_the_theme_and_never_overlap() -> Result<(), DocError> {
    let scene = scene_of(OWNER, Look::Ui)?;
    let nodes = by_key(&scene);
    assert_eq!(nodes.len(), 13);
    for (key, card) in &nodes {
        assert_eq!(card.kind, "rectangle", "{key}: every kind is a card");
        assert_eq!(word(card, "strokeColor"), Some("#1e1e1e"), "{key}");
        assert_eq!(word(card, "backgroundColor"), Some("#fcfffe"), "{key}");
        assert_eq!(
            card.extra.get("roughness").and_then(|v| v.as_f64()),
            Some(0.0)
        );
    }
    let cards: Vec<_> = nodes.values().collect();
    for (at, a) in cards.iter().enumerate() {
        for b in cards.iter().skip(at + 1) {
            let apart = a.x + a.width <= b.x
                || b.x + b.width <= a.x
                || a.y + a.height <= b.y
                || b.y + b.height <= a.y;
            assert!(apart, "{} overlaps {}", a.id, b.id);
        }
    }
    Ok(())
}

#[test]
fn the_ui_look_routes_every_edge_orthogonally_and_binds_both_ends() -> Result<(), DocError> {
    let scene = scene_of(OWNER, Look::Ui)?;
    let arrows: Vec<_> = scene
        .elements()
        .iter()
        .filter(|element| element.kind == "arrow")
        .collect();
    assert_eq!(arrows.len(), 14);
    for arrow in arrows {
        assert_eq!(
            arrow.extra.get("elbowed").and_then(|v| v.as_bool()),
            Some(true)
        );
        for end in ["startBinding", "endBinding"] {
            assert!(
                arrow
                    .extra
                    .get(end)
                    .is_some_and(|binding| !binding.is_null()),
                "{} {end}",
                arrow.id
            );
        }
        let points = arrow.points.clone().unwrap_or_default();
        assert!(points.len() >= 2, "{}", arrow.id);
        for pair in points.windows(2) {
            let straight =
                (pair[0][0] - pair[1][0]).abs() < 1e-6 || (pair[0][1] - pair[1][1]).abs() < 1e-6;
            assert!(straight, "{} bends diagonally: {pair:?}", arrow.id);
        }
    }
    Ok(())
}

#[test]
fn a_group_box_is_dashed_and_holds_exactly_its_members() -> Result<(), DocError> {
    let scene = scene_of(OWNER, Look::Ui)?;
    let nodes = by_key(&scene);
    let groups: Vec<_> = scene
        .elements()
        .iter()
        .filter(|element| {
            word(element, "strokeStyle") == Some("dashed") && element.kind == "rectangle"
        })
        .collect();
    assert_eq!(groups.len(), 1);
    let group = groups[0];
    let inside = |card: &Element| {
        card.x >= group.x
            && card.y >= group.y
            && card.x + card.width <= group.x + group.width
            && card.y + card.height <= group.y + group.height
    };
    for (key, card) in &nodes {
        let member = matches!(key.as_str(), "research" | "review");
        assert_eq!(inside(card), member, "{key}");
    }
    let title = scene
        .elements()
        .iter()
        .any(|element| element.kind == "text" && word(element, "text") == Some("Try/Catch"));
    assert!(title, "the group title is drawn");
    Ok(())
}

const DEMO: &str = "flowchart LR
  s([Start]) --> g[Get license]
  g -->|Try| r[Research license]
  g -->|Catch| m[Review manually]
  r --> e{Eligible?}
  m --> e
  e -->|Yes| a[Send approval email]
  e -->|No| d[Send denial email]
  a --> f[Send follow-up email]
";

fn arrows(scene: &Scene) -> Vec<&Element> {
    scene
        .elements()
        .iter()
        .filter(|element| element.kind == "arrow")
        .collect()
}

fn absolute(arrow: &Element) -> Vec<(f64, f64)> {
    arrow
        .points
        .iter()
        .flatten()
        .map(|[x, y]| (arrow.x + x, arrow.y + y))
        .collect()
}

fn port(arrow: &Element, end: &str) -> Option<(String, [f64; 2])> {
    let binding = arrow.extra.get(end)?;
    let id = binding.get("elementId")?.as_str()?.to_owned();
    let point = binding.get("fixedPoint")?.as_array()?;
    Some((id, [point.first()?.as_f64()?, point.get(1)?.as_f64()?]))
}

fn overlap(a: ((f64, f64), (f64, f64)), b: ((f64, f64), (f64, f64))) -> bool {
    let flat = |s: ((f64, f64), (f64, f64)), axis: fn((f64, f64)) -> f64| {
        (axis(s.0) - axis(s.1)).abs() < 1e-6
    };
    let shared = |along: fn((f64, f64)) -> f64, across: fn((f64, f64)) -> f64| {
        flat(a, across) && flat(b, across) && (across(a.0) - across(b.0)).abs() < 1e-6 && {
            let (a0, a1) = (along(a.0).min(along(a.1)), along(a.0).max(along(a.1)));
            let (b0, b1) = (along(b.0).min(along(b.1)), along(b.0).max(along(b.1)));
            a1.min(b1) - a0.max(b0) > 1e-6
        }
    };
    shared(|p| p.0, |p| p.1) || shared(|p| p.1, |p| p.0)
}

#[test]
fn no_two_arrows_share_a_run() -> Result<(), DocError> {
    for (name, text) in [("owner TD", OWNER), ("demo LR", DEMO)] {
        let scene = scene_of(text, Look::Ui)?;
        let runs: Vec<(String, Vec<_>)> = arrows(&scene)
            .into_iter()
            .map(|arrow| {
                let points = absolute(arrow);
                let runs = points.windows(2).map(|pair| (pair[0], pair[1])).collect();
                (arrow.id.clone(), runs)
            })
            .collect();
        for (at, (a, a_runs)) in runs.iter().enumerate() {
            for (b, b_runs) in runs.iter().skip(at + 1) {
                for &one in a_runs {
                    for &two in b_runs {
                        assert!(
                            !overlap(one, two),
                            "{name}: {a} and {b} share {one:?} {two:?}"
                        );
                    }
                }
            }
        }
    }
    Ok(())
}

#[test]
fn left_to_right_edges_leave_right_and_enter_left_inside_their_gap() -> Result<(), DocError> {
    let scene = scene_of(DEMO, Look::Ui)?;
    let found = arrows(&scene);
    assert_eq!(found.len(), 8);
    for arrow in found {
        let (Some((_, leave)), Some((_, enter))) =
            (port(arrow, "startBinding"), port(arrow, "endBinding"))
        else {
            return Err(DocError::UnknownKey(arrow.id.clone()));
        };
        assert_eq!((leave[0], enter[0]), (1.0, 0.0), "{}", arrow.id);
        let points = absolute(arrow);
        let (tail, head) = (points[0].0, points[points.len() - 1].0);
        for point in &points {
            assert!(
                point.0 >= tail - 1e-6 && point.0 <= head + 1e-6,
                "{} leaves its gap at {point:?}",
                arrow.id
            );
        }
    }
    Ok(())
}

#[test]
fn every_arrow_ends_on_its_bound_port() -> Result<(), DocError> {
    for text in [OWNER, DEMO] {
        let scene = scene_of(text, Look::Ui)?;
        for arrow in arrows(&scene) {
            let points = absolute(arrow);
            for (end, point) in [
                ("startBinding", points[0]),
                ("endBinding", points[points.len() - 1]),
            ] {
                let (id, fixed) = port(arrow, end).ok_or(DocError::UnknownKey(arrow.id.clone()))?;
                let card = scene.get(&id).ok_or(DocError::UnknownKey(id.clone()))?;
                let wanted = (
                    card.x + card.width * fixed[0],
                    card.y + card.height * fixed[1],
                );
                assert!(
                    (point.0 - wanted.0).abs() < 1e-6 && (point.1 - wanted.1).abs() < 1e-6,
                    "{} {end}: {point:?} is not {wanted:?}",
                    arrow.id
                );
            }
        }
    }
    Ok(())
}

#[test]
fn edges_meeting_a_card_use_distinct_ports_and_no_join_dot_remains() -> Result<(), DocError> {
    for text in [OWNER, DEMO] {
        let scene = scene_of(text, Look::Ui)?;
        let mut used: HashMap<String, Vec<[f64; 2]>> = HashMap::new();
        for arrow in arrows(&scene) {
            for end in ["startBinding", "endBinding"] {
                if let Some((id, point)) = port(arrow, end) {
                    let seen = used.entry(id.clone()).or_default();
                    assert!(!seen.contains(&point), "{id}: two edges meet at {point:?}");
                    seen.push(point);
                }
            }
        }
        let dots = scene
            .elements()
            .iter()
            .filter(|element| element.kind == "ellipse")
            .count();
        assert_eq!(dots, 0, "a join dot marks a merge that no longer happens");
    }
    Ok(())
}

#[test]
fn the_left_to_right_rank_gap_fits_a_label() -> Result<(), DocError> {
    let nodes = by_key(&scene_of(DEMO, Look::Ui)?);
    let (Some(start), Some(license)) = (nodes.get("s"), nodes.get("g")) else {
        return Err(DocError::UnknownKey("s or g".into()));
    };
    let gap = license.x - (start.x + start.width);
    assert!(gap >= 120.0, "Start to Get license is {gap} px");
    Ok(())
}

#[test]
fn the_sketch_look_draws_kinds_as_mermaid_does() -> Result<(), DocError> {
    let nodes = by_key(&scene_of(OWNER, Look::Sketch)?);
    let kind = |key: &str| nodes.get(key).map(|element| element.kind.as_str());
    assert_eq!(kind("start"), Some("ellipse"));
    assert_eq!(kind("eligible"), Some("diamond"));
    assert_eq!(kind("license"), Some("rectangle"));
    Ok(())
}

#[test]
fn left_to_right_lays_a_chain_along_x() -> Result<(), DocError> {
    let nodes = by_key(&scene_of("flowchart LR\na --> b --> c", Look::Ui)?);
    let at = |key: &str| nodes.get(key).map(|element| (element.x, element.y));
    let (Some(a), Some(b), Some(c)) = (at("a"), at("b"), at("c")) else {
        return Err(DocError::UnknownKey("a, b or c".into()));
    };
    assert!(a.0 < b.0 && b.0 < c.0, "{a:?} {b:?} {c:?}");
    assert!(a.1 == b.1 && b.1 == c.1, "{a:?} {b:?} {c:?}");
    Ok(())
}

#[test]
fn a_flow_scene_reads_back_as_the_same_graph() -> Result<(), DocError> {
    let parsed = flow(OWNER)?;
    let read = draw_doc::graph(&flow_scene(&parsed, Look::Ui)?)?;
    let pairs = |graph: &draw_doc::Graph| {
        let mut pairs: Vec<_> = graph
            .edges
            .iter()
            .map(|edge| (edge.from.clone(), edge.to.clone(), edge.label.clone()))
            .collect();
        pairs.sort();
        pairs
    };
    assert_eq!(pairs(&read), pairs(&parsed.graph));
    let mut labels: Vec<_> = read.nodes.iter().map(|node| node.label.clone()).collect();
    labels.sort();
    let mut wanted: Vec<_> = parsed
        .graph
        .nodes
        .iter()
        .map(|node| node.label.clone())
        .collect();
    wanted.sort();
    assert_eq!(labels, wanted);
    Ok(())
}
