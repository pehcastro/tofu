use std::collections::BTreeMap;

use super::ModelError;
use crate::protocol::{
    AgentState, ApprovalRequest, DecisionMade, FileEdit, FileEditOp, Notification, PlanStep,
    QuotaWindow, RequestId, SessionForked, TurnCompletedStatus, UsageUpdated,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Role {
    User,
    Assistant,
    Thinking,
    Steer,
    Note,
    Failure,
}

#[derive(Debug, Clone)]
pub struct Message {
    pub turn: String,
    pub agent: Option<String>,
    pub role: Role,
    pub text: String,
    pub complete: bool,
}

#[derive(Debug, Clone)]
pub struct Turn {
    pub task: String,
    pub started_at: String,
    pub status: Option<TurnCompletedStatus>,
    pub worked_for_ms: i64,
}

#[derive(Debug, Clone)]
pub struct Tool {
    pub name: String,
    pub agent: Option<String>,
    pub args: serde_json::Value,
    pub output: Option<String>,
    pub failed: bool,
    pub started_at: Option<String>,
    pub ended_at: Option<String>,
}

#[derive(Debug, Clone)]
pub struct Agent {
    pub number: i64,
    pub kind: String,
    pub model: String,
    pub task: String,
    pub owns: Vec<String>,
    pub state: AgentState,
    pub report: Option<String>,
    pub steps: i64,
    pub tokens: i64,
    pub started_at: Option<String>,
    pub ended_at: Option<String>,
    pub thinking: String,
}

#[derive(Debug, Clone)]
pub struct Shell {
    pub command: String,
    pub agent: Option<String>,
    pub output: String,
    pub exit_code: Option<i64>,
    pub killed: bool,
    pub exited: bool,
    pub pid: Option<i64>,
    pub started_at: Option<String>,
    pub port: Option<u16>,
}

#[derive(Debug, Default)]
pub struct Session {
    pub name: String,
    pub root: String,
    pub forked_from: Option<String>,
    pub forks: Vec<SessionForked>,
    pub turns: BTreeMap<String, Turn>,
    pub messages: Vec<Message>,
    pub tools: BTreeMap<String, Tool>,
    pub agents: BTreeMap<String, Agent>,
    pub files: BTreeMap<String, Vec<FileEdit>>,
    pub shells: BTreeMap<String, Shell>,
    pub decisions: Vec<DecisionMade>,
    pub approvals: BTreeMap<String, (RequestId, ApprovalRequest)>,
    pub plan: Vec<PlanStep>,
    pub quota: Vec<QuotaWindow>,
    pub context: Option<(i64, i64)>,
    pub usage: Vec<UsageUpdated>,
    pub dropped: i64,
}

fn known(state: &AgentState) -> Result<AgentState, ModelError> {
    match state {
        AgentState::Unknown(value) => Err(ModelError::UnknownValue {
            field: "agent state",
            value: value.clone(),
        }),
        state => Ok(state.clone()),
    }
}

fn orphan(event: &'static str, id: &str) -> ModelError {
    ModelError::Orphan {
        event,
        id: id.to_owned(),
    }
}

impl Session {
    pub(super) fn ask(&mut self, id: RequestId, asked: ApprovalRequest) {
        self.approvals.insert(asked.approval.clone(), (id, asked));
    }

    fn say(&mut self, turn: &str, agent: &Option<String>, role: Role, text: &str, complete: bool) {
        self.messages.push(Message {
            turn: turn.to_owned(),
            agent: agent.clone(),
            role,
            text: text.to_owned(),
            complete,
        });
    }

    fn open_message(&mut self, turn: &str, agent: &Option<String>, role: Role) -> &mut Message {
        let open = self
            .messages
            .iter()
            .rposition(|m| !m.complete && m.role == role && m.turn == turn && &m.agent == agent);
        if open.is_none() {
            self.say(turn, agent, role, "", false);
        }
        let index = open.unwrap_or(self.messages.len().saturating_sub(1));
        &mut self.messages[index]
    }

    pub(super) fn apply(&mut self, notification: &Notification) -> Result<(), ModelError> {
        use Notification as N;
        match notification {
            N::SessionUpdated(e) => {
                self.name.clone_from(&e.name);
                self.root.clone_from(&e.root);
            }
            N::TurnStarted(e) => {
                self.turns.insert(
                    e.turn.clone(),
                    Turn {
                        task: e.task.clone(),
                        started_at: e.started_at.clone(),
                        status: None,
                        worked_for_ms: 0,
                    },
                );
            }
            N::TurnCompleted(e) => {
                if let TurnCompletedStatus::Unknown(value) = &e.status {
                    return Err(ModelError::UnknownValue {
                        field: "turn status",
                        value: value.clone(),
                    });
                }
                let turn = self
                    .turns
                    .get_mut(&e.turn)
                    .ok_or_else(|| orphan("turn.completed", &e.turn))?;
                turn.status = Some(e.status.clone());
                turn.worked_for_ms = e.worked_for_ms;
            }
            N::MessageUser(e) => self.say(&e.turn, &e.agent, Role::User, &e.text, true),
            N::TurnSteered(e) => self.say(&e.turn, &e.agent, Role::Steer, &e.text, true),
            N::Note(e) => self.say(&e.turn, &e.agent, Role::Note, &e.text, true),
            N::Failure(e) => self.say(&e.turn, &e.agent, Role::Failure, &e.text, true),
            N::MessageStarted(e) => self.say(&e.turn, &e.agent, Role::Assistant, "", false),
            N::MessageDelta(e) => self
                .open_message(&e.turn, &e.agent, Role::Assistant)
                .text
                .push_str(&e.text),
            N::ThinkingDelta(e) => {
                if let Some(agent) = e.agent.as_ref().and_then(|a| self.agents.get_mut(a)) {
                    agent.thinking.push_str(&e.text);
                }
                self.open_message(&e.turn, &e.agent, Role::Thinking)
                    .text
                    .push_str(&e.text);
            }
            N::MessageReset(e) => self
                .open_message(&e.turn, &e.agent, Role::Assistant)
                .text
                .clear(),
            N::MessageCompleted(e) => {
                let message = self.open_message(&e.turn, &e.agent, Role::Assistant);
                message.text.clone_from(&e.text);
                message.complete = true;
            }
            N::ToolStarted(e) => {
                self.tools.insert(
                    e.item.clone(),
                    Tool {
                        name: e.tool.clone(),
                        agent: e.agent.clone(),
                        args: e.args.clone(),
                        output: None,
                        failed: false,
                        started_at: Some(e.started_at.clone()),
                        ended_at: None,
                    },
                );
            }
            N::ToolCompleted(e) => {
                let tool = self
                    .tools
                    .get_mut(&e.item)
                    .ok_or_else(|| orphan("tool.completed", &e.item))?;
                tool.output = Some(e.output.clone());
                tool.failed = e.failed;
            }
            N::AgentStarted(e) => {
                self.agents.insert(
                    e.instance.clone(),
                    Agent {
                        number: e.number,
                        kind: e.kind.clone(),
                        model: e.model.clone(),
                        task: e.task.clone(),
                        owns: e.owns.clone(),
                        state: known(&e.state)?,
                        report: None,
                        steps: 0,
                        tokens: 0,
                        started_at: Some(e.started_at.clone()),
                        ended_at: None,
                        thinking: String::new(),
                    },
                );
            }
            N::AgentUpdated(e) => {
                let agent = self
                    .agents
                    .get_mut(&e.instance)
                    .ok_or_else(|| orphan("agent.updated", &e.instance))?;
                if let Some(state) = &e.state {
                    agent.state = known(state)?;
                }
                if let Some(model) = &e.model {
                    agent.model.clone_from(model);
                }
                if let Some(task) = &e.task {
                    agent.task.clone_from(task);
                }
                if e.report.is_some() {
                    agent.report.clone_from(&e.report);
                }
                agent.steps = e.steps.unwrap_or(agent.steps);
                agent.tokens = e.tokens.unwrap_or(agent.tokens);
            }
            N::AgentEnded(e) => {
                let agent = self
                    .agents
                    .get_mut(&e.instance)
                    .ok_or_else(|| orphan("agent.ended", &e.instance))?;
                agent.state = known(&e.state)?;
                agent.report = Some(e.report.clone());
            }
            N::FileEdit(e) => {
                if let FileEditOp::Unknown(value) = &e.op {
                    return Err(ModelError::UnknownValue {
                        field: "file edit op",
                        value: value.clone(),
                    });
                }
                self.files
                    .entry(e.path.clone())
                    .or_default()
                    .push((**e).clone());
            }
            N::ShellStarted(e) => {
                self.shells.insert(
                    e.shell.clone(),
                    Shell {
                        command: e.command.clone(),
                        agent: e.agent.clone(),
                        output: String::new(),
                        exit_code: None,
                        killed: false,
                        exited: false,
                        pid: Some(e.pid),
                        started_at: Some(e.started_at.clone()),
                        port: None,
                    },
                );
            }
            N::ShellOutput(e) => {
                let shell = self
                    .shells
                    .get_mut(&e.shell)
                    .ok_or_else(|| orphan("shell.output", &e.shell))?;
                shell.output.push_str(&e.text);
            }
            N::ShellExited(e) => {
                let shell = self
                    .shells
                    .get_mut(&e.shell)
                    .ok_or_else(|| orphan("shell.exited", &e.shell))?;
                shell.exit_code = e.exit_code;
                shell.killed = e.killed;
                shell.exited = true;
            }
            N::Decision(e) => self.decisions.push((**e).clone()),
            N::ApprovalResolved(e) => {
                self.approvals
                    .remove(&e.approval)
                    .ok_or_else(|| orphan("approval.resolved", &e.approval))?;
            }
            N::PlanUpdated(e) => self.plan.clone_from(&e.items),
            N::QuotaUpdated(e) => self.quota.clone_from(&e.windows),
            N::ContextUpdated(e) => self.context = Some((e.used, e.budget)),
            N::UsageUpdated(e) => self.usage.push((**e).clone()),
            N::Resync(e) => self.dropped = self.dropped.saturating_add(e.dropped),
            N::ItemPersisted(_) => {}
            N::SessionForked(e) => self.forks.push((**e).clone()),
            N::Unknown { method, .. } => return Err(ModelError::UnknownEvent(method.clone())),
        }
        Ok(())
    }
}
