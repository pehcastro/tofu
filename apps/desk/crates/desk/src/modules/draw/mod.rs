use std::cmp::Reverse;
use std::fs;
use std::io::ErrorKind;
use std::path::{Component, Path, PathBuf};
use std::time::Instant;

mod export;
mod image;
mod marks;
mod menu;
mod panel;

use desk_canvas::{Ask, BLANK, Backdrop, Canvas, Changed, Framing, Pick, Tool, View};
use desk_plugin::{Capability, Home};
use desk_ui::components::form::TextInput;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::overlay::{MenuItem, context_menu};
use desk_ui::components::paint::{drop, glyph, ink, ring, tint};
use desk_ui::components::size::{FONT_BODY, FONT_SMALL, T1, T2};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::components::width::Width;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use draw_doc::{DocError, Look, Op, Rect, Scene, bounds, flow, flow_scene};
use gpui::{
    AnyElement, AnyView, App, AppContext, Bounds, ClickEvent, Context, Div, ElementId, Entity,
    Focusable, Global, KeyDownEvent, KeyUpEvent, Pixels, Point, Render, SharedString, Stateful,
    StyleRefinement, WeakEntity, Window, div, prelude::*, px,
};
use serde::Deserialize;
use serde_json::{Map, Value, json};

use crate::plugins::{Plugin, consent};
use crate::screens::work::Work;
use desk_tiling::Module;
use marks::ICON_BOX;

const EXTENSION: &str = "excalidraw";
const UNTITLED: &str = "untitled";
const INSET: f32 = 12.0;
const ISLAND_PAD: f32 = 4.0;
const ISLAND_RADIUS: f32 = 8.0;
const ISLAND_RING: f32 = 0.08;
const ISLAND_SHADOW: f32 = 0.5;
const ISLAND_SHADOW_Y: f32 = 1.0;
const ISLAND_SHADOW_BLUR: f32 = 6.0;
const TOOL_BOX: f32 = 36.0;
const TOOL_ON: f32 = 0.18;
const DIGIT_FONT: f32 = 9.0;
const DIGIT_INSET: f32 = 3.0;
const ZOOM_BOX: f32 = 30.0;
const READOUT_WIDTH: f32 = 52.0;
const PANEL_WIDTH: f32 = 200.0;
const DOCK_LEAST: f32 = 640.0;
const MENU_WIDTH: f32 = 232.0;
const EXPORT_PAD: f32 = 16.0;
const EXPORT_MOST_PX: u32 = 4096;

pub const PLUGIN: &str = "tofu.draw";

pub fn mount(plugin: &'static Plugin, cx: &mut App) -> AnyView {
    cx.new(|cx| Draw::new(plugin, cx)).into()
}

#[derive(Default)]
struct Link {
    live: Option<WeakEntity<Draw>>,
    pending: Option<PathBuf>,
    claimed: Vec<PathBuf>,
}

impl Global for Link {}

fn live(cx: &App) -> Option<Entity<Draw>> {
    cx.try_global::<Link>()?.live.as_ref()?.upgrade()
}

pub fn show(path: PathBuf, cx: &mut App) {
    match live(cx) {
        Some(draw) => draw.update(cx, |draw, cx| {
            draw.open(path, cx);
            cx.notify();
        }),
        None => cx.default_global::<Link>().pending = Some(path),
    }
}

pub fn drawn(pad: Pixels, cx: &App) -> Option<Bounds<Pixels>> {
    live(cx)?.read(cx).canvas.read(cx).drawn(pad)
}

pub fn folder() -> Result<PathBuf, String> {
    Home::find()
        .map(|home| home.plugins.join("draw").join("boards"))
        .map_err(|error| error.to_string())
}

pub fn board(name: &str) -> Result<PathBuf, String> {
    let stem = name.strip_suffix(&format!(".{EXTENSION}")).unwrap_or(name);
    let single = Path::new(stem)
        .components()
        .map(|part| matches!(part, Component::Normal(_)))
        .collect::<Vec<_>>();
    if single != [true] {
        return Err(format!(
            "board {name:?} must be a plain name, no folder, no .."
        ));
    }
    Ok(folder()?.join(format!("{stem}.{EXTENSION}")))
}

