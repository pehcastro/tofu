use desk_control::params::{ScreenList, WorkspaceScope};
use desk_control::replies::{ModuleInfo, Screen, ScreenCurrent, Tile, Workspace as Listed};
use desk_control::shapes;
use desk_control::transport::Reply;
use desk_control::{Call, ErrorCode, Failure, Method, Request};
use desk_tiling::{Module, Rect, Workspace, solve};
use gpui::{AnyWindowHandle, App};
use serde_json::Value;

use crate::desk::{Desk, ScreenGroup, screen_mark};
use crate::plugins;

pub fn verb(call: &Call) -> &'static str {
    let shown = format!("{call:?}");
    let variant = shown.split('(').next().unwrap_or_default();
    Method::ALL
        .iter()
        .find(|method| format!("{method:?}") == variant)
        .map_or("unknown", |method| method.name())
}

pub fn answer(
    request: Request,
    window: AnyWindowHandle,
    screens: &[(&'static str, bool)],
    cx: &App,
) -> Reply {
    let Request { id, call } = request;
    let verb = verb(&call);
    let fail = |kind, message: String| {
        Failure::new(Some(id.clone()), kind, message, Some(verb.to_owned()))
    };
    let json = |value: serde_json::Result<Value>| {
        value.map_err(|error| fail(ErrorCode::Refused, error.to_string()))
    };
    window
        .read::<Desk, _, _>(cx, |desk, cx| {
            let desk = desk.read(cx);
            if desk.modal() {
                return Err(fail(
                    ErrorCode::Busy,
                    "a dialog is open in the desk".to_owned(),
                ));
            }
            let board = || {
                let board = desk.work()?.read(cx).board();
                Some((board.workspaces(), board.active(), board.area()))
            };
            match call {
                Call::ScreenList(ScreenList { built }) => {
                    let open = desk.open_screens();
                    json(serde_json::to_value(
                        screens
                            .iter()
                            .filter(|(_, present)| *present || !built)
                            .map(|(name, present)| screen(name, *present, open.contains(name)))
                            .collect::<Vec<_>>(),
                    ))
                }
                Call::ScreenCurrent(_) => json(serde_json::to_value(ScreenCurrent {
                    screen: desk.screen().to_owned(),
                    workspace: board()
                        .and_then(|(workspaces, active, _)| workspaces.get(active))
                        .map(|workspace| workspace.name.clone())
                        .unwrap_or_default(),
                })),
                Call::WorkspaceList(_) => json(serde_json::to_value(
                    board().map_or_else(Vec::new, |(workspaces, active, area)| {
                        listed(workspaces, active, area)
                    }),
                )),
                Call::TileList(WorkspaceScope { workspace }) => {
                    let found = board().and_then(|(workspaces, active, area)| {
                        let at = match &workspace {
                            None => Some(active),
                            Some(wanted) => workspaces
                                .iter()
                                .position(|held| held.name == *wanted)
                                .or_else(|| wanted.parse().ok()),
                        };
                        Some(tiles(workspaces.get(at?)?, area))
                    });
                    match found {
                        Some(tiles) => json(serde_json::to_value(tiles)),
                        None => Err(fail(
                            ErrorCode::NotFound,
                            format!(
                                "the work screen has no workspace {}",
                                workspace.as_deref().unwrap_or("open")
                            ),
                        )),
                    }
                }
                _ => Err(fail(
                    ErrorCode::Refused,
                    format!("{verb} is not built in this desk yet"),
                )),
            }
        })
        .unwrap_or_else(|error| {
            Err(fail(
                ErrorCode::DeskNotRunning,
                format!("the desk window is gone: {error:#}"),
            ))
        })
}

fn screen(name: &str, built: bool, open: bool) -> Screen {
    let mark = screen_mark(name);
    Screen {
        name: name.to_owned(),
        label: mark.map_or_else(|| capitalized(name), |mark| mark.label.to_owned()),
        group: match mark.map(|mark| mark.group) {
            Some(ScreenGroup::Insights) => shapes::ScreenGroup::Insights,
            Some(ScreenGroup::Top) | None => shapes::ScreenGroup::Top,
        },
        glyph: mark.map_or_else(|| name.to_owned(), |mark| format!("{:?}", mark.glyph)),
        built,
        open,
    }
}

fn capitalized(name: &str) -> String {
    let mut chars = name.chars();
    chars
        .next()
        .map(|first| first.to_uppercase().chain(chars).collect())
        .unwrap_or_default()
}

fn count(tiles: usize) -> u32 {
    u32::try_from(tiles).unwrap_or(u32::MAX)
}

fn listed(workspaces: &[Workspace], active: usize, area: Rect) -> Vec<Listed> {
    workspaces
        .iter()
        .enumerate()
        .map(|(at, workspace)| Listed {
            id: at.to_string(),
            name: workspace.name.clone(),
            preset: workspace.preset.name().to_owned(),
            locked: workspace.locked,
            pinned: workspace.pinned,
            icon: workspace.icon.clone(),
            tiles: count(workspace.tree().map_or(0, |tree| solve(tree, area).len())),
            focus: workspace.focus().map(|tile| tile.0),
            zoomed: workspace.zoomed().map(|tile| tile.0),
            active: at == active,
        })
        .collect()
}

fn tiles(workspace: &Workspace, area: Rect) -> Vec<Tile> {
    workspace
        .tiles(area)
        .into_iter()
        .map(|(stack, rect)| Tile {
            id: stack.id.0,
            rect: shapes::Rect {
                x: rect.x,
                y: rect.y,
                w: rect.w,
                h: rect.h,
            },
            modules: stack.modules.iter().map(module).collect(),
            active: count(stack.active),
            focused: workspace.focus() == Some(stack.id),
            zoomed: workspace.zoomed() == Some(stack.id),
        })
        .collect()
}

fn module(module: &Module) -> ModuleInfo {
    let (kind, plugin) = match module {
        Module::Chat => (shapes::Module::Chat, None),
        Module::SubAgents => (shapes::Module::SubAgents, None),
        Module::FileEdits => (shapes::Module::FileEdits, None),
        Module::Shells => (shapes::Module::Shells, None),
        Module::Editor => (shapes::Module::Editor, None),
        Module::Terminal => (shapes::Module::Terminal, None),
        Module::Browser => (shapes::Module::Browser, None),
        Module::SourceControl => (shapes::Module::SourceControl, None),
        Module::Plugin(id) => (shapes::Module::Plugin, Some(id.clone())),
    };
    let name = match plugin.as_deref().and_then(plugins::find) {
        Some(found) => found.name.clone(),
        None => module.name().to_owned(),
    };
    ModuleInfo { name, kind, plugin }
}
