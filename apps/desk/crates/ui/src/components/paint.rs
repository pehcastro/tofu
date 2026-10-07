use std::time::Duration;

use desk_motion::spin;
use desk_motion::tokens::{PRESS, PRESS_MS};
use gpui::{
    AnimationExt, App, BoxShadow, Div, ElementId, KeyDownEvent, Motion, Rgba, Stateful, Svg,
    Transformation, Window, div, percentage, point, prelude::*, px, rgb_to_hsla, rgba, svg,
};

use crate::components::glyph::{Glyph, MARK_BLEED, spinner_path};
use crate::components::size::{PRESS_FILL, RING_STROKE, RISE};
use crate::theme::{ColorToken, NumberToken, Theme};

pub fn ink(theme: &Theme, alpha: f32) -> Rgba {
    tint(theme.color(ColorToken::TextStrong), alpha)
}

pub fn tint(color: Rgba, alpha: f32) -> Rgba {
    Rgba { alpha, ..color }
}

pub fn solid(over: Rgba, base: Rgba) -> Rgba {
    let mix = |top: f32, bottom: f32| top * over.alpha + bottom * (1.0 - over.alpha);
    Rgba::new(
        mix(over.red, base.red),
        mix(over.green, base.green),
        mix(over.blue, base.blue),
        1.0,
    )
}

pub fn ms(theme: &Theme, token: NumberToken) -> Duration {
    Duration::from_secs_f32(theme.number(token) / 1000.0)
}

pub fn ring(color: Rgba) -> BoxShadow {
    BoxShadow {
        color: color.into(),
        offset: point(px(0.0), px(0.0)),
        blur_radius: px(0.0),
        spread_radius: px(1.0),
        inset: true,
    }
}

pub fn focus_ring(element: Stateful<Div>, theme: &Theme) -> Stateful<Div> {
    let color = theme.color(ColorToken::FocusRing);
    element
        .tab_index(0)
        .focus_visible(move |style| style.shadow(vec![ring(color)]))
}

pub fn arrowed(event: &KeyDownEvent, at: usize, count: usize) -> Option<usize> {
    let keys = &event.keystroke;
    match (keys.modifiers.modified(), keys.key.as_str()) {
        (false, "left") => Some(at.saturating_sub(1)),
        (false, "right") => Some(at.saturating_add(1).min(count.saturating_sub(1))),
        _ => None,
    }
}

pub fn halo(color: Rgba, spread: f32) -> BoxShadow {
    BoxShadow {
        color: color.into(),
        offset: point(px(0.0), px(0.0)),
        blur_radius: px(0.0),
        spread_radius: px(spread),
        inset: false,
    }
}

pub fn drop(color: Rgba, y: f32, blur: f32) -> BoxShadow {
    BoxShadow {
        color: color.into(),
        offset: point(px(0.0), px(y)),
        blur_radius: px(blur),
        spread_radius: px(0.0),
        inset: false,
    }
}

pub fn top_light(color: Rgba) -> BoxShadow {
    BoxShadow {
        color: color.into(),
        offset: point(px(0.0), px(1.0)),
        blur_radius: px(0.0),
        spread_radius: px(0.0),
        inset: true,
    }
}

pub fn glyph(glyph: Glyph, size: f32, color: Rgba) -> Svg {
    svg()
        .path(glyph.path())
        .size(px(size))
        .flex_none()
        .text_color(rgb_to_hsla(color))
}

pub fn presented(element: Div, progress: f32, reduced: bool) -> Div {
    let rise = if reduced { 0.0 } else { RISE };
    element.opacity(progress).mt(px(rise * (1.0 - progress)))
}

pub fn spinning_arc(
    id: impl Into<ElementId>,
    size: f32,
    track: Rgba,
    arc: Rgba,
    theme: &Theme,
) -> impl IntoElement {
    Spinner::new(id, size, track, arc, theme)
}

#[derive(IntoElement)]
pub struct Spinner {
    id: ElementId,
    size: f32,
    track: Rgba,
    mark: Rgba,
    period: Duration,
}

impl Spinner {
    pub fn new(
        id: impl Into<ElementId>,
        size: f32,
        track: Rgba,
        mark: Rgba,
        theme: &Theme,
    ) -> Self {
        Self {
            id: id.into(),
            size,
            track,
            mark,
            period: ms(theme, NumberToken::MotionSpin),
        }
    }
}

impl RenderOnce for Spinner {
    fn render(self, _: &mut Window, _: &mut App) -> impl IntoElement {
        let inset = -RING_STROKE - MARK_BLEED;
        div()
            .relative()
            .flex_none()
            .size(px(self.size))
            .rounded_full()
            .border(px(RING_STROKE))
            .border_color(self.track)
            .child(
                svg()
                    .absolute()
                    .top(px(inset))
                    .left(px(inset))
                    .path(spinner_path(self.size))
                    .size(px(self.size + 2.0 * MARK_BLEED))
                    .text_color(self.mark)
                    .with_animation(self.id, spin(self.period), |mark, t| {
                        mark.with_transformation(Transformation::rotate(percentage(t)))
                    }),
            )
    }
}

const PRESS_SINK: f32 = 1.0;
const PRESS_SHADE: f32 = 0.16;

pub fn pressed(element: Stateful<Div>, _backdrop: Rgba) -> Stateful<Div> {
    let spring = || Motion::new(PRESS_MS).with_spring(PRESS);
    let shade = |alpha| tint(rgba(0x000000ff), alpha);
    sinkable(element)
        .inset_ring(px(PRESS_FILL))
        .inset_ring_color(shade(0.0))
        .transitions(|transitions| transitions.top(spring()).inset_ring_color(spring()))
        .active(move |style| {
            style
                .top(px(PRESS_SINK))
                .inset_ring_color(shade(PRESS_SHADE))
        })
}

fn sinkable(mut element: Stateful<Div>) -> Stateful<Div> {
    if element.style().inset.top.is_none() {
        element = element.top(px(0.0));
    }
    element
}
