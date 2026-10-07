use crate::modules::chat::Chat;
use desk_core::control::{Control, TELL_BADGE};
use desk_core::limits::TOAST_LIFETIME;
use desk_ui::component::control;
use desk_ui::components::card::{inner_card, outer_card};
use desk_ui::components::overlay::toast;
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::{
    SIDEBAR_WIDTH, TEXT, TOAST_BOTTOM, WINDOW_HEIGHT, WINDOW_MIN_HEIGHT, WINDOW_MIN_WIDTH,
    WINDOW_WIDTH,
};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyView, App, ClickEvent, Context, Entity, IntoElement, KeyDownEvent, Pixels, Render,
    SharedString, Size, Task, TitlebarOptions, Window, WindowBounds, WindowOptions, div,
    prelude::*, px, size,
};

pub const WINDOW_TITLE: &str = "Tofu Desk";
pub const BOARD_VIEWPORT_WIDTH: f32 = 1440.0;
pub const BOARD_VIEWPORT_HEIGHT: f32 = 900.0;

pub struct ScreenRoot(pub Option<AnyView>);

impl Render for ScreenRoot {
    fn render(&mut self, _: &mut Window, _: &mut Context<Self>) -> impl IntoElement {
        div().size_full().children(self.0.clone())
    }
}

pub struct Desk {
    sidebar_open: bool,
    toast: Option<Toast>,
    chat: Entity<Chat>,
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

impl Desk {
    pub fn new(chat: Entity<Chat>) -> Self {
        Desk {
            sidebar_open: true,
            toast: None,
            chat,
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

    fn global_key(&mut self, event: &KeyDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        let stroke = &event.keystroke;
        if stroke.modifiers.control && stroke.key == "b" {
            cx.stop_propagation();
            self.toggle_sidebar(cx);
        }
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
        div()
            .size_full()
            .relative()
            .capture_key_down(cx.listener(Self::global_key))
            .flex()
            .flex_col()
            .bg(theme.color(ColorToken::SurfaceWindow))
            .text_size(px(TEXT))
            .text_color(theme.color(ColorToken::TextBase))
            .child(crate::title_bar::render(
                self.sidebar_open,
                &theme,
                window,
                cx,
            ))
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .pr_2()
                    .when(self.sidebar_open, |body| {
                        body.child(Self::sidebar(&theme, cx))
                    })
                    .when(!self.sidebar_open, |body| body.pl_2())
                    .child(
                        outer_card(&theme)
                            .flex_1()
                            .child(inner_card(&theme).child(self.chat.clone())),
                    ),
            )
            .child(crate::status_bar::render(&theme, &problems, cx))
            .children(toast_layer)
    }
}
