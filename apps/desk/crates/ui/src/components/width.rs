use gpui::{App, Entity, Pixels, SharedString, Window, canvas, prelude::*, px};

pub const NARROW_BELOW: f32 = 560.0;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Fit {
    Wide,
    Narrow,
}

#[derive(Clone)]
pub struct Width(Entity<Option<Pixels>>);

impl Width {
    pub fn of(key: impl Into<SharedString>, window: &mut Window, cx: &mut App) -> Self {
        Width(window.use_keyed_state(key.into(), cx, |_, _| None))
    }

    pub fn get(&self, cx: &App) -> Option<Pixels> {
        *self.0.read(cx)
    }

    pub fn fit(&self, cx: &App) -> Fit {
        match self.get(cx) {
            Some(width) if width < px(NARROW_BELOW) => Fit::Narrow,
            _ => Fit::Wide,
        }
    }

    pub fn record(&self, width: Pixels, cx: &mut App) {
        self.0.update(cx, |known, cx| {
            if *known != Some(width) {
                *known = Some(width);
                cx.notify();
            }
        });
    }

    pub fn probe(&self) -> impl IntoElement {
        let width = self.clone();
        canvas(
            move |bounds, _, cx| width.record(bounds.size.width, cx),
            |_, _, _, _| {},
        )
        .absolute()
        .top_0()
        .left_0()
        .size_full()
    }
}
