use crate::shells::desk_folder;
use std::fs;
use std::io::ErrorKind;
use std::path::PathBuf;

const FONT_FILE: &str = "terminal-font";
pub(crate) const DEFAULT: u8 = 14;
const SMALLEST: u8 = 8;
const LARGEST: u8 = 32;

fn font_file() -> Result<PathBuf, String> {
    desk_folder()
        .map(|folder| folder.join(FONT_FILE))
        .ok_or_else(|| "the desk has no config folder for its terminal font".to_owned())
}

pub(crate) fn clamped(size: u8) -> u8 {
    size.clamp(SMALLEST, LARGEST)
}

pub(crate) fn saved() -> u8 {
    let read = font_file().and_then(|path| match fs::read_to_string(&path) {
        Ok(text) => text
            .trim()
            .parse::<u8>()
            .map(Some)
            .map_err(|error| format!("{} holds no font size: {error}", path.display())),
        Err(error) if error.kind() == ErrorKind::NotFound => Ok(None),
        Err(error) => Err(format!("could not read {}: {error}", path.display())),
    });
    match read {
        Ok(Some(size)) => clamped(size),
        Ok(None) => DEFAULT,
        Err(error) => {
            eprintln!("terminal: {error}, so the font is {DEFAULT} px");
            DEFAULT
        }
    }
}

pub(crate) fn save(size: u8) -> Result<(), String> {
    let path = font_file()?;
    path.parent()
        .map_or(Ok(()), fs::create_dir_all)
        .and_then(|()| fs::write(&path, format!("{size}\n")))
        .map_err(|error| format!("could not write {}: {error}", path.display()))
}
