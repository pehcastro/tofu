use std::collections::{BTreeMap, BTreeSet};
use std::fmt;

use serde::Deserialize;

use crate::Error;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Api {
    V0_1_0,
}

impl Api {
    fn parse(text: &str) -> Result<Self, Error> {
        match text {
            "desk@0.1" => Ok(Api::V0_1_0),
            _ => Err(Error::Api(text.to_owned())),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum Capability {
    FsRead,
    FsWrite,
    Net,
    Tofu,
    Clipboard,
    Notify,
}

impl Capability {
    pub fn parse(text: &str) -> Result<Self, Error> {
        match text {
            "fs.read" => Ok(Capability::FsRead),
            "fs.write" => Ok(Capability::FsWrite),
            "net" => Ok(Capability::Net),
            "tofu" => Ok(Capability::Tofu),
            "clipboard" => Ok(Capability::Clipboard),
            "notify" => Ok(Capability::Notify),
            _ => Err(Error::Capability(text.to_owned())),
        }
    }

    pub fn name(self) -> &'static str {
        match self {
            Capability::FsRead => "fs.read",
            Capability::FsWrite => "fs.write",
            Capability::Net => "net",
            Capability::Tofu => "tofu",
            Capability::Clipboard => "clipboard",
            Capability::Notify => "notify",
        }
    }
}

impl fmt::Display for Capability {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.name())
    }
}

pub const MAX_ID_LEN: usize = 64;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum IdRule {
    Shape,
    Length,
    Device(String),
}

impl fmt::Display for IdRule {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            IdRule::Shape => f.write_str(
                "an id is publisher.name, one dot, each part starting with a lowercase letter and holding only lowercase letters, digits and hyphens",
            ),
            IdRule::Length => write!(f, "an id is at most {MAX_ID_LEN} characters"),
            IdRule::Device(part) => write!(
                f,
                "{part} is a Windows device name, which no id part may be"
            ),
        }
    }
}

fn is_device(part: &str) -> bool {
    let numbered = |prefix: &str| {
        part.strip_prefix(prefix)
            .is_some_and(|n| n.len() == 1 && n.bytes().all(|b| (b'1'..=b'9').contains(&b)))
    };
    matches!(part, "con" | "prn" | "aux" | "nul") || numbered("com") || numbered("lpt")
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord)]
pub struct Id(String);

impl Id {
    pub fn parse(text: &str) -> Result<Self, Error> {
        let refuse = |rule| Err(Error::Id(text.to_owned(), rule));
        if text.len() > MAX_ID_LEN {
            return refuse(IdRule::Length);
        }
        let Some((publisher, name)) = text.split_once('.') else {
            return refuse(IdRule::Shape);
        };
        for part in [publisher, name] {
            let shaped = part.bytes().next().is_some_and(|b| b.is_ascii_lowercase())
                && part
                    .bytes()
                    .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-');
            if !shaped {
                return refuse(IdRule::Shape);
            }
            if is_device(part) {
                return refuse(IdRule::Device(part.to_owned()));
            }
        }
        Ok(Id(text.to_owned()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for Id {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Command {
    pub name: String,
    pub description: String,
    #[serde(default)]
    pub agent: bool,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Contributes {
    #[serde(default)]
    pub tile: bool,
    pub skill: Option<String>,
    #[serde(default)]
    pub commands: Vec<Command>,
}

#[derive(Debug, Clone)]
pub struct Manifest {
    pub id: Id,
    pub name: String,
    pub icon: String,
    pub version: String,
    pub api: Api,
    pub tofu: String,
    pub capabilities: BTreeMap<Capability, Vec<String>>,
    pub contributes: Contributes,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Raw {
    id: String,
    name: String,
    icon: String,
    version: String,
    api: String,
    tofu: String,
    #[serde(default)]
    capabilities: toml::Table,
    #[serde(default)]
    contributes: Contributes,
}

impl Manifest {
    pub fn parse(text: &str) -> Result<Self, Error> {
        let raw: Raw = toml::from_str(text).map_err(Error::Manifest)?;
        let mut capabilities = BTreeMap::new();
        for (key, value) in raw.capabilities {
            match value {
                toml::Value::Table(inner) => {
                    for (sub, value) in inner {
                        declare(&mut capabilities, &format!("{key}.{sub}"), value)?;
                    }
                }
                value => declare(&mut capabilities, &key, value)?,
            }
        }
        Ok(Self {
            id: Id::parse(&raw.id)?,
            name: raw.name,
            icon: raw.icon,
            version: raw.version,
            api: Api::parse(&raw.api)?,
            tofu: raw.tofu,
            capabilities,
            contributes: raw.contributes,
        })
    }
}

fn declare(
    into: &mut BTreeMap<Capability, Vec<String>>,
    key: &str,
    value: toml::Value,
) -> Result<(), Error> {
    let capability = Capability::parse(key)?;
    let bad = || Error::CapabilityValue(key.to_owned());
    match value {
        toml::Value::Boolean(false) => {}
        toml::Value::Boolean(true) => {
            into.insert(capability, Vec::new());
        }
        toml::Value::Array(items) => {
            let targets = items
                .into_iter()
                .map(|v| match v {
                    toml::Value::String(s) => Ok(s),
                    _ => Err(bad()),
                })
                .collect::<Result<_, _>>()?;
            into.insert(capability, targets);
        }
        _ => return Err(bad()),
    }
    Ok(())
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct RawGrant {
    granted: Vec<String>,
}

pub fn parse_grant(text: &str) -> Result<BTreeSet<Capability>, Error> {
    let raw: RawGrant = serde_json::from_str(text).map_err(Error::Grant)?;
    raw.granted.iter().map(|c| Capability::parse(c)).collect()
}
