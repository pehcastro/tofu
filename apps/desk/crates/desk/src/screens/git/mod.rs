mod banner;
mod changes;
mod commit_box;
mod confirm;
mod detail;
mod diff;
mod header;
mod history;
mod load;
mod model;
mod sidebar;
mod sync;
mod undo;
mod watch;

use std::time::Instant;

use desk_ui::components::card::inner_card;
use desk_ui::components::empty::empty_state;
use desk_ui::components::overlay::toast;
use desk_ui::components::tree::OpenProject;
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::HAIRLINE;
use desk_ui::theme::ColorToken;
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, MouseButton, MouseDownEvent, MouseMoveEvent,
    MouseUpEvent, Render, Window, div, prelude::*, px,
};

use model::{Drag, Edge, GitScreen, Loaded, Told};

const LEFT_RANGE: (f32, f32) = (180.0, 360.0);
const RIGHT_RANGE: (f32, f32) = (300.0, 640.0);
const GRIP: f32 = 5.0;
const TOAST_BOTTOM: f32 = 16.0;

pub fn open(_: Option<&str>, window: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    let root = match cx.try_global::<OpenProject>() {
        Some(OpenProject(folder)) => folder.clone(),
        None => std::env::current_dir()
            .map_err(|error| format!("the git screen has no working folder: {error}"))?,
    };
    let opened = Instant::now();
    let screen = cx.new(|cx| GitScreen::new(root, window, cx));
    window.on_next_frame(move |_, _| {
        eprintln!(
            "desk: git: first frame {:.1} ms after open",
            opened.elapsed().as_secs_f64() * 1000.0
        )
    });
    Ok(screen.into())
}

fn grip(edge: Edge, width: f32, cx: &mut Context<GitScreen>) -> impl IntoElement {
    let theme = ActiveTheme::theme(cx);
    div()
        .id(match edge {
            Edge::Left => "git-grip-left",
            Edge::Right => "git-grip-right",
        })
        .flex_none()
        .w(px(GRIP))
        .h_full()
        .flex()
        .justify_center()
        .cursor_col_resize()
        .on_mouse_down(
            MouseButton::Left,
            cx.listener(move |screen, event: &MouseDownEvent, _, cx| {
                screen.drag = Some(Drag {
                    edge,
                    from_x: event.position.x.as_f32(),
                    from_width: width,
                });
                cx.stop_propagation();
            }),
        )
        .child(
            div()
                .w(px(HAIRLINE))
                .h_full()
                .bg(theme.color(ColorToken::Separator)),
        )
}

impl GitScreen {
    fn dragged(&mut self, event: &MouseMoveEvent, _: &mut Window, cx: &mut Context<Self>) {
        let Some(drag) = self.drag else {
            return;
        };
        let moved = event.position.x.as_f32() - drag.from_x;
        match drag.edge {
            Edge::Left => self.left = (drag.from_width + moved).clamp(LEFT_RANGE.0, LEFT_RANGE.1),
            Edge::Right => {
                self.right = (drag.from_width - moved).clamp(RIGHT_RANGE.0, RIGHT_RANGE.1)
            }
        }
        cx.notify();
    }
}

impl Render for GitScreen {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        if let Loaded::Failed(why) = &self.git {
            return empty_state(
                "git-not-a-repository",
                "Not a git repository",
                Some(format!("{}: {why}", self.root.display()).into()),
                &[],
                &[],
                &theme,
                |_, _, _| {},
            )
            .into_any_element();
        }
        let done = match &self.told {
            Some(Told::Done(said)) => Some(said.clone()),
            Some(Told::Failed { .. }) | None => None,
        };
        let body = div()
            .flex()
            .flex_1()
            .min_h_0()
            .child(
                div()
                    .flex_none()
                    .w(px(self.left))
                    .h_full()
                    .child(sidebar::render(self, window, cx)),
            )
            .child(grip(Edge::Left, self.left, cx))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .h_full()
                    .child(history::render(self, &theme, cx)),
            )
            .child(grip(Edge::Right, self.right, cx))
            .child(
                div()
                    .flex_none()
                    .w(px(self.right))
                    .h_full()
                    .child(changes::panel(self, window, cx)),
            );
        inner_card(&theme)
            .size_full()
            .on_mouse_move(cx.listener(Self::dragged))
            .on_mouse_up(
                MouseButton::Left,
                cx.listener(|screen, _: &MouseUpEvent, _, cx| {
                    if screen.drag.take().is_some() {
                        cx.notify();
                    }
                }),
            )
            .child(header::render(self, window, cx))
            .children(banner::render(self, window, cx))
            .child(body)
            .children(done.map(|said| {
                div()
                    .absolute()
                    .left_0()
                    .right_0()
                    .bottom(px(TOAST_BOTTOM))
                    .flex()
                    .justify_center()
                    .child(toast(
                        said.into(),
                        None,
                        &theme,
                        cx.listener(|screen, _: &ClickEvent, _, cx| {
                            screen.told = None;
                            cx.notify();
                        }),
                    ))
            }))
            .into_any_element()
    }
}
