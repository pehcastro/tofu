mod folder;
mod grant;
mod install;
mod ladder;
mod manifest;
mod scan;

use std::collections::{BTreeMap, BTreeSet};
use std::fmt;
use std::fs;
use std::io;
use std::ops::Range;
use std::path::{Path, PathBuf};
use std::sync::mpsc::{self, Receiver, Sender, TryRecvError};
use std::thread;
use std::time::{Duration, Instant};

use sha2::{Digest, Sha256};
use wasmtime::component::types::ComponentItem;
use wasmtime::component::{Component, HasSelf, Linker};
use wasmtime::{Config, Engine, Store, StoreLimits, StoreLimitsBuilder, Trap};

pub use folder::{Entry, Folder, FsError, Op, Outcome, PathRefusal};
use grant::{COMMAND_GRANT_FILE, COMMAND_GRANT_VERSION};
pub use grant::{Grant, Grants, Withheld};
pub use install::{Home, Origin, Refusal, Report};
pub use ladder::since_v0_2_0::tofu::desk::types::{
    Badge, Choice, Editor, Event, Kind, Node, Table, Toggle, Tone,
};
pub use ladder::since_v0_3_0::exports::tofu::desk::commands::Command as Descriptor;
pub use manifest::{
    Api, Capability, Command, Contributes, GRANT_VERSION, Id, IdRule, MAX_ID_LEN, Manifest, Mode,
    NO_ARGS,
};
pub use scan::{Import, KEY_ENTROPY_BITS, KEY_PREFIXES, KeyLike, MIN_KEY_LEN, ScanReport, scan};

use ladder::{since_v0_1_0, since_v0_2_0, since_v0_3_0};

pub const TICK: Duration = Duration::from_millis(1);
pub const RENDER_TICKS: u64 = 50;
pub const COMMAND_TICKS: u64 = 5_000;
pub const MEMORY_BYTES: usize = 64 << 20;
pub const MAX_NODES: usize = 65_536;
pub const WASMTIME: &str = "48.0.5";
pub const MANIFEST_FILE: &str = "plugin.toml";
pub const WASM_FILE: &str = "plugin.wasm";
pub const GRANT_FILE: &str = "grant.json";
pub const PIN_FILE: &str = "pin.json";
pub const DATA_FOLDERS: [&str; 2] = ["data", "notes"];
pub const RESERVED_PUBLISHER: &str = "tofu";
pub const LOCAL_PUBLISHER: &str = "local";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Export {
    View,
    Rows,
    Handle,
    Commands,
    Run,
}

impl Export {
    fn ticks(self) -> u64 {
        match self {
            Export::View | Export::Rows | Export::Commands => RENDER_TICKS,
            Export::Handle | Export::Run => COMMAND_TICKS,
        }
    }

    fn name(self) -> &'static str {
        match self {
            Export::View => "view",
            Export::Rows => "rows",
            Export::Handle => "handle",
            Export::Commands => "commands.list",
            Export::Run => "commands.run",
        }
    }
}

#[derive(Debug)]
pub enum TreeFault {
    Empty,
    TooLarge(usize),
    BadChild { node: usize, child: u32 },
    Orphan(usize),
}

