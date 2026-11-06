use std::fs::{self, OpenOptions};
use std::io::{ErrorKind, Write};
use std::path::{Path, PathBuf};
use std::sync::Arc;

use desk_ui::theme::{ColorToken, Layer, Mode, Theme, build, build_mode, parse_layer};
use gpui::{Rgba, SharedString};
use serde_json::Value;

const BASE: &str = "tofu-glass";
const SWATCHES: [ColorToken; 4] = [
    ColorToken::SurfaceWindow,
    ColorToken::CardsOuterFill,
    ColorToken::StatusAccent,
    ColorToken::StatusLive,
];

pub struct Entry {
    pub file: String,
    pub label: SharedString,
    pub json: SharedString,
    pub look: Result<Look, SharedString>,
    pub own: bool,
}

pub struct Draft {
    file: String,
    json: Value,
    layers: Vec<Layer>,
    pub theme: Arc<Theme>,
}

impl Draft {
    pub fn open(dirs: &[PathBuf], entry: &Entry, mode: Mode) -> Result<Draft, String> {
        let layers = chain(dirs, &entry.file)?;
        let theme =
            build_mode(&layers, mode, &mut Vec::new()).map_err(|error| error.to_string())?;
        Ok(Draft {
            file: entry.file.clone(),
            json: object(&entry.file, &entry.json)?,
            layers,
            theme: Arc::new(theme),
        })
    }

    pub fn set(&mut self, path: &[&str], value: Value, mode: Mode) -> Result<(), String> {
        put(&self.file, &mut self.json, path, value)?;
        let text = serde_json::to_string(&self.json).map_err(|error| error.to_string())?;
        let top = parse_layer(&self.file, &text, &mut Vec::new())
            .map_err(|error| error.to_string())?
            .layer;
        let Some(first) = self.layers.first_mut() else {
            return Err(format!("{} has no layers to draw", self.file));
        };
        *first = top;
        let theme =
            build_mode(&self.layers, mode, &mut Vec::new()).map_err(|error| error.to_string())?;
        self.theme = Arc::new(theme);
        Ok(())
    }
}

pub struct Look {
    pub mode: &'static str,
    pub swatches: [Rgba; 4],
}

pub fn read(dirs: &[PathBuf]) -> Result<Vec<Entry>, String> {
    let mut found: Vec<Entry> = Vec::new();
    for (at, dir) in dirs.iter().enumerate() {
        read_one(dir, dirs, at == 0, &mut found)?;
    }
    found.sort_by(|left, right| left.label.cmp(&right.label));
    Ok(found)
}

fn read_one(dir: &Path, dirs: &[PathBuf], own: bool, found: &mut Vec<Entry>) -> Result<(), String> {
    let unlisted =
        |error: std::io::Error| format!("the theme screen cannot list {}: {error}", dir.display());
    for entry in fs::read_dir(dir).map_err(unlisted)? {
        let path = entry.map_err(unlisted)?.path();
        let Some(file) = path
            .extension()
            .filter(|extension| *extension == "json")
            .and_then(|_| path.file_stem())
            .and_then(|stem| stem.to_str())
        else {
            continue;
        };
        if found.iter().any(|known| known.file == file) {
            continue;
        }
        let json =
            fs::read_to_string(&path).map_err(|error| format!("{}: {error}", path.display()))?;
        let label = serde_json::from_str::<Value>(&json)
            .ok()
            .and_then(|theme| theme.get("name")?.as_str().map(str::to_owned))
            .unwrap_or_else(|| titled(file));
        let look = built(dirs, file).map(|theme| Look {
            mode: theme.mode().label(),
            swatches: SWATCHES.map(|token| theme.color(token)),
        });
        found.push(Entry {
            file: file.to_owned(),
            label: label.into(),
            json: json.into(),
            look: look.map_err(SharedString::from),
            own,
        });
    }
    Ok(())
}

fn locate(dirs: &[PathBuf], name: &str) -> PathBuf {
    let file = format!("{name}.json");
    dirs.iter()
        .map(|dir| dir.join(&file))
        .find(|path| path.is_file())
        .unwrap_or_else(|| PathBuf::from(file))
}

fn built(dirs: &[PathBuf], file: &str) -> Result<Theme, String> {
    build(&chain(dirs, file)?, &mut Vec::new()).map_err(|error| error.to_string())
}

fn chain(dirs: &[PathBuf], file: &str) -> Result<Vec<Layer>, String> {
    let mut problems = Vec::new();
    let mut layers: Vec<Layer> = Vec::new();
    let mut trail: Vec<String> = Vec::new();
    let mut next = Some(file.to_owned());
    while let Some(name) = next.take() {
        if trail.contains(&name) {
            return Err(format!(
                "{file} extends itself through {}",
                trail.join(", ")
            ));
        }
        let path = locate(dirs, &name);
        let text =
            fs::read_to_string(&path).map_err(|error| format!("{}: {error}", path.display()))?;
        let parsed = parse_layer(&name, &text, &mut problems).map_err(|error| error.to_string())?;
        layers.push(parsed.layer);
        trail.push(name);
        next = parsed.extends;
    }
    if !trail.iter().any(|name| name == BASE) {
        let path = locate(dirs, BASE);
        let text =
            fs::read_to_string(&path).map_err(|error| format!("{}: {error}", path.display()))?;
        layers.push(
            parse_layer(BASE, &text, &mut problems)
                .map_err(|error| error.to_string())?
                .layer,
        );
    }
    Ok(layers)
}

pub fn duplicate(dir: &Path, entry: &Entry) -> Result<String, String> {
    copy(dir, entry, |at| match at {
        1 => (
            format!("{}-copy", entry.file),
            format!("{} copy", entry.label),
        ),
        _ => (
            format!("{}-copy-{at}", entry.file),
            format!("{} copy {at}", entry.label),
        ),
    })
}

