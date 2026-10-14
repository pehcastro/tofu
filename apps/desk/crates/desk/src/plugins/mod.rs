use std::path::{Path, PathBuf};
use std::sync::LazyLock;

use desk_core::home::Home;
use desk_tiling::Module;
use desk_ui::components::glyph::Glyph;
use gpui::{AnyView, App};
use serde::Deserialize;

const API: &str = "desk@0.1";

pub type Mount = fn(&'static Plugin, &mut App) -> AnyView;

enum Kind {
    Native(Mount),
}

pub struct Plugin {
    pub id: String,
    pub name: String,
    pub glyph: Glyph,
    kind: Kind,
}

#[derive(Deserialize)]
struct Manifest {
    id: String,
    name: String,
    icon: String,
    version: String,
    api: String,
}

const NATIVE: &[(&str, Mount)] = &[
    #[cfg(feature = "plugin-notepad")]
    (
        include_str!("../../../../plugins/tofu.notepad/plugin.toml"),
        crate::modules::notepad::mount,
    ),
];

static FIRST_PARTY: LazyLock<Result<Vec<Plugin>, String>> = LazyLock::new(|| {
    NATIVE
        .iter()
        .map(|(manifest, mount)| parse(manifest, Kind::Native(*mount)))
        .collect()
});

pub fn check() -> Result<usize, String> {
    FIRST_PARTY.as_ref().map(Vec::len).map_err(Clone::clone)
}

pub fn all() -> &'static [Plugin] {
    FIRST_PARTY.as_deref().unwrap_or_default()
}

pub fn find(id: &str) -> Option<&'static Plugin> {
    all().iter().find(|plugin| plugin.id == id)
}

pub fn modules() -> impl Iterator<Item = Module> {
    all().iter().map(|plugin| Module::Plugin(plugin.id.clone()))
}

impl Plugin {
    pub fn mount(&'static self, cx: &mut App) -> AnyView {
        let view = match self.kind {
            Kind::Native(mount) => mount(self, cx),
        };
        eprintln!("desk: plugin {} mounted", self.id);
        view
    }

    pub fn folder(&self) -> Result<PathBuf, String> {
        let home = Home::find().map_err(|error| error.to_string())?;
        let tofu = home
            .config
            .parent()
            .and_then(Path::parent)
            .ok_or_else(|| format!("{} has no tofu folder above it", home.config.display()))?;
        Ok(tofu.join("plugins").join(&self.id))
    }
}

fn plain_id(id: &str) -> bool {
    let part = |part: &str| {
        part.bytes().next().is_some_and(|b| b.is_ascii_lowercase())
            && part
                .bytes()
                .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-')
    };
    id.split_once('.')
        .is_some_and(|(publisher, name)| part(publisher) && part(name))
}

fn glyph(icon: &str) -> Option<Glyph> {
    match icon {
        "pencil" => Some(Glyph::Pencil),
        "file" => Some(Glyph::File),
        other => Glyph::workspace(other),
    }
}

fn parse(text: &str, kind: Kind) -> Result<Plugin, String> {
    let manifest: Manifest =
        toml::from_str(text).map_err(|error| format!("a built-in plugin.toml: {error}"))?;
    let id = manifest.id;
    if !plain_id(&id) {
        return Err(format!("plugin id {id:?} is not publisher.name"));
    }
    if manifest.api != API {
        return Err(format!(
            "plugin {id} asks for api {}, the desk speaks {API}",
            manifest.api
        ));
    }
    let glyph = glyph(&manifest.icon).ok_or_else(|| {
        format!(
            "plugin {id} names icon {:?}, which the desk has not",
            manifest.icon
        )
    })?;
    eprintln!("desk: plugin {id} {} registered", manifest.version);
    Ok(Plugin {
        id,
        name: manifest.name,
        glyph,
        kind,
    })
}
