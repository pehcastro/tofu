mod agents;
mod chat;
mod chrome;
mod fixture;
mod glyph;
mod paint;

use std::borrow::Cow;
use std::sync::Arc;

use desk_tiling::{
    self as tiling, DRAG_THRESHOLD, Divider, Module, NUDGE, Preset, Rect, Refusal, Side, Stack,
    Store, Target, TileId, Workspace,
};
use gpui::{
    AnyView, App, AppContext, Context, Div, FocusHandle, Image, ImageFormat, IntoElement,
    KeyDownEvent, MouseButton, MouseDownEvent, MouseMoveEvent, MouseUpEvent, Pixels, Point, Render,
    TextRenderingMode, Window, div, img, prelude::*, px,
};

use fixture::{PROJECT, SESSION, STACK_BADGES};
use glyph::{Glyph, glyph, glyph_at};
use paint::{CAPTION, FAINT, SANS, SHELL, SOFT, STRONG, at, ink, medium, mono, rgb, ring, text};

pub const BOARD_WIDTH: f32 = 1400.0;
const INSET_X: f32 = 20.0;
const INSET_Y: f32 = 18.0;
const AREA_LEFT: f32 = 248.0;
const AREA_TOP: f32 = 42.0;
const AREA_RIGHT: f32 = 8.0;
const AREA_BOTTOM: f32 = 38.0;
const BACKDROP: &[u8] = include_bytes!("../../modules/chat/assets/backdrop.jpg");
const SINGLE_HEADER: f32 = 28.0;
const STACK_HEADER: f32 = 36.0;
const MENU_WIDTH: f32 = 180.0;
const MENU_ROW: f32 = 28.0;
const DIVIDER_LINE: f32 = 2.0;
const WORK_NAME: &str = "work";

const FONTS: [&[u8]; 6] = [
    include_bytes!("../../../../../assets/fonts/Geist-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-SemiBold.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-SemiBold.ttf"),
];

