use desk_control::params::{TabClose, TabScope, TabStep};
use desk_control::replies::{Closed, Reopened, Tab, TabStepped};
use desk_control::shapes::Step;
use desk_control::{Call, ErrorCode};
use desk_tiling::{Rect, Refusal, Stack, TileId, Workspace};
use gpui::{AnyWindowHandle, App, Entity};

use super::change::{Answered, json};
use super::dispatch::{count, module};
use super::editor;
use crate::desk::Desk;
use crate::screens::work::Work;

pub fn answer(
    call: &Call,
    window: AnyWindowHandle,
    work: Option<&Entity<Work>>,
    cx: &mut App,
) -> Answered {
    let on_editor = window
        .read::<Desk, _, _>(cx, |desk, cx| desk.read(cx).screen() == "editor")
        .map_err(|error| {
            (
                ErrorCode::DeskNotRunning,
                format!("the desk window is gone: {error:#}"),
            )
        })?;
    if on_editor {
        return editor::answer(call, window, cx);
    }
    let work = work.ok_or((
        ErrorCode::NotFound,
        "the work screen is not open".to_owned(),
    ))?;
    match call {
        Call::TabList(TabScope { tile }) => {
            let held = active(work, cx)?;
            let listed: Vec<Tab> = match tile {
                Some(id) => tabs(
                    held.stack(TileId(*id))
                        .ok_or_else(|| refused(Refusal::NoSuchTile))?,
                ),
                None => held
                    .tiles(work.read(cx).board().area())
                    .into_iter()
                    .flat_map(|(stack, _)| tabs(stack))
                    .collect(),
            };
            json(&listed)
        }
        Call::TabStep(TabStep { step }) => {
            apply(
                work,
                false,
                |held, _| held.step_tab(matches!(step, Step::Next)),
                cx,
            )?;
            let stack = focused(work, cx)?;
            json(&TabStepped {
                tile: stack.id.0,
                active: count(stack.active),
            })
        }
        Call::TabClose(TabClose { tile, index }) => {
            apply(
                work,
                true,
                |held, area| {
                    if held.locked {
                        return Err(Refusal::Locked);
                    }
                    let id = tile
                        .map(TileId)
                        .or(held.focus())
                        .ok_or(Refusal::NoSuchTile)?;
                    let stack = held.stack(id).ok_or(Refusal::NoSuchTile)?;
                    let at = match index {
                        Some(at) => usize::try_from(*at).map_err(|_| Refusal::NoSuchTile)?,
                        None => stack.active,
                    };
                    match stack.modules.is_empty() {
                        true => held.close_tile(id, area),
                        false => held.close_module(id, at, area),
                    }
                },
                cx,
            )?;
            json(&Closed {
                closed: true,
                undo: None,
            })
        }
        Call::TabReopen(_) => {
            apply(work, true, |held, area| held.reopen(area), cx)?;
            let stack = focused(work, cx)?;
            let tab = tabs(stack)
                .into_iter()
                .nth(stack.active)
                .ok_or_else(|| refused(Refusal::NoSuchTile))?;
            json(&Reopened { tab })
        }
        _ => Err((
            ErrorCode::Refused,
            "tabs answer tab.list, tab.step, tab.close and tab.reopen only".to_owned(),
        )),
    }
}

fn tabs(stack: &Stack) -> Vec<Tab> {
    stack
        .modules
        .iter()
        .enumerate()
        .map(|(index, held)| {
            let shown = module(held);
            Tab {
                tile: stack.id.0,
                index: count(index),
                module: shown.kind,
                title: shown.name,
                active: index == stack.active,
                dirty: false,
            }
        })
        .collect()
}

fn apply(
    work: &Entity<Work>,
    structural: bool,
    step: impl FnOnce(&Workspace, Rect) -> Result<Workspace, Refusal>,
    cx: &mut App,
) -> Result<(), (ErrorCode, String)> {
    work.update(cx, |work, cx| {
        let board = work.board_mut();
        board.apply(structural, step);
        cx.notify();
        board
            .refusal()
            .map_or(Ok(()), |refusal| Err(refused(refusal)))
    })
}

fn active<'a>(work: &Entity<Work>, cx: &'a App) -> Result<&'a Workspace, (ErrorCode, String)> {
    let board = work.read(cx).board();
    board.workspaces().get(board.active()).ok_or((
        ErrorCode::NotFound,
        "the work screen has no open workspace".to_owned(),
    ))
}

fn focused<'a>(work: &Entity<Work>, cx: &'a App) -> Result<&'a Stack, (ErrorCode, String)> {
    let held = active(work, cx)?;
    held.focus()
        .and_then(|tile| held.stack(tile))
        .ok_or_else(|| refused(Refusal::NoSuchTile))
}

fn refused(refusal: Refusal) -> (ErrorCode, String) {
    let kind = match refusal {
        Refusal::TooMany => ErrorCode::TooMany,
        Refusal::TooSmall => ErrorCode::TooSmall,
        Refusal::Locked => ErrorCode::Locked,
        Refusal::NoSuchTile => ErrorCode::NoSuchTile,
        Refusal::NoHistory => ErrorCode::NoHistory,
        Refusal::NothingClosed => ErrorCode::NothingClosed,
    };
    (kind, refusal.to_string())
}
