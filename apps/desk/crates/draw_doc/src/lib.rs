mod diagram;
mod edit;
mod element;
mod layout;
mod scene;

use std::fmt;

pub use diagram::{Edge, Graph, Line, Node, diagram, graph};
pub use edit::{Color, Link, Patch, Shape, ShapeKind, add_shapes, bounds, connect, delete, update};
pub use element::{ArrowEnd, BindMode, Binding, BoundElement, BoundKind, Element, Rect};
pub use scene::{Op, Scene};

#[derive(Debug)]
pub enum DocError {
    Json(serde_json::Error),
    Field {
        id: String,
        field: &'static str,
        source: serde_json::Error,
    },
    UnknownId(String),
    DuplicateId(String),
    UnknownKey(String),
    DuplicateKey(String),
    NotAnArrow(String),
    OutOfRange {
        at: usize,
        len: usize,
    },
    Unwind(Box<DocError>, Box<DocError>),
}

impl fmt::Display for DocError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            DocError::Json(error) => write!(f, "excalidraw json: {error}"),
            DocError::Field { id, field, source } => {
                write!(f, "element {id} field {field}: {source}")
            }
            DocError::UnknownId(id) => write!(f, "no element with id {id}"),
            DocError::DuplicateId(id) => write!(f, "element id {id} already exists"),
            DocError::UnknownKey(key) => write!(f, "no shape with key {key}"),
            DocError::DuplicateKey(key) => write!(f, "key {key} is used twice"),
            DocError::NotAnArrow(id) => write!(f, "element {id} is not an arrow"),
            DocError::OutOfRange { at, len } => {
                write!(f, "position {at} is past the {len} elements")
            }
            DocError::Unwind(first, unwind) => {
                write!(f, "{first}, and undoing the partial step failed: {unwind}")
            }
        }
    }
}

impl std::error::Error for DocError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            DocError::Json(error) | DocError::Field { source: error, .. } => Some(error),
            DocError::Unwind(first, _) => Some(first.as_ref()),
            DocError::UnknownId(_)
            | DocError::DuplicateId(_)
            | DocError::UnknownKey(_)
            | DocError::DuplicateKey(_)
            | DocError::NotAnArrow(_)
            | DocError::OutOfRange { .. } => None,
        }
    }
}

impl From<serde_json::Error> for DocError {
    fn from(error: serde_json::Error) -> Self {
        DocError::Json(error)
    }
}
