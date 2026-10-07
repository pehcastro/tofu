use std::cmp::Ordering;
use std::collections::HashMap;
use std::time::Instant;

use desk_motion::{Glide, GlideKind, reduced_motion};
use desk_tiling::{
    Action, Corner, DRAG_THRESHOLD, Divider, Grab, HEADER_ZONE, Mods, Module, Preset, Preview,
    Rect, Refusal, SHORTCUTS, Spawned, Stack, Target, TileId, WORKSPACE_EDGE, Workspace, Zone, aim,
    readout,
};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{Header, header_action, inner_card, shell};
use desk_ui::components::chip::kbd;
use desk_ui::components::empty::{EmptyAction, EmptyHint, empty_state};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::overlay::menu_surface;
use desk_ui::components::paint::{ink, tint};
use desk_ui::components::size::{
    FONT_SMALL, FONT_TITLE, HEADER, HEADER_TABBED, RADIUS_CHIP, RADIUS_ROW, T1,
};
use desk_ui::components::tabs::{Tab, TabEvent, TabMark, connected_tabs};
use desk_ui::icon::Icon;
use desk_ui::theme::{ColorToken, NumberToken, Theme};
use gpui::{
    Bounds, Context, CursorStyle, Deferred, Div, FontWeight, KeyDownEvent, MouseButton,
    MouseDownEvent, MouseMoveEvent, MouseUpEvent, Pixels, Point, SharedString, Stateful, Window,
    canvas, deferred, div, point, prelude::*, px, size,
};

use super::Book;
use super::kit::label;

const PRESETS: [(&str, Preset); 3] = [
    ("work", Preset::Work),
    ("editor", Preset::Editor),
    ("data", Preset::Data),
];
const DATA_PLUGIN: &str = "data-studio";
const FIRST_TOP: f32 = 160.0;
const BOTTOM_PAD: f32 = 16.0;
const SIDE_WIDTH: f32 = 290.0;
const SIDE_FROM: f32 = 960.0;
const MENU_WIDTH: f32 = 200.0;
const MENU_TOP: f32 = 36.0;
const DIVIDER_LINE: f32 = 2.0;
const BADGE_OFFSET: f32 = 12.0;
const SHRINK_SLACK: f32 = 0.5;
const PREVIEW_OPACITY: f32 = 0.3;
const EMPTY_SPAWNS: [Module; 4] = [
    Module::Chat,
    Module::Editor,
    Module::Terminal,
    Module::Shells,
];

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
        preview: Option<Preview>,
    },
    Resizing {
        divider: Divider,
        base: Workspace,
    },
    Cornering {
        corner: Corner,
        from: (f32, f32),
        base: Workspace,
    },
}

#[derive(Clone, Copy, PartialEq)]
enum Menu {
    Closed,
    Spawn,
    Into(TileId),
}

pub(super) struct TilingPage {
    workspaces: Vec<Workspace>,
    active: usize,
    renaming: Option<String>,
    gesture: Gesture,
    menu: Menu,
    refusal: Option<Refusal>,
    origin: (f32, f32),
    width: f32,
    height: f32,
    glides: HashMap<TileId, (Rect, Glide)>,
    structural: bool,
    hovered: Option<TileId>,
    slot: Option<(TileId, usize)>,
}

fn glyph_of(module: &Module) -> Glyph {
    match module {
        Module::Chat | Module::Browser => Glyph::Chat,
        Module::SubAgents => Glyph::Agents,
        Module::FileEdits | Module::Editor => Glyph::File,
        Module::Shells | Module::Terminal => Glyph::Terminal,
        Module::SourceControl => Glyph::Trace,
        Module::Plugin(_) => Glyph::Attach,
    }
}

fn module_tab(module: &Module, mark: TabMark) -> Tab {
    Tab {
        label: module.name().to_owned().into(),
        icon: Some(glyph_of(module)),
        count: None,
        mark,
    }
}

fn keys_of(action: Action) -> &'static str {
    SHORTCUTS
        .iter()
        .find(|shortcut| shortcut.action == action)
        .map_or("", |shortcut| shortcut.keys)
}

fn spawnable() -> Vec<Module> {
    let mut modules = Module::BUILT_IN.to_vec();
    modules.push(Module::Plugin(DATA_PLUGIN.to_owned()));
    modules
}

fn bounds(rect: Rect) -> Bounds<Pixels> {
    Bounds::new(point(px(rect.x), px(rect.y)), size(px(rect.w), px(rect.h)))
}

fn placed(element: Div, rect: Rect) -> Div {
    element
        .absolute()
        .left(px(rect.x))
        .top(px(rect.y))
        .w(px(rect.w.max(0.0)))
        .h(px(rect.h.max(0.0)))
}