pub fn adopt(dir: &Path, entry: &Entry) -> Result<(), String> {
    match write_new(dir, &entry.file, &entry.json) {
        Err(error) if error.kind() == ErrorKind::AlreadyExists => Ok(()),
        written => written,
    }
    .map_err(|error| {
        format!(
            "{} cannot be saved to {}: {error}",
            entry.file,
            dir.display()
        )
    })
}

pub fn untitled(dir: &Path, base: &Entry) -> Result<String, String> {
    copy(dir, base, |at| {
        (format!("untitled-{at}"), format!("Untitled {at}"))
    })
}

fn object(file: &str, json: &str) -> Result<Value, String> {
    let theme: Value =
        serde_json::from_str(json).map_err(|error| format!("{file} is not JSON: {error}"))?;
    match theme {
        Value::Object(_) => Ok(theme),
        _ => Err(format!("{file} is not a JSON object")),
    }
}

fn pretty(theme: &Value) -> Result<String, String> {
    Ok(serde_json::to_string_pretty(theme).map_err(|error| error.to_string())? + "\n")
}

fn copy(
    dir: &Path,
    entry: &Entry,
    named: impl Fn(u32) -> (String, String),
) -> Result<String, String> {
    let mut theme = object(&entry.file, &entry.json)?;
    let mut at = 1u32;
    loop {
        let (file, name) = named(at);
        if let Value::Object(fields) = &mut theme {
            fields.insert("name".into(), Value::String(name));
        }
        match write_new(dir, &file, &pretty(&theme)?) {
            Err(error) if error.kind() == ErrorKind::AlreadyExists => {
                at = at.checked_add(1).ok_or("no free theme name is left")?;
            }
            written => {
                return written.map(|()| file).map_err(|error| {
                    format!("the theme cannot be written to {}: {error}", dir.display())
                });
            }
        }
    }
}

pub fn set(dir: &Path, file: &str, path: &[&str], value: Value) -> Result<SharedString, String> {
    let at = dir.join(format!("{file}.json"));
    let text = fs::read_to_string(&at).map_err(|error| format!("{}: {error}", at.display()))?;
    let mut theme = object(file, &text)?;
    put(file, &mut theme, path, value)?;
    let text = pretty(&theme)?;
    fs::write(&at, &text).map_err(|error| format!("{}: {error}", at.display()))?;
    Ok(text.into())
}

fn put(file: &str, theme: &mut Value, path: &[&str], value: Value) -> Result<(), String> {
    let mut node = theme;
    for key in path {
        let Value::Object(fields) = node else {
            return Err(format!("{file}: {} is not an object", path.join(".")));
        };
        node = fields
            .entry((*key).to_owned())
            .or_insert_with(|| Value::Object(Default::default()));
    }
    *node = value;
    Ok(())
}

pub fn delete(dir: &Path, file: &str) -> Result<(), String> {
    let at = dir.join(format!("{file}.json"));
    fs::remove_file(&at).map_err(|error| format!("{} cannot be deleted: {error}", at.display()))
}

pub fn rename(dir: &Path, file: &str, name: &str) -> Result<String, String> {
    let slug: String = name
        .trim()
        .to_lowercase()
        .chars()
        .map(|letter| match letter.is_alphanumeric() {
            true => letter,
            false => '-',
        })
        .collect();
    let slug = slug.trim_matches('-');
    if slug.is_empty() {
        return Err(format!("{name:?} has no letters to name a theme file with"));
    }
    set(dir, file, &["name"], Value::String(name.trim().to_owned()))?;
    if slug == file {
        return Ok(file.to_owned());
    }
    let to = dir.join(format!("{slug}.json"));
    if to.exists() {
        return Err(format!(
            "{} already holds a theme named {slug}",
            dir.display()
        ));
    }
    fs::rename(dir.join(format!("{file}.json")), &to)
        .map_err(|error| format!("{file} cannot be renamed to {slug}: {error}"))?;
    Ok(slug.to_owned())
}

pub fn import(dir: &Path, source: &Path) -> Result<String, String> {
    let file = source
        .file_stem()
        .and_then(|stem| stem.to_str())
        .filter(|_| {
            source
                .extension()
                .is_some_and(|extension| extension == "json")
        })
        .ok_or_else(|| format!("{} is not a .json theme file", source.display()))?
        .to_owned();
    let text =
        fs::read_to_string(source).map_err(|error| format!("{}: {error}", source.display()))?;
    parse_layer(&file, &text, &mut Vec::new()).map_err(|error| error.to_string())?;
    write_new(dir, &file, &text).map_err(|error| match error.kind() {
        ErrorKind::AlreadyExists => format!("{} already holds a theme named {file}", dir.display()),
        _ => format!("{file} cannot be imported into {}: {error}", dir.display()),
    })?;
    Ok(file)
}

fn write_new(dir: &Path, file: &str, text: &str) -> std::io::Result<()> {
    let mut out = OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(dir.join(format!("{file}.json")))?;
    out.write_all(text.as_bytes())?;
    out.flush()
}

fn titled(file: &str) -> String {
    file.split(['-', '_'])
        .filter_map(|word| {
            let mut letters = word.chars();
            let first = letters.next()?;
            Some(first.to_uppercase().chain(letters).collect::<String>())
        })
        .collect::<Vec<_>>()
        .join(" ")
}

pub fn hex(color: Rgba) -> String {
    let byte = |channel: f32| (channel.clamp(0.0, 1.0) * 255.0).round() as u8;
    format!(
        "#{:02x}{:02x}{:02x}{:02x}",
        byte(color.red),
        byte(color.green),
        byte(color.blue),
        byte(color.alpha)
    )
}
