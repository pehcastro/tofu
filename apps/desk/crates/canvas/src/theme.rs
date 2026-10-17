use desk_ui::components::flow::{self, Ink, Part};
use draw_doc::{Element, Scene};
use gpui::{App, Bounds, Window};

use crate::paint::Frame;

pub fn ui(
    frame: &Frame,
    element: &Element,
    scene: &Scene,
    ink: &Ink,
    window: &mut Window,
    cx: &mut App,
) -> bool {
    let zoom = frame.view.zoom() as f32;
    let at = |(x, y): (f64, f64)| frame.view.screen(frame.origin, x, y);
    match flow::part(element, scene) {
        Part::Card { label, icon } => {
            let seen = element.bounds();
            let area =
                Bounds::from_corners(at((seen.min_x, seen.min_y)), at((seen.max_x, seen.max_y)));
            flow::card(area, zoom, label, icon, ink, window, cx);
        }
        Part::Route { dashed, label } => {
            let points: Vec<_> = flow::trace(element).into_iter().map(at).collect();
            flow::route(&points, zoom, dashed, ink, window);
            if let (Some(label), Some(middle)) = (label, flow::middle(&points)) {
                flow::pill(middle, label, zoom, ink, window, cx);
            }
        }
        Part::Hidden => {}
        Part::Frame | Part::Dot | Part::Caption(_) | Part::Other => return false,
    }
    true
}
