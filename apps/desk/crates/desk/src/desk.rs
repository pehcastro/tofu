use crate::modules::chat::Find;
#[cfg(feature = "screen-work")]
use crate::modules::chat::{Chat, Listed, Touched};
#[cfg(feature = "screen-work")]
use crate::project::{self, Head};
#[cfg(all(feature = "screen-work", feature = "screen-classifier"))]
use crate::screens::classifier::Classifier;
#[cfg(all(feature = "screen-work", feature = "screen-context"))]
use crate::screens::context::ContextScreen;
#[cfg(all(feature = "screen-work", feature = "screen-library"))]
use crate::screens::library::Library;
#[cfg(all(feature = "screen-work", feature = "screen-limits"))]
use crate::screens::limits::Limits;
#[cfg(all(feature = "screen-work", feature = "screen-session"))]
use crate::screens::session::SessionScreen;
#[cfg(all(feature = "screen-work", feature = "screen-usage"))]
use crate::screens::usage::UsageScreen;
#[cfg(feature = "screen-work")]
use crate::screens::work::{self, Work};
use crate::status_bar::status_bar;
use crate::title_bar::{Bell, title_bar};
use desk_core::control::{Control, TELL_BADGE};
use desk_core::limits::TOAST_LIFETIME;
#[cfg(feature = "screen-work")]
use desk_core::protocol::CronJob;
#[cfg(feature = "screen-work")]
use desk_tiling::WORKSPACE_EDGE;
use desk_tiling::{Key, SHORTCUTS};
use desk_ui::components::card::{inner_card, outer_card};
#[cfg(feature = "screen-work")]
use desk_ui::components::form::TextInput;
use desk_ui::components::overlay::toast;
#[cfg(feature = "screen-work")]
use desk_ui::components::overlay::{Align, MenuButton, MenuItem, Placement, Side};
use desk_ui::components::paint::{ink, ring};
use desk_ui::components::palette::{Palette, PaletteItem};
#[cfg(feature = "screen-work")]
use desk_ui::components::sheet::Sheet;
#[cfg(feature = "screen-work")]
use desk_ui::components::sidebar::SessionAt;
use desk_ui::components::sidebar::{Project, SIDEBAR_COLUMN, Sidebar, SidebarPick};
use desk_ui::components::status_bar::Status;
#[cfg(feature = "screen-work")]
use desk_ui::components::status_bar::{Branch, ContextUse, Quota, cron_trigger};
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
use gpui::EntityInputHandler;
use gpui::{
    AnyElement, AnyView, App, ClickEvent, Context, Entity, FocusHandle, IntoElement, KeyDownEvent,
    Pixels, Render, SharedString, Size, Task, TitlebarOptions, Window, WindowBounds, WindowOptions,
    div, prelude::*, px, size,
};
#[cfg(feature = "screen-work")]
use gpui::{ClipboardItem, Focusable};
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
const LIVE_SCREENS: [&str; 7] = [
    "theme",
    "limits",
    "classifier",
    "library",
    "context",
    "session",
    "usage",
];
#[cfg(feature = "screen-work")]
const CRON_PROMPT_CHARS: usize = 32;

#[cfg(feature = "screen-work")]
fn cron_button(cx: &mut App) -> Entity<MenuButton> {
    let button = MenuButton::new("Cron".into(), Vec::new(), cx);
    button.update(cx, |button, _| {
        button.placement(Placement {
            side: Side::Top,
            align: Align::End,
            ..Placement::below()
        });
    });
    button
}

#[cfg(feature = "screen-work")]
fn shortened(prompt: &str) -> String {
    match prompt.char_indices().nth(CRON_PROMPT_CHARS) {
        Some((cut, _)) => format!("{}…", prompt.get(..cut).unwrap_or(prompt)),
        None => prompt.to_owned(),
    }
}

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
    toast: Option<Toast>,
    screens: Vec<Screen>,
    shown: Shown,
    parked: Vec<Shown>,
    palette: Entity<Palette>,
    focus: FocusHandle,
    bell: Bell,
    #[cfg(feature = "screen-work")]
    projects: Projects,
    #[cfg(feature = "screen-work")]
    tabbed: Vec<&'static str>,
    #[cfg(feature = "screen-work")]
    cron: Entity<MenuButton>,
}

