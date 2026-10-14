use desk_control::params::{ScreenList, ScreenOpen, WorkspaceScope};
use desk_control::replies::{
    Focus, Layout, ModuleInfo, Screen, ScreenCurrent, ScreenOpened, Tile, Workspace as Listed,
};
use desk_control::shapes::{self, FocusKind};
use desk_control::transport::Reply;
use desk_control::{Call, ErrorCode, Failure, Method, Request};
use desk_tiling::{Axis, Module, Node, Rect, Size, Workspace, readout, solve};
use gpui::{AnyWindowHandle, App};
use serde_json::Value;

use super::change::{self, Answered, json};
use super::{capture, editor};
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
    cx: &mut App,
) -> Reply {
    let Request { id, call } = request;
    let verb = verb(&call);
    let fail = |kind, message: String| {
        Failure::new(Some(id.clone()), kind, message, Some(verb.to_owned()))
    };
    let json = |value: serde_json::Result<Value>| {
        value.map_err(|error| fail(ErrorCode::Refused, error.to_string()))
    };
    let gone = |error: String| {
        fail(
            ErrorCode::DeskNotRunning,
            format!("the desk window is gone: {error}"),
        )
    };
    let (modal, work) = window
        .read::<Desk, _, _>(cx, |desk, cx| {
            let desk = desk.read(cx);
            (desk.modal(), desk.work().cloned())
        })
        .map_err(|error| gone(format!("{error:#}")))?;
    if modal {
        return Err(fail(
            ErrorCode::Busy,
            "a dialog is open in the desk".to_owned(),
        ));
    }
    if let Some(changed) = change::answer(&call, work.as_ref(), cx) {
        return changed.map_err(|(kind, message)| fail(kind, message));
    }
    let routed = match &call {
        Call::ScreenOpen(open) => Some(screen_open(open, window, screens, cx)),
        Call::FileOpen(_) | Call::EditorSelectionGet(_) => Some(editor::answer(&call, window, cx)),
        Call::Capture(capture) => Some(capture::answer(capture, window, cx)),
        _ => None,
    };
    if let Some(routed) = routed {
        return routed.map_err(|(kind, message)| fail(kind, message));
    }
    window
        .read::<Desk, _, _>(cx, |desk, cx| {
            let desk = desk.read(cx);
            let board = || {
                let board = desk.work()?.read(cx).board();
                Some((board.workspaces(), board.active(), board.area()))
            };
            let scoped = |workspace: Option<String>| {
                board()
                    .and_then(|(workspaces, active, area)| {
                        let at = match &workspace {
                            None => Some(active),
                            Some(wanted) => workspaces
                                .iter()
                                .position(|held| held.name == *wanted)
                                .or_else(|| wanted.parse().ok()),
                        };
                        Some((workspaces.get(at?)?, area))
                    })
                    .ok_or_else(|| {
                        fail(
                            ErrorCode::NotFound,
                            format!(
                                "the work screen has no workspace {}",
                                workspace.as_deref().unwrap_or("open")
                            ),
                        )
                    })
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
                Call::TileList(WorkspaceScope { workspace }) => scoped(workspace)
                    .and_then(|(held, area)| json(serde_json::to_value(tiles(held, area)))),
                Call::LayoutGet(WorkspaceScope { workspace }) => {
                    scoped(workspace).and_then(|(held, _)| {
                        json(serde_json::to_value(Layout {
                            tree: tree(held.tree()),
                            readout: readout(held.tree()),
                        }))
                    })
                }
                Call::FocusGet(_) => {
                    let held = board().and_then(|(workspaces, active, _)| workspaces.get(active));
                    let tile = held.and_then(Workspace::focus);
                    json(serde_json::to_value(Focus {
                        workspace: held.map(|held| held.name.clone()).unwrap_or_default(),
                        tile: tile.map(|tile| tile.0),
                        tab: held
                            .zip(tile)
                            .and_then(|(held, tile)| held.stack(tile))
                            .map(|stack| count(stack.active)),
                        kind: FocusKind::Tile,
                    }))
                }
                _ => Err(fail(
                    ErrorCode::Refused,
                    format!("{verb} is not built in this desk yet"),
                )),
            }
        })
        .unwrap_or_else(|error| Err(gone(format!("{error:#}"))))
}

fn screen_open(
    open: &ScreenOpen,
    window: AnyWindowHandle,
    screens: &[(&'static str, bool)],
    cx: &mut App,
) -> Answered {
    let ScreenOpen {
        name,
        workspace,
        board,
        file,
        dir,
        repo,
    } = open;
    if workspace.is_some() || board.is_some() || file.is_some() || dir.is_some() || repo.is_some() {
        return Err((
            ErrorCode::BadParams,
            "screen.open takes only a name in this desk; --workspace, --board, --file, --dir and --repo are not built yet".to_owned(),
        ));
    }
    match screens.iter().find(|(known, _)| known == name) {
        None => Err((ErrorCode::NotFound, format!("no screen is called {name}"))),
        Some((_, false)) => Err((
            ErrorCode::Refused,
            format!("screen {name} is not in this build"),
        )),
        Some(_) => {
            let gone = |error: String| {
                (
                    ErrorCode::DeskNotRunning,
                    format!("the desk window is gone: {error}"),
                )
            };
            let desk = window
                .downcast::<Desk>()
                .ok_or_else(|| gone("its root view is not the desk".to_owned()))?;
            let workspace = desk
                .update(cx, |desk, window, cx| {
                    desk.open_screen(name, window, cx)?;
                    Ok(desk
                        .work()
                        .and_then(|work| {
                            let board = work.read(cx).board();
                            board.workspaces().get(board.active())
                        })
                        .map(|held| held.name.clone())
                        .unwrap_or_default())
                })
                .map_err(|error| gone(format!("{error:#}")))?
                .map_err(|error| (ErrorCode::Refused, error))?;
            json(&ScreenOpened {
                screen: name.clone(),
                workspace,
                tile: None,
            })
        }
    }
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

pub(super) fn count(tiles: usize) -> u32 {
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

pub(super) fn tree(held: Option<&Node>) -> shapes::Node {
    held.map_or_else(
        || shapes::Node::Split {
            axis: shapes::Axis::Row,
            parts: Vec::new(),
        },
        node,
    )
}

fn node(held: &Node) -> shapes::Node {
    match held {
        Node::Tile(stack) => shapes::Node::Tile {
            id: stack.id.0,
            modules: stack.modules.iter().map(|held| module(held).kind).collect(),
            active: count(stack.active),
        },
        Node::Split { axis, parts } => shapes::Node::Split {
            axis: match axis {
                Axis::Row => shapes::Axis::Row,
                Axis::Column => shapes::Axis::Column,
            },
            parts: parts
                .iter()
                .map(|part| shapes::Part {
                    size: match part.size {
                        Size::Share(share) => shapes::Size::Share(share),
                        Size::Fixed(px) => shapes::Size::Fixed(px),
                    },
                    node: node(&part.node),
                })
                .collect(),
        },
    }
}

pub(super) fn module(module: &Module) -> ModuleInfo {
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
