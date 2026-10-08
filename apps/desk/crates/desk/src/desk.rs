use crate::modules::chat::Find;
#[cfg(feature = "screen-work")]
use crate::modules::chat::Listed;
#[cfg(feature = "screen-work")]
use crate::project::{self, Head};
#[cfg(feature = "screen-work")]
use crate::screens::work::{self, Work};
use crate::status_bar::status_bar;
use crate::title_bar::title_bar;
use desk_core::control::{Control, TELL_BADGE};
use desk_core::limits::TOAST_LIFETIME;
#[cfg(feature = "screen-work")]
use desk_tiling::WORKSPACE_EDGE;
use desk_tiling::{Key, SHORTCUTS};
use desk_ui::components::card::{inner_card, outer_card};
use desk_ui::components::overlay::toast;
use desk_ui::components::paint::{ink, ring};
use desk_ui::components::palette::{Palette, PaletteItem};
#[cfg(feature = "screen-work")]
use desk_ui::components::sheet::Sheet;
use desk_ui::components::sidebar::{Project, SIDEBAR_COLUMN, Sidebar, SidebarPick};
#[cfg(feature = "screen-work")]
use desk_ui::components::tabs::{Tab, TabMark};
use desk_ui::live::ActiveTheme;
#[cfg(feature = "screen-work")]
use desk_ui::metrics::STATUS_BAR_HEIGHT;
use desk_ui::metrics::{
    TEXT, TOAST_BOTTOM, WINDOW_HEIGHT, WINDOW_MIN_HEIGHT, WINDOW_MIN_WIDTH, WINDOW_WIDTH,
};
use desk_ui::theme::{ColorToken, Theme};
#[cfg(feature = "screen-work")]
use gpui::Focusable;
use gpui::{
    AnyElement, AnyView, App, ClickEvent, Context, Entity, FocusHandle, IntoElement, KeyDownEvent,
    Pixels, Render, SharedString, Size, Task, TitlebarOptions, Window, WindowBounds, WindowOptions,
    div, prelude::*, px, size,
};
#[cfg(feature = "screen-work")]
use std::path::PathBuf;

pub const WINDOW_TITLE: &str = "Tofu Desk";
pub const BOARD_VIEWPORT_WIDTH: f32 = 1440.0;
pub const BOARD_VIEWPORT_HEIGHT: f32 = 900.0;
const GUTTER: f32 = 8.0;
const TILE_TOP: f32 = 2.0;
const TILE_BOTTOM: f32 = 8.0;
const WINDOW_RADIUS: f32 = 8.0;
const WINDOW_RING: f32 = 0.1;
const SETTINGS: &str = "settings";
const SCREEN_ID: &str = "screen.";
const LAYOUT_ID: &str = "layout.";
const OPEN_SETTINGS_ID: &str = "settings.open";
#[cfg(feature = "screen-work")]
const RECENT_ID: &str = "project.recent.";
#[cfg(feature = "screen-work")]
const OPEN_FOLDER_ID: &str = "project.open";
#[cfg(feature = "screen-work")]
const WORK_SCREEN: &str = "work";
#[cfg(feature = "screen-work")]
const LIVE_SCREENS: [&str; 1] = ["theme"];

pub type Open = fn(Option<&str>, &mut Window, &mut App) -> Result<AnyView, String>;

#[derive(Clone, Copy)]
pub struct Screen {
    pub name: &'static str,
    pub open: Open,
}

enum Body {
    #[cfg(feature = "screen-work")]
    Work(Entity<Work>),
    View(AnyView),
}

struct Shown {
    name: SharedString,
    body: Body,
}

pub struct Desk {
    sidebar_open: bool,
    account: Option<SharedString>,
    toast: Option<Toast>,
    screens: Vec<Screen>,
    shown: Shown,
    parked: Vec<Shown>,
    palette: Entity<Palette>,
    focus: FocusHandle,
    #[cfg(feature = "screen-work")]
    projects: Projects,
    #[cfg(feature = "screen-work")]
    tabbed: Vec<&'static str>,
}

#[cfg(feature = "screen-work")]
#[derive(Default)]
struct Projects {
    head: Option<Head>,
    recents: Vec<PathBuf>,
    menu: Option<Entity<Palette>>,
    switching: Option<PathBuf>,
    confirming: bool,
    _listed: Option<gpui::Subscription>,
    _head: Option<Task<()>>,
}

struct Toast {
    control: Control,
    _expiry: Task<gpui::Result<()>>,
}