#[cfg(feature = "screen-work")]
#[derive(Default)]
struct Projects {
    head: Option<Head>,
    recents: Vec<PathBuf>,
    menu: Option<Entity<Palette>>,
    switching: Option<PathBuf>,
    confirming: bool,
    active: Vec<String>,
    renaming: Option<Renaming>,
    refused: Option<Option<SharedString>>,
    _watch: Vec<gpui::Subscription>,
    _head: Option<Task<()>>,
}

#[cfg(feature = "screen-work")]
struct Renaming {
    id: String,
    at: SessionAt,
    input: Entity<TextInput>,
}

struct Toast {
    text: SharedString,
    badge: Option<&'static str>,
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

#[cfg(feature = "screen-work")]
fn thousands(tokens: i64) -> String {
    match tokens {
        ..1000 => tokens.to_string(),
        _ => format!("{}k", tokens / 1000),
    }
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
            toast: None,
            screens,
            shown: Shown::new(name, view),
            parked: Vec::new(),
            palette,
            focus: cx.focus_handle(),
            bell: Bell::default(),
            #[cfg(feature = "screen-work")]
            projects: Projects::default(),
            #[cfg(feature = "screen-work")]
            tabbed: Vec::new(),
            #[cfg(feature = "screen-work")]
            cron: cron_button(cx),
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
        #[cfg(all(
            feature = "screen-work",
            any(
                feature = "screen-limits",
                feature = "screen-classifier",
                feature = "screen-library",
                feature = "screen-context",
                feature = "screen-session",
                feature = "screen-usage"
            )
        ))]
        self.feed_screens(cx);
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

    pub fn read_notices(&mut self, cx: &mut Context<Self>) {
        self.bell.read();
        cx.notify();
    }

    pub fn tell(&mut self, control: Control, cx: &mut Context<Self>) {
        self.toast(control.tell().into(), Some(TELL_BADGE), cx);
    }

    fn toast(&mut self, text: SharedString, badge: Option<&'static str>, cx: &mut Context<Self>) {
        let expiry = cx.spawn(async move |this, cx| {
            cx.background_executor().timer(TOAST_LIFETIME).await;
            this.update(cx, |desk, cx| {
                desk.toast = None;
                cx.notify();
            })
        });
        self.toast = Some(Toast {
            text,
            badge,
            _expiry: expiry,
        });
        cx.notify();
    }

    #[cfg(not(feature = "screen-work"))]
    fn pick_from_sidebar(&mut self, pick: &SidebarPick, _: &mut Window, cx: &mut Context<Self>) {
        match pick {
            SidebarPick::Project => self.tell(Control::OpenProject, cx),
            SidebarPick::NewSession
            | SidebarPick::Open(_)
            | SidebarPick::CopyId(_)
            | SidebarPick::Docs => eprintln!("desk: sidebar: {pick:?} needs the work screen"),
        }
    }

    fn sidebar(&self, cx: &mut Context<Self>) -> Sidebar {
        let sidebar = Sidebar::new(
            "sidebar",
            self.sidebar_project(cx),
            cx.listener(|desk, pick: &SidebarPick, window, cx| {
                desk.pick_from_sidebar(pick, window, cx)
            }),
        );
        #[cfg(feature = "screen-work")]
        if let Some(renaming) = &self.projects.renaming {
            let field = div()
                .on_key_down(cx.listener(Self::rename_key))
                .child(renaming.input.clone());
            return sidebar.editing(renaming.at, field.into_any_element());
        }
        sidebar
    }

    #[cfg(not(feature = "screen-work"))]
    pub fn open_notice(&mut self, _: usize, _: &mut Context<Self>) {}

    #[cfg(not(feature = "screen-work"))]
    fn tofu_version(&self, _: &App) -> Option<SharedString> {
        None
    }

    #[cfg(not(feature = "screen-work"))]
    fn sidebar_project(&self, _: &App) -> Option<Project> {
        None
    }

    #[cfg(not(feature = "screen-work"))]
    fn status(&self, _: &App) -> Status {
        Status::default()
    }

    #[cfg(not(feature = "screen-work"))]
    fn cron_menu(&self, _: usize, _: &mut Context<Self>) -> Option<AnyView> {
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

    fn tofu_version(&self, cx: &App) -> Option<SharedString> {
        let version = self.work()?.read(cx).chat().read(cx).tofu_version();
        (!version.is_empty()).then(|| version.to_owned().into())
    }

    pub fn open_notice(&mut self, ix: usize, cx: &mut Context<Self>) {
        let Some(id) = self.bell.session(ix).map(str::to_owned) else {
            return eprintln!("desk: bell: notice {ix} is gone");
        };
        let Some(chat) = self.work().map(|work| work.read(cx).chat().clone()) else {
            return eprintln!("desk: bell: no work screen for session {id}");
        };
        eprintln!("desk: bell: open {id}");
        chat.update(cx, |chat, cx| chat.open_session(Some(id), cx));
    }

    fn adopt(&mut self, folder: PathBuf, work: &Entity<Work>, cx: &mut Context<Self>) {
        eprintln!("desk: project {}", folder.display());
        let chat = work.read(cx).chat().clone();
        let store = chat.read(cx).store().clone();
        self.projects.active.clear();
        self.projects.renaming = None;
        self.projects._watch = vec![
            cx.subscribe(&chat, |desk, _, _: &Listed, cx| {
                desk.track_running(cx);
                cx.notify();
            }),
            cx.subscribe(&chat, |desk, _, _: &Touched, cx| desk.reread_head(cx)),
            cx.observe(&chat, |desk, _, cx| desk.track_running(cx)),
            cx.observe(&store, |desk, store, cx| {
                for (id, session) in &store.read(cx).sessions {
                    desk.bell.gather(id, session);
                }
                cx.notify();
            }),
        ];
        match project::remember(&folder) {
            Ok(recents) => self.projects.recents = recents,
            Err(error) => eprintln!("desk: recents: {error}"),
        }
        self.projects.head = Some(Head::empty(folder));
        self.reread_head(cx);
        #[cfg(any(
            feature = "screen-limits",
            feature = "screen-classifier",
            feature = "screen-library",
            feature = "screen-context",
            feature = "screen-session",
            feature = "screen-usage"
        ))]
        self.feed_screens(cx);
        cx.notify();
    }

    #[cfg(any(
        feature = "screen-limits",
        feature = "screen-classifier",
        feature = "screen-library",
        feature = "screen-context",
        feature = "screen-session",
        feature = "screen-usage"
    ))]
    fn feed_screens(&mut self, cx: &mut Context<Self>) {
        let Some(chat) = self.work().map(|work| work.read(cx).chat().clone()) else {
            return;
        };
        let views: Vec<AnyView> = std::iter::once(&self.shown)
            .chain(&self.parked)
            .filter_map(|shown| match &shown.body {
                Body::View(view) => Some(view.clone()),
                Body::Work(_) => None,
            })
            .collect();
        for view in views {
            #[cfg(feature = "screen-limits")]
            if let Ok(limits) = view.clone().downcast::<Limits>() {
                limits.update(cx, |limits, cx| limits.read_from(&chat, cx));
            }
            #[cfg(feature = "screen-classifier")]
            if let Ok(classifier) = view.clone().downcast::<Classifier>() {
                classifier.update(cx, |classifier, cx| classifier.read_from(&chat, cx));
            }
            #[cfg(feature = "screen-library")]
            if let Ok(library) = view.clone().downcast::<Library>() {
                library.update(cx, |library, cx| library.read_from(&chat, cx));
            }
            #[cfg(feature = "screen-context")]
            if let Ok(context) = view.clone().downcast::<ContextScreen>() {
                context.update(cx, |context, cx| context.read_from(&chat, cx));
            }
            #[cfg(feature = "screen-session")]
            if let Ok(session) = view.clone().downcast::<SessionScreen>() {
                session.update(cx, |session, cx| session.read_from(&chat, cx));
            }
            #[cfg(feature = "screen-usage")]
            if let Ok(usage) = view.downcast::<UsageScreen>() {
                usage.update(cx, |usage, cx| usage.read_from(&chat, cx));
            }
        }
    }

    fn reread_head(&mut self, cx: &mut Context<Self>) {
        let Some(folder) = self.projects.head.as_ref().map(|head| head.folder.clone()) else {
            return;
        };
        self.projects._head = Some(cx.spawn(async move |this, cx| {
            let head = cx
                .background_executor()
                .spawn(async move { project::head(folder) })
                .await;
            this.update(cx, |desk, cx| {
                eprintln!(
                    "desk: git {} ahead {} changed {}",
                    head.branch, head.ahead, head.changed
                );
                desk.projects.head = Some(head);
                cx.notify();
            })
            .ok();
        }));
        cx.notify();
    }

    fn track_running(&mut self, cx: &mut Context<Self>) {
        let Some(work) = self.work().cloned() else {
            return;
        };
        let chat = work.read(cx).chat().read(cx);
        let busy = chat.open_id().filter(|_| chat.busy(cx));
        let problem = chat.problem().cloned();
        let moved = project::keep_active(&mut self.projects.active, chat.rows(), busy);
        if moved {
            eprintln!("desk: active {}", self.projects.active.join(" "));
            cx.notify();
        }
        if let Some(before) = self.projects.refused.take() {
            match problem {
                Some(text) if Some(&text) != before.as_ref() => {
                    eprintln!("desk: sidebar: rename refused: {text}");
                    self.toast(text, None, cx);
                }
                _ => self.projects.refused = Some(before),
            }
        }
    }

    fn sidebar_project(&self, cx: &App) -> Option<Project> {
        let head = self.projects.head.as_ref()?;
        let chat = self.work()?.read(cx).chat().read(cx);
        let open = chat.open_id();
        let waiting = open
            .and_then(|id| chat.store().read(cx).sessions.get(id))
            .is_some_and(|session| !session.approvals.is_empty());
        let opened = project::Opened {
            active: &self.projects.active,
            open,
            waiting,
            working: chat.busy(cx),
        };
        Some(project::sidebar(head, chat.rows(), &opened))
    }

    fn session_id(&self, at: SessionAt, chat: &Chat) -> Option<String> {
        match at {
            SessionAt::Active(ix) => self.projects.active.get(ix).cloned(),
            SessionAt::Inactive(ix) => project::inactive(chat.rows(), &self.projects.active)
                .nth(ix)
                .map(|row| row.id.clone()),
        }
    }

    fn cron_menu(&self, live: usize, cx: &mut Context<Self>) -> Option<AnyView> {
        let chat = self.work()?.read(cx).chat().clone();
        let jobs: Vec<CronJob> = {
            let chat = chat.read(cx);
            let session = chat.store().read(cx).sessions.get(chat.open_id()?)?;
            session
                .cron
                .as_ref()?
                .jobs
                .iter()
                .filter(|job| job.ended.is_none())
                .cloned()
                .collect()
        };
        if jobs.is_empty() {
            return None;
        }
        let mut items = Vec::new();
        let mut lines = Vec::new();
        for job in &jobs {
            let toggle = if job.paused { "resume" } else { "pause" };
            items.push(MenuItem::Submenu {
                label: format!("{} {} {}", job.id, job.schedule, shortened(&job.prompt)).into(),
                icon: None,
                items: vec![
                    MenuItem::action(if job.paused { "Resume" } else { "Pause" }),
                    MenuItem::action("Delete"),
                ],
            });
            lines.extend([
                Vec::new(),
                vec![format!("/cron {toggle} {}", job.id)],
                vec![format!("/cron delete {}", job.id)],
            ]);
        }
        items.extend([MenuItem::Separator, MenuItem::action("Stop all")]);
        lines.extend([
            Vec::new(),
            jobs.iter()
                .map(|job| format!("/cron delete {}", job.id))
                .collect(),
        ]);
        let chat = chat.downgrade();
        self.cron.update(cx, |button, cx| {
            button.items(items, cx);
            button.trigger(move |_, theme| cron_trigger(live, theme));
            button.on_pick(move |at, _, cx| {
                let sent = lines.get(*at).cloned().unwrap_or_default();
                let updated = chat.update(cx, |chat, cx| {
                    for line in sent {
                        chat.cron_command(line, cx);
                    }
                });
                if let Err(error) = updated {
                    eprintln!("desk: cron menu lost its chat: {error}");
                }
            });
        });
        Some(self.cron.clone().into())
    }

    fn status(&self, cx: &App) -> Status {
        let branch = self
            .projects
            .head
            .as_ref()
            .filter(|head| !head.branch.is_empty())
            .map(|head| Branch {
                name: head.branch.clone().into(),
                ahead: usize::try_from(head.ahead).unwrap_or(usize::MAX),
            });
        let chat = self.work().map(|work| work.read(cx).chat().read(cx));
        let open = chat.and_then(|chat| {
            let id = chat.open_id()?;
            Some((id, chat.store().read(cx).sessions.get(id)?))
        });
        let Some((id, session)) = open else {
            return Status {
                branch,
                ..Status::default()
            };
        };
        let name = if session.name.is_empty() {
            id
        } else {
            &session.name
        };
        Status {
            branch,
            session: Some(name.to_owned().into()),
            context: session.context.map(|(used, budget)| ContextUse {
                tokens: format!("{} / {}", thousands(used), thousands(budget)).into(),
                share: if budget > 0 {
                    used as f32 / budget as f32
                } else {
                    0.0
                },
            }),
            quota: session.quota.first().map(|window| Quota {
                account: window.account.clone().into(),
                window: window.window.clone().into(),
                percent: window.percent.clamp(0.0, 100.0).round() as u8,
            }),
            classifier: session.decisions.len(),
            cron: session
                .cron
                .as_ref()
                .map_or(0, |cron| usize::try_from(cron.live).unwrap_or(0)),
            problem: None,
        }
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
            SidebarPick::Open(at) | SidebarPick::CopyId(at) => {
                let id = self.session_id(*at, chat.read(cx));
                match (pick, id) {
                    (_, None) => eprintln!("desk: sidebar: session row {at:?} is gone"),
                    (SidebarPick::CopyId(_), Some(id)) => {
                        eprintln!("desk: sidebar: copied session id {id}");
                        cx.write_to_clipboard(ClipboardItem::new_string(id));
                    }
                    (_, Some(id)) if chat.read(cx).open_id() == Some(id.as_str()) => {
                        eprintln!("desk: sidebar: session {id} already shows")
                    }
                    (_, Some(id)) => {
                        eprintln!("desk: sidebar: open {id}");
                        chat.update(cx, |chat, cx| chat.open_session(Some(id), cx));
                    }
                }
            }
            SidebarPick::Rename(at) => {
                let Some(id) = self.session_id(*at, chat.read(cx)) else {
                    return eprintln!("desk: sidebar: session row {at:?} is gone");
                };
                let name = chat
                    .read(cx)
                    .rows()
                    .iter()
                    .find(|row| row.id == id)
                    .map_or(id.clone(), |row| row.title().to_owned());
                let input = TextInput::new("Session name".into(), window, cx);
                input.update(cx, |input, cx| {
                    input.replace_text_in_range(None, &name, window, cx)
                });
                window.focus(&input.focus_handle(cx), cx);
                eprintln!("desk: sidebar: rename {id} from {name}");
                self.projects.renaming = Some(Renaming { id, at: *at, input });
                cx.notify();
            }
            SidebarPick::Docs => eprintln!("desk: sidebar: Docs has no source yet"),
        }
    }

    fn rename_key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let enter = match event.keystroke.key.as_str() {
            "enter" => true,
            "escape" => false,
            _ => return,
        };
        cx.stop_propagation();
        let Some(renaming) = self.projects.renaming.take() else {
            return;
        };
        window.focus(&self.focus, cx);
        cx.notify();
        let name = renaming.input.read(cx).text().trim().to_owned();
        let Some(chat) = self.work().map(|work| work.read(cx).chat().clone()) else {
            return;
        };
        if !enter || name.is_empty() {
            return eprintln!("desk: sidebar: rename {} cancelled", renaming.id);
        }
        eprintln!("desk: sidebar: rename {} to {name}", renaming.id);
        self.projects.refused = Some(chat.read(cx).problem().cloned());
        chat.update(cx, |chat, cx| chat.rename(renaming.id, name, cx));
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
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let edge_to_edge = window.is_maximized() || window.is_fullscreen();
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
                    shown.text.clone(),
                    shown.badge,
                    &theme,
                    cx.listener(|desk, _: &ClickEvent, _, cx| {
                        desk.toast = None;
                        cx.notify();
                    }),
                ))
        });
        let (tabs, content) = self.content(&theme, cx);
        let sheet = self.switch_sheet(cx);
        let status = self.status(cx);
        let cron_menu = self.cron_menu(status.cron, cx);
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
            .when(!edge_to_edge, |root| root.rounded(px(WINDOW_RADIUS)))
            .bg(theme.color(ColorToken::SurfaceWindow))
            .shadow(vec![ring(ink(&theme, WINDOW_RING))])
            .text_size(px(TEXT))
            .text_color(theme.color(ColorToken::TextBase))
            .child(title_bar(
                self.sidebar_open,
                self.tiles_x(),
                status.quota.as_ref(),
                &self.bell,
                self.tofu_version(cx),
                tabs,
                cx,
            ))
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .pr(px(GUTTER))
                    .when(self.sidebar_open, |body| body.child(self.sidebar(cx)))
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
            .child(status_bar(&problems, status, cx).cron_menu(cron_menu))
            .child(self.palette.clone())
            .children(menu)
            .children(sheet)
            .children(toast_layer)
    }
}
