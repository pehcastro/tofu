use std::cell::Cell;
use std::collections::{BTreeMap, HashMap, VecDeque};
use std::fs;
use std::io::ErrorKind;
use std::path::{Path, PathBuf};
use std::rc::Rc;
use std::sync::{LazyLock, Mutex, PoisonError};
use std::time::Instant;

use desk_plugin::{
    Capability, GRANT_FILE, GRANT_VERSION, Home, Host, MANIFEST_FILE, Manifest, Mode, Origin,
};
use desk_ui::components::card::caption;
use desk_ui::components::paint::ink;
use desk_ui::components::sheet::Sheet;
use desk_ui::components::size::{FONT_BODY, FONT_SMALL, T1, T2, T3};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{FontWeight, SharedString, canvas, div, prelude::*, px};
use serde::Deserialize;

use super::Plugin;

pub const ACTIVITY_KEPT: usize = 50;
const SHORT_HASH: usize = 12;
const LINE_GAP: f32 = 2.0;
const GROUP_GAP: f32 = 10.0;

pub type Modes = BTreeMap<Capability, Mode>;

#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub enum Group {
    Files,
    Network,
    Tofu,
    Desk,
}

impl Group {
    pub fn label(self) -> &'static str {
        match self {
            Group::Files => "Files",
            Group::Network => "Network",
            Group::Tofu => "Your tofu sessions",
            Group::Desk => "Clipboard and notifications",
        }
    }
}

pub struct Risk {
    pub capability: Capability,
    pub group: Group,
    pub said: String,
    pub risk: &'static str,
    pub acting: bool,
}

pub fn risks(manifest: &Manifest) -> Vec<Risk> {
    let mut risks: Vec<Risk> = manifest
        .capabilities
        .iter()
        .map(|(&capability, targets)| {
            let picked = targets.is_empty() || targets.iter().all(|target| target == "picked");
            let place = if picked {
                "files you pick".to_owned()
            } else {
                targets.join(", ")
            };
            let (group, said, risk, acting) = match capability {
                Capability::FsRead => (
                    Group::Files,
                    format!("Read {place}"),
                    "What it reads it can show, keep, or send wherever else it may reach.",
                    false,
                ),
                Capability::FsWrite => (
                    Group::Files,
                    format!("Create and change {place}"),
                    "It can overwrite or empty what those files hold.",
                    true,
                ),
                Capability::Net => (
                    Group::Network,
                    format!("Send and receive data with {}", targets.join(", ")),
                    "Anything this plugin can read can be sent there.",
                    true,
                ),
                Capability::Tofu => (
                    Group::Tofu,
                    "Read your sessions, turns and settings".to_owned(),
                    "Read only: it never starts a turn, runs a shell or changes a setting.",
                    false,
                ),
                Capability::Clipboard => (
                    Group::Desk,
                    "Read and write your clipboard".to_owned(),
                    "The clipboard can hold a password you copied.",
                    true,
                ),
                Capability::Notify => (
                    Group::Desk,
                    "Show notifications".to_owned(),
                    "It can interrupt you, never act for you.",
                    false,
                ),
            };
            Risk {
                capability,
                group,
                said,
                risk,
                acting,
            }
        })
        .collect();
    risks.sort_by_key(|risk| (risk.group, risk.capability));
    risks
}

pub fn agents(manifest: &Manifest) -> Vec<&desk_plugin::Command> {
    manifest
        .contributes
        .commands
        .iter()
        .filter(|command| command.agent)
        .collect()
}

