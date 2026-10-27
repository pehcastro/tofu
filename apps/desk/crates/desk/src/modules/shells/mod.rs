use super::replayed::{self, Replayed};

use std::collections::BTreeMap;
use std::env;
use std::mem;
use std::path::{Component, Path};
use std::time::{Duration, Instant};

use desk_core::model::{Session, Store};
use desk_core::protocol::{
    Request, ShellHolder, ShellInputParams, ShellReady, ShellReleaseParams, ShellSizeParams,
    ShellTakenOver, request,
};
use desk_terminal::wire::{self, Echo, Key, Wired};
use desk_terminal::{INITIAL_COLS, INITIAL_ROWS, Kept, Output, Palette, Terminal};
use desk_ui::components::form::TextInput;
use desk_ui::components::glyph::Glyph;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};

use super::chat::{Chat, Trace, mention};
use desk_ui::components::shells::{
    Runtime, Shell, ShellEvent, ShellState, Shells as ShellsTile, Take,
};
use gpui::{
    AnyView, App, AppContext, Context, Entity, Focusable, IntoElement, Render, SharedString,
    Subscription, Task, WeakEntity, Window, rgb_to_hsla,
};

const BOARD: &str = "36-agents";
const TILE_WIDTH: f32 = 640.0;
const DIR_TAIL: usize = 2;
const TAKEOVER_LOCAL: &str = "DESK_TAKEOVER_LOCAL";
const LOCAL_NAME: &str = "takeover-local";
const LOCAL_PROGRAM: &str = "cmd";
const LOCAL_ARGS: [&str; 1] = ["/k"];
const NOTE_PLACEHOLDER: &str = "note for the agent";
const NOT_IN_TERMINAL: &str =
    "This shell was started without a terminal. Ask the agent to start it again.";
const OTHER_PROCESS: &str = "This shell belongs to another tofu process.";
const ANSI: [ColorToken; 16] = [
    ColorToken::AnsiBlack,
    ColorToken::AnsiRed,
    ColorToken::AnsiGreen,
    ColorToken::AnsiYellow,
    ColorToken::AnsiBlue,
    ColorToken::AnsiMagenta,
    ColorToken::AnsiCyan,
    ColorToken::AnsiWhite,
    ColorToken::AnsiBrightBlack,
    ColorToken::AnsiBrightRed,
    ColorToken::AnsiBrightGreen,
    ColorToken::AnsiBrightYellow,
    ColorToken::AnsiBrightBlue,
    ColorToken::AnsiBrightMagenta,
    ColorToken::AnsiBrightCyan,
    ColorToken::AnsiBrightWhite,
];

struct Local {
    kept: Kept,
    started: Instant,
    dir: String,
    held: Option<Entity<Terminal>>,
}

struct Wire {
    shell: SharedString,
    note: Entity<TextInput>,
    attached: Option<Attached>,
}

struct Attached {
    terminal: Entity<Terminal>,
    output: flume::Sender<Output>,
    next: i64,
    focus_owed: bool,
    typed: Vec<(Key, Duration)>,
    _input: Task<()>,
    _resize: Task<()>,
}

struct Handed {
    lines: Vec<SharedString>,
    from: usize,
}

impl Handed {
    fn lines(&self, output: &str) -> Vec<SharedString> {
        let after: Vec<&str> = output
            .get(self.from..)
            .unwrap_or_default()
            .lines()
            .map(str::trim_end)
            .filter(|line| !line.is_empty())
            .collect();
        let held: Vec<&str> = self
            .lines
            .iter()
            .map(|line| line.trim_end())
            .filter(|line| !line.is_empty())
            .collect();
        let echoed = (1..=after.len())
            .rev()
            .find(|&k| held.windows(k).any(|window| window == &after[..k]))
            .unwrap_or(0);
        self.lines
            .iter()
            .cloned()
            .chain(after[echoed..].iter().map(|line| (*line).to_owned().into()))
            .collect()
    }
}

