use std::collections::HashMap;
use std::fs::File;
use std::io::{BufWriter, Write};
use std::path::{Component, Path, PathBuf};

use desk_ui::effects::Source;
use gpui::{App, Global, Window};
use image::DynamicImage;
use image::codecs::jpeg::JpegEncoder;
use image::imageops::FilterType;

use super::effects::BUILT_IN;
use super::remembered;

const QUALITY: u8 = 90;
const THUMB: Fit = Fit {
    width: 184,
    height: 112,
};
const NO_DISPLAY: Fit = Fit {
    width: 3840,
    height: 2160,
};
const KEPT_AS: &str = "jpg";
const THUMB_AS: &str = "thumb.jpg";

#[derive(Clone, Copy, Debug)]
pub struct Fit {
    width: u32,
    height: u32,
}

impl Fit {
    pub fn largest(window: &Window, cx: &App) -> Fit {
        let scale = window.scale_factor();
        cx.displays()
            .iter()
            .map(|display| display.bounds().size)
            .map(|size| Fit {
                width: (f32::from(size.width) * scale).round() as u32,
                height: (f32::from(size.height) * scale).round() as u32,
            })
            .max_by_key(|fit| u64::from(fit.width) * u64::from(fit.height))
            .unwrap_or(NO_DISPLAY)
    }
}

#[derive(Default)]
struct Read(HashMap<String, Source>);

impl Global for Read {}

pub fn known(name: &str, cx: &App) -> Option<Source> {
    BUILT_IN
        .into_iter()
        .find(|source| source.name == name)
        .or_else(|| {
            cx.try_global::<Read>()
                .and_then(|read| read.0.get(name).copied())
        })
}

pub fn remember(asked: &str, source: Source, cx: &mut App) {
    let read = &mut cx.default_global::<Read>().0;
    read.insert(asked.to_owned(), source);
    read.insert(source.name.to_owned(), source);
}

pub fn thumb(name: &str) -> String {
    let stem = Path::new(name)
        .file_stem()
        .map(|stem| stem.to_string_lossy().into_owned())
        .unwrap_or_default();
    format!("{stem}.{THUMB_AS}")
}

pub fn is_thumb(path: &Path) -> bool {
    path.to_string_lossy().ends_with(THUMB_AS)
}

fn kept_name(source: &Path) -> Result<String, String> {
    let stem = source
        .file_stem()
        .ok_or_else(|| format!("{} has no file name", source.display()))?;
    Ok(format!("{}.{KEPT_AS}", stem.to_string_lossy()))
}

pub fn keep(source: &Path, fit: Fit) -> Result<String, String> {
    let folder = remembered::kept_images()?;
    let name = kept_name(source)?;
    let target = folder.join(&name);
    if let (Ok(from), Ok(to)) = (source.canonicalize(), target.canonicalize())
        && from == to
    {
        return Ok(name);
    }
    let picture = image::open(source).map_err(|error| format!("{}: {error}", source.display()))?;
    std::fs::create_dir_all(&folder).map_err(|error| format!("{}: {error}", folder.display()))?;
    let fitted = if picture.width() > fit.width || picture.height() > fit.height {
        picture.resize(fit.width, fit.height, FilterType::Lanczos3)
    } else {
        picture
    };
    write_jpeg(&fitted, &target)?;
    let small = fitted.resize_to_fill(THUMB.width, THUMB.height, FilterType::Triangle);
    write_jpeg(&small, &folder.join(thumb(&name)))?;
    eprintln!(
        "desk: intro kept {} as {} at {}x{}",
        source.display(),
        target.display(),
        fitted.width(),
        fitted.height()
    );
    Ok(name)
}

fn write_jpeg(picture: &DynamicImage, path: &Path) -> Result<(), String> {
    let failed = |error: std::io::Error| format!("{}: {error}", path.display());
    let mut writer = BufWriter::new(File::create(path).map_err(failed)?);
    JpegEncoder::new_with_quality(&mut writer, QUALITY)
        .encode_image(&picture.to_rgb8())
        .map_err(|error| format!("{}: {error}", path.display()))?;
    writer.flush().map_err(failed)
}

fn file_name(name: &str) -> bool {
    let mut parts = Path::new(name).components();
    matches!(
        (parts.next(), parts.next()),
        (Some(Component::Normal(_)), None)
    )
}

fn original(name: &str) -> Result<PathBuf, String> {
    if file_name(name) {
        return Ok(remembered::first_kept_images()?.join(name));
    }
    let path = PathBuf::from(name);
    if path.is_absolute() {
        return Ok(path);
    }
    Err(format!(
        "{name} is neither a kept file name nor a full path"
    ))
}

pub fn read(name: &str, fit: Fit) -> Result<Source, String> {
    if let Some(built) = BUILT_IN.into_iter().find(|source| source.name == name) {
        return Ok(built);
    }
    let folder = remembered::kept_images()?;
    let kept = if file_name(name) && folder.join(name).is_file() {
        name.to_owned()
    } else {
        let converted = keep(&original(name)?, fit)?;
        remembered::update(|remembered| {
            if remembered.image.as_deref() == Some(name) {
                remembered.image = Some(converted.clone());
            }
        })?;
        converted
    };
    let path = folder.join(&kept);
    let bytes = std::fs::read(&path).map_err(|error| format!("{}: {error}", path.display()))?;
    Ok(Source {
        name: Box::leak(kept.into_boxed_str()),
        bytes: Box::leak(bytes.into_boxed_slice()),
    })
}

pub fn gather(fit: Fit) -> Result<Vec<String>, String> {
    let first = remembered::first_kept_images()?;
    for path in pictures_in(&first)? {
        let converted = kept_name(&path)?;
        if !remembered::kept_images()?.join(&converted).is_file()
            && let Err(error) = keep(&path, fit)
        {
            eprintln!("desk: intro could not keep {}: {error}", path.display());
        }
    }
    let mut names: Vec<String> = pictures_in(&remembered::kept_images()?)?
        .iter()
        .filter(|path| !is_thumb(path))
        .filter_map(|path| path.file_name())
        .map(|name| name.to_string_lossy().into_owned())
        .collect();
    names.sort();
    Ok(names)
}

fn pictures_in(folder: &Path) -> Result<Vec<PathBuf>, String> {
    let entries = match std::fs::read_dir(folder) {
        Ok(entries) => entries,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(Vec::new()),
        Err(error) => return Err(format!("{}: {error}", folder.display())),
    };
    let mut pictures = Vec::new();
    for entry in entries {
        let path = entry
            .map_err(|error| format!("{}: {error}", folder.display()))?
            .path();
        if pickable(&path) {
            pictures.push(path);
        }
    }
    Ok(pictures)
}

pub fn pickable(path: &Path) -> bool {
    path.extension()
        .and_then(|extension| extension.to_str())
        .is_some_and(|extension| {
            ["png", "jpg", "jpeg", "webp"]
                .iter()
                .any(|known| extension.eq_ignore_ascii_case(known))
        })
}
