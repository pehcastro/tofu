use std::fmt;
use std::fs::File;
use std::io::{self, BufWriter, Write};
use std::path::Path;

use gpui::Window;

use crate::capture::{Capture, CaptureError, Shot};
use crate::limits::GRAB_WAIT;

#[derive(Debug)]
pub enum GrabError {
    Capture(CaptureError),
    NoFrame { dropped: usize },
    Write(io::Error),
}

impl fmt::Display for GrabError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Capture(error) => error.fmt(f),
            Self::NoFrame { dropped } => write!(
                f,
                "no frame arrived within {} ms ({dropped} over the byte limit)",
                GRAB_WAIT.as_millis()
            ),
            Self::Write(error) => write!(f, "the PNG did not write: {error}"),
        }
    }
}

pub fn grab(window: &Window, path: &Path) -> Result<(u32, u32), GrabError> {
    let capture = Capture::start(window).map_err(GrabError::Capture)?;
    capture.wait_for_shot(GRAB_WAIT);
    let (shots, stopped) = capture.stop();
    if let Some(error) = stopped {
        return Err(GrabError::Capture(error));
    }
    let shot = shots
        .taken
        .into_iter()
        .max_by_key(|shot| shot.at)
        .ok_or(GrabError::NoFrame {
            dropped: shots.dropped,
        })?;
    write_png(path, &shot, &mut Vec::new()).map_err(GrabError::Write)
}

pub(crate) fn write_png(path: &Path, shot: &Shot, rgba: &mut Vec<u8>) -> io::Result<(u32, u32)> {
    rgba.clear();
    rgba.extend(
        shot.bgra
            .as_chunks::<4>()
            .0
            .iter()
            .flat_map(|&[b, g, r, _]| [r, g, b]),
    );
    let width = u32::try_from(shot.width).map_err(io::Error::other)?;
    let height = u32::try_from(shot.height).map_err(io::Error::other)?;
    let mut file = BufWriter::new(File::create(path)?);
    let mut encoder = png::Encoder::new(&mut file, width, height);
    encoder.set_color(png::ColorType::Rgb);
    encoder.set_depth(png::BitDepth::Eight);
    encoder.set_compression(png::Compression::Fast);
    let mut writer = encoder.write_header().map_err(io::Error::other)?;
    writer.write_image_data(rgba).map_err(io::Error::other)?;
    writer.finish().map_err(io::Error::other)?;
    file.flush()?;
    Ok((width, height))
}