#[derive(Debug)]
pub enum Error {
    Manifest(toml::de::Error),
    Grant(serde_json::Error),
    GrantShape,
    NoHome(&'static str),
    Refused {
        id: Id,
        refusal: Refusal,
    },
    Conflict {
        id: Id,
        installed: String,
        incoming: String,
    },
    Disabled {
        plugin: Id,
    },
    Api(String),
    Id(String, IdRule),
    Capability(String),
    CapabilityValue(String),
    At {
        path: PathBuf,
        source: Box<Error>,
    },
    Io {
        path: PathBuf,
        source: io::Error,
    },
    Folder {
        folder: String,
        id: Id,
    },
    Undeclared {
        plugin: Id,
        capability: Capability,
    },
    Import {
        plugin: Id,
        import: String,
        capability: Option<Capability>,
    },
    Engine(wasmtime::Error),
    Scan(wasmparser::BinaryReaderError),
    Load {
        plugin: Id,
        source: wasmtime::Error,
    },
    Deadline {
        plugin: Id,
        export: Export,
    },
    Trap {
        plugin: Id,
        export: Export,
        source: wasmtime::Error,
    },
    Tree {
        plugin: Id,
        export: Export,
        fault: TreeFault,
    },
    Stopped {
        plugin: Id,
    },
    CommandArgs {
        command: String,
        source: serde_json::Error,
    },
    Commands {
        plugin: Id,
        declared: Vec<String>,
        exported: Vec<String>,
    },
    CommandGrant(serde_json::Error),
    CommandGrantVersion(u32),
    Withheld {
        plugin: Id,
        command: String,
        why: Withheld,
    },
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Error::Manifest(e) => write!(f, "{MANIFEST_FILE}: {e}"),
            Error::Grant(e) => write!(f, "{GRANT_FILE}: {e}"),
            Error::GrantShape => write!(
                f,
                "{GRANT_FILE}: expected version {GRANT_VERSION} with modes, or an old granted list"
            ),
            Error::NoHome(name) => write!(f, "{name} is not set, so there is no plugins home"),
            Error::Refused { id, refusal } => write!(f, "plugin {id}: refused, {refusal}"),
            Error::Conflict {
                id,
                installed,
                incoming,
            } => write!(
                f,
                "plugin {id}: {installed} is installed, replacing it with {incoming} needs the person to confirm"
            ),
            Error::Disabled { plugin } => write!(
                f,
                "plugin {plugin}: disabled, a plugin from a project folder never enables itself"
            ),
            Error::Api(api) => write!(
                f,
                "api {api} is not supported, this desk speaks desk@0.1, desk@0.2 and desk@0.3"
            ),
            Error::Id(id, rule) => write!(f, "id {id} is refused: {rule}"),
            Error::Capability(c) => write!(f, "unknown capability {c}"),
            Error::CapabilityValue(c) => {
                write!(f, "capability {c} takes true, false or a list of strings")
            }
            Error::At { path, source } => write!(f, "{}: {source}", path.display()),
            Error::Io { path, source } => write!(f, "{}: {source}", path.display()),
            Error::Folder { folder, id } => {
                write!(f, "folder {folder} holds plugin {id}, the names must match")
            }
            Error::Undeclared { plugin, capability } => write!(
                f,
                "plugin {plugin}: {GRANT_FILE} grants {capability}, which {MANIFEST_FILE} does not declare"
            ),
            Error::Import {
                plugin,
                import,
                capability: Some(capability),
            } => write!(
                f,
                "plugin {plugin}: refused, it imports {import}, which needs capability {capability} that {MANIFEST_FILE} does not declare"
            ),
            Error::Import {
                plugin,
                import,
                capability: None,
            } => write!(
                f,
                "plugin {plugin}: refused, it imports {import}, which this desk does not offer"
            ),
            Error::Engine(e) => write!(f, "plugin engine: {e:?}"),
            Error::Scan(e) => write!(f, "plugin wasm does not parse: {e}"),
            Error::Load { plugin, source } => write!(f, "plugin {plugin}: load failed: {source:?}"),
            Error::Deadline { plugin, export } => write!(
                f,
                "plugin {plugin}: {} passed its {} ms deadline and was stopped",
                export.name(),
                export.ticks()
            ),
            Error::Trap {
                plugin,
                export,
                source,
            } => write!(f, "plugin {plugin}: {} trapped: {source:?}", export.name()),
            Error::Tree {
                plugin,
                export,
                fault,
            } => {
                write!(
                    f,
                    "plugin {plugin}: {} returned a bad tree: ",
                    export.name()
                )?;
                match fault {
                    TreeFault::Empty => write!(f, "no root"),
                    TreeFault::TooLarge(n) => write!(f, "{n} nodes, the cap is {MAX_NODES}"),
                    TreeFault::BadChild { node, child } => {
                        write!(f, "node {node} names child {child}")
                    }
                    TreeFault::Orphan(node) => write!(f, "node {node} has no parent"),
                }
            }
            Error::Stopped { plugin } => write!(f, "plugin {plugin}: its worker has stopped"),
            Error::CommandArgs { command, source } => write!(
                f,
                "{MANIFEST_FILE}: command {command} has args that are not a JSON schema: {source}"
            ),
            Error::Commands {
                plugin,
                declared,
                exported,
            } => write!(
                f,
                "plugin {plugin}: refused, {MANIFEST_FILE} names commands [{}] and its export lists [{}]",
                declared.join(", "),
                exported.join(", ")
            ),
            Error::CommandGrant(e) => write!(f, "{COMMAND_GRANT_FILE}: {e}"),
            Error::CommandGrantVersion(version) => write!(
                f,
                "{COMMAND_GRANT_FILE}: version {version}, this desk reads {COMMAND_GRANT_VERSION}"
            ),
            Error::Withheld {
                plugin,
                command,
                why: Withheld::PersonOnly,
            } => write!(
                f,
                "plugin {plugin}: {command} is person only, its {MANIFEST_FILE} does not set agent = true, so only a person runs it from the desk"
            ),
            Error::Withheld {
                plugin,
                command,
                why: Withheld::Ask,
            } => write!(
                f,
                "plugin {plugin}: {command} is refused by its grant, ask, and the desk has no prompt to ask with yet: set Allow in Settings > Plugins > {plugin} > Agent control"
            ),
            Error::Withheld {
                plugin,
                command,
                why: Withheld::Deny,
            } => write!(
                f,
                "plugin {plugin}: {command} is refused by its grant, deny: set Allow in Settings > Plugins > {plugin} > Agent control to let agents run it"
            ),
        }
    }
}

