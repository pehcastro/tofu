use desk_core::model::Tool;
use desk_core::protocol::{CardPiece, CardPieceInk, Family, ToolCompletedOutcome, ToolSummary};
use desk_ui::components::chat::{CallState, Ink, Object, ToolCard};
use desk_ui::components::chip::GitStatus;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::status_icon::TicketStatus;
use gpui::SharedString;
use serde_json::Value;

use super::mention::tool_glyph;

const TICKET: &str = "ticket_";
const HANDLE: &str = "[tool#";
const REFUSED_HEAD: &str = "error: tool call ";
const REFUSED_TAIL: &str = " was not executed";
const NOT_RUN: &str = "Not run:";
const ACCEPTANCE_MARK: &str = "- ";
const WALL_SHOWN_MS: i64 = 100;
const MANY_OBJECTS: usize = 3;
const ACCEPTED: &str = "accepted ";
const ACCEPTED_AS: &str = " as ";
const TRIAGE_HOLDS: &str = " triage holds ";
const EXITED: &str = "the command exited ";
const STILL_RUNNING: &str = " is still running";
const CREATED: &str = "created ";
const RUNNING_IN: &str = "running in ";
const RUNNING_AWAY: &str = "running in a background shell";
const SEPARATORS: [&str; 5] = ["&&", "||", ";", "|", "\n"];
const SETUP: [&str; 7] = [
    "cd",
    "pushd",
    "set",
    "export",
    "source",
    ".",
    "Set-Location",
];
const WRAPPERS: [&str; 9] = [
    "sudo", "time", "timeout", "nice", "rtk", "env", "nohup", "exec", "npx",
];
const SHELLS: [&str; 6] = ["powershell", "pwsh", "bash", "sh", "cmd", "zsh"];
const SCRIPT_FLAGS: [&str; 2] = ["-file", "-f"];
const INLINE_FLAGS: [&str; 3] = ["-command", "-c", "/c"];
const PACKAGE_FLAGS: [&str; 2] = ["-p", "--package"];
const GIT_VIEWS: [&str; 4] = ["status", "diff", "show", "log"];
const LONG_STATUS: [(&str, GitStatus); 6] = [
    ("modified:", GitStatus::Modified),
    ("new file:", GitStatus::Added),
    ("deleted:", GitStatus::Deleted),
    ("renamed:", GitStatus::Modified),
    ("typechange:", GitStatus::Modified),
    ("both modified:", GitStatus::Conflict),
];
const UNTRACKED_SECTION: &str = "Untracked files:";
const CHANGES_SECTION: &str = "Changes ";
const DIFF_HEAD: &str = "diff --git ";
const SHORT_HASH: usize = 7;
const WORD_LONGEST: usize = 32;

struct Said {
    now: String,
    done: String,
    objects: Vec<Object>,
    meta: Option<String>,
}

