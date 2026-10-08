use std::path::PathBuf;
use std::rc::Rc;
use std::time::Instant;

use desk_core::model::Store;
use desk_tiling::{
    Action, Module, NUDGE, Preset, Side, Store as Layouts, Target, TileId, WORKSPACE_EDGE,
    Workspace, Zone,
};
use desk_ui::components::empty::{EmptyAction, empty_state};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::overlay::{MenuButton, MenuItem, context_menu};
use desk_ui::components::tabs::{Tab, TabEvent, header_tabs, new_tab_glyph};
use desk_ui::components::tiling_board::{Host, Reopened, Settled, TilingBoard};
use desk_ui::icon::Icon;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::Theme;
use gpui::{
    AnyElement, AnyView, App, AppContext, Context, Entity, EventEmitter, FocusHandle, Focusable,
    IntoElement, KeyDownEvent, Pixels, Point, Render, SharedString, Subscription, Window, div,
    prelude::*, px,
};

use crate::modules::chat::cassette::{Replay, Step};
use crate::modules::chat::{self, Chat};
use crate::modules::file_edits::{self, FileEdits};
use crate::modules::replayed;
use crate::modules::shells::{self, Kill, Shells};
use crate::modules::subagents::{self, ExpandAgent, Subagents};
use crate::project;

const BOARD: &str = "36-agents";
const WORK_NAME: &str = "work";
const OPENABLE: [Module; 4] = [
    Module::Chat,
    Module::SubAgents,
    Module::FileEdits,
    Module::Shells,
];

pub fn open(board: Option<&str>, window: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    let replay = match board {
        None => None,
        Some(BOARD) => Some(Replay::read()?),
        Some(other) => {
            return Err(format!(
                "the work screen runs a live session, or replays {BOARD}, not {other}"
            ));
        }
    };
    replayed::fonts(cx)?;
    let project = match replay {
        Some(_) => PathBuf::from(BOARD),
        None => project::launch()?,
    };
    Ok(build(replay, project, window, cx)?.into())
}

pub fn open_in(
    project: PathBuf,
    window: &mut Window,
    cx: &mut App,
) -> Result<Entity<Work>, String> {
    replayed::fonts(cx)?;
    build(None, project, window, cx)
}

#[derive(Clone)]
struct Mounted {
    store: Entity<Store>,
    chat: Entity<Chat>,
    subagents: Entity<Subagents>,
    file_edits: Entity<FileEdits>,
    shells: Entity<Shells>,
}

