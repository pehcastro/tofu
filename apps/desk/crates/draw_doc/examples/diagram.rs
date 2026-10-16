use std::error::Error;
use std::time::Instant;

use draw_doc::{Edge, Graph, Layout, Node, diagram};

const RUNS: usize = 5;

fn main() -> Result<(), Box<dyn Error>> {
    let Some(path) = std::env::args().nth(1) else {
        return Err("usage: diagram <out.excalidraw>".into());
    };
    for count in [50, 500] {
        let graph = generated(count);
        let mut times = Vec::with_capacity(RUNS);
        for _ in 0..RUNS {
            let started = Instant::now();
            let text = diagram(&graph, Layout::Layered)?.write()?;
            times.push(started.elapsed().as_secs_f64() * 1000.0);
            std::hint::black_box(text);
        }
        times.sort_by(f64::total_cmp);
        let median = times.get(RUNS / 2).copied().unwrap_or_default();
        println!(
            "diagram {count} nodes {} edges median of {RUNS}: {median:.2} ms",
            graph.edges.len()
        );
    }
    let crates = crates();
    std::fs::write(&path, diagram(&crates, Layout::Layered)?.write()?)?;
    println!(
        "wrote {path}: {} nodes {} edges",
        crates.nodes.len(),
        crates.edges.len()
    );
    Ok(())
}

fn crates() -> Graph {
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
            "desk",
            "desk_core",
            "desk_ui",
            "desk_canvas",
            "draw_doc",
            "desk_plugin",
        ]
        .map(node)
        .into(),
        edges: vec![
            edge("desk", "desk_canvas"),
            edge("desk", "desk_core"),
            edge("desk", "desk_plugin"),
            edge("desk", "desk_ui"),
            edge("desk", "draw_doc"),
            edge("desk_ui", "desk_core"),
            edge("desk_canvas", "desk_ui"),
            edge("desk_canvas", "draw_doc"),
        ],
    }
}

fn generated(count: usize) -> Graph {
    let nodes = (0..count)
        .map(|at| Node {
            key: format!("k{at}"),
            label: format!("node {at}"),
        })
        .collect();
    let mut edges: Vec<Edge> = (1..count)
        .map(|at| Edge {
            from: format!("k{}", (at - 1) / 3),
            to: format!("k{at}"),
            label: None,
        })
        .collect();
    edges.extend((0..count.saturating_sub(5)).step_by(7).map(|at| Edge {
        from: format!("k{at}"),
        to: format!("k{}", at + 5),
        label: Some("uses".to_owned()),
    }));
    Graph { nodes, edges }
}
