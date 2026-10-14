use draw_doc::Element;
use serde_json::Value;

pub const FONT_SIZE: f64 = 20.0;
pub const LINE_HEIGHT: f64 = 1.25;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Kind {
    Rectangle,
    Ellipse,
    Diamond,
    Line,
    Arrow,
    Freedraw,
    Text,
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
            _ => Kind::Other,
        }
    }

    pub fn resizable(self) -> bool {
        match self {
            Kind::Rectangle | Kind::Ellipse | Kind::Diamond | Kind::Other => true,
            Kind::Line | Kind::Arrow | Kind::Freedraw | Kind::Text => false,
        }
    }

    pub fn traced(self) -> bool {
        match self {
            Kind::Line | Kind::Arrow | Kind::Freedraw => true,
            Kind::Rectangle | Kind::Ellipse | Kind::Diamond | Kind::Text | Kind::Other => false,
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
    let kind = Kind::of(element);
    if kind.traced() {
        return trace(element)
            .windows(2)
            .any(|pair| matches!(pair, [a, b] if to_segment((x, y), *a, *b) <= slack));
    }
    let bounds = element.bounds();
    x >= bounds.min_x - slack
        && x <= bounds.max_x + slack
        && y >= bounds.min_y - slack
        && y <= bounds.max_y + slack
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
