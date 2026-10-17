use std::collections::hash_map::RandomState;
use std::hash::BuildHasher;
use std::time::{SystemTime, UNIX_EPOCH};

use draw_doc::Element;
use gpui::CursorStyle;
use serde_json::{Map, Value, json};

use crate::edit;
use crate::shape::{self, FONT_SIZE, Kind, LINE_HEIGHT};
use crate::style::{Pick, Size, Style};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Tool {
    Select,
    Hand,
    Rectangle,
    Ellipse,
    Arrow,
    Line,
    Pencil,
    Text,
    Eraser,
    Frame,
    Note,
    Laser,
}

const NOTE_FILL: &str = "#ffec99";
const NOTE_INK: &str = "#f08c00";
const FRAME_INK: &str = "#9fa8b2";

impl Tool {
    pub const ALL: [Tool; 9] = [
        Tool::Select,
        Tool::Hand,
        Tool::Rectangle,
        Tool::Ellipse,
        Tool::Arrow,
        Tool::Line,
        Tool::Pencil,
        Tool::Text,
        Tool::Eraser,
    ];

    pub const MORE: [Tool; 3] = [Tool::Frame, Tool::Note, Tool::Laser];

    pub fn key(self) -> &'static str {
        match self {
            Tool::Select => "v",
            Tool::Hand => "h",
            Tool::Rectangle => "r",
            Tool::Ellipse => "o",
            Tool::Arrow => "a",
            Tool::Line => "l",
            Tool::Pencil => "p",
            Tool::Text => "t",
            Tool::Eraser => "e",
            Tool::Frame => "f",
            Tool::Note => "n",
            Tool::Laser => "k",
        }
    }

    pub fn digit(self) -> Option<&'static str> {
        match self {
            Tool::Select => Some("1"),
            Tool::Hand => Some("2"),
            Tool::Rectangle => Some("3"),
            Tool::Ellipse => Some("4"),
            Tool::Arrow => Some("5"),
            Tool::Line => Some("6"),
            Tool::Pencil => Some("7"),
            Tool::Text => Some("8"),
            Tool::Eraser => Some("9"),
            Tool::Frame | Tool::Note | Tool::Laser => None,
        }
    }

    pub fn name(self) -> &'static str {
        match self {
            Tool::Select => "Select",
            Tool::Hand => "Hand",
            Tool::Rectangle => "Rectangle",
            Tool::Ellipse => "Ellipse",
            Tool::Arrow => "Arrow",
            Tool::Line => "Line",
            Tool::Pencil => "Pencil",
            Tool::Text => "Text",
            Tool::Eraser => "Eraser",
            Tool::Frame => "Frame",
            Tool::Note => "Note",
            Tool::Laser => "Laser",
        }
    }

    pub fn of_key(key: &str) -> Option<Tool> {
        Tool::ALL
            .into_iter()
            .chain(Tool::MORE)
            .find(|tool| tool.key() == key || tool.digit() == Some(key))
    }

    pub fn cursor(self) -> CursorStyle {
        match self {
            Tool::Select => CursorStyle::Arrow,
            Tool::Hand => CursorStyle::OpenHand,
            Tool::Text => CursorStyle::IBeam,
            Tool::Rectangle
            | Tool::Ellipse
            | Tool::Arrow
            | Tool::Line
            | Tool::Pencil
            | Tool::Eraser
            | Tool::Frame
            | Tool::Note
            | Tool::Laser => CursorStyle::Crosshair,
        }
    }

    pub fn draws(self) -> bool {
        self.kind().is_some()
    }

    pub fn keeps(self) -> bool {
        match self {
            Tool::Pencil | Tool::Line | Tool::Arrow | Tool::Laser => true,
            Tool::Select
            | Tool::Hand
            | Tool::Rectangle
            | Tool::Ellipse
            | Tool::Text
            | Tool::Eraser
            | Tool::Frame
            | Tool::Note => false,
        }
    }

    fn kind(self) -> Option<&'static str> {
        match self {
            Tool::Select | Tool::Hand | Tool::Eraser | Tool::Laser => None,
            Tool::Rectangle | Tool::Note => Some("rectangle"),
            Tool::Frame => Some("frame"),
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

    pub fn id(&mut self) -> String {
        format!("{:016x}", self.next())
    }
}

