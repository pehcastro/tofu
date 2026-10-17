use std::fs;
use std::io::ErrorKind;
use std::path::{Path, PathBuf};

use desk_core::home::Home;
use serde_json::{Map, Value};

use crate::plugins::{self, ENABLED_KEY};

pub const VIM: &str = "editor.vim";
pub const GUIDES: &str = "editor.guides";
const FILE: &str = "settings.json";

pub struct Stored {
    path: PathBuf,
    values: Map<String, Value>,
}

impl Stored {
    pub fn load() -> Result<Self, String> {
        let home = Home::find().map_err(|error| error.to_string())?;
        let path = home.config.join(FILE);
        let values = read(&path)?;
        Ok(Stored { path, values })
    }

    pub fn path(&self) -> &Path {
        &self.path
    }

    pub fn switch(&self, key: &str) -> bool {
        self.values.get(key) == Some(&Value::Bool(true))
    }

    pub fn plugin_off(&self, id: &str) -> bool {
        plugins::switched_off(&self.values, id)
    }

    pub fn set_switch(&mut self, key: &str, on: bool) -> Result<(), String> {
        self.write(|values| {
            values.insert(key.to_owned(), Value::Bool(on));
            Ok(())
        })
    }

    pub fn set_plugin(&mut self, id: &str, on: bool) -> Result<(), String> {
        self.write(|values| {
            let enabled = values
                .entry(ENABLED_KEY)
                .or_insert_with(|| Value::Object(Map::new()));
            let Value::Object(enabled) = enabled else {
                return Err(format!("{ENABLED_KEY} holds {enabled}, not a map"));
            };
            enabled.insert(id.to_owned(), Value::Bool(on));
            Ok(())
        })
    }

    fn write(
        &mut self,
        change: impl FnOnce(&mut Map<String, Value>) -> Result<(), String>,
    ) -> Result<(), String> {
        let shown = self.path.display();
        let mut values = read(&self.path)?;
        change(&mut values).map_err(|problem| format!("{shown}: {problem}"))?;
        let text = serde_json::to_string_pretty(&values).map_err(|e| format!("{shown}: {e}"))?;
        let partial = self.path.with_extension("partial");
        self.path
            .parent()
            .map_or(Ok(()), fs::create_dir_all)
            .and_then(|()| fs::write(&partial, format!("{text}\n")))
            .and_then(|()| fs::rename(&partial, &self.path))
            .map_err(|error| format!("{shown}: {error}"))?;
        self.values = values;
        Ok(())
    }
}

fn read(path: &Path) -> Result<Map<String, Value>, String> {
    match fs::read_to_string(path) {
        Ok(text) => serde_json::from_str(&text).map_err(|e| format!("{}: {e}", path.display())),
        Err(error) if error.kind() == ErrorKind::NotFound => Ok(Map::new()),
        Err(error) => Err(format!("{}: {error}", path.display())),
    }
}
