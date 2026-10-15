use std::collections::{HashMap, HashSet};

use serde::Deserialize;
use serde_json::{Map, Value, json};

use crate::diagram::{Line, dress, shape, text_in};
use crate::element::{ArrowEnd, BoundElement, BoundKind, Element, Rect};
use crate::layout::{BOX, GAP};
use crate::{DocError, Op, Scene};

const COLUMNS: usize = 10;
const ARROW_GAP: f64 = 6.0;

pub type KeyIds = Vec<(String, String)>;

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ShapeKind {
    #[default]
    Rectangle,
    Ellipse,
    Diamond,
    Text,
}

impl ShapeKind {
    fn name(self) -> &'static str {
        match self {
            ShapeKind::Rectangle => "rectangle",
            ShapeKind::Ellipse => "ellipse",
            ShapeKind::Diamond => "diamond",
            ShapeKind::Text => "text",
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq, Deserialize)]
#[serde(try_from = "String")]
pub struct Color {
    stroke: String,
    fill: String,
}

const HUES: [(&str, &str, &str); 10] = [
    ("red", "#e03131", "#ffc9c9"),
    ("orange", "#e8590c", "#ffd8a8"),
    ("yellow", "#f08c00", "#ffec99"),
    ("green", "#2f9e44", "#b2f2bb"),
    ("blue", "#1971c2", "#a5d8ff"),
    ("violet", "#6741d9", "#d0bfff"),
    ("gray", "#868e96", "#e9ecef"),
    ("black", "#1e1e1e", "#1e1e1e"),
    ("white", "#ffffff", "#ffffff"),
    ("transparent", "transparent", "transparent"),
];

impl TryFrom<String> for Color {
    type Error = String;

    fn try_from(text: String) -> Result<Self, String> {
        if let Some(&(_, stroke, fill)) = HUES.iter().find(|(name, _, _)| *name == text) {
            return Ok(Color {
                stroke: stroke.to_owned(),
                fill: fill.to_owned(),
            });
        }
        let hex = text.strip_prefix('#').unwrap_or_default();
        if matches!(hex.len(), 3 | 6) && hex.chars().all(|c| c.is_ascii_hexdigit()) {
            return Ok(Color {
                stroke: text.clone(),
                fill: text,
            });
        }
        let names: Vec<&str> = HUES.iter().map(|(name, _, _)| *name).collect();
        Err(format!(
            "color {text:?} is not #rgb, #rrggbb or one of {}",
            names.join(", ")
        ))
    }
}

#[derive(Clone, Debug, PartialEq, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Shape {
    pub key: String,
    #[serde(rename = "type", default)]
    pub kind: ShapeKind,
    pub x: Option<f64>,
    pub y: Option<f64>,
    pub w: Option<f64>,
    pub h: Option<f64>,
    pub label: Option<String>,
    pub fill: Option<Color>,
    pub stroke: Option<Color>,
    pub meta: Option<Map<String, Value>>,
}

#[derive(Clone, Debug, PartialEq, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Link {
    pub from: String,
    pub to: String,
    pub label: Option<String>,
    pub style: Option<Line>,
    pub arrow: Option<bool>,
}

#[derive(Clone, Debug, PartialEq, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Patch {
    pub key: String,
    pub label: Option<String>,
    pub fill: Option<Color>,
    pub stroke: Option<Color>,
    pub meta: Option<Map<String, Value>>,
}

fn key_of(element: &Element) -> Option<&str> {
    element.extra.get("customData")?.get("key")?.as_str()
}

fn keyed(scene: &Scene) -> HashMap<&str, &Element> {
    scene
        .elements()
        .iter()
        .filter(|element| element.is_deleted != Some(true))
        .filter_map(|element| Some((key_of(element)?, element)))
        .collect()
}

fn found<'a>(keys: &HashMap<&str, &'a Element>, key: &str) -> Result<&'a Element, DocError> {
    keys.get(key)
        .copied()
        .ok_or_else(|| DocError::UnknownKey(key.to_owned()))
}

