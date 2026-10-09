use std::env;
use std::path::Path;
use std::time::{Duration, Instant};

use desk_terminal::{Palette, Terminal};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    App, AppContext, Context, Entity, IntoElement, Render, Subscription, Window, div, prelude::*,
    rgb_to_hsla,
};

const FRAME_LOG: &str = "DESK_FRAME_LOG";
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
    frames: Vec<Instant>,
    _output: Subscription,
}

pub struct TerminalModule {
    terminal: Entity<Terminal>,
    frame_log: Option<FrameLog>,
}

pub fn mount(project: &Path, cx: &mut App) -> Entity<TerminalModule> {
    eprintln!("desk: terminal: a shell starts in {}", project.display());
    cx.new(|cx| {
        let terminal = cx.new(|cx| Terminal::new(project, cx));
        match terminal.read(cx).pid() {
            Some(pid) => eprintln!("desk: terminal pid {pid}"),
            None => eprintln!("desk: terminal: the shell has no pid, it did not start"),
        }
        let frame_log = env::var_os(FRAME_LOG).map(|_| FrameLog {
            output_at: None,
            frames: Vec::new(),
            _output: cx.observe(&terminal, |module: &mut TerminalModule, _, cx| {
                if let Some(log) = &mut module.frame_log {
                    log.output_at = Some(Instant::now());
                    cx.notify();
                }
            }),
        });
        TerminalModule {
            terminal,
            frame_log,
        }
    })
}

fn palette(theme: &Theme) -> Palette {
    let color = |token| rgb_to_hsla(theme.color(token));
    Palette {
        foreground: color(ColorToken::TextBase),
        background: color(ColorToken::CardsInnerFill),
        cursor: color(ColorToken::TextStrong),
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