pub fn desk_client() -> Size<Pixels> {
    size(px(WINDOW_WIDTH), px(WINDOW_HEIGHT))
}

pub fn board_client() -> Size<Pixels> {
    size(px(BOARD_VIEWPORT_WIDTH), px(BOARD_VIEWPORT_HEIGHT))
}

pub fn window_options(title: SharedString, client: Size<Pixels>, cx: &App) -> WindowOptions {
    let mut options = WindowOptions::new()
        .window_bounds(Some(WindowBounds::centered(client, cx)))
        .window_min_size(Some(size(px(WINDOW_MIN_WIDTH), px(WINDOW_MIN_HEIGHT))))
        .titlebar(Some(TitlebarOptions {
            title: Some(title),
            appears_transparent: true,
            traffic_light_position: None,
        }));
    #[cfg(target_os = "windows")]
    {
        options.windows_window_background = gpui::WindowsWindowBackground::Blurred;
    }
    #[cfg(target_os = "macos")]
    {
        options.macos_window_background = gpui::MacosWindowBackground::Blurred;
    }
    #[cfg(any(target_os = "linux", target_os = "freebsd"))]
    {
        options.linux_window_background = gpui::LinuxWindowBackground::Blurred;
    }
    options
}

impl Shown {
    fn new(name: SharedString, view: AnyView) -> Self {
        #[cfg(feature = "screen-work")]
        let view = match view.downcast::<Work>() {
            Ok(work) => {
                return Shown {
                    name,
                    body: Body::Work(work),
                };
            }
            Err(view) => view,
        };
        Shown {
            name,
            body: Body::View(view),
        }
    }
}

fn account_letter() -> Option<SharedString> {
    let name = std::env::var("USERNAME")
        .or_else(|_| std::env::var("USER"))
        .ok()?;
    let first = name.chars().next()?;
    Some(first.to_lowercase().collect::<String>().into())
}

fn commands(screens: &[Screen]) -> Vec<PaletteItem> {
    let screens = screens.iter().map(|screen| PaletteItem {
        id: format!("{SCREEN_ID}{}", screen.name).into(),
        label: screen.name.into(),
        group: "Screens".into(),
        keys: None,
    });
    let layout = SHORTCUTS
        .iter()
        .filter(|shortcut| shortcut.key != Key::Digit)
        .map(|shortcut| PaletteItem {
            id: format!("{LAYOUT_ID}{}", shortcut.label).into(),
            label: shortcut.label.into(),
            group: "Layout".into(),
            keys: Some(shortcut.keys.into()),
        });
    let settings = PaletteItem {
        id: OPEN_SETTINGS_ID.into(),
        label: "Open settings".into(),
        group: "Settings".into(),
        keys: Some("Ctrl ,".into()),
    };
    screens
        .chain(layout)
        .chain(std::iter::once(settings))
        .collect()
}

