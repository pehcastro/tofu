use desk_plugin_sdk::{Event, Part, Plugin, button, stack, text};

#[derive(Default)]
struct Hello {
    clicks: u32,
}

impl Plugin for Hello {
    fn view(&self) -> Part {
        stack(
            "hello",
            [
                text("count", format!("Clicked {} times", self.clicks)),
                button("bump", "Click me"),
            ],
        )
    }

    fn handle(&mut self, event: Event) {
        if matches!(event, Event::Click(key) if key == "bump") {
            self.clicks = self.clicks.saturating_add(1);
        }
    }
}

desk_plugin_sdk::plugin!(Hello);
