use std::time::Duration;

use gpui::{Animation, App};

mod glide;
mod presence;
pub mod tokens;

pub use glide::{Glide, GlideKind};
pub use presence::{Phase, Presence};

const BEZIER_STEPS: u32 = 24;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Ease {
    Standard,
    Out,
    InOut,
    Linear,
}

impl Ease {
    pub fn apply(self, t: f32) -> f32 {
        match self {
            Ease::Standard => cubic_bezier((0.25, 0.1, 0.25, 1.0), t),
            Ease::Out => cubic_bezier((0.0, 0.0, 0.2, 1.0), t),
            Ease::InOut => cubic_bezier((0.4, 0.0, 0.2, 1.0), t),
            Ease::Linear => t.clamp(0.0, 1.0),
        }
    }
}

fn cubic_bezier((x1, y1, x2, y2): (f32, f32, f32, f32), t: f32) -> f32 {
    let x = t.clamp(0.0, 1.0);
    let axis = |a: f32, b: f32, s: f32| {
        ((1.0 - 3.0 * b + 3.0 * a) * s + 3.0 * b - 6.0 * a) * s * s + 3.0 * a * s
    };
    let (mut lo, mut hi) = (0.0, 1.0);
    for _ in 0..BEZIER_STEPS {
        let mid = (lo + hi) / 2.0;
        if axis(x1, x2, mid) < x {
            lo = mid;
        } else {
            hi = mid;
        }
    }
    axis(y1, y2, (lo + hi) / 2.0).clamp(0.0, 1.0)
}

pub fn reduced_motion(cx: &App) -> bool {
    cx.reduce_motion()
}

pub fn set_reduced_motion(on: bool, cx: &mut App) {
    cx.set_reduce_motion(on);
}

#[cfg(target_os = "windows")]
pub fn system_reduced_motion() -> bool {
    windows::UI::ViewManagement::UISettings::new()
        .and_then(|settings| settings.AnimationsEnabled())
        .is_ok_and(|enabled| !enabled)
}

#[cfg(not(target_os = "windows"))]
pub fn system_reduced_motion() -> bool {
    false
}

pub fn spin(period: Duration) -> Animation {
    Animation::new(period).repeat_synced()
}
