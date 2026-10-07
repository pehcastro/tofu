use std::panic::Location;
use std::time::Instant;

use gpui::{
    App, Bounds, DispatchPhase, Element, ElementId, GlobalElementId, InspectorElementId,
    IntoElement, LayoutId, MouseButton, MouseDownEvent, MouseMoveEvent, MouseUpEvent, Pixels,
    Point, Position, ScrollDelta, ScrollWheelEvent, Style, Window, point,
};

use crate::profiler::Profiler;
use crate::record::{Input, InputKind, Probe};

pub(crate) struct ProbeElement;

impl IntoElement for ProbeElement {
    type Element = Self;

    fn into_element(self) -> Self::Element {
        self
    }
}

fn mouse(kind: InputKind, position: Point<Pixels>, button: Option<MouseButton>) -> Input {
    Input {
        at: Instant::now(),
        kind,
        position: Some(position),
        button,
        key: None,
        delta: None,
    }
}

impl Element for ProbeElement {
    type RequestLayoutState = ();
    type PrepaintState = ();

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
        Profiler::record(cx, |recorder| recorder.probe(Probe::Layout));
        let style = Style {
            position: Position::Absolute,
            ..Style::default()
        };
        (window.request_layout(style, [], cx), ())
    }

    fn prepaint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        _: Bounds<Pixels>,
        _: &mut (),
        _: &mut Window,
        cx: &mut App,
    ) {
        Profiler::record(cx, |recorder| recorder.probe(Probe::Prepaint));
    }

    fn paint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        _: Bounds<Pixels>,
        _: &mut (),
        _: &mut (),
        window: &mut Window,
        cx: &mut App,
    ) {
        Profiler::record(cx, |recorder| recorder.probe(Probe::Paint));
        window.on_mouse_event(|event: &MouseDownEvent, phase, _, cx| {
            if phase == DispatchPhase::Capture {
                let input = mouse(InputKind::Down, event.position, Some(event.button));
                Profiler::record(cx, |recorder| recorder.input(input));
            }
        });
        window.on_mouse_event(|event: &MouseUpEvent, phase, _, cx| {
            if phase == DispatchPhase::Capture {
                let input = mouse(InputKind::Up, event.position, Some(event.button));
                Profiler::record(cx, |recorder| recorder.input(input));
            }
        });
        window.on_mouse_event(|event: &MouseMoveEvent, phase, _, cx| {
            if phase == DispatchPhase::Capture {
                let input = mouse(InputKind::Move, event.position, event.pressed_button);
                Profiler::record(cx, |recorder| recorder.input(input));
            }
        });
        window.on_mouse_event(|event: &ScrollWheelEvent, phase, _, cx| {
            if phase == DispatchPhase::Capture {
                let delta = match event.delta {
                    ScrollDelta::Pixels(delta) => point(delta.x.as_f32(), delta.y.as_f32()),
                    ScrollDelta::Lines(delta) => delta,
                };
                let input = Input {
                    delta: Some(delta),
                    ..mouse(InputKind::Scroll, event.position, None)
                };
                Profiler::record(cx, |recorder| recorder.input(input));
            }
        });
    }
}
