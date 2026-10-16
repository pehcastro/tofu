use desk_control::params::{
    LayoutApply, SWITCH_FORMS, Switch, TileRef, TileZoom, WorkspaceNew, WorkspaceRef,
    WorkspaceRename, WorkspaceSwitch,
};
use desk_control::replies::{Active, Closed, Created, Renamed, Tree, Zoomed};
use desk_control::shapes;
use desk_control::{Call, ErrorCode};
use desk_tiling::{Preset, Rect, Refusal, TileId, Workspace};
use desk_ui::components::tiling_board::TilingBoard;
use gpui::{App, Entity};
use serde_json::Value;

use super::change::{Answered, json};
use super::dispatch::tree;
use crate::screens::work::Work;

pub enum Step {
    Created(usize),
    Renamed(usize, String),
    Switched(usize),
    Closed,
    Tree(usize),
    Zoomed(usize),
}

type Board = TilingBoard<Work>;

pub fn answer(
    call: &Call,
    work: Option<&Entity<Work>>,
    journal: &mut Vec<Step>,
    cx: &mut App,
) -> Option<Answered> {
    let mine = matches!(
        call,
        Call::WorkspaceNew(_)
            | Call::WorkspaceRename(_)
            | Call::WorkspaceSwitch(_)
            | Call::WorkspaceClose(_)
            | Call::TileClose(_)
            | Call::TileZoom(_)
    ) || matches!(call, Call::LayoutUndo(_)) && !journal.is_empty();
    if !mine {
        return None;
    }
    let Some(work) = work else {
        return Some(Err((
            ErrorCode::NotFound,
            "the work screen is not open".to_owned(),
        )));
    };
    Some(work.update(cx, |work, cx| {
        let answered = change(call, work.board_mut(), journal);
        cx.notify();
        answered
    }))
}

pub fn record(
    call: &Call,
    answered: &Answered,
    work: Option<&Entity<Work>>,
    journal: &mut Vec<Step>,
    cx: &App,
) {
    let structural = match call {
        Call::TileOpen(_) => true,
        Call::LayoutApply(LayoutApply { dry_run, .. }) => !dry_run,
        _ => false,
    };
    if let (true, Ok(_), Some(work)) = (structural, answered, work) {
        journal.push(Step::Tree(work.read(cx).board().active()));
    }
}

fn change(call: &Call, board: &mut Board, journal: &mut Vec<Step>) -> Answered {
    match call {
        Call::WorkspaceNew(WorkspaceNew { name, preset }) => {
            board.add();
            let index = board.active();
            let name = name.clone().unwrap_or_else(|| board.active_name());
            let preset = preset.as_ref().map_or(Preset::Empty, tiled_preset);
            board.apply(true, |_, _| Ok(Workspace::new(name, preset)));
            journal.push(Step::Created(index));
            json(&Created {
                id: index.to_string(),
            })
        }
        Call::WorkspaceRename(WorkspaceRename { id, name }) => {
            let index = find(board, id)?;
            let name = name.trim();
            if name.is_empty() {
                return Err((
                    ErrorCode::BadParams,
                    "a workspace name cannot be empty".to_owned(),
                ));
            }
            let old = named(board, index, name);
            journal.push(Step::Renamed(index, old));
            json(&Renamed {
                id: index.to_string(),
                name: name.to_owned(),
            })
        }
        Call::WorkspaceSwitch(WorkspaceSwitch { to }) => {
            let count = board.workspaces().len();
            let from = board.active();
            let at_index = |at: &str| {
                at.parse::<usize>()
                    .ok()
                    .filter(|at| *at < count)
                    .ok_or_else(|| missing(at))
            };
            let index = match to {
                Switch::Id(id) | Switch::Name(id) => find(board, id)?,
                Switch::Index(at) => at_index(&at.to_string())?,
                Switch::Bare(Value::Number(at)) => at_index(&at.to_string())?,
                Switch::Bare(Value::String(at)) if at.bytes().all(|b| b.is_ascii_digit()) => {
                    at_index(at)?
                }
                Switch::Bare(Value::String(name)) => board
                    .workspaces()
                    .iter()
                    .position(|held| held.name == *name)
                    .ok_or_else(|| missing(name))?,
                Switch::Bare(_) => return Err((ErrorCode::BadParams, SWITCH_FORMS.to_owned())),
                Switch::Next => (from + 1) % count,
                Switch::Prev => (from + count - 1) % count,
            };
            if index != from {
                board.switch(index);
                journal.push(Step::Switched(from));
            }
            json(&Active {
                active: index.to_string(),
            })
        }
        Call::WorkspaceClose(WorkspaceRef { id }) => {
            let index = find(board, id)?;
            let held = board.workspaces().get(index).ok_or_else(|| missing(id))?;
            if board.workspaces().len() == 1 {
                return Err((
                    ErrorCode::Refused,
                    format!(
                        "workspace {} is the last one and cannot be closed",
                        held.name
                    ),
                ));
            }
            if held.locked || held.pinned {
                return Err((
                    ErrorCode::Locked,
                    format!(
                        "workspace {} is locked or pinned; unlock it first",
                        held.name
                    ),
                ));
            }
            board.close_workspace(index);
            journal.push(Step::Closed);
            json(&Closed {
                closed: true,
                undo: None,
            })
        }
        Call::TileClose(TileRef { id }) => {
            let tile = TileId(*id);
            tiled(board, journal, |held, area| held.close_tile(tile, area))?;
            json(&Closed {
                closed: true,
                undo: None,
            })
        }
        Call::TileZoom(TileZoom { id, off }) => {
            let zoomed = board
                .workspaces()
                .get(board.active())
                .and_then(Workspace::zoomed);
            if zoomed.is_some() || !off {
                let index = board.active();
                attempt(board, |held, _| match (held.zoomed(), id) {
                    (None, Some(tile)) => held.focus_tile(TileId(*tile))?.zoom(),
                    (Some(_), _) | (None, None) => held.zoom(),
                })?;
                journal.push(Step::Zoomed(index));
            }
            json(&Zoomed {
                zoomed: board
                    .workspaces()
                    .get(board.active())
                    .and_then(Workspace::zoomed)
                    .map(|tile| tile.0),
            })
        }
        Call::LayoutUndo(_) => {
            if let Some(step) = journal.pop() {
                undo(board, step)?;
            }
            json(&Tree {
                tree: tree(
                    board
                        .workspaces()
                        .get(board.active())
                        .and_then(Workspace::tree),
                ),
            })
        }
        _ => Err((ErrorCode::Refused, "not a workspace verb".to_owned())),
    }
}

