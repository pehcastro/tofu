use std::time::Instant;

use gpui::{App, Window};
use serde::Serialize;

use crate::frames::FrameSummary;
use crate::limits::BENCH_WARMUP_FRAMES;
use crate::profiler::Profiler;

#[derive(Clone, Debug, Serialize)]
pub struct BenchResult {
    pub scenario: String,
    pub debug_assertions: bool,
    #[serde(flatten)]
    pub frames: FrameSummary,
    pub allocations_per_frame: f64,
    pub peak_working_set_mb: f64,
    pub wall_ms: f64,
}

impl BenchResult {
    pub fn json_line(&self) -> serde_json::Result<String> {
        serde_json::to_string(self)
    }
}

pub(crate) struct BenchMark {
    pub frame: usize,
    pub allocations: usize,
    pub started: Instant,
}

struct Run<Step, Done> {
    scenario: String,
    steps: u32,
    frame: u32,
    mark: Option<BenchMark>,
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
    if !cx.has_global::<Profiler>() {
        Profiler::install(None, cx);
    }
    advance(
        Run {
            scenario,
            steps,
            frame: 0,
            mark: None,
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
    if run.frame == BENCH_WARMUP_FRAMES {
        run.mark = Some(cx.global_mut::<Profiler>().mark());
    }
    if let Some(mark) = &run.mark
        && run.frame >= BENCH_WARMUP_FRAMES.saturating_add(run.steps)
    {
        let result = cx.global_mut::<Profiler>().bench_result(run.scenario, mark);
        (run.done)(result, cx);
        return;
    }
    (run.step)(run.frame, window, cx);
    window.refresh();
    run.frame = run.frame.saturating_add(1);
    window.on_next_frame(move |window, cx| advance(run, window, cx));
}
