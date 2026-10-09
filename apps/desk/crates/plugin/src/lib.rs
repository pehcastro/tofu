mod manifest;

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

pub use manifest::{Api, Capability, Command, Contributes, Id, IdRule, MAX_ID_LEN, Manifest};
pub use since_v0_1_0::tofu::desk::types::{Badge, Choice, Event, Kind, Node, Table, Toggle, Tone};

mod since_v0_1_0 {
    wasmtime::component::bindgen!({ path: "../../wit/since_v0.1.0", world: "plugin" });
}

use since_v0_1_0::tofu::desk::host::Denied;

pub const TICK: Duration = Duration::from_millis(1);
pub const RENDER_TICKS: u64 = 50;
pub const COMMAND_TICKS: u64 = 5_000;
pub const MEMORY_BYTES: usize = 64 << 20;
pub const MAX_NODES: usize = 65_536;
pub const WASMTIME: &str = "48.0.5";
pub const MANIFEST_FILE: &str = "plugin.toml";
pub const WASM_FILE: &str = "plugin.wasm";
pub const GRANT_FILE: &str = "grant.json";
pub const CACHE_DIR: &str = ".cache";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Export {
    View,
    Rows,
    Handle,
}

impl Export {
    fn ticks(self) -> u64 {
        match self {
            Export::View | Export::Rows => RENDER_TICKS,
            Export::Handle => COMMAND_TICKS,
        }
    }

    fn name(self) -> &'static str {
        match self {
            Export::View => "view",
            Export::Rows => "rows",
            Export::Handle => "handle",
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
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Error::Manifest(e) => write!(f, "{MANIFEST_FILE}: {e}"),
            Error::Grant(e) => write!(f, "{GRANT_FILE}: {e}"),
            Error::Api(api) => write!(f, "api {api} is not supported, this desk speaks desk@0.1"),
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
        }
    }
}

impl std::error::Error for Error {}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Activity {
    Notice { title: String, body: String },
    Denied(Capability),
}

struct State {
    limits: StoreLimits,
    granted: BTreeSet<Capability>,
    activity: Vec<Activity>,
}

impl since_v0_1_0::tofu::desk::types::Host for State {}

impl since_v0_1_0::tofu::desk::host::Host for State {
    fn notify(&mut self, title: String, body: String) -> Result<(), Denied> {
        if !self.granted.contains(&Capability::Notify) {
            self.activity.push(Activity::Denied(Capability::Notify));
            return Err(Denied {
                permission: Capability::Notify.name().to_owned(),
            });
        }
        self.activity.push(Activity::Notice { title, body });
        Ok(())
    }
}

#[derive(Debug, Clone)]
pub struct Installed {
    pub dir: PathBuf,
    pub manifest: Manifest,
    pub granted: BTreeSet<Capability>,
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
    home: PathBuf,
    engine: Engine,
    linker_v0_1_0: Linker<State>,
}

