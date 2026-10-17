use std::cell::RefCell;
use std::rc::Rc;
use std::time::Instant;

use gpui::{
    AnyElement, App, Bounds, Element, ElementId, Global, GlobalElementId, InspectorElementId,
    IntoElement, LayoutId, Pixels, Refineable, Style, StyleRefinement, Window,
};

#[derive(Clone, Copy)]
pub enum Phase {
    Layout,
    Compute,
    Prepaint,
    Paint,
}

impl Phase {
    fn word(self) -> &'static str {
        match self {
            Phase::Layout => "layout",
            Phase::Compute => "compute",
            Phase::Prepaint => "prepaint",
            Phase::Paint => "paint",
        }
    }
}

pub type Spans = Rc<RefCell<Vec<(&'static str, Phase, Instant, Instant)>>>;

#[derive(Default)]
struct Watched(Option<Spans>);

impl Global for Watched {}

pub fn watch(cx: &mut App) -> Spans {
    let spans = Spans::default();
    cx.set_global(Watched(Some(Rc::clone(&spans))));
    spans
}

pub fn unwatch(cx: &mut App) {
    cx.set_global(Watched(None));
}

pub fn told(spans: &Spans, since: Instant) -> String {
    let ms = |at: Instant| at.duration_since(since).as_secs_f64() * 1000.0;
    spans
        .borrow()
        .iter()
        .map(|(name, phase, start, end)| {
            format!(
                "{name} {} {:.2} ({:.2}..{:.2})",
                phase.word(),
                ms(*end) - ms(*start),
                ms(*start),
                ms(*end)
            )
        })
        .collect::<Vec<_>>()
        .join(", ")
}

pub struct Timed {
    name: &'static str,
    child: AnyElement,
}

pub fn timed(name: &'static str, child: impl IntoElement) -> Timed {
    Timed {
        name,
        child: child.into_any_element(),
    }
}

fn record(cx: &App, name: &'static str, phase: Phase, start: Instant) {
    if let Some(Watched(Some(spans))) = cx.try_global::<Watched>() {
        spans
            .borrow_mut()
            .push((name, phase, start, Instant::now()));
    }
}

impl IntoElement for Timed {
    type Element = Self;

    fn into_element(self) -> Self::Element {
        self
    }
}

impl Element for Timed {
    type RequestLayoutState = ();
    type PrepaintState = ();

    fn id(&self) -> Option<ElementId> {
        None
    }

    fn source_location(&self) -> Option<&'static core::panic::Location<'static>> {
        None
    }

    fn request_layout(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        window: &mut Window,
        cx: &mut App,
    ) -> (LayoutId, ()) {
        let start = Instant::now();
        let layout = self.child.request_layout(window, cx);
        record(cx, self.name, Phase::Layout, start);
        (layout, ())
    }

    fn prepaint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        _: Bounds<Pixels>,
        _: &mut (),
        window: &mut Window,
        cx: &mut App,
    ) {
        let start = Instant::now();
        self.child.prepaint(window, cx);
        record(cx, self.name, Phase::Prepaint, start);
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
        let start = Instant::now();
        self.child.paint(window, cx);
        record(cx, self.name, Phase::Paint, start);
    }
}

pub struct Rooted {
    name: &'static str,
    style: StyleRefinement,
    child: AnyElement,
}

pub fn rooted(name: &'static str, style: StyleRefinement, child: impl IntoElement) -> Rooted {
    Rooted {
        name,
        style,
        child: child.into_any_element(),
    }
}

impl IntoElement for Rooted {
    type Element = Self;

    fn into_element(self) -> Self::Element {
        self
    }
}

impl Element for Rooted {
    type RequestLayoutState = ();
    type PrepaintState = ();

    fn id(&self) -> Option<ElementId> {
        None
    }

    fn source_location(&self) -> Option<&'static core::panic::Location<'static>> {
        None
    }

    fn request_layout(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        window: &mut Window,
        cx: &mut App,
    ) -> (LayoutId, ()) {
        let mut style = Style::default();
        style.refine(&self.style);
        (window.request_layout(style, None, cx), ())
    }

    fn prepaint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        bounds: Bounds<Pixels>,
        _: &mut (),
        window: &mut Window,
        cx: &mut App,
    ) {
        let start = Instant::now();
        self.child.layout_as_root(bounds.size.into(), window, cx);
        record(cx, self.name, Phase::Compute, start);
        self.child.prepaint_at(bounds.origin, window, cx);
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
        self.child.paint(window, cx);
    }
}