impl std::error::Error for Error {}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Activity {
    Notice {
        title: String,
        body: String,
    },
    Denied(Capability),
    Fs {
        op: Op,
        path: String,
        outcome: Outcome,
    },
}

struct State {
    limits: StoreLimits,
    granted: BTreeSet<Capability>,
    folder: Folder,
    activity: Vec<Activity>,
}

impl State {
    fn notice(&mut self, title: String, body: String) -> Result<(), String> {
        if !self.granted.contains(&Capability::Notify) {
            self.activity.push(Activity::Denied(Capability::Notify));
            return Err(Capability::Notify.name().to_owned());
        }
        self.activity.push(Activity::Notice { title, body });
        Ok(())
    }

    fn fs<T>(
        &mut self,
        op: Op,
        path: String,
        run: impl FnOnce(&Folder, &str) -> Result<T, FsError>,
    ) -> Result<T, FsError> {
        let done = run(&self.folder, &path);
        let outcome = Outcome::of(&done);
        self.activity.push(Activity::Fs { op, path, outcome });
        done
    }
}

macro_rules! guest_fs {
    ($($fs:ident)::+) => {
        impl From<FsError> for $($fs)::+::FsError {
            fn from(error: FsError) -> Self {
                match error {
                    FsError::Denied(refusal) => Self::Denied(refusal.to_string()),
                    FsError::Missing => Self::Missing,
                    FsError::TooLarge(bytes) => Self::TooLarge(bytes),
                    FsError::Io(error) => Self::Failed(error.to_string()),
                }
            }
        }

        impl $($fs)::+::Host for State {
            fn read(&mut self, path: String) -> Result<Vec<u8>, $($fs)::+::FsError> {
                Ok(self.fs(Op::Read, path, Folder::read)?)
            }

            fn write(&mut self, path: String, data: Vec<u8>) -> Result<(), $($fs)::+::FsError> {
                Ok(self.fs(Op::Write, path, |folder, path| folder.write(path, &data))?)
            }

            fn list(&mut self, path: String) -> Result<Vec<$($fs)::+::Entry>, $($fs)::+::FsError> {
                let entries = self.fs(Op::List, path, Folder::list)?;
                Ok(entries
                    .into_iter()
                    .map(|entry| $($fs)::+::Entry {
                        name: entry.name,
                        folder: entry.folder,
                        bytes: entry.bytes,
                    })
                    .collect())
            }

            fn remove(&mut self, path: String) -> Result<(), $($fs)::+::FsError> {
                Ok(self.fs(Op::Remove, path, Folder::remove)?)
            }
        }
    };
}

