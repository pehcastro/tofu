use std::collections::VecDeque;
use std::f64::consts::{FRAC_PI_2, TAU};
use std::fmt;

use serde::{Deserialize, Serialize};

pub const BOX: (f64, f64) = (180.0, 60.0);
pub const GAP: (f64, f64) = (60.0, 100.0);
const PITCH: (f64, f64) = (BOX.0 + GAP.0, BOX.1 + GAP.1);
const SWEEPS: usize = 4;

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Layout {
    #[default]
    Layered,
    Tree,
    Grid,
    Radial,
}

impl fmt::Display for Layout {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            Layout::Layered => "layered",
            Layout::Tree => "tree",
            Layout::Grid => "grid",
            Layout::Radial => "radial",
        })
    }
}

pub struct Placed {
    pub boxes: Vec<(f64, f64)>,
    pub bends: Vec<Vec<(f64, f64)>>,
}

pub fn place(layout: Layout, count: usize, edges: &[(usize, usize)]) -> Placed {
    let forest = || Forest::grow(count, edges);
    let boxes = match layout {
        Layout::Layered => None,
        Layout::Grid => Some(grid(count)),
        Layout::Tree => forest().map(|forest| tree(&forest)),
        Layout::Radial => forest().map(|forest| radial(&forest)),
    };
    match boxes {
        Some(boxes) => Placed {
            boxes,
            bends: vec![Vec::new(); edges.len()],
        },
        None => layered(count, edges),
    }
}

fn grid(count: usize) -> Vec<(f64, f64)> {
    let root = count.isqrt();
    let columns = if root * root < count { root + 1 } else { root }.max(1);
    (0..count)
        .map(|at| {
            (
                (at % columns) as f64 * PITCH.0,
                (at / columns) as f64 * PITCH.1,
            )
        })
        .collect()
}

struct Forest {
    roots: Vec<usize>,
    children: Vec<Vec<usize>>,
    order: Vec<usize>,
    depth: Vec<usize>,
}

impl Forest {
    fn grow(count: usize, edges: &[(usize, usize)]) -> Option<Forest> {
        let mut below = vec![Vec::new(); count];
        let mut waiting = vec![0_usize; count];
        for &(from, to) in edges {
            if from < count && to < count && from != to {
                below[from].push(to);
                waiting[to] += 1;
            }
        }
        let roots: Vec<usize> = (0..count).filter(|&node| waiting[node] == 0).collect();
        let mut ready = roots.clone();
        let mut sorted = 0;
        while let Some(node) = ready.pop() {
            sorted += 1;
            for &child in &below[node] {
                waiting[child] -= 1;
                if waiting[child] == 0 {
                    ready.push(child);
                }
            }
        }
        if sorted < count {
            return None;
        }
        let mut seen = vec![false; count];
        let mut depth = vec![0; count];
        let mut children = vec![Vec::new(); count];
        let mut order = Vec::with_capacity(count);
        for &root in &roots {
            seen[root] = true;
        }
        let mut queue: VecDeque<usize> = roots.iter().copied().collect();
        while let Some(node) = queue.pop_front() {
            order.push(node);
            for &child in &below[node] {
                if !seen[child] {
                    seen[child] = true;
                    depth[child] = depth[node] + 1;
                    children[node].push(child);
                    queue.push_back(child);
                }
            }
        }
        Some(Forest {
            roots,
            children,
            order,
            depth,
        })
    }
}

#[derive(Default)]
struct Contour {
    levels: Vec<(f64, f64)>,
    offset: f64,
}

impl Contour {
    fn level(&self, down: usize) -> (f64, f64) {
        let (left, right) = self.levels[self.levels.len() - 1 - down];
        (left + self.offset, right + self.offset)
    }

    fn set(&mut self, down: usize, (left, right): (f64, f64)) {
        let at = self.levels.len() - 1 - down;
        self.levels[at] = (left - self.offset, right - self.offset);
    }
}

fn pack(nodes: &[usize], contours: &mut [Contour], x: &mut [f64]) -> Contour {
    let mut packed = Contour::default();
    for &node in nodes {
        let mut next = std::mem::take(&mut contours[node]);
        let shared = packed.levels.len().min(next.levels.len());
        let shift = (0..shared)
            .map(|down| packed.level(down).1 - next.level(down).0 + PITCH.0)
            .fold(0.0, f64::max);
        x[node] = shift;
        next.offset += shift;
        let merged: Vec<(f64, f64)> = (0..shared)
            .map(|down| (packed.level(down).0, next.level(down).1))
            .collect();
        if next.levels.len() > packed.levels.len() {
            packed = next;
        }
        for (down, bounds) in merged.into_iter().enumerate() {
            packed.set(down, bounds);
        }
    }
    let middle = match (nodes.first(), nodes.last()) {
        (Some(&first), Some(&last)) => (x[first] + x[last]) / 2.0,
        _ => 0.0,
    };
    for &node in nodes {
        x[node] -= middle;
    }
    packed.offset -= middle;
    packed
}

