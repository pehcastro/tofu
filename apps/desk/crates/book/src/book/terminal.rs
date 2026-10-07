use std::path::{Path, PathBuf};

use desk_terminal::Terminal;
use desk_ui::components::chip::mono;
use desk_ui::components::paint::ink;
use desk_ui::components::size::{FONT_SMALL, T2};
use desk_ui::components::terminal::{TerminalState, TerminalTile};
use desk_ui::theme::Theme;
use gpui::{
    AnyElement, AppContext, ClickEvent, Context, Div, Entity, SharedString, Window, div,
    prelude::*, px,
};

use super::Book;
use super::kit::label;

const BOARD_WIDTH: f32 = 640.0;
const LIVE_HEIGHT: f32 = 360.0;
const EXITED_HEIGHT: f32 = 240.0;
const SCREEN_PAD: f32 = 8.0;
const REPOSITORY_DEPTH: usize = 4;
const SCRATCH: [&str; 5] = [".local", "desk-app", "shots", "desk-139", "scratch"];
const LAST_SCREEN: [&str; 6] = [
    "$ go test ./notes/ -run TestDelete",
    "--- FAIL: TestDelete (0.00s)",
    "    notes_test.go:41: want 2, got 3",
    "FAIL",
    "$ exit 1",
    "[shell exited with code 1]",
];

fn scratch() -> Result<PathBuf, String> {
    let manifest = Path::new(env!("CARGO_MANIFEST_DIR"));
    let repository = manifest
        .ancestors()
        .nth(REPOSITORY_DEPTH)
        .ok_or_else(|| format!("{} has no repository above it", manifest.display()))?;
    let folder: PathBuf = [repository]
        .into_iter()
        .chain(SCRATCH.map(Path::new))
        .collect();
    std::fs::create_dir_all(&folder)
        .map_err(|error| format!("{} was not made: {error}", folder.display()))?;
    Ok(folder)
}

enum Screen {
    Last,
    Live(Entity<Terminal>),
}

struct Shells {
    folder: PathBuf,
    live: Entity<Terminal>,
    exited: Screen,
}

pub(super) struct TerminalPage {
    shells: Option<Result<Shells, String>>,
    restarts: usize,
    readout: SharedString,
}

impl TerminalPage {
    pub(super) fn new(_: &mut Context<Book>) -> Self {
        TerminalPage {
            shells: None,
            restarts: 0,
            readout: "on_restart: not called yet".into(),
        }
    }

    fn last_screen(theme: &Theme) -> Div {
        div()
            .size_full()
            .p(px(SCREEN_PAD))
            .font_family(mono(theme))
            .text_size(px(FONT_SMALL))
            .text_color(ink(theme, T2))
            .children(LAST_SCREEN.map(|line| div().child(line)))
    }

    pub(super) fn render(&mut self, theme: &Theme, _: &mut Window, cx: &mut Context<Book>) -> Div {
        let shells = self.shells.get_or_insert_with(|| {
            scratch().map(|folder| Shells {
                live: cx.new(|cx| Terminal::new(&folder, cx)),
                folder,
                exited: Screen::Last,
            })
        });
        let shells = match shells {
            Ok(shells) => shells,
            Err(error) => return div().child(label(error.clone(), theme)),
        };
        let cwd = SharedString::from(shells.folder.display().to_string());
        let (state, body): (TerminalState, AnyElement) = match &shells.exited {
            Screen::Last => (
                TerminalState::Exited { code: 1 },
                Self::last_screen(theme).into_any_element(),
            ),
            Screen::Live(terminal) => (TerminalState::Running, terminal.clone().into_any_element()),
        };
        let live = TerminalTile::new(
            "terminal-live",
            "Terminal",
            cwd.clone(),
            TerminalState::Running,
            shells.live.clone(),
        );
        let exited = TerminalTile::new("terminal-exited", "Terminal 2", cwd, state, body)
            .on_restart(cx.listener(|this, _: &ClickEvent, _, cx| this.terminal.restart(cx)));
        let tile = |caption: &'static str, height: f32, shown: TerminalTile| {
            div()
                .flex()
                .flex_col()
                .gap_1()
                .child(label(caption, theme))
                .child(div().w(px(BOARD_WIDTH)).h(px(height)).child(shown))
        };
        div()
            .flex()
            .flex_col()
            .gap_4()
            .child(label(self.readout.clone(), theme))
            .child(tile(
                "running: the platform shell in a scratch folder; click it and type",
                LIVE_HEIGHT,
                live,
            ))
            .child(tile(
                "exited with code 1: the last screen dimmed, the code and Restart",
                EXITED_HEIGHT,
                exited,
            ))
    }

    fn restart(&mut self, cx: &mut Context<Book>) {
        let Some(Ok(shells)) = &mut self.shells else {
            return;
        };
        let folder = shells.folder.clone();
        shells.exited = Screen::Live(cx.new(|cx| Terminal::new(&folder, cx)));
        self.restarts += 1;
        self.readout = format!(
            "on_restart: called {}x, Terminal 2 runs a new shell",
            self.restarts
        )
        .into();
        cx.notify();
    }
}