fn fresh(taken: &mut HashSet<String>, base: &str) -> String {
    let id = (1..)
        .map(|at| match at {
            1 => base.to_owned(),
            at => format!("{base}-{at}"),
        })
        .find(|id| !taken.contains(id))
        .unwrap_or_else(|| base.to_owned());
    taken.insert(id.clone());
    id
}

fn ids(scene: &Scene) -> HashSet<String> {
    scene
        .elements()
        .iter()
        .map(|element| element.id.clone())
        .collect()
}

fn union(rects: impl Iterator<Item = Rect>) -> Option<Rect> {
    rects.reduce(|a, b| Rect {
        min_x: a.min_x.min(b.min_x),
        min_y: a.min_y.min(b.min_y),
        max_x: a.max_x.max(b.max_x),
        max_y: a.max_y.max(b.max_y),
    })
}

pub fn bounds(scene: &Scene, keys: Option<&[String]>) -> Result<Option<Rect>, DocError> {
    let Some(keys) = keys else {
        return Ok(union(
            scene
                .elements()
                .iter()
                .filter(|element| element.is_deleted != Some(true))
                .map(Element::bounds),
        ));
    };
    let index = keyed(scene);
    let picked = keys
        .iter()
        .map(|key| found(&index, key).map(|element| element.bounds()))
        .collect::<Result<Vec<_>, _>>()?;
    Ok(union(picked.into_iter()))
}

fn add(element: Element) -> Op {
    Op::Add {
        at: None,
        element: Box::new(element),
    }
}

fn colored(element: &mut Element, fill: Option<&Color>, stroke: Option<&Color>) {
    if let Some(fill) = fill {
        element
            .extra
            .insert("backgroundColor".to_owned(), json!(fill.fill));
        element.extra.insert("fillStyle".to_owned(), json!("solid"));
    }
    if let Some(stroke) = stroke {
        element
            .extra
            .insert("strokeColor".to_owned(), json!(stroke.stroke));
    }
}

fn custom(key: &str, meta: Option<&Map<String, Value>>) -> Value {
    match meta {
        Some(meta) => json!({ "key": key, "meta": meta }),
        None => json!({ "key": key }),
    }
}

pub fn add_shapes(scene: &Scene, shapes: &[Shape]) -> Result<(Vec<Op>, KeyIds), DocError> {
    let mut keys: HashSet<&str> = keyed(scene).into_keys().collect();
    let mut taken = ids(scene);
    let start = bounds(scene, None)?.map_or((0.0, 0.0), |rect| (rect.max_x + GAP.0, rect.min_y));
    let mut ops = Vec::with_capacity(shapes.len() * 2);
    let mut made = Vec::with_capacity(shapes.len());
    let mut unplaced = 0usize;
    for (seed, item) in shapes.iter().enumerate() {
        if !keys.insert(&item.key) {
            return Err(DocError::DuplicateKey(item.key.clone()));
        }
        let size = (item.w.unwrap_or(BOX.0), item.h.unwrap_or(BOX.1));
        let at = match (item.x, item.y) {
            (Some(x), Some(y)) => (x, y),
            _ => {
                let slot = unplaced;
                unplaced += 1;
                (
                    start.0 + (slot % COLUMNS) as f64 * (BOX.0 + GAP.0),
                    start.1 + (slot / COLUMNS) as f64 * (BOX.1 + GAP.1),
                )
            }
        };
        let id = fresh(&mut taken, &format!("k-{}", item.key));
        let mut element = shape(&id, item.kind.name(), at, size, seed);
        element.extra.insert(
            "customData".to_owned(),
            custom(&item.key, item.meta.as_ref()),
        );
        colored(&mut element, item.fill.as_ref(), item.stroke.as_ref());
        let label = item.label.as_deref();
        let middle = (at.0 + size.0 / 2.0, at.1 + size.1 / 2.0);
        if item.kind == ShapeKind::Rectangle {
            element
                .extra
                .insert("roundness".to_owned(), json!({ "type": 3 }));
        }
        let text = match item.kind {
            ShapeKind::Text => {
                let text = label.unwrap_or(&item.key);
                element.extra.insert("text".to_owned(), json!(text));
                element.extra.insert("originalText".to_owned(), json!(text));
                element.extra.insert("fontSize".to_owned(), json!(16));
                None
            }
            ShapeKind::Rectangle | ShapeKind::Ellipse | ShapeKind::Diamond => label.map(|text| {
                let label_id = fresh(&mut taken, &format!("{id}-label"));
                text_in(&label_id, text, &id, middle, seed)
            }),
        };
        if let Some(text) = &text {
            element.set_bound_elements(&[BoundElement {
                id: text.id.clone(),
                kind: BoundKind::Text,
            }]);
        }
        made.push((item.key.clone(), id));
        ops.push(add(element));
        ops.extend(text.map(add));
    }
    Ok((ops, made))
}