fn build(
    replay: Option<Replay>,
    project: PathBuf,
    window: &mut Window,
    cx: &mut App,
) -> Result<Entity<Work>, String> {
    let layouts = Layouts::for_project(&project).map_err(|error| error.to_string())?;
    let mut workspaces = layouts
        .load()
        .unwrap_or_else(|error| {
            eprintln!("the saved layout is unreadable, so the work preset is used: {error}");
            None
        })
        .unwrap_or_default();
    if workspaces.is_empty() {
        workspaces.push(Workspace::new(WORK_NAME, Preset::Work));
    }
    let store = cx.new(|_| Store::default());
    let chat = match replay {
        Some(_) => chat::fed(store.clone(), window, cx),
        None => chat::live(project, store.clone(), window, cx),
    };
    let killer = chat.downgrade();
    let kill: Kill = Rc::new(move |shell, cx| {
        killer
            .update(cx, |chat, cx| chat.kill(shell, cx))
            .unwrap_or_else(|_| eprintln!("desk: shells: the chat is gone, so nothing was killed"));
    });
    let mounted = Mounted {
        subagents: subagents::mount(store.clone(), cx),
        file_edits: file_edits::mount(store.clone(), cx),
        shells: shells::mount(store.clone(), kill, cx),
        store,
        chat,
    };
    Ok(cx.new(|cx| {
        let focus = cx.focus_handle();
        focus.focus(window, cx);
        let watched = [
            cx.observe(&mounted.store, |_, _, cx| cx.notify()),
            cx.subscribe(
                &mounted.subagents,
                |_, module, ExpandAgent(agent): &ExpandAgent, cx| {
                    let view = module.update(cx, |module, cx| module.screen(Some(*agent), cx));
                    cx.emit(Expanded {
                        name: SUB_AGENTS,
                        view: view.into(),
                    });
                },
            ),
        ];
        let bodies = mounted.clone();
        let titles = mounted.clone();
        let sessions = mounted.store.clone();
        let expandable = mounted.clone();
        let board = TilingBoard::new(
            workspaces,
            Host {
                board: |work: &mut Work| &mut work.board,
                body: Box::new(
                    move |stack, _, theme, cx| match stack.modules.get(stack.active) {
                        Some(module) => body(&bodies, module, theme),
                        None => empty_tile(stack.id, theme, cx),
                    },
                ),
                title: Box::new(move |module, mark, cx| Tab {
                    label: module.name().to_owned().into(),
                    icon: glyph(module),
                    count: count(&titles, module, cx),
                    mark,
                }),
                subtitle: Box::new(move |module, cx| {
                    (*module == Module::Chat)
                        .then(|| session(&sessions, cx))
                        .flatten()
                        .map(|name| div().child(name).into_any_element())
                }),
                spawnable: OPENABLE.to_vec(),
                spawns: OPENABLE.to_vec(),
                settled: settled(layouts),
                expand: Box::new(move |module, _, cx| expand(&expandable, module, cx)),
                origin: (0.0, 0.0),
                below: 0.0,
            },
        );
        let plus = MenuButton::new("New".into(), Vec::new(), cx);
        plus.update(cx, |button, _| {
            button.trigger(|_, theme| new_tab_glyph("work-new", theme));
        });
        Work {
            board,
            nudge: None,
            plus,
            pointed: 0,
            menu_at: None,
            focus,
            mounted,
            replay,
            _watched: watched,
        }
    }))
}

pub struct Work {
    board: TilingBoard<Work>,
    nudge: Option<f32>,
    plus: Entity<MenuButton>,
    pointed: usize,
    menu_at: Option<Point<Pixels>>,
    focus: FocusHandle,
    mounted: Mounted,
    replay: Option<Replay>,
    _watched: [Subscription; 2],
}

fn glyph(module: &Module) -> Option<Glyph> {
    match module {
        Module::Chat => Some(Glyph::Chat),
        Module::SubAgents => Some(Glyph::Agents),
        Module::FileEdits => Some(Glyph::File),
        Module::Shells | Module::Terminal => Some(Glyph::Terminal),
        Module::Editor | Module::Browser | Module::SourceControl | Module::Plugin(_) => None,
    }
}

fn module_row(module: &Module) -> MenuItem {
    let row = MenuItem::action(module.name());
    match glyph(module) {
        Some(mark) => row.icon(mark),
        None => row,
    }
}

fn side_of(key: &str) -> Option<Side> {
    match key {
        "left" => Some(Side::Left),
        "right" => Some(Side::Right),
        "up" => Some(Side::Top),
        "down" => Some(Side::Bottom),
        _ => None,
    }
}

fn count(mounted: &Mounted, module: &Module, cx: &App) -> Option<u32> {
    let count = match module {
        Module::SubAgents => mounted.subagents.read(cx).live(),
        Module::FileEdits => mounted.file_edits.read(cx).count(),
        Module::Shells => mounted.shells.read(cx).count(),
        Module::Chat
        | Module::Editor
        | Module::Terminal
        | Module::Browser
        | Module::SourceControl
        | Module::Plugin(_) => return None,
    };
    u32::try_from(count).ok()
}

fn session(store: &Entity<Store>, cx: &App) -> Option<SharedString> {
    let (id, session) = store.read(cx).sessions.iter().next()?;
    let name = if session.name.is_empty() {
        id
    } else {
        &session.name
    };
    Some(name.clone().into())
}