pub fn boards() -> Result<Vec<PathBuf>, String> {
    listed(&folder()?)
}

pub fn replace(path: PathBuf, text: &str, cx: &mut App) -> Result<(), String> {
    if let Some(draw) = live(cx) {
        draw.update(cx, |draw, _| {
            if draw.board.as_ref() == Some(&path) {
                draw.board = None;
            }
        });
    }
    store(&path, text)?;
    show(path, cx);
    Ok(())
}

pub struct FlowBoard {
    pub text: String,
    pub nodes: usize,
    pub edges: usize,
}

pub fn flow_board(source: &str) -> Result<FlowBoard, String> {
    let parsed = flow(source).map_err(|error| error.to_string())?;
    let scene = flow_scene(&parsed, Look::Ui).map_err(|error| error.to_string())?;
    let written = scene.write().map_err(|error| error.to_string())?;
    let ui = Backdrop {
        look: Look::Ui,
        ..BLANK
    };
    Ok(FlowBoard {
        text: with_backdrop(&written, &ui)?,
        nodes: parsed.graph.nodes.len(),
        edges: parsed.graph.edges.len(),
    })
}

pub fn open_flow(source: &str, window: &mut Window, cx: &mut App) -> Result<PathBuf, String> {
    let drawn = flow_board(source)?;
    let Some(Some(desk)) = window.root::<crate::desk::Desk>() else {
        return Err("the chat is not inside the desk".to_owned());
    };
    let work = desk
        .read(cx)
        .work()
        .cloned()
        .ok_or("the work screen is not open")?;
    let path = untitled(&folder()?, cx)?;
    replace(path.clone(), &drawn.text, cx)?;
    reveal(&work, cx)?;
    eprintln!(
        "desk: draw opened a {} node flow from chat as {}",
        drawn.nodes,
        path.display()
    );
    Ok(path)
}

fn reveal(work: &Entity<Work>, cx: &mut App) -> Result<(), String> {
    let draw = Module::Plugin(PLUGIN.to_owned());
    work.update(cx, |work, cx| {
        let board = work.board_mut();
        board.apply(true, |held, area| {
            let found = held.tiles(area).into_iter().find_map(|(stack, _)| {
                Some((
                    stack.id,
                    stack.modules.iter().position(|module| *module == draw)?,
                ))
            });
            match found {
                Some((tile, at)) => held.activate(tile, at),
                None => held.open(draw.clone(), area),
            }
        });
        cx.notify();
        board.refusal().map_or(Ok(()), |refusal| {
            Err(format!("the draw tile did not open: {refusal:?}"))
        })
    })
}

pub fn store(path: &Path, text: &str) -> Result<(), String> {
    let found =
        crate::plugins::find(PLUGIN).ok_or(format!("plugin {PLUGIN} is not in this desk"))?;
    write(found, path, text.as_bytes())
}

fn showing(path: &Path, cx: &App) -> Option<Entity<Draw>> {
    live(cx).filter(|draw| draw.read(cx).board.as_deref() == Some(path))
}

pub fn edit<T>(
    path: &Path,
    change: impl FnOnce(&Scene) -> Result<(Vec<Op>, T), DocError>,
    cx: &mut App,
) -> Result<T, String> {
    let Some(draw) = showing(path, cx) else {
        let text =
            fs::read_to_string(path).map_err(|error| format!("{}: {error}", path.display()))?;
        let mut scene = Scene::read(&text).map_err(|error| error.to_string())?;
        let (ops, made) = change(&scene).map_err(|error| error.to_string())?;
        scene.apply(ops).map_err(|error| error.to_string())?;
        store(path, &scene.write().map_err(|error| error.to_string())?)?;
        return Ok(made);
    };
    draw.update(cx, |draw, cx| {
        let made = draw
            .canvas
            .update(cx, |canvas, cx| {
                let (ops, made) = change(canvas.scene())?;
                canvas.apply(ops, cx)?;
                Ok::<_, DocError>(made)
            })
            .map_err(|error| error.to_string())?;
        draw.store(cx)?;
        cx.notify();
        Ok(made)
    })
}

