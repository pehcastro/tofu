use draw_doc::{ArrowEnd, Binding, BoundKind, DocError, Element, Op, Scene};
use serde_json::Value;

use crate::shape::{self, Kind};

const LEAST_SIDE: f64 = 1.0;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Corner {
    TopLeft,
    TopRight,
    BottomLeft,
    BottomRight,
}

impl Corner {
    pub const ALL: [Corner; 4] = [
        Corner::TopLeft,
        Corner::TopRight,
        Corner::BottomLeft,
        Corner::BottomRight,
    ];

    pub fn of(self, element: &Element) -> (f64, f64) {
        let (left, top) = (element.x, element.y);
        let (right, bottom) = (left + element.width, top + element.height);
        match self {
            Corner::TopLeft => (left, top),
            Corner::TopRight => (right, top),
            Corner::BottomLeft => (left, bottom),
            Corner::BottomRight => (right, bottom),
        }
    }

    fn opposite(self) -> Corner {
        match self {
            Corner::TopLeft => Corner::BottomRight,
            Corner::TopRight => Corner::BottomLeft,
            Corner::BottomLeft => Corner::TopRight,
            Corner::BottomRight => Corner::TopLeft,
        }
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
    corner: Corner,
    to: (f64, f64),
) -> Result<Vec<Element>, DocError> {
    let (fixed_x, fixed_y) = corner.opposite().of(from);
    let element = Element {
        x: fixed_x.min(to.0),
        y: fixed_y.min(to.1),
        width: (fixed_x - to.0).abs().max(LEAST_SIDE),
        height: (fixed_y - to.1).abs().max(LEAST_SIDE),
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

fn edge(target: &Element, toward: (f64, f64), gap: f64) -> (f64, f64) {
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
