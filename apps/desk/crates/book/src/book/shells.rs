use std::time::{Duration, Instant};

use desk_ui::components::card::{Header, inner_card, shell};
use desk_ui::components::shells::{Runtime, Shell, ShellEvent, ShellState, Shells};
use desk_ui::theme::Theme;
use gpui::{Context, Div, SharedString, Window, div, prelude::*, px};

use super::Book;
use super::kit::label;
use super::lists::Strip;

const BOARD_WIDTH: f32 = 640.0;
const NARROW_WIDTH: f32 = 320.0;
const LEFT_OVER_PICK: usize = 1;
const FAILED_PICK: usize = 2;
const EXITED_PICK: usize = 3;
const KILLED_PICK: usize = 4;

#[derive(Clone, Copy)]
enum Kept {
    Running,
    LeftOver,
    Exited(i32),
    Killed,
}

type Sample = (&'static str, Kept, &'static str, i64, Option<u16>, u64);

const SAMPLES: [Sample; 5] = [
    (
        "shell-1",
        Kept::Running,
        "bun run dev",
        48213,
        Some(3000),
        724,
    ),
    (
        "shell-2",
        Kept::LeftOver,
        "npx serve dist -l 5000",
        47920,
        Some(5000),
        1870,
    ),
    (
        "shell-3",
        Kept::Exited(1),
        "bun run build --watch",
        48377,
        None,
        20,
    ),
    (
        "shell-4",
        Kept::Exited(0),
        "bun test --watch",
        48511,
        None,
        66,
    ),
    (
        "shell-5",
        Kept::Killed,
        "python -m http.server 8765",
        47811,
        Some(8765),
        243,
    ),
];

fn shown(kept: Kept, secs: u64) -> (ShellState, Option<SharedString>, Vec<SharedString>, Runtime) {
    let took = Duration::from_secs(secs);
    let since = Instant::now()
        .checked_sub(took)
        .unwrap_or_else(Instant::now);
    match kept {
        Kept::Running => (
            ShellState::Running,
            Some("ready: it printed a ready line".into()),
            vec![
                "$ bun run --hot src/index.ts".into(),
                "Started development server: http://localhost:3000".into(),
            ],
            Runtime::Live(since),
        ),
        Kept::LeftOver => (
            ShellState::LeftOver,
            Some("ready: its port opened".into()),
            vec![" \x1b[32mServing!\x1b[0m  Local: http://localhost:5000".into()],
            Runtime::Live(since),
        ),
        Kept::Exited(code) => (
            ShellState::Exited(Some(code)),
            None,
            vec![if code == 0 {
                "\x1b[2m3 pass, 0 fail\x1b[0m".into()
            } else {
                "\x1b[1;31merror\x1b[0m: Could not resolve \"hono/jsx\"".into()
            }],
            Runtime::Ended(took),
        ),
        Kept::Killed => (
            ShellState::Killed,
            Some("the wait ran out".into()),
            vec!["Serving HTTP on :: port 8765 (http://[::]:8765/) ...".into()],
            Runtime::Ended(took),
        ),
    }
}

fn samples() -> Vec<Shell> {
    SAMPLES
        .iter()
        .map(|(name, kept, command, pid, port, secs)| {
            let (state, ready, lines, runtime) = shown(*kept, *secs);
            Shell {
                name: (*name).into(),
                state,
                command: (*command).into(),
                dir: "\u{2026}\\sandbox\\hono-starter".into(),
                pid: Some(*pid),
                starter: None,
                port: *port,
                ready,
                runtime,
                lines,
            }
        })
        .collect()
}

struct Tile {
    shells: Vec<Shell>,
    active: usize,
    listing: bool,
    asking: Option<usize>,
    modules: Strip,
}

impl Tile {
    fn new(active: usize, listing: bool) -> Self {
        Tile {
            shells: samples(),
            active,
            listing,
            asking: None,
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
                self.asking = None;
                None
            }
            ShellEvent::AskKill(at) => {
                self.asking = Some(at);
                None
            }
            ShellEvent::Keep => {
                self.asking = None;
                Some("kept, nothing was killed".to_owned())
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
            ShellEvent::Trace(at) => Some(format!("{} trace opens in the feed", name(at))),
        }
    }
}

pub(super) struct ShellsPage {
    tiles: [Tile; 6],
    killed: SharedString,
}

impl ShellsPage {
    pub(super) fn new(_: &mut Context<Book>) -> Self {
        ShellsPage {
            tiles: [
                Tile {
                    asking: Some(0),
                    ..Tile::new(0, false)
                },
                Tile::new(LEFT_OVER_PICK, false),
                Tile::new(FAILED_PICK, false),
                Tile::new(EXITED_PICK, false),
                Tile::new(KILLED_PICK, false),
                Tile::new(0, true),
            ],
            killed: "on_kill: not called yet".into(),
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
                "shells-tabs-4",
                "shells-tabs-5",
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
                tile.shells.clone(),
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
            )
            .asking(tile.asking)
            .on_kill(cx.listener(move |this, picked: &usize, _, cx| {
                let tile = this.shells.tiles.get_mut(at);
                let name = tile
                    .as_ref()
                    .and_then(|tile| tile.shells.get(*picked))
                    .map_or_else(SharedString::default, |shell| shell.name.clone());
                if let Some(tile) = tile {
                    tile.asking = None;
                }
                this.shells.killed = format!("on_kill({picked}) from tile {at}: {name}").into();
                cx.notify();
            }));
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
            .child(label(self.killed.clone(), theme))
            .child(tile(
                0,
                BOARD_WIDTH,
                "running: a dev server on :3000, its runtime ticking, kill asked before it runs",
                cx,
            ))
            .child(tile(
                1,
                BOARD_WIDTH,
                "left over: still running, the tofu that kept it is gone, kill offered",
                cx,
            ))
            .child(tile(
                2,
                BOARD_WIDTH,
                "exited 1: a watcher that died, its runtime frozen, no kill",
                cx,
            ))
            .child(tile(
                3,
                BOARD_WIDTH,
                "exited 0: a watcher that ended on its own, no kill",
                cx,
            ))
            .child(tile(
                4,
                BOARD_WIDTH,
                "killed: stopped through shell.kill, its runtime frozen, no kill",
                cx,
            ))
            .child(tile(
                5,
                NARROW_WIDTH,
                "320 px: every kept shell listed under N more",
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
