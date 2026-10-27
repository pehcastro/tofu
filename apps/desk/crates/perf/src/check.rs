use std::env;
use std::time::{Duration, Instant};

use gpui::FrameTiming;

use crate::bench::{BenchResult, MIB};
use crate::frames::{FrameSummary, ms};
use crate::limits::{CHECK_PARK, CHECK_PHASE, CHECK_QUIET, CHECK_SETTLE_MAX};
use crate::memory;

const FRAME_CHECK_VARIABLE: &str = "TOFU_DESK_FRAME_CHECK";

#[derive(Clone, Copy)]
enum Phase {
    Settle,
    Park,
    Idle,
    Hover,
    Scroll,
}

impl Phase {
    fn name(self) -> &'static str {
        match self {
            Phase::Settle => "settle",
            Phase::Park => "park",
            Phase::Idle => "idle",
            Phase::Hover => "hover",
            Phase::Scroll => "scroll",
        }
    }

    fn next(self) -> Option<Phase> {
        match self {
            Phase::Settle => Some(Phase::Park),
            Phase::Park => Some(Phase::Idle),
            Phase::Idle => Some(Phase::Hover),
            Phase::Hover => Some(Phase::Scroll),
            Phase::Scroll => None,
        }
    }

    fn measured(self) -> bool {
        match self {
            Phase::Settle | Phase::Park => false,
            Phase::Idle | Phase::Hover | Phase::Scroll => true,
        }
    }
}

pub(crate) struct FrameCheck {
    phase: Phase,
    started: Instant,
    last_drawn: Option<Instant>,
    draws: Vec<Duration>,
    allocations: usize,
    peak_working_set: usize,
}

impl FrameCheck {
    pub(crate) fn from_env() -> Option<Self> {
        env::var_os(FRAME_CHECK_VARIABLE)?;
        eprintln!("frame-check phase {}", Phase::Settle.name());
        Some(FrameCheck {
            phase: Phase::Settle,
            started: Instant::now(),
            last_drawn: None,
            draws: Vec::new(),
            allocations: 0,
            peak_working_set: 0,
        })
    }

    pub(crate) fn extend(&mut self, frames: &[FrameTiming], allocations: usize) {
        if let Some(last) = frames.last() {
            self.last_drawn = Some(last.draw_end);
        }
        if !self.phase.measured() {
            return;
        }
        self.draws
            .extend(frames.iter().map(FrameTiming::draw_duration));
        if !frames.is_empty() {
            self.allocations = self.allocations.saturating_add(allocations);
        }
        if let Some(sample) = memory::sample() {
            self.peak_working_set = self.peak_working_set.max(sample.working_set);
        }
    }

    pub(crate) fn advance(&mut self, now: Instant) -> bool {
        let elapsed = now.duration_since(self.started);
        let quiet = self
            .last_drawn
            .is_some_and(|at| now.duration_since(at) >= CHECK_QUIET);
        let over = match self.phase {
            Phase::Settle => quiet || elapsed >= CHECK_SETTLE_MAX,
            Phase::Park => elapsed >= CHECK_PARK,
            Phase::Idle | Phase::Hover | Phase::Scroll => elapsed >= CHECK_PHASE,
        };
        if !over {
            return false;
        }
        if matches!(self.phase, Phase::Settle) && !quiet {
            eprintln!(
                "frame-check settle still drawing after {:.0} ms",
                ms(elapsed)
            );
        }
        if self.phase.measured() {
            self.report(elapsed);
        }
        let Some(next) = self.phase.next() else {
            return true;
        };
        self.phase = next;
        self.started = now;
        self.draws.clear();
        self.allocations = 0;
        self.peak_working_set = 0;
        eprintln!("frame-check phase {}", next.name());
        false
    }

    fn report(&self, elapsed: Duration) {
        let frames = FrameSummary::of(self.draws.iter().copied());
        let result = BenchResult {
            scenario: self.phase.name().to_owned(),
            debug_assertions: cfg!(debug_assertions),
            frames,
            allocations_per_frame: (memory::allocations() > 0)
                .then(|| self.allocations as f64 / frames.frames.max(1) as f64),
            peak_working_set_mb: self.peak_working_set as f64 / MIB,
            wall_ms: ms(elapsed),
        };
        match result.json_line() {
            Ok(line) => eprintln!("frame-check result {line}"),
            Err(error) => eprintln!("frame-check result that could not be written: {error}"),
        }
    }
}
