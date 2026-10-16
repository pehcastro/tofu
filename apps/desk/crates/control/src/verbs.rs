use crate::error::ErrorCode::{self, *};
use crate::params as p;
use crate::replies as r;
use schemars::{JsonSchema, SchemaGenerator};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};

macro_rules! verbs {
    ($($variant:ident $name:literal $params:ty => $reply:ty, [$($err:ident),*];)*) => {
        #[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, JsonSchema)]
        pub enum Method {
            $(#[serde(rename = $name)] $variant,)*
        }

        #[derive(Debug, Clone)]
        pub enum Call {
            $($variant($params),)*
        }

        impl Method {
            pub const ALL: &[Method] = &[$(Method::$variant,)*];

            pub fn name(self) -> &'static str {
                match self {
                    $(Method::$variant => $name,)*
                }
            }

            pub fn errors(self) -> &'static [ErrorCode] {
                match self {
                    $(Method::$variant => &[$($err),*],)*
                }
            }

            pub fn call(self, params: Value) -> Result<Call, serde_json::Error> {
                match self {
                    $(Method::$variant => serde_json::from_value(params).map(Call::$variant),)*
                }
            }

            pub fn describe(self, generator: &mut SchemaGenerator) -> Value {
                let (params, result) = match self {
                    $(Method::$variant => (generator.subschema_for::<$params>(), generator.subschema_for::<$reply>()),)*
                };
                let errors: Vec<&str> = self.errors().iter().map(|code| code.name()).collect();
                json!({"params": params, "result": result, "errors": errors})
            }
        }
    };
}

impl Method {
    pub fn wire(self, params: Value) -> (&'static str, Value) {
        match self {
            Method::DrawList
            | Method::DrawOpen
            | Method::DrawRead
            | Method::DrawDiagram
            | Method::DrawCreateBoard
            | Method::DrawAddShapes
            | Method::DrawConnect
            | Method::DrawUpdate
            | Method::DrawDelete
            | Method::DrawView
            | Method::DrawImportExcalidraw => (
                Method::CommandRun.name(),
                json!({ "name": self.name(), "args": params }),
            ),
            _ => (self.name(), params),
        }
    }
}