pub fn fit(path: &Path, keys: Option<&[String]>, cx: &mut App) -> Result<(Rect, f64), String> {
    let draw = showing(path, cx)
        .ok_or_else(|| format!("{} is not showing: run draw.open first", path.display()))?;
    draw.update(cx, |draw, cx| {
        draw.canvas.update(cx, |canvas, cx| {
            let rect = bounds(canvas.scene(), keys)
                .map_err(|error| error.to_string())?
                .ok_or_else(|| "the board is empty".to_owned())?;
            Ok((rect, canvas.fit(rect, cx).zoom()))
        })
    })
}

#[derive(Clone, Copy, PartialEq)]
enum Kept {
    Fresh,
    Restyled,
    Disk,
}

enum Menu {
    Closed,
    Open,
    Export,
    Rename(Entity<TextInput>),
}

enum Note {
    Problem(SharedString),
    Done(SharedString),
}

#[derive(Clone, Copy, PartialEq)]
enum Export {
    Idle,
    Asked,
    Hidden,
}

#[derive(Deserialize)]
struct BoardLook {
    #[serde(rename = "appState")]
    state: Option<AppState>,
}

#[derive(Deserialize)]
struct AppState {
    #[serde(rename = "viewBackgroundColor")]
    color: Option<String>,
    #[serde(rename = "gridModeEnabled")]
    dots: Option<bool>,
    #[serde(rename = "tofuLook")]
    look: Option<String>,
}

const UI_LOOK: &str = "ui";

pub struct Draw {
    plugin: &'static Plugin,
    folder: Result<PathBuf, String>,
    canvas: Entity<Canvas>,
    board: Option<PathBuf>,
    kept: Kept,
    menu: Menu,
    note: Option<Note>,
    opening: Option<(Instant, usize)>,
    export: Export,
    shown_tool: Option<Tool>,
    switch_frames: Vec<f64>,
    styles_open: bool,
    picker: Option<menu::Picker>,
    context: Option<(Point<Pixels>, bool)>,
    picking: bool,
    opacity_drag: Option<u8>,
    more_open: bool,
}

fn permit(plugin: &Plugin, path: &Path) -> Result<(), String> {
    let what = format!("write {}", path.display());
    consent::check(&plugin.id, &plugin.folder()?, Capability::FsWrite, &what)
}

fn write(plugin: &Plugin, path: &Path, bytes: &[u8]) -> Result<(), String> {
    permit(plugin, path)?;
    let at = |error: std::io::Error| format!("{}: {error}", path.display());
    if let Some(folder) = path.parent() {
        fs::create_dir_all(folder).map_err(at)?;
    }
    fs::write(path, bytes).map_err(at)
}

fn listed(folder: &Path) -> Result<Vec<PathBuf>, String> {
    let at = |error: std::io::Error| format!("{}: {error}", folder.display());
    let entries = match fs::read_dir(folder) {
        Ok(entries) => entries,
        Err(error) if error.kind() == ErrorKind::NotFound => return Ok(Vec::new()),
        Err(error) => return Err(at(error)),
    };
    let mut boards = Vec::new();
    for entry in entries {
        let entry = entry.map_err(at)?;
        let path = entry.path();
        if path
            .extension()
            .is_some_and(|extension| extension == EXTENSION)
        {
            let modified = entry.metadata().and_then(|meta| meta.modified());
            boards.push((modified.map_err(at)?, path));
        }
    }
    boards.sort_by_key(|(modified, _)| Reverse(*modified));
    Ok(boards.into_iter().map(|(_, path)| path).collect())
}

fn untitled(folder: &Path, cx: &App) -> Result<PathBuf, String> {
    let claimed = cx
        .try_global::<Link>()
        .map(|link| link.claimed.as_slice())
        .unwrap_or_default();
    (1..=u32::MAX)
        .map(|at| folder.join(format!("{UNTITLED}-{at}.{EXTENSION}")))
        .find(|path| !path.exists() && !claimed.contains(path))
        .ok_or_else(|| format!("{} has no free board name", folder.display()))
}

