use std::time::{Duration, Instant};

use gpui::{Bounds, Pixels, SpringState, point, px, size};

use crate::tokens::{EASE_OUT, GLIDE_MS, HOVER, HOVER_MS};

const SETTLED_PX: f32 = 0.05;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum GlideKind {
    Eased,
    Spring,
}

impl GlideKind {
    pub const ALL: &'static [Self] = &[Self::Eased, Self::Spring];

    pub fn label(self) -> &'static str {
        match self {
            Self::Eased => "glide bar, eased 220 ms",
            Self::Spring => "glide bar, spring",
        }
    }
}

#[derive(Clone, Copy, Debug)]
struct Track {
    from: [SpringState; 4],
    to: [f32; 4],
    moved: Instant,
    shade_from: f32,
    showing: bool,
    shaded: Instant,
}

#[derive(Clone, Copy, Debug)]
pub struct Glide {
    kind: GlideKind,
    reduced: bool,
    track: Option<Track>,
    moving: bool,
}

impl Glide {
    pub fn new(kind: GlideKind) -> Self {
        Self {
            kind,
            reduced: false,
            track: None,
            moving: false,
        }
    }

    pub fn kind(&self) -> GlideKind {
        self.kind
    }

    pub fn set_kind(&mut self, kind: GlideKind) {
        self.kind = kind;
    }

    pub fn set_reduced(&mut self, on: bool) {
        self.reduced = on;
    }

    pub fn retarget(&mut self, to: Bounds<Pixels>, now: Instant) {
        let to = axes(to);
        let next = match self.track {
            Some(track) if track.showing && track.to == to => return,
            Some(track) if track.showing => Track {
                from: self.rect(&track, now),
                to,
                moved: now,
                ..track
            },
            Some(track) if self.shade(&track, now) > 0.0 => Track {
                from: self.rect(&track, now),
                to,
                moved: now,
                shade_from: self.shade(&track, now),
                showing: true,
                shaded: now,
            },
            _ => Track {
                from: to.map(at_rest),
                to,
                moved: now,
                shade_from: 0.0,
                showing: true,
                shaded: now,
            },
        };
        self.track = Some(next);
        self.moving = true;
    }

    pub fn hide(&mut self, now: Instant) {
        let Some(track) = self.track.filter(|track| track.showing) else {
            return;
        };
        self.track = Some(Track {
            shade_from: self.shade(&track, now),
            showing: false,
            shaded: now,
            ..track
        });
        self.moving = true;
    }

    pub fn sample(&mut self, now: Instant) -> Option<(Bounds<Pixels>, f32)> {
        let track = self.track?;
        let rect = self.rect(&track, now);
        let placed = self.reduced
            || match self.kind {
                GlideKind::Spring => rect
                    .iter()
                    .zip(track.to)
                    .all(|(state, to)| HOVER.is_settled(*state, to, SETTLED_PX)),
                GlideKind::Eased => now.saturating_duration_since(track.moved) >= GLIDE_MS,
            };
        let faded = self.reduced || now.saturating_duration_since(track.shaded) >= HOVER_MS;
        self.moving = !(placed && faded);
        if !self.moving && !track.showing {
            return None;
        }
        let [x, y, w, h] = rect.map(|state| state.position);
        Some((
            Bounds::new(point(px(x), px(y)), size(px(w.max(0.0)), px(h.max(0.0)))),
            self.shade(&track, now).clamp(0.0, 1.0),
        ))
    }

    pub fn moving(&self) -> bool {
        self.moving
    }

    fn rect(&self, track: &Track, now: Instant) -> [SpringState; 4] {
        if self.reduced {
            return track.to.map(at_rest);
        }
        let elapsed = now.saturating_duration_since(track.moved);
        let mut rect = track.from;
        for (state, to) in rect.iter_mut().zip(track.to) {
            *state = match self.kind {
                GlideKind::Spring => HOVER.step(*state, to, elapsed.as_secs_f32()),
                GlideKind::Eased => at_rest(
                    state.position + (to - state.position) * EASE_OUT(fraction(elapsed, GLIDE_MS)),
                ),
            };
        }
        rect
    }

    fn shade(&self, track: &Track, now: Instant) -> f32 {
        let to = if track.showing { 1.0 } else { 0.0 };
        if self.reduced {
            return to;
        }
        let eased = EASE_OUT(fraction(
            now.saturating_duration_since(track.shaded),
            HOVER_MS,
        ));
        track.shade_from + (to - track.shade_from) * eased
    }
}

fn at_rest(position: f32) -> SpringState {
    SpringState {
        position,
        velocity: 0.0,
    }
}

fn fraction(elapsed: Duration, span: Duration) -> f32 {
    (elapsed.as_secs_f32() / span.as_secs_f32()).min(1.0)
}

fn axes(bounds: Bounds<Pixels>) -> [f32; 4] {
    [
        bounds.origin.x.as_f32(),
        bounds.origin.y.as_f32(),
        bounds.size.width.as_f32(),
        bounds.size.height.as_f32(),
    ]
}
