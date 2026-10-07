use std::collections::VecDeque;
use std::path::PathBuf;
use std::thread::{self, JoinHandle};
use std::time::Instant;

use gpui::profiler::hang::{HangDetector, SerializedHangIncident};
use gpui::{App, FrameEvent, FrameTiming, FrameTimingCollector, Global, KeystrokeEvent, Window};
use serde::Serialize;

use crate::bench::{BenchMark, BenchResult};
use crate::frames::{FrameSummary, ms};
use crate::limits::{
    HANG_CONTRIBUTORS, HANG_THRESHOLD, MEMORY_INTERVAL, POLL_INTERVAL, RECENT_FRAMES, RECENT_SPANS,
    RECORD_MAX,
};
use crate::memory::{self, MemorySample};
use crate::record::{Input, InputKind, Recorder, Stop};
use crate::span::{self, SpanRecord};
use crate::trace::Recording;

const MIB: f64 = 1024.0 * 1024.0;

pub struct Profiler {
    startup: Instant,
    collector: FrameTimingCollector,
    hangs: HangDetector,
    pub(crate) recent_frames: VecDeque<FrameTiming>,
    pub(crate) recent_spans: VecDeque<SpanRecord>,
    pub(crate) memory: Option<MemorySample>,
    memory_at: Option<Instant>,
    peak_working_set: usize,
    allocations_seen: usize,
    pub(crate) allocations_per_frame: f64,
    recording: Option<Recording>,
    trace_path: Option<PathBuf>,
    pub(crate) overlay_open: bool,
    pub(crate) recorder: Option<Recorder>,
    writer: Option<JoinHandle<()>>,
}

impl Global for Profiler {}

#[derive(Serialize)]
struct HangReport<'a> {
    #[serde(flatten)]
    incident: SerializedHangIncident,
    stacks: Vec<SlowSpan<'a>>,
}

#[derive(Serialize)]
struct SlowSpan<'a> {
    name: &'static str,
    duration_ms: f64,
    stack: &'a str,
}

impl Profiler {
    pub fn install(trace_path: Option<PathBuf>, cx: &mut App) {
        let mut profiler = Profiler {
            startup: Instant::now(),
            collector: FrameTimingCollector::new(),
            hangs: HangDetector::new(cx.foreground_journal(), HANG_THRESHOLD, HANG_THRESHOLD),
            recent_frames: VecDeque::with_capacity(RECENT_FRAMES),
            recent_spans: VecDeque::new(),
            memory: None,
            memory_at: None,
            peak_working_set: 0,
            allocations_seen: memory::allocations(),
            allocations_per_frame: 0.0,
            recording: trace_path.as_ref().map(|_| Recording::new()),
            trace_path,
            overlay_open: false,
            recorder: None,
            writer: None,
        };
        profiler.retrace();
        cx.set_global(profiler);
        cx.spawn(async |cx| {
            loop {
                cx.background_executor().timer(POLL_INTERVAL).await;
                if !cx.update(Self::tick) {
                    break;
                }
            }
        })
        .detach();
        cx.observe_keystrokes(Self::keystroke).detach();
        cx.on_app_quit(|cx| {
            if cx.has_global::<Profiler>() {
                Self::stop_recording(Stop::Key, cx);
                let profiler = cx.global_mut::<Profiler>();
                profiler.write_trace();
                profiler.join_writer();
            }
            async {}
        })
        .detach();
    }

    pub(crate) fn record(cx: &mut App, write: impl FnOnce(&mut Recorder)) {
        if cx.has_global::<Profiler>()
            && let Some(recorder) = cx.global_mut::<Profiler>().recorder.as_mut()
        {
            write(recorder);
        }
    }

    fn keystroke(event: &KeystrokeEvent, window: &mut Window, cx: &mut App) {
        let keystroke = &event.keystroke;
        let modifiers = keystroke.modifiers;
        if keystroke.key != "f9" || modifiers.control || modifiers.alt || modifiers.platform {
            let input = Input {
                at: Instant::now(),
                kind: InputKind::Key,
                position: None,
                button: None,
                key: Some(keystroke.unparse()),
                delta: None,
            };
            Self::record(cx, |recorder| recorder.input(input));
            return;
        }
        if !cx.has_global::<Profiler>() {
            return;
        }
        if cx.global::<Profiler>().recorder.is_some() {
            Self::stop_recording(Stop::Key, cx);
            return;
        }
        let recorder = Recorder::start(!modifiers.shift, window);
        let profiler = cx.global_mut::<Profiler>();
        profiler.poll();
        profiler.recorder = Some(recorder);
        profiler.retrace();
        cx.refresh_windows();
    }

    fn stop_recording(stop: Stop, cx: &mut App) {
        let profiler = cx.global_mut::<Profiler>();
        profiler.poll();
        let Some(recorder) = profiler.recorder.take() else {
            return;
        };
        profiler.retrace();
        let recorder = recorder.finish(stop);
        profiler.join_writer();
        profiler.writer = Some(thread::spawn(move || match recorder.write() {
            Ok(folder) => eprintln!("recording written to {}", folder.display()),
            Err(error) => eprintln!("the recording could not be written: {error}"),
        }));
        cx.refresh_windows();
    }