verbs! {
    DeskInitialize "desk.initialize" p::Initialize => r::Initialized, [UnsupportedVersion, BadParams];
    DeskPing "desk.ping" p::Empty => r::Pong, [DeskNotRunning];
    DeskVersion "desk.version" p::Empty => r::Versions, [];
    DeskSchema "desk.schema" p::SchemaQuery => Value, [NotFound];
    DeskOpen "desk.open" p::DeskOpen => r::Opened, [Refused, Busy];
    DeskQuit "desk.quit" p::Quit => r::Ack, [NeedsApproval];
    DeskWindowGet "desk.window.get" p::Empty => r::Window, [];
    DeskWindowFocus "desk.window.focus" p::Empty => r::Ack, [NotFound];
    DeskWindowSize "desk.window.size" p::WindowSize => r::Sized, [BadParams];
    ScreenList "screen.list" p::ScreenList => Vec<r::Screen>, [];
    ScreenOpen "screen.open" p::ScreenOpen => r::ScreenOpened, [NotFound, BadParams, Refused];
    ScreenCurrent "screen.current" p::Empty => r::ScreenCurrent, [];
    WorkspaceList "workspace.list" p::Empty => Vec<r::Workspace>, [];
    WorkspaceNew "workspace.new" p::WorkspaceNew => r::Created, [OverLimit];
    WorkspaceRename "workspace.rename" p::WorkspaceRename => r::Renamed, [NotFound];
    WorkspaceSwitch "workspace.switch" p::WorkspaceSwitch => r::Active, [NotFound];
    WorkspaceClose "workspace.close" p::WorkspaceRef => r::Closed, [NotFound, NeedsApproval];
    WorkspaceReorder "workspace.reorder" p::WorkspaceReorder => r::Order, [BadParams];
    WorkspaceLock "workspace.lock" p::WorkspaceRef => r::Locked, [NotFound];
    WorkspaceUnlock "workspace.unlock" p::WorkspaceRef => r::Locked, [NotFound];
    WorkspacePin "workspace.pin" p::WorkspaceRef => r::Pinned, [NotFound];
    WorkspaceUnpin "workspace.unpin" p::WorkspaceRef => r::Pinned, [NotFound];
    WorkspaceIcon "workspace.icon" p::WorkspaceIcon => r::Icon, [BadParams];
    WorkspaceSendTile "workspace.send_tile" p::SendTile => r::Sent, [NoSuchTile, TooMany];
    WorkspaceReset "workspace.reset" p::WorkspaceRef => r::Tree, [NotFound];
    TileList "tile.list" p::WorkspaceScope => Vec<r::Tile>, [NotFound];
    TileGet "tile.get" p::TileRef => r::TileDetail, [NoSuchTile];
    TileOpen "tile.open" p::TileOpen => r::TileOpened, [TooMany, TooSmall, Locked, NotFound];
    TileClose "tile.close" p::TileRef => r::Closed, [NoSuchTile];
    TileFocus "tile.focus" p::TileFocus => r::TileFocused, [NoSuchTile];
    TileMove "tile.move" p::TileMove => r::TileRef, [NoSuchTile, TooSmall];
    TileSwap "tile.swap" p::TileSwap => r::Swapped, [NoSuchTile];
    TileSplit "tile.split" p::MaybeTile => r::SplitDone, [TooMany, TooSmall];
    TileResize "tile.resize" p::TileResize => r::Resized, [TooSmall];
    TileZoom "tile.zoom" p::TileZoom => r::Zoomed, [NoSuchTile];
    TileReload "tile.reload" p::TileRef => r::Ack, [PluginTrapped];
    TabList "tab.list" p::TabScope => Vec<r::Tab>, [NoSuchTile];
    TabActivate "tab.activate" p::TabActivate => r::TabActive, [NoSuchTile, NotFound];
    TabStep "tab.step" p::TabStep => r::TabStepped, [NoSuchTile];
    TabClose "tab.close" p::TabClose => r::Closed, [NeedsApproval];
    TabReopen "tab.reopen" p::Empty => r::Reopened, [NothingClosed];
    LayoutGet "layout.get" p::WorkspaceScope => r::Layout, [NotFound];
    LayoutApply "layout.apply" p::LayoutApply => r::Applied, [TooMany, TooSmall, Conflict];
    LayoutEven "layout.even" p::LayoutEven => r::Tree, [];
    LayoutUndo "layout.undo" p::Empty => r::Tree, [NoHistory];
    FileOpen "file.open" p::FileOpen => r::FileOpened, [NotFound, DeniedByConsent, BadParams];
    FileClose "file.close" p::FileClose => r::Closed, [NeedsApproval];
    FileSave "file.save" p::MaybePath => r::FileSaved, [Conflict, DeniedByGate];
    FileSaveAs "file.save_as" p::FileSaveAs => r::PathReply, [Conflict];
    FileList "file.list" p::Empty => Vec<r::ListedFile>, [];
    FileListOpen "file.list_open" p::Empty => Vec<r::OpenFile>, [];
    FileReveal "file.reveal" p::PathRef => r::TileRef, [NotFound];
    FileDiff "file.diff" p::FileDiff => r::TileRef, [NotFound];
    EditorSelectionGet "editor.selection.get" p::MaybePath => r::Selection, [NotFound];
    EditorSelectionSet "editor.selection.set" p::SelectionSet => r::Ranges, [BadParams];
    EditorTextReplace "editor.text.replace" p::TextReplace => r::Edited, [Conflict, DeniedByGate];
    EditorFind "editor.find" p::Find => Vec<r::FindHit>, [OverLimit];
    FocusGet "focus.get" p::Empty => r::Focus, [];
    FocusSet "focus.set" p::FocusTarget => r::FocusSet, [NoSuchTile];
    PaletteList "palette.list" p::PaletteList => Vec<r::PaletteEntry>, [];
    PaletteRun "palette.run" p::PaletteRun => Value, [UnknownCommand, DeniedByConsent];
    MenuInvoke "menu.invoke" p::PathRef => r::Ack, [NotFound];
    StatusbarGet "statusbar.get" p::Empty => Vec<r::StatusControl>, [];
    StatusbarClick "statusbar.click" p::StatusClick => r::StatusOpened, [NotFound];
    NotifyList "notify.list" p::Empty => Vec<r::Notification>, [];
    NotifyDismiss "notify.dismiss" p::MaybeId => r::Ack, [NotFound];
    NotifyShow "notify.show" p::NotifyShow => r::Created, [OverLimit];
    ThemeGet "theme.get" p::Empty => r::Theme, [];
    ThemeSet "theme.set" p::ThemeSet => r::Theme, [NotFound];
    SettingsDeskGet "settings.desk.get" p::SettingKey => r::Setting, [NotFound];
    SettingsDeskList "settings.desk.list" p::Empty => Vec<r::Setting>, [];
    SettingsDeskSet "settings.desk.set" p::SettingSet => r::Setting, [DeniedByConsent];
    ClipboardGet "clipboard.get" p::Empty => r::Clipboard, [DeniedByConsent];
    ClipboardSet "clipboard.set" p::ClipboardSet => r::Clipboard, [DeniedByConsent];
    Capture "capture" p::Capture => r::Captured, [NotFound, OverLimit];
    ProjectGet "project.get" p::Empty => r::Project, [];
    ProjectRecents "project.recents" p::Empty => Vec<String>, [];
    ProjectOpen "project.open" p::PathRef => r::Folder, [NotFound, NeedsApproval];
    PluginList "plugin.list" p::PluginList => Vec<r::Plugin>, [];
    PluginGet "plugin.get" p::PluginRef => r::PluginDetail, [NotFound];
    PluginEnable "plugin.enable" p::PluginRef => r::PluginStateReply, [NotFound];
    PluginDisable "plugin.disable" p::PluginRef => r::PluginStateReply, [NotFound];
    PluginReload "plugin.reload" p::PluginRef => r::Reloaded, [PluginTrapped];
    PluginInstall "plugin.install" p::PluginInstall => r::Installed, [Refused, Conflict];
    PluginUninstall "plugin.uninstall" p::PluginUninstall => r::Uninstalled, [NotFound];
    PluginGrant "plugin.grant" p::PluginGrant => r::Granted, [NotFound];
    PluginActivity "plugin.activity" p::PluginActivity => Vec<r::Activity>, [NotFound];
    PluginState "plugin.state" p::PluginStateQuery => r::PluginDump, [PluginUnavailable];
    CommandList "command.list" p::CommandList => r::CommandPage, [];
    CommandDescribe "command.describe" p::CommandName => r::Command, [UnknownCommand];
    CommandRun "command.run" p::CommandRun => r::CommandResult, [UnknownCommand, BadParams, DeniedByConsent, DeniedByGate, NeedsApproval, Busy, Timeout, PluginTrapped];
    DrawList "draw.list" p::Empty => r::CommandResult, [Refused];
    DrawOpen "draw.open" p::DrawBoard => r::CommandResult, [NotFound, BadParams];
    DrawRead "draw.read" p::DrawBoard => r::CommandResult, [NotFound, Refused];
    DrawDiagram "draw.diagram" p::DrawDiagram => r::CommandResult, [BadParams, NotFound, Refused];
    DrawCreateBoard "draw.create_board" p::DrawBoard => r::CommandResult, [BadParams, Conflict, Refused];
    DrawAddShapes "draw.add_shapes" p::DrawShapes => r::CommandResult, [BadParams, NotFound, Refused];
    DrawConnect "draw.connect" p::DrawConnect => r::CommandResult, [BadParams, NotFound, Refused];
    DrawUpdate "draw.update" p::DrawUpdate => r::CommandResult, [BadParams, NotFound, Refused];
    DrawDelete "draw.delete" p::DrawDelete => r::CommandResult, [BadParams, NotFound, Refused];
    DrawView "draw.view" p::DrawView => r::CommandResult, [BadParams, NotFound, Refused];
    DrawImportExcalidraw "draw.import_excalidraw" p::DrawImport => r::CommandResult, [BadParams, NotFound, Conflict, Refused];
    TaskList "task.list" p::Empty => Vec<r::Task>, [];
    TaskGet "task.get" p::IdRef => r::Task, [NotFound];
    TaskCancel "task.cancel" p::IdRef => r::Task, [NotFound];
    Undo "undo" p::UndoToken => r::Undone, [NotFound, Conflict];
    UndoList "undo.list" p::WorkspaceScope => Vec<r::UndoEntry>, [];
    StateGet "state.get" p::UriRef => r::Snapshot, [NotFound, OverLimit];
    StateList "state.list" p::StateList => Vec<r::StateEntry>, [];
    StateSubscribe "state.subscribe" p::UriRef => r::Subscribed, [NotFound];
    StateUnsubscribe "state.unsubscribe" p::UriRef => r::Ack, [NotFound];
    EventsSubscribe "events.subscribe" p::Empty => r::Following, [];
    EventsSince "events.since" p::EventsSince => r::EventPage, [];
    Audit "audit" p::Audit => Vec<r::AuditEntry>, [];
    GateJudge "gate.judge" p::GateJudge => r::Verdict, [Refused];
    Conformance "conformance" p::Conformance => r::ConformanceReport, [PluginUnavailable];
}
