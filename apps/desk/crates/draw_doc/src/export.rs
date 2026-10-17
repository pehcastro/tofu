use std::collections::HashMap;

use serde_json::Value;

use crate::edit::union;
use crate::element::{ArrowEnd, BoundKind, Element, Rect};
use crate::{DocError, Scene};

const PAD: f64 = 16.0;
const EMPTY: f64 = 100.0;
const PAPER: &str = "#ffffff";
const INK: &str = "#1e1e1e";
const FONT: f64 = 20.0;
const LINE_HEIGHT: f64 = 1.25;
const BASELINE: f64 = 0.85;
const ROUND_SHARE: f64 = 0.25;
const ROUND_MOST: f64 = 32.0;
const HEAD_LENGTH: f64 = 15.0;
const HEAD_SPREAD: f64 = 0.45;
const PATTERN_FILL: f64 = 0.5;
const FONT_FAMILY: &str = "Segoe UI, Helvetica, Arial, sans-serif";

fn word<'a>(element: &'a Element, field: &str) -> Option<&'a str> {
    element.extra.get(field).and_then(Value::as_str)
}

fn number(element: &Element, field: &str) -> Option<f64> {
    element.extra.get(field).and_then(Value::as_f64)
}

fn live(scene: &Scene) -> impl Iterator<Item = &Element> {
    scene
        .elements()
        .iter()
        .filter(|element| element.is_deleted != Some(true))
}

fn escaped(text: &str) -> String {
    let mut out = String::with_capacity(text.len());
    for c in text.chars() {
        match c {
            '&' => out.push_str("&amp;"),
            '<' => out.push_str("&lt;"),
            '>' => out.push_str("&gt;"),
            '"' => out.push_str("&quot;"),
            '\'' => out.push_str("&#39;"),
            c => out.push(c),
        }
    }
    out
}

fn paint(element: &Element, filled: bool) -> String {
    let stroke = match word(element, "strokeColor").unwrap_or(INK) {
        "transparent" => "none",
        color => color,
    };
    let fill = match word(element, "backgroundColor") {
        Some(color) if filled && color != "transparent" => color,
        _ => "none",
    };
    let width = number(element, "strokeWidth").unwrap_or(1.0);
    let dash = match word(element, "strokeStyle") {
        Some("dashed") => format!(" stroke-dasharray=\"{} {}\"", width * 4.0, width * 4.0),
        Some("dotted") => format!(" stroke-dasharray=\"{} {}\"", width, width * 2.0),
        _ => String::new(),
    };
    let patterned = !matches!(word(element, "fillStyle"), Some("solid") | None);
    let fill_opacity = match patterned && fill != "none" {
        true => format!(" fill-opacity=\"{PATTERN_FILL}\""),
        false => String::new(),
    };
    let opacity = number(element, "opacity").unwrap_or(100.0) / 100.0;
    format!(
        "stroke=\"{}\" stroke-width=\"{width}\" fill=\"{}\"{fill_opacity}{dash} opacity=\"{opacity}\" stroke-linecap=\"round\" stroke-linejoin=\"round\"{}",
        escaped(stroke),
        escaped(fill),
        turned(element)
    )
}

fn turned(element: &Element) -> String {
    match element.angle.unwrap_or(0.0) {
        0.0 => String::new(),
        angle => format!(
            " transform=\"rotate({} {} {})\"",
            angle.to_degrees(),
            element.x + element.width / 2.0,
            element.y + element.height / 2.0
        ),
    }
}

fn trace(element: &Element) -> Vec<(f64, f64)> {
    element
        .points
        .iter()
        .flatten()
        .map(|[x, y]| (element.x + x, element.y + y))
        .collect()
}

fn polyline(points: &[(f64, f64)]) -> String {
    points
        .iter()
        .map(|(x, y)| format!("{x},{y}"))
        .collect::<Vec<_>>()
        .join(" ")
}

