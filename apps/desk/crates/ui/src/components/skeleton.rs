use std::f32::consts::TAU;
use std::time::{Duration, Instant};

use gpui::{
    Animation, AnimationExt, AnyElement, App, Div, SharedString, div, prelude::*, px, relative,
};

use crate::components::paint::ink;
use crate::motion::tokens::EASE_OUT;
use crate::theme::Theme;

const FILL: f32 = 0.09;
const DIM: f32 = 0.5;
const PULSE_MS: u64 = 1500;
const STAGGER_MS: u64 = 200;
const LINE: f32 = 10.0;
const LINE_GAP: f32 = 12.0;
const BLOCK_RADIUS: f32 = 10.0;
const LINE_WIDTHS: [f32; 6] = [0.86, 0.62, 0.74, 0.45, 0.8, 0.56];

pub fn skeleton_bar(width: f32, height: f32, theme: &Theme) -> Div {
    div()
        .flex_none()
        .w(relative(width))
        .h(px(height))
        .rounded(px(height.min(BLOCK_RADIUS * 2.0) / 2.0))
        .bg(ink(theme, FILL))
}

pub fn skeleton_block(height: f32, theme: &Theme) -> Div {
    div()
        .flex_none()
        .w_full()
        .h(px(height))
        .rounded(px(BLOCK_RADIUS))
        .bg(ink(theme, FILL))
}

pub fn pulse(id: impl Into<SharedString>, step: usize, shape: Div, cx: &App) -> AnyElement {
    if cx.reduce_motion() {
        return shape.into_any_element();
    }
    let offset = (step as u64 * STAGGER_MS % PULSE_MS) as f32 / PULSE_MS as f32;
    let id: SharedString = id.into();
    shape
        .id((id.clone(), step))
        .with_animation(
            (id, step),
            Animation::new(Duration::from_millis(PULSE_MS)).repeat_synced(),
            move |shape, t| {
                let wave = 0.5 - 0.5 * ((t + offset).fract() * TAU).cos();
                shape.opacity(DIM + (1.0 - DIM) * wave)
            },
        )
        .into_any_element()
}

pub const SKELETON_HOLD: Duration = Duration::from_millis(2000);
const REVEAL: Duration = Duration::from_millis(150);

pub enum Stage {
    Skeleton,
    Wake(Duration),
    Reveal(usize, Option<Duration>),
    Shown,
}

#[derive(Default)]
pub struct Hold {
    since: Option<Instant>,
    woke: bool,
    revealed: Option<Instant>,
    reveals: usize,
}

impl Hold {
    pub fn wait(&mut self, now: Instant) {
        self.since.get_or_insert(now);
        self.woke = false;
    }

    pub fn stage(&mut self, ready: bool, now: Instant) -> Stage {
        if !ready {
            self.wait(now);
            return Stage::Skeleton;
        }
        if let Some(since) = self.since {
            let held = now.saturating_duration_since(since);
            if held < SKELETON_HOLD {
                if self.woke {
                    return Stage::Skeleton;
                }
                self.woke = true;
                return Stage::Wake(SKELETON_HOLD - held);
            }
            self.since = None;
            self.revealed = Some(now);
            self.reveals += 1;
            return Stage::Reveal(self.reveals, Some(held));
        }
        match self.revealed {
            Some(at) if now.saturating_duration_since(at) < REVEAL => {
                Stage::Reveal(self.reveals, None)
            }
            Some(_) | None => Stage::Shown,
        }
    }
}

pub fn reveal(id: impl Into<SharedString>, step: usize, shown: AnyElement) -> AnyElement {
    let id: SharedString = id.into();
    div()
        .id((id.clone(), step))
        .flex()
        .flex_col()
        .child(shown)
        .with_animation(
            (id, step),
            Animation::new(REVEAL).with_easing(EASE_OUT),
            |shown, t| shown.opacity(t),
        )
        .into_any_element()
}

pub fn skeleton_lines(id: impl Into<SharedString>, lines: usize, theme: &Theme, cx: &App) -> Div {
    let id = id.into();
    div()
        .flex()
        .flex_col()
        .gap(px(LINE_GAP))
        .children((0..lines).map(|at| {
            let width = LINE_WIDTHS[at % LINE_WIDTHS.len()];
            pulse(id.clone(), at, skeleton_bar(width, LINE, theme), cx)
        }))
}
