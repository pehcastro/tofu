use std::collections::HashMap;

use serde::{Deserialize, Serialize};
use serde_json::{Map, Value, json};

use crate::DocError;
use crate::element::{ArrowEnd, BoundElement, BoundKind, Element, Rect};

#[derive(Clone, Debug, PartialEq)]
pub enum Op {
    Add {
        at: Option<usize>,
        element: Box<Element>,
    },
    Update(Box<Element>),
    Delete {
        id: String,
    },
    Reorder {
        id: String,
        to: usize,
    },
    BindArrow {
        arrow: String,
        end: ArrowEnd,
        target: String,
        fixed_point: [f64; 2],
    },
}

#[derive(Debug, Default)]
pub struct Scene {
    elements: Vec<Element>,
    by_id: HashMap<String, usize>,
    rest: Map<String, Value>,
    undo: Vec<Vec<Op>>,
    redo: Vec<Vec<Op>>,
}

#[derive(Deserialize)]
struct FileIn {
    elements: Vec<Element>,
    #[serde(flatten)]
    rest: Map<String, Value>,
}

#[derive(Serialize)]
struct FileOut<'a> {
    elements: &'a [Element],
    #[serde(flatten)]
    rest: &'a Map<String, Value>,
}

impl Scene {
    pub fn new() -> Self {
        let rest = [
            ("type", json!("excalidraw")),
            ("version", json!(2)),
            ("source", json!("tofu-draw")),
            ("appState", json!({})),
            ("files", json!({})),
        ];
        let rest = rest
            .into_iter()
            .map(|(key, value)| (key.to_owned(), value))
            .collect();
        Self {
            rest,
            ..Self::default()
        }
    }

    pub fn read(json: &str) -> Result<Self, DocError> {
        let file: FileIn = serde_json::from_str(json)?;
        Self {
            elements: file.elements,
            rest: file.rest,
            ..Self::default()
        }
        .indexed()
    }

    pub(crate) fn of(elements: Vec<Element>) -> Result<Self, DocError> {
        Self {
            elements,
            ..Self::new()
        }
        .indexed()
    }

    fn indexed(mut self) -> Result<Self, DocError> {
        self.by_id.reserve(self.elements.len());
        for (i, element) in self.elements.iter().enumerate() {
            if self.by_id.insert(element.id.clone(), i).is_some() {
                return Err(DocError::DuplicateId(element.id.clone()));
            }
        }
        Ok(self)
    }

    pub fn write(&self) -> Result<String, DocError> {
        Ok(serde_json::to_string(&FileOut {
            elements: &self.elements,
            rest: &self.rest,
        })?)
    }

    pub fn elements(&self) -> &[Element] {
        &self.elements
    }

    pub fn get(&self, id: &str) -> Option<&Element> {
        self.by_id.get(id).map(|&i| &self.elements[i])
    }

    pub fn cull(&self, view: Rect) -> impl Iterator<Item = &Element> {
        self.elements.iter().filter(move |element| {
            element.is_deleted != Some(true) && element.bounds().intersects(&view)
        })
    }

    pub fn apply(&mut self, ops: Vec<Op>) -> Result<(), DocError> {
        let inverse = self.run(ops)?;
        if !inverse.is_empty() {
            self.undo.push(inverse);
            self.redo.clear();
        }
        Ok(())
    }

    pub fn undo(&mut self) -> Result<bool, DocError> {
        let Some(step) = self.undo.pop() else {
            return Ok(false);
        };
        let inverse = self.run(step)?;
        self.redo.push(inverse);
        Ok(true)
    }

    pub fn redo(&mut self) -> Result<bool, DocError> {
        let Some(step) = self.redo.pop() else {
            return Ok(false);
        };
        let inverse = self.run(step)?;
        self.undo.push(inverse);
        Ok(true)
    }

