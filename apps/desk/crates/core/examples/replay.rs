use std::collections::BTreeMap;
use std::error::Error;

use desk_core::bridge::Event;
use desk_core::model::{Session, Store};
use desk_core::protocol::{AgentState, Notification, RequestId, ServerRequest};
use serde_json::Value;

type Summary = BTreeMap<String, usize>;

fn parse(line: &str) -> Result<Event, Box<dyn Error>> {
    let raw: Value = serde_json::from_str(line)?;
    let method = raw["method"].as_str().ok_or("a line with no method")?;
    let params = raw.get("params").cloned().unwrap_or(Value::Null);
    Ok(match raw.get("id") {
        Some(id) => Event::Request {
            id: serde_json::from_value::<RequestId>(id.clone())?,
            request: ServerRequest::parse(method, params)?,
        },
        None => Event::Notification(Notification::parse(method, params)?),
    })
}

fn state_name(state: &AgentState) -> String {
    serde_json::to_value(state)
        .ok()
        .and_then(|value| value.as_str().map(str::to_owned))
        .unwrap_or_default()
}

fn reduced(session: &Session) -> Summary {
    let mut summary = Summary::new();
    for agent in session.agents.values() {
        *summary
            .entry(format!("agents {}", state_name(&agent.state)))
            .or_default() += 1;
    }
    summary.insert("files".into(), session.files.len());
    summary.insert(
        "file revisions".into(),
        session.files.values().map(Vec::len).sum(),
    );
    summary.insert("shells".into(), session.shells.len());
    summary.insert(
        "shells running".into(),
        session.shells.values().filter(|s| !s.exited).count(),
    );
    summary.insert(
        "shells killed".into(),
        session.shells.values().filter(|s| s.killed).count(),
    );
    summary.insert("asks open".into(), session.approvals.len());
    summary.insert("forks".into(), session.forks.len());
    summary
}

fn counted(lines: &[&str]) -> Result<Summary, Box<dyn Error>> {
    let mut states = BTreeMap::<String, String>::new();
    let mut files = BTreeMap::<String, usize>::new();
    let mut shells = BTreeMap::<String, (bool, bool)>::new();
    let (mut asks, mut forks) = (0, 0);
    for line in lines {
        let raw: Value = serde_json::from_str(line)?;
        let p = &raw["params"];
        let text = |key: &str| p[key].as_str().unwrap_or_default().to_owned();
        match raw["method"].as_str().unwrap_or_default() {
            "agent.started" | "agent.updated" | "agent.ended" if p.get("state").is_some() => {
                states.insert(text("instance"), text("state"));
            }
            "file.edit" => *files.entry(text("path")).or_default() += 1,
            "shell.started" => {
                shells.insert(text("shell"), (false, false));
            }
            "shell.exited" => {
                shells.insert(
                    text("shell"),
                    (true, p["killed"].as_bool().unwrap_or(false)),
                );
            }
            "tofu/requestApproval" => asks += 1,
            "approval.resolved" => asks -= 1,
            "session.forked" => forks += 1,
            _ => {}
        }
    }
    let mut summary = Summary::new();
    for state in states.values() {
        *summary.entry(format!("agents {state}")).or_default() += 1;
    }
    summary.insert("files".into(), files.len());
    summary.insert("file revisions".into(), files.values().sum());
    summary.insert("shells".into(), shells.len());
    summary.insert(
        "shells running".into(),
        shells.values().filter(|s| !s.0).count(),
    );
    summary.insert(
        "shells killed".into(),
        shells.values().filter(|s| s.1).count(),
    );
    summary.insert("asks open".into(), asks);
    summary.insert("forks".into(), forks);
    Ok(summary)
}

fn main() -> Result<(), Box<dyn Error>> {
    let path = std::env::args().nth(1).ok_or("usage: replay CASSETTE")?;
    let text = std::fs::read_to_string(&path)?;
    let mut store = Store::default();
    let mut frames = 0;
    let mut batch = Vec::new();
    for line in text.lines().chain([""]) {
        if !line.trim().is_empty() {
            batch.push(parse(line)?);
        } else if !batch.is_empty() {
            store.apply_batch(&batch)?;
            batch.clear();
            frames += 1;
        }
    }
    let lines: Vec<&str> = text
        .lines()
        .filter(|line| !line.trim().is_empty())
        .collect();
    let cassette = counted(&lines)?;
    let first = lines.first().ok_or("an empty cassette")?;
    let first: Value = serde_json::from_str(first)?;
    let id = first["params"]["session"]
        .as_str()
        .ok_or("the first event names no session")?;
    let session = store
        .sessions
        .get(id)
        .ok_or("the reducers built no session")?;
    let model = reduced(session);
    println!(
        "{path}: {} events in {frames} frames, {} sessions",
        lines.len(),
        store.sessions.len()
    );
    println!("{:<20} {:>8} {:>9}", "", "reduced", "cassette");
    let keys: std::collections::BTreeSet<&String> = model.keys().chain(cassette.keys()).collect();
    let mut mismatches = 0;
    for key in keys {
        let (left, right) = (
            model.get(key).copied().unwrap_or(0),
            cassette.get(key).copied().unwrap_or(0),
        );
        let mark = if left == right { "" } else { "  MISMATCH" };
        mismatches += usize::from(left != right);
        println!("{key:<20} {left:>8} {right:>9}{mark}");
    }
    if mismatches > 0 {
        return Err(format!("{mismatches} counts disagree").into());
    }
    println!("all counts match");
    Ok(())
}