fn body(mounted: &Mounted, module: &Module, theme: &Theme) -> AnyElement {
    match module {
        Module::Chat => mounted.chat.clone().into_any_element(),
        Module::SubAgents => mounted.subagents.clone().into_any_element(),
        Module::FileEdits => mounted.file_edits.clone().into_any_element(),
        Module::Shells => mounted.shells.clone().into_any_element(),
        Module::Editor
        | Module::Terminal
        | Module::Browser
        | Module::SourceControl
        | Module::Plugin(_) => empty_state(
            SharedString::from(format!("work-empty-{}", module.name())),
            module.name().to_owned(),
            Some("not mounted in the work screen yet".into()),
            &[],
            &[],
            theme,
            |_, _, _| {},
        )
        .into_any_element(),
    }
}

pub struct Expanded {
    pub name: &'static str,
    pub view: AnyView,
}

impl EventEmitter<Expanded> for Work {}

const SUB_AGENTS: &str = "Sub-agents";

pub const EXPANDABLE: [&str; 4] = ["Chat", SUB_AGENTS, "File edits", "Shells"];

fn expand(mounted: &Mounted, module: &Module, cx: &mut Context<Work>) {
    let view: AnyView = match module {
        Module::Chat => mounted.chat.clone().into(),
        Module::SubAgents => mounted
            .subagents
            .update(cx, |module, cx| module.screen(None, cx))
            .into(),
        Module::FileEdits => mounted.file_edits.clone().into(),
        Module::Shells => mounted.shells.clone().into(),
        Module::Editor
        | Module::Terminal
        | Module::Browser
        | Module::SourceControl
        | Module::Plugin(_) => {
            return eprintln!(
                "desk: work: {} is not mounted, so it does not expand",
                module.name()
            );
        }
    };
    match EXPANDABLE.into_iter().find(|name| *name == module.name()) {
        Some(name) => cx.emit(Expanded { name, view }),
        None => eprintln!("desk: work: {} has no tab name", module.name()),
    }
}

fn empty_tile(tile: TileId, theme: &Theme, cx: &mut Context<Work>) -> AnyElement {
    let actions: Vec<EmptyAction> = OPENABLE
        .iter()
        .map(|module| EmptyAction {
            label: module.name().to_owned().into(),
            glyph: glyph(module),
            keys: None,
        })
        .collect();
    let this = cx.weak_entity();
    empty_state(
        SharedString::from(format!("work-empty-tile-{}", tile.0)),
        "Empty tile",
        Some("open a module, or use a shortcut".into()),
        &actions,
        &[],
        theme,
        move |at, _, cx| {
            let Some(module) = OPENABLE.get(at).cloned() else {
                return;
            };
            this.update(cx, |work, cx| {
                work.board.apply(true, |workspace, area| {
                    workspace.open_at(module, Target::Tile(tile, Zone::Stack { at: 0 }), area)
                });
                cx.notify();
            })
            .unwrap_or_else(|_| eprintln!("desk: work: the screen is gone"));
        },
    )
    .into_any_element()
}

fn settled(layouts: Layouts) -> Settled {
    Box::new(move |all, active, area| {
        if let Err(error) = layouts.save(all) {
            eprintln!("the layout was not saved: {error}");
        }
        for (at, workspace) in all.iter().enumerate() {
            let tiles: Vec<String> = workspace
                .tiles(area)
                .into_iter()
                .map(|(stack, rect)| {
                    let names: Vec<&str> = stack.modules.iter().map(Module::name).collect();
                    format!(
                        "tile {} [{}] {:.1},{:.1} {:.1}x{:.1}",
                        stack.id.0,
                        names.join(", "),
                        rect.x,
                        rect.y,
                        rect.w,
                        rect.h
                    )
                })
                .collect();
            eprintln!(
                "desk: work: workspace {}/{}{} {:?} preset {} locked {} pinned {}: {}",
                at + 1,
                all.len(),
                if at == active { " active" } else { "" },
                workspace.name,
                workspace.preset.name(),
                workspace.locked,
                workspace.pinned,
                tiles.join("; ")
            );
        }
        if let Some(stack) = all
            .get(active)
            .and_then(|workspace| workspace.focus().and_then(|id| workspace.stack(id)))
        {
            eprintln!(
                "desk: work: focus tile {} {}",
                stack.id.0,
                stack
                    .modules
                    .get(stack.active)
                    .map_or("empty", Module::name)
            );
        }
    })
}

