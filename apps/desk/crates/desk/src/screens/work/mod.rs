pub mod split;

use std::cell::RefCell;
use std::ops::Range;
use std::path::PathBuf;
use std::rc::Rc;
use std::time::{Duration, Instant};

use desk_core::model::Store;
use desk_tiling::{
    Action, Module, NUDGE, Preset, Rect, Refusal, Side, Store as Layouts, Target, TileId,
    WORKSPACE_EDGE, Workspace, Zone,
};
use desk_ui::components::card::{Header, inner_card, shell};
use desk_ui::components::empty::{EmptyAction, empty_state};
use desk_ui::components::find::{FindBar, FindGroup, find_ranges};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::overlay::{MenuButton, MenuItem, context_menu};
use desk_ui::components::tabs::{Tab, TabEvent, TabFlag, header_tabs, new_tab_glyph};
use desk_ui::components::tiling_board::{Host, Reopened, Settled, TilingBoard, tile_menu};
use desk_ui::icon::Icon;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::Theme;
use gpui::{
    AnyElement, AnyView, App, AppContext, Context, Entity, EventEmitter, FocusHandle, Focusable,
    IntoElement, KeyDownEvent, Pixels, Point, Render, SharedString, Subscription, Window, div,
    prelude::*, px, relative,
};

use crate::desk::{SCREEN_MARKS, ScreenGroup, ScreenMark, screen_mark};
use crate::modules::chat::cassette::{Replay, Step};
use crate::modules::chat::{self, Chat, ChatMirror, Find};
use crate::modules::file_edits::{self, FileEdits};
use crate::modules::replayed;
use crate::modules::shells::{self, Shells};
use crate::modules::subagents::{self, ExpandAgent, Subagents};
use crate::modules::terminal::{self, TabChanged, TerminalModule};
use crate::plugins::{self, Plugin};
use crate::project;

const BOARD: &str = "36-agents";
const WORK_NAME: &str = "work";
const PLUGINS_POLL: Duration = Duration::from_millis(50);
const MIRROR_NAME: &str = "Chat (mirror)";
const OPENABLE: [Module; 6] = [
    Module::Chat,
    Module::SubAgents,
    Module::FileEdits,
    Module::Shells,
    Module::Terminal,
    Module::SourceControl,
];

fn tile_modules() -> Vec<Module> {
    OPENABLE.into_iter().chain(plugins::modules()).collect()
}

fn name(module: &Module) -> SharedString {
    match module {
        Module::Plugin(id) => plugins::find(id).map_or(id.as_str(), |plugin| plugin.name.as_str()),
        other => other.name(),
    }
    .to_owned()
    .into()
}

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
    terminals: Terminals,
    mirrors: Mirrors,
    plugins: Mounts,
    project: PathBuf,
}

#[derive(Default)]
struct MirrorTiles {
    open: Vec<((usize, TileId), Entity<ChatMirror>)>,
    workspace: usize,
}

#[derive(Clone, Default)]
struct Mirrors(Rc<RefCell<MirrorTiles>>);

impl Mirrors {
    fn at(&self, tile: TileId) -> Option<Entity<ChatMirror>> {
        let tiles = self.0.borrow();
        tiles
            .open
            .iter()
            .find(|(at, _)| *at == (tiles.workspace, tile))
            .map(|(_, mirror)| mirror.clone())
    }
}

struct PluginView {
    plugin: &'static str,
    at: (usize, TileId),
    view: AnyView,
}

#[derive(Default)]
struct PluginViews {
    open: Vec<PluginView>,
    workspace: usize,
}

#[derive(Clone, Default)]
struct Mounts(Rc<RefCell<PluginViews>>);

impl Mounts {
    fn at(&self, at: (usize, TileId)) -> Option<AnyView> {
        self.0
            .borrow()
            .open
            .iter()
            .find(|open| open.at == at)
            .map(|open| open.view.clone())
    }

