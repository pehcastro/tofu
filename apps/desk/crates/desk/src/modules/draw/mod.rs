use std::cmp::Reverse;
use std::fs;
use std::io::ErrorKind;
use std::path::{Component, Path, PathBuf};
use std::time::Instant;

use desk_canvas::{Canvas, Framing, Tool, ToolChanged};
use desk_plugin::{Capability, Home};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::empty::empty_state;
use desk_ui::components::paint::ink;
use desk_ui::components::size::{FONT_BODY, FONT_SMALL, RADIUS_ROW, T1, T2};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use draw_doc::Scene;
use gpui::{
    AnyElement, AnyView, App, AppContext, Bounds, ClickEvent, Context, Entity, Focusable, Global,
    KeyDownEvent, Pixels, Render, SharedString, StyleRefinement, WeakEntity, Window, div,
    prelude::*, px,
};
use serde_json::{Value, json};

use crate::plugins::{Plugin, consent};

const EXTENSION: &str = "excalidraw";
const GRID_SHAPES: usize = 10_000;
const GRID_COLUMNS: usize = 100;
const GRID_PITCH: (f64, f64) = (160.0, 120.0);
const GRID_SHAPE: (f64, f64) = (120.0, 80.0);
const GRID_KINDS: [&str; 5] = ["rectangle", "ellipse", "diamond", "text", "arrow"];

pub const PLUGIN: &str = "tofu.draw";

pub fn mount(plugin: &'static Plugin, cx: &mut App) -> AnyView {
    cx.new(|cx| Draw::new(plugin, cx)).into()
}

struct Link {
    live: Option<WeakEntity<Draw>>,
    pending: Option<PathBuf>,
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
        None => cx.set_global(Link {
            live: None,
            pending: Some(path),
        }),
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
    let plugin =
        crate::plugins::find(PLUGIN).ok_or(format!("plugin {PLUGIN} is not in this desk"))?;
    if let Some(draw) = live(cx) {
        draw.update(cx, |draw, _| {
            if draw.board.as_ref() == Some(&path) {
                draw.board = None;
            }
        });
    }
    write(plugin, &path, text)?;
    show(path, cx);
    Ok(())
}

pub struct Draw {
    plugin: &'static Plugin,
    folder: Result<PathBuf, String>,
    canvas: Entity<Canvas>,
    board: Option<PathBuf>,
    picker: Option<Vec<PathBuf>>,
    problem: Option<SharedString>,
    opening: Option<(Instant, usize)>,
}