fn tree(forest: &Forest) -> Vec<(f64, f64)> {
    let count = forest.order.len();
    let mut contours: Vec<Contour> = (0..count).map(|_| Contour::default()).collect();
    let mut x = vec![0.0; count];
    for &node in forest.order.iter().rev() {
        let mut packed = pack(&forest.children[node], &mut contours, &mut x);
        packed.levels.push((-packed.offset, -packed.offset));
        contours[node] = packed;
    }
    pack(&forest.roots, &mut contours, &mut x);
    for &node in &forest.order {
        let base = x[node];
        for &child in &forest.children[node] {
            x[child] += base;
        }
    }
    corners(
        (0..count)
            .map(|node| (x[node], forest.depth[node] as f64 * PITCH.1))
            .collect(),
    )
}

fn radial(forest: &Forest) -> Vec<(f64, f64)> {
    let count = forest.order.len();
    let mut size = vec![1.0_f64; count];
    for &node in forest.order.iter().rev() {
        size[node] += forest.children[node]
            .iter()
            .map(|&child| size[child])
            .sum::<f64>();
    }
    let mut wedge = vec![(0.0, TAU); count];
    share(&forest.roots, (0.0, TAU), &size, &mut wedge);
    for &node in &forest.order {
        let span = wedge[node];
        share(&forest.children[node], span, &size, &mut wedge);
    }
    let hub = usize::from(forest.roots.len() > 1);
    let ring = |node: usize| forest.depth[node] + hub;
    let angle = |node: usize| wedge[node].0 + wedge[node].1 / 2.0;
    let rings = (0..count).map(|node| ring(node) + 1).max().unwrap_or(0);
    let mut on_ring = vec![Vec::new(); rings];
    for node in 0..count {
        on_ring[ring(node)].push(angle(node));
    }
    let reach = BOX.0.hypot(BOX.1) + GAP.0;
    let mut radius = vec![0.0_f64; rings];
    for (at, angles) in on_ring.iter_mut().enumerate() {
        angles.sort_by(f64::total_cmp);
        let wrap = match (angles.first(), angles.last()) {
            (Some(first), Some(last)) if angles.len() > 1 => first + TAU - last,
            _ => TAU,
        };
        let tightest = angles
            .windows(2)
            .map(|pair| pair[1] - pair[0])
            .fold(wrap, f64::min);
        let spread = match angles.len() {
            0 | 1 => 0.0,
            _ => reach / (2.0 * (tightest / 2.0).sin()),
        };
        radius[at] = match at.checked_sub(1) {
            Some(inner) => (radius[inner] + reach).max(spread),
            None => spread,
        };
    }
    corners(
        (0..count)
            .map(|node| {
                let (turn, out) = (angle(node) - FRAC_PI_2, radius[ring(node)]);
                (out * turn.cos(), out * turn.sin())
            })
            .collect(),
    )
}

fn share(nodes: &[usize], (start, width): (f64, f64), size: &[f64], wedge: &mut [(f64, f64)]) {
    let total: f64 = nodes.iter().map(|&node| size[node]).sum();
    let mut at = start;
    for &node in nodes {
        let part = width * size[node] / total;
        wedge[node] = (at, part);
        at += part;
    }
}

fn corners(centres: Vec<(f64, f64)>) -> Vec<(f64, f64)> {
    let (left, top) = centres
        .iter()
        .fold((f64::INFINITY, f64::INFINITY), |(left, top), &(x, y)| {
            (left.min(x), top.min(y))
        });
    centres
        .into_iter()
        .map(|(x, y)| (x - left, y - top))
        .collect()
}

