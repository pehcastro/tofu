use std::fmt;

use serde_json::Value as Json;

use crate::protocol::{SettingsReport, VerbResult};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Source {
    Default,
    Global,
    Project,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Scope {
    Global,
    Project,
}

impl Scope {
    pub fn wire(self) -> &'static str {
        match self {
            Scope::Global => "global",
            Scope::Project => "project",
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Value {
    Switch(bool),
    Number(i64),
    Text(String),
}

impl Value {
    pub fn wire(&self) -> String {
        match self {
            Value::Switch(on) => on.to_string(),
            Value::Number(number) => number.to_string(),
            Value::Text(text) => text.clone(),
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Setting {
    pub key: String,
    pub value: Value,
    pub source: Source,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Category {
    pub name: String,
    pub settings: Vec<Setting>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Settings {
    pub global_file: String,
    pub project_file: String,
    pub categories: Vec<Category>,
}

impl Settings {
    pub fn file(&self, scope: Scope) -> &str {
        match scope {
            Scope::Global => &self.global_file,
            Scope::Project => &self.project_file,
        }
    }

    pub fn get(&self, key: &str) -> Option<&Setting> {
        self.categories
            .iter()
            .flat_map(|category| &category.settings)
            .find(|setting| setting.key == key)
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum SettingsError {
    Source { key: String, source: String },
    Value { key: String, value: String },
}

impl fmt::Display for SettingsError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            SettingsError::Source { key, source } => write!(
                f,
                "tofu says {key} comes from {source}, which is not default, global or project"
            ),
            SettingsError::Value { key, value } => write!(
                f,
                "tofu sent {value} for {key}, which is not a switch, a number or text"
            ),
        }
    }
}

impl std::error::Error for SettingsError {}

impl TryFrom<SettingsReport> for Settings {
    type Error = SettingsError;

    fn try_from(report: SettingsReport) -> Result<Self, Self::Error> {
        let mut categories: Vec<Category> = Vec::new();
        for setting in report.settings {
            let source = match setting.source.as_str() {
                "default" => Source::Default,
                "global" => Source::Global,
                "project" => Source::Project,
                _ => {
                    return Err(SettingsError::Source {
                        key: setting.key,
                        source: setting.source,
                    });
                }
            };
            let value = match setting.value {
                Json::Bool(on) => Value::Switch(on),
                Json::Number(number) => number
                    .as_i64()
                    .map_or_else(|| Value::Text(number.to_string()), Value::Number),
                Json::String(text) => Value::Text(text),
                other @ (Json::Null | Json::Array(_) | Json::Object(_)) => {
                    return Err(SettingsError::Value {
                        key: setting.key,
                        value: other.to_string(),
                    });
                }
            };
            let parsed = Setting {
                key: setting.key,
                value,
                source,
            };
            match categories
                .iter_mut()
                .find(|category| category.name == setting.category)
            {
                Some(category) => category.settings.push(parsed),
                None => categories.push(Category {
                    name: setting.category,
                    settings: vec![parsed],
                }),
            }
        }
        Ok(Settings {
            global_file: report.global_file,
            project_file: report.project_file,
            categories,
        })
    }
}

pub fn refusal(result: &VerbResult) -> Option<String> {
    if result.ok {
        return None;
    }
    let said: Vec<&str> = result
        .problems
        .iter()
        .map(|problem| problem.what.as_str())
        .collect();
    Some(if said.is_empty() {
        format!("tofu refused {} and said why nowhere", result.verb)
    } else {
        said.join(". ")
    })
}
