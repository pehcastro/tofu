use serde::Deserialize;

use crate::Error;

pub const API: &str = "0.1";

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Scope {
    Project,
    Plugin,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Permission {
    FsRead(Scope),
    FsWrite(Scope),
    Net { host: String, port: u16 },
    DbConnect(String),
    Process(String),
    Clipboard,
    Notify,
}

#[derive(Debug, Clone)]
pub struct Manifest {
    pub name: String,
    pub version: String,
    pub permissions: Vec<Permission>,
}

#[derive(Deserialize)]
struct Raw {
    name: String,
    version: String,
    api: String,
    #[serde(default)]
    permissions: Vec<String>,
}

impl Manifest {
    pub fn parse(text: &str) -> Result<Self, Error> {
        let raw: Raw = toml::from_str(text).map_err(Error::Manifest)?;
        if raw.api != API {
            return Err(Error::Api(raw.api));
        }
        let permissions = raw
            .permissions
            .iter()
            .map(|p| Permission::parse(p))
            .collect::<Result<_, _>>()?;
        Ok(Self {
            name: raw.name,
            version: raw.version,
            permissions,
        })
    }
}

impl Permission {
    fn parse(text: &str) -> Result<Self, Error> {
        let unknown = || Error::Permission(text.to_owned());
        let (verb, target) = text.split_once(':').unwrap_or((text, ""));
        let scope = || match target {
            "project" => Ok(Scope::Project),
            "plugin" => Ok(Scope::Plugin),
            _ => Err(unknown()),
        };
        let named = || {
            if target.is_empty() {
                Err(unknown())
            } else {
                Ok(target.to_owned())
            }
        };
        match verb {
            "fs.read" => scope().map(Permission::FsRead),
            "fs.write" => scope().map(Permission::FsWrite),
            "db.connect" => named().map(Permission::DbConnect),
            "process" => named().map(Permission::Process),
            "net" => {
                let (host, port) = target.rsplit_once(':').ok_or_else(unknown)?;
                let port = port.parse().map_err(|_| unknown())?;
                if host.is_empty() {
                    return Err(unknown());
                }
                Ok(Permission::Net {
                    host: host.to_owned(),
                    port,
                })
            }
            "clipboard" if target.is_empty() => Ok(Permission::Clipboard),
            "notify" if target.is_empty() => Ok(Permission::Notify),
            _ => Err(unknown()),
        }
    }
}
