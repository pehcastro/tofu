use std::path::Path;
use std::time::{Instant, SystemTime, UNIX_EPOCH};

use desk_control::ErrorCode;
use desk_control::params::{Capture, CaptureTarget};
use desk_control::replies::Captured;
use desk_control::transport::runtime_dir;
use desk_tiling::Rect;
use gpui::{AnyWindowHandle, App, Bounds, DevicePixels, point, size};

use super::change::{Answered, json};

pub fn answer(capture: &Capture, window: AnyWindowHandle, cx: &mut App) -> Answered {
    if let Some(CaptureTarget::Tile(_)) = capture.target {
        return Err((
            ErrorCode::BadParams,
            "capture takes the whole desk window; --target tile is not built yet".to_owned(),
        ));
    }
    let refused = |error: String| (ErrorCode::Refused, error);
    let folder = runtime_dir()
        .ok_or_else(|| refused("the desk has no home folder to write captures to".to_owned()))?
        .join("captures");
    std::fs::create_dir_all(&folder)
        .map_err(|error| refused(format!("{}: {error}", folder.display())))?;
    let stamp = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |since| since.as_millis());
    let path = folder.join(format!("capture-{}-{stamp}.png", std::process::id()));
    let started = Instant::now();
    let (w, h) = window
        .update(cx, |_, window, _| desk_perf::grab(window, &path))
        .map_err(|error| {
            (
                ErrorCode::DeskNotRunning,
                format!("the desk window is gone: {error:#}"),
            )
        })?
        .map_err(|error| refused(error.to_string()))?;
    eprintln!(
        "desk: control capture {w}x{h} in {} ms",
        started.elapsed().as_millis()
    );
    let bytes = std::fs::metadata(&path)
        .map_err(|error| refused(format!("{}: {error}", path.display())))?
        .len();
    json(&Captured {
        path: path.display().to_string(),
        w,
        h,
        bytes,
    })
}

pub fn region(
    window: AnyWindowHandle,
    path: &Path,
    rect: Rect,
    max_px: u32,
    cx: &mut App,
) -> Result<(u32, u32), (ErrorCode, String)> {
    window
        .update(cx, |_, window, _| {
            let scale = window.scale_factor();
            let device = |logical: f32| DevicePixels((logical * scale).round() as i32);
            let bounds = Bounds::new(
                point(device(rect.x), device(rect.y)),
                size(device(rect.w), device(rect.h)),
            );
            desk_perf::grab_region(window, path, bounds, max_px)
        })
        .map_err(|error| {
            (
                ErrorCode::DeskNotRunning,
                format!("the desk window is gone: {error:#}"),
            )
        })?
        .map_err(|error| (ErrorCode::Refused, error.to_string()))
}