fn wings(tip: (f64, f64), from: (f64, f64)) -> Option<[(f64, f64); 3]> {
    let (dx, dy) = (from.0 - tip.0, from.1 - tip.1);
    let length = dx.hypot(dy);
    if length <= f64::EPSILON {
        return None;
    }
    let reach = HEAD_LENGTH.min(length / 2.0);
    let (ux, uy) = (dx / length, dy / length);
    let wing = |turn: f64| {
        let (sin, cos) = turn.sin_cos();
        (
            tip.0 + (ux * cos - uy * sin) * reach,
            tip.1 + (ux * sin + uy * cos) * reach,
        )
    };
    Some([wing(HEAD_SPREAD), tip, wing(-HEAD_SPREAD)])
}

fn text(element: &Element) -> String {
    let Some(written) = word(element, "text") else {
        return String::new();
    };
    let size = number(element, "fontSize").unwrap_or(FONT);
    let step = size * number(element, "lineHeight").unwrap_or(LINE_HEIGHT);
    let (anchor, x) = match word(element, "textAlign") {
        Some("center") => ("middle", element.x + element.width / 2.0),
        Some("right") => ("end", element.x + element.width),
        _ => ("start", element.x),
    };
    let color = escaped(word(element, "strokeColor").unwrap_or(INK));
    let turn = turned(element);
    written
        .split('\n')
        .enumerate()
        .map(|(row, line)| {
            let y = element.y + step * row as f64 + size * BASELINE;
            format!(
                "<text x=\"{x}\" y=\"{y}\" font-size=\"{size}\" font-family=\"{FONT_FAMILY}\" text-anchor=\"{anchor}\" fill=\"{color}\"{turn}>{}</text>\n",
                escaped(line)
            )
        })
        .collect()
}

fn traced(element: &Element) -> String {
    let points = trace(element);
    let look = paint(element, false);
    let ends = [
        ("startArrowhead", points.first(), points.get(1)),
        (
            "endArrowhead",
            points.last(),
            points.len().checked_sub(2).and_then(|at| points.get(at)),
        ),
    ];
    let heads = ends
        .into_iter()
        .filter_map(|(field, tip, from)| {
            word(element, field)?;
            wings(*tip?, *from?)
        })
        .map(|head| format!("<polyline points=\"{}\" {look}/>\n", polyline(&head)));
    std::iter::once(format!(
        "<polyline points=\"{}\" {look}/>\n",
        polyline(&points)
    ))
    .chain(heads)
    .collect()
}

fn shape(element: &Element) -> String {
    let (x, y, w, h) = (element.x, element.y, element.width, element.height);
    match element.kind.as_str() {
        "rectangle" | "frame" => {
            let rounded = element.extra.get("roundness").is_some_and(|r| !r.is_null());
            let radius = match rounded {
                true => (w.min(h) * ROUND_SHARE).min(ROUND_MOST),
                false => 0.0,
            };
            format!(
                "<rect x=\"{x}\" y=\"{y}\" width=\"{w}\" height=\"{h}\" rx=\"{radius}\" {}/>\n",
                paint(element, true)
            )
        }
        "ellipse" => format!(
            "<ellipse cx=\"{}\" cy=\"{}\" rx=\"{}\" ry=\"{}\" {}/>\n",
            x + w / 2.0,
            y + h / 2.0,
            w / 2.0,
            h / 2.0,
            paint(element, true)
        ),
        "diamond" => format!(
            "<polygon points=\"{}\" {}/>\n",
            polyline(&[
                (x + w / 2.0, y),
                (x + w, y + h / 2.0),
                (x + w / 2.0, y + h),
                (x, y + h / 2.0)
            ]),
            paint(element, true)
        ),
        "line" | "arrow" | "freedraw" => traced(element),
        "image" => element
            .extra
            .get("customData")
            .and_then(|data| data.get("file"))
            .and_then(Value::as_str)
            .map(|file| {
                format!(
                    "<image href=\"{}\" x=\"{x}\" y=\"{y}\" width=\"{w}\" height=\"{h}\" preserveAspectRatio=\"none\"{}/>\n",
                    escaped(file),
                    turned(element)
                )
            })
            .unwrap_or_default(),
        "text" => text(element),
        _ => String::new(),
    }
}