fn now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .ok()
        .and_then(|since| u64::try_from(since.as_millis()).ok())
        .unwrap_or_default()
}

pub fn image(
    mint: &mut Mint,
    file: &str,
    (x, y): (f64, f64),
    (width, height): (f64, f64),
) -> Element {
    let file_id = file
        .rsplit('/')
        .next()
        .and_then(|name| name.split('.').next())
        .unwrap_or(file);
    let extra = match json!({
        "opacity": 100,
        "groupIds": [],
        "frameId": null,
        "roundness": null,
        "seed": mint.seed(),
        "version": 1,
        "versionNonce": mint.seed(),
        "boundElements": null,
        "updated": now(),
        "link": null,
        "locked": false,
        "strokeColor": "transparent",
        "backgroundColor": "transparent",
        "status": "saved",
        "fileId": file_id,
        "scale": [1, 1],
        "customData": { "file": file },
    }) {
        Value::Object(map) => map,
        _ => Map::new(),
    };
    Element {
        id: mint.id(),
        kind: "image".to_owned(),
        x,
        y,
        width,
        height,
        angle: Some(0.0),
        is_deleted: Some(false),
        points: None,
        extra,
    }
}

pub fn fresh(tool: Tool, mint: &mut Mint, style: &Style, at: (f64, f64)) -> Option<Element> {
    let kind = tool.kind()?;
    let updated = now();
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
    let fields = match tool {
        Tool::Select | Tool::Hand | Tool::Eraser | Tool::Ellipse | Tool::Laser => vec![],
        Tool::Rectangle | Tool::Note => vec![("roundness", json!({ "type": 3 }))],
        Tool::Frame => vec![("name", Value::Null)],
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
    let mut element = Element {
        id: mint.id(),
        kind: kind.to_owned(),
        x: at.0,
        y: at.1,
        width: 0.0,
        height: 0.0,
        angle: Some(0.0),
        is_deleted: Some(false),
        points: traced.then(|| vec![[0.0, 0.0]]),
        extra,
    };
    for pick in style.picks() {
        pick.apply(&mut element);
    }
    let dressed = match tool {
        Tool::Note => vec![
            ("backgroundColor", json!(NOTE_FILL)),
            ("fillStyle", json!("solid")),
            ("strokeColor", json!(NOTE_INK)),
            ("strokeStyle", json!("solid")),
            ("roughness", json!(0)),
        ],
        Tool::Frame => vec![
            ("backgroundColor", json!("transparent")),
            ("strokeColor", json!(FRAME_INK)),
            ("strokeStyle", json!("solid")),
            ("strokeWidth", json!(1)),
            ("roughness", json!(0)),
        ],
        _ => Vec::new(),
    };
    for (field, value) in dressed {
        element.extra.insert(field.to_owned(), value);
    }
    if tool == Tool::Text {
        element.height = shape::line_step(&element);
    }
    Some(element)
}

pub fn label(mint: &mut Mint, style: &Style, host: &Element) -> Option<Element> {
    let bounds = host.bounds();
    let middle = (
        (bounds.min_x + bounds.max_x) / 2.0,
        (bounds.min_y + bounds.max_y) / 2.0,
    );
    let mut label = fresh(Tool::Text, mint, style, middle)?;
    let stroke = host
        .extra
        .get("strokeColor")
        .cloned()
        .unwrap_or_else(|| json!("#1e1e1e"));
    let font = Size::ALL
        .into_iter()
        .find(|size| Pick::Size(*size).on(host))
        .unwrap_or(Size::M)
        .label(Kind::of(host));
    for (field, value) in [
        ("textAlign", json!("center")),
        ("verticalAlign", json!("middle")),
        ("containerId", json!(host.id)),
        ("backgroundColor", json!("transparent")),
        ("strokeColor", stroke),
        ("fontSize", json!(font)),
    ] {
        label.extra.insert(field.to_owned(), value);
    }
    label.height = shape::line_step(&label);
    Some(label)
}

pub fn drawn(from: &Element, trace: &[(f64, f64)]) -> Element {
    if Kind::of(from).traced() {
        return match (edit::elbowed(from), trace) {
            (true, [start, .., end]) => shape::with_trace(
                from,
                &edit::elbow(
                    *start,
                    *end,
                    edit::across(None, *start, *end),
                    edit::midpoint(from),
                ),
            ),
            _ => shape::with_trace(from, trace),
        };
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
