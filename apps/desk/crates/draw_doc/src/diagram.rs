use std::collections::HashMap;
use std::sync::LazyLock;

use serde::{Deserialize, Serialize};
use serde_json::{Map, Value, json};

use crate::element::{ArrowEnd, BoundKind, Element};
use crate::flow::{Direction, Look};
use crate::layout::{BOX, Layout, Placed, place};
use crate::{DocError, Scene};

pub(crate) const PAPER: &str = "#fcfffe";
const UI_FONT: u32 = 2;
const FONT: f64 = 16.0;
const LINE: f64 = 1.25;
const CHAR: f64 = 0.55;

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum NodeKind {
    #[default]
    Step,
    Decision,
    Terminal,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Node {
    pub key: String,
    pub label: String,
    #[serde(default, skip_serializing_if = "is_default")]
    pub kind: NodeKind,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub icon: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub group: Option<String>,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Edge {
    pub from: String,
    pub to: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub label: Option<String>,
    #[serde(default, skip_serializing_if = "is_default")]
    pub line: Line,
}

impl Node {
    pub fn new(key: impl Into<String>, label: impl Into<String>) -> Node {
        Node {
            key: key.into(),
            label: label.into(),
            kind: NodeKind::Step,
            icon: None,
            group: None,
        }
    }
}

impl Edge {
    pub fn new(from: impl Into<String>, to: impl Into<String>, label: Option<String>) -> Edge {
        Edge {
            from: from.into(),
            to: to.into(),
            label,
            line: Line::Solid,
        }
    }
}

fn is_default<T: Default + PartialEq>(value: &T) -> bool {
    *value == T::default()
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Line {
    #[default]
    Solid,
    Dashed,
    Dotted,
}

pub(crate) fn dress(arrow: &mut Element, line: Line, headed: bool) {
    let style = match line {
        Line::Solid => "solid",
        Line::Dashed => "dashed",
        Line::Dotted => "dotted",
    };
    let head = match headed {
        true => Value::from("arrow"),
        false => Value::Null,
    };
    for (field, value) in [
        ("strokeStyle", Value::from(style)),
        ("startArrowhead", Value::Null),
        ("endArrowhead", head),
        ("elbowed", Value::from(false)),
    ] {
        arrow.extra.insert(field.to_owned(), value);
    }
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct Graph {
    pub nodes: Vec<Node>,
    pub edges: Vec<Edge>,
}

pub fn diagram(graph: &Graph, layout: Layout) -> Result<Scene, DocError> {
    let pairs = pairs(graph)?;
    let placed = place(layout, graph.nodes.len(), &pairs);
    Scene::of(elements(
        graph,
        &pairs,
        &placed,
        Look::Sketch,
        Direction::Down,
    )?)
}

pub(crate) fn pairs(graph: &Graph) -> Result<Vec<(usize, usize)>, DocError> {
    let mut index = HashMap::with_capacity(graph.nodes.len());
    for (at, node) in graph.nodes.iter().enumerate() {
        if index.insert(node.key.as_str(), at).is_some() {
            return Err(DocError::DuplicateId(node.key.clone()));
        }
    }
    let find = |key: &str| {
        index
            .get(key)
            .copied()
            .ok_or_else(|| DocError::UnknownId(key.to_owned()))
    };
    graph
        .edges
        .iter()
        .map(|edge| Ok((find(&edge.from)?, find(&edge.to)?)))
        .collect()
}

pub(crate) fn elements(
    graph: &Graph,
    pairs: &[(usize, usize)],
    placed: &Placed,
    look: Look,
    direction: Direction,
) -> Result<Vec<Element>, DocError> {
    let routes = routes(pairs, placed, look, direction)?;
    let mut touching = vec![Vec::new(); graph.nodes.len()];
    for (at, &(from, to)) in pairs.iter().enumerate() {
        for node in [from, to] {
            if let Some(list) = touching.get_mut(node) {
                list.push(at);
            }
        }
    }
    let (mut elements, arrows) = std::thread::scope(|scope| {
        let arrows = scope.spawn(|| {
            let mut arrows = Vec::with_capacity(graph.edges.len() * 2);
            for (at, ((edge, &ends), route)) in
                graph.edges.iter().zip(pairs).zip(&routes).enumerate()
            {
                let (arrow, label) = arrow(at, edge, ends, route, look);
                arrows.push(arrow);
                arrows.extend(label);
            }
            arrows
        });
        let mut elements = Vec::with_capacity(graph.nodes.len() * 2 + graph.edges.len() * 2);
        for (at, ((node, &corner), edges)) in graph
            .nodes
            .iter()
            .zip(&placed.boxes)
            .zip(&touching)
            .enumerate()
        {
            elements.extend(boxed(at, node, corner, edges, look)?);
        }
        let arrows = arrows
            .join()
            .unwrap_or_else(|panic| std::panic::resume_unwind(panic));
        Ok::<_, DocError>((elements, arrows))
    })?;
    elements.extend(arrows);
    Ok(elements)
}

pub(crate) fn ui(element: &mut Element) {
    for (field, value) in [
        ("strokeWidth", Value::from(1)),
        ("fontFamily", Value::from(UI_FONT)),
    ] {
        if let Some(slot) = element.extra.get_mut(field) {
            *slot = value;
        }
    }
}

fn boxed(
    at: usize,
    node: &Node,
    corner: (f64, f64),
    edges: &[usize],
    look: Look,
) -> Result<[Element; 2], DocError> {
    let id = format!("n{at}");
    let label = format!("{id}-label");
    let bound = std::iter::once(bound_entry(label.clone(), "text"))
        .chain(
            edges
                .iter()
                .map(|edge| bound_entry(format!("e{edge}"), "arrow")),
        )
        .collect();
    let kind = match (look, node.kind) {
        (Look::Ui, _) | (Look::Sketch, NodeKind::Step) => "rectangle",
        (Look::Sketch, NodeKind::Decision) => "diamond",
        (Look::Sketch, NodeKind::Terminal) => "ellipse",
    };
    let mut rect = shape(&id, kind, corner, BOX, at);
    if kind == "rectangle" {
        rect.extra
            .insert("roundness".to_owned(), object([("type", Value::from(3))]));
    }
    rect.extra
        .insert("boundElements".to_owned(), Value::Array(bound));
    let mut data = Map::new();
    data.insert("key".to_owned(), Value::from(node.key.as_str()));
    if node.kind != NodeKind::Step {
        data.insert("kind".to_owned(), serde_json::to_value(node.kind)?);
    }
    for (field, value) in [("icon", &node.icon), ("group", &node.group)] {
        if let Some(value) = value {
            data.insert(field.to_owned(), Value::from(value.as_str()));
        }
    }
    rect.extra
        .insert("customData".to_owned(), Value::Object(data));
    let middle = (corner.0 + BOX.0 / 2.0, corner.1 + BOX.1 / 2.0);
    let mut text = text_in(&label, &node.label, &id, middle, at);
    if look == Look::Ui {
        ui(&mut rect);
        ui(&mut text);
        rect.extra
            .insert("backgroundColor".to_owned(), Value::from(PAPER));
    }
    Ok([rect, text])
}

struct Route {
    leave: [f64; 2],
    enter: [f64; 2],
    vertical: bool,
    points: Vec<(f64, f64)>,
}

fn sides(start: (f64, f64), end: (f64, f64), direction: Direction) -> ([f64; 2], [f64; 2], bool) {
    let stacked = end.1 >= start.1 + BOX.1 || end.1 + BOX.1 <= start.1;
    let beside = end.0 >= start.0 + BOX.0 || end.0 + BOX.0 <= start.0;
    let vertical = match direction {
        Direction::Down => stacked,
        Direction::Right => !beside,
    };
    match (vertical, end.1 > start.1, end.0 > start.0) {
        (true, true, _) => ([0.5, 1.0], [0.5, 0.0], true),
        (true, false, _) => ([0.5, 0.0], [0.5, 1.0], true),
        (false, _, true) => ([1.0, 0.5], [0.0, 0.5], false),
        (false, _, false) => ([0.0, 0.5], [1.0, 0.5], false),
    }
}

fn routes(
    pairs: &[(usize, usize)],
    placed: &Placed,
    look: Look,
    direction: Direction,
) -> Result<Vec<Route>, DocError> {
    let corner = |node: usize| {
        placed.boxes.get(node).copied().ok_or(DocError::OutOfRange {
            at: node,
            len: placed.boxes.len(),
        })
    };
    let mut routes = Vec::with_capacity(pairs.len());
    for &(from, to) in pairs {
        let (leave, enter, vertical) = sides(corner(from)?, corner(to)?, direction);
        routes.push(Route {
            leave,
            enter,
            vertical,
            points: Vec::new(),
        });
    }
    if look == Look::Ui {
        spread(&mut routes, pairs, &placed.boxes);
    }
    for (at, (route, &(from, to))) in routes.iter_mut().zip(pairs).enumerate() {
        let on = |corner: (f64, f64), point: [f64; 2]| {
            (corner.0 + BOX.0 * point[0], corner.1 + BOX.1 * point[1])
        };
        route.points = std::iter::once(on(corner(from)?, route.leave))
            .chain(placed.bends.get(at).into_iter().flatten().copied())
            .chain(std::iter::once(on(corner(to)?, route.enter)))
            .collect();
    }
    if look == Look::Ui {
        lanes(&mut routes);
    }
    Ok(routes)
}

fn spread(routes: &mut [Route], pairs: &[(usize, usize)], boxes: &[(f64, f64)]) {
    let middle = |node: usize| {
        boxes
            .get(node)
            .map_or((0.0, 0.0), |c| (c.0 + BOX.0 / 2.0, c.1 + BOX.1 / 2.0))
    };
    type Meeting = (f64, usize, bool);
    let mut sides: HashMap<(usize, usize), Vec<Meeting>> = HashMap::new();
    for (at, (route, &(from, to))) in routes.iter().zip(pairs).enumerate() {
        for (node, point, other, leaving) in [
            (from, route.leave, to, true),
            (to, route.enter, from, false),
        ] {
            let across = middle(other);
            let cross = match point[1] == 0.5 {
                true => across.1,
                false => across.0,
            };
            let side = (point[0] * 2.0) as usize * 3 + (point[1] * 2.0) as usize;
            sides
                .entry((node, side))
                .or_default()
                .push((cross, at, leaving));
        }
    }
    for mut met in sides.into_values().filter(|met| met.len() > 1) {
        met.sort_by(|a, b| a.0.total_cmp(&b.0).then(a.1.cmp(&b.1)));
        let count = met.len() as f64 + 1.0;
        for (slot, (_, at, leaving)) in met.into_iter().enumerate() {
            let Some(route) = routes.get_mut(at) else {
                continue;
            };
            let point = match leaving {
                true => &mut route.leave,
                false => &mut route.enter,
            };
            let varied = usize::from(point[1] == 0.5);
            point[varied] = (slot as f64 + 1.0) / count;
        }
    }
}

fn lanes(routes: &mut [Route]) {
    type Hop = (f64, f64, usize, usize);
    let mut gaps: HashMap<(bool, i64, i64), Vec<Hop>> = HashMap::new();
    for (at, route) in routes.iter().enumerate() {
        for (step, pair) in route.points.windows(2).enumerate() {
            let &[a, b] = pair else {
                continue;
            };
            if (a.0 - b.0).abs() <= f64::EPSILON || (a.1 - b.1).abs() <= f64::EPSILON {
                continue;
            }
            let (along, cross) = match route.vertical {
                true => ((a.1, b.1), (a.0, b.0)),
                false => ((a.0, b.0), (a.1, b.1)),
            };
            let (low, high) = (along.0.min(along.1), along.0.max(along.1));
            gaps.entry((route.vertical, low.round() as i64, high.round() as i64))
                .or_default()
                .push((cross.0, cross.1, at, step));
        }
    }
    let mut elbows: HashMap<(usize, usize), f64> = HashMap::new();
    for ((_, low, high), mut hops) in gaps {
        hops.sort_by(|a, b| a.0.total_cmp(&b.0).then(a.1.total_cmp(&b.1)));
        let count = hops.len() as f64 + 1.0;
        let (low, high) = (low as f64, high as f64);
        for (lane, (_, _, at, step)) in hops.into_iter().enumerate() {
            elbows.insert((at, step), low + (high - low) * (lane as f64 + 1.0) / count);
        }
    }
    for (at, route) in routes.iter_mut().enumerate() {
        let mut out = Vec::with_capacity(route.points.len() * 3);
        for (step, pair) in route.points.windows(2).enumerate() {
            let &[a, b] = pair else {
                continue;
            };
            out.push(a);
            if let Some(&lane) = elbows.get(&(at, step)) {
                match route.vertical {
                    true => out.extend([(a.0, lane), (b.0, lane)]),
                    false => out.extend([(lane, a.1), (lane, b.1)]),
                }
            }
        }
        out.extend(route.points.last());
        route.points = out;
    }
}

fn arrow(
    at: usize,
    edge: &Edge,
    (from, to): (usize, usize),
    route: &Route,
    look: Look,
) -> (Element, Option<Element>) {
    let id = format!("e{at}");
    let (leave, enter, points) = (route.leave, route.enter, &route.points);
    let tail = points.first().copied().unwrap_or_default();
    let route: Vec<[f64; 2]> = points
        .iter()
        .map(|(x, y)| [x - tail.0, y - tail.1])
        .collect();
    let span = |axis: usize| {
        let (low, high) = route.iter().fold((0.0_f64, 0.0_f64), |(low, high), point| {
            (low.min(point[axis]), high.max(point[axis]))
        });
        high - low
    };
    let mut arrow = shape(&id, "arrow", tail, (span(0), span(1)), at);
    let middle = match (
        points.get(points.len().saturating_sub(1) / 2),
        points.get(points.len() / 2),
    ) {
        (Some(a), Some(b)) => ((a.0 + b.0) / 2.0, (a.1 + b.1) / 2.0),
        _ => tail,
    };
    arrow.points = Some(route);
    for (end, node, point) in [(ArrowEnd::Start, from, leave), (ArrowEnd::End, to, enter)] {
        arrow.extra.insert(
            end.field().to_owned(),
            object([
                ("elementId", Value::from(format!("n{node}"))),
                ("fixedPoint", Value::from(point.to_vec())),
                ("mode", Value::from("orbit")),
            ]),
        );
    }
    dress(&mut arrow, edge.line, true);
    let mut label = edge.label.as_deref().map(|text| {
        let label = format!("{id}-label");
        arrow.extra.insert(
            "boundElements".to_owned(),
            Value::Array(vec![bound_entry(label.clone(), "text")]),
        );
        text_in(&label, text, &id, middle, at)
    });
    if look == Look::Ui {
        ui(&mut arrow);
        arrow.extra.insert("elbowed".to_owned(), Value::from(true));
        label.iter_mut().for_each(ui);
    }
    (arrow, label)
}

pub fn graph(scene: &Scene) -> Result<Graph, DocError> {
    let text_of = |id: &str| -> Option<String> {
        let text = scene.get(id)?;
        Some(text.extra.get("text")?.as_str()?.to_owned())
    };
    let label_of = |element: &Element| -> Result<Option<String>, DocError> {
        Ok(element
            .bound_elements()?
            .iter()
            .filter(|bound| bound.kind == BoundKind::Text)
            .find_map(|bound| text_of(&bound.id)))
    };
    let live = scene
        .elements()
        .iter()
        .filter(|element| element.is_deleted != Some(true));
    let mut keys = HashMap::new();
    let mut found = Graph::default();
    for element in live.clone() {
        let data = element.extra.get("customData");
        let field = |name: &str| data.and_then(|data| data.get(name));
        let Some(key) = field("key").and_then(Value::as_str) else {
            continue;
        };
        keys.insert(element.id.as_str(), key.to_owned());
        let text = |name: &str| field(name).and_then(Value::as_str).map(str::to_owned);
        found.nodes.push(Node {
            kind: field("kind")
                .map(|kind| NodeKind::deserialize(kind).map_err(DocError::Json))
                .transpose()?
                .unwrap_or_default(),
            icon: text("icon"),
            group: text("group"),
            ..Node::new(key, label_of(element)?.unwrap_or_default())
        });
    }
    for arrow in live.filter(|element| element.kind == "arrow") {
        let key = |end| -> Result<Option<String>, DocError> {
            Ok(arrow
                .binding(end)?
                .and_then(|binding| keys.get(binding.element_id()).cloned()))
        };
        if let (Some(from), Some(to)) = (key(ArrowEnd::Start)?, key(ArrowEnd::End)?) {
            let line = match arrow.extra.get("strokeStyle").and_then(Value::as_str) {
                Some("dashed") => Line::Dashed,
                Some("dotted") => Line::Dotted,
                _ => Line::Solid,
            };
            found.edges.push(Edge {
                line,
                ..Edge::new(from, to, label_of(arrow)?)
            });
        }
    }
    Ok(found)
}

pub(crate) fn shape(
    id: &str,
    kind: &str,
    at: (f64, f64),
    size: (f64, f64),
    seed: usize,
) -> Element {
    stamped(&SHAPE_FIELDS, id, kind, at, size, seed)
}

static SHAPE_FIELDS: LazyLock<Map<String, Value>> = LazyLock::new(|| {
    members(json!({
        "strokeColor": "#1e1e1e",
        "backgroundColor": "transparent",
        "fillStyle": "solid",
        "strokeWidth": 2,
        "strokeStyle": "solid",
        "roughness": 0,
        "opacity": 100,
        "groupIds": [],
        "frameId": null,
        "roundness": null,
        "seed": 1,
        "version": 1,
        "versionNonce": 1,
        "boundElements": null,
        "updated": 1,
        "link": null,
        "locked": false,
    }))
});

static TEXT_FIELDS: LazyLock<Map<String, Value>> = LazyLock::new(|| {
    let mut fields = SHAPE_FIELDS.clone();
    fields.extend(members(json!({
        "text": "",
        "originalText": "",
        "fontSize": FONT,
        "fontFamily": 5,
        "textAlign": "center",
        "verticalAlign": "middle",
        "containerId": null,
        "autoResize": true,
        "lineHeight": LINE,
        "strokeWidth": 1,
    })));
    fields
});

fn members(value: Value) -> Map<String, Value> {
    match value {
        Value::Object(map) => map,
        _ => Map::new(),
    }
}

fn object<const N: usize>(fields: [(&str, Value); N]) -> Value {
    Value::Object(
        fields
            .into_iter()
            .map(|(key, value)| (key.to_owned(), value))
            .collect(),
    )
}

fn bound_entry(id: String, kind: &str) -> Value {
    object([("id", Value::from(id)), ("type", Value::from(kind))])
}

fn stamped(
    fields: &Map<String, Value>,
    id: &str,
    kind: &str,
    at: (f64, f64),
    size: (f64, f64),
    seed: usize,
) -> Element {
    let mut extra = fields.clone();
    for field in ["seed", "versionNonce"] {
        if let Some(slot) = extra.get_mut(field) {
            *slot = Value::from(seed + 1);
        }
    }
    Element {
        id: id.to_owned(),
        kind: kind.to_owned(),
        x: at.0,
        y: at.1,
        width: size.0,
        height: size.1,
        angle: Some(0.0),
        is_deleted: Some(false),
        points: None,
        extra,
    }
}

pub(crate) fn text_in(
    id: &str,
    text: &str,
    container: &str,
    middle: (f64, f64),
    seed: usize,
) -> Element {
    let width = (text.chars().count() as f64 * FONT * CHAR).min(BOX.0 - 10.0);
    let height = FONT * LINE;
    let corner = (middle.0 - width / 2.0, middle.1 - height / 2.0);
    let mut element = stamped(&TEXT_FIELDS, id, "text", corner, (width, height), seed);
    for (field, value) in [
        ("text", Value::from(text)),
        ("originalText", Value::from(text)),
        ("containerId", Value::from(container)),
    ] {
        if let Some(slot) = element.extra.get_mut(field) {
            *slot = value;
        }
    }
    element
}
