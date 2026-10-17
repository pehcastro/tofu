use draw_doc::{ArrowEnd, BoundKind, Element, Landing, Rect, Scene, clip, paste, unclip};
use serde_json::{Value, json};

fn board() -> Scene {
    let text = json!({
        "type": "excalidraw",
        "elements": [
            {"id": "a", "type": "rectangle", "x": 0.0, "y": 0.0, "width": 100.0, "height": 50.0,
             "groupIds": ["g1"],
             "boundElements": [{"id": "a-label", "type": "text"}, {"id": "ab", "type": "arrow"}, {"id": "ac", "type": "arrow"}]},
            {"id": "a-label", "type": "text", "x": 30.0, "y": 15.0, "width": 40.0, "height": 20.0,
             "text": "A < & B", "containerId": "a", "groupIds": ["g1"]},
            {"id": "b", "type": "ellipse", "x": 200.0, "y": 100.0, "width": 80.0, "height": 80.0,
             "groupIds": ["g1"],
             "boundElements": [{"id": "ab", "type": "arrow"}]},
            {"id": "c", "type": "rectangle", "x": -300.0, "y": -40.0, "width": 60.0, "height": 60.0,
             "boundElements": [{"id": "ac", "type": "arrow"}]},
            {"id": "ab", "type": "arrow", "x": 100.0, "y": 25.0, "width": 100.0, "height": 115.0,
             "points": [[0.0, 0.0], [100.0, 115.0]],
             "startBinding": {"elementId": "a", "fixedPoint": [1.0, 0.5], "mode": "orbit"},
             "endBinding": {"elementId": "b", "fixedPoint": [0.0, 0.5], "mode": "orbit"}},
            {"id": "ac", "type": "arrow", "x": 0.0, "y": 25.0, "width": 240.0, "height": 15.0,
             "points": [[0.0, 0.0], [-240.0, -15.0]],
             "startBinding": {"elementId": "a", "fixedPoint": [0.0, 0.5], "mode": "orbit"},
             "endBinding": {"elementId": "c", "fixedPoint": [1.0, 0.5], "mode": "orbit"}},
            {"id": "gone", "type": "rectangle", "x": 0.0, "y": 0.0, "width": 10.0, "height": 10.0,
             "isDeleted": true}
        ]
    });
    match Scene::read(&text.to_string()) {
        Ok(scene) => scene,
        Err(error) => panic!("fixture does not read: {error}"),
    }
}

fn ids(list: &[&str]) -> Vec<String> {
    list.iter().map(|id| (*id).to_owned()).collect()
}

fn minted() -> impl FnMut() -> String {
    let mut count = 0;
    move || {
        count += 1;
        format!("new{count}")
    }
}

fn copied(scene: &Scene, picked: &[&str]) -> Vec<Element> {
    let text = match clip(scene, &ids(picked)) {
        Ok(text) => text,
        Err(error) => panic!("clip failed: {error}"),
    };
    match unclip(&text) {
        Ok(Some(elements)) => elements,
        other => panic!("own clip does not read back: {other:?}"),
    }
}

fn landed(scene: &mut Scene, elements: &[Element], landing: Landing) -> Vec<String> {
    let (ops, selected) = match paste(scene, elements, landing, minted()) {
        Ok(made) => made,
        Err(error) => panic!("paste failed: {error}"),
    };
    if let Err(error) = scene.apply(ops) {
        panic!("pasted ops do not apply: {error}");
    }
    selected
}

fn span(rects: impl Iterator<Item = Rect>) -> Rect {
    match rects.reduce(|a, b| Rect {
        min_x: a.min_x.min(b.min_x),
        min_y: a.min_y.min(b.min_y),
        max_x: a.max_x.max(b.max_x),
        max_y: a.max_y.max(b.max_y),
    }) {
        Some(rect) => rect,
        None => panic!("nothing to measure"),
    }
}

fn get<'a>(scene: &'a Scene, id: &str) -> &'a Element {
    match scene.get(id) {
        Some(element) => element,
        None => panic!("{id} is not on the board"),
    }
}

fn pasted_of(scene: &Scene, before: usize) -> Vec<&Element> {
    scene.elements().iter().skip(before).collect()
}

fn copy_of<'a>(pasted: &[&'a Element], kind: &str, x: f64) -> &'a Element {
    match pasted
        .iter()
        .find(|element| element.kind == kind && (element.x - x).abs() < 1e-9)
    {
        Some(element) => element,
        None => panic!("no pasted {kind} at x {x}"),
    }
}

#[test]
fn copy_brings_the_label_and_skips_the_deleted() {
    let scene = board();
    let elements = copied(&scene, &["a", "gone"]);
    let kept: Vec<&str> = elements.iter().map(|element| element.id.as_str()).collect();
    assert_eq!(kept, ["a", "a-label"]);
}

#[test]
fn plain_text_is_not_a_clip() {
    assert!(matches!(unclip("hello"), Ok(None)));
    assert!(matches!(unclip("{\"type\":\"excalidraw\"}"), Ok(None)));
    assert!(unclip("{\"type\":\"excalidraw/clipboard\",\"elements\":7}").is_err());
}

