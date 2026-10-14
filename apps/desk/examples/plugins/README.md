# Desk plugins

A desk plugin is a WebAssembly component that draws a tree of parts and answers events. `desk_plugin_sdk` (`apps/desk/crates/plugin-sdk`) gives the bindings, the part builders and a `Plugin` trait, so a plugin is one struct.

`hello/` is the smallest one: a counter and a button.

## Three steps

You need the Rust stable toolchain with the `wasm32-unknown-unknown` target (`rustup target add wasm32-unknown-unknown`).

**1. Write the plugin.** Copy `hello/`, rename the crate in `Cargo.toml` and set `id` and `name` in `plugin.toml`. An id is `publisher.name`, lowercase. In `src/lib.rs`, implement `Plugin` on a struct that derives `Default` and register it:

```rust
use desk_plugin_sdk::{Event, Part, Plugin, button, stack, text};

#[derive(Default)]
struct Hello {
    clicks: u32,
}

impl Plugin for Hello {
    fn view(&self) -> Part {
        stack("hello", [text("count", format!("Clicked {} times", self.clicks)), button("bump", "Click me")])
    }

    fn handle(&mut self, event: Event) {
        if matches!(event, Event::Click(key) if key == "bump") {
            self.clicks = self.clicks.saturating_add(1);
        }
    }
}

desk_plugin_sdk::plugin!(Hello);
```

**2. Pack it.** From `apps/desk`:

```powershell
.\scripts\plugin-pack.ps1 -Plugin examples\plugins\hello -Out ..\..\.local\plugins
```

It builds the crate for `wasm32-unknown-unknown`, turns the module into a component, and writes `<Out>\<id>\` holding `plugin.toml` and `plugin.wasm`. It prints the wall time in ms and the size of `plugin.wasm`. `-TargetDir` picks the cargo target folder, `apps/desk/target` by default.

**3. Install it.**

```powershell
desk.exe --install-plugin ..\..\.local\plugins\luiz.hello
```

The desk opens Settings > Plugins on the consent sheet. Press Allow, relaunch the desk, and open it from + > Hello.

## The parts

| Builder | Draws | Event |
| --- | --- | --- |
| `stack(key, children)` | a column of parts | none |
| `text(key, text)` | a line of text | none |
| `button(key, label)` | a button | `Event::Click(key)` |
| `table(key, columns, row_count)` | a scrolling table; its rows come from `Plugin::rows` | none |
| `row(key, cells)` and `rows(rows)` | the answer to `rows(range)`: `rows` of `row`s of `text` cells | none |
| `part(key, kind, children)` | any other `Kind` the desk draws: badge, chip, switch, segmented, tabs, input, virtual-list | `Change` and `Submit` for switch, segmented, tabs and input |

The desk calls `view` after every event, so `handle` changes the struct and `view` draws it again. A plugin shows at most one `table` or virtual-list. A call into the host, such as `desk_plugin_sdk::notify`, needs the matching capability in `plugin.toml`, `notify = true` under `[capabilities]`, or the desk refuses the install.