impl Desk {
    pub fn new(
        screens: Vec<Screen>,
        name: SharedString,
        view: AnyView,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Self {
        let palette = Palette::new(commands(&screens), window, cx);
        let picked = cx.listener(|desk, id: &SharedString, window, cx| desk.picked(id, window, cx));
        palette.update(cx, |palette, _| palette.on_pick(picked));
        #[cfg_attr(
            not(feature = "screen-work"),
            expect(unused_mut, reason = "only the work screen adopts a project")
        )]
        let mut desk = Desk {
            sidebar_open: true,
            account: account_letter(),
            toast: None,
            screens,
            shown: Shown::new(name, view),
            parked: Vec::new(),
            palette,
            focus: cx.focus_handle(),
            #[cfg(feature = "screen-work")]
            projects: Projects::default(),
            #[cfg(feature = "screen-work")]
            tabbed: Vec::new(),
        };
        #[cfg(feature = "screen-work")]
        if let Body::Work(work) = &desk.shown.body {
            let work = work.clone();
            match project::launch() {
                Ok(folder) => desk.adopt(folder, &work, cx),
                Err(error) => eprintln!("desk: {error}"),
            }
        }
        desk
    }

    pub fn open_palette(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        eprintln!("desk: palette open");
        self.palette
            .update(cx, |palette, cx| palette.open(window, cx));
    }

    fn show(&mut self, name: &str, window: &mut Window, cx: &mut Context<Self>) {
        if self.shown.name == name {
            return eprintln!("desk: screen {name} already shows");
        }
        let next = match self.parked.iter().position(|parked| parked.name == name) {
            Some(at) => self.parked.swap_remove(at),
            None => {
                let Some(screen) = self.screens.iter().find(|screen| screen.name == name) else {
                    return eprintln!("desk: screen {name} is not in this build");
                };
                match (screen.open)(None, window, cx) {
                    Ok(view) => {
                        #[cfg(feature = "screen-work")]
                        if screen.name != WORK_SCREEN {
                            self.tabbed.push(screen.name);
                        }
                        Shown::new(screen.name.into(), view)
                    }
                    Err(error) => return eprintln!("desk: screen {name} did not open: {error}"),
                }
            }
        };
        let left = std::mem::replace(&mut self.shown, next);
        self.parked.push(left);
        let focus = match &self.shown.body {
            #[cfg(feature = "screen-work")]
            Body::Work(work) => work.focus_handle(cx),
            Body::View(_) => self.focus.clone(),
        };
        focus.focus(window, cx);
        eprintln!("desk: screen {name}");
        cx.notify();
    }

    fn picked(&mut self, id: &SharedString, window: &mut Window, cx: &mut Context<Self>) {
        eprintln!("desk: palette picked {id}");
        if let Some(name) = id.strip_prefix(SCREEN_ID) {
            return self.show(name, window, cx);
        }
        if id == OPEN_SETTINGS_ID {
            return self.show(SETTINGS, window, cx);
        }
        let action = id
            .strip_prefix(LAYOUT_ID)
            .and_then(|label| SHORTCUTS.iter().find(|shortcut| shortcut.label == label))
            .map(|shortcut| shortcut.action);
        match (action, &self.shown.body) {
            (None, _) => eprintln!("desk: palette: {id} is not a command the desk knows"),
            #[cfg(feature = "screen-work")]
            (Some(action), Body::Work(work)) => {
                work.update(cx, |work, cx| work.run(action, "", window, cx));
            }
            (Some(action), Body::View(_)) => eprintln!(
                "desk: palette: {action:?} needs the work screen, and {} shows",
                self.shown.name
            ),
        }
    }

    #[cfg_attr(
        not(feature = "screen-work"),
        expect(
            unused_variables,
            reason = "only the work screen has a composer to focus"
        )
    )]
    fn escape(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        if event.keystroke.key != "escape" {
            return;
        }
        match &self.shown.body {
            #[cfg(feature = "screen-work")]
            Body::Work(work) => {
                if work.update(cx, |work, cx| work.cancel_drag(cx)) {
                    return eprintln!("desk: escape: the drag is cancelled");
                }
                let focused = work.update(cx, |work, cx| work.focus_composer(window, cx));
                eprintln!("desk: escape: the composer has focus {focused}");
            }
            Body::View(_) => eprintln!("desk: escape: {} has no composer", self.shown.name),
        }
    }

    fn tiles_x(&self) -> f32 {
        if self.sidebar_open {
            SIDEBAR_COLUMN
        } else {
            GUTTER
        }
    }

    #[cfg(feature = "screen-work")]
    fn strip(&self, theme: &Theme, cx: &mut Context<Self>) -> Option<AnyElement> {
        let work =
            std::iter::once(&self.shown)
                .chain(&self.parked)
                .find_map(|shown| match &shown.body {
                    Body::Work(work) => Some(work.clone()),
                    Body::View(_) => None,
                })?;
        let screens: Vec<Tab> = self
            .tabbed
            .iter()
            .map(|name| Tab {
                label: (*name).into(),
                icon: None,
                count: None,
                mark: TabMark::Close,
            })
            .collect();
        let openable: Vec<&'static str> = self
            .screens
            .iter()
            .map(|screen| screen.name)
            .filter(|name| LIVE_SCREENS.contains(name))
            .collect();
        let screen = self.tabbed.iter().position(|name| self.shown.name == *name);
        let desk = cx.weak_entity();
        Some(work.update(cx, |work, cx| {
            work.tabs(
                &screens,
                &openable,
                screen,
                theme,
                move |strip, window, cx| {
                    desk.update(cx, |desk, cx| desk.stripped(strip, window, cx))
                        .unwrap_or_else(|_| eprintln!("desk: the desk is gone"));
                },
                cx,
            )
        }))
    }

    #[cfg(feature = "screen-work")]
    fn stripped(&mut self, strip: &work::Strip, window: &mut Window, cx: &mut Context<Self>) {
        let named = |at: &usize| self.tabbed.get(*at).copied();
        match strip {
            work::Strip::Work => self.show(WORK_SCREEN, window, cx),
            work::Strip::Show(at) => match named(at) {
                Some(name) => self.show(name, window, cx),
                None => eprintln!("desk: screen tab {at} is not open"),
            },
            work::Strip::Close(at) => match named(at) {
                Some(name) => self.close_screen(name, window, cx),
                None => eprintln!("desk: screen tab {at} is not open"),
            },
            work::Strip::Open(name) => self.show(name, window, cx),
        }
    }

    #[cfg(feature = "screen-work")]
    fn close_screen(&mut self, name: &'static str, window: &mut Window, cx: &mut Context<Self>) {
        if self.shown.name == name {
            self.show(WORK_SCREEN, window, cx);
        }
        self.parked.retain(|parked| parked.name != name);
        self.tabbed.retain(|tabbed| *tabbed != name);
        eprintln!("desk: screen {name} closed");
        cx.notify();
    }

    #[cfg_attr(
        not(feature = "screen-work"),
        expect(
            unused_variables,
            reason = "only the work screen is fitted to the window"
        )
    )]
    fn content(&self, theme: &Theme, cx: &mut Context<Self>) -> (Option<AnyElement>, AnyElement) {
        #[cfg(feature = "screen-work")]
        let tabs = self.strip(theme, cx);
        #[cfg(not(feature = "screen-work"))]
        let tabs = None;
        let body = match &self.shown.body {
            #[cfg(feature = "screen-work")]
            Body::Work(work) => {
                work.update(cx, |work, _| {
                    work.fit(TILE_BOTTOM + STATUS_BAR_HEIGHT - WORKSPACE_EDGE)
                });
                div()
                    .flex_1()
                    .min_w_0()
                    .child(work.clone())
                    .into_any_element()
            }
            Body::View(view) => outer_card(theme)
                .flex_1()
                .child(inner_card(theme).child(view.clone()))
                .into_any_element(),
        };
        (tabs, body)
    }

    pub fn toggle_sidebar(&mut self, cx: &mut Context<Self>) {
        self.sidebar_open = !self.sidebar_open;
        let state = if self.sidebar_open { "open" } else { "closed" };
        eprintln!("desk: sidebar {state}");
        cx.notify();
    }

    fn global_key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let stroke = &event.keystroke;
        let held = stroke.modifiers;
        if !held.control || held.alt || held.shift || held.platform {
            return;
        }
        match stroke.key.as_str() {
            "b" => self.toggle_sidebar(cx),
            "k" => self.open_palette(window, cx),
            "f" => window.dispatch_action(Box::new(Find), cx),
            "," => self.show(SETTINGS, window, cx),
            _ => return,
        }
        cx.stop_propagation();
    }

    pub fn tell(&mut self, control: Control, cx: &mut Context<Self>) {
        let expiry = cx.spawn(async move |this, cx| {
            cx.background_executor().timer(TOAST_LIFETIME).await;
            this.update(cx, |desk, cx| {
                desk.toast = None;
                cx.notify();
            })
        });
        self.toast = Some(Toast {
            control,
            _expiry: expiry,
        });
        cx.notify();
    }

    #[cfg(not(feature = "screen-work"))]
    fn pick_from_sidebar(&mut self, pick: &SidebarPick, _: &mut Window, cx: &mut Context<Self>) {
        match pick {
            SidebarPick::Project => self.tell(Control::OpenProject, cx),
            SidebarPick::NewSession
            | SidebarPick::Running(_)
            | SidebarPick::Inactive(_)
            | SidebarPick::Docs => eprintln!("desk: sidebar: {pick:?} needs the work screen"),
        }
    }

    #[cfg(not(feature = "screen-work"))]
    fn sidebar_project(&self, _: &App) -> Option<Project> {
        None
    }

    #[cfg(not(feature = "screen-work"))]
    fn switch_sheet(&self, _: &mut Context<Self>) -> Option<AnyElement> {
        None
    }
}