impl Work {
    pub fn chat(&self) -> &Entity<Chat> {
        &self.mounted.chat
    }

    pub fn last_closed_at(&self) -> Option<Instant> {
        self.board.last_closed_at()
    }

    pub fn reopen(&mut self, window: &mut Window, cx: &mut Context<Self>) -> Option<Reopened> {
        let reopened = self.board.reopen();
        self.focus.focus(window, cx);
        cx.notify();
        reopened
    }

    pub fn fit(&mut self, below: f32) {
        self.board.keep_below(below);
    }

    pub fn focus_composer(&self, window: &mut Window, cx: &mut App) -> bool {
        self.mounted
            .chat
            .update(cx, |chat, cx| chat.focus_composer(window, cx))
    }

    pub fn cancel_drag(&mut self, cx: &mut Context<Self>) -> bool {
        if !self.board.holding() {
            return false;
        }
        self.board.run(Action::Cancel, "");
        cx.notify();
        true
    }

    pub fn run(&mut self, action: Action, key: &str, window: &mut Window, cx: &mut Context<Self>) {
        self.nudge = None;
        self.board.run(action, key);
        if matches!(
            action,
            Action::Close
                | Action::CloseTab
                | Action::NewWorkspace
                | Action::GoToWorkspace
                | Action::SendToNewWorkspace
        ) {
            self.focus.focus(window, cx);
        }
        cx.notify();
    }

    fn frame(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(replay) = &mut self.replay else {
            return;
        };
        window.request_animation_frame();
        let stepped = match replay.step() {
            Ok(Step::Feed(event)) => {
                self.mounted
                    .chat
                    .update(cx, |chat, cx| chat.take(&[event], cx));
                Ok(())
            }
            Ok(Step::Restart) => {
                self.mounted.chat.update(cx, |chat, cx| chat.restart(cx));
                Ok(())
            }
            Ok(Step::Report) => self.settle(cx),
            Err(error) => Err(error),
        };
        if let Err(error) = stepped {
            eprintln!("desk: the cassette has a bad line: {error}");
            cx.quit();
        }
    }

    fn settle(&mut self, cx: &mut Context<Self>) -> Result<(), String> {
        self.replay = None;
        let events = replayed::whole()?;
        self.mounted.chat.update(cx, |chat, cx| {
            chat.restart(cx);
            chat.take(&events, cx);
        });
        let agents = self
            .mounted
            .subagents
            .update(cx, |module, cx| module.counted(cx));
        let files = self
            .mounted
            .file_edits
            .update(cx, |module, cx| module.counted(cx));
        let shells = self
            .mounted
            .shells
            .update(cx, |module, cx| module.counted(cx));
        eprintln!(
            "desk: work tiles from store {:?}: sub-agents {agents}; file edits {files}; shells {shells}",
            self.mounted.store.entity_id()
        );
        Ok(())
    }

