use std::collections::hash_map::RandomState;
use std::hash::BuildHasher;
use std::time::{SystemTime, UNIX_EPOCH};

use draw_doc::Element;
use serde_json::{Map, Value, json};

use crate::shape::{self, FONT_SIZE, Kind, LINE_HEIGHT};
use crate::style::Style;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Tool {
    Select,
    Rectangle,
    Ellipse,
    Arrow,
    Line,
    Text,
    Pencil,
}

impl Tool {
    pub const ALL: [Tool; 7] = [
        Tool::Select,
        Tool::Rectangle,
        Tool::Ellipse,
        Tool::Arrow,
        Tool::Line,
        Tool::Text,
        Tool::Pencil,
    ];

    pub fn key(self) -> &'static str {
        match self {
            Tool::Select => "v",
            Tool::Rectangle => "r",
            Tool::Ellipse => "o",
            Tool::Arrow => "a",
            Tool::Line => "l",
            Tool::Text => "t",
            Tool::Pencil => "p",
        }
    }

    pub fn name(self) -> &'static str {
        match self {
            Tool::Select => "Select",
            Tool::Rectangle => "Rectangle",
            Tool::Ellipse => "Ellipse",
            Tool::Arrow => "Arrow",
            Tool::Line => "Line",
            Tool::Text => "Text",
            Tool::Pencil => "Pencil",
        }
    }

    pub fn of_key(key: &str) -> Option<Tool> {
        Tool::ALL.into_iter().find(|tool| tool.key() == key)
    }

    fn kind(self) -> Option<&'static str> {
        match self {
            Tool::Select => None,
            Tool::Rectangle => Some("rectangle"),
            Tool::Ellipse => Some("ellipse"),
            Tool::Arrow => Some("arrow"),
            Tool::Line => Some("line"),
            Tool::Text => Some("text"),
            Tool::Pencil => Some("freedraw"),
        }
    }
}

pub struct Mint {
    state: RandomState,
    count: u64,
}

impl Mint {
    pub fn new() -> Self {
        Mint {
            state: RandomState::new(),
            count: 0,
        }
    }

    fn next(&mut self) -> u64 {
        self.count = self.count.wrapping_add(1);
        self.state.hash_one(self.count)
    }

    fn seed(&mut self) -> u64 {
        self.next() >> 33
    }
}

pub fn fresh(tool: Tool, mint: &mut Mint, style: &Style, at: (f64, f64)) -> Option<Element> {
    let kind = tool.kind()?;
    let updated = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .ok()
        .and_then(|since| u64::try_from(since.as_millis()).ok())
        .unwrap_or_default();
    let mut extra = match json!({
        "roughness": 1,
        "opacity": 100,
        "groupIds": [],
        "frameId": null,
        "roundness": null,
        "seed": mint.seed(),
        "version": 1,
        "versionNonce": mint.seed(),
        "boundElements": null,
        "updated": updated,
        "link": null,
        "locked": false,
    }) {
        Value::Object(map) => map,
        _ => Map::new(),
    };
    for pick in style {
        extra.insert(pick.field().to_owned(), pick.value());
    }
    let fields = match tool {
        Tool::Select | Tool::Ellipse => vec![],
        Tool::Rectangle => vec![("roundness", json!({ "type": 3 }))],
        Tool::Arrow | Tool::Line => vec![
            ("roundness", json!({ "type": 2 })),
            ("lastCommittedPoint", Value::Null),
            ("startBinding", Value::Null),
            ("endBinding", Value::Null),
            ("startArrowhead", Value::Null),
            (
                "endArrowhead",
                match tool {
                    Tool::Arrow => json!("arrow"),
                    _ => Value::Null,
                },
            ),
            ("elbowed", json!(false)),
        ],
        Tool::Pencil => vec![
            ("pressures", json!([])),
            ("simulatePressure", json!(true)),
            ("lastCommittedPoint", Value::Null),
        ],
        Tool::Text => vec![
            ("text", json!("")),
            ("originalText", json!("")),
            ("fontSize", json!(FONT_SIZE)),
            ("fontFamily", json!(5)),
            ("textAlign", json!("left")),
            ("verticalAlign", json!("top")),
            ("containerId", Value::Null),
            ("autoResize", json!(true)),
            ("lineHeight", json!(LINE_HEIGHT)),
        ],
    };
    for (field, value) in fields {
        extra.insert(field.to_owned(), value);
    }
    let traced = Kind::of_name(kind).traced();
    Some(Element {
        id: format!("{:016x}", mint.next()),
        kind: kind.to_owned(),
        x: at.0,
        y: at.1,
        width: 0.0,
        height: match tool {
            Tool::Text => FONT_SIZE * LINE_HEIGHT,
            _ => 0.0,
        },
        angle: Some(0.0),
        is_deleted: Some(false),
        points: traced.then(|| vec![[0.0, 0.0]]),
        extra,
    })
}

pub fn drawn(from: &Element, trace: &[(f64, f64)]) -> Element {
    if Kind::of(from).traced() {
        return shape::with_trace(from, trace);
    }
    let (Some(&first), Some(&last)) = (trace.first(), trace.last()) else {
        return from.clone();
    };
    Element {
        x: first.0.min(last.0),
        y: first.1.min(last.1),
        width: (last.0 - first.0).abs(),
        height: (last.1 - first.1).abs(),
        ..from.clone()
    }
}
