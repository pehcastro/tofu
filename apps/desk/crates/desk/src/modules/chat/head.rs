use std::time::Duration;

use chrono::{DateTime, Utc};
use desk_core::model::Store;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::paint::{glyph, ink};
use desk_ui::components::size::{FONT_SMALL, HOVER, RADIUS_CHIP, T2, T3, TRACE};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::ICON_SMALL;
use gpui::{
    ClipboardItem, Context, Entity, IntoElement, Render, Task, Window, div, prelude::*, px,
};

const TICK: Duration = Duration::from_secs(1);
const COPY: &str = "Copy the session name";
const FORKS: &str = "forks in this session's line";

pub struct SessionHead {
    store: Entity<Store>,
    _tick: Task<()>,
}

impl SessionHead {
    pub fn new(store: Entity<Store>, cx: &mut Context<Self>) -> Self {
        let tick = cx.spawn(async move |this, cx| {
            loop {
                cx.background_executor().timer(TICK).await;
                let ticked = this.update(cx, |head, cx| {
                    if head.store.read(cx).info.read.is_some() {
                        cx.notify();
                    }
                });
                if ticked.is_err() {
                    return;
                }
            }
        });
        SessionHead { store, _tick: tick }
    }
}

fn took(since: &str) -> Option<String> {
    let seconds = Utc::now()
        .signed_duration_since(DateTime::parse_from_rfc3339(since).ok()?)
        .num_seconds()
        .max(0);
    Some(match seconds {
        0..3600 => format!("{}m {}s", seconds / 60, seconds % 60),
        _ => format!("{}h {}m", seconds / 3600, seconds % 3600 / 60),
    })
}

impl Render for SessionHead {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let Some(info) = self
            .store
            .read(cx)
            .info
            .read
            .as_ref()
            .map(|read| read.value.clone())
        else {
            return div();
        };
        let name = info.handle.clone();
        let hover = ink(&theme, HOVER);
        let copy = div()
            .id("chat-head-copy")
            .aria_label(COPY)
            .flex()
            .items_center()
            .justify_center()
            .size(px(TRACE))
            .rounded(px(RADIUS_CHIP))
            .cursor_pointer()
            .hover(move |style| style.bg(hover))
            .on_click(move |_, _, cx| {
                eprintln!("desk: chat head copied session name {name}");
                cx.write_to_clipboard(ClipboardItem::new_string(name.clone()));
            })
            .child(glyph(Glyph::Copy, ICON_SMALL, ink(&theme, T3)));
        let forks = div()
            .id("chat-head-forks")
            .flex()
            .items_center()
            .gap_1()
            .child(glyph(Glyph::Fork, ICON_SMALL, ink(&theme, T3)))
            .child(
                info.generations
                    .map_or(0, |line| line.saturating_sub(1).max(0))
                    .to_string(),
            );
        div()
            .flex()
            .items_center()
            .gap_2()
            .text_size(px(FONT_SMALL))
            .text_color(ink(&theme, T2))
            .child(info.handle.clone())
            .child(tooltip(
                "chat-head-copy-tip",
                copy,
                Edge::Frame,
                COPY,
                &theme,
                window,
                cx,
            ))
            .child(tooltip(
                "chat-head-forks-tip",
                forks,
                Edge::Frame,
                FORKS,
                &theme,
                window,
                cx,
            ))
            .children(took(&info.at))
    }
}