#[cfg(feature = "screen-work")]
impl Desk {
    fn work(&self) -> Option<&Entity<Work>> {
        std::iter::once(&self.shown)
            .chain(&self.parked)
            .find_map(|shown| match &shown.body {
                Body::Work(work) => Some(work),
                Body::View(_) => None,
            })
    }

    fn adopt(&mut self, folder: PathBuf, work: &Entity<Work>, cx: &mut Context<Self>) {
        eprintln!("desk: project {}", folder.display());
        let chat = work.read(cx).chat().clone();
        self.projects._listed = Some(cx.subscribe(&chat, |_, _, _: &Listed, cx| cx.notify()));
        match project::remember(&folder) {
            Ok(recents) => self.projects.recents = recents,
            Err(error) => eprintln!("desk: recents: {error}"),
        }
        self.projects.head = Some(Head {
            folder: folder.clone(),
            branch: String::new(),
            changed: 0,
        });
        self.projects._head = Some(cx.spawn(async move |this, cx| {
            let head = cx
                .background_executor()
                .spawn(async move { project::head(folder) })
                .await;
            this.update(cx, |desk, cx| {
                desk.projects.head = Some(head);
                cx.notify();
            })
            .ok();
        }));
        cx.notify();
    }

    fn sidebar_project(&self, cx: &App) -> Option<Project> {
        let head = self.projects.head.as_ref()?;
        let chat = self.work()?.read(cx).chat().read(cx);
        Some(project::sidebar(head, chat.rows(), chat.open_id()))
    }