pub fn open(board: Option<&str>, window: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    let sheet = match board {
        None | Some("IWY-4") => false,
        Some("S-WORK-1") => true,
        Some(other) => {
            return Err(format!(
                "the work screen draws IWY-4 and S-WORK-1, not {other}"
            ));
        }
    };
    cx.set_text_rendering_mode(TextRenderingMode::Grayscale);
    cx.text_system()
        .add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("the work screen could not load Geist: {error:#}"))?;
    let store = Store::for_project(PROJECT).map_err(|error| error.to_string())?;
    let mut saved = store
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
    Ok(cx
        .new(|cx| {
            let focus = cx.focus_handle();
            focus.focus(window, cx);
            Work {
                workspace,
                others,
                store,
                sheet,
                gesture: Gesture::Idle,
                nudge: None,
                menu: false,
                focus,
                backdrop: Arc::new(Image::from_bytes(ImageFormat::Jpeg, BACKDROP.to_vec())),
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
    Pressed {
        grab: Grab,
        from: (f32, f32),
    },
    Dragging {
        grab: Grab,
        at: (f32, f32),
        target: Option<Target>,
    },
    Resizing {
        divider: Divider,
        from: Workspace,
    },
}

pub struct Work {
    workspace: Workspace,
    others: Vec<Workspace>,
    store: Store,
    sheet: bool,
    gesture: Gesture,
    nudge: Option<f32>,
    menu: bool,
    focus: FocusHandle,
    backdrop: Arc<Image>,
}

fn board_size(window: &Window) -> (f32, f32) {
    let size = window.viewport_size();
    (
        f32::from(size.width) - 2.0 * INSET_X,
        f32::from(size.height) - 2.0 * INSET_Y,
    )
}

fn area(width: f32, height: f32) -> Rect {
    Rect {
        x: AREA_LEFT,
        y: AREA_TOP,
        w: width - AREA_LEFT - AREA_RIGHT,
        h: height - AREA_TOP - AREA_BOTTOM,
    }
}

fn window_area(window: &Window) -> Rect {
    let (width, height) = board_size(window);
    area(width, height)
}

fn local(position: Point<Pixels>) -> (f32, f32) {
    (
        f32::from(position.x) - INSET_X,
        f32::from(position.y) - INSET_Y,
    )
}

fn module_glyph(module: &Module) -> Glyph {
    match module {
        Module::Chat => Glyph::Chat,
        Module::SubAgents => Glyph::People,
        Module::FileEdits => Glyph::File,
        Module::Shells | Module::Terminal => Glyph::Terminal,
        Module::Editor => Glyph::Code,
        Module::Browser => Glyph::Search,
        Module::SourceControl => Glyph::Branch,
        Module::Plugin(_) => Glyph::Data,
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
    at(element, rect.x, rect.y).w(px(rect.w)).h(px(rect.h))
}

impl Work {
    fn commit(&mut self, next: Result<Workspace, Refusal>, cx: &mut Context<Self>) {
        match next {
            Ok(workspace) => {
                self.workspace = workspace;
                let all: Vec<Workspace> = std::iter::once(self.workspace.clone())
                    .chain(self.others.iter().cloned())
                    .collect();
                if let Err(error) = self.store.save(&all) {
                    eprintln!("the layout was not saved: {error}");
                }
            }
            Err(refusal) => eprintln!("the layout refused that: {refusal}"),
        }
        cx.notify();
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
                at: (x, y),
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
            Gesture::Pressed { grab, .. } => self.workspace.activate(grab.tile, grab.module),
            Gesture::Dragging { grab, target, .. } => target
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
            "escape" if self.nudge.take().is_some() || self.menu => {
                self.menu = false;
                cx.notify();
                return;
            }
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

    fn grab(
        &self,
        grab: Grab,
        cx: &mut Context<Self>,
    ) -> impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static {
        cx.listener(move |work, event: &MouseDownEvent, _, cx| {
            work.gesture = Gesture::Pressed {
                grab,
                from: local(event.position),
            };
            work.menu = false;
            cx.notify();
        })
    }

    fn header(&self, stack: &Stack, width: f32, cx: &mut Context<Self>) -> Div {
        let tile = stack.id;
        let [module] = stack.modules.as_slice() else {
            return self.tabs(stack, cx);
        };
        div()
            .absolute()
            .left_0()
            .top_0()
            .w(px(width))
            .h(px(SINGLE_HEADER))
            .on_mouse_down(MouseButton::Left, self.grab(Grab { tile, module: 0 }, cx))
            .child(glyph_at(module_glyph(module), 13.0, ink(0.6), 12.0, 7.5))
            .child(at(medium(module.name().to_owned(), 12.0, SOFT), 31.0, 6.0))
            .children((*module == Module::Chat).then(|| {
                at(
                    div()
                        .w(px(width - 40.0))
                        .flex()
                        .justify_end()
                        .child(medium(SESSION, 12.0, FAINT)),
                    31.0,
                    6.0,
                )
            }))
    }

    fn tabs(&self, stack: &Stack, cx: &mut Context<Self>) -> Div {
        let tabs = stack.modules.iter().enumerate().map(|(index, module)| {
            let on = index == stack.active;
            let badge = Module::BUILT_IN
                .iter()
                .position(|known| known == module)
                .and_then(|known| STACK_BADGES.get(known.saturating_sub(1)))
                .filter(|_| *module != Module::Chat);
            div()
                .flex()
                .items_center()
                .h(px(31.0))
                .pb(px(1.4))
                .pl(px(30.0))
                .pr(px(3.0))
                .relative()
                .rounded_t(px(9.0))
                .when(on, |tab| tab.bg(ink(0.06)))
                .child(glyph_at(
                    module_glyph(module),
                    13.0,
                    if on { ink(1.0) } else { ink(0.5) },
                    8.0,
                    9.0,
                ))
                .on_mouse_down(
                    MouseButton::Left,
                    self.grab(
                        Grab {
                            tile: stack.id,
                            module: index,
                        },
                        cx,
                    ),
                )
                .child(medium(
                    module.name().to_owned(),
                    12.5,
                    if on { ink(1.0) } else { ink(0.5) },
                ))
                .children(badge.map(|badge| {
                    div()
                        .ml(px(7.0))
                        .h(px(16.5))
                        .px(px(5.0))
                        .pt(px(1.0))
                        .rounded(px(5.0))
                        .bg(ink(0.06))
                        .child(mono(*badge, 10.5, CAPTION).font_weight(gpui::FontWeight::MEDIUM))
                }))
                .child(div().w(px(24.0)).flex().justify_center().children(
                    (on && stack.modules.len() > 1).then(|| glyph(Glyph::Close, 11.0, CAPTION)),
                ))
        });
        at(
            div().flex().gap(px(2.0)).children(tabs).child(
                div()
                    .ml(px(2.0))
                    .size(px(26.0))
                    .pt(px(5.0))
                    .flex()
                    .justify_center()
                    .child(medium("+", 12.0, CAPTION)),
            ),
            15.0,
            5.0,
        )
    }

    fn body(&self, module: &Module, width: f32, height: f32) -> Vec<Div> {
        match module {
            Module::Chat => chat::body(width, height),
            Module::SubAgents => {
                let mut cells = vec![agents::table(width, height)];
                cells.extend(self.sheet.then(|| agents::sheet(width, height)));
                cells
            }
            Module::FileEdits | Module::Shells => Vec::new(),
            Module::Editor
            | Module::Terminal
            | Module::Browser
            | Module::SourceControl
            | Module::Plugin(_) => vec![
                div()
                    .size_full()
                    .flex()
                    .items_center()
                    .justify_center()
                    .child(medium(module.name().to_owned(), 12.5, FAINT)),
            ],
        }
    }

    fn tile(&self, stack: &Stack, rect: Rect, cx: &mut Context<Self>) -> Div {
        let native = match stack.modules.get(stack.active) {
            Some(Module::Chat) => SINGLE_HEADER,
            _ => STACK_HEADER,
        };
        let header = if stack.modules.len() > 1 {
            STACK_HEADER
        } else {
            SINGLE_HEADER
        };
        let shift = header - native;
        let body = stack
            .modules
            .get(stack.active)
            .map(|module| self.body(module, rect.w, rect.h - shift))
            .unwrap_or_default();
        place(div(), rect)
            .rounded(px(12.0))
            .overflow_hidden()
            .bg(SHELL)
            .shadow(vec![ring(ink(0.06))])
            .child(at(
                div().w(px(rect.w)).h(px(rect.h - shift)).children(body),
                0.0,
                shift,
            ))
            .child(self.header(stack, rect.w, cx))
    }

    fn divider(&self, divider: Divider, cx: &mut Context<Self>) -> Div {
        let rect = divider.rect;
        let (line, cursor) = match divider.axis {
            tiling::Axis::Row => (
                at(div(), (rect.w - DIVIDER_LINE) / 2.0, 0.0)
                    .w(px(DIVIDER_LINE))
                    .h(px(rect.h)),
                gpui::CursorStyle::ResizeLeftRight,
            ),
            tiling::Axis::Column => (
                at(div(), 0.0, (rect.h - DIVIDER_LINE) / 2.0)
                    .w(px(rect.w))
                    .h(px(DIVIDER_LINE)),
                gpui::CursorStyle::ResizeUpDown,
            ),
        };
        place(div(), rect)
            .group("divider")
            .cursor(cursor)
            .child(line.group_hover("divider", |line| line.bg(ink(0.3))))
            .on_mouse_down(
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

    fn overlay(&self, area: Rect) -> Vec<Div> {
        let Gesture::Dragging {
            grab,
            at: (x, y),
            target,
        } = self.gesture
        else {
            return Vec::new();
        };
        let name = self
            .workspace
            .stack(grab.tile)
            .and_then(|stack| stack.modules.get(grab.module))
            .map_or(String::new(), |module| module.name().to_owned());
        let mut out: Vec<Div> = target
            .and_then(|target| tiling::preview(self.workspace.tree(), area, target))
            .map(|rect| {
                place(div(), rect)
                    .rounded(px(12.0))
                    .bg(ink(0.07))
                    .shadow(vec![ring(ink(0.35))])
            })
            .into_iter()
            .collect();
        out.push(at(
            div()
                .h(px(26.0))
                .px(px(10.0))
                .pt(px(4.5))
                .rounded(px(8.0))
                .bg(rgb(34, 33, 41, 0.96))
                .shadow(vec![ring(ink(0.14))])
                .child(medium(name, 12.5, STRONG)),
            x + 10.0,
            y + 10.0,
        ));
        out
    }

    fn menu(&self, cx: &mut Context<Self>) -> Div {
        let (x, y, _, h) = chrome::WORKSPACE_TAB;
        let item =
            |label: &'static str, reset: fn(&Workspace) -> Workspace, cx: &mut Context<Self>| {
                div()
                    .h(px(MENU_ROW))
                    .px(px(10.0))
                    .pt(px(5.5))
                    .rounded(px(7.0))
                    .cursor_pointer()
                    .hover(|row| row.bg(ink(0.07)))
                    .child(text(label, 12.5, STRONG))
                    .on_mouse_down(
                        MouseButton::Left,
                        cx.listener(move |work, _: &MouseDownEvent, _, cx| {
                            work.menu = false;
                            let next = reset(&work.workspace);
                            work.commit(Ok(next), cx);
                        }),
                    )
            };
        at(
            div()
                .w(px(MENU_WIDTH))
                .p(px(4.0))
                .rounded(px(10.0))
                .bg(rgb(34, 33, 41, 0.98))
                .shadow(vec![ring(ink(0.12))])
                .child(item("Reset layout", Workspace::even, cx))
                .child(item("Reset to preset", Workspace::reset, cx)),
            x,
            y + h + 4.0,
        )
    }
}

impl Render for Work {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let (width, height) = board_size(window);
        let area = area(width, height);
        let tiles: Vec<Div> = self
            .workspace
            .tiles(area)
            .into_iter()
            .map(|(stack, rect)| self.tile(stack, rect, cx))
            .collect();
        let dividers: Vec<Div> = self
            .workspace
            .dividers(area)
            .into_iter()
            .map(|divider| self.divider(divider, cx))
            .collect();
        let (mx, my, mw, mh) = chrome::TAB_MENU;
        let toggle = at(div().w(px(mw)).h(px(mh)), mx, my).on_mouse_down(
            MouseButton::Left,
            cx.listener(|work, _: &MouseDownEvent, _, cx| {
                work.menu = !work.menu;
                cx.notify();
            }),
        );
        let menu = self.menu.then(|| self.menu(cx));
        div()
            .size_full()
            .relative()
            .track_focus(&self.focus)
            .on_key_down(cx.listener(Self::key))
            .font_family(SANS)
            .child(img(self.backdrop.clone()).absolute().size_full())
            .on_mouse_move(cx.listener(Self::pointer_moved))
            .on_mouse_up(
                MouseButton::Left,
                cx.listener(|work, _: &MouseUpEvent, window, cx| work.released(window, cx)),
            )
            .child(
                at(div(), INSET_X, INSET_Y)
                    .w(px(width))
                    .h(px(height))
                    .rounded(px(8.0))
                    .shadow(vec![ring(ink(0.1))])
                    .children(chrome::title_bar(width))
                    .children(chrome::sidebar())
                    .children(chrome::status_bar(width, height))
                    .children(tiles)
                    .children(dividers)
                    .child(toggle)
                    .children(self.overlay(area))
                    .children(menu),
            )
    }
}