    fn run(&mut self, ops: Vec<Op>) -> Result<Vec<Op>, DocError> {
        let mut inverse = Vec::with_capacity(ops.len());
        for op in ops {
            match self.step(op) {
                Ok(mut undo) => inverse.append(&mut undo),
                Err(error) => {
                    for op in inverse.into_iter().rev() {
                        if let Err(unwind) = self.step(op) {
                            return Err(DocError::Unwind(Box::new(error), Box::new(unwind)));
                        }
                    }
                    return Err(error);
                }
            }
        }
        inverse.reverse();
        Ok(inverse)
    }

    fn step(&mut self, op: Op) -> Result<Vec<Op>, DocError> {
        match op {
            Op::Add { at, element } => {
                if self.by_id.contains_key(&element.id) {
                    return Err(DocError::DuplicateId(element.id));
                }
                let len = self.elements.len();
                let at = at.unwrap_or(len);
                if at > len {
                    return Err(DocError::OutOfRange { at, len });
                }
                let id = element.id.clone();
                self.elements.insert(at, *element);
                self.reindex(at);
                Ok(vec![Op::Delete { id }])
            }
            Op::Update(element) => {
                let i = self.index(&element.id)?;
                let old = std::mem::replace(&mut self.elements[i], *element);
                Ok(vec![Op::Update(Box::new(old))])
            }
            Op::Delete { id } => {
                let i = self.index(&id)?;
                let element = self.elements.remove(i);
                self.by_id.remove(&id);
                self.reindex(i);
                Ok(vec![Op::Add {
                    at: Some(i),
                    element: Box::new(element),
                }])
            }
            Op::Reorder { id, to } => {
                let from = self.index(&id)?;
                let len = self.elements.len();
                if to >= len {
                    return Err(DocError::OutOfRange { at: to, len });
                }
                let element = self.elements.remove(from);
                self.elements.insert(to, element);
                self.reindex(from.min(to));
                Ok(vec![Op::Reorder { id, to: from }])
            }
            Op::BindArrow {
                arrow,
                end,
                target,
                fixed_point,
            } => self.bind(arrow, end, &target, fixed_point),
        }
    }

    fn bind(
        &mut self,
        arrow: String,
        end: ArrowEnd,
        target: &str,
        fixed_point: [f64; 2],
    ) -> Result<Vec<Op>, DocError> {
        let ai = self.index(&arrow)?;
        let ti = self.index(target)?;
        if self.elements[ai].kind != "arrow" {
            return Err(DocError::NotAnArrow(arrow));
        }
        let previous = match self.elements[ai].binding(end)? {
            Some(binding) if binding.element_id() != target => {
                self.by_id.get(binding.element_id()).copied()
            }
            _ => None,
        };
        let mut target_list = self.elements[ti].bound_elements()?;
        let previous_list = match previous {
            Some(pi) => Some((pi, self.elements[pi].bound_elements()?)),
            None => None,
        };
        let inverse = [Some(ai), Some(ti), previous]
            .into_iter()
            .flatten()
            .map(|i| Op::Update(Box::new(self.elements[i].clone())))
            .collect();
        self.elements[ai].bind_fixed(end, target, fixed_point);
        if let Some((pi, mut list)) = previous_list {
            list.retain(|bound| bound.id != arrow);
            self.elements[pi].set_bound_elements(&list);
        }
        if !target_list.iter().any(|bound| bound.id == arrow) {
            target_list.push(BoundElement {
                id: arrow,
                kind: BoundKind::Arrow,
            });
        }
        self.elements[ti].set_bound_elements(&target_list);
        Ok(inverse)
    }

    fn index(&self, id: &str) -> Result<usize, DocError> {
        self.by_id
            .get(id)
            .copied()
            .ok_or_else(|| DocError::UnknownId(id.to_owned()))
    }

    fn reindex(&mut self, from: usize) {
        for (i, element) in self.elements.iter().enumerate().skip(from) {
            match self.by_id.get_mut(&element.id) {
                Some(slot) => *slot = i,
                None => {
                    self.by_id.insert(element.id.clone(), i);
                }
            }
        }
    }
}
