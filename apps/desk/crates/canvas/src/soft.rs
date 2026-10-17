use std::f64::consts::{FRAC_PI_2, PI};

pub type Spot = (f64, f64);

const FNV_START: u64 = 0xcbf2_9ce4_8422_2325;
const FNV_PRIME: u64 = 0x0100_0000_01b3;
const MIX_STEP: u64 = 0x9e37_79b9_7f4a_7c15;
const MIX_A: u64 = 0xbf58_476d_1ce4_e5b9;
const MIX_B: u64 = 0x94d0_49bb_1331_11eb;
const UNIT_BITS: i32 = 53;
const CURVE_STEPS: u32 = 8;

#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Step {
    Line(Spot),
    Quad(Spot, Spot),
    Cubic(Spot, Spot, Spot),
}

impl Step {
    fn to(self) -> Spot {
        match self {
            Step::Line(to) | Step::Quad(_, to) | Step::Cubic(_, _, to) => to,
        }
    }
}

#[derive(Clone, Debug, PartialEq)]
pub struct Outline {
    pub start: Spot,
    pub steps: Vec<Step>,
    pub closed: bool,
}

pub enum Body {
    Corners {
        points: Vec<Spot>,
        closed: bool,
        round: f64,
    },
    Curve(Outline),
}

pub struct Noise(u64);

impl Noise {
    pub fn new(id: &str, pass: u64) -> Noise {
        let hash = id.bytes().fold(FNV_START, |hash, byte| {
            (hash ^ u64::from(byte)).wrapping_mul(FNV_PRIME)
        });
        Noise(hash.wrapping_add(pass))
    }

    fn next(&mut self) -> f64 {
        self.0 = self.0.wrapping_add(MIX_STEP);
        let mut mixed = self.0;
        mixed = (mixed ^ (mixed >> 30)).wrapping_mul(MIX_A);
        mixed = (mixed ^ (mixed >> 27)).wrapping_mul(MIX_B);
        mixed ^= mixed >> 31;
        (mixed >> 11) as f64 / 2f64.powi(UNIT_BITS) * 2.0 - 1.0
    }

    fn shift(&mut self, most: f64) -> Spot {
        let (dx, dy) = (self.next(), self.next());
        let length = dx.hypot(dy);
        let reach = self.next().abs().sqrt() * most;
        match length > f64::EPSILON {
            true => (dx / length * reach, dy / length * reach),
            false => (0.0, 0.0),
        }
    }
}

fn plus(a: Spot, b: Spot) -> Spot {
    (a.0 + b.0, a.1 + b.1)
}

fn toward(from: Spot, to: Spot, reach: f64) -> Spot {
    let (dx, dy) = (to.0 - from.0, to.1 - from.1);
    let length = dx.hypot(dy);
    match length > f64::EPSILON {
        true => (from.0 + dx / length * reach, from.1 + dy / length * reach),
        false => from,
    }
}

fn apart(a: Spot, b: Spot) -> f64 {
    (b.0 - a.0).hypot(b.1 - a.1)
}

fn turn(before: Spot, at: Spot, after: Spot) -> f64 {
    let (ax, ay) = (at.0 - before.0, at.1 - before.1);
    let (bx, by) = (after.0 - at.0, after.1 - at.1);
    (ax * by - ay * bx).atan2(ax * bx + ay * by).abs()
}

pub fn soften(body: &Body, wobble: f64, noise: Option<&mut Noise>) -> Outline {
    match body {
        Body::Corners {
            points,
            closed,
            round,
        } => corners(points, *closed, *round, wobble, noise),
        Body::Curve(outline) => match noise {
            Some(noise) if wobble > 0.0 => jitter(outline, wobble, noise),
            _ => outline.clone(),
        },
    }
}