    fn pick_from_sidebar(
        &mut self,
        pick: &SidebarPick,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let Some(chat) = self.work().map(|work| work.read(cx).chat().clone()) else {
            return self.open_projects(window, cx);
        };
        match pick {
            SidebarPick::Project => self.open_projects(window, cx),
            SidebarPick::NewSession => chat.update(cx, |chat, cx| chat.open_session(None, cx)),
            SidebarPick::Running(_) => {
                eprintln!("desk: sidebar: the running session already shows")
            }
            SidebarPick::Inactive(at) => chat.update(cx, |chat, cx| {
                let id = project::inactive(chat.rows(), chat.open_id())
                    .nth(*at)
                    .map(|row| row.id.clone());
                match id {
                    Some(id) => chat.open_session(Some(id), cx),
                    None => eprintln!("desk: sidebar: session row {at} is gone"),
                }
            }),
            SidebarPick::Docs => eprintln!("desk: sidebar: Docs has no source yet"),
        }
    }

    fn open_projects(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let recent = self
            .projects
            .recents
            .iter()
            .enumerate()
            .map(|(at, folder)| PaletteItem {
                id: format!("{RECENT_ID}{at}").into(),
                label: folder.display().to_string().into(),
                group: "Recent projects".into(),
                keys: None,
            });
        let open = PaletteItem {
            id: OPEN_FOLDER_ID.into(),
            label: "Open a folder".into(),
            group: "Open".into(),
            keys: None,
        };
        let menu = Palette::new(recent.chain(std::iter::once(open)).collect(), window, cx);
        let picked =
            cx.listener(|desk, id: &SharedString, window, cx| desk.project_picked(id, window, cx));
        menu.update(cx, |menu, cx| {
            menu.on_pick(picked);
            menu.open(window, cx);
        });
        eprintln!(
            "desk: projects menu lists {} recents",
            self.projects.recents.len()
        );
        self.projects.menu = Some(menu);
        cx.notify();
    }

    fn project_picked(&mut self, id: &SharedString, window: &mut Window, cx: &mut Context<Self>) {
        eprintln!("desk: projects menu picked {id}");
        if id == OPEN_FOLDER_ID {
            return self.prompt_folder(window, cx);
        }
        let folder = id
            .strip_prefix(RECENT_ID)
            .and_then(|at| at.parse::<usize>().ok())
            .and_then(|at| self.projects.recents.get(at).cloned());
        match folder {
            Some(folder) => self.switch(folder, window, cx),
            None => eprintln!("desk: projects menu: {id} is not a recent project"),
        }
    }

    fn prompt_folder(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let asked = cx.prompt_for_paths(gpui::PathPromptOptions {
            files: false,
            directories: true,
            multiple: false,
            prompt: Some("Open project".into()),
        });
        cx.spawn_in(window, async move |this, cx| {
            let folder = match asked.await {
                Ok(Ok(Some(mut picked))) => picked.pop(),
                Ok(Ok(None)) => None,
                Ok(Err(error)) => {
                    eprintln!("desk: the folder dialog failed: {error}");
                    None
                }
                Err(_) => {
                    eprintln!("desk: the folder dialog closed without an answer");
                    None
                }
            };
            let Some(folder) = folder else {
                return eprintln!("desk: no folder was picked");
            };
            this.update_in(cx, |desk, window, cx| desk.switch(folder, window, cx))
                .ok();
        })
        .detach();
    }

