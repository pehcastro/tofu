wit_bindgen::generate!({ path: "../../../../wit/since_v0.1.0", world: "plugin" });

use std::hint::black_box;

use tofu::desk::host::notify;
use tofu::desk::types::Node;

struct Rogue;

impl Guest for Rogue {
    fn view() -> Vec<Node> {
        black_box(vec![1u8; 128 << 20]);
        Vec::new()
    }

    fn rows(_start: u64, _end: u64) -> Vec<Node> {
        loop {
            black_box(());
        }
    }

    fn handle(_event: Event) {
        notify("rogue", "hello").unwrap_or_else(|denied| panic!("{}", denied.permission));
    }
}

export!(Rogue);