fn clipped(shown: Rect, solved: Rect) -> Rect {
    let x = shown.x.max(solved.x);
    let y = shown.y.max(solved.y);
    Rect {
        x,
        y,
        w: ((shown.x + shown.w).min(solved.x + solved.w) - x).max(0.0),
        h: ((shown.y + shown.h).min(solved.y + solved.h) - y).max(0.0),
    }
}

impl TilingPage {
    pub(super) fn new(_: &mut Context<Book>) -> Self {
        TilingPage {
            workspaces: PRESETS
                .iter()
                .map(|(name, preset)| Workspace::new(*name, *preset))
                .collect(),
            active: 0,
            renaming: None,
            gesture: Gesture::Idle,
            menu: Menu::Closed,
            refusal: None,
            origin: (0.0, FIRST_TOP),
            width: 0.0,
            height: 0.0,
            glides: HashMap::new(),
            structural: false,
            hovered: None,
            slot: None,
        }
    }

    fn area(&self) -> Rect {
        Rect {
            x: 0.0,
            y: 0.0,
            w: self.width,
            h: self.height,
        }
    }

    fn current(&self) -> Option<&Workspace> {
        self.workspaces.get(self.active)
    }

    fn apply(
        &mut self,
        structural: bool,
        change: impl FnOnce(&Workspace, Rect) -> Result<Workspace, Refusal>,
    ) {
        let area = self.area();
        let Some(workspace) = self.workspaces.get_mut(self.active) else {
            return;
        };
        match change(workspace, area) {
            Ok(next) => {
                *workspace = next;
                self.refusal = None;
                self.structural |= structural;
            }
            Err(refusal) => self.refusal = Some(refusal),
        }
    }

    fn spawn(&mut self, module: Module) {
        let Some(workspace) = self.current() else {
            return;
        };
        match workspace.spawn(module, self.area()) {
            Ok(Spawned::Here(next)) => self.apply(true, |_, _| Ok(next)),
            Ok(Spawned::NewTab(fresh)) => {
                self.workspaces.push(fresh);
                self.switch(self.workspaces.len() - 1);
                self.refusal = None;
            }
            Err(refusal) => self.refusal = Some(refusal),
        }
    }

    fn local(&self, position: Point<Pixels>) -> (f32, f32) {
        (
            f32::from(position.x) - self.origin.0,
            f32::from(position.y) - self.origin.1,
        )
    }

    fn switch(&mut self, index: usize) {
        if index != self.active {
            self.show(index);
        }
    }

    fn show(&mut self, index: usize) {
        self.active = index;
        self.glides.clear();
        self.gesture = Gesture::Idle;
        self.renaming = None;
    }

    fn add(&mut self) {
        let name = format!("workspace {}", self.workspaces.len() + 1);
        self.workspaces.push(Workspace::new(name, Preset::Empty));
        self.switch(self.workspaces.len() - 1);
    }

    fn renamed(&mut self, key: &str, typed: Option<&str>) {
        let Some(buffer) = self.renaming.as_mut() else {
            return;
        };
        match (key, typed) {
            ("enter", _) => {
                let name = buffer.trim().to_owned();
                if let Some(workspace) = self.workspaces.get_mut(self.active)
                    && !name.is_empty()
                {
                    workspace.name = name;
                }
                self.renaming = None;
            }
            ("escape", _) => self.renaming = None,
            ("backspace", _) => {
                buffer.pop();
            }
            (_, Some(text)) => buffer.extend(text.chars().filter(|c| !c.is_control())),
            (_, None) => {}
        }
    }

    pub(super) fn key(&mut self, event: &KeyDownEvent) -> bool {
        let stroke = &event.keystroke;
        let key = stroke.key.as_str();
        let held = stroke.modifiers;
        if self.renaming.is_some() {
            self.renamed(key, stroke.key_char.as_deref());
            return true;
        }
        let mods = Mods {
            ctrl: held.control,
            alt: held.alt,
            shift: held.shift,
        };
        desk_tiling::action(mods, key).is_some_and(|action| self.run(action, key))
    }

    fn aimed(&self, grab: &Grab, (x, y): (f32, f32), held: Option<Target>) -> Option<Target> {
        let workspace = self.current()?;
        let area = self.area();
        let found = aim(workspace.tree(), area, x, y, held)?;
        let Target::Tile(tile, Zone::Stack { .. }) = found else {
            return Some(found);
        };
        let in_header = workspace
            .tiles(area)
            .into_iter()
            .find(|(stack, _)| stack.id == tile)
            .is_some_and(|(_, rect)| y < rect.y + HEADER_ZONE);
        let at = match (self.slot, grab) {
            (Some((on, shown)), Grab::Placed { tile: from, index })
                if on == tile && in_header && *from == tile && *index <= shown =>
            {
                shown + 1
            }
            (Some((on, shown)), _) if on == tile && in_header => shown,
            (Some(_) | None, _) => workspace.stack(tile)?.modules.len(),
        };
        Some(Target::Tile(tile, Zone::Stack { at }))
    }

