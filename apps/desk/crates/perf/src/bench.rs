use std::time::{Duration, Instant};

use gpui::{App, Window};
use serde::Serialize;

use crate::frames::{FrameSummary, ms};
use crate::limits::{BENCH_WARMUP_FRAMES, MEMORY_INTERVAL};
use crate::memory;

const MIB: f64 = 1024.0 * 1024.0;

#[derive(Clone, Debug, Serialize)]
pub struct BenchResult {
    pub scenario: String,
    pub debug_assertions: bool,
    #[serde(flatten)]
    pub frames: FrameSummary,
    pub allocations_per_frame: Option<f64>,
    pub peak_working_set_mb: f64,
    pub wall_ms: f64,
}

impl BenchResult {
    pub fn json_line(&self) -> serde_json::Result<String> {
        serde_json::to_string(self)
    }
}

struct Measured {
    started: Instant,
    last_frame: Instant,
    intervals: Vec<Duration>,
    allocations: usize,
    sampled: Instant,
    peak_working_set: usize,
}

impl Measured {
    fn start(steps: u32) -> Self {
        let now = Instant::now();
        let mut measured = Measured {
            started: now,
            last_frame: now,
            intervals: Vec::with_capacity(steps as usize),
            allocations: memory::allocations(),
            sampled: now,
            peak_working_set: 0,
        };
        measured.sample_memory(now);
        measured
    }

    fn frame(&mut self) {
        let now = Instant::now();
        self.intervals.push(now.duration_since(self.last_frame));
        self.last_frame = now;
        if now.duration_since(self.sampled) >= MEMORY_INTERVAL {
            self.sample_memory(now);
        }
    }

    fn sample_memory(&mut self, now: Instant) {
        self.sampled = now;
        if let Some(sample) = memory::sample() {
            self.peak_working_set = self.peak_working_set.max(sample.working_set);
        }
    }

    fn result(mut self, scenario: String) -> BenchResult {
        self.sample_memory(Instant::now());
        let frames = FrameSummary::of(self.intervals);
        let counted = memory::allocations();
        let allocations = counted.saturating_sub(self.allocations);
        BenchResult {
            scenario,
            debug_assertions: cfg!(debug_assertions),
            frames,
            allocations_per_frame: (counted > 0)
                .then(|| allocations as f64 / frames.frames.max(1) as f64),
            peak_working_set_mb: self.peak_working_set as f64 / MIB,
            wall_ms: ms(self.started.elapsed()),
        }
    }
}

struct Run<Step, Done> {
    scenario: String,
    steps: u32,
    frame: u32,
    measured: Option<Measured>,
    step: Step,
    done: Done,
}

pub fn run_bench<Step, Done>(
    scenario: String,
    steps: u32,
    step: Step,
    done: Done,
    window: &mut Window,
    cx: &mut App,
) where
    Step: FnMut(u32, &mut Window, &mut App) + 'static,
    Done: FnOnce(BenchResult, &mut App) + 'static,
{
    advance(
        Run {
            scenario,
            steps,
            frame: 0,
            measured: None,
            step,
            done,
        },
        window,
        cx,
    );
}

fn advance<Step, Done>(mut run: Run<Step, Done>, window: &mut Window, cx: &mut App)
where
    Step: FnMut(u32, &mut Window, &mut App) + 'static,
    Done: FnOnce(BenchResult, &mut App) + 'static,
{
    match run.measured.as_mut() {
        Some(measured) => measured.frame(),
        None if run.frame == BENCH_WARMUP_FRAMES => {
            run.measured = Some(Measured::start(run.steps));
        }
        None => {}
    }
    if run.frame >= BENCH_WARMUP_FRAMES.saturating_add(run.steps)
        && let Some(measured) = run.measured.take()
    {
        (run.done)(measured.result(run.scenario), cx);
        return;
    }
    (run.step)(run.frame, window, cx);
    window.refresh();
    run.frame = run.frame.saturating_add(1);
    window.on_next_frame(move |window, cx| advance(run, window, cx));
}