#[test]
fn offset_moves_every_shape_by_exactly_sixteen_and_keeps_points() {
    let mut scene = board();
    let elements = copied(&scene, &["a", "b", "ab"]);
    let before = scene.elements().len();
    landed(&mut scene, &elements, Landing::Offset(16.0));
    let pasted = pasted_of(&scene, before);
    assert_eq!(pasted.len(), 4);
    for (copy, original) in pasted.iter().zip(&elements) {
        assert_eq!(copy.x, original.x + 16.0, "{}", original.id);
        assert_eq!(copy.y, original.y + 16.0, "{}", original.id);
        assert_eq!(copy.points, original.points, "{}", original.id);
        assert_ne!(copy.id, original.id);
    }
}

#[test]
fn pointer_centres_the_set_and_keeps_relative_positions() {
    let mut scene = board();
    let elements = copied(&scene, &["a", "b", "ab"]);
    let before = scene.elements().len();
    landed(&mut scene, &elements, Landing::Pointer(1000.0, -500.0));
    let pasted = pasted_of(&scene, before);
    let around = span(pasted.iter().map(|element| element.bounds()));
    assert_eq!((around.min_x + around.max_x) / 2.0, 1000.0);
    assert_eq!((around.min_y + around.max_y) / 2.0, -500.0);
    let a = copy_of(&pasted, "rectangle", pasted[0].x);
    let b = pasted
        .iter()
        .find(|element| element.kind == "ellipse")
        .copied();
    let Some(b) = b else {
        panic!("the ellipse was not pasted");
    };
    assert_eq!((b.x - a.x, b.y - a.y), (200.0, 100.0));
}

#[test]
fn bindings_follow_the_copies_and_drop_what_stayed_behind() {
    let mut scene = board();
    let elements = copied(&scene, &["a", "b", "ab", "ac"]);
    let before = scene.elements().len();
    let selected = landed(&mut scene, &elements, Landing::Offset(16.0));
    let pasted = pasted_of(&scene, before);
    let a = copy_of(&pasted, "rectangle", 16.0);
    let b = copy_of(&pasted, "ellipse", 216.0);
    let ab = copy_of(&pasted, "arrow", 116.0);
    let ac = copy_of(&pasted, "arrow", 16.0);
    let label = copy_of(&pasted, "text", 46.0);
    let bound_to = |arrow: &Element, end| match arrow.binding(end) {
        Ok(found) => found.map(|binding| binding.element_id().to_owned()),
        Err(error) => panic!("binding does not read: {error}"),
    };
    assert_eq!(bound_to(ab, ArrowEnd::Start), Some(a.id.clone()));
    assert_eq!(bound_to(ab, ArrowEnd::End), Some(b.id.clone()));
    assert_eq!(bound_to(ac, ArrowEnd::Start), Some(a.id.clone()));
    assert_eq!(bound_to(ac, ArrowEnd::End), None);
    assert_eq!(
        label.extra.get("containerId"),
        Some(&Value::from(a.id.clone()))
    );
    let listed = match a.bound_elements() {
        Ok(listed) => listed,
        Err(error) => panic!("boundElements does not read: {error}"),
    };
    let listed: Vec<(String, BoundKind)> = listed
        .into_iter()
        .map(|bound| (bound.id, bound.kind))
        .collect();
    assert_eq!(
        listed,
        [
            (label.id.clone(), BoundKind::Text),
            (ab.id.clone(), BoundKind::Arrow),
            (ac.id.clone(), BoundKind::Arrow),
        ]
    );
    let c = get(&scene, "c");
    let c_bound = match c.bound_elements() {
        Ok(listed) => listed,
        Err(error) => panic!("boundElements does not read: {error}"),
    };
    assert_eq!(c_bound.len(), 1, "the original c gains nothing");
    assert!(!selected.contains(&label.id), "a label is not selected");
    assert_eq!(selected.len(), 4);
}

#[test]
fn groups_get_a_new_id_shared_by_the_copies() {
    let mut scene = board();
    let elements = copied(&scene, &["a", "b"]);
    let before = scene.elements().len();
    landed(&mut scene, &elements, Landing::Offset(16.0));
    let pasted = pasted_of(&scene, before);
    let groups: Vec<&Value> = pasted
        .iter()
        .filter_map(|element| element.extra.get("groupIds"))
        .collect();
    assert_eq!(groups.len(), 3);
    assert!(groups.iter().all(|group| *group == groups[0]));
    assert_ne!(groups[0], &json!(["g1"]));
}

#[test]
fn a_second_paste_does_not_collide_and_undo_takes_one_step() {
    let mut scene = board();
    let elements = copied(&scene, &["a", "b", "ab"]);
    let before = scene.elements().len();
    landed(&mut scene, &elements, Landing::Offset(16.0));
    let mut again = match paste(&scene, &elements, Landing::Offset(32.0), minted()) {
        Ok((ops, _)) => ops,
        Err(error) => panic!("second paste failed: {error}"),
    };
    assert!(matches!(scene.apply(std::mem::take(&mut again)), Ok(())));
    assert_eq!(scene.elements().len(), before + 8);
    assert!(matches!(scene.undo(), Ok(true)));
    assert_eq!(scene.elements().len(), before + 4);
    assert!(matches!(scene.undo(), Ok(true)));
    assert_eq!(scene.elements().len(), before);
}
