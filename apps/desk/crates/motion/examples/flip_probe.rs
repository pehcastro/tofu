#[path = "../../ui/src/components/flip.rs"]
mod flip;

fn probe(tracker: &gpui::Entity<desk_motion::FlipTracker>) -> impl gpui::IntoElement {
    flip::flip("card", tracker, gpui::div())
}

fn main() {
    let _ = probe;
}
