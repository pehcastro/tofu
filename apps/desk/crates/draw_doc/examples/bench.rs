use std::collections::HashMap;
use std::error::Error;
use std::time::{Duration, Instant};

use draw_doc::{DocError, Element, Op, Rect, Scene};
use serde_json::{Value, json};

const SIZES: [usize; 3] = [200, 2_000, 10_000];
const COLUMNS: usize = 100;
const PITCH_X: f64 = 160.0;
const PITCH_Y: f64 = 120.0;
const HEAVY_RUNS: usize = 21;
const LIGHT_RUNS: usize = 501;
const GRID_CELL: f64 = 512.0;
const VIEW: Rect = Rect {
    min_x: 0.0,
    min_y: 0.0,
    max_x: 1600.0,
    max_y: 1000.0,
};

fn main() -> Result<(), Box<dyn Error>> {
    println!("| action | shapes | median | detail |");
    println!("|---|---|---|---|");
    let mut grid_rows = Vec::new();
    for size in SIZES {
        let wire = serde_json::to_string(&(0..size).map(element_json).collect::<Vec<Value>>())?;

        let mut scene = Scene::new();
        let apply = median(HEAVY_RUNS, || {
            let elements: Vec<Element> = serde_json::from_str(&wire)?;
            scene = Scene::new();
            scene.apply(
                elements
                    .into_iter()
                    .map(|element| Op::Add {
                        at: None,
                        element: Box::new(element),
                    })
                    .collect(),
            )
        })?;
        row(
            "apply",
            size,
            apply,
            &format!(
                "parse {} KB of element JSON and add as one undo step",
                wire.len() / 1024
            ),
        );

        let mut bytes = 0;
        let read = median(HEAVY_RUNS, || {
            bytes = scene.write()?.len();
            Ok(())
        })?;
        row(
            "read",
            size,
            read,
            &format!("whole file to JSON, {} KB", bytes / 1024),
        );

        let mut visible = 0;
        let cull = median(LIGHT_RUNS, || {
            visible = scene.cull(VIEW).count();
            Ok(())
        })?;
        row(
            "cull",
            size,
            cull,
            &format!("linear scan, 1600x1000 view, {visible} visible"),
        );

        let Some(first) = scene.elements().first().cloned() else {
            return Err("empty scene".into());
        };
        let mut undo = Vec::with_capacity(LIGHT_RUNS);
        for _ in 0..LIGHT_RUNS {
            let mut moved = first.clone();
            moved.x += 5.0;
            scene.apply(vec![Op::Update(Box::new(moved))])?;
            let start = Instant::now();
            scene.undo()?;
            undo.push(start.elapsed());
        }
        undo.sort();
        row(
            "undo",
            size,
            undo[undo.len() / 2],
            "one drag step (move 1 shape), op log inverse",
        );

        let mut grid = Grid::default();
        let build = median(HEAVY_RUNS, || {
            grid = Grid::build(&scene);
            Ok(())
        })?;
        let mut found = 0;
        let query = median(LIGHT_RUNS, || {
            found = grid.query(&scene, VIEW).len();
            Ok(())
        })?;
        grid_rows.push(format!(
            "| {} | {} | {} | {} | {found} visible |",
            group(size),
            fmt(cull),
            fmt(build),
            fmt(query)
        ));
    }
    println!();
    println!("| shapes | linear scan | grid build | grid query | detail |");
    println!("|---|---|---|---|---|");
    for line in grid_rows {
        println!("{line}");
    }
    Ok(())
}

fn median(
    runs: usize,
    mut body: impl FnMut() -> Result<(), DocError>,
) -> Result<Duration, DocError> {
    let mut times = Vec::with_capacity(runs);
    for _ in 0..runs {
        let start = Instant::now();
        body()?;
        times.push(start.elapsed());
    }
    times.sort();
    Ok(times[runs / 2])
}

fn row(action: &str, size: usize, time: Duration, detail: &str) {
    println!("| {action} | {} | {} | {detail} |", group(size), fmt(time));
}

fn fmt(time: Duration) -> String {
    let micros = time.as_secs_f64() * 1e6;
    if micros < 1000.0 {
        format!("{micros:.1} us")
    } else {
        format!("{:.2} ms", micros / 1000.0)
    }
}

fn group(size: usize) -> String {
    let digits = size.to_string();
    let mut out = String::new();
    for (i, c) in digits.chars().enumerate() {
        if i > 0 && (digits.len() - i).is_multiple_of(3) {
            out.push(',');
        }
        out.push(c);
    }
    out
}

fn element_json(i: usize) -> Value {
    let x = (i % COLUMNS) as f64 * PITCH_X;
    let y = (i / COLUMNS) as f64 * PITCH_Y;
    let kind = ["rectangle", "ellipse", "diamond", "text", "arrow"][i % 5];
    let mut element = json!({
        "id": format!("el-{i}"), "type": kind, "x": x, "y": y, "width": 120, "height": 80, "angle": 0,
        "strokeColor": "#1e1e1e", "backgroundColor": "transparent", "fillStyle": "solid", "strokeWidth": 2,
        "strokeStyle": "solid", "roughness": 1, "opacity": 100, "groupIds": [], "frameId": null,
        "index": format!("a{i}"), "roundness": { "type": 3 }, "seed": i, "version": 1, "versionNonce": i,
        "isDeleted": false, "boundElements": null, "updated": 1_760_000_000_000_u64, "link": null, "locked": false,
    });
    let extra = match kind {
        "text" => {
            json!({ "text": format!("node {i}"), "fontSize": 20, "fontFamily": 5, "textAlign": "left",
            "verticalAlign": "top", "containerId": null, "originalText": format!("node {i}"), "autoResize": true, "lineHeight": 1.25 })
        }
        "arrow" => json!({ "points": [[0, 0], [120, 80]], "startBinding": null, "endBinding": null,
            "startArrowhead": null, "endArrowhead": "arrow", "elbowed": false }),
        _ => json!({}),
    };
    if let (Value::Object(element), Value::Object(extra)) = (&mut element, extra) {
        element.extend(extra);
    }
    element
}

#[derive(Default)]
struct Grid {
    cells: HashMap<(i64, i64), Vec<usize>>,
}

impl Grid {
    fn build(scene: &Scene) -> Self {
        let mut cells: HashMap<(i64, i64), Vec<usize>> = HashMap::new();
        for (i, element) in scene.elements().iter().enumerate() {
            let (x0, y0, x1, y1) = cell_span(element.bounds());
            for cx in x0..=x1 {
                for cy in y0..=y1 {
                    cells.entry((cx, cy)).or_default().push(i);
                }
            }
        }
        Self { cells }
    }

    fn query(&self, scene: &Scene, view: Rect) -> Vec<usize> {
        let (x0, y0, x1, y1) = cell_span(view);
        let mut hits: Vec<usize> = (x0..=x1)
            .flat_map(|cx| (y0..=y1).map(move |cy| (cx, cy)))
            .filter_map(|cell| self.cells.get(&cell))
            .flatten()
            .copied()
            .filter(|&i| scene.elements()[i].bounds().intersects(&view))
            .collect();
        hits.sort_unstable();
        hits.dedup();
        hits
    }
}

fn cell_span(rect: Rect) -> (i64, i64, i64, i64) {
    let cell = |v: f64| (v / GRID_CELL).floor() as i64;
    (
        cell(rect.min_x),
        cell(rect.min_y),
        cell(rect.max_x),
        cell(rect.max_y),
    )
}