struct Row {
    shell: Shell,
    trace: Option<Trace>,
    take: Take,
    yours: bool,
}

pub struct Shells {
    store: Entity<Store>,
    chat: WeakEntity<Chat>,
    rows: Vec<Row>,
    active: usize,
    listing: bool,
    asking: Option<usize>,
    local: Option<Local>,
    wire: Option<Wire>,
    refused: BTreeMap<SharedString, SharedString>,
    handed: BTreeMap<SharedString, Handed>,
    _watch: Subscription,
}

pub fn palette(theme: &Theme) -> Palette {
    let color = |token| rgb_to_hsla(theme.color(token));
    Palette {
        foreground: color(ColorToken::TextBase),
        background: color(ColorToken::CardsInnerFill),
        cursor: color(ColorToken::TextStrong),
        selection: color(ColorToken::Selection),
        ansi: ANSI.map(color),
    }
}

fn local() -> Option<Local> {
    env::var_os(TAKEOVER_LOCAL).filter(|on| on == "1")?;
    let dir = match env::current_dir() {
        Ok(dir) => dir,
        Err(error) => {
            eprintln!("desk: shells: no folder for the local test shell: {error}");
            return None;
        }
    };
    match Kept::spawn(Path::new(LOCAL_PROGRAM), &LOCAL_ARGS, &dir) {
        Ok(kept) => {
            eprintln!(
                "desk: shells: local test shell {LOCAL_NAME} started, pid {:?}",
                kept.pid()
            );
            Some(Local {
                kept,
                started: Instant::now(),
                dir: dir.display().to_string(),
                held: None,
            })
        }
        Err(error) => {
            eprintln!("desk: shells: the local test shell did not start: {error}");
            None
        }
    }
}

impl Local {
    fn row(&self) -> Row {
        let alive = self.kept.alive();
        Row {
            shell: Shell {
                name: LOCAL_NAME.into(),
                state: if alive {
                    ShellState::Running
                } else {
                    ShellState::Exited(None)
                },
                command: format!("{LOCAL_PROGRAM} {}", LOCAL_ARGS.join(" ")).into(),
                dir: short_dir(&self.dir),
                pid: self.kept.pid().map(i64::from),
                starter: None,
                port: None,
                ready: None,
                runtime: if alive {
                    Runtime::Live(self.started)
                } else {
                    Runtime::Unknown
                },
                lines: Vec::new(),
            },
            trace: None,
            take: Take::Offered,
            yours: self.held.is_some(),
        }
    }
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if board.is_some_and(|board| board != BOARD) {
        return Err(format!("the shells module replays {BOARD}, not {board:?}"));
    }
    replayed::fonts(cx)?;
    let store = cx.new(|_| Store::default());
    let module = mount(store.clone(), WeakEntity::new_invalid(), cx);
    let counted = module.clone();
    Replayed::open(
        store,
        module.into(),
        (Glyph::Terminal, "Shells", TILE_WIDTH),
        move |cx| {
            let line = counted.update(cx, |module, cx| module.counted(cx));
            eprintln!(
                "desk: shells from the store: {line}; cassette: {} shell.started events",
                cassette_started()
            );
        },
        cx,
    )
}

pub fn mount(store: Entity<Store>, chat: WeakEntity<Chat>, cx: &mut App) -> Entity<Shells> {
    cx.new(|cx: &mut Context<Shells>| {
        let mut shells = Shells {
            _watch: cx.observe(&store, |module, _, cx| {
                module.drain(cx);
                module.rebuild(cx);
                cx.notify();
            }),
            store,
            chat,
            rows: Vec::new(),
            active: 0,
            listing: false,
            asking: None,
            local: local(),
            wire: None,
            refused: BTreeMap::new(),
            handed: BTreeMap::new(),
        };
        shells.rebuild(cx);
        shells
    })
}

