use std::cell::Cell;
use std::fs;
use std::path::PathBuf;
use std::process;
use std::rc::Rc;

use desk_perf::limits::{BENCH_SETTLE, BENCH_WARMUP_FRAMES};
use desk_perf::{BenchResult, run_bench};
use desk_ui::live;
use desk_ui::theme::Mode;
use gpui::{
    App, Modifiers, PlatformInput, ScrollDelta, ScrollWheelEvent, TouchPhase, Window, WindowHandle,
    point, px,
};

use crate::desk::Desk;
use crate::{ScreenLaunch, Target, screen};

pub const KNOWN: &str = "known scenarios: editor-scroll, boardy-list, theme-switch";
const EDITOR_LINES: usize = 2000;
const EDITOR_FILE: &str = "bench.rs";
const SCROLL_PX: f32 = 120.0;
const SCROLL_STEPS: u32 = 400;
const SCROLL_AT_X: f32 = 0.65;
const SCROLL_AT_Y: f32 = 0.5;
const FRAMES_PER_MODE: u32 = 60;
const MODES: [Mode; 3] = [Mode::Light, Mode::Dark, Mode::Light];

#[derive(Clone, Copy)]
pub enum Scenario {
    EditorScroll,
    BoardyList,
    ThemeSwitch,
}

impl Scenario {
    pub fn named(name: &str) -> Option<Self> {
        match name {
            "editor-scroll" => Some(Scenario::EditorScroll),
            "boardy-list" => Some(Scenario::BoardyList),
            "theme-switch" => Some(Scenario::ThemeSwitch),
            _ => None,
        }
    }

    pub fn launch(self) -> Result<ScreenLaunch, String> {
        let (name, target) = match self {
            Scenario::EditorScroll => ("editor", Some(Target::File(editor_file()?))),
            Scenario::BoardyList => ("boardy", None),
            Scenario::ThemeSwitch => ("work", None),
        };
        Ok(ScreenLaunch {
            open: screen(name)?,
            name: name.to_owned(),
            board: None,
            target,
        })
    }

    fn name(self) -> &'static str {
        match self {
            Scenario::EditorScroll => "editor-scroll",
            Scenario::BoardyList => "boardy-list",
            Scenario::ThemeSwitch => "theme-switch",
        }
    }

    fn span(self) -> &'static str {
        match self {
            Scenario::EditorScroll => "bench editor-scroll step",
            Scenario::BoardyList => "bench boardy-list step",
            Scenario::ThemeSwitch => "bench theme-switch step",
        }
    }

    fn steps(self) -> u32 {
        match self {
            Scenario::EditorScroll | Scenario::BoardyList => SCROLL_STEPS,
            Scenario::ThemeSwitch => FRAMES_PER_MODE.saturating_mul(MODES.len() as u32),
        }
    }

    fn step(self, frame: u32, window: &mut Window, cx: &mut App) {
        let Some(at) = frame.checked_sub(BENCH_WARMUP_FRAMES) else {
            return;
        };
        desk_perf::span!(self.span());
        match self {
            Scenario::EditorScroll | Scenario::BoardyList => scroll(window, cx),
            Scenario::ThemeSwitch => {
                if at % FRAMES_PER_MODE != 0 {
                    return;
                }
                let Some(mode) = MODES.get((at / FRAMES_PER_MODE) as usize) else {
                    return;
                };
                if let Err(error) = live::set_mode(*mode, cx) {
                    eprintln!(
                        "desk: bench theme-switch has no {} set: {error}",
                        mode.label()
                    );
                }
            }
        }
    }
}

fn scratch() -> PathBuf {
    std::env::temp_dir().join(format!("tofu-desk-bench-{}", process::id()))
}

fn editor_file() -> Result<PathBuf, String> {
    let folder = scratch();
    let path = folder.join(EDITOR_FILE);
    let text: String = (0..EDITOR_LINES)
        .map(|line| format!("pub fn line_{line}(x: u64) -> u64 {{ x * {line} + 1 }}\n"))
        .collect();
    fs::create_dir_all(&folder)
        .and_then(|()| fs::write(&path, text))
        .map_err(|error| format!("the bench file {} was not written: {error}", path.display()))?;
    Ok(path)
}

fn scroll(window: &mut Window, cx: &mut App) {
    let size = window.viewport_size();
    let event = ScrollWheelEvent {
        position: point(size.width * SCROLL_AT_X, size.height * SCROLL_AT_Y),
        delta: ScrollDelta::Pixels(point(px(0.0), px(-SCROLL_PX))),
        modifiers: Modifiers::default(),
        touch_phase: TouchPhase::Moved,
    };
    window.dispatch_event(PlatformInput::ScrollWheel(event), cx);
}

pub fn start(scenario: Scenario, desk: WindowHandle<Desk>, failed: Rc<Cell<bool>>, cx: &mut App) {
    cx.spawn(async move |cx| {
        cx.background_executor().timer(BENCH_SETTLE).await;
        let started = desk.update(cx, |_, window, cx| {
            run_bench(
                scenario.name().to_owned(),
                scenario.steps(),
                move |frame, window, cx| scenario.step(frame, window, cx),
                move |result, cx| finish(&result, &failed, cx),
                window,
                cx,
            )
        });
        if let Err(error) = started {
            eprintln!("desk: bench {} found no window: {error:#}", scenario.name());
        }
    })
    .detach();
}

fn finish(result: &BenchResult, failed: &Cell<bool>, cx: &mut App) {
    match result.json_line() {
        Ok(line) => println!("{line}"),
        Err(error) => {
            eprintln!("desk: bench result could not be written: {error}");
            failed.set(true);
        }
    }
    let folder = scratch();
    if folder.exists()
        && let Err(error) = fs::remove_dir_all(&folder)
    {
        eprintln!("desk: bench folder {} stays: {error}", folder.display());
    }
    cx.quit();
}
