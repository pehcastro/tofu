use std::f32::consts::TAU;
use std::time::{Duration, Instant};

use gpui::{SpringConfig, SpringState};

const SETTLED: f32 = 0.002;
const VISUAL_TO_PERIOD: f32 = 1.2;
const SHORTEST_VISUAL: Duration = Duration::from_millis(1);

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Phase {
    Closed,
    Opening,
    Open,
    Closing,
}

#[derive(Clone, Copy, Debug)]
pub struct Presence {
    enter: SpringConfig,
    exit: SpringConfig,
    open: bool,
    from: SpringState,
    since: Option<Instant>,
}

impl Presence {
    pub fn new(enter: Duration, exit: Duration) -> Self {
        Self {
            enter: critical(enter),
            exit: critical(exit),
            open: false,
            from: SpringState::default(),
            since: None,
        }
    }

    pub fn set_open(&mut self, open: bool, now: Instant) {
        if open != self.open {
            self.from = self.state(now);
            self.open = open;
            self.since = Some(now);
        }
    }

    pub fn phase(&self, now: Instant) -> Phase {
        let settled = self.since.is_none()
            || self
                .config()
                .is_settled(self.state(now), self.target(), SETTLED);
        match (self.open, settled) {
            (true, false) => Phase::Opening,
            (true, true) => Phase::Open,
            (false, false) => Phase::Closing,
            (false, true) => Phase::Closed,
        }
    }

    pub fn progress(&self, now: Instant) -> f32 {
        self.state(now).position.clamp(0.0, 1.0)
    }

    pub fn mounted(&self, now: Instant) -> bool {
        self.phase(now) != Phase::Closed
    }

    fn state(&self, now: Instant) -> SpringState {
        let Some(since) = self.since else {
            return self.from;
        };
        let elapsed = now.saturating_duration_since(since).as_secs_f32();
        self.config().step(self.from, self.target(), elapsed)
    }

    fn config(&self) -> SpringConfig {
        if self.open { self.enter } else { self.exit }
    }

    fn target(&self) -> f32 {
        if self.open { 1.0 } else { 0.0 }
    }
}

fn critical(visual: Duration) -> SpringConfig {
    let frequency = TAU / (VISUAL_TO_PERIOD * visual.max(SHORTEST_VISUAL).as_secs_f32());
    let stiffness = frequency * frequency;
    SpringConfig::new(stiffness, 2.0 * frequency, 1.0)
}