    fn landed(&self, tile: TileId) -> Option<Stack> {
        let Gesture::Dragging {
            grab,
            target: Some(target),
            preview: Some(Preview::Stack { tile: on, .. }),
            ..
        } = &self.gesture
        else {
            return None;
        };
        if *on != tile {
            return None;
        }
        let workspace = self.current()?;
        let landed = workspace.dropped(grab, *target, self.area()).ok()?;
        landed.stack(tile).cloned()
    }

    fn picked(&mut self, module: Module) {
        let menu = std::mem::replace(&mut self.menu, Menu::Closed);
        self.gesture = Gesture::Idle;
        match menu {
            Menu::Into(tile) => self.apply(true, |workspace, area| {
                workspace.open_at(
                    module,
                    Target::Tile(tile, Zone::Stack { at: usize::MAX }),
                    area,
                )
            }),
            Menu::Spawn | Menu::Closed => self.spawn(module),
        }
    }

    fn pointer_moved(&mut self, event: &MouseMoveEvent, cx: &mut Context<Book>) {
        let (x, y) = self.local(event.position);
        self.gesture = match std::mem::replace(&mut self.gesture, Gesture::Idle) {
            Gesture::Idle => return,
            Gesture::Pressed { grab, from } if (x - from.0).hypot(y - from.1) < DRAG_THRESHOLD => {
                Gesture::Pressed { grab, from }
            }
            Gesture::Pressed { grab, .. } => {
                self.menu = Menu::Closed;
                self.dragging(grab, None, x, y)
            }
            Gesture::Dragging { grab, target, .. } => self.dragging(grab, target, x, y),
            Gesture::Resizing { divider, base } => {
                self.apply(false, |_, _| base.resize(&divider, x, y));
                Gesture::Resizing { divider, base }
            }
            Gesture::Cornering { corner, from, base } => {
                self.apply(false, |_, _| {
                    base.resize_corner(&corner, x - from.0, y - from.1)
                });
                Gesture::Cornering { corner, from, base }
            }
        };
        cx.notify();
    }

    fn dragging(&self, grab: Grab, held: Option<Target>, x: f32, y: f32) -> Gesture {
        let target = self.aimed(&grab, (x, y), held);
        let preview = target
            .zip(self.current())
            .and_then(|(target, workspace)| workspace.preview(&grab, target, self.area()));
        Gesture::Dragging {
            grab,
            at: (x, y),
            target,
            preview,
        }
    }

    fn aimed_tile(&self) -> Option<TileId> {
        self.hovered.or(self.current()?.focus())
    }

    fn send_to_new(&mut self) {
        let (Some(workspace), Some(tile)) = (self.current(), self.aimed_tile()) else {
            return;
        };
        match workspace.send_to_new(tile, self.area()) {
            Ok((left, fresh)) => {
                self.apply(true, |_, _| Ok(left));
                self.workspaces.push(fresh);
                self.switch(self.workspaces.len() - 1);
            }
            Err(refusal) => self.refusal = Some(refusal),
        }
    }

    fn run(&mut self, action: Action, key: &str) -> bool {
        match action {
            Action::NewWorkspace => self.add(),
            Action::GoToWorkspace => {
                let Some(index) = key
                    .parse::<usize>()
                    .ok()
                    .and_then(|digit| digit.checked_sub(1))
                    .filter(|index| *index < self.workspaces.len())
                else {
                    return false;
                };
                self.switch(index);
            }
            Action::SendToNewWorkspace => self.send_to_new(),
            Action::Zoom => {
                let aimed = self.aimed_tile();
                self.apply(true, |workspace, _| match (workspace.zoomed(), aimed) {
                    (None, Some(tile)) => workspace.focus_tile(tile)?.zoom(),
                    (Some(_), _) | (None, None) => workspace.zoom(),
                });
            }
            Action::Close => {
                let Some(tile) = self.aimed_tile() else {
                    return false;
                };
                self.apply(true, |workspace, area| workspace.close_tile(tile, area));
            }
            Action::Even => self.apply(true, |workspace, _| Ok(workspace.even())),
            Action::Reset => self.apply(true, |workspace, _| Ok(workspace.reset())),
            Action::Lock => self.toggle_lock(),
            Action::Undo => self.apply(true, |workspace, _| workspace.undo()),
            Action::Cancel => return self.cancel(),
            Action::CloseTab => {
                let Some(tile) = self.aimed_tile() else {
                    return false;
                };
                self.apply(true, |workspace, area| {
                    workspace.focus_tile(tile)?.close_tab(area)
                });
            }
            Action::Split => {
                let Some(tile) = self.aimed_tile() else {
                    return false;
                };
                self.apply(true, |workspace, area| {
                    workspace.focus_tile(tile)?.split(area)
                });
            }
            Action::ReopenTab => self.apply(true, |workspace, area| workspace.reopen(area)),
            Action::NextTab => self.apply(false, |workspace, _| workspace.step_tab(true)),
            Action::PrevTab => self.apply(false, |workspace, _| workspace.step_tab(false)),
        }
        true
    }

