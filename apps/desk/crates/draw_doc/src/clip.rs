use std::collections::{HashMap, HashSet};

use serde::Deserialize;
use serde_json::{Map, Value, json};

use crate::edit::union;
use crate::element::{ArrowEnd, BoundElement, BoundKind, Element};
use crate::{DocError, Op, Scene};

const CLIPBOARD: &str = "excalidraw/clipboard";
const MINT_TRIES: usize = 64;

#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Landing {
    Pointer(f64, f64),
    Offset(f64),
}

#[derive(Deserialize)]
struct Clip {
    elements: Vec<Element>,
}

pub fn clip(scene: &Scene, ids: &[String]) -> Result<String, DocError> {
    let mut kept: Vec<&str> = Vec::new();
    for id in ids {
        let Some(element) = scene
            .get(id)
            .filter(|element| element.is_deleted != Some(true))
        else {
            continue;
        };
        let labels = element.bound_elements()?;
        let labels = labels
            .iter()
            .filter(|bound| bound.kind == BoundKind::Text)
            .filter_map(|bound| scene.get(&bound.id))
            .map(|label| label.id.as_str());
        for id in std::iter::once(element.id.as_str()).chain(labels) {
            if !kept.contains(&id) {
                kept.push(id);
            }
        }
    }
    let elements: Vec<&Element> = scene
        .elements()
        .iter()
        .filter(|element| kept.contains(&element.id.as_str()))
        .collect();
    Ok(serde_json::to_string(
        &json!({ "type": CLIPBOARD, "elements": elements, "files": {} }),
    )?)
}

pub fn unclip(text: &str) -> Result<Option<Vec<Element>>, DocError> {
    let Ok(Value::Object(file)) = serde_json::from_str::<Value>(text) else {
        return Ok(None);
    };
    if file.get("type").and_then(Value::as_str) != Some(CLIPBOARD) {
        return Ok(None);
    }
    let clip = Clip::deserialize(Value::Object(file))?;
    Ok(Some(
        clip.elements
            .into_iter()
            .filter(|element| element.is_deleted != Some(true))
            .collect(),
    ))
}

pub fn paste(
    scene: &Scene,
    elements: &[Element],
    landing: Landing,
    mut mint: impl FnMut() -> String,
) -> Result<(Vec<Op>, Vec<String>), DocError> {
    let mut taken: HashSet<String> = scene
        .elements()
        .iter()
        .map(|element| element.id.clone())
        .collect();
    let mut fresh = || -> Result<String, DocError> {
        for _ in 0..MINT_TRIES {
            let id = mint();
            if taken.insert(id.clone()) {
                return Ok(id);
            }
        }
        Err(DocError::DuplicateId(mint()))
    };
    let mut renamed = HashMap::with_capacity(elements.len());
    for element in elements {
        renamed.insert(element.id.clone(), fresh()?);
    }
    let mut groups: HashMap<String, String> = HashMap::new();
    let (dx, dy) = match landing {
        Landing::Offset(by) => (by, by),
        Landing::Pointer(x, y) => match union(elements.iter().map(Element::bounds)) {
            Some(around) => (
                x - (around.min_x + around.max_x) / 2.0,
                y - (around.min_y + around.max_y) / 2.0,
            ),
            None => (0.0, 0.0),
        },
    };
    let mut ops = Vec::with_capacity(elements.len());
    let mut selected = Vec::new();
    for element in elements {
        let Some(id) = renamed.get(&element.id) else {
            continue;
        };
        let mut copy = Element {
            id: id.clone(),
            x: element.x + dx,
            y: element.y + dy,
            ..element.clone()
        };
        for end in [ArrowEnd::Start, ArrowEnd::End] {
            if let Some(binding) = element.binding(end)? {
                let target = renamed.get(binding.element_id());
                let rebound = match (binding_value(element, end), target) {
                    (Some(mut value), Some(target)) => {
                        value.insert("elementId".to_owned(), json!(target));
                        Value::Object(value)
                    }
                    _ => Value::Null,
                };
                copy.extra.insert(end.field().to_owned(), rebound);
            }
        }
        let bound: Vec<BoundElement> = element
            .bound_elements()?
            .into_iter()
            .filter_map(|bound| {
                Some(BoundElement {
                    id: renamed.get(&bound.id)?.clone(),
                    kind: bound.kind,
                })
            })
            .collect();
        if element.extra.contains_key("boundElements") {
            match bound.is_empty() {
                true => {
                    copy.extra.insert("boundElements".to_owned(), Value::Null);
                }
                false => copy.set_bound_elements(&bound),
            }
        }
        let mut contained = false;
        for field in ["containerId", "frameId"] {
            if let Some(owner) = element.extra.get(field).and_then(Value::as_str) {
                let moved = renamed.get(owner);
                contained |= field == "containerId" && moved.is_some();
                copy.extra
                    .insert(field.to_owned(), moved.map_or(Value::Null, |id| json!(id)));
            }
        }
        if let Some(Value::Array(list)) = element.extra.get("groupIds") {
            let mut regrouped = Vec::with_capacity(list.len());
            for group in list.iter().filter_map(Value::as_str) {
                let new = match groups.get(group) {
                    Some(new) => new.clone(),
                    None => {
                        let new = fresh()?;
                        groups.insert(group.to_owned(), new.clone());
                        new
                    }
                };
                regrouped.push(json!(new));
            }
            copy.extra
                .insert("groupIds".to_owned(), Value::Array(regrouped));
        }
        if !contained {
            selected.push(copy.id.clone());
        }
        ops.push(Op::Add {
            at: None,
            element: Box::new(copy),
        });
    }
    Ok((ops, selected))
}

fn binding_value(element: &Element, end: ArrowEnd) -> Option<Map<String, Value>> {
    match element.extra.get(end.field())? {
        Value::Object(value) => Some(value.clone()),
        _ => None,
    }
}