fn backdrop(text: &str) -> Result<Backdrop, String> {
    let look = serde_json::from_str::<BoardLook>(text)
        .map_err(|error| error.to_string())?
        .state;
    let ui = match look.as_ref().and_then(|look| look.look.as_deref()) {
        None => Look::Sketch,
        Some(UI_LOOK) => Look::Ui,
        Some(other) => return Err(format!("unknown board look {other}: use ui")),
    };
    Ok(Backdrop {
        color: look.as_ref().and_then(|look| look.color.clone()),
        dots: look.and_then(|look| look.dots).unwrap_or(BLANK.dots),
        look: ui,
    })
}

fn latest_backdrop(folder: &Path) -> Result<Backdrop, String> {
    let Some(latest) = listed(folder)?.into_iter().next() else {
        return Ok(BLANK);
    };
    fs::read_to_string(&latest)
        .map_err(|error| error.to_string())
        .and_then(|text| backdrop(&text))
        .map_err(|error| format!("{}: {error}", latest.display()))
}

fn with_backdrop(text: &str, backdrop: &Backdrop) -> Result<String, String> {
    let mut file: Map<String, Value> =
        serde_json::from_str(text).map_err(|error| error.to_string())?;
    let mut state = match file.remove("appState") {
        Some(Value::Object(state)) => state,
        _ => Map::new(),
    };
    match &backdrop.color {
        Some(color) => state.insert("viewBackgroundColor".to_owned(), json!(color)),
        None => state.remove("viewBackgroundColor"),
    };
    state.insert("gridModeEnabled".to_owned(), json!(backdrop.dots));
    match backdrop.look {
        Look::Ui => state.insert("tofuLook".to_owned(), json!(UI_LOOK)),
        Look::Sketch => state.remove("tofuLook"),
    };
    file.insert("appState".to_owned(), Value::Object(state));
    serde_json::to_string(&file).map_err(|error| error.to_string())
}

fn name(path: &Path) -> String {
    path.file_stem()
        .map(|stem| stem.to_string_lossy().into_owned())
        .unwrap_or_default()
}

fn start() -> Framing {
    Framing::At(View::new(0.0, 0.0, 1.0))
}

fn island(theme: &Theme) -> Div {
    let shadow = theme.color(ColorToken::Shadow);
    div()
        .bg(tint(theme.color(ColorToken::ToastFill), 1.0))
        .rounded(px(ISLAND_RADIUS))
        .text_color(ink(theme, T1))
        .shadow(vec![
            ring(ink(theme, ISLAND_RING)),
            drop(
                tint(shadow, shadow.alpha * ISLAND_SHADOW),
                ISLAND_SHADOW_Y,
                ISLAND_SHADOW_BLUR,
            ),
        ])
}

fn face(id: impl Into<ElementId>, on: bool, theme: &Theme) -> Stateful<Div> {
    let hover = theme.color(ColorToken::StateHover);
    div()
        .id(id)
        .relative()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(TOOL_BOX))
        .rounded(px(ISLAND_RADIUS))
        .cursor_pointer()
        .map(|face| match on {
            true => face.bg(tint(theme.color(ColorToken::FocusRing), TOOL_ON)),
            false => face.hover(move |style| style.bg(hover)),
        })
}

