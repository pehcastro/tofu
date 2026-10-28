---
id: gpui-state-and-entities
domain: dev
framework: gpui-ce
document: state in gpui-ce: one owning entity, observe and subscribe, build once and set, keyed element state, cx.defer for re-entrant updates, replies keyed to their request, Task ownership, background_spawn, on_app_quit and the executor clock
---

# State and entities in gpui-ce

Every value has one owner, an `Entity`, and every view reads it through that entity and is told when it changes. Stale state is nearly always a second copy or a reply nobody checked. Paths are in the vendored copy at revision 175ef66: under `crates/gpui/src`, except `examples/...`, which is under `crates/gpui`, and a path naming its own crate.

## One owner

- Name the owner of a value before writing it. The example shares one `Entity<String>` between an editor and anything that reads it, rather than copying it (`examples/view_example/example_editor.rs:39-63`). A count, a colour or a key is derived from the same collection the view lists, never from a second one kept beside it.
- `cx.observe(&entity, ..)` runs on every `notify` of that entity (`app/context.rs:61-80`); `cx.subscribe(&entity, ..)` runs on each typed event it emits through `EventEmitter` (`app/context.rs:97-117`). `cx.notify()` marks the entity changed (`app/context.rs:229-231`).
- A `Subscription` unsubscribes when dropped (`subscription.rs:188-194`). Keep it in a field, as the example's `_subscriptions` (`example_editor.rs:28`, `71`); `.detach()` keeps it until the entities drop (`subscription.rs:163-168`).

## Build once

- An entity is built once and updated with setters, never rebuilt per event: a rebuilt entity drops its subscriptions, tasks and focus.
- `window.use_keyed_state(key, cx, init)` builds on the first frame and returns the same entity while the element renders in consecutive frames (`window.rs:4051-4071`). `use_state` keys by the caller's source location (`window.rs:4080-4091`), so in a loop every row shares one key: key row state by the row's identity, never by its index or its value.

## Re-entrant updates

- Updating or reading an entity while it is already being updated panics: `cannot update .. while it is already being updated` (`app/entity_map.rs:142`, `207-210`). Push the second update to the end of the effect cycle with `cx.defer` (`app.rs:2083-2087`) or `window.defer` (`window.rs:2478-2483`).

## Tasks and replies

- `cx.spawn` returns a `Task` that must be held or detached (`app/context.rs:234-245`); dropping it cancels it (`crates/gpui_scheduler/src/executor.rs:380-385`). Hold the one task per job in a field and replace it, which cancels the old one, as the caret does (`example_editor.rs:80-88`); never add a second.
- The spawned closure gets a `WeakEntity`, and `this.update(..)` fails once the entity is gone (`example_editor.rs:97-103`). Check every reply against the id it was asked for and drop it when that id is no longer current.
- File, process, network and parse work runs on `cx.background_spawn(..)` (`app/context.rs:863-868`), and its result is applied on the foreground with `this.update`. Draw a loading state at once; never do that work in `render`, in mount or in an event handler.

## Lifetime

- Anything spawned dies with its owner: in `Drop`, and in `cx.on_app_quit(..)`, whose future is polled for up to the shutdown timeout (`app/context.rs:206-225`). Spawning on the main thread after quit is a debug panic (`app.rs:2053-2054`). `cx.on_release(..)` runs when the entity is released (`app/context.rs:135`).
- Time in a test or a driven run comes from the executor clock: `executor.now()`, `timer` and `advance_clock` (`executor.rs:189-212`), never `Instant::now()` in the view.
