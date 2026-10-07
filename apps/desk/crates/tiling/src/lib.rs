mod keys;
mod solve;
mod store;
mod workspace;

use std::fmt;

pub use keys::{Action, Key, Mods, SHORTCUTS, Shortcut, action};
pub use solve::{Corner, Divider, aim, solve, target};
pub use store::{Store, StoreError};
pub use workspace::{Grab, Preview, Spawned, Workspace};

pub const GAP: f32 = 6.0;
pub const MIN_TILE_WIDTH: f32 = 240.0;
pub const MIN_TILE_HEIGHT: f32 = 160.0;
pub const MAX_TILES: usize = 12;
pub const DRAG_THRESHOLD: f32 = 4.0;
pub const WORKSPACE_EDGE: f32 = 24.0;
pub const STRIP_HEIGHT: f32 = 240.0;
pub const STRIP_WIDTH: f32 = 360.0;
pub const NUDGE: f32 = 32.0;
pub const HISTORY_DEPTH: usize = 50;
pub const HEADER_ZONE: f32 = 40.0;
pub const CORNER_ZONE: f32 = 16.0;
pub const HOLD: f32 = 12.0;
pub const EDGE_BAR: f32 = 2.0;
const SLACK: f32 = 0.5;
pub const TILE_MINIMUM: Minimum = Minimum {
    w: MIN_TILE_WIDTH,
    h: MIN_TILE_HEIGHT,
};
pub const CHAT_MINIMUM: Minimum = Minimum { w: 360.0, h: 240.0 };
pub const EDITOR_MINIMUM: Minimum = Minimum { w: 400.0, h: 240.0 };
pub const BROWSER_MINIMUM: Minimum = Minimum { w: 480.0, h: 320.0 };
pub const SOURCE_CONTROL_MINIMUM: Minimum = Minimum { w: 280.0, h: 200.0 };
const CENTRE_ZONE: f32 = 0.4;
const SIDE_ZONE: f32 = (1.0 - CENTRE_ZONE) / 2.0;
const WORK_CHAT_SHARE: f32 = 0.5496;
const DATA_STUDIO: &str = "data-studio";

#[derive(Clone, Copy, PartialEq, Debug)]
pub struct Minimum {
    pub w: f32,
    pub h: f32,
}

#[derive(Clone, PartialEq, Eq, Debug)]
pub enum Module {
    Chat,
    SubAgents,
    FileEdits,
    Shells,
    Editor,
    Terminal,
    Browser,
    SourceControl,
    Plugin(String),
}

impl Module {
    pub const BUILT_IN: [Module; 8] = [
        Module::Chat,
        Module::SubAgents,
        Module::FileEdits,
        Module::Shells,
        Module::Editor,
        Module::Terminal,
        Module::Browser,
        Module::SourceControl,
    ];

    pub fn name(&self) -> &str {
        match self {
            Module::Chat => "Chat",
            Module::SubAgents => "Sub-agents",
            Module::FileEdits => "File edits",
            Module::Shells => "Shells",
            Module::Editor => "Editor",
            Module::Terminal => "Terminal",
            Module::Browser => "Browser",
            Module::SourceControl => "Source control",
            Module::Plugin(id) => id,
        }
    }

    pub fn minimum(&self) -> Minimum {
        match self {
            Module::Chat => CHAT_MINIMUM,
            Module::Editor => EDITOR_MINIMUM,
            Module::Browser => BROWSER_MINIMUM,
            Module::SourceControl => SOURCE_CONTROL_MINIMUM,
            Module::SubAgents
            | Module::FileEdits
            | Module::Shells
            | Module::Terminal
            | Module::Plugin(_) => TILE_MINIMUM,
        }
    }

