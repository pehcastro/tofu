use std::time::Duration;

use gpui::{SpringConfig, millis};

use crate::cubic_bezier;

pub const HOVER: SpringConfig = SpringConfig::new(600.0, 49.0, 1.0);
pub const PRESS: SpringConfig = SpringConfig::new(550.0, 30.0, 1.0);
pub const TOGGLE: SpringConfig = SpringConfig::new(600.0, 25.0, 1.0);
pub const PANEL: SpringConfig = SpringConfig::new(400.0, 25.0, 1.0);

pub const EASE_OUT: fn(f32) -> f32 = ease_out;

pub const HOVER_MS: Duration = millis(150);
pub const GLIDE_MS: Duration = millis(220);
pub const PRESS_MS: Duration = millis(100);
pub const TOGGLE_MS: Duration = millis(200);
pub const PANEL_IN_MS: Duration = millis(200);
pub const PANEL_OUT_MS: Duration = millis(140);

fn ease_out(t: f32) -> f32 {
    cubic_bezier((0.23, 1.0, 0.32, 1.0), t)
}
