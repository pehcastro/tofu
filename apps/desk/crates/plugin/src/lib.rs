mod manifest;

use std::fmt;
use std::ops::Range;
use std::thread;
use std::time::{Duration, Instant};

use wasmtime::component::{Component, HasSelf, Linker};
use wasmtime::{Config, Engine, Store, StoreLimits, StoreLimitsBuilder, Trap};

pub use manifest::{API, Manifest, Permission, Scope};
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
    Api(String),
    Permission(String),
    Engine(wasmtime::Error),
    Load {
        plugin: String,
        source: wasmtime::Error,
    },
    Deadline {
        plugin: String,
        export: Export,
    },
    Trap {
        plugin: String,
        export: Export,
        source: wasmtime::Error,
    },
    Tree {
        plugin: String,
        export: Export,
        fault: TreeFault,
    },
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Error::Manifest(e) => write!(f, "plugin.toml: {e}"),
            Error::Api(api) => write!(
                f,
                "plugin.toml: api {api} is not supported, this desk speaks {API}"
            ),
            Error::Permission(p) => write!(f, "plugin.toml: unknown permission {p}"),
            Error::Engine(e) => write!(f, "plugin engine: {e:?}"),
            Error::Load { plugin, source } => write!(f, "plugin {plugin}: load failed: {source:?}"),
            Error::Deadline { plugin, export } => {
                write!(
                    f,
                    "plugin {plugin}: {} passed its {} ms deadline and was stopped",
                    export.name(),
                    export.ticks()
                )
            }
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
        }
    }
}

impl std::error::Error for Error {}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Notice {
    pub title: String,
    pub body: String,
}

struct State {
    limits: StoreLimits,
    granted: Vec<Permission>,
    notices: Vec<Notice>,
}

impl since_v0_1_0::tofu::desk::types::Host for State {}

impl since_v0_1_0::tofu::desk::host::Host for State {
    fn notify(&mut self, title: String, body: String) -> Result<(), Denied> {
        if !self.granted.contains(&Permission::Notify) {
            return Err(Denied {
                permission: "notify".to_owned(),
            });
        }
        self.notices.push(Notice { title, body });
        Ok(())
    }
}

pub struct Host {
    engine: Engine,
    linker: Linker<State>,
}

impl Host {
    pub fn new() -> Result<Self, Error> {
        let mut config = Config::new();
        config.epoch_interruption(true);
        let engine = Engine::new(&config).map_err(Error::Engine)?;
        let mut linker = Linker::new(&engine);
        since_v0_1_0::Plugin::add_to_linker::<_, HasSelf<_>>(&mut linker, |s| s)
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
        Ok(Self { engine, linker })
    }

    pub fn load(&self, manifest: Manifest, wasm: &[u8]) -> Result<Plugin, Error> {
        let plugin = manifest.name;
        let failed = |source| Error::Load {
            plugin: plugin.clone(),
            source,
        };
        let component = Component::new(&self.engine, wasm).map_err(failed)?;
        let limits = StoreLimitsBuilder::new()
            .memory_size(MEMORY_BYTES)
            .trap_on_grow_failure(true)
            .build();
        let mut store = Store::new(
            &self.engine,
            State {
                limits,
                granted: manifest.permissions,
                notices: Vec::new(),
            },
        );
        store.limiter(|s| &mut s.limits);
        store.set_epoch_deadline(RENDER_TICKS);
        let exports = since_v0_1_0::Plugin::instantiate(&mut store, &component, &self.linker)
            .map_err(failed)?;
        Ok(Plugin {
            name: plugin,
            store,
            exports,
        })
    }
}

pub struct Plugin {
    name: String,
    store: Store<State>,
    exports: since_v0_1_0::Plugin,
}

impl Plugin {
    pub fn view(&mut self) -> Result<Vec<Node>, Error> {
        let tree = self.call(Export::View, |p, s| p.call_view(s))?;
        self.checked(Export::View, tree)
    }

    pub fn rows(&mut self, range: Range<u64>) -> Result<Vec<Node>, Error> {
        let tree = self.call(Export::Rows, |p, s| p.call_rows(s, range.start, range.end))?;
        self.checked(Export::Rows, tree)
    }

    pub fn handle(&mut self, event: &Event) -> Result<(), Error> {
        self.call(Export::Handle, |p, s| p.call_handle(s, event))
    }

    pub fn take_notices(&mut self) -> Vec<Notice> {
        std::mem::take(&mut self.store.data_mut().notices)
    }

    fn call<T>(
        &mut self,
        export: Export,
        run: impl FnOnce(&since_v0_1_0::Plugin, &mut Store<State>) -> wasmtime::Result<T>,
    ) -> Result<T, Error> {
        self.store.set_epoch_deadline(export.ticks());
        run(&self.exports, &mut self.store).map_err(|source| {
            let plugin = self.name.clone();
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
            plugin: self.name.clone(),
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