fn corners(
    points: &[Spot],
    closed: bool,
    round: f64,
    wobble: f64,
    mut noise: Option<&mut Noise>,
) -> Outline {
    let points = match points {
        [first, .., last] if closed && first == last => {
            points.split_last().map_or(points, |(_, kept)| kept)
        }
        _ => points,
    };
    let count = points.len();
    let mut shaped = Vec::with_capacity(count);
    for (at, point) in points.iter().enumerate() {
        let before = match at.checked_sub(1) {
            Some(before) => points.get(before),
            None if closed => points.last(),
            None => None,
        };
        let after = match points.get(at + 1) {
            None if closed => points.first(),
            after => after,
        };
        let shortest = [before, after]
            .into_iter()
            .flatten()
            .map(|other| apart(*other, *point))
            .fold(f64::INFINITY, f64::min);
        let radius = match (before, after) {
            (Some(before), Some(after)) => {
                let bent = turn(*before, *point, *after);
                let eased = round * ((PI - bent) / (PI - FRAC_PI_2)).clamp(0.0, 1.0);
                eased.min(shortest / 4.0)
            }
            _ => 0.0,
        };
        let most = wobble.min((shortest - radius * 2.0) / 4.0).max(0.0);
        let shift = match noise.as_deref_mut() {
            Some(noise) if most > 0.0 => noise.shift(most),
            _ => (0.0, 0.0),
        };
        shaped.push((plus(*point, shift), radius));
    }
    let corner = |at: usize| -> Option<(Spot, Spot, Spot)> {
        let (point, radius) = *shaped.get(at)?;
        let before = match at.checked_sub(1) {
            Some(before) => shaped.get(before)?.0,
            None => shaped.last()?.0,
        };
        let after = shaped.get(at + 1).or_else(|| shaped.first())?.0;
        Some((
            toward(point, before, radius),
            point,
            toward(point, after, radius),
        ))
    };
    let mut steps = Vec::with_capacity(count * 2);
    let push = |steps: &mut Vec<Step>, at: usize| match corner(at) {
        Some((enter, point, leave)) if enter != point => {
            steps.push(Step::Line(enter));
            steps.push(Step::Quad(point, leave));
        }
        Some((_, point, _)) => steps.push(Step::Line(point)),
        None => {}
    };
    let start = match (closed, corner(0)) {
        (true, Some((_, _, leave))) => leave,
        _ => shaped.first().map_or((0.0, 0.0), |(point, _)| *point),
    };
    for at in 1..count {
        push(&mut steps, at);
    }
    if closed {
        push(&mut steps, 0);
    }
    Outline {
        start,
        steps,
        closed,
    }
}

fn jitter(outline: &Outline, wobble: f64, noise: &mut Noise) -> Outline {
    let first = noise.shift(wobble);
    let last = outline.steps.len().saturating_sub(1);
    let mut before = first;
    let steps = outline
        .steps
        .iter()
        .enumerate()
        .map(|(at, step)| {
            let after = match outline.closed && at == last {
                true => first,
                false => noise.shift(wobble),
            };
            let moved = match *step {
                Step::Line(to) => Step::Line(plus(to, after)),
                Step::Quad(control, to) => Step::Quad(
                    plus(
                        control,
                        ((before.0 + after.0) / 2.0, (before.1 + after.1) / 2.0),
                    ),
                    plus(to, after),
                ),
                Step::Cubic(a, b, to) => {
                    Step::Cubic(plus(a, before), plus(b, after), plus(to, after))
                }
            };
            before = after;
            moved
        })
        .collect();
    Outline {
        start: plus(outline.start, first),
        steps,
        closed: outline.closed,
    }
}

pub fn flatten(outline: &Outline) -> Vec<Spot> {
    let mut spots = vec![outline.start];
    let mut from = outline.start;
    for step in &outline.steps {
        match *step {
            Step::Line(to) => spots.push(to),
            Step::Quad(control, to) => spots.extend((1..=CURVE_STEPS).map(|n| {
                let t = f64::from(n) / f64::from(CURVE_STEPS);
                let u = 1.0 - t;
                (
                    u * u * from.0 + 2.0 * u * t * control.0 + t * t * to.0,
                    u * u * from.1 + 2.0 * u * t * control.1 + t * t * to.1,
                )
            })),
            Step::Cubic(a, b, to) => spots.extend((1..=CURVE_STEPS).map(|n| {
                let t = f64::from(n) / f64::from(CURVE_STEPS);
                let u = 1.0 - t;
                let (p, q, r, s) = (u * u * u, 3.0 * u * u * t, 3.0 * u * t * t, t * t * t);
                (
                    p * from.0 + q * a.0 + r * b.0 + s * to.0,
                    p * from.1 + q * a.1 + r * b.1 + s * to.1,
                )
            })),
        }
        from = step.to();
    }
    if outline.closed {
        spots.push(outline.start);
    }
    spots
}

pub fn hatch(polygon: &[Spot], gap: f64) -> Vec<(Spot, Spot)> {
    if gap <= 0.0 {
        return Vec::new();
    }
    let along = |(x, y): Spot| x + y;
    let (least, most) = polygon
        .iter()
        .map(|spot| along(*spot))
        .fold((f64::INFINITY, f64::NEG_INFINITY), |(a, b), v| {
            (a.min(v), b.max(v))
        });
    let mut lines = Vec::new();
    let mut level = (least / gap).floor() * gap + gap / 2.0;
    while level < most {
        let mut crossings: Vec<Spot> = polygon
            .windows(2)
            .filter_map(|pair| match pair {
                [a, b] if (along(*a) > level) != (along(*b) > level) => {
                    let t = (level - along(*a)) / (along(*b) - along(*a));
                    Some((a.0 + (b.0 - a.0) * t, a.1 + (b.1 - a.1) * t))
                }
                _ => None,
            })
            .collect();
        crossings.sort_by(|a, b| a.0.total_cmp(&b.0));
        lines.extend(crossings.as_chunks::<2>().0.iter().map(|[a, b]| (*a, *b)));
        level += gap;
    }
    lines
}