pub fn mode_name(mode: Mode) -> &'static str {
    match mode {
        Mode::Allow => "allow",
        Mode::Ask => "ask",
        Mode::Deny => "deny",
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct GrantFile {
    version: u32,
    modes: BTreeMap<String, Mode>,
}

static GRANTS: LazyLock<Mutex<HashMap<String, Modes>>> = LazyLock::new(Mutex::default);
static ACTIVITY: LazyLock<Mutex<HashMap<String, VecDeque<Call>>>> = LazyLock::new(Mutex::default);

pub fn manifest_at(dir: &Path) -> Result<Manifest, String> {
    let path = dir.join(MANIFEST_FILE);
    let text = fs::read_to_string(&path).map_err(|error| format!("{}: {error}", path.display()))?;
    Manifest::parse(&text).map_err(|error| format!("{}: {error}", path.display()))
}

pub fn modes(id: &str, dir: &Path) -> Result<Modes, String> {
    let cached = GRANTS
        .lock()
        .unwrap_or_else(PoisonError::into_inner)
        .get(id)
        .cloned();
    if let Some(modes) = cached {
        return Ok(modes);
    }
    let path = dir.join(GRANT_FILE);
    let modes = match fs::read_to_string(&path) {
        Ok(text) => {
            let read = |error: String| format!("{}: {error}", path.display());
            let file: GrantFile = serde_json::from_str(&text).map_err(|e| read(e.to_string()))?;
            if file.version != GRANT_VERSION {
                return Err(read(format!(
                    "version {}, this desk writes {GRANT_VERSION}",
                    file.version
                )));
            }
            file.modes
                .iter()
                .map(|(name, &mode)| {
                    Capability::parse(name)
                        .map(|capability| (capability, mode))
                        .map_err(|e| read(e.to_string()))
                })
                .collect::<Result<Modes, String>>()?
        }
        Err(error) if error.kind() == ErrorKind::NotFound => Modes::new(),
        Err(error) => return Err(format!("{}: {error}", path.display())),
    };
    GRANTS
        .lock()
        .unwrap_or_else(PoisonError::into_inner)
        .insert(id.to_owned(), modes.clone());
    Ok(modes)
}

pub fn grant(id: &str, dir: &Path, modes: Modes) -> Result<(), String> {
    let named: BTreeMap<&str, &str> = modes
        .iter()
        .map(|(capability, &mode)| (capability.name(), mode_name(mode)))
        .collect();
    let text = serde_json::json!({ "version": GRANT_VERSION, "modes": named }).to_string();
    let path = dir.join(GRANT_FILE);
    let partial = path.with_extension("partial");
    fs::create_dir_all(dir)
        .and_then(|()| fs::write(&partial, &text))
        .and_then(|()| fs::rename(&partial, &path))
        .map_err(|error| format!("{}: {error}", path.display()))?;
    eprintln!(
        "desk: plugin {id} grant written to {}: {text}",
        path.display()
    );
    GRANTS
        .lock()
        .unwrap_or_else(PoisonError::into_inner)
        .insert(id.to_owned(), modes);
    Ok(())
}

#[derive(Clone)]
pub struct Call {
    pub at: SharedString,
    pub capability: Capability,
    pub what: String,
    pub allowed: bool,
}

pub fn check(id: &str, dir: &Path, capability: Capability, what: &str) -> Result<(), String> {
    let allowed = modes(id, dir)
        .map_err(|error| eprintln!("desk: plugin {id} has no readable grant: {error}"))
        .ok()
        .and_then(|modes| modes.get(&capability).copied())
        == Some(Mode::Allow);
    let call = Call {
        at: chrono::Local::now().format("%H:%M:%S").to_string().into(),
        capability,
        what: what.to_owned(),
        allowed,
    };
    {
        let mut log = ACTIVITY.lock().unwrap_or_else(PoisonError::into_inner);
        let calls = log.entry(id.to_owned()).or_default();
        if calls.len() == ACTIVITY_KEPT {
            calls.pop_front();
        }
        calls.push_back(call);
    }
    if allowed {
        return Ok(());
    }
    eprintln!("desk: plugin {id} denied {capability}");
    Err(format!(
        "{id} may not {what}: {capability} is off in Settings > Plugins"
    ))
}

pub fn activity(id: &str) -> Vec<Call> {
    ACTIVITY
        .lock()
        .unwrap_or_else(PoisonError::into_inner)
        .get(id)
        .map(|calls| calls.iter().rev().cloned().collect())
        .unwrap_or_default()
}

pub enum Provenance {
    BuiltIn,
    Installed { sha256: String },
}

pub struct Pending {
    pub manifest: Manifest,
    pub dir: PathBuf,
    pub provenance: Provenance,
    pub asked: Instant,
}

pub fn manifest_of(plugin: &Plugin) -> Result<Manifest, String> {
    match plugin.built_in() {
        Some(text) => Manifest::parse(text).map_err(|error| format!("{}: {error}", plugin.id)),
        None => manifest_at(&plugin.folder()?),
    }
}

pub fn install(folder: &Path) -> Result<Pending, String> {
    let manifest = manifest_at(folder)?;
    let home = Home::find().map_err(|error| error.to_string())?;
    let report = Host::new(&home)
        .and_then(|host| host.install(folder, Origin::Person))
        .map_err(|error| error.to_string())?;
    eprintln!(
        "desk: plugin {} {} installed to {}",
        report.id,
        report.version,
        report.dir.display()
    );
    Ok(Pending {
        manifest,
        dir: report.dir,
        provenance: Provenance::Installed {
            sha256: report.sha256,
        },
        asked: Instant::now(),
    })
}

pub fn enable(plugin: &Plugin) -> Result<Pending, String> {
    let manifest = manifest_of(plugin)?;
    eprintln!("desk: plugin {} enable asked", plugin.id);
    Ok(Pending {
        manifest,
        dir: plugin.folder()?,
        provenance: Provenance::BuiltIn,
        asked: Instant::now(),
    })
}

pub fn allow(pending: &Pending) -> Result<(), String> {
    let id = pending.manifest.id.as_str();
    let modes = pending
        .manifest
        .capabilities
        .keys()
        .map(|&capability| (capability, Mode::Allow))
        .collect();
    grant(id, &pending.dir, modes)?;
    eprintln!("desk: plugin {id} consent allow");
    Ok(())
}

pub fn cancel(pending: &Pending) -> Result<(), String> {
    let id = &pending.manifest.id;
    eprintln!("desk: plugin {id} consent cancel");
    if let Provenance::BuiltIn = pending.provenance {
        return Ok(());
    }
    GRANTS
        .lock()
        .unwrap_or_else(PoisonError::into_inner)
        .remove(id.as_str());
    Home::find()
        .and_then(|home| Host::new(&home))
        .and_then(|host| host.uninstall(id, true))
        .map_err(|error| error.to_string())?;
    eprintln!("desk: plugin {id} uninstalled, its notes and data kept");
    Ok(())
}

pub fn sheet(
    pending: &Pending,
    open: bool,
    painted: &Rc<Cell<Option<Instant>>>,
    theme: &Theme,
) -> Sheet {
    let manifest = &pending.manifest;
    let id = manifest.id.to_string();
    let (hash, after) = match &pending.provenance {
        Provenance::BuiltIn => ("built into the desk".to_owned(), "enable"),
        Provenance::Installed { sha256 } => (
            format!("sha256 {}", sha256.get(..SHORT_HASH).unwrap_or(sha256)),
            "install",
        ),
    };
    let facts = format!(
        "{} by {} · version {} · {hash}",
        manifest.id,
        manifest.id.publisher(),
        manifest.version
    );
    let risks = risks(manifest);
    let mut groups: Vec<(Group, Vec<&Risk>)> = Vec::new();
    for risk in &risks {
        match groups.last_mut() {
            Some((group, items)) if *group == risk.group => items.push(risk),
            _ => groups.push((risk.group, vec![risk])),
        }
    }
    let leaves = risks.iter().any(|risk| risk.capability == Capability::Net);
    let agents = agents(manifest);
    let line = |text: String, alpha: f32| {
        div()
            .text_size(px(FONT_SMALL))
            .text_color(ink(theme, alpha))
            .child(text)
    };
    let drawn = groups.into_iter().map(|(group, items)| {
        div()
            .flex()
            .flex_col()
            .gap(px(LINE_GAP))
            .child(caption(group.label(), theme))
            .children(items.into_iter().map(|risk| {
                let mark = if risk.acting { "Powerful" } else { "Normal" };
                div()
                    .flex()
                    .flex_col()
                    .child(
                        div()
                            .flex()
                            .gap_2()
                            .text_size(px(FONT_BODY))
                            .text_color(ink(theme, T1))
                            .child(risk.said.clone())
                            .child(
                                div()
                                    .text_size(px(FONT_SMALL))
                                    .text_color(if risk.acting {
                                        theme.color(ColorToken::StatusWarn)
                                    } else {
                                        ink(theme, T3)
                                    })
                                    .child(mark),
                            ),
                    )
                    .child(line(risk.risk.to_owned(), T2))
            }))
    });
    let timed = painted.get().is_some().then(|| {
        let (named, unpainted) = (id.clone(), Rc::clone(painted));
        canvas(
            |_, _, _| {},
            move |_, (), _, _| {
                if let Some(asked) = unpainted.take() {
                    eprintln!(
                        "desk: plugin {named} consent shown in {:.1} ms after {after}",
                        asked.elapsed().as_secs_f64() * 1000.0
                    );
                }
            },
        )
        .absolute()
        .size_0()
    });
    let nothing = risks
        .is_empty()
        .then(|| line("It asks for nothing outside itself.".to_owned(), T2));
    Sheet::new(
        SharedString::from(format!("consent-{id}")),
        format!("{} would like to", manifest.name),
    )
    .modal()
    .open(open)
    .confirm("Allow")
    .child(line(facts, T3))
    .child(
        div()
            .flex()
            .flex_col()
            .gap(px(GROUP_GAP))
            .py(px(GROUP_GAP))
            .child(
                div()
                    .text_size(px(FONT_BODY))
                    .font_weight(FontWeight::SEMIBOLD)
                    .child("Know your risks"),
            )
            .children(nothing)
            .children(drawn)
            .children(leaves.then(|| line("Data can leave this computer.".to_owned(), T2)))
            .children(
                (!agents.is_empty())
                    .then(|| line(format!("Agents can control {}.", manifest.name), T2)),
            )
            .child(line(
                "It cannot reach anything not listed here: the desk checks every call, and Settings > Plugins can turn each one off later.".to_owned(),
                T3,
            )),
    )
    .children(timed)
}