impl Draw {
    fn new(plugin: &'static Plugin, cx: &mut Context<Self>) -> Self {
        let weak = cx.weak_entity();
        let link = cx.default_global::<Link>();
        link.live = Some(weak);
        let pending = link.pending.take();
        cx.on_release(|draw, cx| draw.save(cx)).detach();
        cx.on_app_quit(|draw, cx| {
            draw.save(cx);
            async {}
        })
        .detach();
        let canvas = cx.new(|cx| Canvas::new(Scene::new(), cx));
        cx.subscribe(&canvas, |_, _, _: &Changed, cx| cx.notify())
            .detach();
        cx.subscribe(&canvas, |draw, _, ask: &Ask, cx| match ask {
            Ask::Menu { at, shape } => {
                draw.dismiss("context menu");
                draw.context = Some((*at, *shape));
                cx.notify();
            }
            Ask::Image(image) => draw.paste_image(image, cx),
        })
        .detach();
        let folder = folder();
        canvas.update(cx, |canvas, _| canvas.set_folder(folder.clone().ok()));
        let mut draw = Draw {
            plugin,
            folder,
            canvas,
            board: None,
            kept: Kept::Fresh,
            menu: Menu::Closed,
            note: None,
            opening: None,
            export: Export::Idle,
            shown_tool: None,
            switch_frames: Vec::new(),
            styles_open: false,
            picker: None,
            context: None,
            picking: false,
            opacity_drag: None,
            more_open: false,
        };
        match pending {
            Some(path) => draw.open(path, cx),
            None => draw.fresh(cx),
        }
        draw
    }

    fn fail(&mut self, problem: String) {
        eprintln!("desk: draw: {problem}");
        self.note = Some(Note::Problem(problem.into()));
    }

    fn save(&mut self, cx: &App) {
        if let Err(error) = self.store(cx) {
            self.fail(error);
        }
    }

    fn store(&mut self, cx: &App) -> Result<(), String> {
        let Some(path) = self.board.clone() else {
            return Ok(());
        };
        let canvas = self.canvas.read(cx);
        let shapes = canvas.scene().elements().len();
        if self.kept == Kept::Fresh && shapes == 0 {
            return Ok(());
        }
        let scene = canvas.scene().write().map_err(|error| error.to_string())?;
        let text = with_backdrop(&scene, canvas.backdrop())?;
        let path = match path.parent() {
            Some(folder) if self.kept != Kept::Disk && path.exists() => untitled(folder, cx)?,
            _ => path,
        };
        write(self.plugin, &path, text.as_bytes())?;
        eprintln!("desk: draw saved {} {shapes} shapes", path.display());
        self.board = Some(path);
        self.kept = Kept::Disk;
        Ok(())
    }

    fn fresh(&mut self, cx: &mut Context<Self>) {
        self.save(cx);
        self.menu = Menu::Closed;
        let started = Instant::now();
        let folder = match &self.folder {
            Ok(folder) => folder.clone(),
            Err(error) => return self.fail(error.clone()),
        };
        let backdrop = latest_backdrop(&folder).unwrap_or_else(|error| {
            self.fail(error);
            BLANK
        });
        let path = match untitled(&folder, cx) {
            Ok(path) => path,
            Err(error) => return self.fail(error),
        };
        cx.default_global::<Link>().claimed.push(path.clone());
        self.canvas.update(cx, |canvas, cx| {
            canvas.show(Scene::new(), start(), cx);
            canvas.set_backdrop(backdrop, cx);
        });
        eprintln!("desk: draw new board {}", path.display());
        self.board = Some(path);
        self.kept = Kept::Fresh;
        self.opening = Some((started, 0));
    }

    fn open(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        self.save(cx);
        self.menu = Menu::Closed;
        let started = Instant::now();
        let read = fs::read_to_string(&path)
            .map_err(|error| error.to_string())
            .and_then(|text| {
                let scene = Scene::read(&text).map_err(|error| error.to_string())?;
                Ok((scene, backdrop(&text)?))
            });
        let (scene, backdrop) = match read {
            Ok(read) => read,
            Err(error) => return self.fail(format!("{}: {error}", path.display())),
        };
        let shapes = scene.elements().len();
        let framing = match shapes {
            0 => start(),
            _ => Framing::Fit,
        };
        self.canvas.update(cx, |canvas, cx| {
            canvas.show(scene, framing, cx);
            canvas.set_backdrop(backdrop, cx);
        });
        eprintln!("desk: draw opened {}", path.display());
        self.board = Some(path);
        self.kept = Kept::Disk;
        self.opening = Some((started, shapes));
        self.note = None;
    }