    fn switch(&mut self, folder: PathBuf, window: &mut Window, cx: &mut Context<Self>) {
        if self
            .projects
            .head
            .as_ref()
            .is_some_and(|head| head.folder == folder)
        {
            return eprintln!("desk: {} is already open", folder.display());
        }
        let busy = self
            .work()
            .is_some_and(|work| work.read(cx).chat().read(cx).busy(cx));
        if busy {
            eprintln!("desk: a turn is running, so the switch asks first");
            self.projects.switching = Some(folder);
            self.projects.confirming = true;
            return cx.notify();
        }
        self.switch_now(folder, window, cx);
    }

    fn switch_now(&mut self, folder: PathBuf, window: &mut Window, cx: &mut Context<Self>) {
        let work = match work::open_in(folder.clone(), window, cx) {
            Ok(work) => work,
            Err(error) => return eprintln!("desk: {} did not open: {error}", folder.display()),
        };
        self.parked
            .retain(|parked| matches!(parked.body, Body::View(_)));
        let next = Shown {
            name: WORK_SCREEN.into(),
            body: Body::Work(work.clone()),
        };
        let left = std::mem::replace(&mut self.shown, next);
        if let Body::View(_) = left.body {
            self.parked.push(left);
        }
        work.focus_handle(cx).focus(window, cx);
        self.adopt(folder, &work, cx);
    }

    fn switch_sheet(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let folder = self.projects.switching.as_ref()?;
        let cancel = cx.listener(|desk, _: &(), _, cx| {
            eprintln!("desk: switch cancelled");
            desk.projects.confirming = false;
            cx.notify();
        });
        let confirm = cx.listener(|desk, _: &(), window, cx| {
            desk.projects.confirming = false;
            if let Some(folder) = desk.projects.switching.clone() {
                desk.switch_now(folder, window, cx);
            }
        });
        Some(
            Sheet::new(
                "switch-project",
                format!("Switch to {}?", project::name(folder)),
            )
            .open(self.projects.confirming)
            .on_cancel(move |window, cx| cancel(&(), window, cx))
            .on_confirm(move |window, cx| confirm(&(), window, cx))
            .child(
                div().child(
                    "A turn is running here. Switching stops this tofu, and the turn with it.",
                ),
            )
            .into_any_element(),
        )
    }
}

impl Render for Desk {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let problems = ActiveTheme::problems(cx);
        let toast_layer = self.toast.as_ref().map(|shown| {
            div()
                .absolute()
                .left_0()
                .right_0()
                .bottom(px(TOAST_BOTTOM))
                .flex()
                .justify_center()
                .child(toast(
                    shown.control.tell().into(),
                    TELL_BADGE,
                    &theme,
                    cx.listener(|desk, _: &ClickEvent, _, cx| {
                        desk.toast = None;
                        cx.notify();
                    }),
                ))
        });
        let (tabs, content) = self.content(&theme, cx);
        let sheet = self.switch_sheet(cx);
        #[cfg(feature = "screen-work")]
        let menu = self.projects.menu.clone();
        #[cfg(not(feature = "screen-work"))]
        let menu: Option<Entity<Palette>> = None;
        div()
            .size_full()
            .relative()
            .track_focus(&self.focus)
            .capture_key_down(cx.listener(Self::global_key))
            .on_key_down(cx.listener(Self::escape))
            .on_action(|_: &Find, _, _| {
                eprintln!("desk: find: Ctrl F is not in the focused module, find is chat only");
            })
            .flex()
            .flex_col()
            .rounded(px(WINDOW_RADIUS))
            .bg(theme.color(ColorToken::SurfaceWindow))
            .shadow(vec![ring(ink(&theme, WINDOW_RING))])
            .text_size(px(TEXT))
            .text_color(theme.color(ColorToken::TextBase))
            .child(title_bar(
                self.sidebar_open,
                self.tiles_x(),
                self.account.clone(),
                tabs,
                cx,
            ))
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .pr(px(GUTTER))
                    .when(self.sidebar_open, |body| {
                        body.child(Sidebar::new(
                            "sidebar",
                            self.sidebar_project(cx),
                            cx.listener(|desk, pick: &SidebarPick, window, cx| {
                                desk.pick_from_sidebar(pick, window, cx)
                            }),
                        ))
                    })
                    .when(!self.sidebar_open, |body| body.pl(px(GUTTER)))
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .flex()
                            .pt(px(TILE_TOP))
                            .pb(px(TILE_BOTTOM))
                            .child(content),
                    ),
            )
            .child(status_bar(&problems, cx))
            .child(self.palette.clone())
            .children(menu)
            .children(sheet)
            .children(toast_layer)
    }
}
