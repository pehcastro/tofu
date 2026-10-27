use std::collections::BTreeMap;
use std::path::PathBuf;

use super::ModelError;
use crate::protocol::{
    AccountStateChanged, AgentNow, AgentState, ApprovalRequest, CronState, DecisionMade, FileEdit,
    FileEditOp, GroupSummary, Member, MessageImage, ModelPick, Notification, Origin, OriginKind,
    OriginSub, PlanStep, QuestionRequest, QuotaWindow, RequestId, RunningTurn, SessionForked,
    SettingsChanged, ShellHolder, ShellKept, ShellNow, ShellNowState, ShellReady, ToolCard,
    ToolSummary, TurnAccount, TurnCompletedStatus, TurnDigest, UsageUpdated,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Role {
    User,
    Inbox,
    Cron { job: String, schedule: String },
    Assistant,
    Thinking,
    Steer,
    Queued(String),
    Taken,
    Note,
    Failure,
    Harness(OriginSub),
}

#[derive(Debug, Clone)]
pub struct Message {
    pub turn: String,
    pub agent: Option<String>,
    pub role: Role,
    pub text: String,
    pub images: Vec<(u32, PathBuf)>,
    pub complete: bool,
    pub at: Option<String>,
}

#[derive(Debug, Clone)]
pub struct Turn {
    pub task: String,
    pub images: Vec<(u32, PathBuf)>,
    pub origin: Origin,
    pub started_at: String,
    pub status: Option<TurnCompletedStatus>,
    pub lead_idle: bool,
    pub worked_for_ms: i64,
    pub account: Option<TurnAccount>,
    pub digest: Option<TurnDigest>,
}

#[derive(Debug, Clone)]
pub struct Tool {
    pub name: String,
    pub title: Option<String>,
    pub agent: Option<String>,
    pub instance: Option<String>,
    pub mention: Option<String>,
    pub args: serde_json::Value,
    pub output: Option<String>,
    pub summary: Option<ToolSummary>,
    pub card: Option<ToolCard>,
    pub group: Option<String>,
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
    pub ticket: Option<String>,
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
    pub output_end: i64,
    pub released_at: Option<i64>,
}

impl Shell {
    pub fn output_at(&self, offset: i64) -> usize {
        let behind = self
            .output_end
            .checked_sub(offset)
            .and_then(|behind| usize::try_from(behind).ok())
            .unwrap_or(0);
        let at = self.output.len().saturating_sub(behind);
        (at..=self.output.len())
            .find(|&at| self.output.is_char_boundary(at))
            .unwrap_or(self.output.len())
    }

    fn append(&mut self, offset: i64, text: &str) {
        let skip = self
            .released_at
            .and_then(|released| released.checked_sub(offset))
            .and_then(|skip| usize::try_from(skip).ok())
            .unwrap_or(0)
            .min(text.len());
        let skip = (skip..=text.len())
            .find(|&at| text.is_char_boundary(at))
            .unwrap_or(text.len());
        self.output.push_str(text.get(skip..).unwrap_or_default());
        let end = i64::try_from(text.len())
            .ok()
            .and_then(|length| offset.checked_add(length))
            .unwrap_or(i64::MAX);
        self.output_end = self.output_end.max(end);
    }
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
    pub stretches: BTreeMap<String, GroupSummary>,
    pub agents: BTreeMap<String, Agent>,
    pub running: BTreeMap<String, AgentState>,
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
    pub members: Vec<Member>,
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
        OriginKind::Agent => Ok(Role::Inbox),
        OriginKind::Person => Ok(Role::User),
        OriginKind::Tofu
        | OriginKind::Hook
        | OriginKind::Memory
        | OriginKind::Skill
        | OriginKind::Library
        | OriginKind::Plugin => Ok(Role::Harness(origin.sub.clone())),
        OriginKind::Unknown(value) => Err(ModelError::UnknownValue {
            field: "origin kind",
            value: value.clone(),
        }),
    }
}

fn attached(images: &[MessageImage]) -> Result<Vec<(u32, PathBuf)>, ModelError> {
    images
        .iter()
        .map(|image| {
            let n = u32::try_from(image.n).map_err(|_| ModelError::UnknownValue {
                field: "image number",
                value: image.n.to_string(),
            })?;
            Ok((n, PathBuf::from(&image.path)))
        })
        .collect()
}

