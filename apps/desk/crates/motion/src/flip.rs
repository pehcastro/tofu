use std::collections::HashMap;
use std::time::Instant;

use gpui::{ElementId, Pixels, Point, SpringState, point, px};

use crate::presence::{Phase, Presence};
use crate::tokens::{HOVER, PANEL_IN_MS, PANEL_OUT_MS};

const SETTLED_PX: f32 = 0.25;
const JUMP_PX: f32 = 1.0;
const AT_REST: SpringState = SpringState {
    position: 0.0,
    velocity: 0.0,
};

#[derive(Clone, Copy, Debug)]
struct Card {
    anchor: Point<Pixels>,
    origin: Point<Pixels>,
    offset: [SpringState; 2],
    since: Instant,
    fade: Presence,
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Placed {
    pub offset: Option<Point<Pixels>>,
    pub moving: bool,
}

#[derive(Debug, Default)]
pub struct FlipTracker {
    reduced: bool,
    primed: bool,
    cards: HashMap<ElementId, Card>,
}

impl FlipTracker {
    pub fn set_reduced(&mut self, on: bool) {
        self.reduced = on;
    }

    pub fn opacity(&self, id: &ElementId, now: Instant) -> f32 {
        match self.cards.get(id) {
            Some(card) => card.fade.progress(now),
            None if self.primed => 0.0,
            None => 1.0,
        }
    }

    pub fn place(
        &mut self,
        id: &ElementId,
        origin: Point<Pixels>,
        shift: Point<Pixels>,
        now: Instant,
    ) -> Placed {
        let anchor = origin - shift;
        let Some(card) = self.cards.get_mut(id) else {
            let fade = match self.primed {
                true => {
                    let mut fade = Presence::new(PANEL_IN_MS, PANEL_OUT_MS);
                    fade.set_open(true, now);
                    fade
                }
                false => Presence::shown(PANEL_IN_MS, PANEL_OUT_MS),
            };
            let card = Card {
                anchor,
                origin,
                offset: [AT_REST; 2],
                since: now,
                fade,
            };
            self.cards.insert(id.clone(), card);
            return Placed {
                offset: None,
                moving: self.primed,
            };
        };
        if card.fade.phase(now) == Phase::Closed {
            card.fade.set_open(true, now);
        }
        let mut offset = travel(card, self.reduced, now);
        let moved = anchor - card.anchor;
        if moved.x.abs().as_f32() > JUMP_PX || moved.y.abs().as_f32() > JUMP_PX {
            let back = card.origin - origin;
            for (state, back) in offset.iter_mut().zip([back.x, back.y]) {
                state.position += back.as_f32();
            }
            card.offset = if self.reduced { [AT_REST; 2] } else { offset };
            offset = card.offset;
            card.since = now;
            card.anchor = anchor;
        }
        card.origin = origin;
        let settled = offset
            .iter()
            .all(|state| HOVER.is_settled(*state, 0.0, SETTLED_PX));
        let [x, y] = offset.map(|state| px(state.position));
        Placed {
            offset: (!settled).then_some(point(x, y)),
            moving: !settled || matches!(card.fade.phase(now), Phase::Opening | Phase::Closing),
        }
    }

    pub fn painted(&mut self) {
        self.primed = true;
    }

    pub fn leave(&mut self, id: &ElementId, now: Instant) {
        if let Some(card) = self.cards.get_mut(id) {
            card.fade.set_open(false, now);
        }
    }

    pub fn leaving(&self, id: &ElementId, now: Instant) -> bool {
        self.cards
            .get(id)
            .is_some_and(|card| card.fade.phase(now) == Phase::Closing)
    }
}

fn travel(card: &Card, reduced: bool, now: Instant) -> [SpringState; 2] {
    if reduced {
        return [AT_REST; 2];
    }
    let elapsed = now.saturating_duration_since(card.since).as_secs_f32();
    card.offset.map(|state| HOVER.step(state, 0.0, elapsed))
}
