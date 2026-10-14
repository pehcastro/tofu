use std::collections::VecDeque;

pub const BOX: (f64, f64) = (180.0, 60.0);
pub const GAP: (f64, f64) = (60.0, 100.0);
const SWEEPS: usize = 4;

pub struct Placed {
    pub boxes: Vec<(f64, f64)>,
    pub bends: Vec<Vec<(f64, f64)>>,
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
