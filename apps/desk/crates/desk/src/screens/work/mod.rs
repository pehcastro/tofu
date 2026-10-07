use std::env;
use std::rc::Rc;

use desk_core::model::Store;
use desk_tiling::{
    self as tiling, DRAG_THRESHOLD, Divider, HEADER_ZONE, Module, NUDGE, Preset, Rect, Refusal,
    Side, Stack, Store as Layouts, Target, TileId, Workspace,
};
use desk_ui::components::card::{Header, inner_card, shell};
use desk_ui::components::empty::empty_state;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::tabs::{Tab, TabEvent, TabMark, connected_tabs};
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::TITLE_BAR_HEIGHT;
use desk_ui::theme::Theme;
use gpui::{
    AnyElement, AnyView, App, AppContext, Context, CursorStyle, Div, Entity, FocusHandle,
    IntoElement, KeyDownEvent, MouseButton, MouseDownEvent, MouseMoveEvent, MouseUpEvent, Pixels,
    Point, Render, SharedString, Window, WindowControlArea, div, prelude::*, px,
};

use crate::modules::chat::cassette::{Replay, Step};
use crate::modules::chat::{self, Chat};
use crate::modules::file_edits::{self, FileEdits};
use crate::modules::replayed;
use crate::modules::shells::{self, Kill, Shells};
use crate::modules::subagents::{self, Subagents};

const BOARD: &str = "36-agents";
const INSET: f32 = 8.0;
const WORK_NAME: &str = "work";

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
        Some(_) => BOARD.to_owned(),
        None => env::current_dir()
            .ok()
            .and_then(|dir| Some(dir.file_name()?.to_string_lossy().into_owned()))
            .ok_or("the work screen cannot name the project it runs in")?,
    };
    let layouts = Layouts::for_project(&project).map_err(|error| error.to_string())?;
    let mut saved = layouts
        .load()
        .unwrap_or_else(|error| {
            eprintln!("the saved layout is unreadable, so the work preset is used: {error}");
            None
        })
        .unwrap_or_default()
        .into_iter();
    let workspace = saved
        .next()
        .unwrap_or_else(|| Workspace::new(WORK_NAME, Preset::Work));
    let others = saved.collect();
    let store = cx.new(|_| Store::default());
    let chat = match replay {
        Some(_) => chat::fed(store.clone(), window, cx),
        None => chat::live(store.clone(), window, cx),
    };
    let killer = chat.downgrade();
    let kill: Kill = Rc::new(move |shell, cx| {
        killer
            .update(cx, |chat, cx| chat.kill(shell, cx))
            .unwrap_or_else(|_| eprintln!("desk: shells: the chat is gone, so nothing was killed"));
    });
    let subagents = subagents::mount(store.clone(), cx);
    let file_edits = file_edits::mount(store.clone(), cx);
    let shells = shells::mount(store.clone(), kill, cx);
    Ok(cx
        .new(|cx| {
            let focus = cx.focus_handle();
            focus.focus(window, cx);
            Work {
                workspace,
                others,
                layouts,
                gesture: Gesture::Idle,
                nudge: None,
                focus,
                store,
                chat,
                subagents,
                file_edits,
                shells,
                replay,
            }
        })
        .into())
}

#[derive(Clone, Copy)]
struct Grab {
    tile: TileId,
    module: usize,
}

#[derive(Clone)]
enum Gesture {
    Idle,
    Pressed { grab: Grab, from: (f32, f32) },
    Dragging { grab: Grab, target: Option<Target> },
    Resizing { divider: Divider, from: Workspace },
}

pub struct Work {
    workspace: Workspace,
    others: Vec<Workspace>,
    layouts: Layouts,
    gesture: Gesture,
    nudge: Option<f32>,
    focus: FocusHandle,
    store: Entity<Store>,
    chat: Entity<Chat>,
    subagents: Entity<Subagents>,
    file_edits: Entity<FileEdits>,
    shells: Entity<Shells>,
    replay: Option<Replay>,
}

fn window_area(window: &Window) -> Rect {
    let size = window.viewport_size();
    Rect {
        x: INSET,
        y: TITLE_BAR_HEIGHT,
        w: f32::from(size.width) - 2.0 * INSET,
        h: f32::from(size.height) - TITLE_BAR_HEIGHT - INSET,
    }
}