pub fn layered(count: usize, edges: &[(usize, usize)]) -> Placed {
    let real = |&&(from, to): &&(usize, usize)| from < count && to < count && from != to;
    let mut below = vec![Vec::new(); count];
    let mut above = vec![Vec::new(); count];
    for &(from, to) in edges.iter().filter(real) {
        below[from].push(to);
        above[to].push(from);
    }
    let mut rank = ranks(&below, &above);
    let mut below = vec![Vec::new(); count];
    let mut above = vec![Vec::new(); count];
    let mut chains = Vec::with_capacity(edges.len());
    for &(from, to) in edges {
        let mut chain = Vec::new();
        if real(&&(from, to)) {
            let (start, end) = (rank[from], rank[to]);
            let steps: Vec<usize> = if end > start {
                (start + 1..end).collect()
            } else {
                (end + 1..start).rev().collect()
            };
            let mut previous = from;
            for layer in steps {
                let dummy = rank.len();
                rank.push(layer);
                below.push(Vec::new());
                above.push(Vec::new());
                link(&rank, &mut below, &mut above, previous, dummy);
                chain.push(dummy);
                previous = dummy;
            }
            link(&rank, &mut below, &mut above, previous, to);
        }
        chains.push(chain);
    }
    let height = rank.iter().max().map_or(0, |top| top + 1);
    let mut layers = vec![Vec::new(); height];
    for (node, &at) in rank.iter().enumerate() {
        layers[at].push(node);
    }
    let mut slot = vec![0.0; rank.len()];
    for layer in &layers {
        for (at, &node) in layer.iter().enumerate() {
            slot[node] = at as f64;
        }
    }
    for sweep in 0..SWEEPS {
        let (order, neighbours): (Vec<usize>, _) = if sweep % 2 == 0 {
            ((1..height).collect(), &above)
        } else {
            ((0..height.saturating_sub(1)).rev().collect(), &below)
        };
        for at in order {
            let layer = &mut layers[at];
            let mut keyed: Vec<(f64, usize)> = layer
                .iter()
                .map(|&node| {
                    let seen = &neighbours[node];
                    let key = if seen.is_empty() {
                        slot[node]
                    } else {
                        seen.iter().map(|&other| slot[other]).sum::<f64>() / seen.len() as f64
                    };
                    (key, node)
                })
                .collect();
            keyed.sort_by(|a, b| a.0.total_cmp(&b.0));
            *layer = keyed.into_iter().map(|(_, node)| node).collect();
            for (at, &node) in layer.iter().enumerate() {
                slot[node] = at as f64;
            }
        }
    }
    let widest = layers.iter().map(Vec::len).max().unwrap_or(0) as f64;
    let mut corner = vec![(0.0, 0.0); rank.len()];
    for (row, layer) in layers.iter().enumerate() {
        let indent = (widest - layer.len() as f64) / 2.0 * (BOX.0 + GAP.0);
        for (at, &node) in layer.iter().enumerate() {
            corner[node] = (
                indent + at as f64 * (BOX.0 + GAP.0),
                row as f64 * (BOX.1 + GAP.1),
            );
        }
    }
    let bends = edges
        .iter()
        .zip(chains)
        .map(|(&(from, to), chain)| {
            let down = from < count && to < count && rank[to] > rank[from];
            chain
                .into_iter()
                .flat_map(|dummy| {
                    let (x, y) = corner[dummy];
                    let middle = x + BOX.0 / 2.0;
                    let (top, bottom) = ((middle, y), (middle, y + BOX.1));
                    if down { [top, bottom] } else { [bottom, top] }
                })
                .collect()
        })
        .collect();
    corner.truncate(count);
    Placed {
        boxes: corner,
        bends,
    }
}

fn link(rank: &[usize], below: &mut [Vec<usize>], above: &mut [Vec<usize>], a: usize, b: usize) {
    let (upper, lower) = match rank[a].cmp(&rank[b]) {
        std::cmp::Ordering::Less => (a, b),
        std::cmp::Ordering::Greater => (b, a),
        std::cmp::Ordering::Equal => return,
    };
    below[upper].push(lower);
    above[lower].push(upper);
}