fn write(plugin: &Plugin, path: &Path, text: &str) -> Result<(), String> {
    let what = format!("write {}", path.display());
    consent::check(&plugin.id, &plugin.folder()?, Capability::FsWrite, &what)?;
    let at = |error: std::io::Error| format!("{}: {error}", path.display());
    if let Some(folder) = path.parent() {
        fs::create_dir_all(folder).map_err(at)?;
    }
    fs::write(path, text).map_err(at)
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

fn grid(count: usize) -> Result<Scene, String> {
    let elements: Vec<Value> = (0..count)
        .map(|at| {
            let x = (at % GRID_COLUMNS) as f64 * GRID_PITCH.0;
            let y = (at / GRID_COLUMNS) as f64 * GRID_PITCH.1;
            let kind = GRID_KINDS
                .get(at % GRID_KINDS.len())
                .copied()
                .unwrap_or("rectangle");
            let mut element = json!({
                "id": format!("bench-{at}"),
                "type": kind,
                "x": x,
                "y": y,
                "width": GRID_SHAPE.0,
                "height": GRID_SHAPE.1,
                "strokeColor": "#1e1e1e",
                "backgroundColor": "transparent",
                "strokeWidth": 1,
                "roundness": null,
            });
            match kind {
                "text" => {
                    element["text"] = json!(format!("shape {at}"));
                    element["fontSize"] = json!(20);
                }
                "arrow" => {
                    element["points"] = json!([[0.0, 0.0], [GRID_SHAPE.0, GRID_SHAPE.1]]);
                    element["endArrowhead"] = json!("arrow");
                }
                _ => {}
            }
            element
        })
        .collect();
    let file =
        json!({ "type": "excalidraw", "version": 2, "source": "tofu-draw", "elements": elements });
    Scene::read(&file.to_string()).map_err(|error| format!("grid board: {error}"))
}

fn name(path: &Path) -> String {
    path.file_stem()
        .map(|stem| stem.to_string_lossy().into_owned())
        .unwrap_or_default()
}

impl Draw {
    fn new(plugin: &'static Plugin, cx: &mut Context<Self>) -> Self {
        let pending = cx
            .try_global::<Link>()
            .and_then(|link| link.pending.clone());
        cx.set_global(Link {
            live: Some(cx.weak_entity()),
            pending: None,
        });
        cx.on_release(|draw, cx| draw.save(cx)).detach();
        cx.on_app_quit(|draw, cx| {
            draw.save(cx);
            async {}
        })
        .detach();
        let canvas = cx.new(|cx| Canvas::new(Scene::new(), cx));
        cx.subscribe(&canvas, |_, _, _: &ToolChanged, cx| cx.notify())
            .detach();
        let mut draw = Draw {
            plugin,
            folder: folder(),
            canvas,
            board: None,
            picker: None,
            problem: None,
            opening: None,
        };
        if let Some(path) = pending {
            draw.open(path, cx);
        }
        draw
    }

    fn fail(&mut self, problem: String) {
        eprintln!("desk: draw: {problem}");
        self.problem = Some(problem.into());
    }

    fn save(&mut self, cx: &App) {
        let Some(path) = &self.board else {
            return;
        };
        let scene = self.canvas.read(cx).scene();
        let written = scene
            .write()
            .map_err(|error| error.to_string())
            .and_then(|text| write(self.plugin, path, &text));
        match written {
            Ok(()) => eprintln!(
                "desk: draw saved {} {} shapes",
                path.display(),
                scene.elements().len()
            ),
            Err(error) => self.fail(error),
        }
    }

    fn open(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        self.save(cx);
        self.picker = None;
        let started = Instant::now();
        let read = fs::read_to_string(&path)
            .map_err(|error| error.to_string())
            .and_then(|text| Scene::read(&text).map_err(|error| error.to_string()));
        let scene = match read {
            Ok(scene) => scene,
            Err(error) => return self.fail(format!("{}: {error}", path.display())),
        };
        let shapes = scene.elements().len();
        self.canvas
            .update(cx, |canvas, cx| canvas.show(scene, Framing::Fit, cx));
        eprintln!("desk: draw opened {}", path.display());
        self.board = Some(path);
        self.opening = Some((started, shapes));
        self.problem = None;
    }

    fn new_board(&mut self, stem: &str, scene: Result<Scene, String>, cx: &mut Context<Self>) {
        let folder = match &self.folder {
            Ok(folder) => folder.clone(),
            Err(error) => return self.fail(error.clone()),
        };
        let Some(path) = (1..=u32::MAX)
            .map(|at| folder.join(format!("{stem}-{at}.{EXTENSION}")))
            .find(|path| !path.exists())
        else {
            return self.fail(format!("{} has no free board name", folder.display()));
        };
        let written = scene
            .and_then(|scene| scene.write().map_err(|error| error.to_string()))
            .and_then(|text| write(self.plugin, &path, &text));
        match written {
            Ok(()) => self.open(path, cx),
            Err(error) => self.fail(error),
        }
    }

    fn pick(&mut self) {
        if self.picker.take().is_some() {
            return;
        }
        let listing = match &self.folder {
            Ok(folder) => listed(folder),
            Err(error) => Err(error.clone()),
        };
        match listing {
            Ok(boards) => self.picker = Some(boards),
            Err(error) => self.fail(error),
        }
    }

    fn key(&mut self, event: &KeyDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        let keys = &event.keystroke;
        if keys.key == "s" && keys.modifiers.secondary() && !keys.modifiers.shift {
            cx.stop_propagation();
            self.save(cx);
            cx.notify();
        }
    }

    fn tools(
        &self,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> impl IntoElement {
        let (active, locked) = self.canvas.read(cx).tool();
        let picks = Tool::ALL
            .into_iter()
            .map(|tool| (Some(tool), tool.name(), tool.key(), tool == active))
            .chain([(None, "Lock", "q", locked)]);
        let mut row = div()
            .flex()
            .flex_none()
            .items_center()
            .gap_0p5()
            .px_2()
            .pb_1()
            .overflow_hidden();
        for (at, (tool, name, key, on)) in picks.enumerate() {
            let face = button(("draw-tool", at), name, None, ButtonKind::Text, theme)
                .px_2()
                .when(on, |face| {
                    face.bg(theme.color(ColorToken::ButtonPrimary))
                        .text_color(theme.color(ColorToken::ButtonPrimaryText))
                })
                .on_click(cx.listener(move |draw, _: &ClickEvent, window, cx| {
                    draw.canvas.update(cx, |canvas, cx| {
                        match tool {
                            Some(tool) => canvas.set_tool(tool, cx),
                            None => canvas.toggle_lock(cx),
                        }
                        canvas.focus_handle(cx).focus(window, cx);
                    });
                }));
            let tip = format!("{name}  {}", key.to_uppercase());
            row = row.child(tooltip(
                ("draw-tool-tip", at),
                face,
                Edge::Frame,
                tip,
                theme,
                window,
                cx,
            ));
        }
        row
    }

    fn rows(&self, boards: &[PathBuf], theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
        if boards.is_empty() {
            let folder = self.folder.as_ref().map(|f| f.display().to_string());
            let line = format!("No .{EXTENSION} files in {}", folder.unwrap_or_default());
            return empty_state(
                "draw-none",
                "No boards",
                Some(line.into()),
                &[],
                &[],
                theme,
                |_, _, _| {},
            )
            .into_any_element();
        }
        let hover = theme.color(ColorToken::StateHover);
        div()
            .id("draw-boards")
            .flex()
            .flex_col()
            .flex_1()
            .min_h_0()
            .p_1()
            .overflow_y_scroll()
            .children(boards.iter().enumerate().map(|(at, path)| {
                let picked = path.clone();
                div()
                    .id(("draw-board", at))
                    .px_2()
                    .py_1()
                    .rounded(px(RADIUS_ROW))
                    .truncate()
                    .cursor_pointer()
                    .text_size(px(FONT_BODY))
                    .text_color(ink(theme, T2))
                    .hover(move |style| style.bg(hover))
                    .on_click(cx.listener(move |draw, _: &ClickEvent, _, cx| {
                        draw.open(picked.clone(), cx);
                        cx.notify();
                    }))
                    .child(name(path))
            }))
            .into_any_element()
    }
}

impl Render for Draw {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if let Some((started, shapes)) = self.opening.take() {
            window.on_next_frame(move |_, _| {
                eprintln!(
                    "desk: draw opened {shapes} shapes in {} ms",
                    started.elapsed().as_millis()
                )
            });
        }
        let theme = ActiveTheme::theme(cx);
        let title: SharedString = match &self.board {
            Some(path) => name(path).into(),
            None => "No board open".into(),
        };
        let header = div()
            .flex()
            .flex_none()
            .items_center()
            .gap_2()
            .px_2()
            .py_1()
            .min_w_0()
            .child(
                button("draw-new", "New board", None, ButtonKind::Plain, &theme).on_click(
                    cx.listener(|draw, _: &ClickEvent, _, cx| {
                        draw.new_board("untitled", Ok(Scene::new()), cx);
                        cx.notify();
                    }),
                ),
            )
            .child(
                button(
                    "draw-grid",
                    "10,000 shapes",
                    None,
                    ButtonKind::Plain,
                    &theme,
                )
                .on_click(cx.listener(|draw, _: &ClickEvent, _, cx| {
                    draw.new_board("shapes-10000", grid(GRID_SHAPES), cx);
                    cx.notify();
                })),
            )
            .child(
                button("draw-open", "Open", None, ButtonKind::Plain, &theme).on_click(cx.listener(
                    |draw, _: &ClickEvent, _, cx| {
                        draw.pick();
                        cx.notify();
                    },
                )),
            )
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .truncate()
                    .text_size(px(FONT_BODY))
                    .text_color(ink(&theme, T1))
                    .child(title),
            );
        let problem = self.problem.clone().map(|problem| {
            div()
                .px_2()
                .truncate()
                .text_size(px(FONT_SMALL))
                .text_color(theme.color(ColorToken::StatusDanger))
                .child(problem)
        });
        let body = match (&self.picker, &self.board) {
            (Some(boards), _) => self.rows(boards, &theme, cx),
            (None, Some(_)) => div()
                .flex_1()
                .min_h_0()
                .flex()
                .flex_col()
                .child(self.tools(&theme, window, cx))
                .child(
                    div().flex_1().min_h_0().child(
                        self.canvas
                            .clone()
                            .cached(StyleRefinement::default().size_full()),
                    ),
                )
                .into_any_element(),
            (None, None) => empty_state(
                "draw-empty",
                "No board open",
                Some("Start a new board, or open one from your boards folder.".into()),
                &[],
                &[],
                &theme,
                |_, _, _| {},
            )
            .into_any_element(),
        };
        div()
            .size_full()
            .flex()
            .flex_col()
            .min_w_0()
            .on_key_down(cx.listener(Self::key))
            .child(header)
            .children(problem)
            .child(body)
    }
}
