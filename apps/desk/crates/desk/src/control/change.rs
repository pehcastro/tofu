use desk_control::params::{
    FocusTarget, LayoutApply, LayoutSource, TabActivate, TabPick, TileFocus, TileOpen,
};
use desk_control::replies::{Applied, FocusSet, TabActive, TileFocused, TileOpened, Tree};
use desk_control::shapes::{self, FocusKind};
use desk_control::{Call, ErrorCode};
use desk_tiling::{
    Axis, Module, Node, Part, Preset, Rect, Refusal, Side, Size, Stack, Target, TileId, Workspace,
    Zone, readout,
};
use gpui::{App, Entity};
use serde::Serialize;
use serde_json::Value;

use super::dispatch::{count, module, tree};
use crate::screens::work::Work;

pub type Answered = Result<Value, (ErrorCode, String)>;

pub fn answer(call: &Call, work: Option<&Entity<Work>>, cx: &mut App) -> Option<Answered> {
    let work = work.ok_or_else(|| {
        (
            ErrorCode::NotFound,
            "the work screen is not open".to_owned(),
        )
    });
    Some(match call {
        Call::TileOpen(open) => work.and_then(|work| tile_open(open, work, cx)),
        Call::TabActivate(pick) => work.and_then(|work| tab_activate(pick, work, cx)),
        Call::LayoutApply(layout) => work.and_then(|work| layout_apply(layout, work, cx)),
        Call::LayoutUndo(_) => work.and_then(|work| {
            apply(work, true, |held, _| held.undo(), cx)?;
            json(&Tree {
                tree: tree(active(work, cx).and_then(Workspace::tree)),
            })
        }),
        Call::TileFocus(TileFocus { id, toward }) => work.and_then(|work| {
            if id.is_some() == toward.is_some() {
                return Err((
                    ErrorCode::BadParams,
                    "tile.focus takes a tile id or --toward left|right|top|bottom, one of them"
                        .to_owned(),
                ));
            }
            apply(
                work,
                false,
                |held, area| match (id, toward) {
                    (Some(tile), _) => held.focus_tile(TileId(*tile)),
                    (None, Some(at)) => held.focus_toward(side(*at), area),
                    (None, None) => Err(Refusal::NoSuchTile),
                },
                cx,
            )?;
            let focus = active(work, cx)
                .and_then(Workspace::focus)
                .ok_or_else(|| refused(Refusal::NoSuchTile))?;
            json(&TileFocused { focus: focus.0 })
        }),
        Call::FocusSet(FocusTarget::Tile(tile)) => work.and_then(|work| {
            apply(work, false, |held, _| held.focus_tile(TileId(*tile)), cx)?;
            json(&FocusSet {
                focus: FocusKind::Tile,
            })
        }),
        Call::FocusSet(FocusTarget::Palette | FocusTarget::Status(_)) => Err((
            ErrorCode::Refused,
            "focus.set takes a tile in this desk; the palette and the status bar are not built yet"
                .to_owned(),
        )),
        _ => return None,
    })
}

fn tile_open(open: &TileOpen, work: &Entity<Work>, cx: &mut App) -> Answered {
    if open.args.is_some() {
        return Err((
            ErrorCode::BadParams,
            "tile.open takes no args yet".to_owned(),
        ));
    }
    let module = tiled_module(&open.module, open.plugin.as_deref())?;
    let target = open.target.as_ref().map(target).transpose()?;
    apply(
        work,
        true,
        |held, area| match target {
            Some(target) => held.open_at(module, target, area),
            None => held.open(module, area),
        },
        cx,
    )?;
    let held = active(work, cx);
    let tile = held
        .and_then(Workspace::focus)
        .ok_or_else(|| refused(Refusal::NoSuchTile))?;
    json(&TileOpened {
        tile: tile.0,
        tab: held
            .and_then(|held| held.stack(tile))
            .map_or(0, |stack| count(stack.active)),
    })
}

fn tab_activate(
    TabActivate { tile, tab }: &TabActivate,
    work: &Entity<Work>,
    cx: &mut App,
) -> Answered {
    let id = TileId(*tile);
    let stack = active(work, cx)
        .and_then(|held| held.stack(id))
        .ok_or_else(|| refused(Refusal::NoSuchTile))?;
    let index = match tab {
        TabPick::Index(at) => usize::try_from(*at)
            .ok()
            .filter(|at| *at < stack.modules.len()),
        TabPick::Name(name) => stack
            .modules
            .iter()
            .position(|held| held.name() == name.as_str() || module(held).name == *name),
    }
    .ok_or_else(|| {
        (
            ErrorCode::NotFound,
            format!("tile {tile} has no tab {tab:?}"),
        )
    })?;
    apply(work, false, |held, _| held.activate(id, index), cx)?;
    json(&TabActive {
        active: count(index),
    })
}