fn moment(at: Option<&str>) -> Option<chrono::DateTime<chrono::FixedOffset>> {
    at.and_then(|at| chrono::DateTime::parse_from_rfc3339(at).ok())
}

fn runtime(state: ShellState, stored: &desk_core::model::Shell) -> Runtime {
    let started = moment(stored.started_at.as_deref());
    if state.endable() {
        return Runtime::Live(
            started
                .and_then(|at| (chrono::Utc::now() - at.to_utc()).to_std().ok())
                .and_then(|age| Instant::now().checked_sub(age))
                .unwrap_or(stored.first_seen),
        );
    }
    started
        .zip(moment(stored.ended_at.as_deref()))
        .and_then(|(started, ended)| (ended - started).to_std().ok())
        .map_or(Runtime::Unknown, Runtime::Ended)
}

fn short_dir(dir: &str) -> SharedString {
    let names: Vec<_> = Path::new(dir)
        .components()
        .filter_map(|part| match part {
            Component::Normal(name) => Some(name.to_string_lossy()),
            Component::Prefix(_)
            | Component::RootDir
            | Component::CurDir
            | Component::ParentDir => None,
        })
        .collect();
    match names.len().checked_sub(DIR_TAIL) {
        None | Some(0) => dir.to_owned().into(),
        Some(from) => format!(
            "\u{2026}{}{}",
            std::path::MAIN_SEPARATOR,
            names
                .get(from..)
                .unwrap_or_default()
                .join(std::path::MAIN_SEPARATOR_STR)
        )
        .into(),
    }
}

fn ready(ready: &ShellReady) -> SharedString {
    match ready {
        ShellReady::Port => "ready: its port opened".into(),
        ShellReady::Line => "ready: it printed a ready line".into(),
        ShellReady::Waited => "the wait ran out".into(),
        ShellReady::Stopped => "the turn stopped".into(),
        ShellReady::Unknown(raw) => format!("ready: {raw}").into(),
    }
}

fn shown(session: &Session, name: &str, stored: &desk_core::model::Shell) -> Option<Shell> {
    if stored.kept.is_none() && !stored.left_over {
        return None;
    }
    let state = match (stored.exited, stored.killed, stored.left_over) {
        (true, true, _) => ShellState::Killed,
        (true, false, _) => {
            ShellState::Exited(stored.exit_code.and_then(|code| i32::try_from(code).ok()))
        }
        (false, _, true) => ShellState::LeftOver,
        (false, _, false) => ShellState::Running,
    };
    let starter = stored.agent.as_ref().map(|id| {
        session.agents.get(id).map_or_else(
            || SharedString::from(id.clone()),
            |agent| format!("{} {}", agent.kind, agent.number).into(),
        )
    });
    Some(Shell {
        name: name.to_owned().into(),
        state,
        command: stored.command.clone().into(),
        dir: short_dir(&stored.dir),
        pid: stored.pid,
        starter,
        port: stored.port,
        ready: stored.ready.as_ref().map(ready),
        runtime: runtime(state, stored),
        lines: stored
            .output
            .lines()
            .map(|line| line.to_owned().into())
            .collect(),
    })
}

fn take(stored: &desk_core::model::Shell, refused: Option<&SharedString>) -> Take {
    match (stored.left_over, stored.terminal, refused) {
        (true, ..) => Take::Refused(OTHER_PROCESS.into()),
        (false, false, _) => Take::Refused(NOT_IN_TERMINAL.into()),
        (false, true, Some(refused)) => Take::Refused(refused.clone()),
        (false, true, None) => Take::Offered,
    }
}

fn stored_mut<'a>(store: &'a mut Store, name: &str) -> Option<&'a mut desk_core::model::Shell> {
    let open = store.open.as_ref()?;
    store.sessions.get_mut(open)?.shells.get_mut(name)
}