fn stamp<Zone: chrono::TimeZone>(at: &chrono::DateTime<Zone>) -> String
where
    Zone::Offset: std::fmt::Display,
{
    at.to_rfc3339_opts(chrono::SecondsFormat::AutoSi, true)
}

fn earlier(known: &str, said: &str) -> bool {
    let at = chrono::DateTime::parse_from_rfc3339;
    matches!((at(known), at(said)), (Ok(known), Ok(said)) if known < said)
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
                    output_end: 0,
                    released_at: None,
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

    pub(super) fn agents_now(&mut self, now: &[AgentNow]) -> Result<(), ModelError> {
        self.running.clear();
        for live in now {
            let state = known(&live.state)?;
            self.running.insert(live.instance.clone(), state.clone());
            let agent = self
                .agents
                .entry(live.instance.clone())
                .or_insert_with(|| Agent {
                    number: 0,
                    kind: live.kind.clone(),
                    model: live.model.clone(),
                    task: live.task.clone(),
                    owns: live.owns.clone(),
                    state: state.clone(),
                    report: None,
                    steps: 0,
                    tokens: 0,
                    started_at: Some(live.started_at.clone()),
                    ended_at: None,
                    thinking: String::new(),
                    mention: None,
                    ticket: None,
                });
            agent.state = state;
            agent.ended_at = None;
            agent.steps = live.steps;
            agent.tokens = live.tokens;
        }
        Ok(())
    }

    fn held(&self, instance: &str, state: AgentState) -> AgentState {
        match (state, self.running.get(instance)) {
            (AgentState::Parked, Some(running)) => running.clone(),
            (state, _) => state,
        }
    }

    pub fn reset_transcript(&mut self) {
        self.messages.clear();
        self.tools.clear();
        self.stretches.clear();
        self.agents.clear();
        self.files.clear();
        self.decisions.clear();
        self.plan.clear();
    }

    pub(super) fn running_now(&mut self, turn: &RunningTurn) {
        self.turns.entry(turn.id.clone()).or_insert_with(|| Turn {
            task: turn.task.clone(),
            images: Vec::new(),
            origin: Origin {
                job: None,
                kind: OriginKind::Tofu,
                name: None,
                schedule: None,
                sub: OriginSub::Continue,
                id: None,
                exit: None,
                duration_ms: None,
            },
            started_at: turn.started_at.clone(),
            status: None,
            lead_idle: false,
            worked_for_ms: 0,
            account: None,
            digest: None,
        });
    }

    pub fn queue(&mut self, turn: &str, item: &str, text: &str, images: Vec<(u32, PathBuf)>) {
        let queued = Role::Queued(item.to_owned());
        if !self.messages.iter().any(|m| m.role == queued) {
            self.say(turn, &None, queued, text, false);
            if let Some(message) = self.messages.last_mut() {
                message.images = images;
            }
        }
    }

    pub fn taken(&mut self, item: &str) {
        self.settle(item, Role::Taken, &mut Vec::new());
    }

    fn settle(&mut self, item: &str, role: Role, images: &mut Vec<(u32, PathBuf)>) -> bool {
        let queued = Role::Queued(item.to_owned());
        let Some(message) = self.messages.iter_mut().find(|m| m.role == queued) else {
            return false;
        };
        if message.images.is_empty() {
            message.images = std::mem::take(images);
        }
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
            images: Vec::new(),
            complete,
            at: None,
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
                let started_at = self
                    .turns
                    .get(&e.turn)
                    .map(|known| known.started_at.as_str())
                    .filter(|known| earlier(known, &e.started_at))
                    .unwrap_or(&e.started_at)
                    .to_owned();
                self.turns.insert(
                    e.turn.clone(),
                    Turn {
                        task: e.task.clone(),
                        images: attached(&e.images)?,
                        origin: e.origin.clone(),
                        started_at,
                        status: None,
                        lead_idle: false,
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
                self.messages.push(Message {
                    turn: e.turn.clone(),
                    agent: e.agent.clone(),
                    role: speaker(&e.origin)?,
                    text: e.text.clone(),
                    images: attached(&e.images)?,
                    complete: true,
                    at: e.sent_at.clone(),
                });
            }
            N::TurnSteered(e) => {
                let mut images = attached(&e.images)?;
                if !self.settle(&e.item, Role::Steer, &mut images) {
                    self.say(&e.turn, &e.agent, Role::Steer, &e.text, true);
                    if let Some(message) = self.messages.last_mut() {
                        message.images = images;
                    }
                }
                if let Some(steer) = self
                    .messages
                    .iter_mut()
                    .rev()
                    .find(|m| m.role == Role::Steer && m.text == e.text)
                {
                    steer.at = Some(e.read_at.clone());
                }
            }
            N::TurnSteerQueued(e) if e.agent.is_none() => {
                self.queue(&e.turn, &e.item, &e.text, attached(&e.images)?);
            }
            N::Note(e) => self.say(&e.turn, &e.agent, Role::Note, &e.text, true),
            N::Failure(e) => self.say(&e.turn, &e.agent, Role::Failure, &e.text, true),
            N::MessageStarted(e) => {
                self.say(&e.turn, &e.agent, Role::Assistant, "", false);
                if let Some(message) = self.messages.last_mut() {
                    message.at.clone_from(&e.at);
                }
            }
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
                        title: e.title.clone(),
                        agent: e.agent.clone(),
                        instance: e.instance.clone(),
                        mention: e.r#ref.clone(),
                        args: e.args.clone(),
                        output: None,
                        summary: None,
                        card: e.card.clone(),
                        group: e.group.clone(),
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
                tool.summary = Some(e.summary.clone());
                tool.failed = e.failed;
                if e.card.is_some() {
                    tool.card.clone_from(&e.card);
                }
                if e.group.is_some() {
                    tool.group.clone_from(&e.group);
                }
                if let (Some(group), Some(summary)) = (&tool.group, &e.group_summary) {
                    self.stretches.insert(group.clone(), summary.clone());
                }
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
                        state: self.held(&e.instance, known(&e.state)?),
                        report: None,
                        steps: 0,
                        tokens: 0,
                        started_at: Some(e.started_at.clone()),
                        ended_at: None,
                        thinking: String::new(),
                        mention: e.r#ref.clone(),
                        ticket: e.ticket.clone(),
                    },
                );
            }
            N::AgentUpdated(e) => {
                let state = e
                    .state
                    .as_ref()
                    .map(|state| Ok(self.held(&e.instance, known(state)?)))
                    .transpose()?;
                let agent = self
                    .agents
                    .get_mut(&e.instance)
                    .ok_or_else(|| orphan("agent.updated", &e.instance))?;
                if let Some(state) = state {
                    agent.state = state;
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
                self.running.remove(&e.instance);
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
                        output_end: 0,
                        released_at: None,
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
                shell.released_at = (shell.holder != ShellHolder::Person).then_some(e.offset);
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
                shell.append(e.offset, &e.text);
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
            N::ApprovalRevoked(e) => self
                .approvals
                .retain(|_, (_, asked)| asked.target != e.target || asked.agent != e.agent),
            N::ModelsChanged(_) => {}
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
            N::SessionMember(e) => self.members.clone_from(&e.members),
            N::QuestionResolved(e) => {
                self.questions
                    .remove(&e.question)
                    .ok_or_else(|| orphan("question.resolved", &e.question))?;
            }
            N::ItemPersisted(e) if e.agent.is_none() => {
                if let Some(turn) = self.turns.get_mut(&e.turn) {
                    match e.kind.as_str() {
                        "turn_start" => turn.lead_idle = false,
                        "turn_end" => turn.lead_idle = true,
                        _ => {}
                    }
                }
            }
            N::ItemPersisted(_)
            | N::SessionListed(_)
            | N::Status(_)
            | N::MemoryScoped(_)
            | N::TurnSteerQueued(_)
            | N::RequestSent(_) => {}
            N::SessionForked(e) => self.forks.push((**e).clone()),
            N::Unknown { method, .. } => return Err(ModelError::UnknownEvent(method.clone())),
        }
        Ok(())
    }
}
