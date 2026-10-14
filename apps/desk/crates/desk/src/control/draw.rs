use std::fs;
use std::path::Path;
use std::time::Instant;

use desk_control::params::{CommandRun, FocusTarget, TabActivate, TabPick, TileOpen};
use desk_control::replies::CommandResult;
use desk_control::{Call, ErrorCode, shapes};
use desk_tiling::Module;
use draw_doc::{Edge, Graph, Node, Scene, diagram, graph};
use gpui::{App, Entity};
use serde::Deserialize;
use serde::de::DeserializeOwned;
use serde_json::{Value, json};

use super::change::{self, Answered};
use super::dispatch::count;
use crate::modules::draw::{self as tile, PLUGIN};
use crate::screens::work::Work;

type Refused = (ErrorCode, String);

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Board {
    board: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Diagram {
    board: String,
    nodes: Vec<Node>,
    edges: Vec<Edge>,
}

pub fn answer(run: &CommandRun, work: Option<&Entity<Work>>, cx: &mut App) -> Answered {
    if run.dry_run {
        return Err(bad(format!("{} has no dry run", run.name)));
    }
    let args = Value::Object(run.args.clone().unwrap_or_default());
    let answered = match run.name.as_str() {
        "draw.list" => list(),
        "draw.read" => {
            let Board { board } = parsed(args)?;
            read(&tile::board(&board).map_err(bad)?).and_then(|scene| {
                serde_json::to_value(graph(&scene).map_err(|error| refused(error.to_string()))?)
                    .map_err(|error| refused(error.to_string()))
            })
        }
        "draw.open" => {
            let Board { board } = parsed(args)?;
            let path = tile::board(&board).map_err(bad)?;
            if !path.is_file() {
                return Err((
                    ErrorCode::NotFound,
                    format!("no board {board} at {}", path.display()),
                ));
            }
            focus(work, cx)?;
            tile::show(path.clone(), cx);
            Ok(json!({ "board": board, "path": path }))
        }
        "draw.diagram" => {
            let Diagram {
                board,
                nodes,
                edges,
            } = parsed(args)?;
            let started = Instant::now();
            let path = tile::board(&board).map_err(bad)?;
            let graph = Graph { nodes, edges };
            let text = diagram(&graph)
                .and_then(|scene| scene.write())
                .map_err(|error| bad(error.to_string()))?;
            focus(work, cx)?;
            tile::replace(path.clone(), &text, cx).map_err(refused)?;
            let (n, e) = (graph.nodes.len(), graph.edges.len());
            let ms = started.elapsed().as_millis();
            eprintln!("desk: draw diagram {n} nodes {e} edges in {ms} ms");
            Ok(json!({ "board": board, "path": path, "nodes": n, "edges": e, "ms": ms }))
        }
        other => Err((
            ErrorCode::UnknownCommand,
            format!("{other} is not a draw command: draw.list, draw.open, draw.read, draw.diagram"),
        )),
    }?;
    change::json(&CommandResult {
        content: vec![json!({ "type": "text", "text": answered.to_string() })],
        structured_content: Some(answered),
        is_error: false,
        changed: Vec::new(),
        undo: None,
        revision: 0,
    })
}

fn bad(message: String) -> Refused {
    (ErrorCode::BadParams, message)
}

fn refused(message: String) -> Refused {
    (ErrorCode::Refused, message)
}

fn parsed<T: DeserializeOwned>(args: Value) -> Result<T, Refused> {
    serde_json::from_value(args).map_err(|error| bad(error.to_string()))
}

fn read(path: &Path) -> Result<Scene, Refused> {
    let text = fs::read_to_string(path)
        .map_err(|error| (ErrorCode::NotFound, format!("{}: {error}", path.display())))?;
    Scene::read(&text).map_err(|error| refused(format!("{}: {error}", path.display())))
}

fn list() -> Answered {
    let boards = tile::boards().map_err(refused)?;
    Ok(Value::Array(
        boards
            .iter()
            .map(|path| {
                let board = path
                    .file_stem()
                    .map(|stem| stem.to_string_lossy().into_owned())
                    .unwrap_or_default();
                match read(path) {
                    Ok(scene) => json!({ "board": board, "elements": scene.elements().len() }),
                    Err((_, error)) => json!({ "board": board, "error": error }),
                }
            })
            .collect(),
    ))
}

fn focus(work: Option<&Entity<Work>>, cx: &mut App) -> Result<(), Refused> {
    let work = work.ok_or((
        ErrorCode::NotFound,
        "the work screen is not open".to_owned(),
    ))?;
    let draw = Module::Plugin(PLUGIN.to_owned());
    let found = {
        let board = work.read(cx).board();
        board.workspaces().get(board.active()).and_then(|held| {
            held.tiles(board.area()).into_iter().find_map(|(stack, _)| {
                let at = stack.modules.iter().position(|module| *module == draw)?;
                Some((stack.id.0, at))
            })
        })
    };
    let calls = match found {
        Some((tile, at)) => vec![
            Call::TabActivate(TabActivate {
                tile,
                tab: TabPick::Index(count(at)),
            }),
            Call::FocusSet(FocusTarget::Tile(tile)),
        ],
        None => vec![Call::TileOpen(TileOpen {
            module: shapes::Module::Plugin,
            plugin: Some(PLUGIN.to_owned()),
            target: None,
            args: None,
        })],
    };
    for call in calls {
        if let Some(answer) = change::answer(&call, Some(work), cx) {
            answer?;
        }
    }
    Ok(())
}