impl Shells {
    pub fn count(&self) -> usize {
        self.rows
            .iter()
            .filter(|row| row.shell.state == ShellState::Running)
            .count()
    }

    fn rebuild(&mut self, cx: &mut Context<Self>) {
        let session = self.store.read(cx).open_session();
        let mut rows: Vec<(Option<&str>, Row)> = session.map_or_else(Vec::new, |session| {
            session
                .shells
                .iter()
                .filter_map(|(name, stored)| {
                    let trace = stored.mention.clone().map(|token| Trace {
                        token,
                        glyph: Glyph::Terminal,
                        label: name.clone(),
                        detail: stored.command.clone(),
                    });
                    let mut shell = shown(session, name, stored)?;
                    if let Some(handed) = self.handed.get(name.as_str()) {
                        shell.lines = handed.lines(&stored.output);
                    }
                    let row = Row {
                        shell,
                        trace,
                        take: take(stored, self.refused.get(name.as_str())),
                        yours: stored.holder == ShellHolder::Person,
                    };
                    Some((stored.started_at.as_deref(), row))
                })
                .collect()
        });
        rows.sort_by_key(|(started, _)| *started);
        self.rows = rows.into_iter().map(|(_, row)| row).collect();
        if let Some(local) = &self.local {
            self.rows.insert(0, local.row());
        }
        self.active = self.active.min(self.rows.len().saturating_sub(1));
        self.asking = self.asking.filter(|at| {
            self.rows
                .get(*at)
                .is_some_and(|row| row.shell.state.endable())
        });
        eprintln!(
            "desk: tile shells rebuilt {}, {} with a ref on the wire",
            self.listed(),
            self.rows.iter().filter(|row| row.trace.is_some()).count()
        );
    }

    fn listed(&self) -> String {
        let shells: Vec<String> = self
            .rows
            .iter()
            .map(|Row { shell, yours, .. }| {
                let pid = shell
                    .pid
                    .map_or_else(|| "no pid".to_owned(), |pid| format!("pid {pid}"));
                let held = if *yours { " held by you" } else { "" };
                format!("{} {pid} {}{held}", shell.name, shell.state.word())
            })
            .collect();
        format!(
            "{} kept shells, {} running ({})",
            self.rows.len(),
            self.count(),
            shells.join(", ")
        )
    }

    pub fn counted(&mut self, cx: &mut Context<Self>) -> String {
        self.rebuild(cx);
        self.listed()
    }

    pub fn reveal(&mut self, name: &str, window: &mut Window, cx: &mut Context<Self>) -> bool {
        let Some(at) = self.rows.iter().position(|row| row.shell.name == name) else {
            return false;
        };
        self.apply(ShellEvent::Pick(at), window, cx);
        cx.notify();
        true
    }

    fn apply(&mut self, event: ShellEvent, window: &mut Window, cx: &mut Context<Self>) {
        match event {
            ShellEvent::Pick(at) => {
                self.active = at;
                self.listing = false;
                self.asking = None;
            }
            ShellEvent::More => self.listing = true,
            ShellEvent::Dismiss => self.listing = false,
            ShellEvent::AskKill(at) => self.asking = Some(at),
            ShellEvent::Keep => self.asking = None,
            ShellEvent::Trace(at) => match self.rows.get(at).and_then(|row| row.trace.clone()) {
                Some(trace) => mention(trace, window, cx),
                None => eprintln!("desk: shells: shell {at} has no ref on the wire to mention"),
            },
            ShellEvent::Close(at) => self.release(at, cx),
            ShellEvent::Open(at) => {
                eprintln!("desk: shells: {event:?} on shell {at} is not wired");
            }
        }
    }

    fn local_at(&self) -> Option<usize> {
        self.local.as_ref()?;
        self.rows
            .iter()
            .position(|row| row.shell.name == LOCAL_NAME)
    }

    fn wire_at(&self) -> Option<usize> {
        let wire = self.wire.as_ref()?;
        self.rows
            .iter()
            .position(|row| row.shell.name == wire.shell)
    }