    fn key(&self) -> &'static str {
        match self {
            Module::Chat => "chat",
            Module::SubAgents => "sub-agents",
            Module::FileEdits => "file-edits",
            Module::Shells => "shells",
            Module::Editor => "editor",
            Module::Terminal => "terminal",
            Module::Browser => "browser",
            Module::SourceControl => "source-control",
            Module::Plugin(_) => "plugin",
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Axis {
    Row,
    Column,
}

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Side {
    Left,
    Right,
    Top,
    Bottom,
}

impl Side {
    pub const ALL: [Side; 4] = [Side::Left, Side::Right, Side::Top, Side::Bottom];

    pub fn axis(self) -> Axis {
        match self {
            Side::Left | Side::Right => Axis::Row,
            Side::Top | Side::Bottom => Axis::Column,
        }
    }

    fn leads(self) -> bool {
        matches!(self, Side::Left | Side::Top)
    }
}

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Zone {
    Side(Side),
    Stack { at: usize },
}

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Target {
    Tile(TileId, Zone),
    Edge(Side),
}

#[derive(Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Debug)]
pub struct TileId(pub u32);

#[derive(Clone, Copy, PartialEq, Debug)]
pub enum Size {
    Share(f32),
    Fixed(f32),
}

#[derive(Clone, Debug, PartialEq)]
pub struct Stack {
    pub id: TileId,
    pub modules: Vec<Module>,
    pub active: usize,
}

impl Stack {
    pub fn minimum(&self) -> Minimum {
        self.modules
            .iter()
            .map(Module::minimum)
            .fold(TILE_MINIMUM, |most, one| Minimum {
                w: most.w.max(one.w),
                h: most.h.max(one.h),
            })
    }
}

#[derive(Clone, Debug, PartialEq)]
pub struct Part {
    pub size: Size,
    pub node: Node,
}

#[derive(Clone, Debug, PartialEq)]
pub enum Node {
    Tile(Stack),
    Split { axis: Axis, parts: Vec<Part> },
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Rect {
    pub x: f32,
    pub y: f32,
    pub w: f32,
    pub h: f32,
}

impl Rect {
    pub fn contains(self, x: f32, y: f32) -> bool {
        x >= self.x && x < self.x + self.w && y >= self.y && y < self.y + self.h
    }

    fn along(self, axis: Axis) -> (f32, f32) {
        match axis {
            Axis::Row => (self.x, self.w),
            Axis::Column => (self.y, self.h),
        }
    }

    fn across(self, axis: Axis) -> (f32, f32) {
        match axis {
            Axis::Row => (self.y, self.h),
            Axis::Column => (self.x, self.w),
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Preset {
    Work,
    Editor,
    Data,
    Empty,
}

impl Preset {
    pub const ALL: [Preset; 4] = [Preset::Work, Preset::Editor, Preset::Data, Preset::Empty];

    pub fn name(self) -> &'static str {
        match self {
            Preset::Work => "work",
            Preset::Editor => "editor",
            Preset::Data => "data",
            Preset::Empty => "empty",
        }
    }

    fn tree(self, next: &mut u32) -> Option<Node> {
        let mut tile = |modules: Vec<Module>| {
            let id = TileId(*next);
            *next += 1;
            Node::Tile(Stack {
                id,
                modules,
                active: 0,
            })
        };
        match self {
            Preset::Work => Some(Node::Split {
                axis: Axis::Row,
                parts: vec![
                    Part {
                        size: Size::Share(WORK_CHAT_SHARE),
                        node: tile(vec![Module::Chat]),
                    },
                    Part {
                        size: Size::Share(1.0 - WORK_CHAT_SHARE),
                        node: tile(vec![Module::SubAgents, Module::FileEdits, Module::Shells]),
                    },
                ],
            }),
            Preset::Editor => Some(tile(vec![Module::Editor])),
            Preset::Data => Some(tile(vec![Module::Plugin(DATA_STUDIO.to_owned())])),
            Preset::Empty => None,
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Refusal {
    TooMany,
    TooSmall,
    Locked,
    NoSuchTile,
    NoHistory,
}

impl fmt::Display for Refusal {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Refusal::TooMany => write!(f, "a workspace holds at most {MAX_TILES} tiles"),
            Refusal::TooSmall => write!(
                f,
                "a tile cannot be smaller than its module's minimum, {MIN_TILE_WIDTH} by {MIN_TILE_HEIGHT} px at least"
            ),
            Refusal::Locked => write!(f, "this workspace is locked, so it takes no new tiles"),
            Refusal::NoSuchTile => write!(f, "there is no tile there"),
            Refusal::NoHistory => write!(f, "there is nothing to undo"),
        }
    }
}

impl std::error::Error for Refusal {}

pub fn readout(node: Option<&Node>) -> Vec<String> {
    let Some(node) = node else {
        return vec!["empty".to_owned()];
    };
    let mut lines = Vec::new();
    let mut pending = vec![(node, 0usize)];
    while let Some((node, depth)) = pending.pop() {
        let indent = "  ".repeat(depth);
        match node {
            Node::Tile(stack) => {
                let names: Vec<&str> = stack.modules.iter().map(Module::name).collect();
                lines.push(format!("{indent}Tile {} {}", stack.id.0, names.join(", ")));
            }
            Node::Split { axis, parts } => {
                let total: f32 = parts
                    .iter()
                    .filter_map(|part| match part.size {
                        Size::Share(share) => Some(share),
                        Size::Fixed(_) => None,
                    })
                    .sum();
                let sizes: Vec<String> = parts
                    .iter()
                    .map(|part| match part.size {
                        Size::Share(share) => {
                            let fraction = format!("{:.2}", share / total);
                            format!("Share {}", fraction.trim_start_matches('0'))
                        }
                        Size::Fixed(pixels) => format!("Fixed {}", pixels.round()),
                    })
                    .collect();
                lines.push(format!("{indent}{axis:?}[{}]", sizes.join(", ")));
                pending.extend(parts.iter().rev().map(|part| (&part.node, depth + 1)));
            }
        }
    }
    lines
}
