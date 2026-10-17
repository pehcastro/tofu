use draw_doc::{Scene, html, mermaid, svg};
use serde_json::json;

fn board() -> Scene {
    let text = json!({
        "type": "excalidraw",
        "elements": [
            {"id": "1-a", "type": "rectangle", "x": -120.0, "y": -80.0, "width": 100.0, "height": 50.0,
             "strokeColor": "#1e1e1e", "backgroundColor": "transparent",
             "boundElements": [{"id": "1-a-label", "type": "text"}, {"id": "ab", "type": "arrow"}]},
            {"id": "1-a-label", "type": "text", "x": -100.0, "y": -65.0, "width": 60.0, "height": 20.0,
             "text": "say \"hi\" <b> & go", "fontSize": 20, "containerId": "1-a", "strokeColor": "#1e1e1e"},
            {"id": "b", "type": "ellipse", "x": 100.0, "y": 100.0, "width": 80.0, "height": 80.0,
             "strokeColor": "#e03131", "backgroundColor": "#ffc9c9", "fillStyle": "solid",
             "boundElements": [{"id": "ab", "type": "arrow"}]},
            {"id": "d", "type": "diamond", "x": 300.0, "y": 0.0, "width": 60.0, "height": 60.0},
            {"id": "ab", "type": "arrow", "x": -20.0, "y": -55.0, "width": 120.0, "height": 195.0,
             "points": [[0.0, 0.0], [120.0, 195.0]], "endArrowhead": "arrow",
             "startBinding": {"elementId": "1-a", "fixedPoint": [1.0, 0.5], "mode": "orbit"},
             "endBinding": {"elementId": "b", "fixedPoint": [0.0, 0.5], "mode": "orbit"}},
            {"id": "loose", "type": "arrow", "x": 300.0, "y": 100.0, "width": 50.0, "height": 0.0,
             "points": [[0.0, 0.0], [50.0, 0.0]],
             "startBinding": {"elementId": "d", "fixedPoint": [0.5, 1.0], "mode": "orbit"},
             "endBinding": null},
            {"id": "gone", "type": "rectangle", "x": 5000.0, "y": 5000.0, "width": 10.0, "height": 10.0,
             "isDeleted": true}
        ]
    });
    match Scene::read(&text.to_string()) {
        Ok(scene) => scene,
        Err(error) => panic!("fixture does not read: {error}"),
    }
}

fn view_box(drawn: &str) -> Vec<f64> {
    let Some(start) = drawn.find("viewBox=\"") else {
        panic!("no viewBox in {drawn}");
    };
    let rest = &drawn[start + 9..];
    let Some(end) = rest.find('"') else {
        panic!("viewBox is not closed");
    };
    rest[..end]
        .split(' ')
        .map(|number| match number.parse() {
            Ok(number) => number,
            Err(error) => panic!("viewBox number {number:?}: {error}"),
        })
        .collect()
}

#[test]
fn svg_covers_negative_coordinates_and_skips_deleted() {
    let drawn = svg(&board(), None);
    let frame = view_box(&drawn);
    assert_eq!(frame.len(), 4);
    assert!(frame[0] <= -120.0 && frame[1] <= -80.0, "{frame:?}");
    assert!(
        frame[0] + frame[2] >= 360.0 && frame[1] + frame[3] >= 180.0,
        "{frame:?}"
    );
    assert!(
        frame[0] + frame[2] < 5000.0,
        "the deleted shape widened the frame"
    );
    assert!(drawn.starts_with("<svg"));
    assert!(drawn.trim_end().ends_with("</svg>"));
}

#[test]
fn svg_escapes_text_and_leaves_transparent_unfilled() {
    let drawn = svg(&board(), Some("#fffce8"));
    assert!(
        drawn.contains("say &quot;hi&quot; &lt;b&gt; &amp; go"),
        "{drawn}"
    );
    assert!(!drawn.contains("<b>"));
    assert!(drawn.contains("fill=\"none\""));
    assert!(drawn.contains("#fffce8"));
    assert!(drawn.contains("#ffc9c9"));
}

#[test]
fn empty_board_is_still_an_svg() {
    let drawn = svg(&Scene::new(), None);
    assert_eq!(view_box(&drawn).len(), 4);
    assert!(drawn.trim_end().ends_with("</svg>"));
}

#[test]
fn mermaid_names_nodes_safely_and_drops_free_ends() {
    let graph = match mermaid(&board()) {
        Ok(graph) => graph,
        Err(error) => panic!("mermaid failed: {error}"),
    };
    let lines: Vec<&str> = graph.lines().map(str::trim).collect();
    assert_eq!(lines.first().copied(), Some("flowchart LR"));
    assert!(
        lines.contains(&"n1[\"say #quot;hi#quot; #lt;b#gt; #amp; go\"]"),
        "{graph}"
    );
    assert!(lines.contains(&"n2((\" \"))"), "{graph}");
    assert!(lines.contains(&"n3{\" \"}"), "{graph}");
    assert!(lines.contains(&"n1 --> n2"), "{graph}");
    assert_eq!(lines.iter().filter(|line| line.contains("-->")).count(), 1);
    assert!(!graph.contains("1-a"));
}

#[test]
fn html_wraps_the_svg_under_an_escaped_title() {
    let page = html(&board(), "plans <draft>", None);
    assert!(page.starts_with("<!doctype html>"));
    assert!(page.contains("<title>plans &lt;draft&gt;</title>"));
    assert!(page.contains(&svg(&board(), None)));
}
