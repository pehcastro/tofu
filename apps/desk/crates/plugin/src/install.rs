use std::env;
use std::fmt;
use std::fs;
use std::io;
use std::path::{Path, PathBuf};

use crate::{
    DATA_FOLDERS, Error, Host, Id, LOCAL_PUBLISHER, MANIFEST_FILE, PIN_FILE, RESERVED_PUBLISHER,
    WASM_FILE, io_at, manifest_in, named_manifest, sha256_hex, write_whole,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Home {
    pub plugins: PathBuf,
    pub cache: PathBuf,
}

impl Home {
    pub fn find() -> Result<Self, Error> {
        let name = if cfg!(windows) { "USERPROFILE" } else { "HOME" };
        let user = env::var_os(name)
            .filter(|v| !v.is_empty())
            .ok_or(Error::NoHome(name))?;
        Ok(Self::at(&PathBuf::from(user).join(".tofu")))
    }

    pub fn at(tofu: &Path) -> Self {
        Self {
            plugins: tofu.join("plugins"),
            cache: tofu.join("desk").join("cache").join("plugins"),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Origin {
    BuiltIn,
    Person,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Refusal {
    Reserved,
    Publisher { installed: Id },
    LocalOver { installed: Id },
}

impl fmt::Display for Refusal {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Refusal::Reserved => write!(
                f,
                "the {RESERVED_PUBLISHER}. publisher is kept for the plugins the desk ships"
            ),
            Refusal::Publisher { installed } => write!(
                f,
                "its folder already holds {installed}, from another publisher"
            ),
            Refusal::LocalOver { installed } => write!(
                f,
                "a {LOCAL_PUBLISHER}. plugin never installs over another, and {installed} is installed"
            ),
        }
    }
}

#[derive(Debug, Clone)]
pub struct Report {
    pub id: Id,
    pub version: String,
    pub sha256: String,
    pub imports: Vec<String>,
    pub dir: PathBuf,
    pub replaced: Option<String>,
}

impl Host {
    pub fn install(&self, folder: &Path, origin: Origin) -> Result<Report, Error> {
        self.put(folder, origin, false)
    }

    pub fn update(&self, folder: &Path, origin: Origin) -> Result<Report, Error> {
        self.put(folder, origin, true)
    }

    fn put(&self, folder: &Path, origin: Origin, replace: bool) -> Result<Report, Error> {
        let (manifest, text) = named_manifest(folder)?;
        let id = manifest.id.clone();
        let refuse = |refusal| Error::Refused {
            id: id.clone(),
            refusal,
        };
        if origin == Origin::Person && id.publisher() == RESERVED_PUBLISHER {
            return Err(refuse(Refusal::Reserved));
        }
        let wasm_path = folder.join(WASM_FILE);
        let wasm = fs::read(&wasm_path).map_err(io_at(&wasm_path))?;
        let (component, _) = self.compile(&id, &wasm)?;
        let imports = self.check_imports(&manifest, &component)?;
        let dir = self.home.plugins.join(id.as_str());
        let replaced = match manifest_in(&dir) {
            Ok((installed, _)) if installed.id.publisher() != id.publisher() => {
                return Err(refuse(Refusal::Publisher {
                    installed: installed.id,
                }));
            }
            Ok((installed, _)) if !replace => {
                return Err(Error::Conflict {
                    id,
                    installed: installed.version,
                    incoming: manifest.version,
                });
            }
            Ok((installed, _)) => Some(installed.version),
            Err(Error::Io { source, .. }) if source.kind() == io::ErrorKind::NotFound => None,
            Err(e) => return Err(e),
        };
        if id.publisher() == LOCAL_PUBLISHER
            && let Some(installed) = self.same_name(&id)?
        {
            return Err(refuse(Refusal::LocalOver { installed }));
        }
        let sha256 = sha256_hex(&wasm);
        let pin = serde_json::json!({ "version": manifest.version, "sha256": sha256 }).to_string();
        fs::create_dir_all(&dir).map_err(io_at(&dir))?;
        write_whole(&dir.join(WASM_FILE), &wasm)?;
        write_whole(&dir.join(PIN_FILE), pin.as_bytes())?;
        write_whole(&dir.join(MANIFEST_FILE), text.as_bytes())?;
        Ok(Report {
            id,
            version: manifest.version,
            sha256,
            imports,
            dir,
            replaced,
        })
    }

    fn same_name(&self, id: &Id) -> Result<Option<Id>, Error> {
        let root = &self.home.plugins;
        let entries = match fs::read_dir(root) {
            Ok(entries) => entries,
            Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(None),
            Err(source) => return Err(io_at(root)(source)),
        };
        for entry in entries {
            let name = entry.map_err(io_at(root))?.file_name();
            let Ok(other) = Id::parse(&name.to_string_lossy()) else {
                continue;
            };
            if other.publisher() != LOCAL_PUBLISHER && other.name() == id.name() {
                return Ok(Some(other));
            }
        }
        Ok(None)
    }

    pub fn uninstall(&self, id: &Id, keep_data: bool) -> Result<(), Error> {
        let dir = self.home.plugins.join(id.as_str());
        if !keep_data {
            return fs::remove_dir_all(&dir).map_err(io_at(&dir));
        }
        let mut kept = false;
        for entry in fs::read_dir(&dir).map_err(io_at(&dir))? {
            let entry = entry.map_err(io_at(&dir))?;
            let path = entry.path();
            let name = entry.file_name();
            if DATA_FOLDERS.iter().any(|d| name == *d) {
                kept = true;
                continue;
            }
            let removed = if entry.file_type().map_err(io_at(&path))?.is_dir() {
                fs::remove_dir_all(&path)
            } else {
                fs::remove_file(&path)
            };
            removed.map_err(io_at(&path))?;
        }
        if !kept {
            fs::remove_dir(&dir).map_err(io_at(&dir))?;
        }
        Ok(())
    }
}