    fn call<R: Request>(
        &self,
        params: R::Params,
        cx: &mut Context<Self>,
        then: impl FnOnce(&mut Self, Result<R::Result, String>, &mut Context<Self>) + 'static,
    ) where
        R::Result: 'static,
    {
        let module = cx.weak_entity();
        let sent = self.chat.update(cx, |chat, cx| {
            chat.request::<R>(&params, cx, move |_, reply, cx| {
                cx.defer(move |cx| {
                    if module
                        .update(cx, |module, cx| then(module, reply, cx))
                        .is_err()
                    {
                        eprintln!("desk: shells: {} answered after the tile closed", R::METHOD);
                    }
                });
            });
        });
        if sent.is_err() {
            eprintln!(
                "desk: shells: {} not sent, there is no chat to reach tofu",
                R::METHOD
            );
        }
    }

    fn kill(&self, at: usize, cx: &mut Context<Self>) {
        let Some(row) = self.rows.get(at) else {
            return eprintln!("desk: shells: kill names shell {at}, which is not listed");
        };
        let shell = row.shell.name.clone();
        if self
            .chat
            .update(cx, |chat, cx| chat.kill(shell, cx))
            .is_err()
        {
            eprintln!("desk: shells: no chat reaches tofu, so nothing was killed");
        }
    }

    fn take_over(&mut self, at: usize, window: &mut Window, cx: &mut Context<Self>) {
        if self.local_at() == Some(at) {
            return self.take_local(window, cx);
        }
        let Some(name) = self.rows.get(at).map(|row| row.shell.name.clone()) else {
            return eprintln!("desk: shells: take over names shell {at}, which is not listed");
        };
        if self.wire.as_ref().is_some_and(|wire| wire.shell == name) {
            return;
        }
        if let Some(held) = self.wire_at() {
            self.release(held, cx);
        }
        let note = TextInput::new(NOTE_PLACEHOLDER.into(), window, cx);
        self.ask(
            Wire {
                shell: name,
                note,
                attached: None,
            },
            (INITIAL_COLS, INITIAL_ROWS),
            cx,
        );
    }

    fn ask(&mut self, wire: Wire, (cols, rows): (u16, u16), cx: &mut Context<Self>) {
        let shell = wire.shell.clone();
        self.tap(&shell, true, cx);
        self.refused.remove(&shell);
        self.wire = Some(wire);
        eprintln!("desk: shell.takeover {shell} at {cols}x{rows}");
        let params = ShellSizeParams {
            shell: shell.to_string(),
            cols: cols.into(),
            rows: rows.into(),
        };
        let asked = Instant::now();
        self.call::<request::ShellTakeover>(params, cx, move |module, reply, cx| {
            module.taken(&shell, asked, reply, cx);
        });
    }

