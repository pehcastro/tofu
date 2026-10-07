use std::path::PathBuf;
use std::rc::Rc;

use desk_core::model::Store;
use desk_tiling::{
    Action, Module, NUDGE, Preset, Side, Store as Layouts, Target, TileId, WORKSPACE_EDGE,
    Workspace, Zone,
};
use desk_ui::components::empty::{EmptyAction, empty_state};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::tabs::{Tab, TabEvent, TabMark, TabStrip, header_tabs};
use desk_ui::components::tiling_board::{Host, Settled, TilingBoard};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::Theme;
use gpui::{
    AnyElement, AnyView, App, AppContext, Context, Entity, FocusHandle, Focusable, IntoElement,
    KeyDownEvent, Render, SharedString, Subscription, WeakEntity, Window, div, prelude::*, px,
};

use crate::modules::chat::cassette::{Replay, Step};
use crate::modules::chat::{self, Chat};
use crate::modules::file_edits::{self, FileEdits};
use crate::modules::replayed;
use crate::modules::shells::{self, Kill, Shells};
use crate::modules::subagents::{self, Subagents};
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
        let observed = cx.observe(&mounted.store, |_, _, cx| cx.notify());
        let bodies = mounted.clone();
        let titles = mounted.clone();
        let sessions = mounted.store.clone();
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
                origin: (0.0, 0.0),
                below: 0.0,
            },
        );
        Work {
            board,
            nudge: None,
            focus,
            mounted,
            replay,
            _observed: observed,
        }
    }))
}

pub struct Work {
    board: TilingBoard<Work>,
    nudge: Option<f32>,
    focus: FocusHandle,
    mounted: Mounted,
    replay: Option<Replay>,
    _observed: Subscription,
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
                "desk: work: workspace {}/{}{} {:?} preset {} locked {}: {}",
                at + 1,
                all.len(),
                if at == active { " active" } else { "" },
                workspace.name,
                workspace.preset.name(),
                workspace.locked,
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

    pub fn tabs(&self, this: WeakEntity<Self>, theme: &Theme) -> TabStrip {
        let tabs: Vec<Tab> = self
            .board
            .workspaces()
            .iter()
            .map(|workspace| Tab {
                label: workspace.name.clone().into(),
                icon: None,
                count: None,
                mark: if workspace.locked {
                    TabMark::Locked
                } else {
                    TabMark::Close
                },
            })
            .collect();
        header_tabs(
            "work-workspaces",
            &tabs,
            self.board.active(),
            &[],
            theme,
            move |event, window, cx| {
                this.update(cx, |work, cx| {
                    match *event {
                        TabEvent::Select(index) => work.board.switch(index),
                        TabEvent::Close(index) => work.board.close_workspace(index),
                        TabEvent::New => work.board.add(),
                    }
                    work.focus.focus(window, cx);
                    cx.notify();
                })
                .unwrap_or_else(|_| eprintln!("desk: work: the screen is gone"));
            },
        )
    }
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
