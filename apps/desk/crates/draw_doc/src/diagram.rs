use std::collections::HashMap;

use serde::{Deserialize, Serialize};
use serde_json::{Map, Value, json};

use crate::element::{ArrowEnd, BoundKind, Element};
use crate::layout::{BOX, layered};
use crate::{DocError, Op, Scene};

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
        true => json!("arrow"),
        false => Value::Null,
    };
    for (field, value) in [
        ("strokeStyle", json!(style)),
        ("startArrowhead", Value::Null),
        ("endArrowhead", head),
        ("elbowed", json!(false)),
    ] {
        arrow.extra.insert(field.to_owned(), value);
    }
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct Graph {
    pub nodes: Vec<Node>,
    pub edges: Vec<Edge>,
}

pub fn diagram(graph: &Graph) -> Result<Scene, DocError> {
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
    let placed = layered(graph.nodes.len(), &pairs);
    let mut bound: Vec<Vec<Value>> = graph
        .nodes
        .iter()
        .enumerate()
        .map(|(at, _)| vec![json!({ "id": format!("n{at}-label"), "type": "text" })])
        .collect();
    let mut arrows = Vec::with_capacity(graph.edges.len() * 2);
    for (at, (edge, &(from, to))) in graph.edges.iter().zip(&pairs).enumerate() {
        let id = format!("e{at}");
        let (Some(&start), Some(&end)) = (placed.boxes.get(from), placed.boxes.get(to)) else {
            return Err(DocError::OutOfRange {
                at: from.max(to),
                len: placed.boxes.len(),
            });
        };
        let down = end.1 > start.1;
        let (leave, enter) = if down { (1.0, 0.0) } else { (0.0, 1.0) };
        let tail = (start.0 + BOX.0 / 2.0, start.1 + BOX.1 * leave);
        let head = (end.0 + BOX.0 / 2.0, end.1 + BOX.1 * enter);
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
        let ends = [
            (ArrowEnd::Start, from, [0.5, leave]),
            (ArrowEnd::End, to, [0.5, enter]),
        ];
        for (end, node, point) in ends {
            arrow.extra.insert(
                end.field().to_owned(),
                json!({ "elementId": format!("n{node}"), "fixedPoint": point, "mode": "orbit" }),
            );
            if let Some(list) = bound.get_mut(node) {
                list.push(json!({ "id": id, "type": "arrow" }));
            }
        }
        dress(&mut arrow, Line::Solid, true);
        let label = edge.label.as_deref().map(|text| {
            arrow.extra.insert(
                "boundElements".to_owned(),
                json!([{ "id": format!("{id}-label"), "type": "text" }]),
            );
            text_in(&format!("{id}-label"), text, &id, middle, at)
        });
        arrows.push(arrow);
        arrows.extend(label);
    }
    let mut elements = Vec::with_capacity(graph.nodes.len() * 2 + arrows.len());
    for (at, ((node, &corner), list)) in
        graph.nodes.iter().zip(&placed.boxes).zip(bound).enumerate()
    {
        let id = format!("n{at}");
        let mut rect = shape(&id, "rectangle", corner, BOX, at);
        rect.extra
            .insert("roundness".to_owned(), json!({ "type": 3 }));
        rect.extra
            .insert("boundElements".to_owned(), Value::Array(list));
        rect.extra
            .insert("customData".to_owned(), json!({ "key": node.key }));
        let middle = (corner.0 + BOX.0 / 2.0, corner.1 + BOX.1 / 2.0);
        elements.push(rect);
        elements.push(text_in(
            &format!("{id}-label"),
            &node.label,
            &id,
            middle,
            at,
        ));
    }
    elements.extend(arrows);
    let mut scene = Scene::new();
    scene.apply(
        elements
            .into_iter()
            .map(|element| Op::Add {
                at: None,
                element: Box::new(element),
            })
            .collect(),
    )?;
    Ok(scene)
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
    let extra = json!({
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
        "seed": seed + 1,
        "version": 1,
        "versionNonce": seed + 1,
        "boundElements": null,
        "updated": 1,
        "link": null,
        "locked": false,
    });
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
        extra: match extra {
            Value::Object(map) => map,
            _ => Map::new(),
        },
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
    let mut element = shape(id, "text", corner, (width, height), seed);
    for (field, value) in [
        ("text", json!(text)),
        ("originalText", json!(text)),
        ("fontSize", json!(FONT)),
        ("fontFamily", json!(5)),
        ("textAlign", json!("center")),
        ("verticalAlign", json!("middle")),
        ("containerId", json!(container)),
        ("autoResize", json!(true)),
        ("lineHeight", json!(LINE)),
        ("strokeWidth", json!(1)),
    ] {
        element.extra.insert(field.to_owned(), value);
    }
    element
}