fn undo(board: &mut Board, step: Step) -> Result<(), (ErrorCode, String)> {
    match step {
        Step::Created(index) => board.close_workspace(index),
        Step::Renamed(index, name) => {
            named(board, index, &name);
        }
        Step::Switched(from) => board.switch(from),
        Step::Closed => {
            board.reopen();
        }
        Step::Tree(index) => {
            board.switch(index);
            attempt(board, |held, _| held.undo())?;
        }
        Step::Zoomed(index) => {
            board.switch(index);
            attempt(board, |held, _| held.zoom())?;
        }
    }
    Ok(())
}

fn named(board: &mut Board, index: usize, name: &str) -> String {
    let back = board.active();
    board.switch(index);
    let old = board.active_name();
    board.apply(false, |held, _| {
        let mut next = held.clone();
        name.clone_into(&mut next.name);
        Ok(next)
    });
    board.switch(back);
    old
}

fn tiled(
    board: &mut Board,
    journal: &mut Vec<Step>,
    step: impl FnOnce(&Workspace, Rect) -> Result<Workspace, Refusal>,
) -> Result<(), (ErrorCode, String)> {
    let index = board.active();
    attempt(board, step)?;
    journal.push(Step::Tree(index));
    Ok(())
}

fn attempt(
    board: &mut Board,
    step: impl FnOnce(&Workspace, Rect) -> Result<Workspace, Refusal>,
) -> Result<(), (ErrorCode, String)> {
    board.apply(true, step);
    board.refusal().map_or(Ok(()), |refusal| {
        let kind = match refusal {
            Refusal::TooMany => ErrorCode::TooMany,
            Refusal::TooSmall => ErrorCode::TooSmall,
            Refusal::Locked => ErrorCode::Locked,
            Refusal::NoSuchTile => ErrorCode::NoSuchTile,
            Refusal::NoHistory => ErrorCode::NoHistory,
            Refusal::NothingClosed => ErrorCode::NothingClosed,
        };
        Err((kind, refusal.to_string()))
    })
}

fn find(board: &Board, id: &str) -> Result<usize, (ErrorCode, String)> {
    let workspaces = board.workspaces();
    workspaces
        .iter()
        .position(|held| held.name == id)
        .or_else(|| id.parse().ok().filter(|at| *at < workspaces.len()))
        .ok_or_else(|| missing(id))
}

fn missing(id: &str) -> (ErrorCode, String) {
    (
        ErrorCode::NotFound,
        format!("the work screen has no workspace {id}"),
    )
}

fn tiled_preset(shape: &shapes::Preset) -> Preset {
    match shape {
        shapes::Preset::Work => Preset::Work,
        shapes::Preset::Editor => Preset::Editor,
        shapes::Preset::Data => Preset::Data,
        shapes::Preset::Empty => Preset::Empty,
    }
}