    fn taken(
        &mut self,
        shell: &SharedString,
        asked: Instant,
        reply: Result<ShellTakenOver, String>,
        cx: &mut Context<Self>,
    ) {
        let waiting = self
            .wire
            .as_ref()
            .is_some_and(|wire| wire.shell == *shell && wire.attached.is_none());
        let taken = match reply {
            Ok(taken) if waiting => taken,
            Ok(_) => {
                eprintln!("desk: shell.takeover {shell} answered after the hold ended, released");
                return self.send_release(shell.clone(), None, cx);
            }
            Err(error) if waiting => return self.refuse(shell, error, cx),
            Err(error) => return eprintln!("desk: shell.takeover {shell} refused late: {error}"),
        };
        let replay = match wire::decode(&taken.replay) {
            Ok(replay) => replay,
            Err(error) => {
                self.end(cx);
                self.send_release(shell.clone(), None, cx);
                return self.refuse(shell, error, cx);
            }
        };
        eprintln!(
            "desk: shell.takeover {shell} answered in {} ms, holder {}, replay {} bytes, offsets {} to {}",
            asked.elapsed().as_millis(),
            String::from(taken.holder.clone()),
            replay.len(),
            taken.base,
            taken.next
        );
        let Wired {
            attachment,
            keys,
            sizes,
            output,
        } = wire::wired(replay);
        let terminal = cx.new(|cx| Terminal::attach(attachment, cx));
        let input = input(shell.clone(), keys, cx);
        let resize = resize(shell.clone(), sizes, cx);
        let name = shell.to_string();
        self.store.update(cx, |store, _| {
            if let Some(stored) = stored_mut(store, &name) {
                stored.holder = ShellHolder::Person;
                stored.held_since = Some(taken.since);
            }
        });
        if let Some(wire) = &mut self.wire {
            wire.attached = Some(Attached {
                terminal,
                output,
                next: taken.next,
                focus_owed: true,
                typed: Vec::new(),
                _input: input,
                _resize: resize,
            });
        }
        eprintln!("desk: terminal attached {shell} over the wire");
        self.drain(cx);
        self.rebuild(cx);
        cx.notify();
    }

    fn refuse(&mut self, shell: &SharedString, error: String, cx: &mut Context<Self>) {
        eprintln!("desk: shell.takeover {shell} refused: {error}");
        self.refused.insert(shell.clone(), error.into());
        self.end(cx);
        self.rebuild(cx);
        cx.notify();
    }

    fn tap(&self, shell: &str, on: bool, cx: &mut Context<Self>) {
        self.store.update(cx, |store, _| {
            if let Some(stored) = stored_mut(store, shell) {
                stored.raw = on.then(Vec::new);
            }
        });
    }

    fn end(&mut self, cx: &mut Context<Self>) -> Option<Wire> {
        let wire = self.wire.take()?;
        self.tap(&wire.shell, false, cx);
        if let Some(attached) = &wire.attached {
            let from = self
                .store
                .read(cx)
                .open_session()
                .and_then(|session| session.shells.get(wire.shell.as_ref()))
                .map_or(0, |stored| {
                    stored
                        .released_at
                        .map_or(stored.output.len(), |offset| stored.output_at(offset))
                });
            let lines = attached.terminal.read(cx).text();
            eprintln!(
                "desk: shell {} read view keeps {} lines from the held terminal",
                wire.shell,
                lines.len()
            );
            self.handed
                .insert(wire.shell.clone(), Handed { lines, from });
        }
        Some(wire)
    }

    fn drain(&mut self, cx: &mut Context<Self>) {
        let Some(Wire {
            shell,
            attached: Some(_),
            ..
        }) = &self.wire
        else {
            return;
        };
        let name = shell.to_string();
        let drained = self.store.update(cx, |store, _| {
            let stored = stored_mut(store, &name)?;
            Some((
                stored.raw.as_mut().map(mem::take).unwrap_or_default(),
                stored.holder == ShellHolder::Person,
            ))
        });
        let raw = match drained {
            Some((raw, true)) => raw,
            Some((_, false)) => {
                eprintln!("desk: shell hold on {name} ended by tofu, the agent holds it again");
                self.end(cx);
                return;
            }
            None => {
                eprintln!("desk: shell hold on {name} ended, the shell is no longer listed");
                self.end(cx);
                return;
            }
        };
        let Some(Wire {
            attached: Some(attached),
            ..
        }) = &mut self.wire
        else {
            return;
        };
        let mut gap = None;
        for (offset, data) in raw {
            let bytes = match wire::decode(&data) {
                Ok(bytes) => bytes,
                Err(error) => {
                    gap = Some(error);
                    break;
                }
            };
            let end = i64::try_from(bytes.len())
                .ok()
                .and_then(|length| offset.checked_add(length));
            let skip = attached
                .next
                .checked_sub(offset)
                .and_then(|skip| usize::try_from(skip).ok());
            let (Some(end), Some(skip)) = (end, skip) else {
                gap = Some(format!(
                    "shell.raw at {offset}, the terminal has up to {}",
                    attached.next
                ));
                break;
            };
            let Some(fresh) = bytes.get(skip..).filter(|fresh| !fresh.is_empty()) else {
                continue;
            };
            let echoes = attached
                .typed
                .drain(..)
                .map(|(key, batched)| Echo {
                    key,
                    batched,
                    back: key.down.elapsed(),
                })
                .collect();
            let sent = attached
                .output
                .send(Output::Echoed(echoes))
                .and_then(|()| attached.output.send(Output::Bytes(fresh.to_vec())));
            if sent.is_err() {
                gap = Some("the terminal stopped reading".to_owned());
                break;
            }
            attached.next = end;
        }
        if let Some(gap) = gap {
            self.regain(&gap, cx);
        }
    }

