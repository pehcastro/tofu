use draw_doc::{ArrowEnd, Binding, BoundKind, DocError, Element, Op, Scene};
use serde_json::Value;

use crate::shape::{self, Kind};

const LEAST_SIDE: f64 = 8.0;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Pull {
    Least,
    Middle,
    Most,
}

impl Pull {
    fn share(self) -> f64 {
        match self {
            Pull::Least => 0.0,
            Pull::Middle => 0.5,
            Pull::Most => 1.0,
        }
    }

    fn reach(self, start: f64, span: f64, to: f64) -> Option<(f64, f64)> {
        let fixed = match self {
            Pull::Least => start + span,
            Pull::Most => start,
            Pull::Middle => return None,
        };
        Some((fixed, to - fixed))
    }

    fn place(self, start: f64, span: f64, side: f64, to: f64) -> f64 {
        match self.reach(start, span, to) {
            Some((fixed, toward)) if toward < 0.0 => fixed - side,
            Some((fixed, _)) => fixed,
            None => start + (span - side) / 2.0,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Handle {
    across: Pull,
    down: Pull,
}

impl Handle {
    pub const ALL: [Handle; 8] = [
        Handle::new(Pull::Least, Pull::Least),
        Handle::new(Pull::Middle, Pull::Least),
        Handle::new(Pull::Most, Pull::Least),
        Handle::new(Pull::Most, Pull::Middle),
        Handle::new(Pull::Most, Pull::Most),
        Handle::new(Pull::Middle, Pull::Most),
        Handle::new(Pull::Least, Pull::Most),
        Handle::new(Pull::Least, Pull::Middle),
    ];

    const fn new(across: Pull, down: Pull) -> Handle {
        Handle { across, down }
    }

    pub fn of(self, element: &Element) -> (f64, f64) {
        (
            element.x + element.width * self.across.share(),
            element.y + element.height * self.down.share(),
        )
    }
}

pub fn updates(changed: Vec<Element>) -> Vec<Op> {
    changed
        .into_iter()
        .map(|element| Op::Update(Box::new(element)))
        .collect()
}

pub fn moved(scene: &Scene, from: &Element, by: (f64, f64)) -> Result<Vec<Element>, DocError> {
    let element = Element {
        x: from.x + by.0,
        y: from.y + by.1,
        ..from.clone()
    };
    follow(scene, vec![element])
}

pub fn resized(
    scene: &Scene,
    from: &Element,
    handle: Handle,
    to: (f64, f64),
    keep_aspect: bool,
) -> Result<Vec<Element>, DocError> {
    let pulled = |pull: Pull, start: f64, span: f64, to: f64| {
        pull.reach(start, span, to)
            .map(|(_, toward)| toward.abs() / span.max(f64::EPSILON))
    };
    let across = pulled(handle.across, from.x, from.width, to.0);
    let down = pulled(handle.down, from.y, from.height, to.1);
    let (scale_x, scale_y) = match (keep_aspect, across, down) {
        (true, Some(x), Some(y)) => (x.max(y), x.max(y)),
        (true, Some(x), None) => (x, x),
        (true, None, Some(y)) => (y, y),
        (_, x, y) => (x.unwrap_or(1.0), y.unwrap_or(1.0)),
    };
    let width = (from.width * scale_x).max(LEAST_SIDE);
    let height = (from.height * scale_y).max(LEAST_SIDE);
    let element = Element {
        x: handle.across.place(from.x, from.width, width, to.0),
        y: handle.down.place(from.y, from.height, height, to.1),
        width,
        height,
        ..from.clone()
    };
    follow(scene, vec![element])
}

pub fn retexted(
    scene: &Scene,
    from: &Element,
    text: &str,
    width: f64,
) -> Result<Vec<Element>, DocError> {
    let mut element = Element {
        width,
        height: shape::line_step(from) * text.split('\n').count() as f64,
        ..from.clone()
    };
    for field in ["text", "originalText"] {
        element
            .extra
            .insert(field.to_owned(), Value::String(text.to_owned()));
    }
    if let Some(host) = shape::container(&element).and_then(|id| scene.get(id)) {
        element = centred(&element, host);
    }
    Ok(vec![element])
}

fn follow(scene: &Scene, changed: Vec<Element>) -> Result<Vec<Element>, DocError> {
    let mut riders: Vec<(String, BoundKind)> = Vec::new();
    for element in &changed {
        for bound in element.bound_elements()? {
            let seen = riders.iter().any(|(id, _)| *id == bound.id)
                || changed.iter().any(|other| other.id == bound.id);
            if !seen {
                riders.push((bound.id, bound.kind));
            }
        }
    }
    let mut ridden = Vec::with_capacity(riders.len());
    for (id, kind) in riders {
        let Some(rider) = scene.get(&id) else {
            continue;
        };
        match kind {
            BoundKind::Text => {
                let host = shape::container(rider)
                    .and_then(|host| changed.iter().find(|element| element.id == host));
                if let Some(host) = host {
                    ridden.push(centred(rider, host));
                }
            }
            BoundKind::Arrow => ridden.push(rebound(rider, &changed)?),
        }
    }
    Ok(changed.into_iter().chain(ridden).collect())
}

fn centred(text: &Element, host: &Element) -> Element {
    let bounds = host.bounds();
    Element {
        x: (bounds.min_x + bounds.max_x - text.width) / 2.0,
        y: (bounds.min_y + bounds.max_y - text.height) / 2.0,
        ..text.clone()
    }
}

fn rebound(arrow: &Element, changed: &[Element]) -> Result<Element, DocError> {
    let mut trace = shape::trace(arrow);
    let last = trace.len().saturating_sub(1);
    if last == 0 {
        return Ok(arrow.clone());
    }
    for (end, at, neighbour) in [
        (ArrowEnd::Start, 0, 1.min(last)),
        (ArrowEnd::End, last, last.saturating_sub(1)),
    ] {
        let Some(binding) = arrow.binding(end)? else {
            continue;
        };
        let Some(target) = changed
            .iter()
            .find(|element| element.id == binding.element_id())
        else {
            continue;
        };
        let toward = trace
            .get(neighbour)
            .copied()
            .unwrap_or((target.x, target.y));
        let point = match binding {
            Binding::Fixed { fixed_point, .. } => (
                target.x + fixed_point[0] * target.width,
                target.y + fixed_point[1] * target.height,
            ),
            Binding::Legacy { gap, .. } => edge(target, toward, gap),
        };
        if let Some(slot) = trace.get_mut(at) {
            *slot = point;
        }
    }
    Ok(shape::with_trace(arrow, &trace))
}

pub fn edge(target: &Element, toward: (f64, f64), gap: f64) -> (f64, f64) {
    let (a, b) = (target.width / 2.0, target.height / 2.0);
    let middle = (target.x + a, target.y + b);
    let (dx, dy) = (toward.0 - middle.0, toward.1 - middle.1);
    let length = dx.hypot(dy);
    if length <= f64::EPSILON || a <= 0.0 || b <= 0.0 {
        return middle;
    }
    let (rx, ry) = ((dx / a).abs(), (dy / b).abs());
    let reach = match Kind::of(target) {
        Kind::Ellipse => 1.0 / rx.hypot(ry),
        Kind::Diamond => 1.0 / (rx + ry),
        Kind::Rectangle | Kind::Line | Kind::Arrow | Kind::Freedraw | Kind::Text | Kind::Other => {
            1.0 / rx.max(ry)
        }
    };
    let out = reach * length + gap;
    (middle.0 + dx / length * out, middle.1 + dy / length * out)
}
