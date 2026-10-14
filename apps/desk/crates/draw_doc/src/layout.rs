use std::collections::VecDeque;

pub const BOX: (f64, f64) = (180.0, 60.0);
pub const GAP: (f64, f64) = (60.0, 100.0);
const SWEEPS: usize = 4;

pub fn layered(count: usize, edges: &[(usize, usize)]) -> Vec<(f64, f64)> {
    let mut below = vec![Vec::new(); count];
    let mut above = vec![Vec::new(); count];
    for &(from, to) in edges {
        if from < count && to < count && from != to {
            below[from].push(to);
            above[to].push(from);
        }
    }
    let rank = ranks(&below, &above);
    let height = rank.iter().max().map_or(0, |top| top + 1);
    let mut layers = vec![Vec::new(); height];
    for (node, &at) in rank.iter().enumerate() {
        layers[at].push(node);
    }
    let mut slot = vec![0.0; count];
    let place = |layers: &[Vec<usize>], slot: &mut [f64]| {
        for layer in layers {
            for (at, &node) in layer.iter().enumerate() {
                slot[node] = at as f64;
            }
        }
    };
    place(&layers, &mut slot);
    for sweep in 0..SWEEPS {
        let (order, neighbours): (Vec<usize>, _) = if sweep % 2 == 0 {
            ((1..height).collect(), &above)
        } else {
            ((0..height.saturating_sub(1)).rev().collect(), &below)
        };
        for at in order {
            let layer = &mut layers[at];
            let keys: Vec<(f64, usize)> = layer
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
            let mut keyed = keys;
            keyed.sort_by(|a, b| a.0.total_cmp(&b.0));
            *layer = keyed.into_iter().map(|(_, node)| node).collect();
            for (at, &node) in layer.iter().enumerate() {
                slot[node] = at as f64;
            }
        }
    }
    let widest = layers.iter().map(Vec::len).max().unwrap_or(0) as f64;
    let mut placed = vec![(0.0, 0.0); count];
    for (row, layer) in layers.iter().enumerate() {
        let indent = (widest - layer.len() as f64) / 2.0 * (BOX.0 + GAP.0);
        for (at, &node) in layer.iter().enumerate() {
            placed[node] = (
                indent + at as f64 * (BOX.0 + GAP.0),
                row as f64 * (BOX.1 + GAP.1),
            );
        }
    }
    placed
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
