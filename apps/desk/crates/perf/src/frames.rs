use std::time::Duration;

use serde::Serialize;

use crate::limits::FRAME_BUDGET;

#[derive(Clone, Copy, Debug, Default, Serialize)]
pub struct FrameSummary {
    pub frames: usize,
    pub p50_ms: f64,
    pub p95_ms: f64,
    pub p99_ms: f64,
    pub max_ms: f64,
    pub over_budget: usize,
}

impl FrameSummary {
    pub fn of(durations: impl IntoIterator<Item = Duration>) -> Self {
        let mut sorted: Vec<Duration> = durations.into_iter().collect();
        sorted.sort_unstable();
        let at = |quantile: f64| {
            let rank = (quantile * sorted.len() as f64).ceil() as usize;
            sorted.get(rank.saturating_sub(1)).copied().map_or(0.0, ms)
        };
        FrameSummary {
            frames: sorted.len(),
            p50_ms: at(0.50),
            p95_ms: at(0.95),
            p99_ms: at(0.99),
            max_ms: sorted.last().copied().map_or(0.0, ms),
            over_budget: sorted.iter().filter(|frame| **frame > FRAME_BUDGET).count(),
        }
    }
}

pub(crate) fn ms(duration: Duration) -> f64 {
    duration.as_secs_f64() * 1000.0
}
