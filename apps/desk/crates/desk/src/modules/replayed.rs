use super::chat::cassette::{Replay, Step};

use std::borrow::Cow;

use desk_core::bridge::Event;
use desk_core::model::Store;
use desk_ui::components::card::{Header, inner_card, shell};
use desk_ui::components::glyph::Glyph;
use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, Context, Entity, IntoElement, Render, Window, div, prelude::*, px,
};

const INSET: f32 = 8.0;
const FONTS: [&[u8]; 6] = [
    include_bytes!("../../../../assets/fonts/Geist-Regular.ttf"),
    include_bytes!("../../../../assets/fonts/Geist-Medium.ttf"),
    include_bytes!("../../../../assets/fonts/Geist-SemiBold.ttf"),
    include_bytes!("../../../../assets/fonts/GeistMono-Regular.ttf"),
    include_bytes!("../../../../assets/fonts/GeistMono-Medium.ttf"),
    include_bytes!("../../../../assets/fonts/GeistMono-SemiBold.ttf"),
];

type Settled = Box<dyn Fn(&mut App)>;

pub fn fonts(cx: &mut App) -> Result<(), String> {
    cx.text_system()
        .add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("the desk cannot load the Geist fonts: {error:#}"))
}

pub struct Replayed {
    store: Entity<Store>,
    replay: Option<Replay>,
    view: AnyView,
    glyph: Glyph,
    title: &'static str,
    width: f32,
    settled: Settled,
}

pub fn whole() -> Result<Vec<Event>, String> {
    let mut replay = Replay::read()?;
    let mut events = Vec::new();
    while let Step::Feed(event) = replay.step()? {
        events.push(event);
    }
    Ok(events)
}

pub fn feed(store: &Entity<Store>, events: &[Event], cx: &mut App) {
    store.update(cx, |store, cx| {
        for event in events {
            if let Err(error) = store.apply_batch(std::slice::from_ref(event)) {
                eprintln!("desk: the store refused an event from the cassette: {error}");
            }
        }
        store.open = store.sessions.keys().next().cloned();
        cx.notify();
    });
}

pub fn reset(store: &Entity<Store>, cx: &mut App) {
    store.update(cx, |store, cx| {
        *store = Store::default();
        cx.notify();
    });
}

impl Replayed {
    pub fn open(
        store: Entity<Store>,
        view: AnyView,
        (glyph, title, width): (Glyph, &'static str, f32),
        settled: impl Fn(&mut App) + 'static,
        cx: &mut App,
    ) -> Result<AnyView, String> {
        let replay = Replay::read()?;
        Ok(cx
            .new(|_| Replayed {
                store,
                replay: Some(replay),
                view,
                glyph,
                title,
                width,
                settled: Box::new(settled),
            })
            .into())
    }

    fn frame(&mut self, window: &mut Window, cx: &mut Context<Self>) -> Result<(), String> {
        let Some(replay) = &mut self.replay else {
            return Ok(());
        };
        window.request_animation_frame();
        match replay.step()? {
            Step::Feed(event) => feed(&self.store, &[event], cx),
            Step::Restart => reset(&self.store, cx),
            Step::Report => {
                self.replay = None;
                reset(&self.store, cx);
                feed(&self.store, &whole()?, cx);
                (self.settled)(cx);
            }
        }
        Ok(())
    }
}

impl Render for Replayed {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if let Err(error) = self.frame(window, cx) {
            eprintln!("desk: the cassette has a bad line: {error}");
            cx.quit();
        }
        let theme = ActiveTheme::theme(cx);
        let tile = shell(
            Header::Title(Some(self.glyph), self.title.into(), None),
            &theme,
        )
        .w(px(self.width))
        .h_full()
        .child(
            inner_card(&theme)
                .flex_1()
                .min_h_0()
                .child(self.view.clone()),
        );
        div()
            .size_full()
            .flex()
            .justify_center()
            .p(px(INSET))
            .child(tile)
            .children(self.replay.as_ref().map(Replay::meter))
    }
}