fn local(position: Point<Pixels>) -> (f32, f32) {
    (f32::from(position.x), f32::from(position.y))
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

fn place(element: Div, rect: Rect) -> Div {
    element
        .absolute()
        .left(px(rect.x))
        .top(px(rect.y))
        .w(px(rect.w))
        .h(px(rect.h))
}

impl Work {
    fn commit(&mut self, next: Result<Workspace, Refusal>, cx: &mut Context<Self>) {
        match next {
            Ok(workspace) => {
                self.workspace = workspace;
                let all: Vec<Workspace> = std::iter::once(self.workspace.clone())
                    .chain(self.others.iter().cloned())
                    .collect();
                if let Err(error) = self.layouts.save(&all) {
                    eprintln!("the layout was not saved: {error}");
                }
            }
            Err(refusal) => eprintln!("the layout refused that: {refusal}"),
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
                self.chat.update(cx, |chat, cx| chat.take(&[event], cx));
                Ok(())
            }
            Ok(Step::Restart) => {
                self.chat.update(cx, |chat, cx| chat.restart(cx));
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
        self.chat.update(cx, |chat, cx| {
            chat.restart(cx);
            chat.take(&events, cx);
        });
        let agents = self.subagents.update(cx, |module, cx| module.counted(cx));
        let files = self.file_edits.update(cx, |module, cx| module.counted(cx));
        let shells = self.shells.update(cx, |module, cx| module.counted(cx));
        eprintln!(
            "desk: work tiles from store {:?}: sub-agents {agents}; file edits {files}; shells {shells}",
            self.store.entity_id()
        );
        Ok(())
    }

    fn target(&self, area: Rect, grab: Grab, x: f32, y: f32) -> Option<Target> {
        let target = tiling::target(self.workspace.tree(), area, x, y)?;
        let next = self
            .workspace
            .move_to(grab.tile, grab.module, target, area)
            .ok()?;
        (next.tree() != self.workspace.tree()).then_some(target)
    }

    fn pointer_moved(
        &mut self,
        event: &MouseMoveEvent,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let (x, y) = local(event.position);
        let area = window_area(window);
        self.gesture = match std::mem::replace(&mut self.gesture, Gesture::Idle) {
            Gesture::Idle => return,
            Gesture::Pressed { grab, from } if (x - from.0).hypot(y - from.1) < DRAG_THRESHOLD => {
                self.gesture = Gesture::Pressed { grab, from };
                return;
            }
            Gesture::Pressed { grab, .. } | Gesture::Dragging { grab, .. } => Gesture::Dragging {
                grab,
                target: self.target(area, grab, x, y),
            },
            Gesture::Resizing { divider, from } => {
                if let Ok(next) = from.resize(&divider, x, y) {
                    self.workspace = next;
                }
                Gesture::Resizing { divider, from }
            }
        };
        cx.notify();
    }

    fn released(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let area = window_area(window);
        let next = match std::mem::replace(&mut self.gesture, Gesture::Idle) {
            Gesture::Idle => return,
            Gesture::Pressed { grab, .. } => self.workspace.focus_tile(grab.tile),
            Gesture::Dragging { grab, target } => target
                .ok_or(Refusal::NoSuchTile)
                .and_then(|target| self.workspace.move_to(grab.tile, grab.module, target, area)),
            Gesture::Resizing { .. } => Ok(self.workspace.clone()),
        };
        self.commit(next, cx);
    }

    fn key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let stroke = &event.keystroke;
        let held = stroke.modifiers;
        let chord = held.control && held.alt;
        let area = window_area(window);
        let key = stroke.key.as_str();
        let next = match key {
            "escape"
                if matches!(
                    self.gesture,
                    Gesture::Pressed { .. } | Gesture::Dragging { .. }
                ) =>
            {
                self.gesture = Gesture::Idle;
                cx.notify();
                return;
            }
            "escape" if self.nudge.take().is_some() => return,
            "escape" if self.workspace.zoomed().is_some() => self.workspace.zoom(),
            "z" if held.control && !held.alt => self.workspace.undo(),
            "+" | "=" if chord => {
                self.nudge = Some(NUDGE);
                return;
            }
            "-" if chord => {
                self.nudge = Some(-NUDGE);
                return;
            }
            "enter" if chord => self.workspace.zoom(),
            "w" if chord => self
                .workspace
                .focus()
                .ok_or(Refusal::NoSuchTile)
                .and_then(|focus| self.workspace.close_tile(focus, area)),
            _ => {
                let Some(side) = side_of(key).filter(|_| chord) else {
                    return;
                };
                match self.nudge.take() {
                    Some(step) => self.workspace.nudge(side, step, area),
                    None if held.shift => self.workspace.move_focused(side, area),
                    None => self.workspace.focus_toward(side, area),
                }
            }
        };
        self.commit(next, cx);
    }

    fn count(&self, module: &Module, cx: &App) -> Option<u32> {
        let count = match module {
            Module::SubAgents => self.subagents.read(cx).live(),
            Module::FileEdits => self.file_edits.read(cx).count(),
            Module::Shells => self.shells.read(cx).count(),
            Module::Chat
            | Module::Editor
            | Module::Terminal
            | Module::Browser
            | Module::SourceControl
            | Module::Plugin(_) => return None,
        };
        u32::try_from(count).ok()
    }

    fn session(&self, cx: &App) -> Option<SharedString> {
        let (id, session) = self.store.read(cx).sessions.iter().next()?;
        let name = if session.name.is_empty() {
            id
        } else {
            &session.name
        };
        Some(name.clone().into())
    }

    fn header(&self, stack: &Stack, area: Rect, theme: &Theme, cx: &mut Context<Self>) -> Header {
        let tile = stack.id;
        if let [module] = stack.modules.as_slice() {
            let session = (*module == Module::Chat)
                .then(|| self.session(cx))
                .flatten()
                .map(|name| div().child(name).into_any_element());
            return Header::Title(glyph(module), module.name().to_owned().into(), session);
        }
        let tabs: Vec<Tab> = stack
            .modules
            .iter()
            .map(|module| Tab {
                label: module.name().to_owned().into(),
                icon: glyph(module),
                count: self.count(module, cx),
                mark: TabMark::Close,
            })
            .collect();
        let this = cx.weak_entity();
        let strip = connected_tabs(
            SharedString::from(format!("work-tabs-{}", tile.0)),
            &tabs,
            stack.active,
            usize::MAX,
            theme,
            move |event, _, cx| {
                this.update(cx, |work, cx| {
                    let next = match *event {
                        TabEvent::Select(at) => work.workspace.activate(tile, at),
                        TabEvent::Close(at) => work.workspace.close_module(tile, at, area),
                        TabEvent::More | TabEvent::New => {
                            return eprintln!("desk: work: {event:?} on a tile is not wired");
                        }
                    };
                    work.commit(next, cx);
                })
                .ok();
            },
        );
        Header::Tabs(strip.into_any_element(), None)
    }

    fn body(&self, module: &Module, theme: &Theme) -> AnyElement {
        match module {
            Module::Chat => self.chat.clone().into_any_element(),
            Module::SubAgents => self.subagents.clone().into_any_element(),
            Module::FileEdits => self.file_edits.clone().into_any_element(),
            Module::Shells => self.shells.clone().into_any_element(),
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

    fn tile(
        &self,
        stack: &Stack,
        rect: Rect,
        area: Rect,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Div {
        let grab = Grab {
            tile: stack.id,
            module: stack.active,
        };
        let body = stack
            .modules
            .get(stack.active)
            .map(|module| self.body(module, theme));
        place(div(), rect)
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(move |work, event: &MouseDownEvent, _, cx| {
                    let from = local(event.position);
                    if from.1 - rect.y <= HEADER_ZONE {
                        work.gesture = Gesture::Pressed { grab, from };
                        cx.notify();
                    }
                }),
            )
            .child(
                shell(self.header(stack, area, theme, cx), theme)
                    .size_full()
                    .child(inner_card(theme).children(body)),
            )
    }

    fn divider(&self, divider: Divider, cx: &mut Context<Self>) -> Div {
        let cursor = match divider.axis {
            tiling::Axis::Row => CursorStyle::ResizeLeftRight,
            tiling::Axis::Column => CursorStyle::ResizeUpDown,
        };
        place(div(), divider.rect).cursor(cursor).on_mouse_down(
            MouseButton::Left,
            cx.listener(move |work, event: &MouseDownEvent, _, cx| {
                if event.click_count >= 2 {
                    work.gesture = Gesture::Idle;
                    let next = work.workspace.even_split(&divider);
                    work.commit(Ok(next), cx);
                    return;
                }
                work.gesture = Gesture::Resizing {
                    divider: divider.clone(),
                    from: work.workspace.clone(),
                };
                cx.notify();
            }),
        )
    }
}

impl Render for Work {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        self.frame(window, cx);
        let theme = ActiveTheme::theme(cx);
        let area = window_area(window);
        let stacks: Vec<(Stack, Rect)> = self
            .workspace
            .tiles(area)
            .into_iter()
            .map(|(stack, rect)| (stack.clone(), rect))
            .collect();
        let tiles: Vec<Div> = stacks
            .iter()
            .map(|(stack, rect)| self.tile(stack, *rect, area, &theme, cx))
            .collect();
        let dividers: Vec<Div> = self
            .workspace
            .dividers(area)
            .into_iter()
            .map(|divider| self.divider(divider, cx))
            .collect();
        div()
            .size_full()
            .relative()
            .track_focus(&self.focus)
            .on_key_down(cx.listener(Self::key))
            .on_mouse_move(cx.listener(Self::pointer_moved))
            .on_mouse_up(
                MouseButton::Left,
                cx.listener(|work, _: &MouseUpEvent, window, cx| work.released(window, cx)),
            )
            .child(
                div()
                    .id("work-drag")
                    .absolute()
                    .top_0()
                    .left_0()
                    .w_full()
                    .h(px(TITLE_BAR_HEIGHT))
                    .window_control_area(WindowControlArea::Drag),
            )
            .children(tiles)
            .children(dividers)
            .children(self.replay.as_ref().map(Replay::meter))
    }
}
