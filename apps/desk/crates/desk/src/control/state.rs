use desk_control::events::{Feed, Uri};
use desk_control::params::{Empty, FocusTarget};
use desk_control::replies::Snapshot;
use desk_control::shapes::EventKind;
use desk_control::{Call, ErrorCode};
use desk_tiling::{TileId, solve};
use gpui::{AnyWindowHandle, App, Entity};
use serde_json::Value;

use super::change::{Answered, json};
use super::dispatch::{listed, tiles};
use super::editor;
use crate::desk::Desk;
use crate::screens::work::Work;

pub struct Seen {
    index: usize,
    count: usize,
    name: String,
    tiles: Vec<u32>,
    focus: Option<u32>,
}

pub enum Touch {
    Nothing,
    File,
    Draw,
    Focus,
}

pub fn touched(call: &Call) -> Touch {
    match call {
        Call::FileSave(_) => Touch::File,
        Call::TileFocus(_) | Call::FocusSet(FocusTarget::Tile(_)) => Touch::Focus,
        Call::CommandRun(run)
            if matches!(
                run.name.as_str(),
                "draw.create_board"
                    | "draw.diagram"
                    | "draw.add_shapes"
                    | "draw.connect"
                    | "draw.update"
                    | "draw.delete"
                    | "draw.import_excalidraw"
            ) =>
        {
            Touch::Draw
        }
        _ => Touch::Nothing,
    }
}

pub fn seen(window: AnyWindowHandle, cx: &App) -> Option<Seen> {
    window
        .read::<Desk, _, _>(cx, |desk, cx| {
            let board = desk.read(cx).work()?.read(cx).board();
            let held = board.workspaces().get(board.active())?;
            Some(Seen {
                index: board.active(),
                count: board.workspaces().len(),
                name: held.name.clone(),
                tiles: held.tree().map_or_else(Vec::new, |tree| {
                    solve(tree, board.area())
                        .into_iter()
                        .map(|(stack, _)| stack.id.0)
                        .collect()
                }),
                focus: held.focus().map(|tile| tile.0),
            })
        })
        .ok()
        .flatten()
}

pub fn emit(feed: &Feed, touch: &Touch, before: Option<Seen>, after: Option<Seen>, reply: &Value) {
    let mut changed: Vec<(Uri, EventKind)> = match touch {
        Touch::File => reply["path"]
            .as_str()
            .map(|path| (Uri::Editor(path.to_owned()), EventKind::FileSaved))
            .into_iter()
            .collect(),
        Touch::Draw => reply["structuredContent"]["board"]
            .as_str()
            .map(|board| (Uri::Draw(board.to_owned()), EventKind::DrawSaved))
            .into_iter()
            .collect(),
        Touch::Focus | Touch::Nothing => Vec::new(),
    };
    match (touch, before, after) {
        (Touch::Focus, _, Some(after)) => changed.extend(
            after
                .focus
                .map(|id| (tile(after.index, id), EventKind::TileFocused)),
        ),
        (_, Some(before), Some(after)) => changed.extend(moved(&before, &after)),
        _ => {}
    }
    for (uri, kind) in changed {
        let event = feed.emit(&uri, kind);
        eprintln!("desk: event {} cursor {}", kind.name(), event.cursor);
    }
}

fn moved(before: &Seen, after: &Seen) -> Vec<(Uri, EventKind)> {
    let same_slot = before.index == after.index && before.count == after.count;
    let same_workspace = before.name == after.name;
    if !same_slot && !same_workspace {
        return vec![(Uri::Workspace(after.index), EventKind::WorkspaceSwitched)];
    }
    let opened = after
        .tiles
        .iter()
        .filter(|tile| !before.tiles.contains(tile));
    let closed = before
        .tiles
        .iter()
        .filter(|tile| !after.tiles.contains(tile));
    let mut changed: Vec<_> = opened
        .map(|id| (tile(after.index, *id), EventKind::TileOpened))
        .chain(closed.map(|id| (tile(after.index, *id), EventKind::TileClosed)))
        .collect();
    if changed.is_empty()
        && before.focus != after.focus
        && let Some(focus) = after.focus
    {
        changed.push((tile(after.index, focus), EventKind::TileFocused));
    }
    changed
}

fn tile(workspace: usize, id: u32) -> Uri {
    Uri::Tile {
        workspace: Some(workspace),
        id,
    }
}

pub fn get(
    uri: &str,
    window: AnyWindowHandle,
    work: Option<&Entity<Work>>,
    feed: &Feed,
    cx: &mut App,
) -> Answered {
    let board = work.map(|work| work.read(cx).board());
    let parsed = match uri.parse().map_err(|error| (ErrorCode::NotFound, error))? {
        Uri::Tile {
            workspace: None,
            id,
        } => Uri::Tile {
            workspace: board.and_then(|board| {
                let workspaces = board.workspaces();
                std::iter::once(board.active())
                    .chain(0..workspaces.len())
                    .find(|at| {
                        workspaces
                            .get(*at)
                            .is_some_and(|held| held.stack(TileId(id)).is_some())
                    })
            }),
            id,
        },
        exact => exact,
    };
    let value = match &parsed {
        Uri::Workspace(at) => board
            .and_then(|board| {
                listed(board.workspaces(), board.active(), board.area())
                    .into_iter()
                    .nth(*at)
            })
            .map(serde_json::to_value)
            .transpose(),
        Uri::Tile { workspace, id } => board
            .and_then(|board| {
                let held = board
                    .workspaces()
                    .get(workspace.unwrap_or(board.active()))?;
                tiles(held, board.area())
                    .into_iter()
                    .find(|tile| tile.id == *id)
            })
            .map(serde_json::to_value)
            .transpose(),
        Uri::Editor(path) => {
            let open = editor::answer(&Call::FileList(Empty {}), window, cx)?;
            Ok(open
                .as_array()
                .and_then(|open| open.iter().find(|file| file["path"] == path.as_str()))
                .cloned())
        }
        Uri::Draw(board) => Ok(drawn(board, work, cx)?),
    }
    .map_err(|error| (ErrorCode::Refused, error.to_string()))?
    .ok_or_else(|| {
        (
            ErrorCode::NotFound,
            format!("{uri} names nothing open in the desk"),
        )
    })?;
    json(&Snapshot {
        value,
        revision: feed.revision(&parsed),
    })
}

#[cfg(feature = "plugin-draw")]
fn drawn(
    board: &str,
    work: Option<&Entity<Work>>,
    cx: &mut App,
) -> Result<Option<Value>, (ErrorCode, String)> {
    let read = desk_control::params::CommandRun {
        name: "draw.read".to_owned(),
        args: Some(serde_json::Map::from_iter([(
            "board".to_owned(),
            Value::String(board.to_owned()),
        )])),
        dry_run: false,
        wait: false,
        idempotency_key: None,
        timeout_ms: None,
    };
    let result = super::draw::answer(&read, work, cx)?;
    Ok(result.get("structuredContent").cloned())
}

#[cfg(not(feature = "plugin-draw"))]
fn drawn(
    _: &str,
    _: Option<&Entity<Work>>,
    _: &mut App,
) -> Result<Option<Value>, (ErrorCode, String)> {
    Err((
        ErrorCode::Refused,
        "this desk was built without the draw plugin: rebuild with --features plugin-draw"
            .to_owned(),
    ))
}
