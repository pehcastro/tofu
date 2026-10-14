use std::error::Error;
use std::process::ExitCode;

use draw_doc::{Op, Scene};
use serde_json::Value;

fn main() -> Result<ExitCode, Box<dyn Error>> {
    let Some(path) = std::env::args().nth(1) else {
        return Err("usage: roundtrip <file.excalidraw>".into());
    };
    let text = std::fs::read_to_string(&path)?;
    let input: Value = serde_json::from_str(&text)?;
    let mut scene = Scene::read(&text)?;
    let written: Value = serde_json::from_str(&scene.write()?)?;
    if let Some(at) = first_difference(&input, &written) {
        println!("roundtrip differs at {at}");
        return Ok(ExitCode::FAILURE);
    }
    println!("roundtrip equal");

    let edits = scene
        .elements()
        .iter()
        .map(|element| {
            let mut moved = element.clone();
            moved.x += 10.0;
            Op::Update(Box::new(moved))
        })
        .chain(scene.elements().first().map(|first| Op::Delete {
            id: first.id.clone(),
        }))
        .collect();
    scene.apply(edits)?;
    let edited: Value = serde_json::from_str(&scene.write()?)?;
    scene.undo()?;
    let undone: Value = serde_json::from_str(&scene.write()?)?;
    match (
        first_difference(&input, &edited),
        first_difference(&input, &undone),
    ) {
        (Some(_), None) => {
            println!("undo equal");
            Ok(ExitCode::SUCCESS)
        }
        (None, _) => {
            println!("edit changed nothing");
            Ok(ExitCode::FAILURE)
        }
        (Some(_), Some(at)) => {
            println!("undo differs at {at}");
            Ok(ExitCode::FAILURE)
        }
    }
}

fn first_difference(left: &Value, right: &Value) -> Option<String> {
    let mut pending = vec![("$".to_owned(), left, right)];
    while let Some((path, left, right)) = pending.pop() {
        match (left, right) {
            (Value::Object(a), Value::Object(b)) => {
                if let Some(key) = a
                    .keys()
                    .chain(b.keys())
                    .find(|key| a.contains_key(*key) != b.contains_key(*key))
                {
                    return Some(format!("{path}.{key}"));
                }
                pending.extend(a.iter().filter_map(|(key, value)| {
                    Some((format!("{path}.{key}"), value, b.get(key)?))
                }));
            }
            (Value::Array(a), Value::Array(b)) if a.len() == b.len() => {
                pending.extend(
                    a.iter()
                        .zip(b)
                        .enumerate()
                        .map(|(i, (x, y))| (format!("{path}[{i}]"), x, y)),
                );
            }
            (Value::Number(a), Value::Number(b)) if a.as_f64() == b.as_f64() => {}
            _ if left == right => {}
            _ => return Some(path),
        }
    }
    None
}