fn layout_apply(
    LayoutApply { layout, dry_run }: &LayoutApply,
    work: &Entity<Work>,
    cx: &mut App,
) -> Answered {
    match layout {
        LayoutSource::Tree(shape) => {
            let tree = tiled_tree(shape, &mut Vec::new())?;
            arrange(work, *dry_run, |held, area| held.arranged(tree, area), cx)
        }
        LayoutSource::Preset(shape) => {
            let preset = match shape {
                shapes::Preset::Work => Preset::Work,
                shapes::Preset::Editor => Preset::Editor,
                shapes::Preset::Data => Preset::Data,
                shapes::Preset::Empty => Preset::Empty,
            };
            arrange(
                work,
                *dry_run,
                |held, _| {
                    if held.locked {
                        return Err(Refusal::Locked);
                    }
                    let mut next = held.clone();
                    next.preset = preset;
                    Ok(next.reset())
                },
                cx,
            )
        }
    }
}

fn arrange(
    work: &Entity<Work>,
    dry_run: bool,
    step: impl FnOnce(&Workspace, Rect) -> Result<Workspace, Refusal>,
    cx: &mut App,
) -> Answered {
    let diff = if dry_run {
        let area = work.read(cx).board().area();
        let held = active(work, cx).ok_or_else(|| refused(Refusal::NoSuchTile))?;
        readout(step(held, area).map_err(refused)?.tree())
    } else {
        apply(work, true, step, cx)?;
        readout(active(work, cx).and_then(Workspace::tree))
    };
    json(&Applied {
        applied: !dry_run,
        undo: None,
        diff,
    })
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

fn active<'a>(work: &Entity<Work>, cx: &'a App) -> Option<&'a Workspace> {
    let board = work.read(cx).board();
    board.workspaces().get(board.active())
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

pub(super) fn json(reply: &impl Serialize) -> Answered {
    serde_json::to_value(reply).map_err(|error| (ErrorCode::Refused, error.to_string()))
}

fn tiled_module(
    shape: &shapes::Module,
    plugin: Option<&str>,
) -> Result<Module, (ErrorCode, String)> {
    Ok(match shape {
        shapes::Module::Chat => Module::Chat,
        shapes::Module::SubAgents => Module::SubAgents,
        shapes::Module::FileEdits => Module::FileEdits,
        shapes::Module::Shells => Module::Shells,
        shapes::Module::Editor => Module::Editor,
        shapes::Module::Terminal => Module::Terminal,
        shapes::Module::Browser => Module::Browser,
        shapes::Module::SourceControl => Module::SourceControl,
        shapes::Module::Plugin => Module::Plugin(
            plugin
                .ok_or_else(|| {
                    (
                        ErrorCode::BadParams,
                        "a plugin tile needs --plugin <id>".to_owned(),
                    )
                })?
                .to_owned(),
        ),
    })
}

fn side(shape: shapes::Side) -> Side {
    match shape {
        shapes::Side::Left => Side::Left,
        shapes::Side::Right => Side::Right,
        shapes::Side::Top => Side::Top,
        shapes::Side::Bottom => Side::Bottom,
    }
}

fn target(shape: &shapes::Target) -> Result<Target, (ErrorCode, String)> {
    Ok(match shape {
        shapes::Target::Edge(edge) => Target::Edge(side(*edge)),
        shapes::Target::Tile { tile, zone } => Target::Tile(
            TileId(*tile),
            match zone {
                shapes::Zone::Side(at) => Zone::Side(side(*at)),
                shapes::Zone::Stack { at } => Zone::Stack {
                    at: usize::try_from(*at)
                        .map_err(|error| (ErrorCode::BadParams, error.to_string()))?,
                },
            },
        ),
    })
}

fn tiled_tree(shape: &shapes::Node, seen: &mut Vec<u32>) -> Result<Node, (ErrorCode, String)> {
    let bad = |message: String| (ErrorCode::BadParams, message);
    match shape {
        shapes::Node::Tile {
            id,
            modules,
            active,
        } => {
            if seen.contains(id) {
                return Err((ErrorCode::Conflict, format!("tile {id} appears twice")));
            }
            seen.push(*id);
            let modules = modules
                .iter()
                .map(|held| tiled_module(held, None))
                .collect::<Result<Vec<_>, _>>()?;
            let active = usize::try_from(*active)
                .ok()
                .filter(|at| *at < modules.len())
                .ok_or_else(|| bad(format!("tile {id} has no tab {active}")))?;
            Ok(Node::Tile(Stack {
                id: TileId(*id),
                modules,
                active,
            }))
        }
        shapes::Node::Split { axis, parts } => {
            if parts.is_empty() {
                return Err(bad("a split needs at least one part".to_owned()));
            }
            let parts = parts
                .iter()
                .map(|part| {
                    let size = match part.size {
                        shapes::Size::Share(share) if share.is_finite() && share > 0.0 => {
                            Size::Share(share)
                        }
                        shapes::Size::Fixed(px) if px.is_finite() && px > 0.0 => Size::Fixed(px),
                        shapes::Size::Share(wrong) | shapes::Size::Fixed(wrong) => {
                            return Err(bad(format!("a part size must be above 0, not {wrong}")));
                        }
                    };
                    Ok(Part {
                        size,
                        node: tiled_tree(&part.node, seen)?,
                    })
                })
                .collect::<Result<Vec<_>, _>>()?;
            Ok(Node::Split {
                axis: match axis {
                    shapes::Axis::Row => Axis::Row,
                    shapes::Axis::Column => Axis::Column,
                },
                parts,
            })
        }
    }
}
