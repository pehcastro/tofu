use std::fmt;
use std::fs::File;
use std::io::{self, BufWriter, Write};
use std::path::Path;

use gpui::{Bounds, DevicePixels, Window};

use crate::capture::{Capture, CaptureError, Shot};
use crate::limits::GRAB_WAIT;

#[derive(Debug)]
pub enum GrabError {
    Capture(CaptureError),
    NoFrame { dropped: usize },
    Write(io::Error),
    EmptyRegion,
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
            Self::EmptyRegion => write!(f, "the region lies outside the captured frame"),
        }
    }
}

pub fn grab(window: &Window, path: &Path) -> Result<(u32, u32), GrabError> {
    write_png(path, &latest(window)?, &mut Vec::new()).map_err(GrabError::Write)
}

pub fn grab_region(
    window: &Window,
    path: &Path,
    region: Bounds<DevicePixels>,
    max_px: u32,
) -> Result<(u32, u32), GrabError> {
    let shot = latest(window)?;
    let start = |at: DevicePixels, limit: usize| usize::try_from(at.0).unwrap_or(0).min(limit);
    let (x0, y0) = (
        start(region.origin.x, shot.width),
        start(region.origin.y, shot.height),
    );
    let x1 = start(region.origin.x + region.size.width, shot.width);
    let y1 = start(region.origin.y + region.size.height, shot.height);
    let (w, h) = (x1.saturating_sub(x0), y1.saturating_sub(y0));
    if w == 0 || h == 0 {
        return Err(GrabError::EmptyRegion);
    }
    let longest = w.max(h);
    let cap = usize::try_from(max_px)
        .unwrap_or(usize::MAX)
        .clamp(1, longest);
    let (out_w, out_h) = ((w * cap).div_ceil(longest), (h * cap).div_ceil(longest));
    let mut bgra = Vec::with_capacity(out_w * out_h * 4);
    for oy in 0..out_h {
        let (sy0, sy1) = (
            oy * h / out_h,
            ((oy + 1) * h / out_h).max(oy * h / out_h + 1),
        );
        for ox in 0..out_w {
            let (sx0, sx1) = (
                ox * w / out_w,
                ((ox + 1) * w / out_w).max(ox * w / out_w + 1),
            );
            let mut sum = [0usize; 4];
            for sy in sy0..sy1 {
                let row = (y0 + sy) * shot.width;
                for sx in sx0..sx1 {
                    let at = (row + x0 + sx) * 4;
                    for (total, byte) in sum
                        .iter_mut()
                        .zip(shot.bgra.get(at..at + 4).unwrap_or(&[0; 4]))
                    {
                        *total += usize::from(*byte);
                    }
                }
            }
            let count = (sy1 - sy0) * (sx1 - sx0);
            bgra.extend(sum.map(|total| u8::try_from(total / count).unwrap_or(u8::MAX)));
        }
    }
    let scaled = Shot {
        at: shot.at,
        width: out_w,
        height: out_h,
        bgra,
    };
    write_png(path, &scaled, &mut Vec::new()).map_err(GrabError::Write)
}

fn latest(window: &Window) -> Result<Shot, GrabError> {
    let capture = Capture::start(window).map_err(GrabError::Capture)?;
    capture.wait_for_shot(GRAB_WAIT);
    let (shots, stopped) = capture.stop();
    if let Some(error) = stopped {
        return Err(GrabError::Capture(error));
    }
    shots
        .taken
        .into_iter()
        .max_by_key(|shot| shot.at)
        .ok_or(GrabError::NoFrame {
            dropped: shots.dropped,
        })
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
