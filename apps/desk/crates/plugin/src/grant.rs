use std::collections::BTreeMap;
use std::fs;
use std::io;
use std::path::{Path, PathBuf};

use serde::Deserialize;

use crate::{Command, Error, Home, Id, Mode, at, io_at, write_whole};

pub const COMMAND_GRANT_FILE: &str = "commands.json";
pub const COMMAND_GRANT_FOLDER: &str = "plugins";
pub const COMMAND_GRANT_VERSION: u32 = 1;

type Table = BTreeMap<String, BTreeMap<String, Mode>>;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Grant {
    PersonOnly,
    Agent(Mode),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Withheld {
    PersonOnly,
    Ask,
    Deny,
}

impl Grant {
    pub fn name(self) -> &'static str {
        match self {
            Grant::PersonOnly => "person only",
            Grant::Agent(Mode::Ask) => "ask",
            Grant::Agent(Mode::Allow) => "allow",
            Grant::Agent(Mode::Deny) => "deny",
        }
    }

    pub fn admit(self, plugin: &Id, command: &str) -> Result<(), Error> {
        let why = match self {
            Grant::Agent(Mode::Allow) => return Ok(()),
            Grant::Agent(Mode::Ask) => Withheld::Ask,
            Grant::Agent(Mode::Deny) => Withheld::Deny,
            Grant::PersonOnly => Withheld::PersonOnly,
        };
        Err(Error::Withheld {
            plugin: plugin.clone(),
            command: command.to_owned(),
            why,
        })
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct File {
    version: u32,
    grants: Table,
}

#[derive(Debug, Clone)]
pub struct Grants {
    path: PathBuf,
    grants: Table,
}

impl Grants {
    pub fn load(home: &Home) -> Result<Self, Error> {
        let path = home
            .config
            .join(COMMAND_GRANT_FOLDER)
            .join(COMMAND_GRANT_FILE);
        let grants = read(&path)?;
        Ok(Self { path, grants })
    }

    pub fn path(&self) -> &Path {
        &self.path
    }

    pub fn of(&self, plugin: &Id, command: &Command) -> Grant {
        if !command.agent {
            return Grant::PersonOnly;
        }
        let stored = self
            .grants
            .get(plugin.as_str())
            .and_then(|commands| commands.get(&command.name));
        Grant::Agent(stored.copied().unwrap_or(Mode::Ask))
    }

    pub fn set(&mut self, plugin: &Id, command: &Command, mode: Mode) -> Result<(), Error> {
        if !command.agent {
            return Grant::PersonOnly.admit(plugin, &command.name);
        }
        let mut grants = read(&self.path)?;
        grants
            .entry(plugin.as_str().to_owned())
            .or_default()
            .insert(command.name.clone(), mode);
        let text = serde_json::to_string_pretty(&serde_json::json!({
            "version": COMMAND_GRANT_VERSION,
            "grants": &grants,
        }))
        .map_err(|source| at(&self.path)(Error::CommandGrant(source)))?;
        if let Some(dir) = self.path.parent() {
            fs::create_dir_all(dir).map_err(io_at(dir))?;
        }
        write_whole(&self.path, format!("{text}\n").as_bytes())?;
        self.grants = grants;
        Ok(())
    }
}

fn read(path: &Path) -> Result<Table, Error> {
    let text = match fs::read_to_string(path) {
        Ok(text) => text,
        Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(Table::new()),
        Err(source) => return Err(io_at(path)(source)),
    };
    let file: File =
        serde_json::from_str(&text).map_err(|source| at(path)(Error::CommandGrant(source)))?;
    if file.version != COMMAND_GRANT_VERSION {
        return Err(at(path)(Error::CommandGrantVersion(file.version)));
    }
    Ok(file.grants)
}
