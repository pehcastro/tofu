use draw_doc::{Element, Rect};
use serde_json::Value;

pub const FONT_SIZE: f64 = 20.0;
pub const LINE_HEIGHT: f64 = 1.25;
const ELLIPSE_STEPS: u32 = 32;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Kind {
    Rectangle,
    Ellipse,
    Diamond,
    Line,
    Arrow,
    Freedraw,
    Text,
    Image,
    Other,
}

impl Kind {
    pub fn of(element: &Element) -> Kind {
        Kind::of_name(&element.kind)
    }

    pub fn of_name(name: &str) -> Kind {
        match name {
            "rectangle" => Kind::Rectangle,
            "ellipse" => Kind::Ellipse,
            "diamond" => Kind::Diamond,
            "line" => Kind::Line,
            "arrow" => Kind::Arrow,
            "freedraw" => Kind::Freedraw,
            "text" => Kind::Text,
            "image" => Kind::Image,
            _ => Kind::Other,
        }
    }

    pub fn resizable(self) -> bool {
        match self {
            Kind::Rectangle | Kind::Ellipse | Kind::Diamond | Kind::Image | Kind::Other => true,
            Kind::Line | Kind::Arrow | Kind::Freedraw | Kind::Text => false,
        }
    }

    pub fn labelled(self) -> bool {
        match self {
            Kind::Rectangle | Kind::Ellipse | Kind::Diamond => true,
            Kind::Line | Kind::Arrow | Kind::Freedraw | Kind::Text | Kind::Image | Kind::Other => {
                false
            }
        }
    }

    pub fn traced(self) -> bool {
        match self {
            Kind::Line | Kind::Arrow | Kind::Freedraw => true,
            Kind::Rectangle
            | Kind::Ellipse
            | Kind::Diamond
            | Kind::Text
            | Kind::Image
            | Kind::Other => false,
        }
    }
}

pub fn word<'a>(element: &'a Element, field: &str) -> Option<&'a str> {
    element.extra.get(field).and_then(Value::as_str)
}

pub fn number(element: &Element, field: &str) -> Option<f64> {
    element.extra.get(field).and_then(Value::as_f64)
}

pub fn label(element: &Element) -> Option<&str> {
    word(element, "text")
}

pub fn raw(element: &Element) -> Option<&str> {
    word(element, "originalText").or_else(|| label(element))
}

pub fn container(element: &Element) -> Option<&str> {
    word(element, "containerId")
}

pub fn font_size(element: &Element) -> f64 {
    number(element, "fontSize").unwrap_or(FONT_SIZE)
}

pub fn line_step(element: &Element) -> f64 {
    font_size(element) * number(element, "lineHeight").unwrap_or(LINE_HEIGHT)
}

pub fn trace(element: &Element) -> Vec<(f64, f64)> {
    element
        .points
        .iter()
        .flatten()
        .map(|[x, y]| (element.x + x, element.y + y))
        .collect()
}

pub fn with_trace(element: &Element, trace: &[(f64, f64)]) -> Element {
    let Some(&(x, y)) = trace.first() else {
        return element.clone();
    };
    let points: Vec<[f64; 2]> = trace.iter().map(|(px, py)| [px - x, py - y]).collect();
    let span = |pick: fn(&[f64; 2]) -> f64| {
        let (least, most) = points
            .iter()
            .map(pick)
            .fold((0.0_f64, 0.0_f64), |(a, b), v| (a.min(v), b.max(v)));
        most - least
    };
    Element {
        x,
        y,
        width: span(|p| p[0]),
        height: span(|p| p[1]),
        points: Some(points),
        ..element.clone()
    }
}

pub fn hit(element: &Element, x: f64, y: f64, slack: f64) -> bool {
    if element.is_deleted == Some(true) {
        return false;
    }
    if Kind::of(element).traced() {
        return near(&trace(element), (x, y), slack);
    }
    let bounds = element.bounds();
    x >= bounds.min_x - slack
        && x <= bounds.max_x + slack
        && y >= bounds.min_y - slack
        && y <= bounds.max_y + slack
}

