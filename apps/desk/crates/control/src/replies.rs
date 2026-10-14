use crate::error::ErrorCode;
use crate::shapes::{
    Access, Extent, FocusKind, Mode, Module, Node, PluginState, Range, Rect, ScreenGroup,
    TaskState, TileId,
};
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use serde_json::Value;

macro_rules! replies {
    ($($name:ident { $($(#[$meta:meta])* $field:ident : $ty:ty),* $(,)? })*) => {
        $(
            #[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
            pub struct $name {
                $($(#[$meta])* pub $field: $ty,)*
            }
        )*
    };
}

replies! {
    Ack { ok: bool }
    Initialized { version: u32, capabilities: Vec<String>, desk_version: String, pid: u32, project: String, features: Vec<String> }
    Pong { rev: u64, uptime_ms: u64 }
    Versions { cli: String, desk: String, contract: Vec<u32>, wit: Vec<String> }
    Opened { launched: bool, instance: String }
    Window { title: String, size: Extent, focused: bool, maximized: bool, theme: String, mode: Mode }
    Sized { size: Extent }
    Screen { name: String, label: String, group: ScreenGroup, glyph: String, built: bool, open: bool }
    ScreenOpened { screen: String, workspace: String, tile: Option<TileId> }
    ScreenCurrent { screen: String, workspace: String }
    Workspace {
        id: String,
        name: String,
        preset: String,
        locked: bool,
        pinned: bool,
        icon: Option<String>,
        tiles: u32,
        focus: Option<TileId>,
        zoomed: Option<TileId>,
        active: bool,
    }
    Created { id: String }
    Renamed { id: String, name: String }
    Active { active: String }
    Closed { closed: bool, undo: Option<String> }
    Order { order: Vec<String> }
    Locked { locked: bool }
    Pinned { pinned: bool }
    Icon { icon: Option<String> }
    Sent { from: String, to: String }
    Tree { tree: Node }
    ModuleInfo { name: String, kind: Module, plugin: Option<String> }
    Tile { id: TileId, rect: Rect, modules: Vec<ModuleInfo>, active: u32, focused: bool, zoomed: bool }
    TileDetail { #[serde(flatten)] tile: Tile, minimum: Extent }
    TileOpened { tile: TileId, tab: u32 }
    TileFocused { focus: TileId }
    TileRef { tile: TileId }
    Swapped { a: TileId, b: TileId }
    SplitDone { new_tile: TileId }
    Resized { rect: Rect }
    Zoomed { zoomed: Option<TileId> }
    Tab { tile: TileId, index: u32, module: Module, title: String, active: bool, dirty: bool }
    TabActive { active: u32 }
    TabStepped { tile: TileId, active: u32 }
    Reopened { tab: Tab }
    Layout { tree: Node, readout: Vec<String> }
    Applied { applied: bool, undo: Option<String>, diff: Vec<String> }
    FileOpened { tile: TileId, tab: u32, path: String, line: u32, col: u32, size: u64, dirty: bool }
    FileSaved { path: String, bytes: u64, hash: String }
    PathReply { path: String }
    OpenFile { path: String, tile: TileId, dirty: bool, line: u32, col: u32, language: String }
    Selection { path: String, ranges: Vec<Range>, text: String }
    Ranges { ranges: Vec<Range> }
    Edited { rev: u64, undo: String }
    FindHit { path: String, line: u32, col: u32, text: String }
    Focus { workspace: String, tile: Option<TileId>, tab: Option<u32>, kind: FocusKind }
    FocusSet { focus: FocusKind }
    PaletteEntry { id: String, title: String, source: String, args_schema: Value }
    StatusControl { control: String, label: String, tooltip: String }
    StatusOpened { opened: String }
    Notification { id: String, title: String, body: String, source: String, at: String }
    Theme { theme: String, mode: Mode }
    Setting { key: String, value: Value, source: String }
    Clipboard { text: String }
    Captured { path: String, w: u32, h: u32, bytes: u64 }
    Project { folder: String, name: String, session: Option<String>, branch: Option<String> }
    Folder { folder: String }
    Plugin { id: String, name: String, version: String, api: String, state: PluginState, commands: u32, agent_commands: u32 }
    PluginDetail {
        #[serde(flatten)] plugin: Plugin,
        manifest: Value,
        grant: Value,
        capabilities: Vec<String>,
        activity: Vec<Activity>,
        frame_ms: f64,
        input_ms: f64,
        scan: Option<Scan>,
    }
    ScanImport { module: String, name: String, capability: Option<String>, declared: bool }
    KeyLike { offset: u64, prefix: String, entropy: f64 }
    Scan {
        imports: Vec<ScanImport>,
        hosts: Vec<String>,
        urls: Vec<String>,
        key_like: Vec<KeyLike>,
        size: u64,
        producer: Option<String>,
        exports: Vec<String>,
    }
    PluginStateReply { state: PluginState }
    Reloaded { state: PluginState, load_ms: u64 }
    Installed { id: String, consent_needed: bool }
    Uninstalled { removed: bool, kept_data: bool }
    Granted { capability: String, mode: Access }
    Activity { at: String, call: String, args_hash: String, bytes: u64, code: Option<ErrorCode>, ms: u64 }
    PluginDump { state: Value, schema: Value }
    CommandPage { commands: Vec<Command>, next: Option<String> }
    Task { id: String, state: TaskState, progress: u64, total: Option<u64>, message: Option<String> }
    Undone { undone: bool, label: String }
    UndoEntry { token: String, label: String, at: String, source: String }
    Snapshot { value: Value, revision: u64 }
    StateEntry { uri: String, revision: u64, bytes: u64 }
    Subscribed { uri: String, revision: u64 }
    Event { cursor: String, #[serde(rename = "type")] kind: String, source: String, at: String, data: Value }
    EventPage { events: Vec<Event>, resync: bool }
    AuditEntry { caller: String, command: String, args_hash: String, code: Option<ErrorCode>, ms: u64 }
    Verdict { verdict: String, reason: String }
    Check { name: String, passed: bool, detail: String }
    ConformanceReport { plugin: String, checks: Vec<Check> }
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct Command {
    pub name: String,
    pub title: String,
    pub description: String,
    pub input_schema: Value,
    pub output_schema: Value,
    pub annotations: Value,
    pub agent: Access,
    pub needs_screen: bool,
    pub risk: Vec<String>,
    pub reversible: bool,
    pub progress: bool,
    pub cancellable: bool,
    pub timeout_ms: u64,
    pub emits: Vec<String>,
    pub since: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
pub struct UndoHandle {
    pub token: String,
    pub label: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct CommandResult {
    pub content: Vec<Value>,
    pub structured_content: Option<Value>,
    pub is_error: bool,
    pub changed: Vec<String>,
    pub undo: Option<UndoHandle>,
    pub revision: u64,
}
