---
id: gpui-input-and-focus
domain: dev
framework: gpui-ce
document: text input, shortcuts and focus in gpui-ce: EntityInputHandler, actions and key_context, track_focus and dispatch order, focusing a new view, the caret task and shifted keys on Windows
---

# Input and focus in gpui-ce

Every key goes to the focused element and its ancestors, and every typed character goes to an input handler. A shortcut or a field that does nothing is almost always a focus or a dispatch-order fact. Paths are in the vendored copy at revision 175ef66: under `crates/gpui/src`, except `examples/...`, which is under `crates/gpui`, and a path naming its own crate.

## Text goes through EntityInputHandler

- A text field implements `EntityInputHandler` (`input.rs:13`) and registers it in `paint` with `window.handle_input(&focus_handle, ElementInputHandler::new(bounds, entity), cx)` (`examples/view_example/example_editor.rs:486-490`). It registers only while that handle is focused (`window.rs:5385`). IME composition, dead keys and paste arrive there (`input.rs:41-44`).
- Ranges in it are UTF-16 offsets (`input.rs:67-73`, `UTF16Selection`): convert to byte offsets before slicing a `String`, and clamp to a char boundary as the example does (`example_editor.rs:55-62`).
- Never type with `on_key_down` and `key_char`: key listeners run after the bindings (`window.rs:6126-6153`) and see no composition.

## Shortcuts are actions

- Declare them with `actions!(namespace, [..])`, bind with `KeyBinding::new("left", Left, Some("TextInput"))`, name the context on the element with `.key_context("TextInput")`, and handle with `.on_action(..)` (`examples/view_example/view_example_main.rs:36-39`, `144-153`; `example_input.rs:93-94`; `example_editor.rs:523`).
- A binding matches against the key contexts on the path from the root to the focused element (`window.rs:5981-5982`, `6047-6051`). An element outside that path never sees it.

## Focus

- With nothing focused, or a handle that no element in the rendered frame tracks, keys dispatch from the root alone (`window.rs:6305-6312`). So a new view's handle must be `track_focus`ed by an element that renders.
- `track_focus` adds a bubble-phase mouse-down that focuses the handle and calls `prevent_default` so a focusable parent does not take it (`elements/div.rs:3180-3195`). A `track_focus` on a root or a large container takes focus on every click inside that no focusable child handled.
- Key listeners run in capture from the root down to the focused node, then bubble back up (`window.rs:6193-6214`). A root `capture_key_down` that stops propagation swallows every key below it.
- `window.focus` returns at once when the handle already has focus; otherwise it clears pending keystrokes and refreshes the whole window (`window.rs:2285-2297`). Focus on a transition, never on every render.
- To focus a view you just built, implement `Focusable` (`window.rs:703-713`) and call `cx.focus_view(&entity, window)` (`app/context.rs:287-289`) from `window.defer(cx, ..)` (`window.rs:2478-2483`), so the entity is no longer leased when focus moves.

## The caret

- The example starts a caret task on focus and drops it on blur (`example_editor.rs:43-48`, `80-89`); the task flips the caret and notifies every 500 ms (`example_editor.rs:91-105`). A focused field blinks forever, so a preview or a thumbnail draws no caret and holds no focus.
- Hover styles are suppressed after keyboard input (`elements/div.rs:4127-4134`), so a keyboard-only path must show focus with `focus_visible`, not hover.

## Shifted keys on Windows

- On Windows, Shift with a digit or a punctuation key arrives as the shifted character with shift cleared: Shift+1 is `!` (`crates/gpui_windows/src/keyboard.rs:147-157`, `175-200`). Bind `"!"`, never `"shift-1"`; a binding with both shift and a shifted key logs that it is likely a bug (`crates/gpui_windows/src/keyboard.rs:48-50`).