fn border(from: &Element, toward: (f64, f64)) -> (f64, f64) {
    let (a, b) = (from.width / 2.0, from.height / 2.0);
    let middle = (from.x + a, from.y + b);
    let (dx, dy) = (toward.0 - middle.0, toward.1 - middle.1);
    let length = dx.hypot(dy);
    if length <= f64::EPSILON || a <= 0.0 || b <= 0.0 {
        return middle;
    }
    let reach = 1.0 / (dx / a).abs().max((dy / b).abs());
    let out = (reach * length + ARROW_GAP) / length;
    (middle.0 + dx * out, middle.1 + dy * out)
}

fn share(point: (f64, f64), on: &Element) -> [f64; 2] {
    [
        (point.0 - on.x) / on.width.max(f64::EPSILON),
        (point.1 - on.y) / on.height.max(f64::EPSILON),
    ]
}

fn middle(element: &Element) -> (f64, f64) {
    (
        element.x + element.width / 2.0,
        element.y + element.height / 2.0,
    )
}

pub fn connect(scene: &Scene, edges: &[Link]) -> Result<(Vec<Op>, Vec<String>), DocError> {
    let keys = keyed(scene);
    let mut taken = ids(scene);
    let mut ops = Vec::with_capacity(edges.len() * 4);
    let mut made = Vec::with_capacity(edges.len());
    for (seed, edge) in edges.iter().enumerate() {
        let (from, to) = (found(&keys, &edge.from)?, found(&keys, &edge.to)?);
        let tail = border(from, middle(to));
        let head = border(to, middle(from));
        let (dx, dy) = (head.0 - tail.0, head.1 - tail.1);
        let id = fresh(&mut taken, &format!("e-{}-{}", edge.from, edge.to));
        let mut arrow = shape(&id, "arrow", tail, (dx.abs(), dy.abs()), seed);
        arrow.points = Some(vec![[0.0, 0.0], [dx, dy]]);
        dress(
            &mut arrow,
            edge.style.unwrap_or(Line::Solid),
            edge.arrow.unwrap_or(true),
        );
        let label = edge.label.as_deref().map(|text| {
            let label = text_in(
                &fresh(&mut taken, &format!("{id}-label")),
                text,
                &id,
                (tail.0 + dx / 2.0, tail.1 + dy / 2.0),
                seed,
            );
            arrow.set_bound_elements(&[BoundElement {
                id: label.id.clone(),
                kind: BoundKind::Text,
            }]);
            label
        });
        ops.push(add(arrow));
        ops.extend(label.map(add));
        for (end, target, point) in [(ArrowEnd::Start, from, tail), (ArrowEnd::End, to, head)] {
            ops.push(Op::BindArrow {
                arrow: id.clone(),
                end,
                target: target.id.clone(),
                fixed_point: share(point, target),
            });
        }
        made.push(id);
    }
    Ok((ops, made))
}

