use std::path::{Path, PathBuf};

use desk_terminal::{Palette, Terminal, TerminalEvent};
use desk_ui::components::terminal::{TerminalState, TerminalTile};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AppContext, ClickEvent, Context, Div, Entity, SharedString, Subscription, Window, div,
    prelude::*, px, rgb_to_hsla,
};

use super::Book;
use super::kit::label;

const BOARD_WIDTH: f32 = 640.0;
const TILE_HEIGHT: f32 = 300.0;
const REPOSITORY_DEPTH: usize = 4;
const UNKNOWN_EXIT: i32 = -1;
const SCRATCH: [&str; 5] = [".local", "desk-app", "shots", "desk-139", "scratch"];
const TITLES: [&str; 2] = ["Terminal", "Terminal 2"];
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

struct Shell {
    terminal: Entity<Terminal>,
    state: TerminalState,
    _exits: Subscription,
}

struct Shells {
    folder: PathBuf,
    tiles: Vec<Shell>,
}

pub(super) struct TerminalPage {
    shells: Option<Result<Shells, String>>,
    readout: SharedString,
}

impl TerminalPage {
    pub(super) fn new(_: &mut Context<Book>) -> Self {
        TerminalPage {
            shells: None,
            readout: "type exit 1 into a tile to see it exit".into(),
        }
    }

    pub(super) fn render(&mut self, theme: &Theme, _: &mut Window, cx: &mut Context<Book>) -> Div {
        let shells = self.shells.get_or_insert_with(|| {
            scratch().map(|folder| Shells {
                tiles: (0..TITLES.len())
                    .map(|index| {
                        let terminal = cx.new(|cx| Terminal::new(&folder, cx));
                        let exits = cx.subscribe(
                            &terminal,
                            move |book: &mut Book, _, event: &TerminalEvent, cx| {
                                book.terminal.exited(index, *event, cx)
                            },
                        );
                        Shell {
                            terminal,
                            state: TerminalState::Running,
                            _exits: exits,
                        }
                    })
                    .collect(),
                folder,
            })
        });
        let shells = match shells {
            Ok(shells) => shells,
            Err(error) => return div().child(label(error.clone(), theme)),
        };
        let cwd = SharedString::from(shells.folder.display().to_string());
        let palette = palette(theme);
        let tiles = shells
            .tiles
            .iter()
            .zip(TITLES)
            .enumerate()
            .map(|(index, (shell, title))| {
                shell
                    .terminal
                    .update(cx, |terminal, cx| terminal.set_palette(palette, cx));
                let tile =
                    TerminalTile::new(
                        format!("terminal-{index}"),
                        title,
                        cwd.clone(),
                        shell.state,
                        shell.terminal.clone(),
                    )
                    .on_restart(cx.listener(
                        move |book, _: &ClickEvent, _, cx| book.terminal.restart(index, cx),
                    ));
                div().w(px(BOARD_WIDTH)).h(px(TILE_HEIGHT)).child(tile)
            })
            .collect::<Vec<_>>();
        div()
            .flex()
            .flex_col()
            .gap_4()
            .child(label(self.readout.clone(), theme))
            .children(tiles)
    }

    fn shell(&mut self, index: usize) -> Option<&mut Shell> {
        match &mut self.shells {
            Some(Ok(shells)) => shells.tiles.get_mut(index),
            _ => None,
        }
    }

    fn exited(&mut self, index: usize, event: TerminalEvent, cx: &mut Context<Book>) {
        let code = match event {
            TerminalEvent::Exited(code) => code,
            TerminalEvent::Started => {
                if let Some(shell) = self.shell(index) {
                    shell.state = TerminalState::Running;
                    cx.notify();
                }
                return;
            }
            TerminalEvent::Retitled | TerminalEvent::Bell => return,
        };
        let code = code
            .and_then(|code| i32::try_from(code).ok())
            .unwrap_or(UNKNOWN_EXIT);
        let Some(shell) = self.shell(index) else {
            return;
        };
        shell.state = TerminalState::Exited { code };
        self.readout = format!(
            "{}: Exited {{ code: {code} }}",
            TITLES.get(index).unwrap_or(&"?")
        )
        .into();
        cx.notify();
    }

    fn restart(&mut self, index: usize, cx: &mut Context<Book>) {
        let Some(shell) = self.shell(index) else {
            return;
        };
        shell.state = TerminalState::Running;
        shell
            .terminal
            .update(cx, |terminal, cx| terminal.restart(cx));
        self.readout = format!(
            "{}: restart called, Running",
            TITLES.get(index).unwrap_or(&"?")
        )
        .into();
        cx.notify();
    }
}
