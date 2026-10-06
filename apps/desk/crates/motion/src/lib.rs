use std::time::{Duration, Instant};

use gpui::Animation;

const SPIN_MAX_FPS: f32 = 30.0;
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
        let x = t.clamp(0.0, 1.0);
        let (x1, y1, x2, y2) = match self {
            Ease::Standard => (0.25, 0.1, 0.25, 1.0),
            Ease::Out => (0.0, 0.0, 0.2, 1.0),
            Ease::InOut => (0.4, 0.0, 0.2, 1.0),
            Ease::Linear => return x,
        };
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
}

#[cfg(target_os = "windows")]
pub fn reduced_motion() -> bool {
    windows::UI::ViewManagement::UISettings::new()
        .and_then(|settings| settings.AnimationsEnabled())
        .is_ok_and(|enabled| !enabled)
}

#[cfg(not(target_os = "windows"))]
pub fn reduced_motion() -> bool {
    false
}

pub fn spin(period: Duration) -> Animation {
    Animation::new(period).repeat().with_max_fps(SPIN_MAX_FPS)
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Phase {
    Closed,
    Opening,
    Open,
    Closing,
}

#[derive(Clone, Copy, Debug)]
pub struct Presence {
    enter: Duration,
    exit: Duration,
    open: bool,
    from: f32,
    since: Option<Instant>,
}

impl Presence {
    pub fn new(enter: Duration, exit: Duration) -> Self {
        Self {
            enter,
            exit,
            open: false,
            from: 0.0,
            since: None,
        }
    }

    pub fn set_open(&mut self, open: bool, now: Instant) {
        if open != self.open {
            self.from = self.progress(now);
            self.open = open;
            self.since = Some(now);
        }
    }

    pub fn phase(&self, now: Instant) -> Phase {
        match (self.open, self.fraction(now) >= 1.0) {
            (true, false) => Phase::Opening,
            (true, true) => Phase::Open,
            (false, false) => Phase::Closing,
            (false, true) => Phase::Closed,
        }
    }

    pub fn progress(&self, now: Instant) -> f32 {
        let target = if self.open { 1.0 } else { 0.0 };
        self.from + (target - self.from) * Ease::Out.apply(self.fraction(now))
    }

    pub fn mounted(&self, now: Instant) -> bool {
        self.phase(now) != Phase::Closed
    }

    fn fraction(&self, now: Instant) -> f32 {
        let Some(since) = self.since else { return 1.0 };
        let (full, remaining) = if self.open {
            (self.enter, 1.0 - self.from)
        } else {
            (self.exit, self.from)
        };
        let span = full.mul_f32(remaining).as_secs_f32();
        if span <= 0.0 {
            return 1.0;
        }
        (now.saturating_duration_since(since).as_secs_f32() / span).min(1.0)
    }
}
