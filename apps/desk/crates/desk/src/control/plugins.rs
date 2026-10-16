use std::path::PathBuf;
use std::time::Instant;

use desk_control::params::{FocusTarget, PluginRef, PluginRun, TabActivate, TabPick, TileOpen};
use desk_control::replies::{CommandResult, PluginCommand};
use desk_control::{Call, ErrorCode, shapes};
use desk_plugin::{Grants, Home, Manifest, NO_ARGS};
use desk_tiling::Module;
use gpui::{App, Entity};
use serde_json::{Value, json};

use super::change::{self, Answered};
use super::dispatch::count;
use crate::plugins::{self, Plugin, consent};
use crate::screens::work::Work;

type Refused = (ErrorCode, String);

pub fn answer(call: &Call, work: Option<&Entity<Work>>, cx: &mut App) -> Option<Answered> {
    match call {
        Call::PluginCommands(PluginRef { id }) => Some(commands(id).and_then(|c| change::json(&c))),
        Call::PluginRun(run) => Some(run_command(run, work, cx)),
        _ => None,
    }
}

fn declared(id: &str) -> Result<(&'static Plugin, Manifest), Refused> {
    let plugin = plugins::find(id)
        .ok_or_else(|| (ErrorCode::NotFound, format!("no plugin is called {id}")))?;
    if let Some(state) = plugin.state().filter(|state| *state != "running") {
        return Err((ErrorCode::Refused, format!("plugin {id} is {state}")));
    }
    let manifest = consent::manifest_of(plugin).map_err(|error| (ErrorCode::Refused, error))?;
    Ok((plugin, manifest))
}

fn commands(id: &str) -> Result<Vec<PluginCommand>, Refused> {
    let (_, manifest) = declared(id)?;
    manifest
        .contributes
        .commands
        .into_iter()
        .map(|command| {
            let args_schema = serde_json::from_str(&command.args)
                .map_err(|error| (ErrorCode::Refused, format!("{}: {error}", command.name)))?;
            Ok(PluginCommand {
                id: command.name,
                title: command.title,
                args_schema,
            })
        })
        .collect()
}

fn gated(manifest: &Manifest, command: &str, dry_run: bool) -> Result<Option<Value>, Refused> {
    let id = &manifest.id;
    let declared = manifest
        .contributes
        .commands
        .iter()
        .find(|c| c.name == command)
        .ok_or_else(|| {
            (
                ErrorCode::UnknownCommand,
                format!("plugin {id} has no command {command}"),
            )
        })?;
    let grants = Home::find()
        .and_then(|home| Grants::load(&home))
        .map_err(|error| (ErrorCode::Refused, error.to_string()))?;
    let grant = grants.of(id, declared);
    let refused = grant
        .admit(id, command)
        .err()
        .map(|error| error.to_string());
    eprintln!("desk: plugin {id} {command} grant {}", grant.name());
    match (dry_run, refused) {
        (true, refused) => Ok(Some(json!({
            "plugin": id.as_str(),
            "command": command,
            "title": declared.title,
            "grant": grant.name(),
            "grants": grants.path(),
            "runs": refused.is_none(),
            "refused": refused,
            "dry_run": true,
        }))),
        (false, Some(refused)) => Err((ErrorCode::Refused, refused)),
        (false, None) => Ok(None),
    }
}

fn run_command(run: &PluginRun, work: Option<&Entity<Work>>, cx: &mut App) -> Answered {
    let started = Instant::now();
    let PluginRun {
        id,
        command,
        dry_run,
        ..
    } = run;
    let (plugin, manifest) = declared(id)?;
    if let Some(dry) = gated(&manifest, command, *dry_run)? {
        return ran(id, command, started, Ok(dry));
    }
    let answered = match (plugin.built_in(), id.as_str(), command.as_str()) {
        (Some(_), "tofu.notepad", "notepad.new") => {
            let shown = show(id, work, cx)?;
            #[cfg(feature = "plugin-notepad")]
            crate::modules::notepad::untitled(cx);
            shown
        }
        (Some(_), _, _) => {
            return Err((
                ErrorCode::UnknownCommand,
                format!("plugin {id} names {command}, which the desk does not serve"),
            ));
        }
        (None, _, _) => {
            return Err((
                ErrorCode::Refused,
                format!("plugin {id} is not running, so its commands cannot run"),
            ));
        }
    };
    ran(id, command, started, Ok(answered))
}

pub struct Running {
    guest: &'static desk_plugin::Plugin,
    dir: PathBuf,
    run: PluginRun,
    started: Instant,
}

pub fn guest_run(call: &Call) -> Option<Result<Running, Refused>> {
    let Call::PluginRun(run) = call else {
        return None;
    };
    let plugin = plugins::find(&run.id)?;
    let guest = plugin.guest()?;
    Some(
        plugin
            .folder()
            .map(|dir| Running {
                guest,
                dir,
                run: run.clone(),
                started: Instant::now(),
            })
            .map_err(|error| (ErrorCode::Refused, error)),
    )
}

impl Running {
    pub fn wait(self) -> Answered {
        let Running {
            guest,
            dir,
            run,
            started,
        } = self;
        let manifest = consent::manifest_at(&dir).map_err(|error| (ErrorCode::Refused, error))?;
        if let Some(dry) = gated(&manifest, &run.command, run.dry_run)? {
            return ran(&run.id, &run.command, started, Ok(dry));
        }
        let args = run
            .args
            .as_ref()
            .map_or_else(|| NO_ARGS.to_owned(), Value::to_string);
        let answered = match guest.run(run.command.clone(), args).wait() {
            Ok(Ok(text)) => Ok(serde_json::from_str(&text).unwrap_or(Value::String(text))),
            Ok(Err(refused)) => Err(refused),
            Err(error @ desk_plugin::Error::Deadline { .. }) => {
                return Err((ErrorCode::Timeout, error.to_string()));
            }
            Err(error) => return Err((ErrorCode::PluginTrapped, error.to_string())),
        };
        ran(&run.id, &run.command, started, answered)
    }
}

fn ran(id: &str, command: &str, started: Instant, answered: Result<Value, String>) -> Answered {
    let ms = started.elapsed().as_millis();
    eprintln!("desk: plugin {id} ran {command} in {ms} ms");
    let (value, is_error) = match answered {
        Ok(value) => (value, false),
        Err(refused) => (Value::String(refused), true),
    };
    change::json(&CommandResult {
        content: vec![json!({ "type": "text", "text": value.to_string() })],
        structured_content: Some(value),
        is_error,
        changed: Vec::new(),
        undo: None,
        revision: 0,
    })
}

fn show(id: &str, work: Option<&Entity<Work>>, cx: &mut App) -> Answered {
    let work = work.ok_or((
        ErrorCode::NotFound,
        "the work screen is not open".to_owned(),
    ))?;
    let wanted = Module::Plugin(id.to_owned());
    let found = {
        let board = work.read(cx).board();
        board.workspaces().get(board.active()).and_then(|held| {
            held.tiles(board.area()).into_iter().find_map(|(stack, _)| {
                let at = stack.modules.iter().position(|module| *module == wanted)?;
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
            plugin: Some(id.to_owned()),
            target: None,
            args: None,
        })],
    };
    let mut last = json!(null);
    for call in calls {
        if let Some(answer) = change::answer(&call, Some(work), cx) {
            last = answer?;
        }
    }
    let tile = found.map_or_else(|| last["tile"].clone(), |(tile, _)| json!(tile));
    Ok(json!({ "plugin": id, "tile": tile, "opened": found.is_none() }))
}
