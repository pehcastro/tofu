use std::collections::HashSet;
use std::sync::{Mutex, OnceLock};

use gpui::{AnyElement, Div, Pixels, SharedString, div, prelude::*};

#[derive(Clone, Copy)]
pub enum LayoutEdge {
    Left,
    Right,
}

fn layout_log() -> Option<&'static Mutex<HashSet<SharedString>>> {
    static LOGGED: OnceLock<Option<Mutex<HashSet<SharedString>>>> = OnceLock::new();
    LOGGED
        .get_or_init(|| std::env::var_os("DESK_LAYOUT_LOG").map(|_| Mutex::new(HashSet::new())))
        .as_ref()
}

fn first_time(logged: &Mutex<HashSet<SharedString>>, name: &SharedString) -> bool {
    logged
        .lock()
        .is_ok_and(|mut logged| logged.insert(name.clone()))
}

pub fn probe(
    name: impl Into<SharedString>,
    edge: LayoutEdge,
    child: impl IntoElement,
) -> AnyElement {
    let Some(logged) = layout_log() else {
        return child.into_any_element();
    };
    let name = name.into();
    div()
        .flex_none()
        .child(child)
        .on_children_prepainted(move |bounds, _, _| {
            let Some(bounds) = bounds.first() else {
                return;
            };
            if !first_time(logged, &name) {
                return;
            }
            let (word, x) = match edge {
                LayoutEdge::Left => ("left", bounds.left()),
                LayoutEdge::Right => ("right", bounds.right()),
            };
            eprintln!("layout {name} edge={word} x={}", f32::from(x));
        })
        .into_any_element()
}

pub fn corner(name: impl Into<SharedString>, radius: Pixels, mut surface: Div) -> Div {
    let Some(logged) = layout_log() else {
        return surface;
    };
    let name = name.into();
    surface.interactivity().on_prepaint(move |prepaint, _, _| {
        let bounds = prepaint.bounds;
        let (x, y) = (f32::from(bounds.left()), f32::from(bounds.top()));
        let (w, h) = (f32::from(bounds.size.width), f32::from(bounds.size.height));
        let placed: SharedString = format!(
            "{name}@{},{},{},{}",
            x.round(),
            y.round(),
            w.round(),
            h.round()
        )
        .into();
        if !first_time(logged, &placed) {
            return;
        }
        eprintln!(
            "corner {placed} x={x} y={y} w={w} h={h} r={}",
            f32::from(radius)
        );
    });
    surface
}
