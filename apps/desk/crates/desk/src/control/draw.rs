use std::fs;
use std::path::{Path, PathBuf};
use std::time::Instant;

use desk_control::params::{
    CommandRun, DrawConnect, DrawDelete, DrawImport, DrawShapes, DrawUpdate, DrawView, FocusTarget,
    TabActivate, TabPick, TileOpen,
};
use desk_control::replies::CommandResult;
use desk_control::{Call, ErrorCode, shapes};
use desk_core::home::Home;
use desk_tiling::{Module, Rect};
use draw_doc::{
    Edge, Graph, Layout, Link, Node, Patch, Scene, Shape, add_shapes, connect, delete, diagram,
    graph, update,
};
use gpui::{App, Entity, px};
use serde::Deserialize;
use serde::de::DeserializeOwned;
use serde_json::{Map, Value, json};

use super::capture;
use super::change::{self, Answered};
use super::dispatch::count;
use crate::desk::Desk;
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
    #[serde(default)]
    nodes: Vec<Node>,
    #[serde(default)]
    edges: Vec<Edge>,
    layout: Option<Layout>,
    flow: Option<String>,
    #[serde(alias = "flow-file")]
    flow_file: Option<String>,
}

fn flow_text(flow: Option<String>, flow_file: Option<String>) -> Result<Option<String>, Refused> {
    match (flow, flow_file) {
        (None, None) => Ok(None),
        (Some(text), None) => Ok(Some(text)),
        (None, Some(file)) => fs::read_to_string(&file)
            .map(Some)
            .map_err(|error| (ErrorCode::NotFound, format!("{file}: {error}"))),
        (Some(_), Some(_)) => Err(bad(
            "give the flow as --flow text or --flow-file, not both".to_owned()
        )),
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Snapshot {
    board: String,
    max_px: Option<u32>,
    path: Option<PathBuf>,
}

const SNAPSHOT_MAX_PX: u32 = 512;
const SNAPSHOT_PAD_PX: f32 = 24.0;

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
                layout,
                flow,
                flow_file,
            } = parsed(args)?;
            let started = Instant::now();
            let path = tile::board(&board).map_err(bad)?;
            let (text, n, e, layout) = match flow_text(flow, flow_file)? {
                Some(_) if !nodes.is_empty() || !edges.is_empty() => {
                    return Err(bad(
                        "give either a flow or nodes and edges, not both".to_owned()
                    ));
                }
                Some(_) if layout.is_some_and(|layout| layout != Layout::Layered) => {
                    return Err(bad("a flow is always laid out layered".to_owned()));
                }
                Some(source) => {
                    let drawn = tile::flow_board(&source).map_err(bad)?;
                    (drawn.text, drawn.nodes, drawn.edges, Layout::Layered)
                }
                None => {
                    let layout = layout.unwrap_or_default();
                    let graph = Graph { nodes, edges };
                    let scene = diagram(&graph, layout).map_err(|error| bad(error.to_string()))?;
                    let text = scene.write().map_err(|error| bad(error.to_string()))?;
                    (text, graph.nodes.len(), graph.edges.len(), layout)
                }
            };
            eprintln!(
                "desk: draw layout {layout} {n} nodes in {} ms",
                started.elapsed().as_millis()
            );
            focus(work, cx)?;
            tile::replace(path.clone(), &text, cx).map_err(refused)?;
            let ms = started.elapsed().as_millis();
            eprintln!("desk: draw diagram {n} nodes {e} edges in {ms} ms");
            Ok(
                json!({ "board": board, "path": path, "nodes": n, "edges": e, "layout": layout, "ms": ms }),
            )
        }
        "draw.snapshot" => {
            let Snapshot {
                board,
                max_px,
                path,
            } = parsed(args)?;
            snapshot(&board, max_px.unwrap_or(SNAPSHOT_MAX_PX), path, work, cx)
        }
        "draw.create_board" => {
            let Board { board } = parsed(args)?;
            let path = tile::board(&board).map_err(bad)?;
            if path.exists() {
                return Err((ErrorCode::Conflict, format!("board {board} already exists")));
            }
            let text = Scene::new()
                .write()
                .map_err(|error| refused(error.to_string()))?;
            tile::store(&path, &text).map_err(refused)?;
            Ok(json!({ "board": board, "path": path }))
        }
        "draw.add_shapes" => {
            let DrawShapes {
                board,
                shapes,
                shapes_file,
            } = parsed(args)?;
            let shapes: Vec<Shape> = listed(shapes, shapes_file, "shapes")?;
            let started = Instant::now();
            let made = tile::edit(&existing(&board)?, |scene| add_shapes(scene, &shapes), cx)
                .map_err(refused)?;
            let ids: Map<String, Value> = made
                .into_iter()
                .map(|(key, id)| (key, Value::String(id)))
                .collect();
            let ms = started.elapsed().as_millis();
            eprintln!("desk: draw add_shapes {} shapes in {ms} ms", ids.len());
            Ok(json!({ "board": board, "ids": ids, "ms": ms }))
        }
        "draw.connect" => {
            let DrawConnect {
                board,
                edges,
                edges_file,
            } = parsed(args)?;
            let edges: Vec<Link> = listed(edges, edges_file, "edges")?;
            let ids = tile::edit(&existing(&board)?, |scene| connect(scene, &edges), cx)
                .map_err(refused)?;
            Ok(json!({ "board": board, "ids": ids }))
        }
        "draw.update" => {
            let DrawUpdate {
                board,
                patches,
                patches_file,
            } = parsed(args)?;
            let patches: Vec<Patch> = listed(patches, patches_file, "patches")?;
            let path = existing(&board)?;
            tile::edit(&path, |scene| Ok((update(scene, &patches)?, ())), cx).map_err(refused)?;
            Ok(json!({ "board": board, "updated": patches.len() }))
        }
        "draw.delete" => {
            let DrawDelete { board, keys } = parsed(args)?;
            let keys = key_list(keys)?;
            let path = existing(&board)?;
            tile::edit(&path, |scene| Ok((delete(scene, &keys)?, ())), cx).map_err(refused)?;
            Ok(json!({ "board": board, "deleted": keys }))
        }
        "draw.view" => {
            let DrawView { board, fit } = parsed(args)?;
            let keys = match fit.as_deref() {
                None | Some("all") => None,
                Some(keys) => Some(keys.split(',').map(str::to_owned).collect::<Vec<_>>()),
            };
            let (seen, zoom) =
                tile::fit(&existing(&board)?, keys.as_deref(), cx).map_err(refused)?;
            Ok(json!({
                "board": board,
                "camera": {
                    "x": seen.min_x,
                    "y": seen.min_y,
                    "w": seen.max_x - seen.min_x,
                    "h": seen.max_y - seen.min_y,
                    "zoom": zoom,
                },
            }))
        }
        "draw.import_excalidraw" => {
            let DrawImport { path, board } = parsed(args)?;
            import(&path, board)
        }
        other => Err((
            ErrorCode::UnknownCommand,
            format!(
                "{other} is not a draw command: draw.list, draw.create_board, draw.open, draw.read, draw.add_shapes, draw.connect, draw.update, draw.delete, draw.view, draw.diagram, draw.snapshot, draw.import_excalidraw"
            ),
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

fn existing(board: &str) -> Result<PathBuf, Refused> {
    let path = tile::board(board).map_err(bad)?;
    if !path.is_file() {
        return Err((
            ErrorCode::NotFound,
            format!("no board {board} at {}", path.display()),
        ));
    }
    Ok(path)
}

fn listed<T: DeserializeOwned>(
    inline: Option<Vec<Value>>,
    file: Option<String>,
    name: &str,
) -> Result<Vec<T>, Refused> {
    match (inline, file) {
        (Some(items), None) => parsed(Value::Array(items)),
        (None, Some(file)) => {
            let text = fs::read_to_string(&file)
                .map_err(|error| (ErrorCode::NotFound, format!("{file}: {error}")))?;
            serde_json::from_str(&text).map_err(|error| bad(format!("{file}: {error}")))
        }
        (Some(_), Some(_)) | (None, None) => Err(bad(format!(
            "give {name} as a JSON list or --{name}-file with a path to one, exactly one of them"
        ))),
    }
}

fn key_list(keys: Value) -> Result<Vec<String>, Refused> {
    match keys {
        Value::String(keys) => Ok(keys.split(',').map(str::to_owned).collect()),
        Value::Array(_) => parsed(keys),
        other => Err(bad(format!(
            "keys is a key, a comma list of keys, or a JSON list of keys, not {other}"
        ))),
    }
}

fn import(path: &str, board: Option<String>) -> Answered {
    let from = Path::new(path);
    let text = fs::read_to_string(from)
        .map_err(|error| (ErrorCode::NotFound, format!("{path}: {error}")))?;
    let scene = Scene::read(&text).map_err(|error| bad(format!("{path}: {error}")))?;
    let board = match board {
        Some(board) => board,
        None => from
            .file_stem()
            .map(|stem| stem.to_string_lossy().into_owned())
            .ok_or_else(|| bad(format!("{path} has no file name")))?,
    };
    let target = tile::board(&board).map_err(bad)?;
    if target.exists() {
        return Err((ErrorCode::Conflict, format!("board {board} already exists")));
    }
    tile::store(&target, &text).map_err(refused)?;
    Ok(json!({ "board": board, "path": target, "elements": scene.elements().len() }))
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

fn snapshot(
    board: &str,
    max_px: u32,
    path: Option<PathBuf>,
    work: Option<&Entity<Work>>,
    cx: &mut App,
) -> Answered {
    let started = Instant::now();
    if !tile::board(board).map_err(bad)?.is_file() {
        return Err((ErrorCode::NotFound, format!("no board {board}")));
    }
    let not_showing = || refused(format!("board {board} is not showing: run draw.open first"));
    let work = work.ok_or_else(not_showing)?;
    let draw = Module::Plugin(PLUGIN.to_owned());
    let tiling = work.read(cx).board();
    tiling
        .workspaces()
        .get(tiling.active())
        .and_then(|held| {
            held.tiles(tiling.area())
                .into_iter()
                .find(|(stack, _)| stack.modules.get(stack.active) == Some(&draw))
        })
        .ok_or_else(not_showing)?;
    let drawn = tile::drawn(px(SNAPSHOT_PAD_PX), cx)
        .ok_or_else(|| refused(format!("board {board} has nothing drawn in view")))?;
    let rect = Rect {
        x: f32::from(drawn.origin.x),
        y: f32::from(drawn.origin.y),
        w: f32::from(drawn.size.width),
        h: f32::from(drawn.size.height),
    };
    let path = match path {
        Some(path) => path,
        None => Home::find()
            .map_err(|error| refused(error.to_string()))?
            .cache
            .join("draw")
            .join(format!("{board}.png")),
    };
    if let Some(folder) = path.parent() {
        fs::create_dir_all(folder)
            .map_err(|error| refused(format!("{}: {error}", folder.display())))?;
    }
    let window = cx
        .windows()
        .into_iter()
        .find(|window| window.downcast::<Desk>().is_some())
        .ok_or((ErrorCode::DeskNotRunning, "no desk window".to_owned()))?;
    let (width, height) = capture::region(window, &path, rect, max_px, cx)?;
    let bytes = fs::metadata(&path)
        .map_err(|error| refused(format!("{}: {error}", path.display())))?
        .len();
    let ms = started.elapsed().as_millis();
    eprintln!("desk: draw snapshot {board} {width}x{height} in {ms} ms");
    Ok(json!({ "path": path, "width": width, "height": height, "bytes": bytes, "ms": ms }))
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
