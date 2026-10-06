use std::time::Duration;

pub const TOAST_LIFETIME: Duration = Duration::from_millis(4200);
pub const TERMINAL_SCROLLBACK_LINES: usize = 10_000;
pub const BRIDGE_EVENT_QUEUE: usize = 1024;
pub const TOFU_LOG_LINES: usize = 200;
pub const TOFU_STOP_GRACE: Duration = Duration::from_secs(5);
pub const TOFU_STOP_POLL: Duration = Duration::from_millis(20);