    fn restyle(&mut self, change: impl FnOnce(&mut Backdrop), cx: &mut Context<Self>) {
        self.canvas.update(cx, |canvas, cx| {
            let mut backdrop = canvas.backdrop().clone();
            change(&mut backdrop);
            canvas.set_backdrop(backdrop, cx);
        });
        if self.kept == Kept::Fresh {
            self.kept = Kept::Restyled;
        }
        self.save(cx);
        cx.notify();
    }

    fn use_tool(&mut self, tool: Option<Tool>, window: &mut Window, cx: &mut Context<Self>) {
        self.canvas.update(cx, |canvas, cx| {
            match tool {
                Some(tool) => canvas.set_tool(tool, cx),
                None => canvas.toggle_lock(cx),
            }
            canvas.focus_handle(cx).focus(window, cx);
        });
    }

    fn key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        if self.picker.is_some() || matches!(self.menu, Menu::Rename(_)) {
            return;
        }
        let keys = &event.keystroke;
        if keys.key == "s" && keys.modifiers.secondary() && !keys.modifiers.shift {
            cx.stop_propagation();
            self.save(cx);
            cx.notify();
            return;
        }
        let typing = self.canvas.read(cx).typing();
        if keys.key == "space" && !typing {
            cx.stop_propagation();
            self.canvas.update(cx, |canvas, cx| {
                canvas.hold_space(true, cx);
                canvas.focus_handle(cx).focus(window, cx);
            });
            return;
        }
        let plain = !keys.modifiers.secondary() && !keys.modifiers.alt;
        let tool = Tool::of_key(&keys.key);
        if !plain || typing || (tool.is_none() && keys.key != "q") {
            return;
        }
        cx.stop_propagation();
        self.use_tool(tool, window, cx);
    }

    fn escape(&mut self, event: &KeyDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        if event.keystroke.key == "escape" && self.dismiss("escape") {
            cx.stop_propagation();
            cx.notify();
        }
    }

    fn dismiss(&mut self, by: &str) -> bool {
        let open = !matches!(self.menu, Menu::Closed)
            || self.styles_open
            || self.more_open
            || self.context.is_some()
            || self.picker.is_some();
        if open {
            eprintln!("desk: draw: popover closed by {by}");
            self.menu = Menu::Closed;
            self.styles_open = false;
            self.more_open = false;
            self.context = None;
            self.picker = None;
        }
        open
    }

    fn key_up(&mut self, event: &KeyUpEvent, _: &mut Window, cx: &mut Context<Self>) {
        if event.keystroke.key == "space" {
            self.canvas
                .update(cx, |canvas, cx| canvas.hold_space(false, cx));
        }
    }

    fn tool_menu(
        &self,
        tool: Tool,
        at: usize,
        button: AnyElement,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let sections = panel::sections(tool, tool == Tool::Arrow);
        if sections.is_empty() {
            return button;
        }
        let canvas = self.canvas.read(cx);
        let mut flat: Vec<Option<Pick>> = Vec::new();
        let mut items = Vec::with_capacity(sections.len());
        for section in sections {
            flat.push(None);
            flat.extend(section.picks.iter().copied().map(Some));
            items.push(MenuItem::Submenu {
                label: section.title.into(),
                icon: None,
                items: section
                    .picks
                    .iter()
                    .map(|pick| match canvas.defaults(*pick) {
                        true => MenuItem::action(pick.name()).icon(Glyph::Check),
                        false => MenuItem::action(pick.name()),
                    })
                    .collect(),
            });
        }
        context_menu(items)
            .id(("draw-tool-menu", at))
            .on_pick(cx.listener(move |draw, picked: &usize, window, cx| {
                let Some(Some(pick)) = flat.get(*picked).copied() else {
                    return;
                };
                eprintln!("desk: draw: tool menu {} {pick:?}", tool.name());
                draw.use_tool(Some(tool), window, cx);
                draw.canvas
                    .update(cx, |canvas, cx| canvas.set_default(pick, cx));
            }))
            .child(button)
            .into_any_element()
    }

    fn toolbar(
        &self,
        narrow: bool,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        let (active, locked) = self.canvas.read(cx).tool();
        let accent = theme.color(ColorToken::FocusRing);
        let plain = ink(theme, T1);
        let lock = face("draw-lock", locked, theme)
            .child(glyph(
                Glyph::Lock,
                ICON_BOX,
                if locked { accent } else { plain },
            ))
            .on_click(
                cx.listener(|draw, _: &ClickEvent, window, cx| draw.use_tool(None, window, cx)),
            );
        let mut tools = vec![
            tooltip(
                "draw-lock-tip",
                lock,
                Edge::Frame,
                "Keep the tool after drawing  Q",
                theme,
                window,
                cx,
            )
            .into_any_element(),
            div()
                .w(px(1.0))
                .h(px(ICON_BOX))
                .mx_1()
                .bg(theme.color(ColorToken::Separator))
                .into_any_element(),
        ];
        for (at, tool) in Tool::ALL.into_iter().enumerate() {
            let on = tool == active;
            let button = face(("draw-tool", at), on, theme)
                .child(marks::paint(
                    marks::of_tool(tool),
                    if on { accent } else { plain },
                ))
                .child(
                    div()
                        .absolute()
                        .right(px(DIGIT_INSET))
                        .bottom(px(0.0))
                        .text_size(px(DIGIT_FONT))
                        .text_color(ink(theme, T2))
                        .children(tool.digit()),
                )
                .on_click(cx.listener(move |draw, _: &ClickEvent, window, cx| {
                    draw.use_tool(Some(tool), window, cx)
                }));
            let key = tool.key().to_uppercase();
            let tip = match tool.digit() {
                Some(digit) => format!("{}  {key} or {digit}", tool.name()),
                None => format!("{}  {key}", tool.name()),
            };
            let tipped = tooltip(
                ("draw-tool-tip", at),
                button,
                Edge::Frame,
                tip,
                theme,
                window,
                cx,
            );
            tools.push(self.tool_menu(tool, at, tipped.into_any_element(), cx));
        }
        tools.push(self.more(theme, cx));
        if narrow {
            tools.push(self.compact(theme, window, cx));
        }
        island(theme)
            .id("draw-toolbar")
            .occlude()
            .flex()
            .flex_wrap()
            .justify_center()
            .items_center()
            .max_w_full()
            .gap_0p5()
            .p(px(ISLAND_PAD))
            .children(tools)
    }

    fn bottom(
        &self,
        narrow: bool,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Div {
        div()
            .absolute()
            .left(px(INSET))
            .right(px(INSET))
            .bottom(px(INSET))
            .flex()
            .flex_wrap_reverse()
            .items_end()
            .gap(px(ISLAND_PAD * 2.0))
            .child(self.zoom(theme, cx))
            .child(
                div()
                    .flex()
                    .flex_grow_1()
                    .min_w_0()
                    .justify_center()
                    .child(self.toolbar(narrow, theme, window, cx)),
            )
    }

    fn zoom(&self, theme: &Theme, cx: &mut Context<Self>) -> Stateful<Div> {
        let percent = format!("{:.0}%", self.canvas.read(cx).zoom() * 100.0);
        let nudge = |id: &'static str, label: &'static str, steps: f64, cx: &mut Context<Self>| {
            face(id, false, theme)
                .size(px(ZOOM_BOX))
                .child(label)
                .on_click(cx.listener(move |draw, _: &ClickEvent, _, cx| {
                    draw.canvas
                        .update(cx, |canvas, cx| canvas.nudge_zoom(steps, cx));
                }))
        };
        island(theme)
            .id("draw-zoom")
            .occlude()
            .flex_none()
            .flex()
            .items_center()
            .p(px(ISLAND_PAD))
            .text_size(px(FONT_BODY))
            .child(nudge("draw-zoom-out", "-", -1.0, cx))
            .child(
                face("draw-zoom-reset", false, theme)
                    .h(px(ZOOM_BOX))
                    .w(px(READOUT_WIDTH))
                    .child(percent)
                    .on_click(cx.listener(|draw, _: &ClickEvent, _, cx| {
                        draw.canvas.update(cx, |canvas, cx| canvas.zoom_to(1.0, cx));
                    })),
            )
            .child(nudge("draw-zoom-in", "+", 1.0, cx))
    }

    fn note(&self, theme: &Theme, cx: &mut Context<Self>) -> Option<Div> {
        let (text, color) = match self.note.as_ref()? {
            Note::Problem(text) => (text.clone(), theme.color(ColorToken::StatusDanger)),
            Note::Done(text) => (text.clone(), ink(theme, T2)),
        };
        Some(
            div()
                .absolute()
                .left_0()
                .right_0()
                .top(px(INSET))
                .flex()
                .justify_center()
                .child(
                    island(theme)
                        .id("draw-note")
                        .occlude()
                        .max_w(px(MENU_WIDTH * 2.0))
                        .px_2()
                        .py_1()
                        .truncate()
                        .cursor_pointer()
                        .text_size(px(FONT_SMALL))
                        .text_color(color)
                        .child(text)
                        .on_click(cx.listener(|draw, _: &ClickEvent, _, cx| {
                            draw.note = None;
                            cx.notify();
                        })),
                ),
        )
    }

    fn time_switch(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let started = Instant::now();
        let this = cx.weak_entity();
        window.on_next_frame(move |_, cx| {
            let ms = started.elapsed().as_secs_f64() * 1000.0;
            let timed = this.update(cx, |draw, _| {
                draw.switch_frames.push(ms);
                let mut sorted = draw.switch_frames.clone();
                sorted.sort_by(f64::total_cmp);
                let median = sorted.get(sorted.len() / 2).copied().unwrap_or_default();
                let worst = sorted.last().copied().unwrap_or_default();
                eprintln!(
                    "desk: draw tool switch frames: {} frames, median {median:.2} ms, worst {worst:.2} ms",
                    sorted.len()
                );
            });
            if let Err(error) = timed {
                eprintln!("desk: draw: tool switch timing: the draw tile is gone: {error}");
            }
        });
    }
}

