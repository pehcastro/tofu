use std::time::{Duration, Instant};

use desk_motion::tokens;
use gpui::{Div, Window, div, prelude::*, px};

const RISE: f32 = 8.0;

#[derive(Clone, Copy)]
pub(super) struct Crossfade<T> {
    shown: T,
    entered_from: f32,
    leaving: Option<(T, f32)>,
    since: Option<Instant>,
}

fn eased(since: Option<Instant>, span: Duration, now: Instant) -> f32 {
    since.map_or(1.0, |since| {
        tokens::EASE_OUT(now.saturating_duration_since(since).as_secs_f32() / span.as_secs_f32())
    })
}

impl<T: Copy + PartialEq> Crossfade<T> {
    pub(super) fn new(shown: T) -> Self {
        Self {
            shown,
            entered_from: 1.0,
            leaving: None,
            since: None,
        }
    }

    pub(super) fn shown(&self) -> T {
        self.shown
    }

    fn entering(&self, now: Instant) -> f32 {
        let from = self.entered_from;
        from + (1.0 - from) * eased(self.since, tokens::PANEL_IN_MS, now)
    }

    fn fading(&self, now: Instant) -> Option<(T, f32)> {
        let (page, from) = self.leaving?;
        let left = eased(self.since, tokens::PANEL_OUT_MS, now);
        (left < 1.0).then_some((page, from * (1.0 - left)))
    }

    pub(super) fn leaving(&self, now: Instant) -> Option<T> {
        self.fading(now).map(|(page, _)| page)
    }

    pub(super) fn switch(&mut self, to: T, now: Instant) {
        if to == self.shown {
            return;
        }
        let back = self.fading(now).filter(|(page, _)| *page == to);
        self.leaving = Some((self.shown, self.entering(now)));
        self.entered_from = back.map_or(0.0, |(_, opacity)| opacity);
        self.shown = to;
        self.since = Some(now);
    }

    pub(super) fn stage(
        &self,
        now: Instant,
        reduced: bool,
        window: &mut Window,
        entering: Div,
        leaving: Option<Div>,
    ) -> Div {
        let shown = self.entering(now);
        if shown < 1.0 {
            window.request_animation_frame();
        }
        let rise = if reduced { 0.0 } else { RISE * (1.0 - shown) };
        let fade = self.fading(now).map_or(0.0, |(_, opacity)| opacity);
        div()
            .relative()
            .children(leaving.filter(|_| fade > 0.0).map(|leaving| {
                div()
                    .absolute()
                    .top_0()
                    .left_0()
                    .right_0()
                    .opacity(fade)
                    .child(leaving)
            }))
            .child(
                div()
                    .relative()
                    .top(px(rise))
                    .opacity(shown)
                    .child(entering),
            )
    }
}