    fn join_writer(&mut self) {
        if let Some(writer) = self.writer.take()
            && writer.join().is_err()
        {
            eprintln!("the recording writer panicked");
        }
    }

    pub fn toggle_overlay(cx: &mut App) {
        if !cx.has_global::<Profiler>() {
            return;
        }
        let profiler = cx.global_mut::<Profiler>();
        profiler.overlay_open = !profiler.overlay_open;
        profiler.retrace();
        cx.refresh_windows();
    }

    fn tick(cx: &mut App) -> bool {
        if !cx.has_global::<Profiler>() {
            return false;
        }
        let profiler = cx.global_mut::<Profiler>();
        profiler.poll();
        let overlay_open = profiler.overlay_open;
        if profiler
            .recorder
            .as_ref()
            .is_some_and(|recorder| recorder.started.elapsed() >= RECORD_MAX)
        {
            Self::stop_recording(Stop::Limit, cx);
        }
        if overlay_open {
            cx.refresh_windows();
        }
        true
    }

    fn retrace(&mut self) {
        let wanted = self.overlay_open || self.recording.is_some() || self.recorder.is_some();
        if gpui::set_trace_enabled(wanted) && wanted {
            self.collector = FrameTimingCollector::new();
        }
    }

    fn poll(&mut self) {
        let now = Instant::now();
        let (spans, dropped) = span::drain();
        let events = self.collector.collect_unseen();
        let frames: Vec<FrameTiming> = events
            .iter()
            .filter_map(|event| match event {
                FrameEvent::Draw(frame) => Some(*frame),
                FrameEvent::Present(_) => None,
            })
            .collect();
        if let Some(recording) = &mut self.recording {
            recording.extend(&spans, &frames, dropped);
        }
        if let Some(recorder) = &mut self.recorder {
            recorder.extend(&spans, &frames, dropped, &events);
        }
        let allocations = memory::allocations();
        if !frames.is_empty() {
            self.allocations_per_frame =
                allocations.saturating_sub(self.allocations_seen) as f64 / frames.len() as f64;
        }
        self.allocations_seen = allocations;
        if self
            .memory_at
            .is_none_or(|at| now.duration_since(at) >= MEMORY_INTERVAL)
        {
            self.sample_memory(now);
        }
        self.recent_frames.extend(frames);
        while self.recent_frames.len() > RECENT_FRAMES {
            self.recent_frames.pop_front();
        }
        self.recent_spans.extend(spans);
        while self
            .recent_spans
            .front()
            .is_some_and(|span| now.duration_since(span.end) > RECENT_SPANS)
        {
            self.recent_spans.pop_front();
        }
        for incident in self.hangs.poll() {
            let (start, end) = incident.active_window();
            let report = HangReport {
                incident: SerializedHangIncident::convert(
                    self.startup,
                    &incident,
                    HANG_CONTRIBUTORS,
                    self.hangs.first_present_at(),
                ),
                stacks: self
                    .recent_spans
                    .iter()
                    .filter(|span| span.start < end && start < span.end)
                    .filter_map(|span| {
                        Some(SlowSpan {
                            name: span.name,
                            duration_ms: ms(span.end.duration_since(span.start)),
                            stack: span.stack.as_deref()?,
                        })
                    })
                    .collect(),
            };
            match serde_json::to_string(&report) {
                Ok(line) => eprintln!("hang {line}"),
                Err(error) => eprintln!("hang that could not be written: {error}"),
            }
        }
    }

    fn sample_memory(&mut self, now: Instant) {
        self.memory = memory::sample();
        self.memory_at = Some(now);
        if let Some(sample) = self.memory {
            self.peak_working_set = self.peak_working_set.max(sample.working_set);
        }
    }

    fn write_trace(&mut self) {
        self.poll();
        let (Some(path), Some(recording)) = (&self.trace_path, &self.recording) else {
            return;
        };
        if let Err(error) = recording.write_chrome_trace(path) {
            eprintln!(
                "tofu desk could not write its trace to {}: {error}",
                path.display()
            );
        }
    }

    pub(crate) fn mark(&mut self) -> BenchMark {
        self.recording.get_or_insert_with(Recording::new);
        self.retrace();
        self.poll();
        BenchMark {
            frame: self
                .recording
                .as_ref()
                .map_or(0, |recording| recording.frames.len()),
            allocations: memory::allocations(),
            started: Instant::now(),
        }
    }

    pub(crate) fn bench_result(&mut self, scenario: String, mark: &BenchMark) -> BenchResult {
        self.poll();
        self.sample_memory(Instant::now());
        let frames = FrameSummary::of(
            self.recording
                .iter()
                .flat_map(|recording| recording.frames.get(mark.frame..).unwrap_or_default())
                .map(FrameTiming::draw_duration),
        );
        let allocations = memory::allocations().saturating_sub(mark.allocations);
        BenchResult {
            scenario,
            debug_assertions: cfg!(debug_assertions),
            frames,
            allocations_per_frame: allocations as f64 / frames.frames.max(1) as f64,
            peak_working_set_mb: self.peak_working_set as f64 / MIB,
            wall_ms: ms(mark.started.elapsed()),
        }
    }
}