pub fn svg(scene: &Scene, ground: Option<&str>) -> String {
    let frame = union(live(scene).map(Element::bounds)).map_or(
        Rect {
            min_x: 0.0,
            min_y: 0.0,
            max_x: EMPTY,
            max_y: EMPTY,
        },
        |around| Rect {
            min_x: around.min_x - PAD,
            min_y: around.min_y - PAD,
            max_x: around.max_x + PAD,
            max_y: around.max_y + PAD,
        },
    );
    let (left, top) = (frame.min_x, frame.min_y);
    let (width, height) = (frame.max_x - left, frame.max_y - top);
    let mut out = format!(
        "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"{left} {top} {width} {height}\" width=\"{width}\" height=\"{height}\">\n<rect x=\"{left}\" y=\"{top}\" width=\"{width}\" height=\"{height}\" fill=\"{}\"/>\n",
        escaped(ground.unwrap_or(PAPER))
    );
    out.extend(live(scene).map(shape));
    out.push_str("</svg>\n");
    out
}

pub fn html(scene: &Scene, title: &str, ground: Option<&str>) -> String {
    format!(
        "<!doctype html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n<title>{}</title>\n<style>body{{margin:0;min-height:100vh;display:grid;place-items:center;background:{}}}svg{{max-width:100vw;max-height:100vh;height:auto}}</style>\n</head>\n<body>\n{}</body>\n</html>\n",
        escaped(title),
        escaped(ground.unwrap_or(PAPER)),
        svg(scene, ground)
    )
}

fn quoted(text: &str) -> String {
    let mut out = String::from("\"");
    for c in text.chars() {
        match c {
            '"' => out.push_str("#quot;"),
            '<' => out.push_str("#lt;"),
            '>' => out.push_str("#gt;"),
            '&' => out.push_str("#amp;"),
            '\n' => out.push_str("<br/>"),
            c => out.push(c),
        }
    }
    if text.trim().is_empty() {
        out.push(' ');
    }
    out.push('"');
    out
}

fn label<'a>(scene: &'a Scene, element: &'a Element) -> Result<&'a str, DocError> {
    if element.kind == "text" {
        return Ok(word(element, "text").unwrap_or_default());
    }
    Ok(element
        .bound_elements()?
        .iter()
        .filter(|bound| bound.kind == BoundKind::Text)
        .find_map(|bound| scene.get(&bound.id))
        .and_then(|text| word(text, "text"))
        .unwrap_or_default())
}

pub fn mermaid(scene: &Scene) -> Result<String, DocError> {
    let mut out = String::from("flowchart LR\n");
    let mut names: HashMap<&str, String> = HashMap::new();
    for element in live(scene) {
        let (open, close) = match element.kind.as_str() {
            "ellipse" => ("((", "))"),
            "diamond" => ("{", "}"),
            "rectangle" | "frame" | "image" => ("[", "]"),
            "text" if word(element, "containerId").is_none() => ("[", "]"),
            _ => continue,
        };
        let name = format!("n{}", names.len() + 1);
        out.push_str(&format!(
            "    {name}{open}{}{close}\n",
            quoted(label(scene, element)?)
        ));
        names.insert(element.id.as_str(), name);
    }
    for arrow in live(scene).filter(|element| matches!(element.kind.as_str(), "arrow" | "line")) {
        let end = |end| -> Result<Option<&String>, DocError> {
            Ok(arrow
                .binding(end)?
                .and_then(|binding| names.get(binding.element_id())))
        };
        let (Some(from), Some(to)) = (end(ArrowEnd::Start)?, end(ArrowEnd::End)?) else {
            continue;
        };
        let headed = word(arrow, "endArrowhead").is_some();
        let dashed = matches!(word(arrow, "strokeStyle"), Some("dashed" | "dotted"));
        let link = match (dashed, headed) {
            (false, true) => "-->",
            (false, false) => "---",
            (true, true) => "-.->",
            (true, false) => "-.-",
        };
        let said = label(scene, arrow)?;
        out.push_str(&match said.trim().is_empty() {
            true => format!("    {from} {link} {to}\n"),
            false => format!("    {from} {link}|{}| {to}\n", quoted(said)),
        });
    }
    Ok(out)
}
