use std::panic::Location;
use std::time::Instant;

use desk_motion::{FlipTracker, Placed};
use gpui::{
    AnyElement, App, Bounds, ElementId, Empty, Entity, GlobalElementId, InspectorElementId,
    LayoutId, Pixels, Window, prelude::*,
};

const FLYING_PRIORITY: usize = 0;

#[derive(IntoElement)]
pub struct Flip<E: IntoElement + Styled + 'static> {
    id: ElementId,
    tracker: Entity<FlipTracker>,
    child: E,
}

pub fn flip<E: IntoElement + Styled + 'static>(
    id: impl Into<ElementId>,
    tracker: &Entity<FlipTracker>,
    child: E,
) -> Flip<E> {
    Flip {
        id: id.into(),
        tracker: tracker.clone(),
        child,
    }
}

impl<E: IntoElement + Styled + 'static> RenderOnce for Flip<E> {
    fn render(self, _: &mut Window, cx: &mut App) -> impl IntoElement {
        let opacity = self.tracker.read(cx).opacity(&self.id, Instant::now());
        let child = match opacity < 1.0 {
            true => self.child.opacity(opacity),
            false => self.child,
        };
        Flying {
            id: self.id,
            tracker: self.tracker,
            child: child.into_any_element(),
        }
    }
}

struct Flying {
    id: ElementId,
    tracker: Entity<FlipTracker>,
    child: AnyElement,
}

impl IntoElement for Flying {
    type Element = Self;

    fn into_element(self) -> Self {
        self
    }
}

impl Element for Flying {
    type RequestLayoutState = ();
    type PrepaintState = bool;

    fn id(&self) -> Option<ElementId> {
        None
    }

    fn source_location(&self) -> Option<&'static Location<'static>> {
        None
    }

    fn request_layout(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        window: &mut Window,
        cx: &mut App,
    ) -> (LayoutId, ()) {
        (self.child.request_layout(window, cx), ())
    }

    fn prepaint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        bounds: Bounds<Pixels>,
        _: &mut (),
        window: &mut Window,
        cx: &mut App,
    ) -> bool {
        let shift = window.element_offset();
        let reduced = cx.reduce_motion();
        let Placed { offset, moving } = self.tracker.update(cx, |tracker, _| {
            tracker.set_reduced(reduced);
            tracker.place(&self.id, bounds.origin, shift, Instant::now())
        });
        if moving {
            window.request_animation_frame();
        }
        let Some(offset) = offset else {
            self.child.prepaint(window, cx);
            return true;
        };
        let child = std::mem::replace(&mut self.child, Empty.into_any_element());
        window.defer_draw(child, shift + offset, FLYING_PRIORITY, None);
        false
    }

    fn paint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        _: Bounds<Pixels>,
        _: &mut (),
        inline: &mut bool,
        window: &mut Window,
        cx: &mut App,
    ) {
        self.tracker.update(cx, |tracker, _| tracker.painted());
        if *inline {
            self.child.paint(window, cx);
        }
    }
}