    fn view(&self, plugin: &'static Plugin, at: (usize, TileId), cx: &mut App) -> AnyView {
        let mounted = self
            .0
            .borrow()
            .open
            .iter()
            .find(|open| open.plugin == plugin.id && open.at == at)
            .map(|open| open.view.clone());
        mounted.unwrap_or_else(|| {
            let view = plugin.mount(cx);
            self.0.borrow_mut().open.push(PluginView {
                plugin: &plugin.id,
                at,
                view: view.clone(),
            });
            view
        })
    }

    fn workspace(&self) -> usize {
        self.0.borrow().workspace
    }
}

struct OpenTerminal {
    at: (usize, TileId),
    module: Entity<TerminalModule>,
    _tab: Subscription,
}

#[derive(Default)]
struct TerminalTabs {
    open: Vec<OpenTerminal>,
    workspace: usize,
    opened: usize,
}

#[derive(Clone, Default)]
struct Terminals(Rc<RefCell<TerminalTabs>>);

impl Terminals {
    fn in_view(&self) -> Vec<Entity<TerminalModule>> {
        let tabs = self.0.borrow();
        tabs.open
            .iter()
            .filter(|open| open.at.0 == tabs.workspace)
            .map(|open| open.module.clone())
            .collect()
    }

    fn at(&self, tile: TileId) -> Option<Entity<TerminalModule>> {
        let tabs = self.0.borrow();
        tabs.open
            .iter()
            .find(|open| open.at == (tabs.workspace, tile))
            .map(|open| open.module.clone())
    }
}

fn build(
    replay: Option<Replay>,
    project: PathBuf,
    window: &mut Window,
    cx: &mut App,
) -> Result<Entity<Work>, String> {
    plugins::check()?;
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
        None => chat::live(project.clone(), store.clone(), window, cx),
    };
    let mounted = Mounted {
        subagents: subagents::mount(store.clone(), cx),
        file_edits: file_edits::mount(store.clone(), cx),
        shells: shells::mount(store.clone(), chat.downgrade(), cx),
        terminals: Terminals::default(),
        mirrors: Mirrors::default(),
        plugins: Mounts::default(),
        project,
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
                |work: &mut Work, module, ExpandAgent(agent): &ExpandAgent, cx| {
                    let view = module.update(cx, |module, cx| module.screen(Some(*agent), cx));
                    cx.emit(Expanded {
                        name: SUB_AGENTS,
                        workspace: work.board.active_name().into(),
                        view: view.into(),
                    });
                },
            ),
        ];
        cx.spawn(async move |work, cx| {
            while !plugins::settled() {
                cx.background_executor().timer(PLUGINS_POLL).await;
            }
            if work.update(cx, |_, cx| cx.notify()).is_err() {
                eprintln!("desk: work: the screen closed before the plugins loaded");
            }
        })
        .detach();
        let bodies = mounted.clone();
        let titles = mounted.clone();
        let badges = mounted.clone();
        let head = mounted.chat.read(cx).head();
        let expandable = mounted.clone();
        let board = TilingBoard::new(
            workspaces,
            Host {
                board: |work: &mut Work| &mut work.board,
                body: Box::new(
                    move |stack, _, theme, cx| match stack.modules.get(stack.active) {
                        Some(module) => body(&bodies, module, stack.id, theme, cx),
                        None => empty_tile(stack.id, theme, cx),
                    },
                ),
                title: Box::new(move |tile, module, mark, cx| {
                    let terminal = (*module == Module::Terminal)
                        .then(|| titles.terminals.at(tile))
                        .flatten();
                    let terminal = terminal.as_ref().map(|terminal| terminal.read(cx));
                    let mirrored = *module == Module::Chat && titles.mirrors.at(tile).is_some();
                    Tab {
                        label: terminal
                            .and_then(|terminal| terminal.label(cx))
                            .or_else(|| mirrored.then(|| MIRROR_NAME.into()))
                            .unwrap_or_else(|| name(module)),
                        icon: glyph(module),
                        count: count(&titles, module, cx),
                        mark,
                        flag: terminal
                            .filter(|terminal| terminal.ringing())
                            .map(|_| TabFlag {
                                icon: Icon::Bell,
                                tip: "the shell rang its bell".into(),
                            }),
                    }
                }),
                subtitle: Box::new(move |tile, module, cx| match module {
                    Module::Chat => Some(head.clone().into_any_element()),
                    Module::Terminal => badges
                        .terminals
                        .at(tile)
                        .map(|terminal| terminal.read(cx).badge(cx)),
                    _ => None,
                }),
                spawnable: tile_modules(),
                spawns: tile_modules(),
                settled: settled(layouts),
                expand: Box::new(move |module, tile, workspace, _, cx| {
                    expand(&expandable, module, tile, workspace, cx)
                }),
                origin: (0.0, 0.0),
                below: 0.0,
            },
        );
        let find = FindBar::new(window, cx);
        let (searcher, jumper) = (cx.weak_entity(), cx.weak_entity());
        find.update(cx, |bar, _| {
            bar.on_change(move |query, _, _, cx| {
                searcher
                    .update(cx, |work, cx| work.search(query, cx))
                    .unwrap_or_else(|_| eprintln!("desk: work: the screen is gone"));
            });
            bar.on_pick(move |at, window, cx| {
                jumper
                    .update(cx, |work, cx| work.jump(at, window, cx))
                    .unwrap_or_else(|_| eprintln!("desk: work: the screen is gone"));
            });
        });
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
            find,
            found: Vec::new(),
            query: String::new(),
            seen: None,
            picked_at: None,
            _watched: watched,
        }
    }))
}