    fn regain(&mut self, gap: &str, cx: &mut Context<Self>) {
        let Some(mut wire) = self.wire.take() else {
            return;
        };
        let grid = wire
            .attached
            .take()
            .map_or((INITIAL_COLS, INITIAL_ROWS), |attached| {
                attached.terminal.read(cx).grid()
            });
        eprintln!(
            "desk: shell {} raw gap ({gap}), asking shell.takeover again",
            wire.shell
        );
        self.ask(wire, grid, cx);
    }

    fn release(&mut self, at: usize, cx: &mut Context<Self>) {
        if self.local_at() == Some(at) {
            return self.release_local();
        }
        if self.wire_at() != Some(at) {
            return;
        }
        let Some(wire) = self.end(cx) else {
            return;
        };
        let note =
            Some(wire.note.read(cx).text().trim().to_owned()).filter(|note| !note.is_empty());
        self.send_release(wire.shell, note, cx);
        self.rebuild(cx);
    }

    fn send_release(&self, shell: SharedString, note: Option<String>, cx: &mut Context<Self>) {
        eprintln!(
            "desk: shell.release {shell}, note {}",
            note.as_deref().unwrap_or("none")
        );
        let params = ShellReleaseParams {
            shell: shell.to_string(),
            note,
        };
        self.call::<request::ShellRelease>(params, cx, move |module, reply, cx| match reply {
            Ok(held) => {
                eprintln!(
                    "desk: shell.release {} answered holder {} since {}",
                    held.shell,
                    String::from(held.holder),
                    held.since
                );
                eprintln!(
                    "desk: shell {} released at offset {}",
                    held.shell, held.offset
                );
                let from = module.store.update(cx, |store, _| {
                    let stored = stored_mut(store, &held.shell)?;
                    stored.released_at = Some(held.offset);
                    Some(stored.output_at(held.offset))
                });
                if let (Some(from), Some(handed)) =
                    (from, module.handed.get_mut(held.shell.as_str()))
                {
                    handed.from = from;
                }
                module.rebuild(cx);
                cx.notify();
            }
            Err(error) => eprintln!("desk: shell.release {shell} refused: {error}"),
        });
    }

    fn take_local(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(local) = &mut self.local else {
            return;
        };
        let attachment = local.kept.attach();
        let terminal = cx.new(|cx| Terminal::attach(attachment, cx));
        window.focus(&terminal.focus_handle(cx), cx);
        eprintln!("desk: terminal attached {LOCAL_NAME}");
        local.held = Some(terminal);
    }

    fn release_local(&mut self) {
        let Some(local) = &mut self.local else {
            return;
        };
        if local.held.take().is_none() {
            return;
        }
        local.kept.release();
        let child = if local.kept.alive() {
            "child alive"
        } else {
            "child gone"
        };
        eprintln!("desk: terminal released {LOCAL_NAME}, {child}");
    }

