use std::collections::HashMap;
use std::time::Duration;

use gpui::prelude::FluentBuilder;
use gpui::{App, FrameTiming, IntoElement, ParentElement, Styled, div, px, rgba};

use crate::frames::{FrameSummary, ms};
use crate::limits::{FRAME_BUDGET, OVERLAY_TOP_SPANS};
use crate::probe::ProbeElement;
use crate::profiler::Profiler;

const RECORDING: u32 = 0xef4444ff;
const DOT: f32 = 10.0;
const DOT_MARGIN: f32 = 6.0;
const PANEL: u32 = 0x0b0d12e8;
const TEXT: u32 = 0xe6e9efff;
const MUTED: u32 = 0x9aa3b2ff;
const WITHIN_BUDGET: u32 = 0x4ade80ff;
const OVER_BUDGET: u32 = 0xf87171ff;
const MARGIN: f32 = 12.0;
const PADDING: f32 = 10.0;
const GAP: f32 = 4.0;
const RADIUS: f32 = 6.0;
const WIDTH: f32 = 340.0;
const BAR_WIDTH: f32 = 1.25;
const GRAPH_HEIGHT: f32 = 48.0;
const GRAPH_BUDGETS: f32 = 2.0;
const MIB: f64 = 1024.0 * 1024.0;

pub fn overlay(cx: &App) -> Option<impl IntoElement> {
    let profiler = cx.try_global::<Profiler>()?;
    let recording = profiler.recorder.is_some();
    if !profiler.overlay_open && !recording {
        return None;
    }
    Some(
        div()
            .absolute()
            .top_0()
            .left_0()
            .size_full()
            .when(profiler.overlay_open, |this| this.child(panel(profiler)))
            .when(recording, |this| {
                this.child(
                    div()
                        .absolute()
                        .top(px(DOT_MARGIN))
                        .right(px(DOT_MARGIN))
                        .size(px(DOT))
                        .rounded_full()
                        .bg(rgba(RECORDING)),
                )
                .child(ProbeElement)
            }),
    )
}

fn panel(profiler: &Profiler) -> impl IntoElement {
    let summary = FrameSummary::of(
        profiler
            .recent_frames
            .iter()
            .map(FrameTiming::draw_duration),
    );
    let memory = profiler.memory.unwrap_or_default();
    let mut spans: HashMap<&'static str, (usize, Duration)> = HashMap::new();
    for span in &profiler.recent_spans {
        let (count, total) = spans.entry(span.name).or_default();
        *count = count.saturating_add(1);
        *total = total.saturating_add(span.end.duration_since(span.start));
    }
    let mut spans: Vec<_> = spans.into_iter().collect();
    spans.sort_by_key(|(_, (_, total))| std::cmp::Reverse(*total));
    let scale = GRAPH_HEIGHT / (GRAPH_BUDGETS * ms(FRAME_BUDGET) as f32);
    let graph =
        div()
            .h(px(GRAPH_HEIGHT))
            .flex()
            .items_end()
            .children(profiler.recent_frames.iter().map(|frame| {
                let draw = frame.draw_duration();
                div()
                    .w(px(BAR_WIDTH))
                    .h(px((ms(draw) as f32 * scale).min(GRAPH_HEIGHT)))
                    .bg(rgba(if draw > FRAME_BUDGET {
                        OVER_BUDGET
                    } else {
                        WITHIN_BUDGET
                    }))
            }));
    div()
        .absolute()
        .top(px(MARGIN))
        .right(px(MARGIN))
        .w(px(WIDTH))
        .p(px(PADDING))
        .rounded(px(RADIUS))
        .bg(rgba(PANEL))
        .text_color(rgba(TEXT))
        .text_xs()
        .flex()
        .flex_col()
        .gap(px(GAP))
        .child(format!(
            "frame p50 {:.2}  p95 {:.2}  p99 {:.2}  max {:.2} ms",
            summary.p50_ms, summary.p95_ms, summary.p99_ms, summary.max_ms
        ))
        .child(format!(
            "over {} ms: {} of {} frames",
            FRAME_BUDGET.as_millis(),
            summary.over_budget,
            summary.frames
        ))
        .child(graph)
        .child(format!(
            "working set {:.1} MB  private {:.1} MB  allocs/frame {:.0}",
            memory.working_set as f64 / MIB,
            memory.private_bytes as f64 / MIB,
            profiler.allocations_per_frame
        ))
        .children(
            spans
                .into_iter()
                .take(OVERLAY_TOP_SPANS)
                .map(|(name, (count, total))| {
                    div()
                        .text_color(rgba(MUTED))
                        .child(format!("{name}  {count}x  {:.2} ms", ms(total)))
                }),
        )
}
