use desk_core::control::{Control, TELL_BADGE};
use desk_core::limits::TOAST_LIFETIME;
use desk_ui::component::{control, toast};
use desk_ui::metrics::{
    SIDEBAR_WIDTH, TEXT, TOAST_BOTTOM, WINDOW_HEIGHT, WINDOW_MIN_HEIGHT, WINDOW_MIN_WIDTH,
    WINDOW_WIDTH,
};
use desk_ui::theme;
use gpui::{
    App, ClickEvent, Context, IntoElement, Render, Task, TitlebarOptions, Window, WindowBounds,
    WindowOptions, div, prelude::*, px, rgba, size,
};

pub const WINDOW_TITLE: &str = "Tofu Desk";

pub struct Desk {
    sidebar_open: bool,
    toast: Option<Toast>,
}

struct Toast {
    control: Control,
    _expiry: Task<gpui::Result<()>>,
}

pub fn window_options(cx: &App) -> WindowOptions {
    let mut options = WindowOptions::new()
        .window_bounds(Some(WindowBounds::centered(
            size(px(WINDOW_WIDTH), px(WINDOW_HEIGHT)),
            cx,
        )))
        .window_min_size(Some(size(px(WINDOW_MIN_WIDTH), px(WINDOW_MIN_HEIGHT))))
        .titlebar(Some(TitlebarOptions {
            title: Some(WINDOW_TITLE.into()),
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
    pub fn new() -> Self {
        Desk {
            sidebar_open: true,
            toast: None,
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
        cx.notify();
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

    fn sidebar(&self, cx: &mut Context<Self>) -> impl IntoElement {
        div()
            .w(px(SIDEBAR_WIDTH))
            .flex_none()
            .flex()
            .flex_col()
            .py_2p5()
            .px_2()
            .child(
                control("open-project", Control::OpenProject.label())
                    .justify_start()
                    .px_2p5()
                    .py_1p5()
                    .rounded_lg()
                    .text_color(rgba(theme::TEXT_MUTED))
                    .child(Control::OpenProject.label())
                    .on_click(Self::teller(Control::OpenProject, cx)),
            )
    }
}

impl Render for Desk {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
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
                    cx.listener(|desk, _: &ClickEvent, _, cx| {
                        desk.toast = None;
                        cx.notify();
                    }),
                ))
        });
        div()
            .size_full()
            .relative()
            .flex()
            .flex_col()
            .bg(rgba(theme::WINDOW))
            .text_size(px(TEXT))
            .text_color(rgba(theme::TEXT))
            .child(crate::title_bar::render(self.sidebar_open, window, cx))
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .when(self.sidebar_open, |body| body.child(self.sidebar(cx))),
            )
            .child(crate::status_bar::render(cx))
            .children(toast_layer)
    }
}
