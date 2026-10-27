use std::ops::Range;
use std::rc::Rc;

use desk_core::model::Store;
use desk_ui::components::chat::hit_marks;
use desk_ui::components::transcript::{Transcript, transcript};
use desk_ui::live::ActiveTheme;
use gpui::{
    AnyElement, App, AppContext, Context, Entity, IntoElement, Render, StyleRefinement,
    Subscription, WeakEntity, Window, div, prelude::*, px,
};

use super::{CONTENT_WIDTH, Chat, ROW_INSET, head, items};

#[derive(Clone, Copy, PartialEq, Eq)]
pub(super) enum Seat {
    Chat,
    Mirror,
}

pub(super) struct Feed {
    id: &'static str,
    seat: Seat,
    chat: WeakEntity<Chat>,
    transcript: Transcript,
    forks: Option<i64>,
    _watched: [Subscription; 2],
}

impl Feed {
    pub(super) fn new(
        id: &'static str,
        seat: Seat,
        chat: WeakEntity<Chat>,
        (store, count): (&Entity<Store>, usize),
        cx: &mut App,
    ) -> Entity<Feed> {
        cx.new(|cx| {
            let watched = [
                cx.observe(store, |feed: &mut Feed, store, cx| {
                    let forks = head::forks(store.read(cx));
                    if forks != feed.forks {
                        feed.forks = forks;
                        cx.notify();
                    }
                }),
                cx.observe_global::<ActiveTheme>(|_, cx| cx.notify()),
            ];
            Feed {
                id,
                seat,
                chat,
                transcript: Transcript::new(count).content_width(px(CONTENT_WIDTH)),
                forks: head::forks(store.read(cx)),
                _watched: watched,
            }
        })
    }

    pub(super) fn cached(feed: &Entity<Feed>) -> impl IntoElement + use<> {
        feed.clone()
            .cached(StyleRefinement::default().size_full().min_h_0())
    }

    pub(super) fn splice(&mut self, old: Range<usize>, count: usize, cx: &mut Context<Self>) {
        self.transcript.splice(old, count);
        cx.notify();
    }

    pub(super) fn reset(&mut self, count: usize, cx: &mut Context<Self>) {
        self.transcript.reset(count);
        cx.notify();
    }

    pub(super) fn reveal(&mut self, item: usize, cx: &mut Context<Self>) {
        self.transcript.reveal(item);
        cx.notify();
    }
}

impl Render for Feed {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        desk_perf::span!(self.id);
        let Some(chat) = self.chat.upgrade() else {
            return div().into_any_element();
        };
        if self.seat == Seat::Chat {
            let visible = self.transcript.visible();
            chat.update(cx, |chat, cx| chat.want_page(visible, window, cx));
        }
        let theme = ActiveTheme::theme(cx);
        let chat = chat.read(cx);
        let items = chat.items.clone();
        let (hits, current) = match self.seat {
            Seat::Chat => (chat.hits.clone(), chat.current),
            Seat::Mirror => (Rc::default(), 0),
        };
        let forks = self.forks;
        transcript(
            self.id,
            &self.transcript,
            &theme,
            move |at, window, cx| -> AnyElement {
                let theme = ActiveTheme::theme(cx);
                items.get(at).map_or_else(
                    || div().into_any_element(),
                    |item| {
                        let marks = hit_marks(&hits, current, at, &theme);
                        div()
                            .px(px(ROW_INSET))
                            .child(items::render(item, at, &marks, forks, &theme, window, cx))
                            .into_any_element()
                    },
                )
            },
        )
    }
}