pub fn filled(element: &Element) -> bool {
    match Kind::of(element) {
        Kind::Text | Kind::Image => true,
        Kind::Line | Kind::Arrow | Kind::Freedraw => false,
        Kind::Rectangle | Kind::Ellipse | Kind::Diamond | Kind::Other => {
            word(element, "backgroundColor").is_some_and(|color| color != "transparent")
        }
    }
}

pub fn outline(element: &Element) -> Vec<(f64, f64)> {
    let (left, top) = (element.x, element.y);
    let (right, bottom) = (left + element.width, top + element.height);
    let (mx, my) = ((left + right) / 2.0, (top + bottom) / 2.0);
    match Kind::of(element) {
        Kind::Line | Kind::Arrow | Kind::Freedraw => trace(element),
        Kind::Diamond => vec![(mx, top), (right, my), (mx, bottom), (left, my), (mx, top)],
        Kind::Ellipse => (0..=ELLIPSE_STEPS)
            .map(|step| {
                let turn = std::f64::consts::TAU * f64::from(step) / f64::from(ELLIPSE_STEPS);
                (
                    mx + element.width / 2.0 * turn.cos(),
                    my + element.height / 2.0 * turn.sin(),
                )
            })
            .collect(),
        Kind::Rectangle | Kind::Text | Kind::Image | Kind::Other => vec![
            (left, top),
            (right, top),
            (right, bottom),
            (left, bottom),
            (left, top),
        ],
    }
}

pub fn pressed(element: &Element, x: f64, y: f64, slack: f64) -> bool {
    if element.is_deleted == Some(true) {
        return false;
    }
    let outline = outline(element);
    near(&outline, (x, y), slack) || (filled(element) && inside(&outline, (x, y)))
}

pub fn brushed(element: &Element, brush: &Rect) -> bool {
    if element.is_deleted == Some(true) {
        return false;
    }
    let bounds = element.bounds();
    if !bounds.intersects(brush) {
        return false;
    }
    if brush.min_x <= bounds.min_x
        && bounds.max_x <= brush.max_x
        && brush.min_y <= bounds.min_y
        && bounds.max_y <= brush.max_y
    {
        return true;
    }
    let outline = outline(element);
    let corners = [
        (brush.min_x, brush.min_y),
        (brush.max_x, brush.min_y),
        (brush.max_x, brush.max_y),
        (brush.min_x, brush.max_y),
    ];
    let edges = [
        (corners[0], corners[1]),
        (corners[1], corners[2]),
        (corners[2], corners[3]),
        (corners[3], corners[0]),
    ];
    edges.into_iter().any(|(a, b)| {
        outline
            .windows(2)
            .any(|pair| matches!(pair, [c, d] if crossing((a, b), (*c, *d))))
    }) || (filled(element) && corners.into_iter().any(|corner| inside(&outline, corner)))
}

fn near(path: &[(f64, f64)], p: (f64, f64), slack: f64) -> bool {
    path.windows(2)
        .any(|pair| matches!(pair, [a, b] if to_segment(p, *a, *b) <= slack))
}

fn inside(polygon: &[(f64, f64)], (x, y): (f64, f64)) -> bool {
    polygon.windows(2).fold(false, |inside, pair| match pair {
        [(ax, ay), (bx, by)]
            if (*ay > y) != (*by > y) && x < ax + (y - ay) * (bx - ax) / (by - ay) =>
        {
            !inside
        }
        _ => inside,
    })
}

type Segment = ((f64, f64), (f64, f64));

fn crossing((a, b): Segment, (c, d): Segment) -> bool {
    let turn = |p: (f64, f64), q: (f64, f64), r: (f64, f64)| {
        (q.0 - p.0) * (r.1 - p.1) - (q.1 - p.1) * (r.0 - p.0)
    };
    turn(c, d, a) * turn(c, d, b) <= 0.0 && turn(a, b, c) * turn(a, b, d) <= 0.0
}

fn to_segment(p: (f64, f64), a: (f64, f64), b: (f64, f64)) -> f64 {
    let (dx, dy) = (b.0 - a.0, b.1 - a.1);
    let length = dx * dx + dy * dy;
    let t = match length > 0.0 {
        true => (((p.0 - a.0) * dx + (p.1 - a.1) * dy) / length).clamp(0.0, 1.0),
        false => 0.0,
    };
    (p.0 - a.0 - t * dx).hypot(p.1 - a.1 - t * dy)
}