    fn toggle_lock(&mut self) {
        if let Some(workspace) = self.workspaces.get_mut(self.active) {
            workspace.locked = !workspace.locked;
        }
    }

    fn cancel(&mut self) -> bool {
        if matches!(
            self.gesture,
            Gesture::Dragging { .. } | Gesture::Pressed { .. }
        ) {
            self.gesture = Gesture::Idle;
            self.slot = None;
        } else if self.menu != Menu::Closed {
            self.menu = Menu::Closed;
        } else if self
            .current()
            .is_some_and(|workspace| workspace.zoomed().is_some())
        {
            self.apply(true, |workspace, _| workspace.zoom());
        } else {
            return false;
        }
        true
    }

    fn released(&mut self, cx: &mut Context<Book>) {
        match std::mem::replace(&mut self.gesture, Gesture::Idle) {
            Gesture::Idle | Gesture::Resizing { .. } | Gesture::Cornering { .. } => {}
            Gesture::Pressed {
                grab: Grab::Placed { tile, index },
                ..
            } => {
                self.apply(false, |workspace, _| workspace.activate(tile, index));
            }
            Gesture::Pressed {
                grab: Grab::New(module),
                ..
            } => self.picked(module),
            Gesture::Dragging { target: None, .. } => {}
            Gesture::Dragging {
                grab,
                target: Some(target),
                ..
            } => {
                self.apply(true, |workspace, area| {
                    workspace.dropped(&grab, target, area)
                });
            }
        }
        self.slot = None;
        cx.notify();
    }

    fn press(&mut self, grab: Grab, at: Point<Pixels>, cx: &mut Context<Book>) {
        self.gesture = Gesture::Pressed {
            grab,
            from: self.local(at),
        };
        cx.notify();
    }

    fn measure(&mut self, bounds: Bounds<Pixels>) -> bool {
        let origin = (f32::from(bounds.origin.x), f32::from(bounds.origin.y));
        let width = f32::from(bounds.size.width);
        let moved = origin != self.origin || width != self.width;
        self.origin = origin;
        self.width = width;
        moved
    }

    fn glide(
        &mut self,
        tiles: &[(TileId, Rect)],
        reduced: bool,
        now: Instant,
    ) -> (Vec<Rect>, bool) {
        let structural = std::mem::take(&mut self.structural);
        self.glides
            .retain(|id, _| tiles.iter().any(|(tile, _)| tile == id));
        let fresh = |rect: Rect| {
            let mut glide = Glide::new(GlideKind::Spring);
            glide.retarget(bounds(rect), now);
            glide
        };
        let mut moving = false;
        let shown = tiles
            .iter()
            .map(|(id, rect)| {
                let (last, glide) = self
                    .glides
                    .entry(*id)
                    .or_insert_with(|| (*rect, fresh(*rect)));
                if last != rect {
                    if structural {
                        glide.retarget(bounds(*rect), now);
                    } else {
                        *glide = fresh(*rect);
                    }
                    *last = *rect;
                }
                glide.set_reduced(reduced);
                let sampled = glide.sample(now).map(|(at, _)| Rect {
                    x: f32::from(at.origin.x),
                    y: f32::from(at.origin.y),
                    w: f32::from(at.size.width),
                    h: f32::from(at.size.height),
                });
                moving |= glide.moving();
                sampled.unwrap_or(*rect)
            })
            .collect();
        (shown, moving)
    }

    fn close_workspace(&mut self, index: usize) {
        if index >= self.workspaces.len() {
            return;
        }
        self.workspaces.remove(index);
        if self.workspaces.is_empty() {
            self.workspaces
                .push(Workspace::new("workspace 1", Preset::Empty));
        }
        match index.cmp(&self.active) {
            Ordering::Less => self.active -= 1,
            Ordering::Equal => self.show(index.min(self.workspaces.len() - 1)),
            Ordering::Greater => {}
        }
    }

