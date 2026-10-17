use std::collections::BTreeMap;

use super::ModelError;
use crate::protocol::{
    AccountStateChanged, AgentState, ApprovalRequest, CronState, DecisionMade, FileEdit,
    FileEditOp, ModelPick, Notification, Origin, OriginKind, PlanStep, QuestionRequest,
    QuotaWindow, RequestId, RunningTurn, SessionForked, SettingsChanged, ShellHolder, ShellKept,
    ShellNow, ShellNowState, ShellReady, TurnAccount, TurnCompletedStatus, TurnDigest,
    UsageUpdated,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Role {
    User,
    Cron { job: String, schedule: String },
    Assistant,
    Thinking,
    Steer,
    Queued(String),
    Taken,
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
    pub origin: Origin,
    pub started_at: String,
    pub status: Option<TurnCompletedStatus>,
    pub worked_for_ms: i64,
    pub account: Option<TurnAccount>,
    pub digest: Option<TurnDigest>,
}

#[derive(Debug, Clone)]
pub struct Tool {
    pub name: String,
    pub agent: Option<String>,
    pub instance: Option<String>,
    pub mention: Option<String>,
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
    pub mention: Option<String>,
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
    pub ended_at: Option<String>,
    pub first_seen: std::time::Instant,
    pub port: Option<u16>,
    pub dir: String,
    pub kept: Option<ShellKept>,
    pub ready: Option<ShellReady>,
    pub left_over: bool,
    pub mention: Option<String>,
    pub terminal: bool,
    pub holder: ShellHolder,
    pub held_since: Option<String>,
    pub raw: Option<Vec<(i64, String)>>,
}

fn holder(holder: Option<&ShellHolder>) -> Result<ShellHolder, ModelError> {
    match holder {
        None | Some(ShellHolder::Agent) => Ok(ShellHolder::Agent),
        Some(ShellHolder::Person) => Ok(ShellHolder::Person),
        Some(ShellHolder::Unknown(value)) => Err(ModelError::UnknownValue {
            field: "shell holder",
            value: value.clone(),
        }),
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PersonKind {
    Choice,
    Multi,
    Text,
    YesNo,
}

#[derive(Debug, Clone)]
pub struct PersonAsk {
    pub id: RequestId,
    pub request: QuestionRequest,
    pub kinds: Vec<PersonKind>,
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
    pub questions: BTreeMap<String, PersonAsk>,
    pub plan: Vec<PlanStep>,
    pub quota: Vec<QuotaWindow>,
    pub context: Option<(i64, i64)>,
    pub usage: Vec<UsageUpdated>,
    pub cron: Option<CronState>,
    pub pick: Option<ModelPick>,
    pub dropped: i64,
    pub account: Option<AccountStateChanged>,
    pub settings: BTreeMap<String, SettingsChanged>,
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

fn speaker(origin: &Origin) -> Result<Role, ModelError> {
    match &origin.kind {
        OriginKind::Cron => Ok(Role::Cron {
            job: origin.job.clone().unwrap_or_default(),
            schedule: origin.schedule.clone().unwrap_or_default(),
        }),
        OriginKind::Person | OriginKind::Agent | OriginKind::Tofu => Ok(Role::User),
        OriginKind::Unknown(value) => Err(ModelError::UnknownValue {
            field: "origin kind",
            value: value.clone(),
        }),
    }
}

fn stamp<Zone: chrono::TimeZone>(at: &chrono::DateTime<Zone>) -> String
where
    Zone::Offset: std::fmt::Display,
{
    at.to_rfc3339_opts(chrono::SecondsFormat::AutoSi, true)
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

    pub(super) fn question(
        &mut self,
        id: RequestId,
        request: QuestionRequest,
    ) -> Result<(), ModelError> {
        let kinds = request
            .questions
            .iter()
            .map(|asked| match asked.r#type.as_str() {
                "choice" => Ok(PersonKind::Choice),
                "multi" => Ok(PersonKind::Multi),
                "text" => Ok(PersonKind::Text),
                "yesno" => Ok(PersonKind::YesNo),
                other => Err(ModelError::UnknownValue {
                    field: "question type",
                    value: other.to_owned(),
                }),
            })
            .collect::<Result<_, _>>()?;
        let ask = PersonAsk { id, request, kinds };
        self.questions.insert(ask.request.question.clone(), ask);
        Ok(())
    }

    pub(super) fn shells_now(&mut self, now: &[ShellNow]) -> Result<(), ModelError> {
        for later in now {
            let shell = self
                .shells
                .entry(later.shell.clone())
                .or_insert_with(|| Shell {
                    command: later.command.clone(),
                    agent: None,
                    output: String::new(),
                    exit_code: None,
                    killed: false,
                    exited: false,
                    pid: Some(later.pid),
                    started_at: Some(later.started_at.clone()),
                    ended_at: None,
                    first_seen: std::time::Instant::now(),
                    port: None,
                    dir: String::new(),
                    kept: None,
                    ready: None,
                    left_over: false,
                    mention: None,
                    terminal: false,
                    holder: ShellHolder::Agent,
                    held_since: None,
                    raw: None,
                });
            match &later.state {
                ShellNowState::Running => {}
                ShellNowState::Exited => shell.exited = true,
                ShellNowState::Killed => {
                    shell.exited = true;
                    shell.killed = true;
                }
                ShellNowState::Unknown(value) => {
                    return Err(ModelError::UnknownValue {
                        field: "shell state",
                        value: value.clone(),
                    });
                }
            }
            if shell.exited {
                shell.exit_code = shell.exit_code.or(later.exit_code);
                shell.ended_at = shell.ended_at.take().or_else(|| later.ended_at.clone());
            }
            shell.dir.clone_from(&later.dir);
            shell.kept.clone_from(&later.kept);
            shell.ready.clone_from(&later.ready);
            shell.left_over = later.left_over.unwrap_or_default();
            shell.port = later.port.and_then(|port| u16::try_from(port).ok());
            shell.terminal = later.terminal.unwrap_or_default();
            shell.holder = holder(later.holder.as_ref())?;
            shell.held_since.clone_from(&later.held_since);
        }
        Ok(())
    }

    pub fn reset_transcript(&mut self) {
        self.messages.clear();
        self.tools.clear();
        self.agents.clear();
        self.files.clear();
        self.decisions.clear();
        self.plan.clear();
    }

    pub(super) fn running_now(&mut self, turn: &RunningTurn) {
        self.turns.entry(turn.id.clone()).or_insert_with(|| Turn {
            task: turn.task.clone(),
            origin: Origin {
                job: None,
                kind: OriginKind::Tofu,
                name: None,
                schedule: None,
                source: None,
            },
            started_at: turn.started_at.clone(),
            status: None,
            worked_for_ms: 0,
            account: None,
            digest: None,
        });
    }

    pub fn queue(&mut self, turn: &str, item: &str, text: &str) {
        let queued = Role::Queued(item.to_owned());
        if !self.messages.iter().any(|m| m.role == queued) {
            self.say(turn, &None, queued, text, false);
        }
    }

    pub fn taken(&mut self, item: &str) {
        self.settle(item, Role::Taken);
    }

    fn settle(&mut self, item: &str, role: Role) -> bool {
        let queued = Role::Queued(item.to_owned());
        let Some(message) = self.messages.iter_mut().find(|m| m.role == queued) else {
            return false;
        };
        message.role = role;
        message.complete = true;
        true
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

    fn last_seen(&mut self, instance: &str) {
        let Some(agent) = self.agents.get_mut(instance) else {
            return;
        };
        if agent.report.is_none() {
            return;
        }
        let tools = self
            .tools
            .values()
            .filter(|tool| tool.agent.as_deref() == Some(instance))
            .flat_map(|tool| tool.started_at.iter().chain(&tool.ended_at));
        agent.ended_at = agent
            .started_at
            .iter()
            .chain(tools)
            .filter_map(|at| Some((chrono::DateTime::parse_from_rfc3339(at).ok()?, at)))
            .max_by_key(|(parsed, _)| *parsed)
            .map(|(_, at)| at.clone());
    }

    pub(super) fn apply(&mut self, notification: &Notification) -> Result<(), ModelError> {
        use Notification as N;
        match notification {
            N::SessionUpdated(e) => {
                self.name.clone_from(&e.name);
                self.root.clone_from(&e.root);
            }
            N::TurnStarted(e) => {
                speaker(&e.origin)?;
                self.turns.insert(
                    e.turn.clone(),
                    Turn {
                        task: e.task.clone(),
                        origin: e.origin.clone(),
                        started_at: e.started_at.clone(),
                        status: None,
                        worked_for_ms: 0,
                        account: None,
                        digest: None,
                    },
                );
            }
            N::TurnAccount(e) => {
                self.turns
                    .get_mut(&e.turn)
                    .ok_or_else(|| orphan("turn.account", &e.turn))?
                    .account = Some((**e).clone());
            }
            N::SessionTurnsUpdated(e) => {
                if let Some(turn) = self.turns.get_mut(&e.turn) {
                    turn.digest = Some(e.digest.clone());
                }
            }
            N::AccountState(e) => self.account = Some((**e).clone()),
            N::SettingsChanged(e) => {
                self.settings.insert(e.key.clone(), (**e).clone());
            }
            N::ShellReady(e) => {
                let shell = self
                    .shells
                    .get_mut(&e.shell)
                    .ok_or_else(|| orphan("shell.ready", &e.shell))?;
                shell.port = u16::try_from(e.port).ok();
                if e.r#ref.is_some() {
                    shell.mention.clone_from(&e.r#ref);
                }
            }
            N::BoardyChanged(_) | N::ScratchChanged(_) => {}
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
            N::MessageUser(e) => {
                let role = speaker(&e.origin)?;
                self.say(&e.turn, &e.agent, role, &e.text, true);
            }
            N::TurnSteered(e) => {
                if !self.settle(&e.item, Role::Steer) {
                    self.say(&e.turn, &e.agent, Role::Steer, &e.text, true);
                }
            }
            N::TurnSteerQueued(e) if e.agent.is_none() => self.queue(&e.turn, &e.item, &e.text),
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
                        instance: e.instance.clone(),
                        mention: e.r#ref.clone(),
                        args: e.args.clone(),
                        output: None,
                        failed: false,
                        started_at: Some(e.started_at.clone()),
                        ended_at: None,
                    },
                );
                if let Some(agent) = &e.agent {
                    self.last_seen(agent);
                }
            }
            N::ToolCompleted(e) => {
                let tool = self
                    .tools
                    .get_mut(&e.item)
                    .ok_or_else(|| orphan("tool.completed", &e.item))?;
                tool.output = Some(e.output.clone());
                tool.failed = e.failed;
                tool.ended_at = tool
                    .started_at
                    .as_deref()
                    .and_then(|at| chrono::DateTime::parse_from_rfc3339(at).ok())
                    .and_then(|at| {
                        at.checked_add_signed(chrono::TimeDelta::milliseconds(e.duration_ms))
                    })
                    .map(|at| stamp(&at));
                if let Some(agent) = tool.agent.clone() {
                    self.last_seen(&agent);
                }
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
                        mention: e.r#ref.clone(),
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
                agent.ended_at = Some(e.ended_at.clone());
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
                        ended_at: None,
                        first_seen: std::time::Instant::now(),
                        port: e.port.and_then(|port| u16::try_from(port).ok()),
                        dir: e.dir.clone(),
                        kept: e.kept.clone(),
                        ready: e.ready.clone(),
                        left_over: e.left_over.unwrap_or_default(),
                        mention: e.r#ref.clone(),
                        terminal: e.terminal.unwrap_or_default(),
                        holder: holder(e.holder.as_ref())?,
                        held_since: e.held_since.clone(),
                        raw: None,
                    },
                );
            }
            N::ShellHolder(e) => {
                let shell = self
                    .shells
                    .get_mut(&e.shell)
                    .ok_or_else(|| orphan("shell.holder", &e.shell))?;
                shell.holder = holder(Some(&e.holder))?;
                shell.held_since = (shell.holder == ShellHolder::Person).then(|| e.since.clone());
            }
            N::ShellRaw(e) => {
                let shell = self
                    .shells
                    .get_mut(&e.shell)
                    .ok_or_else(|| orphan("shell.raw", &e.shell))?;
                if let Some(raw) = &mut shell.raw {
                    raw.push((e.offset, e.data.clone()));
                }
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
                shell.ended_at.clone_from(&e.ended_at);
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
            N::CronUpdated(e) => {
                self.cron = Some(CronState {
                    live: e.live,
                    goals: e.goals,
                    jobs: e.jobs.clone(),
                });
            }
            N::Resync(e) => self.dropped = self.dropped.saturating_add(e.dropped),
            N::SessionSettings(e) => self.pick = Some(e.pick.clone()),
            N::QuestionResolved(e) => {
                self.questions
                    .remove(&e.question)
                    .ok_or_else(|| orphan("question.resolved", &e.question))?;
            }
            N::ItemPersisted(_)
            | N::SessionListed(_)
            | N::Status(_)
            | N::MemoryScoped(_)
            | N::TurnSteerQueued(_) => {}
            N::SessionForked(e) => self.forks.push((**e).clone()),
            N::Unknown { method, .. } => return Err(ModelError::UnknownEvent(method.clone())),
        }
        Ok(())
    }
}
