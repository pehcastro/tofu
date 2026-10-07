use std::time::{Duration, Instant};

use desk_ui::components::card::{Header, inner_card, shell};
use desk_ui::components::shells::{Shell, ShellEvent, ShellState, Shells};
use desk_ui::components::term::TermStatus;
use desk_ui::theme::Theme;
use gpui::{Context, Div, SharedString, Window, div, prelude::*, px};

use super::Book;
use super::kit::label;
use super::lists::Strip;

const BOARD_WIDTH: f32 = 640.0;
const NARROW_WIDTH: f32 = 320.0;
const HIDDEN_PICK: usize = 4;

type Sample = (
    &'static str,
    ShellState,
    &'static str,
    u32,
    &'static str,
    &'static str,
    Option<u16>,
);

const SAMPLES: [Sample; 6] = [
    (
        "shell-1",
        ShellState::Running,
        "npm run dev",
        48213,
        "lead",
        "12m",
        Some(5173),
    ),
    (
        "shell-2",
        ShellState::Failed,
        "go test ./notes/ -run TestDelete",
        48377,
        "builder",
        "4m",
        None,
    ),
    (
        "shell-3",
        ShellState::Waiting,
        "npm run build",
        48402,
        "scout",
        "1m",
        None,
    ),
    (
        "shell-4",
        ShellState::Running,
        "cargo watch -x check",
        47920,
        "lead",
        "31m",
        None,
    ),
    (
        "shell-5",
        ShellState::Running,
        "python -m http.server 8765",
        47811,
        "judge",
        "44m",
        Some(8765),
    ),
    (
        "shell-6",
        ShellState::Failed,
        "make lint",
        48450,
        "builder",
        "20s",
        None,
    ),
];

fn output(state: ShellState) -> (Vec<SharedString>, TermStatus) {
    match state {
        ShellState::Running => (
            vec![
                "\x1b[36mVITE\x1b[0m v6.2.0  ready in \x1b[1m412\x1b[0m ms".into(),
                "  \x1b[32m\u{279c}\x1b[0m  Local:   http://localhost:5173/".into(),
                "  \x1b[2m\u{279c}  press h + enter to show help\x1b[0m".into(),
            ],
            TermStatus::Running {
                since: Instant::now(),
            },
        ),
        ShellState::Failed => (
            vec![
                "\x1b[36m=== RUN\x1b[0m   TestDelete".into(),
                "    notes_test.go:41: exit 1, want \x1b[32m2\x1b[0m, got \x1b[31m3\x1b[0m".into(),
                "\x1b[1;31mFAIL\x1b[0m notes \x1b[2m0.208s\x1b[0m".into(),
            ],
            TermStatus::Exited {
                code: 1,
                took: Duration::from_millis(208),
            },
        ),
        ShellState::Waiting => (
            vec!["\x1b[33mwaiting on you\x1b[0m: approve npm run build".into()],
            TermStatus::Running {
                since: Instant::now(),
            },
        ),
    }
}

fn samples() -> Vec<Shell> {
    SAMPLES
        .iter()
        .map(|(name, state, command, pid, by, age, port)| {
            let (lines, status) = output(*state);
            Shell {
                name: (*name).into(),
                state: *state,
                command: (*command).into(),
                pid: *pid,
                by: (*by).into(),
                age: (*age).into(),
                port: *port,
                lines,
                status,
            }
        })
        .collect()
}

struct Tile {
    shells: Vec<Shell>,
    active: usize,
    listing: bool,
    modules: Strip,
}

impl Tile {
    fn new(active: usize, listing: bool) -> Self {
        Tile {
            shells: samples(),
            active,
            listing,
            modules: Strip::modules(),
        }
    }

    fn apply(&mut self, event: ShellEvent) -> Option<String> {
        let name = |at: usize| {
            self.shells
                .get(at)
                .map_or_else(String::new, |shell| shell.name.to_string())
        };
        match event {
            ShellEvent::Pick(at) => {
                self.active = at;
                self.listing = false;
                None
            }
            ShellEvent::Close(at) if at < self.shells.len() => {
                let closed = name(at);
                self.shells.remove(at);
                if at < self.active {
                    self.active -= 1;
                }
                self.active = self.active.min(self.shells.len().saturating_sub(1));
                Some(format!("{closed} closed, still running under Hidden"))
            }
            ShellEvent::Close(_) => None,
            ShellEvent::More => {
                self.listing = true;
                None
            }
            ShellEvent::Dismiss => {
                self.listing = false;
                None
            }
            ShellEvent::Open(at) => Some(format!("{} opens as a Terminal", name(at))),
            ShellEvent::Kill(at) => Some(format!("{} killed", name(at))),
            ShellEvent::Trace(at) => Some(format!("{} trace opens in the feed", name(at))),
        }
    }
}

pub(super) struct ShellsPage {
    tiles: [Tile; 4],
}

impl ShellsPage {
    pub(super) fn new(_: &mut Context<Book>) -> Self {
        ShellsPage {
            tiles: [
                Tile::new(0, false),
                Tile::new(0, false),
                Tile::new(HIDDEN_PICK, false),
                Tile::new(1, true),
            ],
        }
    }

    pub(super) fn render(&mut self, theme: &Theme, _: &mut Window, cx: &mut Context<Book>) -> Div {
        let tile = |at: usize, width: f32, caption: &'static str, cx: &mut Context<Book>| {
            let id = SharedString::from(format!("shells-{at}"));
            let Some(tile) = self.tiles.get(at) else {
                return div();
            };
            let tabs_id: &'static str = [
                "shells-tabs-0",
                "shells-tabs-1",
                "shells-tabs-2",
                "shells-tabs-3",
            ]
            .get(at)
            .copied()
            .unwrap_or("shells-tabs");
            let modules = tile
                .modules
                .connected(tabs_id, usize::MAX, theme, cx, move |book| {
                    book.shells.tiles.get_mut(at).map(|tile| &mut tile.modules)
                });
            let shells = Shells::new(
                id,
                &tile.shells,
                tile.active,
                tile.listing,
                cx.listener(move |this, event: &ShellEvent, _, cx| {
                    let told = this
                        .shells
                        .tiles
                        .get_mut(at)
                        .and_then(|tile| tile.apply(*event));
                    if let Some(told) = told {
                        this.tell(told, cx);
                    }
                    cx.notify();
                }),
            );
            div()
                .flex()
                .flex_col()
                .gap_1()
                .child(label(caption, theme))
                .child(
                    shell(Header::Tabs(modules.into_any_element(), None), theme)
                        .w(px(width))
                        .child(inner_card(theme).child(shells)),
                )
        };
        div()
            .flex()
            .flex_col()
            .gap_4()
            .pb(px(BOARD_WIDTH / 2.0))
            .child(tile(
                0,
                BOARD_WIDTH,
                "board width: 6 shells, 3 visible, 3 more; pid, by and age shown",
                cx,
            ))
            .child(tile(
                1,
                NARROW_WIDTH,
                "320 px: module tabs and shell tabs fold into N more; pid, by and age hidden",
                cx,
            ))
            .child(tile(
                2,
                NARROW_WIDTH,
                "320 px: a hidden shell picked from the list takes the last visible slot",
                cx,
            ))
            .child(tile(
                3,
                BOARD_WIDTH,
                "board width: N more open, listing every shell",
                cx,
            ))
    }

    pub(super) fn key(&mut self, key: &str) -> bool {
        if key != "escape" {
            return false;
        }
        let open = self.tiles.iter().any(|tile| tile.listing);
        self.tiles.iter_mut().for_each(|tile| tile.listing = false);
        open
    }
}
