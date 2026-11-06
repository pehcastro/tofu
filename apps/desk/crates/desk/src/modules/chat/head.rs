use std::time::Duration;

use chrono::{DateTime, Utc};
use desk_core::model::Store;
use desk_core::sessions::SessionRow;
use desk_ui::components::avatar::spinner;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::paint::{glyph, ink};
use desk_ui::components::size::{FONT_SMALL, HOVER, RADIUS_CHIP, T2, T3, TRACE};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::ICON_SMALL;
use gpui::{
    App, ClipboardItem, Context, Entity, IntoElement, Render, Task, Window, div, prelude::*, px,
};

const TICK: Duration = Duration::from_secs(1);
const COPY: &str = "Copy the session name";
const FORKS: &str = "forks in this session's line";
const FORKS_SCREEN: &str = "forks";
const OPENING: &str = "Opening session";
const NONE_OPEN: &str = "No session open";
const PENDING: &str = "--";
const DURATION_WIDTH: f32 = 48.0;

fn forks_in(generations: Option<i64>) -> i64 {
    generations.map_or(0, |line| line.saturating_sub(1).max(0))
}

pub(super) fn forks(store: &Store) -> Option<i64> {
    store
        .info
        .read
        .as_ref()
        .filter(|read| store.open.as_ref() == Some(&read.value.id))
        .map(|read| forks_in(read.value.generations))
}

pub struct SessionHead {
    store: Entity<Store>,
    row: Option<SessionRow>,
    loading: Option<String>,
    _tick: Task<()>,
}

struct Known {
    handle: String,
    generation: Option<i64>,
    at: String,
}

impl Known {
    fn count(&self) -> String {
        self.generation
            .or_else(|| self.handle.rsplit_once('.')?.1.parse().ok())
            .map_or_else(|| PENDING.to_owned(), |generation| generation.to_string())
    }
}

impl SessionHead {
    pub fn new(store: Entity<Store>, cx: &mut Context<Self>) -> Self {
        let tick = cx.spawn(async move |this, cx| {
            loop {
                cx.background_executor().timer(TICK).await;
                let ticked = this.update(cx, |head, cx| {
                    if head.row.is_some() || head.store.read(cx).info.read.is_some() {
                        cx.notify();
                    }
                });
                if ticked.is_err() {
                    return;
                }
            }
        });
        SessionHead {
            store,
            row: None,
            loading: None,
            _tick: tick,
        }
    }

    pub(super) fn loading(&mut self, loading: Option<String>, cx: &mut Context<Self>) {
        if self.loading != loading {
            self.loading = loading;
            cx.notify();
        }
    }

    pub fn show(&mut self, row: SessionRow, cx: &mut Context<Self>) {
        self.row = Some(row);
        cx.notify();
    }

    fn known(&self, store: &Store) -> Option<Known> {
        let row = self
            .row
            .as_ref()
            .filter(|row| store.open.as_ref().is_none_or(|open| *open == row.id));
        let wanted = store.open.as_ref().or(row.map(|row| &row.id))?;
        match (&store.info.read, row) {
            (Some(read), _) if read.value.id == *wanted => Some(Known {
                handle: read.value.handle.clone(),
                generation: read.value.generation,
                at: read.value.at.clone(),
            }),
            (_, Some(row)) => Some(Known {
                handle: row.handle.clone(),
                generation: row.generation,
                at: row.at.clone(),
            }),
            (_, None) => None,
        }
    }
}

fn open_forks(window: &mut Window, cx: &mut App) {
    let Some(Some(desk)) = window.root::<crate::desk::Desk>() else {
        return eprintln!("desk: chat head is not inside the desk, so it cannot open forks");
    };
    eprintln!("desk: chat head opens forks");
    window.defer(cx, move |window, cx| {
        if let Err(error) = desk.update(cx, |desk, cx| desk.open_screen(FORKS_SCREEN, window, cx)) {
            eprintln!("desk: chat head could not open forks: {error}");
        }
    });
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
        let store = self.store.read(cx);
        let info = self.known(store);
        let name = info
            .as_ref()
            .map(|info| info.handle.clone())
            .or_else(|| store.open.clone());
        let title = match &name {
            Some(name) => div().child(name.clone()),
            None => div().text_color(ink(&theme, T3)).child(match self.row {
                Some(_) => OPENING,
                None => NONE_OPEN,
            }),
        };
        let dim = ink(&theme, T3);
        let forks_text = info
            .as_ref()
            .map_or_else(|| PENDING.to_owned(), Known::count);
        let took_text = info.as_ref().and_then(|info| took(&info.at));
        let pending = info.is_none();
        let hover = ink(&theme, HOVER);
        let copy = div()
            .id("chat-head-copy")
            .aria_label(COPY)
            .flex()
            .items_center()
            .justify_center()
            .size(px(TRACE))
            .rounded(px(RADIUS_CHIP))
            .when_some(name, |copy, name| {
                copy.cursor_pointer()
                    .hover(move |style| style.bg(hover))
                    .on_click(move |_, _, cx| {
                        eprintln!("desk: session head copied {name}");
                        cx.write_to_clipboard(ClipboardItem::new_string(name.clone()));
                    })
            })
            .child(glyph(Glyph::Copy, ICON_SMALL, ink(&theme, T3)));
        let forks = div()
            .id("chat-head-forks")
            .flex()
            .items_center()
            .gap_1()
            .px_1()
            .rounded(px(RADIUS_CHIP))
            .cursor_pointer()
            .hover(move |style| style.bg(hover))
            .on_click(|_, window, cx| open_forks(window, cx))
            .child(glyph(Glyph::Fork, ICON_SMALL, ink(&theme, T3)))
            .child(
                div()
                    .when(pending, |count| count.text_color(dim))
                    .child(forks_text),
            );
        let loading = self.loading.clone().map(|said| {
            tooltip(
                "chat-head-loading-tip",
                div()
                    .id("chat-head-loading")
                    .flex()
                    .items_center()
                    .child(spinner("chat-head-spinner", &theme)),
                Edge::FrameStart,
                said,
                &theme,
                window,
                cx,
            )
        });
        let duration = div()
            .min_w(px(DURATION_WIDTH))
            .when(pending, |duration| duration.text_color(dim))
            .child(took_text.unwrap_or_else(|| PENDING.to_owned()));
        div()
            .flex()
            .items_center()
            .gap_2()
            .text_size(px(FONT_SMALL))
            .text_color(ink(&theme, T2))
            .child(title)
            .children(loading)
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
            .child(duration)
    }
}
