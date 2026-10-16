use std::collections::HashMap;
use std::sync::LazyLock;

use serde::{Deserialize, Serialize};
use serde_json::{Map, Value, json};

use crate::element::{ArrowEnd, BoundKind, Element};
use crate::layout::{BOX, Layout, Placed, place};
use crate::{DocError, Scene};

const FONT: f64 = 16.0;
const LINE: f64 = 1.25;
const CHAR: f64 = 0.55;

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Node {
    pub key: String,
    pub label: String,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Edge {
    pub from: String,
    pub to: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub label: Option<String>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Line {
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
    let pairs = graph
        .edges
        .iter()
        .map(|edge| Ok((find(&edge.from)?, find(&edge.to)?)))
        .collect::<Result<Vec<_>, DocError>>()?;
    let placed = place(layout, graph.nodes.len(), &pairs);
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
            for (at, (edge, &ends)) in graph.edges.iter().zip(&pairs).enumerate() {
                let (arrow, label) = arrow(at, edge, ends, &placed)?;
                arrows.push(arrow);
                arrows.extend(label);
            }
            Ok::<_, DocError>(arrows)
        });
        let mut elements = Vec::with_capacity(graph.nodes.len() * 2 + graph.edges.len() * 2);
        for (at, ((node, &corner), edges)) in graph
            .nodes
            .iter()
            .zip(&placed.boxes)
            .zip(&touching)
            .enumerate()
        {
            elements.extend(boxed(at, node, corner, edges));
        }
        let arrows = arrows
            .join()
            .unwrap_or_else(|panic| std::panic::resume_unwind(panic));
        (elements, arrows)
    });
    elements.extend(arrows?);
    Scene::of(elements)
}

fn boxed(at: usize, node: &Node, corner: (f64, f64), edges: &[usize]) -> [Element; 2] {
    let id = format!("n{at}");
    let label = format!("{id}-label");
    let bound = std::iter::once(bound_entry(label.clone(), "text"))
        .chain(
            edges
                .iter()
                .map(|edge| bound_entry(format!("e{edge}"), "arrow")),
        )
        .collect();
    let mut rect = shape(&id, "rectangle", corner, BOX, at);
    rect.extra
        .insert("roundness".to_owned(), object([("type", Value::from(3))]));
    rect.extra
        .insert("boundElements".to_owned(), Value::Array(bound));
    rect.extra.insert(
        "customData".to_owned(),
        object([("key", Value::from(node.key.as_str()))]),
    );
    let middle = (corner.0 + BOX.0 / 2.0, corner.1 + BOX.1 / 2.0);
    let text = text_in(&label, &node.label, &id, middle, at);
    [rect, text]
}

fn arrow(
    at: usize,
    edge: &Edge,
    (from, to): (usize, usize),
    placed: &Placed,
) -> Result<(Element, Option<Element>), DocError> {
    let id = format!("e{at}");
    let (Some(&start), Some(&end)) = (placed.boxes.get(from), placed.boxes.get(to)) else {
        return Err(DocError::OutOfRange {
            at: from.max(to),
            len: placed.boxes.len(),
        });
    };
    let (leave, enter) = if end.1 >= start.1 + BOX.1 {
        ([0.5, 1.0], [0.5, 0.0])
    } else if end.1 + BOX.1 <= start.1 {
        ([0.5, 0.0], [0.5, 1.0])
    } else if end.0 > start.0 {
        ([1.0, 0.5], [0.0, 0.5])
    } else {
        ([0.0, 0.5], [1.0, 0.5])
    };
    let tail = (start.0 + BOX.0 * leave[0], start.1 + BOX.1 * leave[1]);
    let head = (end.0 + BOX.0 * enter[0], end.1 + BOX.1 * enter[1]);
    let route: Vec<[f64; 2]> = std::iter::once(tail)
        .chain(placed.bends.get(at).into_iter().flatten().copied())
        .chain(std::iter::once(head))
        .map(|(x, y)| [x - tail.0, y - tail.1])
        .collect();
    let span = |axis: usize| {
        let (low, high) = route.iter().fold((0.0_f64, 0.0_f64), |(low, high), point| {
            (low.min(point[axis]), high.max(point[axis]))
        });
        high - low
    };
    let mut arrow = shape(&id, "arrow", tail, (span(0), span(1)), at);
    let middle = route
        .get(route.len() / 2)
        .map_or(tail, |point| (tail.0 + point[0], tail.1 + point[1]));
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
    dress(&mut arrow, Line::Solid, true);
    let label = edge.label.as_deref().map(|text| {
        let label = format!("{id}-label");
        arrow.extra.insert(
            "boundElements".to_owned(),
            Value::Array(vec![bound_entry(label.clone(), "text")]),
        );
        text_in(&label, text, &id, middle, at)
    });
    Ok((arrow, label))
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
        let key = element
            .extra
            .get("customData")
            .and_then(|data| data.get("key"))
            .and_then(Value::as_str);
        if let Some(key) = key {
            keys.insert(element.id.as_str(), key.to_owned());
            found.nodes.push(Node {
                key: key.to_owned(),
                label: label_of(element)?.unwrap_or_default(),
            });
        }
    }
    for arrow in live.filter(|element| element.kind == "arrow") {
        let key = |end| -> Result<Option<String>, DocError> {
            Ok(arrow
                .binding(end)?
                .and_then(|binding| keys.get(binding.element_id()).cloned()))
        };
        if let (Some(from), Some(to)) = (key(ArrowEnd::Start)?, key(ArrowEnd::End)?) {
            found.edges.push(Edge {
                from,
                to,
                label: label_of(arrow)?,
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
