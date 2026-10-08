use desk_core::model::{Agent as Member, Session, Tool};
use desk_core::protocol::{AgentState, FileEdit, HunkLineKind};
use desk_ui::components::agents::{
    AgentBoard, AgentEvent, AgentLine, AgentStep, DiffLine, DiffSign, ago, lasted,
};
use desk_ui::components::avatar::{Agent, AgentKind, AgentStatus};
use gpui::SharedString;
use serde_json::Value;

const ARGUMENT_KEYS: [&str; 5] = ["command", "path", "pattern", "query", "url"];
const OUTPUT_LINES: usize = 8;
const SECONDS_PER_MINUTE: f32 = 60.0;

pub fn status(state: &AgentState) -> AgentStatus {
    match state {
        AgentState::Working | AgentState::InReview | AgentState::Reopened => AgentStatus::Working,
        AgentState::WaitingAnswer => AgentStatus::Asking,
        AgentState::Parked | AgentState::Finished => AgentStatus::Finished,
        AgentState::Errored | AgentState::Unknown(_) => AgentStatus::Failed,
    }
}

pub fn shown(member: &Member) -> Option<Agent> {
    Some(Agent {
        kind: AgentKind::ALL
            .into_iter()
            .find(|kind| kind.name() == member.kind)?,
        instance: u32::try_from(member.number).ok()?,
        status: status(&member.state),
    })
}

pub fn board(session: &Session) -> AgentBoard {
    let now = latest(session);
    let since = |at: &Option<String>| {
        let at = at.as_deref().and_then(seconds).unwrap_or(now);
        now.saturating_sub(at) as f32 / SECONDS_PER_MINUTE
    };
    let mut lines = Vec::new();
    let mut events = Vec::new();
    for (id, member) in &session.agents {
        let Some(agent) = shown(member) else {
            continue;
        };
        let tools: Vec<(&String, &Tool)> = session
            .tools
            .iter()
            .filter(|(_, tool)| tool.agent.as_deref() == Some(id.as_str()))
            .collect();
        let asked = session
            .approvals
            .values()
            .find(|(_, asked)| asked.agent.as_deref() == Some(id.as_str()))
            .map(|(_, asked)| format!("{} {}", asked.tool, argument(&asked.args)));
        let edited: Vec<Vec<&FileEdit>> = session
            .files
            .values()
            .map(|edits| {
                edits
                    .iter()
                    .filter(|edit| edit.agent.as_deref() == Some(id.as_str()))
                    .collect::<Vec<_>>()
            })
            .filter(|mine| !mine.is_empty())
            .collect();
        let changed = |kind: HunkLineKind| {
            edited
                .iter()
                .flatten()
                .flat_map(|edit| &edit.hunks)
                .flat_map(|hunk| &hunk.lines)
                .filter(|line| line.kind == kind)
                .count()
        };
        let report = member.report.clone().unwrap_or_default();
        let (now_doing, time) = match agent.status {
            AgentStatus::Working => (
                tools
                    .iter()
                    .max_by(|(_, a), (_, b)| a.started_at.cmp(&b.started_at))
                    .map_or_else(|| "starting".to_owned(), |(_, tool)| doing(tool)),
                lasted(since(&member.started_at)),
            ),
            AgentStatus::Asking => (
                format!("asked: {}", asked.clone().unwrap_or_default()),
                ago(since(
                    &member.ended_at.clone().or(member.started_at.clone()),
                )),
            ),
            AgentStatus::Failed => (report.clone(), ago(since(&member.ended_at))),
            AgentStatus::Finished => (
                report.clone(),
                lasted(since(&member.started_at) - since(&member.ended_at)),
            ),
        };
        let mut steps = vec![(
            since(&member.started_at),
            AgentStep::Spawn {
                task: member.task.clone().into(),
                owns: member.owns.join(", ").into(),
            },
        )];
        steps.extend(tools.iter().filter_map(|(item, tool)| {
            let edit = session.files.values().flatten().find(|e| &e.item == *item);
            Some((since(&tool.started_at), step(tool, edit)?))
        }));
        let last = match agent.status {
            AgentStatus::Working => None,
            AgentStatus::Asking => Some(AgentStep::Ask {
                question: asked.unwrap_or_default().into(),
            }),
            AgentStatus::Failed => Some(AgentStep::Fail {
                text: report.into(),
            }),
            AgentStatus::Finished => Some(AgentStep::Report {
                text: report.into(),
                worked: format!("worked {time}").into(),
            }),
        };
        steps.extend(last.map(|last| (since(&member.ended_at), last)));
        events.extend(steps.into_iter().map(|(minutes, step)| AgentEvent {
            agent,
            minutes,
            step,
        }));
        lines.push(AgentLine {
            agent,
            task: member.task.clone().into(),
            now: now_doing.into(),
            time,
            owns: member.owns.join(", ").into(),
            thinking: member.thinking.clone().into(),
            added: changed(HunkLineKind::Added),
            removed: changed(HunkLineKind::Removed),
            files: edited.len(),
            tools: tools.len(),
        });
    }
    events.reverse();
    events.sort_by(|a, b| a.minutes.total_cmp(&b.minutes));
    let attributed = session.tools.values().any(|tool| tool.agent.is_some())
        || session
            .files
            .values()
            .flatten()
            .any(|edit| edit.agent.is_some());
    AgentBoard {
        lines,
        events,
        attributed,
    }
}

