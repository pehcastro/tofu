use std::path::Path;
use std::sync::OnceLock;
use std::time::Instant;

use desk_plugin::{Cache, Error, Home, Host};
use desk_ui::components::glyph::Glyph;

use super::{Kind, Plugin, glyph};

pub enum Load {
    Running(desk_plugin::Plugin),
    Refused(String),
    Disabled(String),
}

pub struct Guest {
    pub load: Load,
    trapped: OnceLock<String>,
}

impl Guest {
    pub fn running(&self) -> Option<&desk_plugin::Plugin> {
        match &self.load {
            Load::Running(plugin) if self.trapped.get().is_none() => Some(plugin),
            Load::Running(_) | Load::Refused(_) | Load::Disabled(_) => None,
        }
    }

    pub fn stopped(&self) -> Option<(&'static str, &str)> {
        match (&self.load, self.trapped.get()) {
            (Load::Refused(reason), _) => Some(("refused", reason)),
            (Load::Disabled(reason), _) => Some(("disabled", reason)),
            (Load::Running(_), Some(reason)) => Some(("trapped", reason)),
            (Load::Running(_), None) => None,
        }
    }

    pub fn state(&self) -> &'static str {
        self.stopped().map_or("running", |(state, _)| state)
    }

    pub fn trap(&self, id: &str, reason: String) {
        let kept = self.trapped.get_or_init(|| reason);
        eprintln!("desk: plugin {id} trapped: {kept}");
    }
}

pub fn discover(project: Option<&Path>) -> Vec<Plugin> {
    let host = match Home::find().and_then(|home| Host::new(&home)) {
        Ok(host) => host,
        Err(error) => {
            eprintln!("desk: plugins: no WASM host, so none load: {error}");
            return Vec::new();
        }
    };
    let found = host.discover(project);
    let mut entries: Vec<Plugin> = found
        .plugins
        .iter()
        .map(|installed| {
            let manifest = &installed.manifest;
            let id = manifest.id.as_str();
            let started = Instant::now();
            let load = match host.load(installed) {
                Ok((plugin, cache)) => {
                    let how = match cache {
                        Cache::Hit => "cache hit",
                        Cache::Miss => "cache miss",
                    };
                    eprintln!(
                        "desk: plugin {id} loaded in {:.1} ms ({how})",
                        started.elapsed().as_secs_f64() * 1000.0
                    );
                    Load::Running(plugin)
                }
                Err(error @ Error::Disabled { .. }) => {
                    eprintln!("desk: plugin {id} disabled: {error}");
                    Load::Disabled(error.to_string())
                }
                Err(error) => {
                    eprintln!("desk: plugin {id} refused: {error}");
                    Load::Refused(error.to_string())
                }
            };
            let icon = glyph(&manifest.icon).unwrap_or(Glyph::Window);
            entry(id.to_owned(), manifest.name.clone(), icon, load)
        })
        .collect();
    entries.extend(found.refused.into_iter().map(|error| {
        let folder = folder(&error);
        eprintln!("desk: plugin {folder} refused: {error}");
        let load = Load::Refused(error.to_string());
        entry(folder.clone(), folder, Glyph::Window, load)
    }));
    entries
}

fn entry(id: String, name: String, glyph: Glyph, load: Load) -> Plugin {
    Plugin {
        id,
        name,
        glyph,
        kind: Kind::Wasm(Guest {
            load,
            trapped: OnceLock::new(),
        }),
    }
}

fn folder(error: &Error) -> String {
    let path = match error {
        Error::Folder { folder, .. } => return folder.clone(),
        Error::At { path, .. } | Error::Io { path, .. } => path.parent(),
        _ => None,
    };
    path.and_then(Path::file_name).map_or_else(
        || "unknown".to_owned(),
        |name| name.to_string_lossy().into_owned(),
    )
}