guest_fs!(since_v0_2_0::tofu::desk::fs);
guest_fs!(since_v0_3_0::tofu::desk::fs);

impl since_v0_1_0::tofu::desk::types::Host for State {}

impl since_v0_1_0::tofu::desk::host::Host for State {
    fn notify(
        &mut self,
        title: String,
        body: String,
    ) -> Result<(), since_v0_1_0::tofu::desk::host::Denied> {
        self.notice(title, body)
            .map_err(|permission| since_v0_1_0::tofu::desk::host::Denied { permission })
    }
}

impl since_v0_2_0::tofu::desk::types::Host for State {}

impl since_v0_2_0::tofu::desk::host::Host for State {
    fn notify(
        &mut self,
        title: String,
        body: String,
    ) -> Result<(), since_v0_2_0::tofu::desk::host::Denied> {
        self.notice(title, body)
            .map_err(|permission| since_v0_2_0::tofu::desk::host::Denied { permission })
    }
}

impl since_v0_3_0::tofu::desk::types::Host for State {}

impl since_v0_3_0::tofu::desk::host::Host for State {
    fn notify(
        &mut self,
        title: String,
        body: String,
    ) -> Result<(), since_v0_3_0::tofu::desk::host::Denied> {
        self.notice(title, body)
            .map_err(|permission| since_v0_3_0::tofu::desk::host::Denied { permission })
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Status {
    Enabled,
    Disabled,
}

#[derive(Debug, Clone)]
pub struct Installed {
    pub dir: PathBuf,
    pub manifest: Manifest,
    pub modes: BTreeMap<Capability, Mode>,
    pub status: Status,
}

#[derive(Debug, Default)]
pub struct Discovered {
    pub plugins: Vec<Installed>,
    pub refused: Vec<Error>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Cache {
    Hit,
    Miss,
}

pub struct Host {
    home: Home,
    engine: Engine,
    linker_v0_1_0: Linker<State>,
    linker_v0_2_0: Linker<State>,
    linker_v0_3_0: Linker<State>,
}

impl Host {
    pub fn new(home: &Home) -> Result<Self, Error> {
        let mut config = Config::new();
        config.epoch_interruption(true);
        let engine = Engine::new(&config).map_err(Error::Engine)?;
        let mut linker_v0_1_0 = Linker::new(&engine);
        since_v0_1_0::Plugin::add_to_linker::<_, HasSelf<_>>(&mut linker_v0_1_0, |s| s)
            .map_err(Error::Engine)?;
        let mut linker_v0_2_0 = Linker::new(&engine);
        since_v0_2_0::Plugin::add_to_linker::<_, HasSelf<_>>(&mut linker_v0_2_0, |s| s)
            .map_err(Error::Engine)?;
        let mut linker_v0_3_0 = Linker::new(&engine);
        since_v0_3_0::Plugin::add_to_linker::<_, HasSelf<_>>(&mut linker_v0_3_0, |s| s)
            .map_err(Error::Engine)?;
        let weak = engine.weak();
        thread::spawn(move || {
            let started = Instant::now();
            let mut ticks = 0;
            while let Some(engine) = weak.upgrade() {
                while ticks < started.elapsed().as_millis() {
                    engine.increment_epoch();
                    ticks += 1;
                }
                drop(engine);
                thread::sleep(TICK);
            }
        });
        Ok(Self {
            home: home.clone(),
            engine,
            linker_v0_1_0,
            linker_v0_2_0,
            linker_v0_3_0,
        })
    }

    pub fn discover(&self, project: Option<&Path>) -> Discovered {
        let mut found = BTreeMap::new();
        let mut refused = Vec::new();
        let roots = [(self.home.plugins.as_path(), Status::Enabled)]
            .into_iter()
            .chain(project.map(|p| (p, Status::Disabled)));
        for (root, status) in roots {
            let entries = match fs::read_dir(root) {
                Ok(entries) => entries,
                Err(e) if e.kind() == io::ErrorKind::NotFound => continue,
                Err(source) => {
                    refused.push(Error::Io {
                        path: root.to_owned(),
                        source,
                    });
                    continue;
                }
            };
            for entry in entries {
                let dir = match entry {
                    Ok(entry) => entry.path(),
                    Err(source) => {
                        refused.push(Error::Io {
                            path: root.to_owned(),
                            source,
                        });
                        continue;
                    }
                };
                let hidden = dir
                    .file_name()
                    .is_none_or(|n| n.to_string_lossy().starts_with('.'));
                if hidden || !dir.join(MANIFEST_FILE).is_file() {
                    continue;
                }
                match installed(&dir, status) {
                    Ok(plugin) => {
                        found.insert(plugin.manifest.id.clone(), plugin);
                    }
                    Err(e) => refused.push(e),
                }
            }
        }
        Discovered {
            plugins: found.into_values().collect(),
            refused,
        }
    }

    pub fn load(&self, plugin: &Installed) -> Result<(Plugin, Cache), Error> {
        let id = &plugin.manifest.id;
        if plugin.status == Status::Disabled {
            return Err(Error::Disabled { plugin: id.clone() });
        }
        let wasm_path = plugin.dir.join(WASM_FILE);
        let wasm = fs::read(&wasm_path).map_err(io_at(&wasm_path))?;
        let (component, cache) = self.compile(id, &wasm)?;
        self.check_imports(&plugin.manifest, &component)?;
        if let Some(&capability) = plugin
            .modes
            .keys()
            .find(|c| !plugin.manifest.capabilities.contains_key(c))
        {
            return Err(Error::Undeclared {
                plugin: id.clone(),
                capability,
            });
        }
        let limits = StoreLimitsBuilder::new()
            .memory_size(MEMORY_BYTES)
            .trap_on_grow_failure(true)
            .build();
        let mut store = Store::new(
            &self.engine,
            State {
                limits,
                granted: plugin
                    .modes
                    .iter()
                    .filter(|&(_, &mode)| mode == Mode::Allow)
                    .map(|(&c, _)| c)
                    .collect(),
                folder: Folder::new(id.clone(), plugin.dir.clone()),
                activity: Vec::new(),
            },
        );
        store.limiter(|s| &mut s.limits);
        store.set_epoch_deadline(RENDER_TICKS);
        let failed = |source| Error::Load {
            plugin: id.clone(),
            source,
        };
        let exports = match plugin.manifest.api {
            Api::V0_1_0 => Exports::V0_1_0(
                since_v0_1_0::Plugin::instantiate(&mut store, &component, &self.linker_v0_1_0)
                    .map_err(failed)?,
            ),
            Api::V0_2_0 => Exports::V0_2_0(
                since_v0_2_0::Plugin::instantiate(&mut store, &component, &self.linker_v0_2_0)
                    .map_err(failed)?,
            ),
            Api::V0_3_0 => Exports::V0_3_0(
                since_v0_3_0::Plugin::instantiate(&mut store, &component, &self.linker_v0_3_0)
                    .map_err(failed)?,
            ),
        };
        let mut instance = Instance {
            id: id.clone(),
            store,
            exports,
        };
        let commands = instance.call(Export::Commands, |exports, s| match exports {
            Exports::V0_3_0(p) => p.tofu_desk_commands().call_list(s),
            Exports::V0_1_0(_) | Exports::V0_2_0(_) => Ok(Vec::new()),
        })?;
        let mut declared: Vec<String> = plugin
            .manifest
            .contributes
            .commands
            .iter()
            .map(|command| command.name.clone())
            .collect();
        let mut exported: Vec<String> = commands.iter().map(|c| c.id.clone()).collect();
        declared.sort();
        exported.sort();
        if declared != exported {
            return Err(Error::Commands {
                plugin: id.clone(),
                declared,
                exported,
            });
        }
        Ok((Plugin::spawn(instance, commands)?, cache))
    }

    fn compile(&self, id: &Id, wasm: &[u8]) -> Result<(Component, Cache), Error> {
        let dir = &self.home.cache;
        let path = dir.join(format!("{}-{WASMTIME}", sha256_hex(wasm)));
        match fs::read(&path) {
            Ok(bytes) => {
                #[expect(
                    unsafe_code,
                    reason = "deserialize trusts its bytes; the file sits in the plugins home cache, which only this host writes, under a name keyed by the wasm hash and the wasmtime version, and wasmtime still checks the version and engine config header"
                )]
                let cached = unsafe { Component::deserialize(&self.engine, &bytes) };
                if let Ok(component) = cached {
                    return Ok((component, Cache::Hit));
                }
            }
            Err(e) if e.kind() == io::ErrorKind::NotFound => {}
            Err(source) => return Err(Error::Io { path, source }),
        }
        let component = Component::new(&self.engine, wasm).map_err(|source| Error::Load {
            plugin: id.clone(),
            source,
        })?;
        let bytes = component.serialize().map_err(Error::Engine)?;
        fs::create_dir_all(dir).map_err(io_at(dir))?;
        write_whole(&path, &bytes)?;
        Ok((component, Cache::Miss))
    }

    fn check_imports(
        &self,
        manifest: &Manifest,
        component: &Component,
    ) -> Result<Vec<String>, Error> {
        let refuse = |import: String, capability| Error::Import {
            plugin: manifest.id.clone(),
            import,
            capability,
        };
        let mut imports = Vec::new();
        for (name, import) in component.component_type().imports(&self.engine) {
            let ComponentItem::ComponentInstance(instance) = import.ty else {
                return Err(refuse(name.to_owned(), None));
            };
            for (func, item) in instance.exports(&self.engine) {
                if !matches!(item.ty, ComponentItem::ComponentFunc(_)) {
                    continue;
                }
                let import = format!("{name}#{func}");
                match need_of(manifest.api, name, func) {
                    Some(Need::Free) => imports.push(import),
                    Some(Need::Granted(c)) if manifest.capabilities.contains_key(&c) => {
                        imports.push(import)
                    }
                    Some(Need::Granted(c)) => return Err(refuse(import, Some(c))),
                    None => return Err(refuse(import, None)),
                }
            }
        }
        Ok(imports)
    }
}

fn sha256_hex(bytes: &[u8]) -> String {
    Sha256::digest(bytes)
        .iter()
        .map(|b| format!("{b:02x}"))
        .collect()
}

fn io_at(path: &Path) -> impl FnOnce(io::Error) -> Error + use<> {
    let path = path.to_owned();
    move |source| Error::Io { path, source }
}

fn write_whole(path: &Path, bytes: &[u8]) -> Result<(), Error> {
    let partial = path.with_extension("partial");
    fs::write(&partial, bytes).map_err(io_at(&partial))?;
    fs::rename(&partial, path).map_err(io_at(path))
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Need {
    Free,
    Granted(Capability),
}

fn need_of(api: Api, interface: &str, func: &str) -> Option<Need> {
    match (api, interface, func) {
        (Api::V0_1_0, "tofu:desk/host@0.1.0", "notify")
        | (Api::V0_2_0, "tofu:desk/host@0.2.0", "notify")
        | (Api::V0_3_0, "tofu:desk/host@0.3.0", "notify") => {
            Some(Need::Granted(Capability::Notify))
        }
        (Api::V0_2_0, "tofu:desk/fs@0.2.0", "read" | "write" | "list" | "remove")
        | (Api::V0_3_0, "tofu:desk/fs@0.3.0", "read" | "write" | "list" | "remove") => {
            Some(Need::Free)
        }
        _ => None,
    }
}

fn at(path: &Path) -> impl FnOnce(Error) -> Error + use<> {
    let path = path.to_owned();
    move |source| Error::At {
        path,
        source: Box::new(source),
    }
}

fn manifest_in(dir: &Path) -> Result<(Manifest, String), Error> {
    let path = dir.join(MANIFEST_FILE);
    let text = fs::read_to_string(&path).map_err(io_at(&path))?;
    let manifest = Manifest::parse(&text).map_err(at(&path))?;
    Ok((manifest, text))
}

fn named_manifest(dir: &Path) -> Result<(Manifest, String), Error> {
    let (manifest, text) = manifest_in(dir)?;
    let folder = dir
        .file_name()
        .map(|n| n.to_string_lossy().into_owned())
        .unwrap_or_default();
    if folder != manifest.id.as_str() {
        return Err(Error::Folder {
            folder,
            id: manifest.id,
        });
    }
    Ok((manifest, text))
}

fn installed(dir: &Path, status: Status) -> Result<Installed, Error> {
    let (manifest, _) = named_manifest(dir)?;
    let grant_path = dir.join(GRANT_FILE);
    let grant = match status {
        Status::Disabled => None,
        Status::Enabled => match fs::read_to_string(&grant_path) {
            Ok(text) => Some(text),
            Err(e) if e.kind() == io::ErrorKind::NotFound => None,
            Err(source) => return Err(io_at(&grant_path)(source)),
        },
    };
    let modes = match grant {
        Some(text) => manifest::parse_grant(&text).map_err(at(&grant_path))?,
        None => BTreeMap::new(),
    };
    Ok(Installed {
        dir: dir.to_owned(),
        manifest,
        modes,
        status,
    })
}

enum Exports {
    V0_1_0(since_v0_1_0::Plugin),
    V0_2_0(since_v0_2_0::Plugin),
    V0_3_0(since_v0_3_0::Plugin),
}

struct Instance {
    id: Id,
    store: Store<State>,
    exports: Exports,
}

type Job = Box<dyn FnOnce(&mut Instance) + Send>;

pub struct Plugin {
    id: Id,
    folder: Folder,
    commands: Vec<Descriptor>,
    jobs: Sender<Job>,
}

pub struct Pending<T> {
    plugin: Id,
    reply: Receiver<Result<T, Error>>,
}

impl<T> Pending<T> {
    pub fn wait(self) -> Result<T, Error> {
        self.reply.recv().unwrap_or_else(|_| {
            Err(Error::Stopped {
                plugin: self.plugin,
            })
        })
    }

    pub fn try_take(&self) -> Option<Result<T, Error>> {
        match self.reply.try_recv() {
            Ok(result) => Some(result),
            Err(TryRecvError::Empty) => None,
            Err(TryRecvError::Disconnected) => Some(Err(Error::Stopped {
                plugin: self.plugin.clone(),
            })),
        }
    }
}

impl Plugin {
    fn spawn(mut instance: Instance, commands: Vec<Descriptor>) -> Result<Self, Error> {
        let id = instance.id.clone();
        let folder = instance.store.data().folder.clone();
        let (jobs, inbox) = mpsc::channel::<Job>();
        thread::Builder::new()
            .name(format!("plugin {id}"))
            .spawn(move || {
                for job in inbox {
                    job(&mut instance);
                }
            })
            .map_err(|source| Error::Io {
                path: PathBuf::from(id.as_str()),
                source,
            })?;
        Ok(Self {
            id,
            folder,
            commands,
            jobs,
        })
    }

    pub fn id(&self) -> &Id {
        &self.id
    }

    pub fn commands(&self) -> &[Descriptor] {
        &self.commands
    }

    pub fn folder(&self) -> &Folder {
        &self.folder
    }

    fn ask<T: Send + 'static>(
        &self,
        run: impl FnOnce(&mut Instance) -> Result<T, Error> + Send + 'static,
    ) -> Pending<T> {
        let (tx, reply) = mpsc::channel();
        let job: Job = Box::new(move |instance| {
            if let Err(unread) = tx.send(run(instance)) {
                drop(unread);
            }
        });
        if let Err(lost) = self.jobs.send(job) {
            drop(lost);
        }
        Pending {
            plugin: self.id.clone(),
            reply,
        }
    }

    pub fn view(&self) -> Pending<Vec<Node>> {
        self.ask(|i| {
            let tree = i.call(Export::View, |exports, s| match exports {
                Exports::V0_1_0(p) => p.call_view(s).map(ladder::tree),
                Exports::V0_2_0(p) => p.call_view(s),
                Exports::V0_3_0(p) => p.call_view(s).map(ladder::from_next),
            })?;
            i.checked(Export::View, tree)
        })
    }

    pub fn rows(&self, range: Range<u64>) -> Pending<Vec<Node>> {
        self.ask(move |i| {
            let (start, end) = (range.start, range.end);
            let tree = i.call(Export::Rows, |exports, s| match exports {
                Exports::V0_1_0(p) => p.call_rows(s, start, end).map(ladder::tree),
                Exports::V0_2_0(p) => p.call_rows(s, start, end),
                Exports::V0_3_0(p) => p.call_rows(s, start, end).map(ladder::from_next),
            })?;
            i.checked(Export::Rows, tree)
        })
    }

    pub fn handle(&self, event: Event) -> Pending<()> {
        self.ask(move |i| {
            i.call(Export::Handle, |exports, s| match exports {
                Exports::V0_1_0(p) => match ladder::event(&event) {
                    Some(old) => p.call_handle(s, &old),
                    None => Ok(()),
                },
                Exports::V0_2_0(p) => p.call_handle(s, &event),
                Exports::V0_3_0(p) => p.call_handle(s, &ladder::to_next(&event)),
            })
        })
    }

    pub fn run(&self, command: String, args: String) -> Pending<Result<String, String>> {
        let none = format!("plugin {} exports no command {command}", self.id);
        self.ask(move |i| {
            i.call(Export::Run, |exports, s| match exports {
                Exports::V0_3_0(p) => p.tofu_desk_commands().call_run(s, &command, &args),
                Exports::V0_1_0(_) | Exports::V0_2_0(_) => Ok(Err(none)),
            })
        })
    }

    pub fn take_activity(&self) -> Pending<Vec<Activity>> {
        self.ask(|i| Ok(std::mem::take(&mut i.store.data_mut().activity)))
    }
}

impl Instance {
    fn call<T>(
        &mut self,
        export: Export,
        run: impl FnOnce(&Exports, &mut Store<State>) -> wasmtime::Result<T>,
    ) -> Result<T, Error> {
        self.store.set_epoch_deadline(export.ticks());
        run(&self.exports, &mut self.store).map_err(|source| {
            let plugin = self.id.clone();
            match source.downcast_ref::<Trap>() {
                Some(Trap::Interrupt) => Error::Deadline { plugin, export },
                _ => Error::Trap {
                    plugin,
                    export,
                    source,
                },
            }
        })
    }

    fn checked(&self, export: Export, tree: Vec<Node>) -> Result<Vec<Node>, Error> {
        let fault = |fault| Error::Tree {
            plugin: self.id.clone(),
            export,
            fault,
        };
        if tree.is_empty() {
            return Err(fault(TreeFault::Empty));
        }
        if tree.len() > MAX_NODES {
            return Err(fault(TreeFault::TooLarge(tree.len())));
        }
        let mut parented = vec![false; tree.len()];
        for (node, n) in tree.iter().enumerate() {
            for &child in &n.children {
                let slot = usize::try_from(child)
                    .ok()
                    .filter(|&c| c > node)
                    .and_then(|c| parented.get_mut(c));
                match slot {
                    Some(seen) if !*seen => *seen = true,
                    _ => return Err(fault(TreeFault::BadChild { node, child })),
                }
            }
        }
        match parented.iter().skip(1).position(|seen| !seen) {
            Some(orphan) => Err(fault(TreeFault::Orphan(orphan + 1))),
            None => Ok(tree),
        }
    }
}