fn step(tool: &Tool, edit: Option<&FileEdit>) -> Option<AgentStep> {
    let arg = |key: &str| -> SharedString {
        tool.args
            .get(key)
            .and_then(Value::as_str)
            .unwrap_or_default()
            .to_owned()
            .into()
    };
    let output = tool.output.as_deref().unwrap_or_default();
    let counted = SharedString::from(format!("{} lines", output.lines().count()));
    Some(match tool.name.as_str() {
        "read" => AgentStep::Read {
            path: arg("path"),
            lines: counted,
        },
        "edit" | "write" => {
            let hunk = edit.map_or_else(Vec::new, diff);
            let count = |sign| {
                u32::try_from(hunk.iter().filter(|line| line.sign == sign).count())
                    .unwrap_or(u32::MAX)
            };
            AgentStep::Edit {
                path: arg("path"),
                added: count(DiffSign::Added),
                removed: count(DiffSign::Removed),
                hunk,
            }
        }
        "bash" => AgentStep::Bash {
            command: arg("command"),
            exit: i32::from(tool.failed),
            output: output
                .lines()
                .skip(output.lines().count().saturating_sub(OUTPUT_LINES))
                .map(|line| line.to_owned().into())
                .collect(),
        },
        "search" | "grep" | "glob" => AgentStep::Grep {
            query: arg("pattern"),
            scope: arg("path"),
            hits: counted,
            files: Vec::new(),
        },
        "web_search" => AgentStep::Web {
            query: arg("query"),
            results: Vec::new(),
        },
        "fetch" | "web_fetch" => AgentStep::Fetch {
            url: arg("url"),
            title: SharedString::default(),
            size: counted,
        },
        name if name.starts_with("browser_") => AgentStep::Browse {
            action: name.to_owned().into(),
            target: argument(&tool.args).into(),
            result: output.lines().next().unwrap_or_default().to_owned().into(),
        },
        _ => return None,
    })
}

fn diff(edit: &FileEdit) -> Vec<DiffLine> {
    let mut lines = Vec::new();
    for hunk in &edit.hunks {
        let (mut old, mut new) = (hunk.old_start, hunk.new_start);
        for line in &hunk.lines {
            let (sign, number) = match line.kind {
                HunkLineKind::Added => (DiffSign::Added, &mut new),
                HunkLineKind::Removed => (DiffSign::Removed, &mut old),
                HunkLineKind::Context | HunkLineKind::Unknown(_) => {
                    old = old.saturating_add(1);
                    (DiffSign::Context, &mut new)
                }
            };
            lines.push(DiffLine {
                number: u32::try_from(*number).unwrap_or_default(),
                sign,
                text: line.text.clone().into(),
            });
            *number = number.saturating_add(1);
        }
    }
    lines
}

fn doing(tool: &Tool) -> String {
    let verb = match tool.name.as_str() {
        "read" | "fetch" | "web_fetch" => "reading",
        "edit" | "write" => "editing",
        "bash" => "running",
        "search" | "grep" | "glob" | "web_search" => "searching",
        name if name.starts_with("browser_") => "browsing",
        name => name,
    };
    format!("{verb} {}", argument(&tool.args))
}

fn argument(args: &Value) -> String {
    ARGUMENT_KEYS
        .iter()
        .find_map(|key| args.get(key)?.as_str())
        .map_or_else(|| args.to_string(), str::to_owned)
}

fn latest(session: &Session) -> i64 {
    let agents = session
        .agents
        .values()
        .flat_map(|agent| [&agent.started_at, &agent.ended_at]);
    let tools = session
        .tools
        .values()
        .flat_map(|tool| [&tool.started_at, &tool.ended_at]);
    agents
        .chain(tools)
        .filter_map(|at| seconds(at.as_deref()?))
        .max()
        .unwrap_or_default()
}

fn seconds(at: &str) -> Option<i64> {
    let field = |range: std::ops::Range<usize>| at.get(range)?.parse::<i64>().ok();
    let (year, month, day) = (field(0..4)?, field(5..7)?, field(8..10)?);
    let (hour, minute, second) = (field(11..13)?, field(14..16)?, field(17..19)?);
    let year = if month <= 2 { year - 1 } else { year };
    let era = year.div_euclid(400);
    let of_era = year - era * 400;
    let of_year = (153 * ((month + 9) % 12) + 2) / 5 + day - 1;
    let days = era * 146_097 + of_era * 365 + of_era / 4 - of_era / 100 + of_year - 719_468;
    Some(((days * 24 + hour) * 60 + minute) * 60 + second)
}