impl Said {
    fn new(now: &str, done: &str, objects: Vec<Object>, meta: Option<String>) -> Self {
        Said {
            now: now.to_owned(),
            done: done.to_owned(),
            objects,
            meta,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub enum Kind {
    Created,
    Moved,
    Logged,
    Board,
    Agent,
    Edit,
    Git,
    Other,
    Read,
    Search,
    List,
    Run,
    Delete,
    Refused,
}

impl Kind {
    pub fn glyph(self) -> Glyph {
        match self {
            Kind::Created | Kind::Moved | Kind::Logged | Kind::Board => Glyph::Inbox,
            Kind::Agent => Glyph::Agents,
            Kind::Edit => Glyph::Pencil,
            Kind::Git => Glyph::Branch,
            Kind::Other => Glyph::Code,
            Kind::Read => Glyph::File,
            Kind::Search => Glyph::Search,
            Kind::List => Glyph::Folder,
            Kind::Run => Glyph::Terminal,
            Kind::Delete => Glyph::Trash,
            Kind::Refused => Glyph::Attention,
        }
    }

    pub fn counted(self, count: usize) -> String {
        let (one, many) = match self {
            Kind::Created => ("ticket created", "tickets created"),
            Kind::Moved => ("moved", "moved"),
            Kind::Logged => ("logged", "logged"),
            Kind::Board => ("board call", "board calls"),
            Kind::Agent => ("agent", "agents"),
            Kind::Edit => ("edit", "edits"),
            Kind::Git => ("git", "git"),
            Kind::Other => ("other", "other"),
            Kind::Read => ("read", "reads"),
            Kind::Search => ("search", "searches"),
            Kind::List => ("list", "lists"),
            Kind::Run => ("run", "runs"),
            Kind::Delete => ("delete", "deletes"),
            Kind::Refused => ("refused", "refused"),
        };
        format!("{count} {}", if count == 1 { one } else { many })
    }
}

#[derive(Clone, Debug, PartialEq)]
pub struct TicketView {
    pub id: SharedString,
    pub status: Option<TicketStatus>,
    pub title: SharedString,
    pub said: Option<SharedString>,
    pub acceptance: usize,
}

#[derive(Clone, Debug, PartialEq)]
pub enum View {
    Output(Option<SharedString>),
    Command {
        line: SharedString,
        exit: Option<SharedString>,
        said: Option<SharedString>,
        output: Option<SharedString>,
    },
    Files(Vec<(GitStatus, SharedString)>),
    Commits(Vec<(SharedString, SharedString)>),
    Ticket(TicketView),
}

enum Invoked {
    Script(String),
    Words(Vec<String>),
}

pub fn kind(tool: &Tool) -> Kind {
    if refused(tool).is_some() {
        return Kind::Refused;
    }
    match tool.name.as_str() {
        "ticket_create" | "ticket_request" => Kind::Created,
        "ticket_move" => Kind::Moved,
        "ticket_log" => Kind::Logged,
        "triage" => Kind::Board,
        name if name.starts_with(TICKET) => Kind::Board,
        "spawn" | "subagents" | "message" => Kind::Agent,
        "edit" | "write" => Kind::Edit,
        "read" | "fetch" => Kind::Read,
        "search" | "symbols" | "web_search" => Kind::Search,
        "glob" => Kind::List,
        "shell" | "test" | "typecheck" => Kind::Run,
        "bash" => match invoked(text(&tool.args, "command")) {
            Some(Invoked::Words(words)) => match words.first().map(|word| program(word)).as_deref()
            {
                Some("git") => Kind::Git,
                Some("rg" | "grep" | "findstr" | "select-string") => Kind::Search,
                Some("ls" | "dir" | "tree" | "find" | "get-childitem") => Kind::List,
                Some("cat" | "head" | "tail" | "less" | "type" | "get-content") => Kind::Read,
                Some("rm" | "del" | "remove-item") => Kind::Delete,
                _ => Kind::Run,
            },
            Some(Invoked::Script(_)) | None => Kind::Run,
        },
        _ => Kind::Other,
    }
}

pub fn spawned(tool: &Tool) -> bool {
    tool.name == "spawn" && matches!(state(tool), CallState::Running | CallState::Done)
}

fn spoken(output: &str) -> impl Iterator<Item = &str> {
    output
        .lines()
        .map(str::trim)
        .filter(|line| !line.is_empty() && !line.starts_with(HANDLE))
}

pub fn result(output: &str) -> String {
    output
        .lines()
        .skip_while(|line| {
            let line = line.trim();
            line.is_empty() || line.starts_with(HANDLE)
        })
        .collect::<Vec<_>>()
        .join("\n")
}

fn refused(tool: &Tool) -> Option<String> {
    let first = spoken(tool.output.as_deref()?).next()?;
    let rest = first.strip_prefix(REFUSED_HEAD)?;
    let (_, reason) = rest.split_once(REFUSED_TAIL)?;
    Some(reason.trim_start_matches(':').trim().to_owned())
}

pub fn state(tool: &Tool) -> CallState {
    if refused(tool).is_some() {
        return CallState::Skipped;
    }
    match tool.outcome {
        Some(ToolCompletedOutcome::Skipped | ToolCompletedOutcome::Aborted) => {
            return CallState::Warned;
        }
        Some(ToolCompletedOutcome::Failed) => return CallState::Failed,
        Some(ToolCompletedOutcome::Ran | ToolCompletedOutcome::Unknown(_)) | None => {}
    }
    match (tool.failed, &tool.output) {
        (true, _) => CallState::Failed,
        (false, None) => CallState::Running,
        (false, Some(_)) => CallState::Done,
    }
}

pub fn card(tool: &Tool) -> ToolCard {
    let state = state(tool);
    let said = said(tool);
    let output = tool.output.as_deref().unwrap_or_default();
    let summed = tool.summary.as_ref().and_then(summed);
    let (verb, objects) = match (state, &tool.title) {
        (CallState::Skipped, _) => (NOT_RUN.to_owned(), said.objects),
        (_, Some(title)) => (title.clone(), Vec::new()),
        (CallState::Running, None) => (said.now, said.objects),
        (CallState::Done | CallState::Failed | CallState::Warned, None) => {
            (said.done, said.objects)
        }
    };
    ToolCard {
        glyphs: vec![tool_glyph(&tool.name)],
        verb: verb.into(),
        objects,
        meta: match state {
            CallState::Failed => summed.or_else(|| Some(failure(output))),
            CallState::Skipped => refused(tool),
            CallState::Warned => match tool.outcome {
                Some(ToolCompletedOutcome::Aborted) => Some("aborted".to_owned()),
                _ => Some("skipped".to_owned()),
            },
            CallState::Running | CallState::Done => {
                let parts: Vec<String> = [said.meta, summed].into_iter().flatten().collect();
                (!parts.is_empty()).then(|| parts.join(" "))
            }
        }
        .map(Into::into),
        state,
    }
}

pub fn family_glyph(family: &Family) -> Option<Glyph> {
    match family {
        Family::Explored => Some(Glyph::Search),
        Family::Edited => Some(Glyph::Pencil),
        Family::Tests => Some(Glyph::Beaker),
        Family::Git => Some(Glyph::Branch),
        Family::Board => Some(Glyph::Inbox),
        Family::Memory => Some(Glyph::Book),
        Family::Agents => Some(Glyph::Agents),
        Family::Asked => Some(Glyph::Talk),
        Family::Web => Some(Glyph::Globe),
        Family::Ran => Some(Glyph::Terminal),
        Family::Unknown(_) => None,
    }
}

pub fn inked(pieces: &[CardPiece]) -> Option<Vec<(&str, Ink)>> {
    pieces
        .iter()
        .map(|piece| {
            let tone = match &piece.ink {
                CardPieceInk::Plain => Ink::Plain,
                CardPieceInk::Muted | CardPieceInk::Failed => Ink::Muted,
                CardPieceInk::Ref => Ink::Ref,
                CardPieceInk::Added => Ink::Added,
                CardPieceInk::Removed => Ink::Removed,
                CardPieceInk::Unknown(_) => return None,
            };
            Some((piece.text.as_str(), tone))
        })
        .collect()
}

pub fn doing(tool: &Tool) -> String {
    let said = said(tool);
    said.objects
        .iter()
        .map(Object::text)
        .fold(said.now, |line, object| format!("{line} {object}"))
}

pub fn view(tool: &Tool) -> View {
    let output = tool.output.as_deref();
    if refused(tool).is_none() && tool.name.starts_with(TICKET) && tool.name != "ticket_list" {
        return View::Ticket(ticket(tool));
    }
    if tool.name == "bash" {
        let line = text(&tool.args, "command");
        return output
            .and_then(|said| git(line, said))
            .unwrap_or_else(|| ran(line, output, tool.summary.as_ref().and_then(|s| s.exit)));
    }
    View::Output(
        output
            .map(result)
            .filter(|said| !said.is_empty())
            .map(Into::into),
    )
}

fn ran(line: &str, output: Option<&str>, code: Option<i64>) -> View {
    let away = output.and_then(background);
    let exit = code.filter(|code| away.is_none() && *code != 0);
    View::Command {
        line: line.lines().next().unwrap_or_default().to_owned().into(),
        exit: exit.map(|code| format!("exit {code}").into()),
        said: match away {
            Some(away) => Some(away),
            None => output
                .and_then(|said| {
                    spoken(said)
                        .filter(|line| !line.starts_with(EXITED) && !line.starts_with('['))
                        .last()
                })
                .map(str::to_owned),
        }
        .map(Into::into),
        output: output
            .filter(|_| exit.is_some())
            .map(|said| result(said).into()),
    }
}

pub fn away(tool: &Tool) -> Option<&str> {
    let (shell, _) = spoken(tool.output.as_deref()?)
        .next()?
        .split_once(STILL_RUNNING)?;
    (!shell.contains(' ')).then_some(shell)
}

fn background(output: &str) -> Option<String> {
    let (shell, _) = spoken(output).next()?.split_once(STILL_RUNNING)?;
    Some(match shell.contains(' ') {
        true => RUNNING_AWAY.to_owned(),
        false => format!("{RUNNING_IN}{shell}"),
    })
}

fn git(line: &str, output: &str) -> Option<View> {
    let Some(Invoked::Words(words)) = invoked(line) else {
        return None;
    };
    if program(words.first()?) != "git" {
        return None;
    }
    let sub = words
        .iter()
        .skip(1)
        .find(|word| GIT_VIEWS.contains(&word.as_str()))?;
    let viewed = match sub.as_str() {
        "status" => View::Files(status_files(output)),
        "log" => View::Commits(commits(output)),
        _ => View::Files(diff_files(output)),
    };
    match &viewed {
        View::Files(files) if files.is_empty() => None,
        View::Commits(commits) if commits.is_empty() => None,
        _ => Some(viewed),
    }
}

fn short_status(code: &str) -> Option<GitStatus> {
    match code {
        "??" => Some(GitStatus::Untracked),
        "!!" => Some(GitStatus::Ignored),
        "AA" | "DD" => Some(GitStatus::Conflict),
        code if code.contains('U') => Some(GitStatus::Conflict),
        code if code.contains('D') => Some(GitStatus::Deleted),
        code if code.contains('A') => Some(GitStatus::Added),
        code if code.contains(['M', 'R', 'C', 'T']) => Some(GitStatus::Modified),
        _ => None,
    }
}

fn status_files(output: &str) -> Vec<(GitStatus, SharedString)> {
    let mut untracked = false;
    let mut files = Vec::new();
    for line in output.lines().filter(|line| !line.starts_with(HANDLE)) {
        let trimmed = line.trim();
        if line.starts_with(UNTRACKED_SECTION) {
            untracked = true;
            continue;
        }
        if line.starts_with(CHANGES_SECTION) {
            untracked = false;
            continue;
        }
        let short = (line.get(2..3) == Some(" "))
            .then(|| Some((short_status(line.get(..2)?)?, line.get(3..)?)))
            .flatten();
        let long = || {
            LONG_STATUS
                .iter()
                .find_map(|(head, status)| Some((*status, trimmed.strip_prefix(head)?.trim())))
        };
        let listed = (untracked && line.starts_with('\t') && !trimmed.is_empty())
            .then_some((GitStatus::Untracked, trimmed));
        if let Some((status, path)) = short.or_else(long).or(listed) {
            let path = path.rsplit(" -> ").next().unwrap_or(path);
            files.push((status, path.trim_matches('"').to_owned().into()));
        }
    }
    files
}

fn diff_files(output: &str) -> Vec<(GitStatus, SharedString)> {
    let mut files: Vec<(GitStatus, SharedString)> = Vec::new();
    for line in output.lines() {
        if let Some(paths) = line.strip_prefix(DIFF_HEAD) {
            let path = paths.rsplit(" b/").next().unwrap_or(paths);
            files.push((GitStatus::Modified, path.to_owned().into()));
            continue;
        }
        let marked = match line {
            line if line.starts_with("new file mode") => Some(GitStatus::Added),
            line if line.starts_with("deleted file mode") => Some(GitStatus::Deleted),
            _ => None,
        };
        if let (Some(status), Some(last)) = (marked, files.last_mut()) {
            last.0 = status;
            continue;
        }
        let named = line
            .split_once('\t')
            .filter(|(code, _)| code.len() <= 4 && !code.is_empty())
            .and_then(|(code, path)| {
                let status = short_status(code.get(..1)?)?;
                Some((status, path.rsplit('\t').next().unwrap_or(path)))
            });
        if let Some((status, path)) = named {
            files.push((status, path.to_owned().into()));
        }
    }
    files
}

fn commits(output: &str) -> Vec<(SharedString, SharedString)> {
    let mut found: Vec<(SharedString, SharedString)> = Vec::new();
    let mut open = false;
    for line in output.lines() {
        if let Some(sha) = line.strip_prefix("commit ") {
            let sha = sha.split_whitespace().next().unwrap_or(sha);
            found.push((short(sha).into(), SharedString::default()));
            open = true;
            continue;
        }
        if open && line.starts_with("    ") && !line.trim().is_empty() {
            if let Some(last) = found.last_mut() {
                last.1 = line.trim().to_owned().into();
            }
            open = false;
            continue;
        }
        let oneline = line.trim_start_matches(['*', '|', '/', '\\', ' ']);
        if let Some((sha, subject)) = oneline.split_once(' ')
            && sha.len() >= SHORT_HASH
            && sha.chars().all(|letter| letter.is_ascii_hexdigit())
            && !open
        {
            found.push((short(sha).into(), subject.trim().to_owned().into()));
        }
    }
    found
}

fn short(sha: &str) -> String {
    sha.chars().take(SHORT_HASH).collect()
}

pub fn wall(tools: &[&Tool]) -> Option<String> {
    let at = |stamp: Option<&str>| chrono::DateTime::parse_from_rfc3339(stamp?).ok();
    let first = tools
        .iter()
        .map(|tool| at(tool.started_at.as_deref()))
        .collect::<Option<Vec<_>>>()?
        .into_iter()
        .min()?;
    let last = tools
        .iter()
        .map(|tool| at(tool.ended_at.as_deref()))
        .collect::<Option<Vec<_>>>()?
        .into_iter()
        .max()?;
    took(last.signed_duration_since(first).num_milliseconds())
}

fn took(millis: i64) -> Option<String> {
    match millis {
        ..WALL_SHOWN_MS => None,
        WALL_SHOWN_MS..60_000 => Some(format!("{}.{}s", millis / 1000, millis % 1000 / 100)),
        _ => Some(format!("{}m {}s", millis / 60_000, millis % 60_000 / 1000)),
    }
}

fn ticket_status(said: &str) -> Option<TicketStatus> {
    let word = said
        .split([' ', ',', '.', ':'])
        .find(|word| !word.is_empty())?;
    match word.to_lowercase().as_str() {
        "triage" => Some(TicketStatus::Triage),
        "backlog" => Some(TicketStatus::Backlog),
        "todo" => Some(TicketStatus::Todo),
        "doing" => Some(TicketStatus::Doing),
        "review" => Some(TicketStatus::Review),
        "done" => Some(TicketStatus::Done),
        "blocked" => Some(TicketStatus::Blocked),
        "needs-decision" => Some(TicketStatus::NeedsDecision),
        "dropped" => Some(TicketStatus::Dropped),
        _ => None,
    }
}

fn ticket(tool: &Tool) -> TicketView {
    let args = &tool.args;
    let output = tool.output.as_deref().unwrap_or_default();
    let said = spoken(output).next().unwrap_or_default();
    let created = said
        .strip_prefix(CREATED)
        .and_then(|rest| rest.split_once(", "));
    let moved = said
        .split_once(" is ")
        .filter(|(id, _)| !id.contains(' ') && tool.name == "ticket_move");
    let field = |key: &str| {
        spoken(output)
            .find_map(|line| line.strip_prefix(key))
            .map(str::trim)
            .filter(|value| !value.is_empty())
    };
    let id = [
        text(args, "id"),
        created.map_or("", |(id, _)| id),
        moved.map_or("", |(id, _)| id),
    ]
    .into_iter()
    .find(|id| !id.is_empty())
    .or_else(|| field("id: "))
    .unwrap_or_default();
    let status = created
        .map(|(_, status)| status)
        .or(moved.map(|(_, status)| status))
        .or_else(|| field("status: "))
        .or_else(|| (tool.name == "ticket_move").then(|| text(args, "to")))
        .and_then(ticket_status);
    let summary = ["text", "reason", "question"]
        .into_iter()
        .map(|key| text(args, key))
        .find(|said| !said.is_empty())
        .and_then(first_line);
    let title = Some(text(args, "title"))
        .filter(|title| !title.is_empty())
        .or_else(|| field("title: "))
        .map(str::to_owned)
        .or_else(|| (tool.name == "ticket_move").then(|| format!("Moved to {}", text(args, "to"))))
        .or_else(|| summary.clone())
        .unwrap_or_else(|| id.to_owned());
    TicketView {
        id: id.to_owned().into(),
        status,
        said: summary.filter(|said| *said != title).map(Into::into),
        title: title.into(),
        acceptance: text(args, "acceptance")
            .lines()
            .map(|line| line.trim())
            .filter(|line| !line.is_empty() && *line != ACCEPTANCE_MARK.trim())
            .count(),
    }
}

pub fn group(kind: Kind, tools: &[&Tool]) -> ToolCard {
    let running = tools.iter().any(|tool| state(tool) == CallState::Running);
    let objects = capped(tools.iter().flat_map(|tool| {
        match kind {
            Kind::Moved | Kind::Logged | Kind::Board => token(text(&tool.args, "id")),
            Kind::Created => token(text(&tool.args, "board")),
            Kind::Edit => said(tool)
                .objects
                .into_iter()
                .filter(|object| matches!(object, Object::File(_)))
                .collect(),
            Kind::Agent
            | Kind::Git
            | Kind::Other
            | Kind::Read
            | Kind::Search
            | Kind::List
            | Kind::Run
            | Kind::Delete
            | Kind::Refused => Vec::new(),
        }
    }));
    let targets: Vec<&str> = tools.iter().map(|tool| text(&tool.args, "to")).collect();
    let meta = match kind {
        Kind::Moved => targets
            .first()
            .filter(|to| !to.is_empty() && targets.iter().all(|other| other == *to))
            .map(|to| format!("to {to}")),
        Kind::Refused => tools.first().and_then(|tool| refused(tool)),
        _ => None,
    };
    ToolCard {
        glyphs: vec![kind.glyph()],
        verb: kind.counted(tools.len()).into(),
        objects,
        meta: meta.map(Into::into),
        state: match running {
            true => CallState::Running,
            false => CallState::Done,
        },
    }
}

pub fn capped(objects: impl Iterator<Item = Object>) -> Vec<Object> {
    let mut seen: Vec<Object> = Vec::new();
    for object in objects {
        if !seen.contains(&object) {
            seen.push(object);
        }
    }
    let more = seen.len().saturating_sub(MANY_OBJECTS);
    seen.truncate(MANY_OBJECTS);
    if more > 0 {
        seen.push(Object::Token(format!("+{more} more").into()));
    }
    seen
}

pub fn addressed(tool: &Tool) -> Option<(&str, String)> {
    if tool.name != "message" || refused(tool).is_some() {
        return None;
    }
    let args = &tool.args;
    let said = message(args);
    let verb = [" to", " from", " of"]
        .into_iter()
        .find_map(|tail| said.done.strip_suffix(tail))
        .unwrap_or(&said.done);
    let owns: Vec<&str> = args
        .get("owns")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
        .filter_map(Value::as_str)
        .map(name)
        .collect();
    let line = match (owns.is_empty(), &said.meta) {
        (false, _) => format!("{verb}: {}", owns.join(", ")),
        (true, Some(text)) => format!("{verb}: {text}"),
        (true, None) => verb.to_owned(),
    };
    Some((text(args, "to"), line))
}

pub fn opens(tools: &[&Tool]) -> bool {
    tools.iter().any(|tool| state(tool) == CallState::Failed)
}

pub fn text<'a>(args: &'a Value, key: &str) -> &'a str {
    args.get(key).and_then(Value::as_str).unwrap_or_default()
}

pub fn name(path: &str) -> &str {
    path.trim_end_matches(['/', '\\'])
        .rsplit(['/', '\\'])
        .next()
        .unwrap_or(path)
}

fn file(path: &str) -> Vec<Object> {
    match path.is_empty() {
        true => Vec::new(),
        false => vec![Object::File(name(path).to_owned().into())],
    }
}

fn token(said: &str) -> Vec<Object> {
    let said = said.lines().next().unwrap_or_default().trim();
    match said.is_empty() || said.starts_with(HANDLE) {
        true => Vec::new(),
        false => vec![Object::Token(said.to_owned().into())],
    }
}

fn first_line(said: &str) -> Option<String> {
    spoken(said).next().map(str::to_owned)
}

fn scope(path: &str) -> Option<String> {
    match path {
        "" | "." | "./" => None,
        path => Some(format!("in {path}")),
    }
}

fn failure(output: &str) -> String {
    exit(output)
        .map(|code| format!("exit {code}"))
        .or_else(|| first_line(output))
        .unwrap_or_else(|| "failed".to_owned())
}

fn summed(summary: &ToolSummary) -> Option<String> {
    let parts = [
        summary.exit.map(|code| match code {
            0 => "\u{2713}".to_owned(),
            code => format!("exit {code}"),
        }),
        took(summary.duration_ms),
        summary.hits.map(|hits| match hits {
            1 => "1 hit".to_owned(),
            hits => format!("{hits} hits"),
        }),
        (summary.added > 0).then(|| format!("+{}", summary.added)),
        (summary.removed > 0).then(|| format!("\u{2212}{}", summary.removed)),
    ];
    let said: Vec<String> = parts.into_iter().flatten().collect();
    (!said.is_empty()).then(|| said.join(" "))
}

fn said(tool: &Tool) -> Said {
    let args = &tool.args;
    let output = tool.output.as_deref();
    match tool.name.as_str() {
        "read" => Said::new("Reading", "Read", file(text(args, "path")), range(args)),
        "write" => write(args, output),
        "edit" => edit(args),
        "search" => Said::new(
            "Searching",
            "Searched",
            token(text(args, "pattern")),
            scope(text(args, "path")),
        ),
        "glob" => glob(args, output),
        "symbols" => Said::new(
            "Looking up",
            "Looked up",
            token(text(args, "name")),
            scope(text(args, "path")),
        ),
        "bash" => bash(args, output),
        "shell" => shell(args),
        "fetch" => Said::new("Fetching", "Fetched", token(host(text(args, "url"))), None),
        "web_search" => Said::new(
            "Searching the web for",
            "Searched the web for",
            token(text(args, "query")),
            None,
        ),
        "message" => message(args),
        "triage" => triage(args, output),
        "ask_person" => Said::new("Asking you", "Asked you", Vec::new(), None),
        "plan" => Said::new("Updating the plan", "Updated the plan", Vec::new(), None),
        "typecheck" => Said::new(
            "Typechecking",
            "Typechecked",
            token(text(args, "path")),
            None,
        ),
        "test" => Said::new("Testing", "Tested", token(text(args, "path")), None),
        "finish" => Said::new(
            "Finishing",
            "Finished",
            token(text(args, "verdict")),
            first_line(text(args, "summary")),
        ),
        "remember" => Said::new("Remembering", "Remembered", Vec::new(), None),
        "cron" => Said::new("Scheduling", "Scheduled", Vec::new(), None),
        "subagents" => Said::new(
            "Checking sub-agents",
            "Checked sub-agents",
            token(text(args, "name")),
            None,
        ),
        name if name.starts_with(TICKET) => ticket_said(name, args),
        name if name.starts_with("browser_") => {
            Said::new("Using the browser", "Used the browser", Vec::new(), None)
        }
        name => {
            let words = name.replace('_', " ");
            Said {
                now: format!("Running {words}"),
                done: format!("Ran {words}"),
                objects: Vec::new(),
                meta: None,
            }
        }
    }
}

fn range(args: &Value) -> Option<String> {
    let line = |key| args.get(key).and_then(Value::as_u64);
    match (line("start_line"), line("end_line")) {
        (Some(start), Some(end)) => Some(format!("L{start}-{end}")),
        (Some(start), None) => Some(format!("from L{start}")),
        (None, Some(end)) => Some(format!("to L{end}")),
        (None, None) => None,
    }
}

fn write(args: &Value, output: Option<&str>) -> Said {
    let path = text(args, "path");
    if args.get("append").and_then(Value::as_bool) == Some(true) {
        return Said::new("Appending to", "Appended to", file(path), None);
    }
    let created = output.is_some_and(|said| said.starts_with(CREATED));
    let done = if created { "Created" } else { "Wrote" };
    Said::new("Writing", done, file(path), None)
}

fn edit(args: &Value) -> Said {
    let mut paths: Vec<&str> = Vec::new();
    let listed = args
        .get("edits")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
        .filter_map(|change| change.get("path")?.as_str());
    for path in std::iter::once(text(args, "path")).chain(listed) {
        if !path.is_empty() && !paths.contains(&path) {
            paths.push(path);
        }
    }
    if let [path] = paths.as_slice() {
        let mut objects = file(path);
        objects.extend(token(text(args, "symbol")));
        return Said::new("Editing", "Edited", objects, None);
    }
    let count = paths.len();
    Said {
        now: format!("Editing {count} files"),
        done: format!("Edited {count} files"),
        objects: capped(paths.iter().flat_map(|path| file(path))),
        meta: None,
    }
}

fn glob(args: &Value, output: Option<&str>) -> Said {
    let found = output.and_then(|said| {
        let header = said.lines().find(|line| line.contains(" files under "))?;
        header.split_once(" of ")?.0.trim().parse::<usize>().ok()
    });
    let done = match found {
        Some(1) => "Found 1 file".to_owned(),
        Some(count) => format!("Found {count} files"),
        None => "Listed".to_owned(),
    };
    Said {
        now: "Listing".to_owned(),
        done,
        objects: token(text(args, "pattern")),
        meta: scope(text(args, "path")),
    }
}

fn shell(args: &Value) -> Said {
    let (now, done) = match text(args, "op") {
        "wait" => ("Waiting for", "Waited on"),
        "logs" => ("Reading logs of", "Read logs of"),
        "stop" => ("Stopping", "Stopped"),
        "restart" => ("Restarting", "Restarted"),
        _ => ("Using shell", "Used shell"),
    };
    Said::new(now, done, token(text(args, "name")), None)
}

fn host(url: &str) -> &str {
    let rest = url.split_once("://").map_or(url, |(_, rest)| rest);
    rest.split(['/', '?', '#']).next().unwrap_or(rest)
}

fn message(args: &Value) -> Said {
    let (now, done) = match (text(args, "answer"), text(args, "do")) {
        ("allow", _) => ("Allowing", "Allowed"),
        ("allow_here", _) => ("Allowing for the run", "Allowed for the run"),
        ("deny", _) => ("Denying", "Denied"),
        (_, "stop") => ("Stopping", "Stopped"),
        (_, "kill") => ("Killing", "Killed"),
        (_, "release") => ("Releasing", "Released"),
        (_, "backup") => ("Backing up", "Backed up"),
        (_, "grant") => ("Granting paths to", "Granted paths to"),
        (_, "revoke") => ("Revoking paths from", "Revoked paths from"),
        (_, "replace") => ("Replacing paths of", "Replaced paths of"),
        _ => ("Messaging", "Messaged"),
    };
    let to = text(args, "to");
    let objects = match to.is_empty() {
        true => Vec::new(),
        false => vec![Object::Agent(to.to_owned().into())],
    };
    Said::new(now, done, objects, first_line(text(args, "text")))
}

fn triage(args: &Value, output: Option<&str>) -> Said {
    let request = token(text(args, "request"));
    let board = token(text(args, "board"));
    let said = || output.into_iter().flat_map(spoken);
    match text(args, "action") {
        "accept" => Said::new(
            "Accepting",
            "Accepted",
            request,
            said().find_map(|line| {
                let (_, id) = line.strip_prefix(ACCEPTED)?.split_once(ACCEPTED_AS)?;
                Some(format!("as {id}"))
            }),
        ),
        "drop" => Said::new(
            "Dropping",
            "Dropped",
            request,
            first_line(text(args, "reason")),
        ),
        "list" => Said::new(
            "Listing triage on",
            "Listed triage on",
            board,
            said().find_map(|line| Some(line.split_once(TRIAGE_HOLDS)?.1.to_owned())),
        ),
        _ => Said::new("Using triage on", "Used triage on", board, None),
    }
}

fn ticket_said(name: &str, args: &Value) -> Said {
    let id = token(text(args, "id"));
    let board = token(text(args, "board"));
    let to = || (!text(args, "to").is_empty()).then(|| format!("to {}", text(args, "to")));
    let said = || first_line(text(args, "text"));
    let title = || first_line(text(args, "title"));
    match name {
        "ticket_read" => Said::new("Reading", "Read", id, None),
        "ticket_log" => Said::new("Logging on", "Logged on", id, said()),
        "ticket_ask" => Said::new(
            "Asking on",
            "Asked on",
            id,
            said().or_else(|| first_line(text(args, "question"))),
        ),
        "ticket_move" => Said::new("Moving", "Moved", id, to()),
        "ticket_assign" => Said::new("Assigning", "Assigned", id, to()),
        "ticket_create" => Said::new(
            "Creating a ticket on",
            "Created a ticket on",
            board,
            title(),
        ),
        "ticket_request" => Said::new(
            "Requesting a ticket on",
            "Requested a ticket on",
            board,
            title(),
        ),
        "ticket_list" => Said::new("Listing tickets on", "Listed tickets on", board, None),
        _ => Said::new("Using the board on", "Used the board on", id, None),
    }
}

fn exit(output: &str) -> Option<&str> {
    output
        .lines()
        .find_map(|line| line.trim().strip_prefix(EXITED))
        .map(str::trim)
}

fn bash(args: &Value, output: Option<&str>) -> Said {
    let mut said = command(text(args, "command"));
    let away = output.and_then(background).or_else(|| {
        (output.is_some() && args.get("background").and_then(Value::as_bool) == Some(true))
            .then(|| RUNNING_AWAY.to_owned())
    });
    if let Some(away) = away {
        said.done = said.now.clone();
        said.meta = Some(away);
    }
    said
}

fn segments(command: &str) -> Vec<Vec<String>> {
    let mut joined = command.to_owned();
    for separator in SEPARATORS {
        joined = joined.replace(separator, "\u{1}");
    }
    joined
        .split('\u{1}')
        .map(|segment| {
            segment
                .split_whitespace()
                .map(|word| word.trim_matches(['"', '\'', '`']).to_owned())
                .filter(|word| !word.is_empty())
                .collect::<Vec<String>>()
        })
        .collect()
}

fn assignment(word: &str) -> bool {
    word.split_once('=').is_some_and(|(key, _)| {
        !key.is_empty()
            && key
                .chars()
                .all(|letter| letter.is_ascii_alphanumeric() || letter == '_')
    })
}

fn program(word: &str) -> String {
    let named = name(word).to_lowercase();
    named
        .strip_suffix(".exe")
        .map_or(named.clone(), str::to_owned)
}

fn meaningful(segment: &[String]) -> Option<&[String]> {
    let mut wrapped = false;
    let start = segment.iter().position(|word| {
        let wrapper = WRAPPERS.contains(&program(word).as_str());
        let option =
            wrapped && word.starts_with(|letter: char| letter == '-' || letter.is_ascii_digit());
        wrapped |= wrapper;
        !(assignment(word) || wrapper || option)
    })?;
    let words = segment.get(start..)?;
    let first = words.first()?;
    (!SETUP.iter().any(|setup| setup.eq_ignore_ascii_case(first))).then_some(words)
}

fn invoked(line: &str) -> Option<Invoked> {
    let pieces = segments(line);
    let words = pieces.iter().find_map(|segment| meaningful(segment))?;
    let shell = words.first().map(|word| program(word)).unwrap_or_default();
    if SHELLS.contains(&shell.as_str()) {
        let flagged = |flags: &[&str]| {
            words
                .iter()
                .position(|word| flags.contains(&word.to_lowercase().as_str()))
                .and_then(|at| words.get(at + 1..))
        };
        if let Some(script) = flagged(&SCRIPT_FLAGS[..]).and_then(|rest| rest.first()) {
            return Some(Invoked::Script(script.clone()));
        }
        if let Some(inline) = flagged(&INLINE_FLAGS[..]) {
            let inner = segments(&inline.join(" "));
            if let Some(words) = inner.iter().find_map(|segment| meaningful(segment)) {
                return Some(Invoked::Words(words.to_vec()));
            }
        }
        if let Some(script) = words.get(1).filter(|word| word.contains('.')) {
            return Some(Invoked::Script(script.clone()));
        }
    }
    Some(Invoked::Words(words.to_vec()))
}

fn command(line: &str) -> Said {
    match invoked(line) {
        None => Said::new("Running", "Ran", Vec::new(), None),
        Some(Invoked::Script(script)) => Said::new("Running", "Ran", file(&script), None),
        Some(Invoked::Words(words)) => run(&words),
    }
}

fn word(said: &str) -> bool {
    said.len() <= WORD_LONGEST
        && said.starts_with(|letter: char| letter.is_ascii_alphabetic())
        && said
            .chars()
            .all(|letter| letter.is_ascii_alphanumeric() || "_.:-".contains(letter))
}

fn operand(words: &[String], from: usize) -> Option<&str> {
    words
        .get(from..)?
        .iter()
        .find(|word| !word.starts_with('-'))
        .map(String::as_str)
}

fn run(words: &[String]) -> Said {
    let tool = words.first().map(|word| program(word)).unwrap_or_default();
    let sub = words.get(1).map(String::as_str).filter(|said| word(said));
    let named = |sub: Option<&str>| match sub {
        Some(sub) => token(&format!("{tool} {sub}")),
        None => token(&tool),
    };
    match (tool.as_str(), sub) {
        ("cargo" | "go", Some(sub)) => {
            let (now, done) = match sub {
                "build" => ("Building", "Built"),
                "check" | "vet" => ("Checking", "Checked"),
                "test" | "nextest" => ("Testing", "Tested"),
                "clippy" => ("Linting", "Linted"),
                "fmt" => ("Formatting", "Formatted"),
                "install" => ("Installing", "Installed"),
                "doc" => ("Documenting", "Documented"),
                _ => ("Running", "Ran"),
            };
            let package = words
                .windows(2)
                .find(|pair| {
                    pair.first()
                        .is_some_and(|flag| PACKAGE_FLAGS.contains(&flag.as_str()))
                })
                .and_then(|pair| pair.get(1))
                .map(String::as_str)
                .or_else(|| {
                    words
                        .iter()
                        .find_map(|word| word.strip_prefix("--package="))
                });
            let objects = match (package, tool.as_str()) {
                (Some(package), _) => token(package),
                (None, "go") => operand(words, 2).map_or_else(|| named(Some(sub)), token),
                (None, _) => named(Some(sub)),
            };
            Said::new(now, done, objects, None)
        }
        ("git", Some(sub)) => {
            let (now, done) = match sub {
                "status" | "diff" | "log" | "show" | "blame" => ("Checking git", "Checked git"),
                "commit" => ("Committing", "Committed"),
                "add" => ("Staging", "Staged"),
                _ => ("Running", "Ran"),
            };
            Said::new(now, done, named(Some(sub)), None)
        }
        ("rg" | "grep" | "findstr" | "select-string", _) => Said::new(
            "Searching",
            "Searched",
            operand(words, 1).map_or_else(Vec::new, token),
            None,
        ),
        ("ls" | "dir" | "tree" | "find" | "get-childitem", _) => Said::new(
            "Listing",
            "Listed",
            operand(words, 1).map_or_else(Vec::new, file),
            None,
        ),
        ("cat" | "head" | "tail" | "less" | "type" | "get-content", _) => Said::new(
            "Reading",
            "Read",
            operand(words, 1).map_or_else(Vec::new, file),
            None,
        ),
        ("rm" | "del" | "remove-item", _) => Said::new(
            "Deleting",
            "Deleted",
            operand(words, 1).map_or_else(Vec::new, file),
            None,
        ),
        ("npm" | "pnpm" | "yarn" | "bun", Some("test")) => {
            Said::new("Testing", "Tested", named(Some("test")), None)
        }
        ("npm" | "pnpm" | "yarn" | "bun", Some("install" | "i" | "add")) => {
            Said::new("Installing", "Installed", named(sub), None)
        }
        ("npm" | "pnpm" | "yarn" | "bun", Some("run")) => Said::new(
            "Running",
            "Ran",
            operand(words, 2).map_or_else(
                || named(sub),
                |script| token(&format!("{tool} run {script}")),
            ),
            None,
        ),
        _ => Said::new("Running", "Ran", named(sub), None),
    }
}