    fn tabs(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let tabs: Vec<Tab> = self
            .workspaces
            .iter()
            .enumerate()
            .map(|(index, workspace)| {
                let name = match (&self.renaming, index == self.active) {
                    (Some(buffer), true) => format!("{buffer}|"),
                    _ => workspace.name.clone(),
                };
                let zoomed = workspace.zoomed().map(|_| " · zoomed");
                Tab {
                    label: format!("{name}{}", zoomed.unwrap_or_default()).into(),
                    icon: None,
                    count: None,
                    mark: TabMark::Close,
                }
            })
            .collect();
        let strip = connected_tabs(
            "tiling-tabs",
            &tabs,
            self.active,
            usize::MAX,
            theme,
            cx.listener(|book, event: &TabEvent, _, cx| {
                let page = &mut book.tiling;
                match *event {
                    TabEvent::Select(index) => page.switch(index),
                    TabEvent::Close(index) => page.close_workspace(index),
                    TabEvent::New => page.add(),
                }
                cx.notify();
            }),
        );
        let add = header_action("tiling-add", Icon::Plus, "New workspace", theme).on_click(
            cx.listener(|book, _, _, cx| {
                book.tiling.add();
                cx.notify();
            }),
        );
        div()
            .flex()
            .items_end()
            .gap_1()
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .on_mouse_down(
                        MouseButton::Left,
                        cx.listener(|book, event: &MouseDownEvent, _, cx| {
                            let page = &mut book.tiling;
                            if event.click_count >= 2 {
                                page.renaming =
                                    page.current().map(|workspace| workspace.name.clone());
                                cx.notify();
                            }
                        }),
                    )
                    .child(strip),
            )
            .child(add)
    }

    fn toolbar(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let locked = self.current().is_some_and(|workspace| workspace.locked);
        let action =
            |id: &'static str, title: &'static str, action: Action, cx: &mut Context<Book>| {
                button(id, title, None, ButtonKind::Plain, theme)
                    .child(kbd(keys_of(action), theme))
                    .on_click(cx.listener(move |book, _, _, cx| {
                        book.tiling.run(action, "");
                        cx.notify();
                    }))
            };
        let menu = (self.menu == Menu::Spawn).then(|| {
            self.spawn_menu(
                (0.0, MENU_TOP),
                "click splits the largest tile; drag places it",
                theme,
                cx,
            )
        });
        div()
            .relative()
            .flex()
            .items_center()
            .gap_2()
            .child(
                button("tiling-spawn-open", "Spawn", None, ButtonKind::Plain, theme).on_click(
                    cx.listener(|book, _, _, cx| {
                        book.tiling.menu = match book.tiling.menu {
                            Menu::Spawn => Menu::Closed,
                            Menu::Closed | Menu::Into(_) => Menu::Spawn,
                        };
                        cx.notify();
                    }),
                ),
            )
            .child(action("tiling-even", "Even", Action::Even, cx))
            .child(action("tiling-zoom", "Zoom", Action::Zoom, cx))
            .child(action("tiling-undo", "Undo", Action::Undo, cx))
            .child(action("tiling-reset", "Reset", Action::Reset, cx))
            .child(action(
                "tiling-lock",
                if locked { "Unlock" } else { "Lock" },
                Action::Lock,
                cx,
            ))
            .child(action(
                "tiling-send",
                "New workspace from tile",
                Action::SendToNewWorkspace,
                cx,
            ))
            .children(menu)
    }

    fn spawn_menu(
        &self,
        (left, top): (f32, f32),
        hint: &'static str,
        theme: &Theme,
        cx: &mut Context<Book>,
    ) -> Deferred {
        let rows = spawnable().into_iter().enumerate().map(|(index, module)| {
            let name = module.name().to_owned();
            div()
                .id(("tiling-spawn", index))
                .px_2()
                .py_1()
                .rounded(px(RADIUS_CHIP))
                .cursor_pointer()
                .hover(|row| row.bg(theme.color(ColorToken::StateHover)))
                .child(name)
                .on_mouse_down(MouseButton::Left, {
                    let module = module.clone();
                    cx.listener(move |book, event: &MouseDownEvent, _, cx| {
                        book.tiling
                            .press(Grab::New(module.clone()), event.position, cx);
                    })
                })
                .on_click(cx.listener(move |book, _, _, cx| {
                    book.tiling.picked(module.clone());
                    cx.notify();
                }))
        });
        deferred(
            menu_surface(theme)
                .absolute()
                .top(px(top))
                .left(px(left))
                .w(px(MENU_WIDTH))
                .p_1()
                .rounded(px(RADIUS_ROW))
                .child(label(hint, theme))
                .children(rows.collect::<Vec<_>>()),
        )
        .priority(1)
    }

    fn header(&self, stack: &Stack, theme: &Theme, cx: &mut Context<Book>) -> Header {
        let tile = stack.id;
        let shown = self.landed(tile).unwrap_or_else(|| stack.clone());
        if let [module] = shown.modules.as_slice() {
            return Header::Title(
                Some(glyph_of(module)),
                module.name().to_owned().into(),
                None,
            );
        }
        let mark = match self.current().is_some_and(|workspace| workspace.locked) {
            true => TabMark::Locked,
            false => TabMark::Close,
        };
        let tabs: Vec<Tab> = shown
            .modules
            .iter()
            .map(|module| module_tab(module, mark))
            .collect();
        let (pressing, pointing) = (cx.entity().downgrade(), cx.entity().downgrade());
        let strip = connected_tabs(
            SharedString::from(format!("tiling-tabs-{}", tile.0)),
            &tabs,
            shown.active,
            usize::MAX,
            theme,
            cx.listener(move |book, event: &TabEvent, _, cx| {
                let page = &mut book.tiling;
                match *event {
                    TabEvent::Select(index) => {
                        page.apply(false, |workspace, _| workspace.activate(tile, index))
                    }
                    TabEvent::Close(index) => page.apply(true, |workspace, area| {
                        workspace.close_module(tile, index, area)
                    }),
                    TabEvent::New => page.menu = Menu::Into(tile),
                }
                cx.notify();
            }),
        )
        .on_press(move |index, at, _, cx| {
            if let Some(book) = pressing.upgrade() {
                book.update(cx, |book, cx| {
                    book.tiling.press(Grab::Placed { tile, index }, at, cx);
                });
            }
        })
        .on_point(move |index, _, cx| {
            if let Some(book) = pointing.upgrade() {
                book.update(cx, |book, _| {
                    if matches!(book.tiling.gesture, Gesture::Dragging { .. }) {
                        book.tiling.slot = Some((tile, index));
                    }
                });
            }
        });
        let plus = header_action(
            SharedString::from(format!("tiling-plus-{}", tile.0)),
            Icon::Plus,
            "Add a tab",
            theme,
        )
        .on_click(cx.listener(move |book, _, _, cx| {
            book.tiling.menu = Menu::Into(tile);
            cx.notify();
        }));
        Header::Tabs(strip.into_any_element(), Some(plus.into_any_element()))
    }

    fn card(
        &self,
        stack: &Stack,
        (drawn, solved): (Rect, Rect),
        theme: &Theme,
        cx: &mut Context<Book>,
    ) -> Stateful<Div> {
        let tile = stack.id;
        let active = stack.active;
        let single = stack.modules.len() == 1;
        let name = stack
            .modules
            .get(stack.active)
            .map_or("", Module::name)
            .to_owned();
        let body = div()
            .id(SharedString::from(format!("tiling-body-{}", tile.0)))
            .flex_1()
            .min_h_0()
            .flex()
            .flex_col()
            .items_center()
            .justify_center()
            .gap_1()
            .overflow_hidden()
            .child(
                div()
                    .text_size(px(FONT_TITLE))
                    .font_weight(FontWeight::SEMIBOLD)
                    .text_color(ink(theme, T1))
                    .child(name),
            )
            .child(label(format!("tile {}", tile.0), theme))
            .child(label(
                format!("{:.0} × {:.0} px", solved.w, solved.h),
                theme,
            ));
        placed(shell(self.header(stack, theme, cx), theme), drawn)
            .flex_none()
            .id(SharedString::from(format!("tiling-card-{}", tile.0)))
            .on_hover(cx.listener(move |book, hovered: &bool, _, cx| {
                let page = &mut book.tiling;
                if *hovered {
                    page.hovered = Some(tile);
                } else if page.hovered == Some(tile) {
                    page.hovered = None;
                }
                cx.notify();
            }))
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(move |book, event: &MouseDownEvent, _, cx| {
                    let page = &mut book.tiling;
                    if single && page.local(event.position).1 < drawn.y + HEADER {
                        page.press(Grab::Placed { tile, index: 0 }, event.position, cx);
                    }
                }),
            )
            .on_mouse_down(
                MouseButton::Right,
                cx.listener(move |book, event: &MouseDownEvent, _, cx| {
                    if event.modifiers.alt {
                        let grab = Grab::Placed {
                            tile,
                            index: active,
                        };
                        book.tiling.press(grab, event.position, cx);
                        cx.stop_propagation();
                    }
                }),
            )
            .child(inner_card(theme).child(body))
    }

    fn corner(&self, corner: Corner, index: usize, cx: &mut Context<Book>) -> Stateful<Div> {
        let cursor = if corner.falls_left() {
            CursorStyle::ResizeUpLeftDownRight
        } else {
            CursorStyle::ResizeUpRightDownLeft
        };
        placed(div(), corner.rect)
            .id(("tiling-corner", index))
            .cursor(cursor)
            .on_mouse_down(
                MouseButton::Right,
                cx.listener(move |book, event: &MouseDownEvent, _, cx| {
                    if !event.modifiers.alt {
                        return;
                    }
                    let page = &mut book.tiling;
                    let Some(base) = page.current().cloned() else {
                        return;
                    };
                    page.gesture = Gesture::Cornering {
                        corner: corner.clone(),
                        from: page.local(event.position),
                        base,
                    };
                    cx.stop_propagation();
                    cx.notify();
                }),
            )
    }

    fn divider(&self, divider: Divider, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let line = theme.color(ColorToken::Separator);
        let row = matches!(divider.axis, desk_tiling::Axis::Row);
        let rect = divider.rect;
        let mark = div()
            .absolute()
            .when(row, |mark| {
                mark.top_0()
                    .bottom_0()
                    .left(px((rect.w - DIVIDER_LINE) / 2.0))
                    .w(px(DIVIDER_LINE))
            })
            .when(!row, |mark| {
                mark.left_0()
                    .right_0()
                    .top(px((rect.h - DIVIDER_LINE) / 2.0))
                    .h(px(DIVIDER_LINE))
            })
            .group_hover("tiling-divider", move |mark| mark.bg(line));
        placed(div(), rect)
            .group("tiling-divider")
            .cursor(if row {
                CursorStyle::ResizeLeftRight
            } else {
                CursorStyle::ResizeUpDown
            })
            .child(mark)
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(move |book, event: &MouseDownEvent, _, cx| {
                    let page = &mut book.tiling;
                    if event.click_count >= 2 {
                        let divider = divider.clone();
                        page.apply(true, move |workspace, _| Ok(workspace.even_split(&divider)));
                    } else if let Some(base) = page.current().cloned() {
                        page.gesture = Gesture::Resizing {
                            divider: divider.clone(),
                            base,
                        };
                    }
                    cx.notify();
                }),
            )
    }

    fn grabbed_name(&self, grab: &Grab) -> String {
        match grab {
            Grab::New(module) => module.name().to_owned(),
            Grab::Placed { tile, index } => self
                .current()
                .and_then(|workspace| workspace.stack(*tile))
                .and_then(|stack| stack.modules.get(*index))
                .map_or(String::new(), |module| module.name().to_owned()),
        }
    }

    fn drag_overlay(&self, theme: &Theme) -> Vec<Div> {
        let Gesture::Dragging {
            grab,
            at: (x, y),
            preview,
            ..
        } = &self.gesture
        else {
            return Vec::new();
        };
        let radius = px(theme.number(NumberToken::CardsOuterRadius));
        let line = theme.color(ColorToken::Separator);
        let fill = tint(theme.color(ColorToken::StateHover), PREVIEW_OPACITY);
        let filled = |rect: Rect| {
            placed(div(), rect)
                .rounded(radius)
                .bg(fill)
                .border_1()
                .border_color(line)
        };
        let mut out: Vec<Div> = match preview {
            None => Vec::new(),
            Some(Preview::Stack { outline, .. }) => vec![
                placed(div(), *outline)
                    .rounded(radius)
                    .border_1()
                    .border_color(line),
            ],
            Some(Preview::Split(rect)) => vec![filled(*rect)],
            Some(Preview::Edge { strip, bar }) => {
                vec![filled(*strip), placed(div(), *bar).bg(line)]
            }
        };
        let name = self.grabbed_name(grab);
        out.push(
            menu_surface(theme)
                .absolute()
                .left(px(x + BADGE_OFFSET))
                .top(px(y + BADGE_OFFSET))
                .px_2()
                .py_1()
                .rounded(px(RADIUS_ROW))
                .child(name),
        );
        out
    }

    fn side(&self, theme: &Theme) -> Div {
        let workspace = self.current();
        let refusal = self.refusal.map_or("none".to_owned(), |refusal| {
            format!("{refusal:?}: {refusal}")
        });
        let lines = readout(workspace.and_then(Workspace::tree));
        div()
            .w(px(SIDE_WIDTH))
            .flex_none()
            .flex()
            .flex_col()
            .gap_2()
            .text_size(px(FONT_SMALL))
            .child(label("last refusal", theme))
            .child(
                div()
                    .text_color(theme.color(if self.refusal.is_some() {
                        ColorToken::StatusDanger
                    } else {
                        ColorToken::TextBase
                    }))
                    .child(refusal),
            )
            .child(label("tree", theme))
            .children(
                lines
                    .into_iter()
                    .map(|line| div().text_color(ink(theme, T1)).child(line))
                    .collect::<Vec<_>>(),
            )
            .child(label(
                "Alt + right-drag or a header drag moves a tile · Alt + right-drag on a corner resizes it",
                theme,
            ))
            .children(SHORTCUTS.iter().map(|shortcut| {
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .child(kbd(shortcut.keys, theme))
                    .child(label(shortcut.label, theme))
            }))
    }

    fn empty(&self, theme: &Theme, cx: &mut Context<Book>) -> Stateful<Div> {
        let elsewhere = |module: &Module| {
            self.workspaces
                .iter()
                .enumerate()
                .filter(|(index, _)| *index != self.active)
                .any(|(_, workspace)| {
                    workspace
                        .tiles(self.area())
                        .iter()
                        .any(|(stack, _)| stack.modules.contains(module))
                })
        };
        let mut modules = EMPTY_SPAWNS.to_vec();
        modules.sort_by_key(|module| elsewhere(module));
        let actions: Vec<EmptyAction> = modules
            .iter()
            .map(|module| EmptyAction {
                label: module.name().to_owned().into(),
                glyph: None,
                keys: None,
            })
            .collect();
        let hints: Vec<EmptyHint> = SHORTCUTS
            .iter()
            .map(|shortcut| EmptyHint {
                keys: shortcut.keys.into(),
                label: shortcut.label.into(),
            })
            .collect();
        let entity = cx.entity().downgrade();
        empty_state(
            "tiling-empty",
            "Empty workspace",
            Some("Spawn a module or drag one here.".into()),
            &actions,
            &hints,
            theme,
            move |at, _, cx| {
                let (Some(book), Some(module)) = (entity.upgrade(), modules.get(at).cloned())
                else {
                    return;
                };
                book.update(cx, |book, cx| {
                    book.tiling.spawn(module);
                    cx.notify();
                });
            },
        )
    }

    pub(super) fn render(
        &mut self,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Book>,
    ) -> Div {
        let viewport = window.viewport_size();
        self.height =
            (f32::from(viewport.height) - self.origin.1 - WORKSPACE_EDGE - BOTTOM_PAD).max(0.0);
        let area = self.area();
        let now = Instant::now();
        let reduced = reduced_motion(cx);
        let solved: Vec<(Stack, Rect)> = self
            .current()
            .map(|workspace| {
                workspace
                    .tiles(area)
                    .into_iter()
                    .map(|(stack, rect)| (stack.clone(), rect))
                    .collect()
            })
            .unwrap_or_default();
        let targets: Vec<(TileId, Rect)> = solved
            .iter()
            .map(|(stack, rect)| (stack.id, *rect))
            .collect();
        let (shown, gliding) = self.glide(&targets, reduced, now);
        if gliding {
            window.request_animation_frame();
        }
        let mut cards: Vec<(bool, Stateful<Div>)> = solved
            .iter()
            .zip(shown)
            .map(|((stack, rect), shown)| {
                let shrinking = shown.w > rect.w + SHRINK_SLACK || shown.h > rect.h + SHRINK_SLACK;
                let drawn = if shrinking {
                    clipped(shown, *rect)
                } else {
                    shown
                };
                let card = self.card(stack, (drawn, *rect), theme, cx);
                (shrinking, card)
            })
            .collect();
        cards.sort_by_key(|(shrinking, _)| !*shrinking);
        let dividers: Vec<Div> = self
            .current()
            .map(|workspace| workspace.dividers(area))
            .unwrap_or_default()
            .into_iter()
            .map(|divider| self.divider(divider, theme, cx))
            .collect();
        let corners: Vec<Stateful<Div>> = self
            .current()
            .filter(|_| window.modifiers().alt)
            .map(|workspace| workspace.corners(area))
            .unwrap_or_default()
            .into_iter()
            .enumerate()
            .map(|(index, corner)| self.corner(corner, index, cx))
            .collect();
        let entity = cx.entity().downgrade();
        let measure = canvas(
            move |bounds, _, cx| {
                if let Some(book) = entity.upgrade() {
                    book.update(cx, |book, cx| {
                        if book.tiling.measure(bounds) {
                            cx.notify();
                        }
                    });
                }
            },
            |_, _, _, _| {},
        )
        .absolute()
        .size_full();
        let wide = f32::from(viewport.width) >= SIDE_FROM + SIDE_WIDTH;
        let tile_menu = solved.iter().find_map(|(stack, rect)| {
            (self.menu == Menu::Into(stack.id)).then(|| {
                self.spawn_menu(
                    (rect.x + rect.w - MENU_WIDTH, rect.y + HEADER_TABBED),
                    "click adds a tab here; drag places it",
                    theme,
                    cx,
                )
            })
        });
        let board = div()
            .w_full()
            .relative()
            .h(px(self.height))
            .child(measure)
            .children(cards.into_iter().map(|(_, card)| card))
            .children(dividers)
            .children(corners)
            .children(self.drag_overlay(theme))
            .children(tile_menu)
            .when(solved.is_empty(), |board| {
                board.child(div().absolute().size_full().child(self.empty(theme, cx)))
            });
        let edge_band = div().flex_1().min_w_0().p(px(WORKSPACE_EDGE)).child(board);
        let cancel = |book: &mut Book, _: &MouseUpEvent, _: &mut Window, cx: &mut Context<Book>| {
            book.tiling.gesture = Gesture::Idle;
            cx.notify();
        };
        let release =
            |book: &mut Book, _: &MouseUpEvent, _: &mut Window, cx: &mut Context<Book>| {
                book.tiling.released(cx);
            };
        div()
            .flex()
            .flex_col()
            .gap_2()
            .pt_2()
            .on_mouse_move(cx.listener(|book, event: &MouseMoveEvent, _, cx| {
                book.tiling.pointer_moved(event, cx);
            }))
            .on_mouse_up(MouseButton::Left, cx.listener(release))
            .on_mouse_up(MouseButton::Right, cx.listener(release))
            .on_mouse_up_out(MouseButton::Left, cx.listener(cancel))
            .on_mouse_up_out(MouseButton::Right, cx.listener(cancel))
            .child(self.tabs(theme, cx))
            .child(self.toolbar(theme, cx))
            .child(
                div()
                    .flex()
                    .gap_3()
                    .child(edge_band)
                    .when(wide, |row| row.child(self.side(theme))),
            )
    }
}
