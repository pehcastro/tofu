use std::collections::BTreeMap;
use std::fs::File;
use std::io::{self, BufWriter, Write};
use std::path::Path;
use std::time::Instant;

use gpui::FrameTiming;
use serde_json::{Value, json};

use crate::frames::ms;
use crate::limits::RECORDED_EVENTS;
use crate::span::{CascadeId, SpanRecord};

const PROCESS: u32 = 1;
const FRAME_TRACK: u32 = 0;

pub(crate) struct Recording {
    pub started: Instant,
    pub spans: Vec<SpanRecord>,
    pub frames: Vec<FrameTiming>,
    pub dropped: u64,
}

struct CascadeWindow {
    root: &'static str,
    start: Instant,
    end: Instant,
}

impl Recording {
    pub fn new() -> Self {
        Recording {
            started: Instant::now(),
            spans: Vec::new(),
            frames: Vec::new(),
            dropped: 0,
        }
    }

    pub fn extend(&mut self, spans: &[SpanRecord], frames: &[FrameTiming], dropped: u64) {
        let room = RECORDED_EVENTS.saturating_sub(self.spans.len());
        let kept = spans.len().min(room);
        self.spans.extend(spans.iter().take(kept).cloned());
        let room = RECORDED_EVENTS.saturating_sub(self.frames.len());
        self.frames.extend(frames.iter().take(room).copied());
        let lost = u64::try_from(spans.len().saturating_sub(kept)).unwrap_or(u64::MAX);
        self.dropped = self.dropped.saturating_add(dropped).saturating_add(lost);
    }

    pub fn write_chrome_trace(&self, path: &Path) -> io::Result<()> {
        let at = |instant: Instant| {
            instant
                .saturating_duration_since(self.started)
                .as_secs_f64()
                * 1e6
        };
        let mut cascades: BTreeMap<CascadeId, CascadeWindow> = BTreeMap::new();
        for span in &self.spans {
            let Some(id) = span.cascade else { continue };
            let window = cascades.entry(id).or_insert(CascadeWindow {
                root: span.name,
                start: span.start,
                end: span.end,
            });
            if span.start < window.start {
                window.root = span.name;
                window.start = span.start;
            }
            window.end = window.end.max(span.end);
        }
        let frame_cascades: Vec<Option<CascadeId>> = self
            .frames
            .iter()
            .map(|frame| {
                let dirty_at = frame.dirty_at?;
                cascades
                    .iter()
                    .rev()
                    .find(|(_, window)| window.start <= dirty_at && dirty_at <= window.end)
                    .map(|(id, _)| *id)
            })
            .collect();
        for (frame, id) in self.frames.iter().zip(&frame_cascades) {
            if let Some(window) = id.and_then(|id| cascades.get_mut(&id)) {
                window.end = window.end.max(frame.draw_end);
            }
        }
        let mut events = vec![json!({
            "name": "thread_name", "ph": "M", "pid": PROCESS, "tid": FRAME_TRACK,
            "args": { "name": "frames" },
        })];
        events.extend(self.spans.iter().map(|span| {
            json!({
                "name": span.name, "cat": "span", "ph": "X", "pid": PROCESS, "tid": span.thread,
                "ts": at(span.start), "dur": at(span.end) - at(span.start),
                "args": { "cascade": span.cascade.map(CascadeId::raw), "depth": span.depth },
            })
        }));
        events.extend(self.frames.iter().zip(&frame_cascades).map(|(frame, id)| {
            json!({
                "name": "frame", "cat": "frame", "ph": "X", "pid": PROCESS, "tid": FRAME_TRACK,
                "ts": at(frame.draw_start), "dur": at(frame.draw_end) - at(frame.draw_start),
                "args": {
                    "cascade": id.map(CascadeId::raw),
                    "invalidations": frame.invalidations,
                    "dirty_to_draw_ms": frame.dirty_to_draw_duration().map(ms),
                },
            })
        }));
        for (id, window) in &cascades {
            for (phase, instant) in [("b", window.start), ("e", window.end)] {
                events.push(json!({
                    "name": window.root, "cat": "cascade", "ph": phase, "id": id.raw(),
                    "pid": PROCESS, "tid": FRAME_TRACK, "ts": at(instant),
                }));
            }
        }
        let trace: Value = json!({
            "traceEvents": events,
            "displayTimeUnit": "ms",
            "otherData": { "dropped_spans": self.dropped },
        });
        let mut file = BufWriter::new(File::create(path)?);
        serde_json::to_writer(&mut file, &trace)?;
        file.flush()
    }
}
