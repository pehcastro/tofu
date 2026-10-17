use draw_doc::{ArrowEnd, Binding, BoundKind, DocError, Element, Op, Scene};
use gpui::CursorStyle;
use serde_json::{Value, json};

use crate::shape::{self, Kind};

const LEAST_SIDE: f64 = 8.0;
pub const LABEL_PAD: f64 = 16.0;
const LABEL_LEAST: f64 = 51.0;
const LEAST_BEND: f64 = 8.0;
const ELBOW_MIDDLE: f64 = 0.5;
const ELBOW_FIELD: &str = "elbowMidPoint";

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

    pub fn corner(self) -> bool {
        self.across != Pull::Middle && self.down != Pull::Middle
    }

    pub fn along(self, element: &Element) -> (f64, f64) {
        let half = |pull: Pull, span: f64| match pull {
            Pull::Middle => span / 2.0,
            Pull::Least | Pull::Most => 0.0,
        };
        (
            half(self.across, element.width),
            half(self.down, element.height),
        )
    }

    pub fn cursor(self) -> CursorStyle {
        match (self.across, self.down) {
            (Pull::Least, Pull::Least) | (Pull::Most, Pull::Most) => {
                CursorStyle::ResizeUpLeftDownRight
            }
            (Pull::Most, Pull::Least) | (Pull::Least, Pull::Most) => {
                CursorStyle::ResizeUpRightDownLeft
            }
            (Pull::Middle, _) => CursorStyle::ResizeUpDown,
            (_, Pull::Middle) => CursorStyle::ResizeLeftRight,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Side {
    Top,
    Right,
    Bottom,
    Left,
}

impl Side {
    pub const ALL: [Side; 4] = [Side::Top, Side::Right, Side::Bottom, Side::Left];

    fn share(self) -> (f64, f64) {
        match self {
            Side::Top => (0.5, 0.0),
            Side::Right => (1.0, 0.5),
            Side::Bottom => (0.5, 1.0),
            Side::Left => (0.0, 0.5),
        }
    }

    fn normal(self) -> (f64, f64) {
        match self {
            Side::Top => (0.0, -1.0),
            Side::Right => (1.0, 0.0),
            Side::Bottom => (0.0, 1.0),
            Side::Left => (-1.0, 0.0),
        }
    }

    pub fn of(self, element: &Element, gap: f64) -> (f64, f64) {
        let ((sx, sy), (nx, ny)) = (self.share(), self.normal());
        (
            element.x + element.width * sx + nx * gap,
            element.y + element.height * sy + ny * gap,
        )
    }
}

pub fn elbowed(arrow: &Element) -> bool {
    arrow.extra.get("elbowed").and_then(Value::as_bool) == Some(true)
}

pub fn midpoint(arrow: &Element) -> f64 {
    shape::number(arrow, ELBOW_FIELD)
        .unwrap_or(ELBOW_MIDDLE)
        .clamp(0.0, 1.0)
}

pub fn elbow(start: (f64, f64), end: (f64, f64), across: bool, share: f64) -> Vec<(f64, f64)> {
    let (mx, my) = (
        start.0 + (end.0 - start.0) * share,
        start.1 + (end.1 - start.1) * share,
    );
    match across {
        true => vec![start, (mx, start.1), (mx, end.1), end],
        false => vec![start, (start.0, my), (end.0, my), end],
    }
}

pub fn across(leaving: Option<[f64; 2]>, start: (f64, f64), end: (f64, f64)) -> bool {
    match leaving {
        Some([x, _]) if !(0.0..=1.0).contains(&x) => true,
        Some([_, y]) if !(0.0..=1.0).contains(&y) => false,
        _ => (end.0 - start.0).abs() >= (end.1 - start.1).abs(),
    }
}

fn leaving(arrow: &Element) -> Option<[f64; 2]> {
    match arrow.binding(ArrowEnd::Start) {
        Ok(Some(Binding::Fixed { fixed_point, .. })) => Some(fixed_point),
        Ok(Some(Binding::Legacy { .. }) | None) | Err(_) => None,
    }
}

fn normal(a: (f64, f64), b: (f64, f64)) -> Option<((f64, f64), (f64, f64))> {
    let (dx, dy) = (b.0 - a.0, b.1 - a.1);
    let length = dx.hypot(dy);
    (length > f64::EPSILON).then(|| {
        (
            ((a.0 + b.0) / 2.0, (a.1 + b.1) / 2.0),
            (-dy / length, dx / length),
        )
    })
}

pub fn bend_of(a: (f64, f64), through: (f64, f64), b: (f64, f64)) -> f64 {
    normal(a, b).map_or(0.0, |((mx, my), (px, py))| {
        (through.0 - mx) * px + (through.1 - my) * py
    })
}

pub fn bent(a: (f64, f64), b: (f64, f64), bend: f64) -> Vec<(f64, f64)> {
    match normal(a, b) {
        Some(((mx, my), (px, py))) if bend.abs() >= LEAST_BEND => {
            vec![a, (mx + px * bend, my + py * bend), b]
        }
        _ => vec![a, b],
    }
}

pub fn bend_point(arrow: &Element, least: f64) -> Option<(f64, f64)> {
    match (elbowed(arrow), shape::trace(arrow).as_slice()) {
        (true, [_, a, b, _]) if (b.0 - a.0).hypot(b.1 - a.1) >= least => {
            Some(((a.0 + b.0) / 2.0, (a.1 + b.1) / 2.0))
        }
        (true, _) => None,
        (false, [a, b]) => Some(((a.0 + b.0) / 2.0, (a.1 + b.1) / 2.0)),
        (false, [_, through, _]) => Some(*through),
        (false, _) => None,
    }
}

pub fn dragged(arrow: &Element, at: (f64, f64)) -> Element {
    let trace = shape::trace(arrow);
    let (Some(&start), Some(&end)) = (trace.first(), trace.last()) else {
        return arrow.clone();
    };
    if !elbowed(arrow) {
        return shape::with_trace(arrow, &bent(start, end, bend_of(start, at, end)));
    }
    let across = across(leaving(arrow), start, end);
    let (from, to, pointer) = match across {
        true => (start.0, end.0, at.0),
        false => (start.1, end.1, at.1),
    };
    let share = match (to - from).abs() > f64::EPSILON {
        true => ((pointer - from) / (to - from)).clamp(0.0, 1.0),
        false => ELBOW_MIDDLE,
    };
    let mut moved = shape::with_trace(arrow, &elbow(start, end, across, share));
    moved.extra.insert(ELBOW_FIELD.to_owned(), json!(share));
    moved
}

pub fn tipped(arrow: &Element, end: ArrowEnd, to: (f64, f64)) -> Element {
    let before = shape::trace(arrow);
    let (Some(&first), Some(&last)) = (before.first(), before.last()) else {
        return arrow.clone();
    };
    let (start, finish) = match end {
        ArrowEnd::Start => (to, last),
        ArrowEnd::End => (first, to),
    };
    shape::with_trace(arrow, &routed(arrow, &before, start, finish))
}

pub fn tip(arrow: &Element, end: ArrowEnd) -> Option<(f64, f64)> {
    let trace = shape::trace(arrow);
    match end {
        ArrowEnd::Start => trace.first().copied(),
        ArrowEnd::End => trace.last().copied(),
    }
}

pub fn retipped(
    scene: &Scene,
    arrow: Element,
    end: ArrowEnd,
    onto: Option<&Element>,
) -> Result<Vec<Op>, DocError> {
    if let (Some(target), Some(point)) = (onto, tip(&arrow, end)) {
        let bind = Op::BindArrow {
            arrow: arrow.id.clone(),
            end,
            target: target.id.clone(),
            fixed_point: share(point, target),
        };
        return Ok(vec![Op::Update(Box::new(arrow)), bind]);
    }
    let Some(binding) = arrow.binding(end)? else {
        return Ok(vec![Op::Update(Box::new(arrow))]);
    };
    let (field, other) = match end {
        ArrowEnd::Start => ("startBinding", ArrowEnd::End),
        ArrowEnd::End => ("endBinding", ArrowEnd::Start),
    };
    let held = arrow
        .binding(other)?
        .is_some_and(|kept| kept.element_id() == binding.element_id());
    let mut freed = arrow;
    freed.extra.insert(field.to_owned(), Value::Null);
    let old = scene
        .get(binding.element_id())
        .filter(|_| !held)
        .map(|target| {
            let mut target = target.clone();
            if let Some(Value::Array(list)) = target.extra.get_mut("boundElements") {
                list.retain(|bound| bound.get("id").and_then(Value::as_str) != Some(&freed.id));
            }
            Op::Update(Box::new(target))
        });
    Ok(std::iter::once(Op::Update(Box::new(freed)))
        .chain(old)
        .collect())
}

pub fn routed(
    arrow: &Element,
    before: &[(f64, f64)],
    start: (f64, f64),
    end: (f64, f64),
) -> Vec<(f64, f64)> {
    if elbowed(arrow) {
        return elbow(
            start,
            end,
            across(leaving(arrow), start, end),
            midpoint(arrow),
        );
    }
    match before {
        [a, through, b] => bent(start, end, bend_of(*a, *through, *b)),
        [_, middle @ .., _] => std::iter::once(start)
            .chain(middle.iter().copied())
            .chain(std::iter::once(end))
            .collect(),
        _ => vec![start, end],
    }
}

pub fn share(point: (f64, f64), on: &Element) -> [f64; 2] {
    [
        (point.0 - on.x) / on.width.max(f64::EPSILON),
        (point.1 - on.y) / on.height.max(f64::EPSILON),
    ]
}

pub fn updates(changed: Vec<Element>) -> Vec<Op> {
    changed
        .into_iter()
        .map(|element| Op::Update(Box::new(element)))
        .collect()
}

pub fn moved(scene: &Scene, from: &[Element], by: (f64, f64)) -> Result<Vec<Element>, DocError> {
    let shifted = from
        .iter()
        .map(|element| Element {
            x: element.x + by.0,
            y: element.y + by.1,
            ..element.clone()
        })
        .collect();
    follow(scene, shifted)
}

pub fn resized(
    scene: &Scene,
    from: &Element,
    handle: Handle,
    to: (f64, f64),
    keep_aspect: bool,
    (widest, tall): (f64, &dyn Fn(f64) -> f64),
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
    let width = (from.width * scale_x).max(LEAST_SIDE).max(widest);
    let height = (from.height * scale_y).max(LEAST_SIDE).max(tall(width));
    let element = Element {
        x: handle.across.place(from.x, from.width, width, to.0),
        y: handle.down.place(from.y, from.height, height, to.1),
        width,
        height,
        ..from.clone()
    };
    follow(scene, vec![element])
}

pub fn retexted(from: &Element, shown: &str, original: &str, width: f64) -> Element {
    let mut element = Element {
        width,
        height: shape::line_step(from) * shown.split('\n').count() as f64,
        ..from.clone()
    };
    for (field, text) in [("text", shown), ("originalText", original)] {
        element
            .extra
            .insert(field.to_owned(), Value::String(text.to_owned()));
    }
    element
}

pub fn grown(host: &Element, label_height: f64) -> Element {
    let mut grown = host.clone();
    if grown.width < LABEL_LEAST && grown.height < LABEL_LEAST {
        let side = grown.width.max(grown.height).max(LABEL_LEAST);
        (grown.width, grown.height) = (side, side);
    }
    grown.height = grown.height.max(label_height + LABEL_PAD * 2.0);
    grown
}

pub fn hosting(host: &Element, label: &str) -> Element {
    let mut host = host.clone();
    let mut bound = match host.extra.get("boundElements") {
        Some(Value::Array(list)) => list.clone(),
        _ => Vec::new(),
    };
    bound.push(json!({ "id": label, "type": "text" }));
    host.extra
        .insert("boundElements".to_owned(), Value::Array(bound));
    host
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

pub fn centred(text: &Element, host: &Element) -> Element {
    let bounds = host.bounds();
    Element {
        x: (bounds.min_x + bounds.max_x - text.width) / 2.0,
        y: (bounds.min_y + bounds.max_y - text.height) / 2.0,
        ..text.clone()
    }
}

fn rebound(arrow: &Element, changed: &[Element]) -> Result<Element, DocError> {
    let before = shape::trace(arrow);
    let mut trace = before.clone();
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
    let (Some(&start), Some(&end)) = (trace.first(), trace.last()) else {
        return Ok(arrow.clone());
    };
    Ok(shape::with_trace(
        arrow,
        &routed(arrow, &before, start, end),
    ))
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
        Kind::Rectangle
        | Kind::Line
        | Kind::Arrow
        | Kind::Freedraw
        | Kind::Text
        | Kind::Image
        | Kind::Other => 1.0 / rx.max(ry),
    };
    let out = reach * length + gap;
    (middle.0 + dx / length * out, middle.1 + dy / length * out)
}