pub struct Work {
    find: Entity<FindBar>,
    found: Vec<Anchor>,
    query: String,
    board: TilingBoard<Work>,
    nudge: Option<f32>,
    plus: Entity<MenuButton>,
    pointed: usize,
    menu_at: Option<Point<Pixels>>,
    focus: FocusHandle,
    mounted: Mounted,
    replay: Option<Replay>,
    seen: Option<Vec<(usize, TileId, Module)>>,
    picked_at: Option<Instant>,
    _watched: [Subscription; 2],
}

#[cfg(feature = "plugin-notepad")]
fn plugin_focus(view: AnyView, cx: &App) -> Option<FocusHandle> {
    let notepad = view.downcast::<crate::modules::notepad::Notepad>().ok()?;
    Some(notepad.read(cx).focus_handle(cx))
}

#[cfg(not(feature = "plugin-notepad"))]
fn plugin_focus(_: AnyView, _: &App) -> Option<FocusHandle> {
    None
}

fn glyph(module: &Module) -> Option<Glyph> {
    match module {
        Module::Chat => Some(Glyph::Chat),
        Module::SubAgents => Some(Glyph::Agents),
        Module::FileEdits => Some(Glyph::File),
        Module::Shells | Module::Terminal => Some(Glyph::Terminal),
        Module::Plugin(id) => plugins::find(id).map(|plugin| plugin.glyph),
        Module::SourceControl => Some(Glyph::Branch),
        Module::Editor | Module::Browser => None,
    }
}

