use std::collections::HashMap;

use serde_json::Value;

use crate::diagram::{self, shape, text_in, ui};
use crate::element::Element;
use crate::layout::{BOX, GAP, Placed, layered};
use crate::{DocError, Edge, Graph, Line, Node, NodeKind, Scene};

const GROUP_PAD: f64 = 16.0;
const GROUP_TITLE: f64 = 28.0;
const TITLE_INSET: (f64, f64) = (12.0, 6.0);
const RIGHT_GAP: (f64, f64) = (140.0, 48.0);
const SHAPES: [(&str, &str, NodeKind); 7] = [
    ("([", "])", NodeKind::Terminal),
    ("((", "))", NodeKind::Terminal),
    ("{{", "}}", NodeKind::Step),
    ("[(", ")]", NodeKind::Step),
    ("[", "]", NodeKind::Step),
    ("(", ")", NodeKind::Step),
    ("{", "}", NodeKind::Decision),
];
const UNSUPPORTED: [&str; 6] = [
    "classDef",
    "class",
    "style",
    "linkStyle",
    "click",
    "direction",
];

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Direction {
    Down,
    Right,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Look {
    Sketch,
    Ui,
}

#[derive(Clone, Debug, PartialEq)]
pub struct Flow {
    pub graph: Graph,
    pub direction: Direction,
}

fn fail(line: usize, problem: impl Into<String>) -> DocError {
    DocError::Flow {
        line,
        problem: problem.into(),
    }
}

#[derive(Default)]
struct Reader {
    direction: Option<Direction>,
    seen: bool,
    graph: Graph,
    index: HashMap<String, usize>,
    group: Option<(String, usize)>,
}

pub fn flow(text: &str) -> Result<Flow, DocError> {
    let mut reader = Reader::default();
    for (at, raw) in text.lines().enumerate() {
        let trimmed = raw.trim();
        if trimmed.starts_with("%%") {
            continue;
        }
        for statement in statements(trimmed) {
            reader.statement(statement.trim(), at + 1)?;
        }
    }
    if let Some((_, opened)) = reader.group {
        return Err(fail(opened, "the subgraph is not closed with end"));
    }
    if reader.graph.nodes.is_empty() {
        return Err(fail(0, "a flow needs at least one node"));
    }
    Ok(Flow {
        graph: reader.graph,
        direction: reader.direction.unwrap_or(Direction::Down),
    })
}

fn statements(text: &str) -> Vec<&str> {
    let (mut out, mut start, mut depth, mut quoted) = (Vec::new(), 0, 0_usize, false);
    for (at, c) in text.char_indices() {
        match c {
            '"' => quoted = !quoted,
            '[' | '(' | '{' if !quoted => depth += 1,
            ']' | ')' | '}' if !quoted => depth = depth.saturating_sub(1),
            ';' if !quoted && depth == 0 => {
                out.extend(text.get(start..at));
                start = at + 1;
            }
            _ => {}
        }
    }
    out.extend(text.get(start..));
    out
}

impl Reader {
    fn statement(&mut self, text: &str, line: usize) -> Result<(), DocError> {
        if text.is_empty() {
            return Ok(());
        }
        let (word, rest) = text.split_once(char::is_whitespace).unwrap_or((text, ""));
        let rest = rest.trim();
        match word {
            "flowchart" | "graph" if self.seen => {
                Err(fail(line, "the flowchart header must come first"))
            }
            "flowchart" | "graph" => {
                self.direction = Some(match rest {
                    "" | "TD" | "TB" => Direction::Down,
                    "LR" => Direction::Right,
                    "BT" | "RL" => {
                        return Err(fail(line, format!("{rest} is not drawn: use TD or LR")));
                    }
                    other => {
                        return Err(fail(
                            line,
                            format!("unknown direction {other}: use TD or LR"),
                        ));
                    }
                });
                self.seen = true;
                Ok(())
            }
            "subgraph" => {
                if self.group.is_some() {
                    return Err(fail(line, "a subgraph inside a subgraph is not supported"));
                }
                let title = match rest.split_once('[') {
                    Some((_, inner)) => inner
                        .strip_suffix(']')
                        .ok_or_else(|| fail(line, "the subgraph title is not closed with ]"))?,
                    None => rest,
                };
                let title = title.trim().trim_matches('"').trim();
                if title.is_empty() {
                    return Err(fail(line, "a subgraph needs a title"));
                }
                self.seen = true;
                self.group = Some((title.to_owned(), line));
                Ok(())
            }
            "end" => match self.group.take() {
                Some(_) => Ok(()),
                None => Err(fail(line, "end closes no subgraph")),
            },
            word if UNSUPPORTED.contains(&word) => Err(fail(
                line,
                format!("{word} is not supported in a flow: write nodes, links and subgraphs"),
            )),
            _ => {
                self.seen = true;
                self.chain(text, line)
            }
        }
    }

    fn chain(&mut self, mut rest: &str, line: usize) -> Result<(), DocError> {
        let mut from = self.fan(&mut rest, line)?;
        while !rest.trim().is_empty() {
            let Some((style, said)) = link(&mut rest, line)? else {
                return Err(fail(
                    line,
                    format!("expected a link such as --> before {}", rest.trim()),
                ));
            };
            let to = self.fan(&mut rest, line)?;
            for a in &from {
                for b in &to {
                    self.graph.edges.push(Edge {
                        line: style,
                        ..Edge::new(a.as_str(), b.as_str(), said.clone())
                    });
                }
            }
            from = to;
        }
        Ok(())
    }

    fn fan(&mut self, rest: &mut &str, line: usize) -> Result<Vec<String>, DocError> {
        let mut keys = vec![self.node(rest, line)?];
        while let Some(more) = rest.trim_start().strip_prefix('&') {
            *rest = more;
            keys.push(self.node(rest, line)?);
        }
        Ok(keys)
    }

    fn node(&mut self, rest: &mut &str, line: usize) -> Result<String, DocError> {
        let text = rest.trim_start();
        let end = text
            .find(|c: char| !(c.is_alphanumeric() || c == '_'))
            .unwrap_or(text.len());
        let (key, after) = text.split_at(end);
        if key.is_empty() {
            return Err(fail(line, format!("expected a node id at {text}")));
        }
        *rest = after;
        let shaped = match SHAPES
            .iter()
            .find_map(|&(open, close, kind)| Some((rest.strip_prefix(open)?, close, kind)))
        {
            Some((inside, close, kind)) => {
                *rest = inside;
                Some((kind, label(rest, close, line)?))
            }
            None => None,
        };
        let count = self.graph.nodes.len();
        let at = *self.index.entry(key.to_owned()).or_insert(count);
        if at == count {
            self.graph.nodes.push(Node::new(key, key));
        }
        let group = self.group.as_ref().map(|(title, _)| title.clone());
        if let Some(node) = self.graph.nodes.get_mut(at) {
            if let Some((kind, label)) = shaped {
                let (icon, label) = iconed(&label);
                node.kind = kind;
                node.icon = icon;
                node.label = label;
            }
            if node.group.is_none() {
                node.group = group;
            }
        }
        Ok(key.to_owned())
    }
}

fn label(rest: &mut &str, close: &str, line: usize) -> Result<String, DocError> {
    let said = if let Some(quoted) = rest.strip_prefix('"') {
        let (said, after) = quoted
            .split_once('"')
            .ok_or_else(|| fail(line, "a quoted label is not closed"))?;
        *rest = after
            .strip_prefix(close)
            .ok_or_else(|| fail(line, format!("expected {close} after the quoted label")))?;
        said.to_owned()
    } else {
        let mut depth = 0_usize;
        let mut found = None;
        for (at, c) in rest.char_indices() {
            let (said, tail) = rest.split_at(at);
            if depth == 0
                && let Some(after) = tail.strip_prefix(close)
            {
                found = Some((said.trim().to_owned(), after));
                break;
            }
            match c {
                '[' | '(' | '{' => depth += 1,
                ']' | ')' | '}' => {
                    depth = depth.checked_sub(1).ok_or_else(|| {
                        fail(
                            line,
                            format!("{c} does not close a label opened for {close}"),
                        )
                    })?;
                }
                _ => {}
            }
        }
        let (said, after) =
            found.ok_or_else(|| fail(line, format!("the label is not closed with {close}")))?;
        *rest = after;
        said
    };
    match said.is_empty() {
        true => Err(fail(line, "a node label is empty")),
        false => Ok(said),
    }
}

fn iconed(label: &str) -> (Option<String>, String) {
    if let Some(after) = label.strip_prefix(':')
        && let Some((name, rest)) = after.split_once(": ")
        && !name.is_empty()
        && !name.contains(char::is_whitespace)
    {
        return (Some(name.to_owned()), rest.trim().to_owned());
    }
    (None, label.to_owned())
}

type Link = (Line, Option<String>);

fn link(rest: &mut &str, line: usize) -> Result<Option<Link>, DocError> {
    let text = rest.trim_start();
    if text.starts_with("==") {
        return Err(fail(line, "thick links are not drawn: use -->"));
    }
    if text.starts_with('<') {
        return Err(fail(line, "a flow link points one way: use -->"));
    }
    let (style, said, after) = if let Some(dotted) = text.strip_prefix("-.") {
        let dots = dotted.trim_start_matches('.');
        if let Some(after) = dots.strip_prefix("->") {
            (Line::Dashed, None, after)
        } else if dots.starts_with('-') {
            return Err(fail(line, "-.- draws no arrow head: use -.->"));
        } else {
            let (said, after) = dotted
                .split_once(".->")
                .ok_or_else(|| fail(line, "a dotted link label ends with .->"))?;
            (Line::Dashed, Some(said), after)
        }
    } else if let Some(dashed) = text.strip_prefix("--") {
        let more = dashed.trim_start_matches('-');
        if let Some(after) = more.strip_prefix('>') {
            (Line::Solid, None, after)
        } else if more.len() != dashed.len() {
            return Err(fail(line, "--- draws no arrow head: use -->"));
        } else {
            let (said, after) = dashed
                .split_once("-->")
                .ok_or_else(|| fail(line, "a link label written -- text needs --> after it"))?;
            (Line::Solid, Some(said), after)
        }
    } else {
        return Ok(None);
    };
    let (said, after) = match after.trim_start().strip_prefix('|') {
        Some(_) if said.is_some() => return Err(fail(line, "a link has two labels")),
        Some(piped) => {
            let (said, after) = piped
                .split_once('|')
                .ok_or_else(|| fail(line, "a |label| is not closed"))?;
            (Some(said), after)
        }
        None => (said, after),
    };
    *rest = after;
    let said = said.map(str::trim).filter(|said| !said.is_empty());
    Ok(Some((style, said.map(str::to_owned))))
}

pub fn flow_scene(flow: &Flow, look: Look) -> Result<Scene, DocError> {
    let graph = &flow.graph;
    let pairs = diagram::pairs(graph)?;
    let mut placed = layered(graph.nodes.len(), &pairs);
    if flow.direction == Direction::Right {
        transpose(&mut placed);
    }
    let mut elements = groups(graph, &placed.boxes);
    elements.extend(diagram::elements(
        graph,
        &pairs,
        &placed,
        look,
        flow.direction,
    )?);
    Scene::of(elements)
}

fn transpose(placed: &mut Placed) {
    let (slot, row) = (BOX.0 + GAP.0, BOX.1 + GAP.1);
    let (rank_pitch, slot_pitch) = (BOX.0 + RIGHT_GAP.0, BOX.1 + RIGHT_GAP.1);
    for corner in &mut placed.boxes {
        *corner = (corner.1 / row * rank_pitch, corner.0 / slot * slot_pitch);
    }
    for point in placed.bends.iter_mut().flatten() {
        let inside = point.1.rem_euclid(row);
        *point = (
            (point.1 - inside) / row * rank_pitch + inside / BOX.1 * BOX.0,
            (point.0 - BOX.0 / 2.0) / slot * slot_pitch + BOX.1 / 2.0,
        );
    }
}

fn groups(graph: &Graph, boxes: &[(f64, f64)]) -> Vec<Element> {
    let mut titles: Vec<&str> = Vec::new();
    for title in graph.nodes.iter().filter_map(|node| node.group.as_deref()) {
        if !titles.contains(&title) {
            titles.push(title);
        }
    }
    let mut elements = Vec::with_capacity(titles.len() * 2);
    for (at, title) in titles.into_iter().enumerate() {
        let corners = graph
            .nodes
            .iter()
            .zip(boxes)
            .filter(|(node, _)| node.group.as_deref() == Some(title));
        let (left, top, right, bottom) = corners.fold(
            (
                f64::INFINITY,
                f64::INFINITY,
                f64::NEG_INFINITY,
                f64::NEG_INFINITY,
            ),
            |(left, top, right, bottom), (_, &(x, y))| {
                (
                    left.min(x),
                    top.min(y),
                    right.max(x + BOX.0),
                    bottom.max(y + BOX.1),
                )
            },
        );
        let id = format!("g{at}");
        let corner = (left - GROUP_PAD, top - GROUP_PAD - GROUP_TITLE);
        let size = (
            right - left + GROUP_PAD * 2.0,
            bottom - top + GROUP_PAD * 2.0 + GROUP_TITLE,
        );
        let mut frame = shape(&id, "rectangle", corner, size, at);
        frame
            .extra
            .insert("roundness".to_owned(), serde_json::json!({ "type": 3 }));
        frame
            .extra
            .insert("strokeStyle".to_owned(), Value::from("dashed"));
        ui(&mut frame);
        let mut name = text_in(&format!("{id}-title"), title, &id, (0.0, 0.0), at);
        name.x = corner.0 + TITLE_INSET.0;
        name.y = corner.1 + TITLE_INSET.1;
        for (field, value) in [
            ("containerId", Value::Null),
            ("textAlign", Value::from("left")),
        ] {
            name.extra.insert(field.to_owned(), value);
        }
        ui(&mut name);
        elements.extend([frame, name]);
    }
    elements
}