fn label_of<'a>(scene: &'a Scene, element: &Element) -> Result<Option<&'a Element>, DocError> {
    Ok(element
        .bound_elements()?
        .iter()
        .filter(|bound| bound.kind == BoundKind::Text)
        .find_map(|bound| scene.get(&bound.id)))
}

pub fn update(scene: &Scene, patches: &[Patch]) -> Result<Vec<Op>, DocError> {
    let keys = keyed(scene);
    let mut seen = HashSet::new();
    let mut taken = ids(scene);
    let mut ops = Vec::with_capacity(patches.len() * 2);
    for (seed, patch) in patches.iter().enumerate() {
        if !seen.insert(patch.key.as_str()) {
            return Err(DocError::DuplicateKey(patch.key.clone()));
        }
        let mut element = found(&keys, &patch.key)?.clone();
        colored(&mut element, patch.fill.as_ref(), patch.stroke.as_ref());
        if let Some(meta) = &patch.meta {
            element
                .extra
                .insert("customData".to_owned(), custom(&patch.key, Some(meta)));
        }
        if let Some(text) = patch.label.as_deref() {
            let at = middle(&element);
            match (element.kind.as_str(), label_of(scene, &element)?) {
                ("text", _) => {
                    element.extra.insert("text".to_owned(), json!(text));
                    element.extra.insert("originalText".to_owned(), json!(text));
                }
                (_, Some(old)) => {
                    let sized = text_in(&old.id, text, &element.id, at, seed);
                    let mut label = old.clone();
                    (label.x, label.width) = (sized.x, sized.width);
                    label.extra.insert("text".to_owned(), json!(text));
                    label.extra.insert("originalText".to_owned(), json!(text));
                    ops.push(Op::Update(Box::new(label)));
                }
                (_, None) => {
                    let label = text_in(
                        &fresh(&mut taken, &format!("{}-label", element.id)),
                        text,
                        &element.id,
                        at,
                        seed,
                    );
                    let mut bound = element.bound_elements()?;
                    bound.push(BoundElement {
                        id: label.id.clone(),
                        kind: BoundKind::Text,
                    });
                    element.set_bound_elements(&bound);
                    ops.push(add(label));
                }
            }
        }
        ops.push(Op::Update(Box::new(element)));
    }
    Ok(ops)
}

pub fn delete(scene: &Scene, keys: &[String]) -> Result<Vec<Op>, DocError> {
    let index = keyed(scene);
    let mut gone: Vec<String> = Vec::new();
    let mut arrows = Vec::new();
    for key in keys {
        let element = found(&index, key)?;
        gone.push(element.id.clone());
        for bound in element.bound_elements()? {
            gone.push(bound.id.clone());
            if bound.kind == BoundKind::Arrow
                && let Some(arrow) = scene.get(&bound.id)
            {
                arrows.push(arrow);
            }
        }
    }
    for arrow in &arrows {
        gone.extend(arrow.bound_elements()?.into_iter().map(|bound| bound.id));
    }
    let mut unique = HashSet::new();
    gone.retain(|id| scene.get(id).is_some() && unique.insert(id.clone()));
    let mut touched: Vec<Element> = Vec::new();
    for arrow in arrows {
        for end in [ArrowEnd::Start, ArrowEnd::End] {
            let Some(binding) = arrow.binding(end)? else {
                continue;
            };
            let target = binding.element_id();
            if unique.contains(target) {
                continue;
            }
            let at = match touched.iter().position(|element| element.id == target) {
                Some(at) => at,
                None => match scene.get(target) {
                    Some(element) => {
                        touched.push(element.clone());
                        touched.len() - 1
                    }
                    None => continue,
                },
            };
            if let Some(element) = touched.get_mut(at) {
                let mut bound = element.bound_elements()?;
                bound.retain(|bound| bound.id != arrow.id);
                element.set_bound_elements(&bound);
            }
        }
    }
    Ok(touched
        .into_iter()
        .map(|element| Op::Update(Box::new(element)))
        .chain(gone.into_iter().map(|id| Op::Delete { id }))
        .collect())
}