    fn key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let stroke = &event.keystroke;
        let held = stroke.modifiers;
        let key = stroke.key.as_str();
        if key == "enter" && held.control && held.shift && !held.alt {
            match self.board.aimed_module() {
                Some(module) => expand(&self.mounted, &module, cx),
                None => eprintln!("desk: work: no tile is focused, so nothing expands"),
            }
            return;
        }
        if self.board.key(event) {
            self.nudge = None;
            self.focus.focus(window, cx);
            return cx.notify();
        }
        if !(held.control && held.alt) {
            return;
        }
        let side = match key {
            "+" | "=" => {
                self.nudge = Some(NUDGE);
                return;
            }
            "-" => {
                self.nudge = Some(-NUDGE);
                return;
            }
            _ => match side_of(key) {
                Some(side) => side,
                None => return,
            },
        };
        let nudge = self.nudge.take();
        let shift = held.shift;
        self.board.apply(true, |workspace, area| match nudge {
            Some(step) => workspace.nudge(side, step, area),
            None if shift => workspace.move_focused(side, area),
            None => workspace.focus_toward(side, area),
        });
        cx.notify();
    }

    fn plus_menu(openable: &[&'static str]) -> Vec<MenuItem> {
        let new = MenuItem::Action {
            label: "New workspace".into(),
            keys: Some("Ctrl T".into()),
            icon: Some(Icon::Plus.into()),
        };
        std::iter::once(new)
            .chain([MenuItem::Caption("Tiles, open in this workspace".into())])
            .chain(OPENABLE.iter().map(module_row))
            .chain([MenuItem::Caption("Screens, open as a tab".into())])
            .chain(
                openable
                    .iter()
                    .map(|name| MenuItem::action(*name).icon(Glyph::Window)),
            )
            .collect()
    }

    fn tab_menu(workspace: Option<&Workspace>) -> Vec<MenuItem> {
        let (pinned, locked) = workspace.map_or((false, false), |at| (at.pinned, at.locked));
        [
            Some(MenuItem::action("Rename").icon(Glyph::Pencil)),
            Some(MenuItem::Submenu {
                label: "Icon".into(),
                icon: Some(Glyph::Sparkle.into()),
                items: std::iter::once(MenuItem::action("None"))
                    .chain(
                        Glyph::WORKSPACE
                            .iter()
                            .map(|(name, icon)| MenuItem::action(*name).icon(*icon)),
                    )
                    .collect(),
            }),
            Some(MenuItem::action(if pinned { "Unpin" } else { "Pin" }).icon(Glyph::Pin)),
            Some(MenuItem::action(if locked { "Unlock" } else { "Lock" }).icon(Glyph::Lock)),
            Some(MenuItem::action("Reset layout").icon(Icon::Restore)),
            (!pinned).then(|| MenuItem::action("Close").icon(Icon::Close)),
            Some(MenuItem::Separator),
            Some(MenuItem::Submenu {
                label: "Spawn".into(),
                icon: Some(Icon::Plus.into()),
                items: OPENABLE.iter().map(module_row).collect(),
            }),
        ]
        .into_iter()
        .flatten()
        .collect()
    }

    fn picked_plus(&mut self, pick: usize, openable: &[&'static str]) -> Option<Strip> {
        let tiles = 2..2 + OPENABLE.len();
        match pick {
            0 => self.board.add(),
            at if tiles.contains(&at) => {
                self.board.spawn(OPENABLE.get(at - tiles.start)?.clone());
            }
            at => {
                let name = openable.get(at.checked_sub(tiles.end + 1)?)?;
                return Some(Strip::Open(name));
            }
        }
        None
    }

    fn picked_tab(&mut self, pick: usize, index: usize) {
        let pinned = self
            .board
            .workspaces()
            .get(index)
            .is_some_and(|at| at.pinned);
        let icons = 3..3 + Glyph::WORKSPACE.len();
        let rest = icons.end;
        match pick + usize::from(pinned && pick >= rest + 3) {
            0 => self.board.rename(index),
            2 => self.board.set_icon(index, None),
            at if icons.contains(&at) => {
                let named = Glyph::WORKSPACE.get(at - icons.start);
                self.board
                    .set_icon(index, named.map(|(name, _)| (*name).to_owned()));
            }
            at if at == rest => self.board.toggle_pin(index),
            at if at == rest + 1 => self.board.toggle_lock(index),
            at if at == rest + 2 => {
                eprintln!("desk: work: reset layout of workspace {index}");
                self.board.switch(index);
                self.board.apply(true, |workspace, _| Ok(workspace.reset()));
            }
            at if at == rest + 3 => self.board.close_workspace(index),
            at => {
                if let Some(module) = at.checked_sub(rest + 6).and_then(|row| OPENABLE.get(row)) {
                    self.board.switch(index);
                    self.board.spawn(module.clone());
                }
            }
        }
    }

    pub fn tabs(
        &mut self,
        screens: &[Tab],
        openable: &[&'static str],
        screen: Option<usize>,
        theme: &Theme,
        on: impl Fn(&Strip, &mut Window, &mut App) + 'static,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let workspaces = self.board.tabs();
        let count = workspaces.len();
        let on = Rc::new(on);
        let this = cx.weak_entity();
        let (pointer, plus, tab, picker) = (this.clone(), this.clone(), this.clone(), on.clone());
        let (tabbed, opener) = (this.clone(), this);
        let opened: Vec<&'static str> = openable.to_vec();
        let strip = header_tabs(
            "work-workspaces",
            &workspaces,
            screen.map_or(self.board.active(), |at| count + at),
            screens,
            theme,
            move |event, window, cx| {
                let shown = match *event {
                    TabEvent::Select(index) if index >= count => Strip::Show(index - count),
                    TabEvent::Close(index) if index >= count => Strip::Close(index - count),
                    TabEvent::Select(_) | TabEvent::Close(_) | TabEvent::New => Strip::Work,
                };
                tabbed
                    .update(cx, |work, cx| {
                        match *event {
                            TabEvent::Select(index) if index < count => work.board.switch(index),
                            TabEvent::Close(index) if index < count => {
                                work.board.close_workspace(index)
                            }
                            TabEvent::New => work.board.add(),
                            TabEvent::Select(_) | TabEvent::Close(_) => {}
                        }
                        work.focus.focus(window, cx);
                        cx.notify();
                    })
                    .unwrap_or_else(|_| eprintln!("desk: work: the screen is gone"));
                picker(&shown, window, cx);
            },
        )
        .on_point(move |at, _, cx| {
            pointer
                .update(cx, |work, cx| {
                    if work.pointed != at {
                        work.pointed = at;
                        cx.notify();
                    }
                })
                .unwrap_or_else(|_| eprintln!("desk: work: the screen is gone"));
        })
        .on_menu(move |at, position, window, cx| {
            if at < count {
                opener
                    .update(cx, |work, cx| {
                        work.pointed = at;
                        work.menu_at = Some(position);
                        cx.notify();
                    })
                    .unwrap_or_else(|_| eprintln!("desk: work: the screen is gone"));
                window.refresh();
            }
        });
        let tab_menu = context_menu(Self::tab_menu(self.board.workspaces().get(self.pointed)))
            .id("work-tab-menu")
            .open_at(self.menu_at.take())
            .on_pick(move |pick, window, cx| {
                tab.update(cx, |work, cx| {
                    work.picked_tab(*pick, work.pointed);
                    work.focus.focus(window, cx);
                    cx.notify();
                })
                .unwrap_or_else(|_| eprintln!("desk: work: the screen is gone"));
            })
            .child(strip.new_button(self.plus.clone()));
        self.plus.update(cx, |button, cx| {
            button.items(Self::plus_menu(openable), cx);
            button.on_pick(move |pick, window, cx| {
                let strip = plus
                    .update(cx, |work, cx| {
                        let strip = work.picked_plus(*pick, &opened);
                        cx.notify();
                        strip
                    })
                    .unwrap_or_else(|_| {
                        eprintln!("desk: work: the screen is gone");
                        None
                    });
                on(&strip.unwrap_or(Strip::Work), window, cx);
            });
        });
        div().flex_1().min_w_0().child(tab_menu).into_any_element()
    }
}

pub enum Strip {
    Work,
    Show(usize),
    Close(usize),
    Open(&'static str),
}

impl Focusable for Work {
    fn focus_handle(&self, _: &App) -> FocusHandle {
        self.focus.clone()
    }
}

impl Render for Work {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        self.frame(window, cx);
        let theme = ActiveTheme::theme(cx);
        let board = self.board.board(&theme, window, cx);
        let edge = px(-WORKSPACE_EDGE);
        let band = div()
            .absolute()
            .top(edge)
            .left(edge)
            .right(edge)
            .bottom(edge)
            .flex();
        div()
            .size_full()
            .relative()
            .track_focus(&self.focus)
            .on_key_down(cx.listener(Self::key))
            .child(self.board.watch(band, cx).child(board))
            .children(self.replay.as_ref().map(Replay::meter))
    }
}
