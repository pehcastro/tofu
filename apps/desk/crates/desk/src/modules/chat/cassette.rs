use std::cell::{Cell, RefCell};
use std::path::PathBuf;
use std::rc::Rc;
use std::time::{Duration, Instant};

use desk_core::bridge::Event;
use desk_core::protocol::{Notification, RequestId, ServerRequest};
use gpui::{IntoElement, Styled, canvas};
use serde_json::Value;

const CASSETTE: &str = "../../cassettes/36-agents.cassette";
const WARMUP: usize = 30;
const FRAMES: usize = 600;

pub enum Step {
    Feed(Event),
    Restart,
    Report,
}

pub struct Replay {
    lines: Vec<String>,
    next: usize,
    started: Rc<Cell<Option<Instant>>>,
    samples: Rc<RefCell<Vec<Duration>>>,
}

fn event(line: &str) -> Result<Event, String> {
    let raw: Value = serde_json::from_str(line).map_err(|error| error.to_string())?;
    let method = raw["method"]
        .as_str()
        .ok_or_else(|| format!("a cassette line with no method: {line}"))?;
    let params = raw.get("params").cloned().unwrap_or(Value::Null);
    let parsed = match raw.get("id") {
        Some(id) => serde_json::from_value::<RequestId>(id.clone()).and_then(|id| {
            ServerRequest::parse(method, params).map(|request| Event::Request { id, request })
        }),
        None => Notification::parse(method, params).map(Event::Notification),
    };
    parsed.map_err(|error| format!("{method}: {error}"))
}

fn report(samples: &[Duration]) {
    let mut measured: Vec<f64> = samples
        .iter()
        .skip(WARMUP)
        .map(|sample| sample.as_secs_f64() * 1000.0)
        .collect();
    measured.sort_by(f64::total_cmp);
    let at = |share: f64| {
        let index = ((measured.len() as f64 - 1.0) * share).round() as usize;
        measured.get(index).copied().unwrap_or_default()
    };
    println!(
        "frames {} p50 {:.2} p95 {:.2} max {:.2} ms",
        measured.len(),
        at(0.5),
        at(0.95),
        at(1.0)
    );
}

impl Replay {
    pub fn read() -> Result<Replay, String> {
        let path = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join(CASSETTE);
        let text = std::fs::read_to_string(&path)
            .map_err(|error| format!("the chat cannot read {}: {error}", path.display()))?;
        Ok(Replay {
            lines: text
                .lines()
                .filter(|line| !line.trim().is_empty())
                .map(str::to_owned)
                .collect(),
            next: 0,
            started: Rc::default(),
            samples: Rc::default(),
        })
    }

    pub fn step(&mut self) -> Result<Step, String> {
        if self.samples.borrow().len() >= WARMUP + FRAMES {
            report(&self.samples.borrow());
            return Ok(Step::Report);
        }
        self.started.set(Some(Instant::now()));
        let Some(line) = self.lines.get(self.next) else {
            self.next = 0;
            return Ok(Step::Restart);
        };
        self.next += 1;
        event(line).map(Step::Feed)
    }

    pub fn meter(&self) -> impl IntoElement {
        let (started, samples) = (self.started.clone(), self.samples.clone());
        canvas(
            |_, _, _| (),
            move |_, (), _, _| {
                if let Some(at) = started.take() {
                    samples.borrow_mut().push(at.elapsed());
                }
            },
        )
        .absolute()
        .size_0()
    }
}
