use super::replayed::{self, Replayed};

use std::env;
use std::path::{Component, Path};
use std::rc::Rc;
use std::time::Instant;

use desk_core::model::{Session, Store};
use desk_core::protocol::ShellReady;
use desk_terminal::{Kept, Palette, Terminal};
use desk_ui::components::glyph::Glyph;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};

use super::chat::{Trace, mention};
use desk_ui::components::shells::{Runtime, Shell, ShellEvent, ShellState, Shells as ShellsTile};
use gpui::{
    AnyView, App, AppContext, Context, Entity, Focusable, IntoElement, Render, SharedString,
    Subscription, Window, rgb_to_hsla,
};

const BOARD: &str = "36-agents";
const TILE_WIDTH: f32 = 640.0;
const DIR_TAIL: usize = 2;
const TAKEOVER_LOCAL: &str = "DESK_TAKEOVER_LOCAL";
const LOCAL_NAME: &str = "takeover-local";
const LOCAL_PROGRAM: &str = "cmd";
const LOCAL_ARGS: [&str; 1] = ["/k"];
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

pub type Kill = Rc<dyn Fn(SharedString, &mut App)>;

struct Local {
    kept: Kept,
    started: Instant,
    dir: String,
    held: Option<Entity<Terminal>>,
}

pub struct Shells {
    store: Entity<Store>,
    kill: Kill,
    shells: Vec<Shell>,
    traces: Vec<Option<Trace>>,
    active: usize,
    listing: bool,
    asking: Option<usize>,
    local: Option<Local>,
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
    fn shell(&self) -> Shell {
        let alive = self.kept.alive();
        Shell {
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
        }
    }
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if board.is_some_and(|board| board != BOARD) {
        return Err(format!("the shells module replays {BOARD}, not {board:?}"));
    }
    replayed::fonts(cx)?;
    let store = cx.new(|_| Store::default());
    let kill: Kill = Rc::new(|shell, _| {
        eprintln!("desk: shells: kill {shell}: a replayed cassette has no tofu to ask");
    });
    let module = mount(store.clone(), kill, cx);
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

pub fn mount(store: Entity<Store>, kill: Kill, cx: &mut App) -> Entity<Shells> {
    cx.new(|cx: &mut Context<Shells>| {
        let mut shells = Shells {
            _watch: cx.observe(&store, |module, _, cx| {
                module.rebuild(cx);
                cx.notify();
            }),
            store,
            kill,
            shells: Vec::new(),
            traces: Vec::new(),
            active: 0,
            listing: false,
            asking: None,
            local: local(),
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

impl Shells {
    pub fn count(&self) -> usize {
        self.shells
            .iter()
            .filter(|shell| shell.state == ShellState::Running)
            .count()
    }

    fn rebuild(&mut self, cx: &mut Context<Self>) {
        let session = self.store.read(cx).open_session();
        let mut shells: Vec<(Option<&str>, Shell, Option<Trace>)> =
            session.map_or_else(Vec::new, |session| {
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
                        shown(session, name, stored)
                            .map(|shell| (stored.started_at.as_deref(), shell, trace))
                    })
                    .collect()
            });
        shells.sort_by_key(|(started, ..)| *started);
        (self.shells, self.traces) = shells
            .into_iter()
            .map(|(_, shell, trace)| (shell, trace))
            .unzip();
        if let Some(local) = &self.local {
            self.shells.insert(0, local.shell());
            self.traces.insert(0, None);
        }
        self.active = self.active.min(self.shells.len().saturating_sub(1));
        self.asking = self.asking.filter(|at| {
            self.shells
                .get(*at)
                .is_some_and(|shell| shell.state.endable())
        });
        eprintln!(
            "desk: tile shells rebuilt {}, {} with a ref on the wire",
            self.listed(),
            self.traces.iter().flatten().count()
        );
    }

    fn listed(&self) -> String {
        let shells: Vec<String> = self
            .shells
            .iter()
            .map(|shell| {
                let pid = shell
                    .pid
                    .map_or_else(|| "no pid".to_owned(), |pid| format!("pid {pid}"));
                format!("{} {pid} {}", shell.name, shell.state.word())
            })
            .collect();
        format!(
            "{} kept shells, {} running ({})",
            self.shells.len(),
            self.count(),
            shells.join(", ")
        )
    }

    pub fn counted(&mut self, cx: &mut Context<Self>) -> String {
        self.rebuild(cx);
        self.listed()
    }

    pub fn reveal(&mut self, name: &str, window: &mut Window, cx: &mut Context<Self>) -> bool {
        let Some(at) = self.shells.iter().position(|shell| shell.name == name) else {
            return false;
        };
        self.apply(ShellEvent::Pick(at), window, cx);
        cx.notify();
        true
    }

    fn apply(&mut self, event: ShellEvent, window: &mut Window, cx: &mut App) {
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
            ShellEvent::Trace(at) => match self.traces.get(at).cloned().flatten() {
                Some(trace) => mention(trace, window, cx),
                None => eprintln!("desk: shells: shell {at} has no ref on the wire to mention"),
            },
            ShellEvent::Close(at) if self.local_at() == Some(at) => self.release(),
            ShellEvent::Close(at) | ShellEvent::Open(at) => {
                eprintln!("desk: shells: {event:?} on shell {at} is not wired");
            }
        }
    }

    fn local_at(&self) -> Option<usize> {
        self.local.as_ref()?;
        self.shells
            .iter()
            .position(|shell| shell.name == LOCAL_NAME)
    }

    fn take_over(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(local) = &mut self.local else {
            return eprintln!("desk: shells: take over needs the tofu wire, which is not wired");
        };
        let attachment = local.kept.attach();
        let terminal = cx.new(|cx| Terminal::attach(attachment, cx));
        window.focus(&terminal.focus_handle(cx), cx);
        eprintln!("desk: terminal attached {LOCAL_NAME}");
        local.held = Some(terminal);
    }

    fn release(&mut self) {
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
}

fn cassette_started() -> usize {
    include_str!("../../../../../cassettes/36-agents.cassette")
        .lines()
        .filter(|line| line.contains("\"method\": \"shell.started\""))
        .count()
}

impl Render for Shells {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let tile = ShellsTile::new(
            "shells",
            self.shells.clone(),
            self.active,
            self.listing,
            cx.listener(|module, event: &ShellEvent, window, cx| {
                module.apply(*event, window, cx);
                cx.notify();
            }),
        )
        .asking(self.asking)
        .traced(self.traces.iter().map(Option::is_some).collect())
        .on_kill(cx.listener(|module, picked: &usize, _, cx| {
            module.asking = None;
            match module.shells.get(*picked) {
                Some(shell) => (module.kill)(shell.name.clone(), cx),
                None => eprintln!("desk: shells: kill names shell {picked}, which is not listed"),
            }
            cx.notify();
        }));
        let Some(at) = self.local_at() else {
            return tile;
        };
        let takeable = (0..self.shells.len()).map(|ix| ix == at).collect();
        let tile = tile.on_take_over(
            takeable,
            cx.listener(|module, _: &usize, window, cx| {
                module.take_over(window, cx);
                cx.notify();
            }),
        );
        let Some(terminal) = self.local.as_ref().and_then(|local| local.held.clone()) else {
            return tile;
        };
        let palette = palette(&ActiveTheme::theme(cx));
        terminal.update(cx, |terminal, cx| terminal.set_palette(palette, cx));
        tile.held(
            at,
            terminal.into(),
            cx.listener(|module, _: &usize, _, cx| {
                module.release();
                cx.notify();
            }),
        )
    }
}