fn ranks(below: &[Vec<usize>], above: &[Vec<usize>]) -> Vec<usize> {
    let count = below.len();
    let mut waiting: Vec<usize> = above.iter().map(Vec::len).collect();
    let mut done = vec![false; count];
    let mut rank = vec![0; count];
    let mut ready: VecDeque<usize> = (0..count).filter(|&node| waiting[node] == 0).collect();
    let mut next_forced = 0;
    loop {
        let node = match ready.pop_front() {
            Some(node) => node,
            None => match (next_forced..count).find(|&node| !done[node]) {
                Some(node) => {
                    next_forced = node;
                    node
                }
                None => return rank,
            },
        };
        if done[node] {
            continue;
        }
        done[node] = true;
        for &child in &below[node] {
            if done[child] {
                continue;
            }
            rank[child] = rank[child].max(rank[node] + 1);
            waiting[child] = waiting[child].saturating_sub(1);
            if waiting[child] == 0 {
                ready.push_back(child);
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use std::time::Instant;

    use super::{BOX, Layout, Placed, layered, place};
    use crate::{DocError, Edge, Graph, Node, diagram};

    const KINDS: [Layout; 4] = [Layout::Layered, Layout::Tree, Layout::Grid, Layout::Radial];

    fn org_chart() -> Graph {
        let node = |key: &str| Node {
            key: key.to_owned(),
            label: key.to_owned(),
        };
        let edge = |from: &str, to: &str| Edge {
            from: from.to_owned(),
            to: to.to_owned(),
            label: None,
        };
        Graph {
            nodes: [
                "ceo", "cto", "cfo", "coo", "eng", "platform", "security", "finance", "legal",
                "ops", "support", "frontend",
            ]
            .map(node)
            .into(),
            edges: vec![
                edge("ceo", "cto"),
                edge("ceo", "cfo"),
                edge("ceo", "coo"),
                edge("cto", "eng"),
                edge("cto", "platform"),
                edge("cto", "security"),
                edge("cfo", "finance"),
                edge("cfo", "legal"),
                edge("coo", "ops"),
                edge("coo", "support"),
                edge("eng", "frontend"),
            ],
        }
    }

    fn generated(count: usize) -> Vec<(usize, usize)> {
        let mut edges: Vec<(usize, usize)> = (1..count).map(|at| ((at - 1) / 3, at)).collect();
        edges.extend(
            (0..count.saturating_sub(5))
                .step_by(7)
                .map(|at| (at, at + 5)),
        );
        edges
    }

    type Case = (&'static str, usize, Vec<(usize, usize)>);

    fn cases() -> Vec<Case> {
        vec![
            ("empty", 0, vec![]),
            ("single", 1, vec![]),
            ("self loop", 3, vec![(0, 0), (0, 1), (0, 2)]),
            (
                "forest of uneven depth",
                9,
                vec![(0, 1), (1, 2), (2, 3), (3, 4), (5, 6), (5, 7), (7, 8)],
            ),
            (
                "narrow between wide",
                13,
                vec![
                    (0, 1),
                    (0, 2),
                    (0, 3),
                    (1, 4),
                    (1, 5),
                    (1, 6),
                    (2, 7),
                    (3, 8),
                    (3, 9),
                    (3, 10),
                    (4, 11),
                    (10, 12),
                ],
            ),
            (
                "two parents",
                6,
                vec![(0, 1), (0, 2), (1, 3), (2, 3), (3, 4), (0, 5), (5, 4)],
            ),
            ("cycle", 4, vec![(0, 1), (1, 2), (2, 0), (2, 3)]),
            ("star", 40, (1..40).map(|at| (0, at)).collect()),
            ("generated 500", 500, generated(500)),
        ]
    }

    fn apart(name: &str, layout: Layout, placed: &Placed) {
        for (at, a) in placed.boxes.iter().enumerate() {
            assert!(
                a.0.is_finite() && a.1.is_finite(),
                "{name} {layout}: box {at} at {a:?}"
            );
            for (other, b) in placed.boxes.iter().enumerate().skip(at + 1) {
                let clear = (a.0 - b.0).abs() >= BOX.0 || (a.1 - b.1).abs() >= BOX.1;
                assert!(
                    clear,
                    "{name} {layout}: box {at} {a:?} overlaps box {other} {b:?}"
                );
            }
        }
    }

    #[test]
    fn every_layout_keeps_every_box_apart() {
        for (name, count, edges) in cases() {
            for layout in KINDS {
                let placed = place(layout, count, &edges);
                assert_eq!(placed.boxes.len(), count, "{name} {layout}");
                assert_eq!(placed.bends.len(), edges.len(), "{name} {layout}");
                apart(name, layout, &placed);
            }
        }
    }

    #[test]
    fn the_org_chart_diagram_has_no_overlapping_rectangles() -> Result<(), DocError> {
        for layout in KINDS {
            let scene = diagram(&org_chart(), layout)?;
            let boxes: Vec<(f64, f64)> = scene
                .elements()
                .iter()
                .filter(|element| element.kind == "rectangle")
                .map(|element| (element.x, element.y))
                .collect();
            assert_eq!(boxes.len(), 12, "{layout}");
            let placed = Placed {
                boxes,
                bends: Vec::new(),
            };
            apart("org chart", layout, &placed);
        }
        Ok(())
    }

    #[test]
    fn a_cycle_falls_back_to_layered_for_tree_and_radial() {
        let edges = [(0, 1), (1, 2), (2, 0), (2, 3)];
        let fallback = layered(4, &edges).boxes;
        assert_eq!(place(Layout::Tree, 4, &edges).boxes, fallback);
        assert_eq!(place(Layout::Radial, 4, &edges).boxes, fallback);
    }

    #[test]
    fn a_self_loop_is_not_a_cycle() {
        let looped = [(0, 0), (0, 1), (0, 2), (1, 3)];
        let plain = [(0, 1), (0, 2), (1, 3)];
        for layout in [Layout::Tree, Layout::Radial] {
            assert_eq!(
                place(layout, 4, &looped).boxes,
                place(layout, 4, &plain).boxes,
                "{layout}"
            );
        }
    }

    #[test]
    fn a_tree_centres_each_parent_over_its_children() {
        let placed = place(
            Layout::Tree,
            7,
            &[(0, 1), (0, 2), (0, 3), (1, 4), (1, 5), (3, 6)],
        );
        let b = &placed.boxes;
        let close = |a: f64, b: f64| (a - b).abs() < 1e-6;
        assert!(close(b[0].0, (b[1].0 + b[3].0) / 2.0), "{b:?}");
        assert!(close(b[1].0, (b[4].0 + b[5].0) / 2.0), "{b:?}");
        assert!(close(b[3].0, b[6].0), "{b:?}");
        assert!(b[0].1 < b[1].1 && b[1].1 == b[2].1 && b[2].1 < b[4].1);
    }

    #[test]
    fn a_grid_is_row_major_with_ceil_sqrt_columns() {
        for (count, columns) in [(0, 0), (1, 1), (4, 2), (5, 3), (9, 3), (10, 4), (12, 4)] {
            let placed = place(Layout::Grid, count, &[]);
            let mut xs: Vec<f64> = placed.boxes.iter().map(|corner| corner.0).collect();
            xs.sort_by(f64::total_cmp);
            xs.dedup();
            assert_eq!(xs.len(), columns, "{count} nodes");
            for pair in placed.boxes.windows(2) {
                assert!(
                    pair[0].1 < pair[1].1 || pair[0].0 < pair[1].0,
                    "{count}: {pair:?}"
                );
            }
        }
    }

    #[test]
    fn radial_rings_sit_around_one_root() {
        let placed = place(Layout::Radial, 5, &[(0, 1), (0, 2), (0, 3), (1, 4)]);
        let centre = placed.boxes[0];
        let reach = |at: usize| {
            let corner = placed.boxes[at];
            (corner.0 - centre.0).hypot(corner.1 - centre.1)
        };
        assert!((reach(1) - reach(2)).abs() < 1e-6 && (reach(2) - reach(3)).abs() < 1e-6);
        assert!(reach(4) > reach(1) + BOX.0.hypot(BOX.1));
    }

    #[test]
    fn a_hundred_thousand_deep_chain_places_without_overflow() {
        let count = 100_000;
        let edges: Vec<(usize, usize)> = (1..count).map(|at| (at - 1, at)).collect();
        for layout in [Layout::Tree, Layout::Grid, Layout::Radial] {
            assert_eq!(place(layout, count, &edges).boxes.len(), count, "{layout}");
        }
    }

    #[test]
    fn an_unknown_layout_names_the_four() {
        let error = serde_json::from_str::<Layout>("\"spiral\"")
            .err()
            .map(|error| error.to_string())
            .unwrap_or_default();
        for name in ["layered", "tree", "grid", "radial"] {
            assert!(error.contains(name), "{error}");
        }
        assert_eq!(
            serde_json::from_str::<Layout>("\"tree\"").ok(),
            Some(Layout::Tree)
        );
    }

    #[test]
    fn five_hundred_nodes_per_layout() {
        let edges = generated(500);
        let graph = Graph {
            nodes: (0..500)
                .map(|at| Node {
                    key: format!("k{at}"),
                    label: format!("node {at}"),
                })
                .collect(),
            edges: edges
                .iter()
                .map(|&(from, to)| Edge {
                    from: format!("k{from}"),
                    to: format!("k{to}"),
                    label: (to == from + 5).then(|| "uses".to_owned()),
                })
                .collect(),
        };
        for layout in KINDS {
            let mut times: Vec<_> = (0..15)
                .map(|_| {
                    let started = Instant::now();
                    let scene = std::hint::black_box(diagram(&graph, layout));
                    let took = started.elapsed();
                    drop(scene);
                    took
                })
                .collect();
            times.sort();
            println!(
                "diagram {layout} 500 nodes, 15 runs: min {:?}, median {:?}, max {:?}",
                times[0], times[7], times[14]
            );
        }
    }
}