    fn held(
        &mut self,
        window: &mut Window,
        cx: &mut App,
    ) -> Option<(usize, Entity<Terminal>, Option<AnyView>)> {
        if let Some(at) = self.wire_at()
            && let Some(Wire {
                note,
                attached: Some(attached),
                ..
            }) = &mut self.wire
        {
            if attached.focus_owed {
                attached.focus_owed = false;
                window.focus(&attached.terminal.focus_handle(cx), cx);
            }
            return Some((at, attached.terminal.clone(), Some(note.clone().into())));
        }
        let at = self.local_at()?;
        let terminal = self.local.as_ref()?.held.clone()?;
        Some((at, terminal, None))
    }
}

fn input(
    shell: SharedString,
    keys: flume::Receiver<(Key, Vec<u8>)>,
    cx: &mut Context<Shells>,
) -> Task<()> {
    cx.spawn(async move |module, cx| {
        while let Some(wire::Input { keys: typed, data }) = wire::next_input(&keys).await {
            let sent = module.update(cx, |module, cx| {
                for data in data {
                    let params = ShellInputParams {
                        shell: shell.to_string(),
                        data,
                    };
                    module.call::<request::ShellInput>(params, cx, |_, reply, _| {
                        if let Err(error) = reply {
                            eprintln!("desk: shell.input refused: {error}");
                        }
                    });
                }
                if let Some(attached) = module.wire.as_mut().and_then(|wire| wire.attached.as_mut())
                {
                    attached
                        .typed
                        .extend(typed.into_iter().map(|key| (key, key.down.elapsed())));
                }
            });
            if sent.is_err() {
                return;
            }
        }
    })
}

fn resize(
    shell: SharedString,
    sizes: flume::Receiver<(u16, u16)>,
    cx: &mut Context<Shells>,
) -> Task<()> {
    cx.spawn(async move |module, cx| {
        while let Some((cols, rows)) = wire::next_size(&sizes).await {
            let params = ShellSizeParams {
                shell: shell.to_string(),
                cols: cols.into(),
                rows: rows.into(),
            };
            let sent = module.update(cx, |module, cx| {
                module.call::<request::ShellResize>(params, cx, move |_, reply, _| match reply {
                    Ok(_) => eprintln!("desk: shell.resize {cols}x{rows} answered ok"),
                    Err(error) => eprintln!("desk: shell.resize {cols}x{rows} refused: {error}"),
                });
            });
            if sent.is_err() {
                return;
            }
        }
    })
}

fn cassette_started() -> usize {
    include_str!("../../../../../cassettes/36-agents.cassette")
        .lines()
        .filter(|line| line.contains("\"method\": \"shell.started\""))
        .count()
}

impl Render for Shells {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let tile = ShellsTile::new(
            "shells",
            self.rows.iter().map(|row| row.shell.clone()).collect(),
            self.active,
            self.listing,
            cx.listener(|module, event: &ShellEvent, window, cx| {
                module.apply(*event, window, cx);
                cx.notify();
            }),
        )
        .asking(self.asking)
        .traced(self.rows.iter().map(|row| row.trace.is_some()).collect())
        .yours(self.rows.iter().map(|row| row.yours).collect())
        .on_kill(cx.listener(|module, picked: &usize, _, cx| {
            module.asking = None;
            module.kill(*picked, cx);
            cx.notify();
        }))
        .on_take_over(
            self.rows.iter().map(|row| row.take.clone()).collect(),
            cx.listener(|module, at: &usize, window, cx| {
                module.take_over(*at, window, cx);
                cx.notify();
            }),
        );
        let Some((at, terminal, note)) = self.held(window, cx) else {
            return tile;
        };
        let palette = palette(&ActiveTheme::theme(cx));
        terminal.update(cx, |terminal, cx| terminal.set_palette(palette, cx));
        tile.held(
            at,
            terminal.into(),
            note,
            cx.listener(|module, at: &usize, _, cx| {
                module.release(*at, cx);
                cx.notify();
            }),
        )
    }
}
