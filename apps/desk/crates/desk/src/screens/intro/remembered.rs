use std::io::ErrorKind;
use std::path::PathBuf;

use desk_ui::effects::ascii::Ascii;
use serde_json::{Value, json};

use super::tuning::Tuning;

const FILE: &str = "intro.json";
const KEPT_IMAGES: &str = "intro";
const BACKGROUNDS: &str = "bg";

#[derive(Clone, Copy, PartialEq, Eq, Hash, Debug)]
pub enum Effect {
    Sort,
    Dither,
    Fade,
    Duotone,
    Halftone,
    Ascii,
    Grain,
    Scan,
    Glow,
    Crt,
}

impl Effect {
    pub const ALL: [Effect; 10] = [
        Effect::Sort,
        Effect::Dither,
        Effect::Fade,
        Effect::Duotone,
        Effect::Halftone,
        Effect::Ascii,
        Effect::Grain,
        Effect::Scan,
        Effect::Glow,
        Effect::Crt,
    ];

    pub fn key(self) -> &'static str {
        match self {
            Effect::Sort => "pixel_sort",
            Effect::Ascii => "ascii",
            Effect::Crt => "crt",
            Effect::Dither => "dither",
            Effect::Fade => "fade",
            Effect::Duotone => "duotone",
            Effect::Halftone => "halftone",
            Effect::Grain => "grain",
            Effect::Scan => "scan",
            Effect::Glow => "glow",
        }
    }

    fn on_at_start(self) -> bool {
        matches!(self, Effect::Dither | Effect::Scan)
    }
}

#[derive(Clone)]
pub struct Remembered {
    pub project: Option<PathBuf>,
    pub session: Option<String>,
    pub session_name: Option<String>,
    pub model: Option<String>,
    pub image: Option<String>,
    pub effects: Vec<Effect>,
    pub motion: bool,
    pub ascii: Ascii,
    pub tuning: Tuning,
}

impl Default for Remembered {
    fn default() -> Self {
        Remembered {
            project: None,
            session: None,
            session_name: None,
            model: None,
            image: None,
            effects: Effect::ALL
                .into_iter()
                .filter(|effect| effect.on_at_start())
                .collect(),
            motion: true,
            ascii: Ascii::default(),
            tuning: Tuning::default(),
        }
    }
}

fn root() -> Result<desk_core::home::Home, String> {
    desk_core::home::Home::find().map_err(|error| error.to_string())
}

pub fn kept_images() -> Result<PathBuf, String> {
    let home = root()?;
    home.config
        .parent()
        .map(|desk| desk.join(KEPT_IMAGES).join(BACKGROUNDS))
        .ok_or_else(|| format!("{} has no desk folder above it", home.config.display()))
}

pub fn first_kept_images() -> Result<PathBuf, String> {
    let home = root()?;
    home.config
        .parent()
        .and_then(|desk| desk.parent())
        .map(|tofu| tofu.join(KEPT_IMAGES))
        .ok_or_else(|| format!("{} has no tofu folder above it", home.config.display()))
}

pub fn read() -> Result<Remembered, String> {
    let path = root()?.config.join(FILE);
    let text = match std::fs::read_to_string(&path) {
        Ok(text) => text,
        Err(error) if error.kind() == ErrorKind::NotFound => return Ok(Remembered::default()),
        Err(error) => return Err(format!("{}: {error}", path.display())),
    };
    let value: Value =
        serde_json::from_str(&text).map_err(|error| format!("{}: {error}", path.display()))?;
    let field = |key: &str| value.get(key).and_then(Value::as_str).map(str::to_owned);
    let fallback = Remembered::default();
    let effects = match value.get("effects").and_then(Value::as_array) {
        None => fallback.effects,
        Some(keys) => Effect::ALL
            .into_iter()
            .filter(|effect| keys.iter().any(|key| key.as_str() == Some(effect.key())))
            .collect(),
    };
    let ascii = match value.get("ascii") {
        None => fallback.ascii,
        Some(ascii) => {
            Ascii::from_json(ascii).map_err(|error| format!("{}: {error}", path.display()))?
        }
    };
    let tuning = match value.get("tuning") {
        None => fallback.tuning,
        Some(tuning) => {
            Tuning::from_json(tuning).map_err(|error| format!("{}: {error}", path.display()))?
        }
    };
    Ok(Remembered {
        project: field("project").map(PathBuf::from),
        session: field("session"),
        session_name: field("session_name"),
        model: field("model"),
        image: field("image"),
        effects,
        motion: value
            .get("motion")
            .and_then(Value::as_bool)
            .unwrap_or(fallback.motion),
        ascii,
        tuning,
    })
}

pub fn update(change: impl FnOnce(&mut Remembered)) -> Result<(), String> {
    let mut kept = read()?;
    change(&mut kept);
    let path = root()?.config.join(FILE);
    let failed = |error: std::io::Error| format!("{}: {error}", path.display());
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent).map_err(failed)?;
    }
    let effects: Vec<&str> = kept.effects.iter().map(|effect| effect.key()).collect();
    let text = serde_json::to_string_pretty(&json!({
        "project": kept.project,
        "session": kept.session,
        "session_name": kept.session_name,
        "model": kept.model,
        "image": kept.image,
        "effects": effects,
        "motion": kept.motion,
        "ascii": kept.ascii.to_json(),
        "tuning": kept.tuning.to_json(),
    }))
    .map_err(|error| error.to_string())?;
    std::fs::write(&path, text).map_err(failed)
}