impl Host {
    pub fn new(home: &Path) -> Result<Self, Error> {
        let mut config = Config::new();
        config.epoch_interruption(true);
        let engine = Engine::new(&config).map_err(Error::Engine)?;
        let mut linker_v0_1_0 = Linker::new(&engine);
        since_v0_1_0::Plugin::add_to_linker::<_, HasSelf<_>>(&mut linker_v0_1_0, |s| s)
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
            home: home.to_owned(),
            engine,
            linker_v0_1_0,
        })
    }

    pub fn discover(&self, project: Option<&Path>) -> Discovered {
        let mut found = BTreeMap::new();
        let mut refused = Vec::new();
        for root in [Some(self.home.as_path()), project].into_iter().flatten() {
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
                if hidden || !dir.is_dir() {
                    continue;
                }
                match installed(&dir) {
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
        let wasm_path = plugin.dir.join(WASM_FILE);
        let wasm = fs::read(&wasm_path).map_err(|source| Error::Io {
            path: wasm_path,
            source,
        })?;
        let (component, cache) = self.compile(id, &wasm)?;
        self.check_imports(&plugin.manifest, &component)?;
        if let Some(&capability) = plugin
            .granted
            .iter()
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
                granted: plugin.granted.clone(),
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
        };
        let instance = Instance {
            id: id.clone(),
            store,
            exports,
        };
        Ok((Plugin::spawn(instance)?, cache))
    }

    fn compile(&self, id: &Id, wasm: &[u8]) -> Result<(Component, Cache), Error> {
        let hash: String = Sha256::digest(wasm)
            .iter()
            .map(|b| format!("{b:02x}"))
            .collect();
        let dir = self.home.join(CACHE_DIR);
        let path = dir.join(format!("{hash}-{WASMTIME}"));
        let io = |path: &Path| {
            let path = path.to_owned();
            move |source| Error::Io { path, source }
        };
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
        fs::create_dir_all(&dir).map_err(io(&dir))?;
        let partial = path.with_extension("partial");
        fs::write(&partial, bytes).map_err(io(&partial))?;
        fs::rename(&partial, &path).map_err(io(&path))?;
        Ok((component, Cache::Miss))
    }

    fn check_imports(&self, manifest: &Manifest, component: &Component) -> Result<(), Error> {
        let refuse = |import: String, capability| Error::Import {
            plugin: manifest.id.clone(),
            import,
            capability,
        };
        for (name, import) in component.component_type().imports(&self.engine) {
            let ComponentItem::ComponentInstance(instance) = import.ty else {
                return Err(refuse(name.to_owned(), None));
            };
            for (func, item) in instance.exports(&self.engine) {
                if !matches!(item.ty, ComponentItem::ComponentFunc(_)) {
                    continue;
                }
                let import = format!("{name}#{func}");
                match capability_of(manifest.api, name, func) {
                    Some(c) if manifest.capabilities.contains_key(&c) => {}
                    capability => return Err(refuse(import, capability)),
                }
            }
        }
        Ok(())
    }
}

fn capability_of(api: Api, interface: &str, func: &str) -> Option<Capability> {
    match (api, interface, func) {
        (Api::V0_1_0, "tofu:desk/host@0.1.0", "notify") => Some(Capability::Notify),
        _ => None,
    }
}

fn installed(dir: &Path) -> Result<Installed, Error> {
    let at = |path: &Path| {
        let path = path.to_owned();
        move |source| Error::At {
            path,
            source: Box::new(source),
        }
    };
    let manifest_path = dir.join(MANIFEST_FILE);
    let text = fs::read_to_string(&manifest_path).map_err(|source| Error::Io {
        path: manifest_path.clone(),
        source,
    })?;
    let manifest = Manifest::parse(&text).map_err(at(&manifest_path))?;
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
    let grant_path = dir.join(GRANT_FILE);
    let granted = match fs::read_to_string(&grant_path) {
        Ok(text) => manifest::parse_grant(&text).map_err(at(&grant_path))?,
        Err(e) if e.kind() == io::ErrorKind::NotFound => BTreeSet::new(),
        Err(source) => {
            return Err(Error::Io {
                path: grant_path,
                source,
            });
        }
    };
    Ok(Installed {
        dir: dir.to_owned(),
        manifest,
        granted,
    })
}

enum Exports {
    V0_1_0(since_v0_1_0::Plugin),
}

struct Instance {
    id: Id,
    store: Store<State>,
    exports: Exports,
}

type Job = Box<dyn FnOnce(&mut Instance) + Send>;

pub struct Plugin {
    id: Id,
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
    fn spawn(mut instance: Instance) -> Result<Self, Error> {
        let id = instance.id.clone();
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
        Ok(Self { id, jobs })
    }

    pub fn id(&self) -> &Id {
        &self.id
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
            let tree = i.call(Export::View, |Exports::V0_1_0(p), s| p.call_view(s))?;
            i.checked(Export::View, tree)
        })
    }

    pub fn rows(&self, range: Range<u64>) -> Pending<Vec<Node>> {
        self.ask(move |i| {
            let tree = i.call(Export::Rows, |Exports::V0_1_0(p), s| {
                p.call_rows(s, range.start, range.end)
            })?;
            i.checked(Export::Rows, tree)
        })
    }

    pub fn handle(&self, event: Event) -> Pending<()> {
        self.ask(move |i| {
            i.call(Export::Handle, |Exports::V0_1_0(p), s| {
                p.call_handle(s, &event)
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
