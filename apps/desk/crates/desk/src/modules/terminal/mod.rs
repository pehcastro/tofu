use std::env;
use std::path::Path;
use std::time::{Duration, Instant};

use desk_terminal::{Palette, Terminal, TerminalEvent, shells};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, App, AppContext, Context, Entity, EventEmitter, IntoElement, Render, SharedString,
    Subscription, Task, Window, div, prelude::*, rgb_to_hsla,
};

const FRAME_LOG: &str = "DESK_FRAME_LOG";
const BELL: Duration = Duration::from_millis(450);
const QUIET: Duration = Duration::from_millis(2000);
const SLOW_FRAME_MS: f64 = 16.7;
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

struct FrameLog {
    output_at: Option<Instant>,
    seen: Option<Instant>,
    frames: Vec<Instant>,
    _output: Subscription,
}

pub struct TabChanged;

pub struct TerminalModule {
    terminal: Entity<Terminal>,
    number: usize,
    frame_log: Option<FrameLog>,
    ringing: Option<Task<()>>,
    _events: Subscription,
}

impl EventEmitter<TabChanged> for TerminalModule {}

fn log_pid(terminal: &Terminal) {
    match terminal.pid() {
        Some(pid) => eprintln!("desk: terminal pid {pid}"),
        None => eprintln!("desk: terminal: the shell has no pid, it did not start"),
    }
}

impl TerminalModule {
    pub fn label(&self, cx: &App) -> Option<SharedString> {
        self.terminal
            .read(cx)
            .title()
            .or_else(|| (self.number > 1).then(|| format!("Terminal {}", self.number).into()))
    }

    pub fn badge(&self, cx: &App) -> AnyElement {
        div()
            .text_xs()
            .text_color(ActiveTheme::theme(cx).color(ColorToken::TextMuted))
            .child(shells::name(self.terminal.read(cx).shell()))
            .into_any_element()
    }

    pub fn ringing(&self) -> bool {
        self.ringing.is_some()
    }

    fn heard(&mut self, terminal: Entity<Terminal>, event: &TerminalEvent, cx: &mut Context<Self>) {
        match event {
            TerminalEvent::Started => log_pid(terminal.read(cx)),
            TerminalEvent::Exited(code) => {
                eprintln!("desk: terminal: the shell exited with {code:?}, Enter starts a new one");
            }
            TerminalEvent::Retitled => {
                eprintln!("desk: terminal title {:?}", terminal.read(cx).title());
                cx.emit(TabChanged);
            }
            TerminalEvent::Bell => {
                if self.ringing.is_some() {
                    return;
                }
                eprintln!("desk: terminal bell");
                self.ringing = Some(cx.spawn(async move |module, cx| {
                    cx.background_executor().timer(BELL).await;
                    module
                        .update(cx, |module, cx| {
                            module.ringing = None;
                            cx.emit(TabChanged);
                        })
                        .unwrap_or_else(|_| eprintln!("desk: terminal: the module is gone"));
                }));
                cx.emit(TabChanged);
            }
        }
    }
}

pub fn mount(project: &Path, number: usize, cx: &mut App) -> Entity<TerminalModule> {
    eprintln!("desk: terminal: a shell starts in {}", project.display());
    cx.new(|cx| {
        let terminal = cx.new(|cx| Terminal::new(project, cx));
        eprintln!(
            "desk: terminal shell {}",
            terminal.read(cx).shell().display()
        );
        let events = cx.subscribe(&terminal, TerminalModule::heard);
        let frame_log = env::var_os(FRAME_LOG).map(|_| FrameLog {
            output_at: None,
            seen: None,
            frames: Vec::new(),
            _output: cx.observe(&terminal, |module: &mut TerminalModule, terminal, cx| {
                let output_at = terminal.read(cx).last_output();
                if let Some(log) = &mut module.frame_log
                    && output_at.is_some()
                    && log.seen != output_at
                {
                    log.seen = output_at;
                    log.output_at = output_at;
                    cx.notify();
                }
            }),
        });
        TerminalModule {
            terminal,
            number,
            frame_log,
            ringing: None,
            _events: events,
        }
    })
}

fn palette(theme: &Theme) -> Palette {
    let color = |token| rgb_to_hsla(theme.color(token));
    Palette {
        foreground: color(ColorToken::TextBase),
        background: color(ColorToken::CardsInnerFill),
        cursor: color(ColorToken::TextStrong),
        selection: color(ColorToken::Selection),
        ansi: ANSI.map(color),
    }
}

impl FrameLog {
    fn sample(&mut self, window: &Window) {
        let now = Instant::now();
        if self
            .output_at
            .is_some_and(|at| now.duration_since(at) < QUIET)
        {
            self.frames.push(now);
            window.request_animation_frame();
            return;
        }
        let mut intervals: Vec<f64> = self
            .frames
            .windows(2)
            .filter_map(|pair| match pair {
                [from, to] => Some(to.duration_since(*from).as_secs_f64() * 1000.0),
                _ => None,
            })
            .collect();
        self.frames.clear();
        self.output_at = None;
        intervals.sort_by(f64::total_cmp);
        let (Some(median), Some(worst)) = (intervals.get(intervals.len() / 2), intervals.last())
        else {
            return;
        };
        let p95 = intervals.get(intervals.len() * 95 / 100).unwrap_or(worst);
        let slow = intervals.iter().filter(|ms| **ms > SLOW_FRAME_MS).count();
        eprintln!(
            "desk: terminal frames while output streamed: {} frames, median {median:.2} ms, p95 {p95:.2} ms, worst {worst:.2} ms, {slow} over {SLOW_FRAME_MS} ms",
            intervals.len() + 1
        );
    }
}

impl Render for TerminalModule {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if let Some(log) = &mut self.frame_log {
            log.sample(window);
        }
        let palette = palette(&ActiveTheme::theme(cx));
        self.terminal
            .update(cx, |terminal, cx| terminal.set_palette(palette, cx));
        div().size_full().child(self.terminal.clone())
    }
}