#[derive(Clone)]
enum Plus {
    Workspace,
    Tile(Module),
    Screen(&'static str),
}

const TOP_SCREENS: [&str; 3] = ["editor", "git", "boardy"];

fn screen_row(mark: &ScreenMark) -> MenuItem {
    MenuItem::action(mark.label).icon(mark.glyph)
}

fn module_row(module: &Module, chatting: bool) -> MenuItem {
    let state = match module {
        Module::Plugin(id) => plugins::find(id).and_then(Plugin::state),
        _ => None,
    };
    let mirrors = chatting && *module == Module::Chat;
    MenuItem::Action {
        label: if mirrors {
            MIRROR_NAME.into()
        } else {
            name(module)
        },
        keys: state.map(SharedString::from),
        icon: glyph(module).map(Into::into),
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

const EXCERPT_LEAD: usize = 24;
const EVERYWHERE: Rect = Rect {
    x: 0.0,
    y: 0.0,
    w: 0.0,
    h: 0.0,
};

fn excerpt(text: &str, range: Range<usize>) -> SharedString {
    let start = text
        .get(..range.start)
        .and_then(|head| head.rfind('\n'))
        .map_or(0, |at| at + 1);
    let end = text
        .get(range.end..)
        .and_then(|tail| tail.find('\n'))
        .map_or(text.len(), |at| range.end + at);
    let lead = text.get(start..range.start).unwrap_or_default();
    let lead = match lead.char_indices().rev().nth(EXCERPT_LEAD) {
        Some((at, _)) => format!("…{}", lead.get(at..).unwrap_or_default()),
        None => lead.trim_start().to_owned(),
    };
    format!("{lead}{}", text.get(range.start..end).unwrap_or_default()).into()
}

#[derive(Clone)]
enum Anchor {
    Chat(usize),
    Row(Module, String),
}

impl Anchor {
    fn module(&self) -> Module {
        match self {
            Anchor::Chat(_) => Module::Chat,
            Anchor::Row(module, _) => module.clone(),
        }
    }
}

fn find_groups(
    store: &Store,
    open: &[Module],
    query: &str,
    chat_hits: &[(String, Range<usize>)],
) -> (Vec<FindGroup>, Vec<Anchor>) {
    let sessions = &store.sessions;
    let Some(session) = store
        .open
        .as_ref()
        .and_then(|id| sessions.get(id))
        .or_else(|| sessions.values().next())
    else {
        return (Vec::new(), Vec::new());
    };
    let mut groups = Vec::new();
    let mut found = Vec::new();
    for module in OPENABLE.iter().filter(|module| open.contains(module)) {
        let rows: Vec<(&str, String)> = match module {
            Module::Chat => {
                found.extend((0..chat_hits.len()).map(Anchor::Chat));
                let hits: Vec<SharedString> = chat_hits
                    .iter()
                    .map(|(text, range)| excerpt(text, range.clone()))
                    .collect();
                if !hits.is_empty() {
                    groups.push(FindGroup {
                        label: module.name().to_owned().into(),
                        hits,
                    });
                }
                continue;
            }
            Module::SubAgents => session
                .agents
                .iter()
                .map(|(id, a)| (id.as_str(), format!("{} {} {}", a.kind, a.number, a.task)))
                .collect(),
            Module::FileEdits => session
                .files
                .keys()
                .map(|path| (path.as_str(), path.clone()))
                .collect(),
            Module::Shells => session
                .shells
                .iter()
                .map(|(name, s)| (name.as_str(), s.command.clone()))
                .collect(),
            Module::Editor
            | Module::Terminal
            | Module::Browser
            | Module::SourceControl
            | Module::Plugin(_) => Vec::new(),
        };
        let (hits, anchors): (Vec<SharedString>, Vec<Anchor>) = rows
            .iter()
            .flat_map(|(key, text)| {
                find_ranges(text, query).into_iter().map(|range| {
                    (
                        excerpt(text, range),
                        Anchor::Row(module.clone(), (*key).to_owned()),
                    )
                })
            })
            .unzip();
        if hits.is_empty() {
            continue;
        }
        found.extend(anchors);
        groups.push(FindGroup {
            label: module.name().to_owned().into(),
            hits,
        });
    }
    (groups, found)
}

fn body(
    mounted: &Mounted,
    module: &Module,
    tile: TileId,
    theme: &Theme,
    cx: &mut App,
) -> AnyElement {
    match module {
        Module::Chat => mounted.mirrors.at(tile).map_or_else(
            || mounted.chat.clone().into_any_element(),
            IntoElement::into_any_element,
        ),
        Module::SubAgents => mounted.subagents.clone().into_any_element(),
        Module::FileEdits => mounted.file_edits.clone().into_any_element(),
        Module::Shells => mounted.shells.clone().into_any_element(),
        Module::Terminal => mounted
            .terminals
            .at(tile)
            .map_or_else(|| div().into_any_element(), IntoElement::into_any_element),
        Module::Plugin(id) => match plugins::find(id) {
            Some(plugin) => {
                let at = (mounted.plugins.workspace(), tile);
                mounted.plugins.view(plugin, at, cx).into_any_element()
            }
            None => empty_state(
                SharedString::from(format!("work-missing-plugin-{id}")),
                format!("plugin {id} is not installed"),
                Some("this build of the desk does not carry it".into()),
                &[],
                &[],
                theme,
                |_, _, _| {},
            )
            .into_any_element(),
        },
        Module::Editor | Module::Browser | Module::SourceControl => empty_state(
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
    pub workspace: SharedString,
    pub view: AnyView,
}

impl EventEmitter<Expanded> for Work {}

const SUB_AGENTS: &str = "Sub-agents";

pub struct Expandable;

pub static EXPANDABLE: Expandable = Expandable;

impl Expandable {
    pub fn contains(&self, name: &&str) -> bool {
        self.find(name).is_some()
    }

    fn find(&self, name: &str) -> Option<&'static str> {
        [
            "Chat",
            SUB_AGENTS,
            "File edits",
            "Shells",
            "Terminal",
            "Source control",
        ]
        .into_iter()
        .chain(plugins::all().map(|plugin| plugin.name.as_str()))
        .find(|tab| *tab == name)
    }
}

struct Tiled {
    glyph: Option<Glyph>,
    name: SharedString,
    view: AnyView,
    chat: Option<(Entity<Chat>, Subscription)>,
}

impl Render for Tiled {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let session = self.chat.as_ref().and_then(|(chat, _)| {
            let chat = chat.read(cx);
            let open = chat.open_id()?;
            let title = chat
                .rows()
                .iter()
                .find(|row| row.id == open)
                .map_or(open, |row| row.title());
            Some(
                div()
                    .flex_none()
                    .max_w(relative(0.5))
                    .truncate()
                    .child(SharedString::from(title.to_owned()))
                    .into_any_element(),
            )
        });
        shell(
            Header::Title(self.glyph, self.name.clone(), session),
            &theme,
        )
        .size_full()
        .min_w_0()
        .child(inner_card(&theme).child(self.view.clone()))
    }
}

fn expand(
    mounted: &Mounted,
    module: &Module,
    tile: TileId,
    workspace: &str,
    cx: &mut Context<Work>,
) {
    let tiled = |view: AnyView, chat: Option<Entity<Chat>>, cx: &mut Context<Work>| -> AnyView {
        cx.new(|cx| Tiled {
            glyph: glyph(module),
            name: name(module),
            view,
            chat: chat.map(|chat| {
                let watch = cx.observe(&chat, |_, _, cx| cx.notify());
                (chat, watch)
            }),
        })
        .into()
    };
    let view: AnyView = match module {
        Module::Chat => tiled(mounted.chat.clone().into(), Some(mounted.chat.clone()), cx),
        Module::SubAgents => mounted
            .subagents
            .update(cx, |module, cx| module.screen(None, cx))
            .into(),
        Module::FileEdits => tiled(mounted.file_edits.clone().into(), None, cx),
        Module::Shells => tiled(mounted.shells.clone().into(), None, cx),
        Module::Terminal => match mounted.terminals.in_view().first() {
            Some(terminal) => tiled(terminal.clone().into(), None, cx),
            None => return eprintln!("desk: work: no terminal is open, so none expands"),
        },
        Module::Plugin(id) => match plugins::find(id) {
            Some(plugin) => {
                let at = (mounted.plugins.workspace(), tile);
                tiled(mounted.plugins.view(plugin, at, cx), None, cx)
            }
            None => {
                return eprintln!(
                    "desk: work: plugin {id} is not installed, so it does not expand"
                );
            }
        },
        Module::Editor | Module::Browser | Module::SourceControl => {
            return eprintln!(
                "desk: work: {} is not mounted, so it does not expand",
                module.name()
            );
        }
    };
    let shown = name(module);
    match EXPANDABLE.find(&shown) {
        Some(name) => cx.emit(Expanded {
            name,
            workspace: workspace.to_owned().into(),
            view,
        }),
        None => eprintln!("desk: work: {shown} has no tab name"),
    }
}

fn empty_tile(tile: TileId, theme: &Theme, cx: &mut Context<Work>) -> AnyElement {
    let modules = tile_modules();
    let actions: Vec<EmptyAction> = modules
        .iter()
        .map(|module| EmptyAction {
            label: name(module),
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
            let Some(module) = modules.get(at).cloned() else {
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
    pub fn board(&self) -> &TilingBoard<Work> {
        &self.board
    }

    pub fn board_mut(&mut self) -> &mut TilingBoard<Work> {
        &mut self.board
    }

    pub fn chat(&self) -> &Entity<Chat> {
        &self.mounted.chat
    }

    fn placed(&self, module: &Module) -> Vec<(usize, TileId)> {
        self.board
            .workspaces()
            .iter()
            .enumerate()
            .flat_map(|(at, workspace)| {
                workspace
                    .tiles(EVERYWHERE)
                    .into_iter()
                    .filter(|(stack, _)| stack.modules.contains(module))
                    .map(move |(stack, _)| (at, stack.id))
            })
            .collect()
    }

    fn keep_mirrors(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let wanted: Vec<(usize, TileId)> = self.placed(&Module::Chat).into_iter().skip(1).collect();
        let mut tiles = self.mounted.mirrors.0.borrow_mut();
        tiles.workspace = self.board.active();
        let before = tiles.open.len();
        tiles.open.retain(|(at, _)| wanted.contains(at));
        if tiles.open.len() < before {
            eprintln!("desk: chat mirror closed, the session stays with the chat");
        }
        for at in wanted {
            if tiles.open.iter().any(|(open, _)| *open == at) {
                continue;
            }
            let mirror = chat::mirror(self.mounted.chat.clone(), window, cx);
            tiles.open.push((at, mirror));
        }
    }

    fn focus_new_tile(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let now: Vec<(usize, TileId, Module)> = self
            .board
            .workspaces()
            .iter()
            .enumerate()
            .flat_map(|(at, workspace)| {
                workspace
                    .tiles(EVERYWHERE)
                    .into_iter()
                    .flat_map(|(stack, _)| {
                        let id = stack.id;
                        stack
                            .modules
                            .iter()
                            .map(move |module| (at, id, module.clone()))
                    })
                    .collect::<Vec<_>>()
            })
            .collect();
        let Some(seen) = self.seen.replace(now) else {
            return;
        };
        let active = self.board.active();
        let Some((tile, module)) = self.board.workspaces().get(active).and_then(|workspace| {
            let stack = workspace.stack(workspace.focus()?)?;
            Some((stack.id, stack.modules.get(stack.active)?.clone()))
        }) else {
            return;
        };
        if seen.contains(&(active, tile, module.clone())) {
            return;
        }
        let started = self.picked_at.take().unwrap_or_else(Instant::now);
        let work = cx.weak_entity();
        window.defer(cx, move |window, cx| {
            let handle = work.update(cx, |work, cx| match &module {
                Module::Terminal => work
                    .mounted
                    .terminals
                    .at(tile)
                    .map(|terminal| terminal.read(cx).focus_handle(cx)),
                Module::Plugin(_) => work
                    .mounted
                    .plugins
                    .at((active, tile))
                    .and_then(|view| plugin_focus(view, cx)),
                _ => None,
            });
            match handle {
                Ok(Some(handle)) => {
                    handle.focus(window, cx);
                    eprintln!(
                        "desk: work: focused new {} tile in {} ms",
                        name(&module),
                        started.elapsed().as_millis()
                    );
                }
                Ok(None) => {}
                Err(_) => eprintln!("desk: work: the screen is gone"),
            }
        });
    }

    fn keep_plugins(&self) {
        let mut views = self.mounted.plugins.0.borrow_mut();
        views.workspace = self.board.active();
        views.open.retain(|open| {
            self.placed(&Module::Plugin(open.plugin.to_owned()))
                .contains(&open.at)
        });
    }

    fn keep_terminals(&mut self, cx: &mut Context<Self>) {
        let wanted = self.placed(&Module::Terminal);
        let mut tabs = self.mounted.terminals.0.borrow_mut();
        tabs.workspace = self.board.active();
        let before = tabs.open.len();
        tabs.open.retain(|open| wanted.contains(&open.at));
        if tabs.open.len() < before {
            eprintln!("desk: terminal: its tile closed, so its shell ends");
        }
        for at in wanted {
            if tabs.open.iter().any(|open| open.at == at) {
                continue;
            }
            tabs.opened = tabs.opened.saturating_add(1);
            let module = terminal::mount(&self.mounted.project, tabs.opened, cx);
            let tab = cx.subscribe(&module, |_, _, _: &TabChanged, cx| cx.notify());
            tabs.open.push(OpenTerminal {
                at,
                module,
                _tab: tab,
            });
        }
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

    fn open_find(&mut self, _: &Find, window: &mut Window, cx: &mut Context<Self>) {
        eprintln!("desk: work: find across tiles open");
        self.find.update(cx, |bar, cx| bar.open(window, cx));
        cx.stop_propagation();
    }

    fn search(&mut self, query: &str, cx: &mut Context<Self>) {
        let open: Vec<Module> = self
            .board
            .workspaces()
            .get(self.board.active())
            .map(|workspace| {
                workspace
                    .tiles(EVERYWHERE)
                    .into_iter()
                    .flat_map(|(stack, _)| stack.modules.clone())
                    .collect()
            })
            .unwrap_or_default();
        let chat_hits = self.mounted.chat.read(cx).search_hits(query);
        let (groups, found) = find_groups(self.mounted.store.read(cx), &open, query, &chat_hits);
        query.clone_into(&mut self.query);
        eprintln!(
            "desk: work: find {query:?}: {}",
            groups
                .iter()
                .map(|group| format!("{} {}", group.label, group.hits.len()))
                .collect::<Vec<_>>()
                .join(", ")
        );
        self.found = found;
        self.find.update(cx, |bar, cx| bar.set_groups(groups, cx));
    }

    fn jump(&mut self, at: usize, window: &mut Window, cx: &mut Context<Self>) {
        let Some(anchor) = self.found.get(at).cloned() else {
            return;
        };
        let module = anchor.module();
        self.board.apply(true, |workspace, area| {
            let (tile, index) = workspace
                .tiles(area)
                .into_iter()
                .find_map(|(stack, _)| {
                    let index = stack.modules.iter().position(|open| *open == module)?;
                    Some((stack.id, index))
                })
                .ok_or(Refusal::NoSuchTile)?;
            workspace.activate(tile, index)
        });
        eprintln!("desk: work: find jumped to {}", module.name());
        self.focus.focus(window, cx);
        let revealed = match &anchor {
            Anchor::Chat(hit) => {
                let query = &self.query;
                self.mounted
                    .chat
                    .update(cx, |chat, cx| chat.anchor(query, *hit, cx));
                true
            }
            Anchor::Row(Module::SubAgents, key) => self
                .mounted
                .subagents
                .update(cx, |module, cx| module.reveal(key, window, cx)),
            Anchor::Row(Module::FileEdits, key) => self
                .mounted
                .file_edits
                .update(cx, |module, cx| module.reveal(key, cx)),
            Anchor::Row(Module::Shells, key) => self
                .mounted
                .shells
                .update(cx, |module, cx| module.reveal(key, window, cx)),
            Anchor::Row(
                Module::Chat
                | Module::Editor
                | Module::Terminal
                | Module::Browser
                | Module::SourceControl
                | Module::Plugin(_),
                _,
            ) => true,
        };
        if let (false, Anchor::Row(_, key)) = (revealed, &anchor) {
            eprintln!("desk: find: {} row {key} is gone", module.name());
        }
        cx.notify();
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
                Some((tile, module)) => {
                    expand(&self.mounted, &module, tile, &self.board.active_name(), cx)
                }
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

    fn plus_menu(openable: &[&'static str], chatting: bool) -> (Vec<MenuItem>, Vec<Option<Plus>>) {
        let open = |mark: &&'static ScreenMark| openable.contains(&mark.name);
        let grouped = |group: ScreenGroup| -> Vec<&'static ScreenMark> {
            SCREEN_MARKS
                .iter()
                .filter(|mark| mark.group == group && !TOP_SCREENS.contains(&mark.name))
                .filter(open)
                .collect()
        };
        let screens = |marks: &[&'static ScreenMark]| -> (Vec<MenuItem>, Vec<Option<Plus>>) {
            marks
                .iter()
                .map(|mark| (screen_row(mark), Some(Plus::Screen(mark.name))))
                .unzip()
        };
        let insights = grouped(ScreenGroup::Insights);
        let (mut more, mut more_picks) = screens(&grouped(ScreenGroup::Top));
        if !insights.is_empty() {
            let (rows, picks) = screens(&insights);
            more.push(MenuItem::Submenu {
                label: "Insights".into(),
                icon: Some(Glyph::Sparkle.into()),
                items: rows,
            });
            more_picks.push(None);
            more_picks.extend(picks);
        }
        let top: Vec<&'static ScreenMark> = TOP_SCREENS
            .iter()
            .filter_map(|name| screen_mark(name))
            .filter(open)
            .collect();
        let (top, top_picks) = screens(&top);
        let (tiles, tile_picks) = tile_menu(&tile_modules(), |module| module_row(module, chatting));
        let mut rows = vec![
            MenuItem::Action {
                label: "New workspace".into(),
                keys: Some("Ctrl T".into()),
                icon: Some(Icon::Plus.into()),
            },
            MenuItem::Separator,
        ];
        rows.extend(top);
        rows.push(MenuItem::Separator);
        rows.push(MenuItem::Submenu {
            label: "Tile".into(),
            icon: Some(Icon::Plus.into()),
            items: tiles,
        });
        rows.push(MenuItem::Submenu {
            label: "More".into(),
            icon: None,
            items: more,
        });
        let picks = [Some(Plus::Workspace), None]
            .into_iter()
            .chain(top_picks)
            .chain([None, None])
            .chain(tile_picks.into_iter().map(|pick| pick.map(Plus::Tile)))
            .chain([None])
            .chain(more_picks)
            .collect();
        (rows, picks)
    }

    fn tab_menu(workspace: Option<&Workspace>, chatting: bool) -> Vec<MenuItem> {
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
                items: tile_modules()
                    .iter()
                    .map(|module| module_row(module, chatting))
                    .collect(),
            }),
        ]
        .into_iter()
        .flatten()
        .collect()
    }

    fn picked_plus(&mut self, pick: Plus) -> Option<Strip> {
        match pick {
            Plus::Workspace => self.board.add(),
            Plus::Tile(Module::SourceControl) => return Some(Strip::Open("git")),
            Plus::Tile(module) => {
                self.picked_at = Some(Instant::now());
                self.board.spawn(module);
            }
            Plus::Screen(name) => return Some(Strip::Open(name)),
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
                if let Some(module) = at
                    .checked_sub(rest + 6)
                    .and_then(|row| tile_modules().get(row).cloned())
                {
                    self.board.switch(index);
                    self.board.spawn(module);
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
        let chatting = !self.placed(&Module::Chat).is_empty();
        let tab_rows = Self::tab_menu(self.board.workspaces().get(self.pointed), chatting);
        let tab_menu = context_menu(tab_rows)
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
        let (rows, picks) = Self::plus_menu(openable, chatting);
        self.plus.update(cx, |button, cx| {
            button.items(rows, cx);
            button.on_pick(move |pick, window, cx| {
                let Some(choice) = picks.get(*pick).cloned().flatten() else {
                    return;
                };
                let strip = plus
                    .update(cx, |work, cx| {
                        let strip = work.picked_plus(choice);
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
        self.keep_terminals(cx);
        self.keep_mirrors(window, cx);
        self.keep_plugins();
        let above = window
            .focused(cx)
            .is_none_or(|held| held != self.focus && self.focus.within_focused(window, cx));
        if above {
            eprintln!("desk: work: focus was above the workspace, so the workspace takes it");
            self.focus.focus(window, cx);
        }
        let theme = ActiveTheme::theme(cx);
        let board = self.board.board(&theme, window, cx);
        self.focus_new_tile(window, cx);
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
            .capture_action(cx.listener(Self::open_find))
            .child(self.board.watch(band, cx).child(board))
            .child(self.find.clone())
            .children(self.replay.as_ref().map(Replay::meter))
    }
}
