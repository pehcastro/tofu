use std::cell::RefCell;
use std::path::PathBuf;
use std::rc::Rc;
use std::sync::Arc;
use std::time::{Duration, Instant};

use desk_core::bridge::Event;
use desk_core::model::{Role, Session, Store};
use desk_core::protocol::{AgentState, Notification, RequestId, ServerRequest};
use gpui::private::serde_json::{self, Value};
use gpui::{
    AnyView, App, AppContext, Context, Image, IntoElement, ListAlignment, ListState, Render,
    Window, canvas, div, list, prelude::*, px,
};

use super::boards::{inner, shell};
use super::chrome;
use super::composer::compact;
use super::paint::{DEL, T3, WARN, white};
use super::rows::{Block, COLUMN, Mark, Outcome, Row, plain, row};

const CASSETTE: &str = "../../cassettes/36-agents.cassette";
const WARMUP: usize = 30;
const FRAMES: usize = 600;
const STEP: f32 = 6.0;
const LEG: usize = 100;
const OVERDRAW: f32 = 200.0;

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

fn session(text: &str) -> Result<Session, String> {
    let mut store = Store::default();
    let mut batch = Vec::new();
    for line in text.lines().chain([""]) {
        if !line.trim().is_empty() {
            batch.push(event(line)?);
        } else if !batch.is_empty() {
            store
                .apply_batch(&batch)
                .map_err(|error| error.to_string())?;
            batch.clear();
        }
    }
    store
        .sessions
        .into_values()
        .next()
        .ok_or_else(|| "the cassette built no session".to_owned())
}

fn clock(stamp: &str) -> String {
    stamp.get(11..16).unwrap_or_default().to_owned()
}

fn rows(session: &Session) -> Vec<Row> {
    let time = session
        .turns
        .values()
        .next()
        .map(|turn| clock(&turn.started_at))
        .unwrap_or_default();
    let said = |role: Role| {
        session
            .messages
            .iter()
            .filter(move |message| message.role == role)
            .map(|message| message.text.clone())
    };
    let mut rows: Vec<Row> = said(Role::User)
        .map(|text| Row::You {
            time: time.clone().into(),
            text: text.into(),
            trace: false,
        })
        .collect();
    rows.extend(session.tools.values().map(|tool| Row::Command {
        mark: Mark::Prompt,
        command: format!("{} {}", tool.name, tool.args).into(),
        tail: vec![if tool.failed {
            ("failed".into(), DEL)
        } else {
            (
                format!(
                    "{} lines",
                    tool.output.as_deref().unwrap_or_default().lines().count()
                )
                .into(),
                white(T3),
            )
        }],
    }));
    rows.extend(session.agents.values().map(|agent| {
        Row::Agent {
            outcome: match agent.state {
                AgentState::Finished => Outcome::Done,
                _ => Outcome::Running,
            },
            name: format!("{} {}", agent.kind, agent.number).into(),
            summary: agent
                .report
                .clone()
                .unwrap_or_else(|| agent.task.clone())
                .into(),
            link: true,
        }
    }));
    rows.extend(session.shells.values().map(|shell| {
        Row::Command {
            mark: if shell.exited {
                Mark::Prompt
            } else {
                Mark::Running
            },
            command: format!("bash {}", shell.command).into(),
            tail: shell
                .exit_code
                .map(|code| (format!("exit {code}").into(), white(T3)))
                .into_iter()
                .collect(),
        }
    }));
    rows.extend(session.approvals.values().map(|(_, asked)| Row::Command {
        mark: Mark::Running,
        command: asked.target.clone().into(),
        tail: vec![("ask".into(), WARN)],
    }));
    rows.extend(said(Role::Assistant).map(|text| Row::Lead {
        time: time.clone().into(),
        body: vec![Block::Para(vec![plain(text)])],
    }));
    rows
}

pub fn open(backdrop: Arc<Image>, cx: &mut App) -> Result<AnyView, String> {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join(CASSETTE);
    let text = std::fs::read_to_string(&path)
        .map_err(|error| format!("the chat cannot read {}: {error}", path.display()))?;
    let rows = Rc::new(rows(&session(&text)?));
    Ok(cx
        .new(|_| Replay {
            state: ListState::new(rows.len(), ListAlignment::Top, px(OVERDRAW)),
            rows,
            backdrop,
            frame: 0,
            started: Rc::default(),
            samples: Rc::default(),
        })
        .into())
}

struct Replay {
    rows: Rc<Vec<Row>>,
    state: ListState,
    backdrop: Arc<Image>,
    frame: usize,
    started: Rc<RefCell<Option<Instant>>>,
    samples: Rc<RefCell<Vec<Duration>>>,
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

impl Render for Replay {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if self.samples.borrow().len() >= WARMUP + FRAMES {
            report(&self.samples.borrow());
            cx.quit();
        }
        *self.started.borrow_mut() = Some(Instant::now());
        let forward = (self.frame / LEG).is_multiple_of(2);
        self.frame += 1;
        self.state.scroll_by(px(if forward { STEP } else { -STEP }));
        window.request_animation_frame();
        let scale = window.scale_factor();
        let rows = self.rows.clone();
        let (started, samples) = (self.started.clone(), self.samples.clone());
        let transcript = list(self.state.clone(), move |index, window, _| {
            let scale = window.scale_factor();
            rows.get(index).map_or_else(
                || div().into_any_element(),
                |each| {
                    div()
                        .flex()
                        .justify_center()
                        .child(div().w(px(COLUMN)).flex().flex_col().child(row(
                            each,
                            index == 0,
                            scale,
                        )))
                        .into_any_element()
                },
            )
        })
        .flex_1()
        .pt(px(22.0));
        chrome::window(
            &self.backdrop,
            scale,
            shell()
                .flex_1()
                .mb(px(8.0))
                .child(div().h(px(28.0)).flex_none())
                .child(
                    inner().child(transcript).child(
                        div()
                            .flex()
                            .justify_center()
                            .pt(px(6.0))
                            .pb(px(12.0))
                            .child(compact("Ask tofu to build, inspect, or delegate", scale)),
                    ),
                ),
        )
        .child(
            canvas(
                |_, _, _| (),
                move |_, (), _, _| {
                    if let Some(at) = started.borrow_mut().take() {
                        samples.borrow_mut().push(at.elapsed());
                    }
                },
            )
            .absolute()
            .size_0(),
        )
    }
}