impl Render for Draw {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if let Some((started, shapes)) = self.opening.take() {
            window.on_next_frame(move |_, _| {
                eprintln!(
                    "desk: draw open to first paint {} ms {shapes} shapes",
                    started.elapsed().as_millis()
                )
            });
        }
        if self.export == Export::Asked {
            self.hide_for_export(window, cx);
        }
        let tool = self.canvas.read(cx).tool().0;
        if self.shown_tool.replace(tool).is_some_and(|was| was != tool) {
            self.time_switch(window, cx);
        }
        let theme = ActiveTheme::theme(cx);
        let width = Width::of(format!("draw-width-{}", cx.entity_id()), window, cx);
        let narrow = width.get(cx).is_some_and(|width| width < px(DOCK_LEAST));
        self.styles_open &= narrow;
        let overlays: Vec<AnyElement> = match self.export {
            Export::Idle => {
                let mut overlays = vec![self.bottom(narrow, &theme, window, cx).into_any_element()];
                if !narrow {
                    overlays.extend(
                        self.docked(&theme, window, cx)
                            .map(IntoElement::into_any_element),
                    );
                }
                overlays.extend(self.note(&theme, cx).map(IntoElement::into_any_element));
                overlays.push(self.menu(&theme, cx));
                overlays.extend(self.context_menu(&theme, cx));
                overlays.extend(self.picker_sheet(&theme, cx));
                overlays
            }
            Export::Asked | Export::Hidden => Vec::new(),
        };
        div()
            .relative()
            .size_full()
            .min_w_0()
            .child(width.probe())
            .capture_key_down(cx.listener(Self::escape))
            .on_key_down(cx.listener(Self::key))
            .on_key_up(cx.listener(Self::key_up))
            .child(
                self.canvas
                    .clone()
                    .cached(StyleRefinement::default().size_full()),
            )
            .children(overlays)
    }
}
