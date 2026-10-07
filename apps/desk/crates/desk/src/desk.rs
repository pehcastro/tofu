use crate::modules::chat::Find;
#[cfg(feature = "screen-work")]
use crate::screens::work::Work;
use desk_core::control::{Control, TELL_BADGE};
use desk_core::limits::TOAST_LIFETIME;
#[cfg(feature = "screen-work")]
use desk_tiling::Rect;
use desk_tiling::{Key, SHORTCUTS};
use desk_ui::component::control;
use desk_ui::components::card::{inner_card, outer_card};
use desk_ui::components::overlay::toast;
use desk_ui::components::palette::{Palette, PaletteItem};
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::{
    SIDEBAR_WIDTH, TEXT, TOAST_BOTTOM, WINDOW_HEIGHT, WINDOW_MIN_HEIGHT, WINDOW_MIN_WIDTH,
    WINDOW_WIDTH,
};
#[cfg(feature = "screen-work")]
use desk_ui::metrics::{STATUS_BAR_HEIGHT, TITLE_BAR_HEIGHT};
use desk_ui::theme::{ColorToken, Theme};
#[cfg(feature = "screen-work")]
use gpui::Focusable;
use gpui::{
    AnyElement, AnyView, App, ClickEvent, Context, Entity, FocusHandle, IntoElement, KeyDownEvent,
    Pixels, Render, SharedString, Size, Task, TitlebarOptions, Window, WindowBounds, WindowOptions,
    div, prelude::*, px, size,
};

pub const WINDOW_TITLE: &str = "Tofu Desk";
pub const BOARD_VIEWPORT_WIDTH: f32 = 1440.0;
pub const BOARD_VIEWPORT_HEIGHT: f32 = 900.0;
const GUTTER: f32 = 8.0;
const SETTINGS: &str = "settings";
const SCREEN_ID: &str = "screen.";
const LAYOUT_ID: &str = "layout.";
const OPEN_SETTINGS_ID: &str = "settings.open";

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
        Desk {
            sidebar_open: true,
            toast: None,
            screens,
            shown: Shown::new(name, view),
            parked: Vec::new(),
            palette,
            focus: cx.focus_handle(),
        }
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
                    Ok(view) => Shown::new(screen.name.into(), view),
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
                let focused = work.update(cx, |work, cx| work.focus_composer(window, cx));
                eprintln!("desk: escape: the composer has focus {focused}");
            }
            Body::View(_) => eprintln!("desk: escape: {} has no composer", self.shown.name),
        }
    }

    #[cfg_attr(
        not(feature = "screen-work"),
        expect(
            unused_variables,
            reason = "only the work screen is fitted to the window"
        )
    )]
    fn content(
        &self,
        theme: &Theme,
        window: &Window,
        cx: &mut App,
    ) -> (Option<AnyElement>, AnyElement) {
        match &self.shown.body {
            #[cfg(feature = "screen-work")]
            Body::Work(work) => {
                let viewport = window.viewport_size();
                let x = if self.sidebar_open {
                    SIDEBAR_WIDTH
                } else {
                    GUTTER
                };
                let area = Rect {
                    x,
                    y: TITLE_BAR_HEIGHT,
                    w: f32::from(viewport.width) - x - GUTTER,
                    h: f32::from(viewport.height) - TITLE_BAR_HEIGHT - STATUS_BAR_HEIGHT,
                };
                work.update(cx, |work, _| work.fit(area));
                let tabs = work.read(cx).tabs(work.downgrade(), theme);
                (
                    Some(tabs.into_any_element()),
                    div()
                        .flex_1()
                        .min_w_0()
                        .child(work.clone())
                        .into_any_element(),
                )
            }
            Body::View(view) => (
                None,
                outer_card(theme)
                    .flex_1()
                    .child(inner_card(theme).child(view.clone()))
                    .into_any_element(),
            ),
        }
    }

    pub fn teller(
        control: Control,
        cx: &mut Context<Self>,
    ) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static {
        cx.listener(move |desk, _: &ClickEvent, _, cx| desk.tell(control, cx))
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

    fn tell(&mut self, control: Control, cx: &mut Context<Self>) {
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

    fn sidebar(theme: &Theme, cx: &mut Context<Self>) -> impl IntoElement {
        div()
            .w(px(SIDEBAR_WIDTH))
            .flex_none()
            .flex()
            .flex_col()
            .py_2p5()
            .px_2()
            .child(
                control("open-project", Control::OpenProject.label(), theme)
                    .justify_start()
                    .px_2p5()
                    .py_1p5()
                    .rounded_lg()
                    .text_color(theme.color(ColorToken::TextMuted))
                    .child(Control::OpenProject.label())
                    .on_click(Self::teller(Control::OpenProject, cx)),
            )
    }
}

impl Render for Desk {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
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
        let (tabs, content) = self.content(&theme, window, cx);
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
            .bg(theme.color(ColorToken::SurfaceWindow))
            .text_size(px(TEXT))
            .text_color(theme.color(ColorToken::TextBase))
            .child(crate::title_bar::render(
                self.sidebar_open,
                tabs,
                &theme,
                window,
                cx,
            ))
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .pr(px(GUTTER))
                    .when(self.sidebar_open, |body| {
                        body.child(Self::sidebar(&theme, cx))
                    })
                    .when(!self.sidebar_open, |body| body.pl(px(GUTTER)))
                    .child(content),
            )
            .child(crate::status_bar::render(&theme, &problems, cx))
            .child(self.palette.clone())
            .children(toast_layer)
    }
}
