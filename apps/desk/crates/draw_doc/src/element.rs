use serde::{Deserialize, Serialize};
use serde_json::{Map, Value, json};

use crate::DocError;

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Element {
    pub id: String,
    #[serde(rename = "type")]
    pub kind: String,
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub angle: Option<f64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub is_deleted: Option<bool>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub points: Option<Vec<[f64; 2]>>,
    #[serde(flatten)]
    pub extra: Map<String, Value>,
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Rect {
    pub min_x: f64,
    pub min_y: f64,
    pub max_x: f64,
    pub max_y: f64,
}

impl Rect {
    pub fn intersects(&self, other: &Rect) -> bool {
        self.min_x <= other.max_x
            && other.min_x <= self.max_x
            && self.min_y <= other.max_y
            && other.min_y <= self.max_y
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ArrowEnd {
    Start,
    End,
}

impl ArrowEnd {
    pub(crate) fn field(self) -> &'static str {
        match self {
            ArrowEnd::Start => "startBinding",
            ArrowEnd::End => "endBinding",
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum BindMode {
    Inside,
    Orbit,
    Skip,
}

#[derive(Clone, Debug, PartialEq, Deserialize)]
#[serde(untagged)]
pub enum Binding {
    #[serde(rename_all = "camelCase")]
    Fixed {
        element_id: String,
        fixed_point: [f64; 2],
        #[serde(default)]
        mode: Option<BindMode>,
    },
    #[serde(rename_all = "camelCase")]
    Legacy {
        element_id: String,
        focus: f64,
        gap: f64,
    },
}

impl Binding {
    pub fn element_id(&self) -> &str {
        match self {
            Binding::Fixed { element_id, .. } | Binding::Legacy { element_id, .. } => element_id,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum BoundKind {
    Arrow,
    Text,
}

#[derive(Clone, Debug, PartialEq, Deserialize)]
pub struct BoundElement {
    pub id: String,
    #[serde(rename = "type")]
    pub kind: BoundKind,
}

impl Element {
    pub fn bounds(&self) -> Rect {
        let (min_x, min_y, max_x, max_y) = match self.points.as_deref() {
            Some([first, rest @ ..]) => rest.iter().fold(
                (first[0], first[1], first[0], first[1]),
                |(a, b, c, d), p| (a.min(p[0]), b.min(p[1]), c.max(p[0]), d.max(p[1])),
            ),
            _ => (0.0, 0.0, self.width, self.height),
        };
        let (min_x, max_x) = (self.x + min_x.min(max_x), self.x + min_x.max(max_x));
        let (min_y, max_y) = (self.y + min_y.min(max_y), self.y + min_y.max(max_y));
        let angle = self.angle.unwrap_or(0.0);
        if angle == 0.0 {
            return Rect {
                min_x,
                min_y,
                max_x,
                max_y,
            };
        }
        let (cx, cy) = ((min_x + max_x) / 2.0, (min_y + max_y) / 2.0);
        let (hw, hh) = ((max_x - min_x) / 2.0, (max_y - min_y) / 2.0);
        let (sin, cos) = angle.sin_cos();
        let ex = (hw * cos).abs() + (hh * sin).abs();
        let ey = (hw * sin).abs() + (hh * cos).abs();
        Rect {
            min_x: cx - ex,
            min_y: cy - ey,
            max_x: cx + ex,
            max_y: cy + ey,
        }
    }

    pub fn binding(&self, end: ArrowEnd) -> Result<Option<Binding>, DocError> {
        match self.extra.get(end.field()) {
            None | Some(Value::Null) => Ok(None),
            Some(value) => Binding::deserialize(value)
                .map(Some)
                .map_err(|source| self.field_error(end.field(), source)),
        }
    }

    pub fn bound_elements(&self) -> Result<Vec<BoundElement>, DocError> {
        match self.extra.get("boundElements") {
            None | Some(Value::Null) => Ok(Vec::new()),
            Some(value) => Vec::<BoundElement>::deserialize(value)
                .map_err(|source| self.field_error("boundElements", source)),
        }
    }

    pub(crate) fn bind_fixed(&mut self, end: ArrowEnd, target: &str, fixed_point: [f64; 2]) {
        self.extra.insert(
            end.field().to_owned(),
            json!({ "elementId": target, "fixedPoint": fixed_point, "mode": "orbit" }),
        );
    }

    pub(crate) fn set_bound_elements(&mut self, list: &[BoundElement]) {
        let list: Vec<Value> = list
            .iter()
            .map(|bound| {
                let kind = match bound.kind {
                    BoundKind::Arrow => "arrow",
                    BoundKind::Text => "text",
                };
                json!({ "id": bound.id, "type": kind })
            })
            .collect();
        self.extra
            .insert("boundElements".to_owned(), Value::Array(list));
    }

    fn field_error(&self, field: &'static str, source: serde_json::Error) -> DocError {
        DocError::Field {
            id: self.id.clone(),
            field,
            source,
        }
    }
}
