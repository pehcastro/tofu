use schemars::JsonSchema;
use serde::{Deserialize, Deserializer, Serialize, de};
use std::str::FromStr;

pub type TileId = u32;

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "kebab-case")]
pub enum Module {
    Chat,
    SubAgents,
    FileEdits,
    Shells,
    Editor,
    Terminal,
    Browser,
    SourceControl,
    Plugin,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum Side {
    Left,
    Right,
    Top,
    Bottom,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum Axis {
    Row,
    Column,
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum Zone {
    Side(Side),
    Stack { at: u32 },
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum Target {
    Tile { tile: TileId, zone: Zone },
    Edge(Side),
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum Size {
    Share(f32),
    Fixed(f32),
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
pub struct Part {
    pub size: Size,
    pub node: Node,
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum Node {
    Tile {
        id: TileId,
        modules: Vec<Module>,
        active: u32,
    },
    Split {
        axis: Axis,
        parts: Vec<Part>,
    },
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
pub struct Rect {
    pub x: f32,
    pub y: f32,
    pub w: f32,
    pub h: f32,
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
pub struct Extent {
    pub w: u32,
    pub h: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
pub struct Position {
    pub l: u32,
    pub c: u32,
}

#[derive(Debug, Clone, Serialize, JsonSchema)]
pub struct Range {
    pub start: Position,
    pub end: Position,
}

impl<'de> Deserialize<'de> for Range {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        #[derive(Deserialize)]
        #[serde(untagged)]
        enum Form {
            Spelled(String),
            Whole { start: Position, end: Position },
        }
        match Form::deserialize(deserializer)? {
            Form::Whole { start, end } => Ok(Range { start, end }),
            Form::Spelled(text) => text.parse().map_err(de::Error::custom),
        }
    }
}

impl FromStr for Range {
    type Err = String;

    fn from_str(text: &str) -> Result<Self, Self::Err> {
        let position = |at: &str| {
            let (l, c) = at.split_once(':')?;
            Some(Position {
                l: l.trim().parse().ok()?,
                c: c.trim().parse().ok()?,
            })
        };
        let (start, end) = text.split_once('-').unwrap_or((text, text));
        position(start)
            .zip(position(end))
            .map(|(start, end)| Range { start, end })
            .ok_or(format!(
                "range `{text}` is not L:C-L:C or L:C, lines and columns counted from 1"
            ))
    }
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum Preset {
    Work,
    Editor,
    Data,
    Empty,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum Mode {
    Light,
    Dark,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum Step {
    Next,
    Prev,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum Access {
    Allow,
    Ask,
    Deny,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum ScreenGroup {
    Top,
    Insights,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum FocusKind {
    Tile,
    Palette,
    Dialog,
    StatusBar,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum PluginState {
    Loaded,
    Disabled,
    Crashed,
    NeedsConsent,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum TaskState {
    Working,
    Completed,
    Failed,
    Cancelled,
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum WorkspacePick {
    New,
    Id(String),
}
