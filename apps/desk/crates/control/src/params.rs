use crate::shapes::{Access, Mode, Node, Preset, Range, Side, Step, Target, TileId, WorkspacePick};
use crate::verbs::Method;
use schemars::JsonSchema;
use serde::{Deserialize, Deserializer, Serialize};
use serde_json::{Map, Value};

macro_rules! params {
    ($($name:ident { $($(#[$meta:meta])* $field:ident : $ty:ty),* $(,)? })*) => {
        $(
            #[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
            #[serde(deny_unknown_fields)]
            pub struct $name {
                $($(#[$meta])* pub $field: $ty,)*
            }
        )*
    };
}

params! {
    Empty {}
    Initialize { client: String, versions: Vec<u32>, #[serde(default)] capabilities: Vec<String>, token: String }
    SchemaQuery { method: Option<Method> }
    DeskOpen { project: Option<String>, screen: Option<String>, workspace: Option<WorkspacePick> }
    Quit { #[serde(default)] force: bool }
    WindowSize { w: u32, h: u32 }
    ScreenList { #[serde(default)] built: bool }
    ScreenOpen {
        name: String,
        workspace: Option<WorkspacePick>,
        board: Option<String>,
        file: Option<String>,
        dir: Option<String>,
        repo: Option<String>,
    }
    WorkspaceNew { name: Option<String>, preset: Option<Preset> }
    WorkspaceRef { id: String }
    WorkspaceRename { id: String, name: String }
    WorkspaceSwitch { to: Switch }
    WorkspaceReorder { id: String, to: u32 }
    WorkspaceIcon { id: String, icon: Option<String> }
    SendTile { tile: TileId, to: WorkspacePick }
    WorkspaceScope { workspace: Option<String> }
    TileRef { id: TileId }
    MaybeTile { id: Option<TileId> }
    TileFocus { id: Option<TileId>, toward: Option<Side> }
    TileOpen { module: crate::shapes::Module, plugin: Option<String>, target: Option<Target>, args: Option<Value> }
    TileSwap { a: TileId, b: TileId }
    TileResize { id: TileId, side: Side, by: Resize }
    TileZoom { id: Option<TileId>, #[serde(default)] off: bool }
    TabScope { tile: Option<TileId> }
    TabActivate { tile: TileId, tab: TabPick }
    TabStep { step: Step }
    TabClose { tile: Option<TileId>, index: Option<u32> }
    LayoutApply { layout: LayoutSource, #[serde(default)] dry_run: bool }
    LayoutEven { divider: Option<String> }
    FileOpen { path: String, line: Option<u32>, col: Option<u32>, tile: Option<TileId>, #[serde(default)] new_tab: bool }
    FileClose { path: String, #[serde(default)] discard: bool }
    MaybePath { path: Option<String> }
    PathRef { path: String }
    FileSaveAs { path: String, new: String }
    SelectionSet { path: String, range: Range }
    TextReplace {
        path: String,
        range: Range,
        #[serde(deserialize_with = "unescaped")] text: String,
        expect_hash: Option<String>,
    }
    Find { pattern: String, path: Option<String>, #[serde(default)] regex: bool }
    PaletteList { query: Option<String> }
    PaletteRun { id: String, args: Option<Value> }
    StatusClick { control: String }
    MaybeId { id: Option<String> }
    NotifyShow { title: String, #[serde(default)] body: String, tone: Option<String> }
    ThemeSet { name: Option<String>, mode: Option<Mode> }
    SettingKey { key: Option<String> }
    SettingSet { key: String, value: Value }
    ClipboardSet { text: String }
    Capture { target: Option<CaptureTarget>, format: Option<ImageFormat> }
    PluginList { filter: Option<PluginFilter> }
    PluginRef { id: String }
    PluginInstall { source: String }
    PluginUninstall { id: String, #[serde(default)] keep_data: bool }
    PluginGrant { id: String, capability: String, mode: Access }
    PluginActivity { id: String, since: Option<String> }
    PluginStateQuery { id: String, uri: Option<String> }
    PluginRun { id: String, command: String, args: Option<Value>, #[serde(default)] dry_run: bool }
    CommandList { plugin: Option<String>, #[serde(default)] agent_only: bool, cursor: Option<String> }
    CommandName { name: String }
    DrawBoard { board: String }
    DrawNode { key: String, label: String }
    DrawEdge { from: String, to: String, #[serde(default, skip_serializing_if = "Option::is_none")] label: Option<String> }
    DrawDiagram { board: String, nodes: Vec<DrawNode>, edges: Vec<DrawEdge>, layout: Option<String> }
    DrawShapes { board: String, shapes: Option<Vec<Value>>, #[serde(alias = "shapes-file")] shapes_file: Option<String> }
    DrawConnect { board: String, edges: Option<Vec<Value>>, #[serde(alias = "edges-file")] edges_file: Option<String> }
    DrawUpdate { board: String, patches: Option<Vec<Value>>, #[serde(alias = "patches-file")] patches_file: Option<String> }
    DrawDelete { board: String, keys: Value }
    DrawView { board: String, fit: Option<String> }
    DrawImport { path: String, board: Option<String> }
    CommandRun {
        name: String,
        args: Option<Map<String, Value>>,
        #[serde(default)] dry_run: bool,
        #[serde(default)] wait: bool,
        idempotency_key: Option<String>,
        timeout_ms: Option<u64>,
    }
    IdRef { id: String }
    UndoToken { token: Option<String> }
    UriRef { uri: String }
    StateList { prefix: Option<String>, cursor: Option<String> }
    EventsSince { cursor: u64 }
    Audit { since: Option<String>, caller: Option<String> }
    GateJudge { caller: String, command: String, arguments: Value, purpose: String }
    Conformance { plugin: String }
}

fn unescaped<'de, D: Deserializer<'de>>(deserializer: D) -> Result<String, D::Error> {
    let raw = String::deserialize(deserializer)?;
    let mut text = String::with_capacity(raw.len());
    let mut chars = raw.chars().peekable();
    while let Some(ch) = chars.next() {
        match (ch, chars.peek()) {
            ('\\', Some('n')) => {
                text.push('\n');
                chars.next();
            }
            ('\\', Some('\\')) => {
                text.push('\\');
                chars.next();
            }
            _ => text.push(ch),
        }
    }
    Ok(text)
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum Switch {
    Id(String),
    Name(String),
    Index(u32),
    Next,
    Prev,
    #[serde(untagged)]
    Bare(Value),
}

pub const SWITCH_FORMS: &str = "workspace.switch takes to as a name, a decimal index, next, prev, or one of {\"id\": s}, {\"name\": s}, {\"index\": n}";

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum TileMove {
    Target { id: TileId, target: Target },
    Side { id: TileId, side: Side },
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum Resize {
    Px(f32),
    Share(f32),
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum TabPick {
    Index(u32),
    Name(String),
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum LayoutSource {
    Tree(Node),
    Preset(Preset),
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum FileDiff {
    Pair { a: String, b: String },
    Against { path: String, against: String },
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum FocusTarget {
    Tile(TileId),
    Palette,
    Status(String),
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum CaptureTarget {
    Tile(TileId),
    Window,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum ImageFormat {
    Png,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum PluginFilter {
    Installed,
    Loaded,
}
