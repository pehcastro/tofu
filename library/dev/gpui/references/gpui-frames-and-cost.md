---
id: gpui-frames-and-cost
domain: dev
framework: gpui-ce
document: how gpui-ce schedules a frame, what an idle window costs, which views re-render, what hover, clicks, tooltips, focus and a caret dirty, and what each primitive costs per frame
---

# Frames and what they cost in gpui-ce

gpui-ce is immediate mode: a dirty view re-runs `render`, layout, prepaint and paint for its whole subtree and rebuilds its part of the scene. Smooth motion is mostly a question of how little is dirty each frame and of drawing nothing when nothing moves. Paths are in the gpui-ce repository, under `crates/gpui/src` unless named.

## Asking for a frame

- `Window::request_animation_frame()` (`window.rs`) is `on_next_frame` plus `cx.notify` of the view rendering when it is called. It dirties that view only, not the window.
- `Context::notify` dirties one entity. `Window::refresh` and `App::refresh_windows` set `refreshing`, which disables every cached view for that frame. Never use either for motion.
- `with_animation`, `with_spring`, `.transitions(..)` and `Transition::evaluate` all call `request_animation_frame` while they are still moving and stop when they settle. An idle window draws nothing: the frame callback runs, finds nothing dirty and returns.
- `Animation::with_max_fps(n)` takes a different path: it spawns a timer task per tick that notifies the view after `1/n` seconds, not tied to vsync, re-armed only after the next layout. Two such animations drift against each other and against the display. For a spinner use `Animation::repeat_synced()`, which takes the vsync path and phase-locks every synced animation to one app clock (`App`'s synced animation epoch).

## The caps that are not obvious

- An unfocused window with something animating is capped at 30 fps: `WindowOptions::inactive_frame_interval` defaults to `Some(33_333 µs)` (`platform.rs`), and the cap lifts for about a second after fast input. Set it to `None` for a window that must animate while unfocused, and never judge motion on an unfocused window.
- A thermal state of serious or critical caps a focused window at 60 fps.
- Windows paces on the compositor clock (`DwmFlush`), so a draw longer than one refresh interval misses a vsync and the next frame lands one interval later: at 144 Hz a 7.5 ms draw shows as an uneven 72 fps.

## Keep the dirty set small

- Put anything that animates in its own `Entity` and render it with `.cached(style)` (`view.rs`). A cached view replays its last prepaint and paint when its bounds, content mask and text style are unchanged and it is not dirty, so the frame request re-renders the moving part only.
- Never animate inside the view that holds a long list or a whole screen; the frame request will re-render all of it every frame.
- Many spinners: one small view that draws them all from one phase beats one animation element each, which is one notify each.

## What dirties more than it looks

Line numbers are at the vendored revision 175ef66.

- A notify dirties the view and every ancestor view (`window.rs:2178-2189`). A cached view that misses re-renders its whole subtree with caching off, so every cached view inside it renders too (`view.rs:447-457`). A small moving or blinking view stays cheap only when no cached view above it holds much else.
- `.hover(style)` and `.group_hover(..)` notify the view rendering the element on every enter and leave (`elements/div.rs:3247-3271`, `3274-3292`). A hover style on each row of a long list re-renders that whole view on every crossing: give hover to the smallest view that owns it, or one shared highlight.
- `on_click` calls `window.refresh()` on the mouse down inside it and again on the mouse up (`elements/div.rs:3383-3392`, `3495-3512`), and so does the pressed style (`elements/div.rs:3640-3672`). A refresh skips every cached view (`view.rs:435`), so each click re-renders the whole window twice. Where that cost shows, press on `on_mouse_down` and notify the small view that owns the press.
- Showing or hiding a tooltip refreshes the window (`elements/div.rs:4153-4178`), and so does moving focus (`window.rs:2285-2297`).
- A `deferred` popover belongs to the view it is declared in and hangs from its dispatch node (`window.rs:4276-4281`), so a caret blinking inside a popover declared in a cached card dirties the card and everything above it.
- A focused text field blinks forever: its caret task notifies every 500 ms (`examples/view_example/example_editor.rs:91-105`, under `crates/gpui`). A preview draws no caret and holds no focus.
- `cx.refresh_windows()` queues a refresh of every window (`app.rs:1128-1130`). Never call it from an overlay, a hover or anything per frame.
- A list without a small fixed bound is a `uniform_list`, which renders only the visible rows (`elements/uniform_list.rs:473-490`), or a `list`. Derived data (parsed text, layouts, strings, colours) is computed when its input changes and stored; `render` only reads. Batch incoming chunks and notify once.

## Cost per primitive

- Shadows: a blurred `BoxShadow` covers its bounds plus three blur radii on each side (`gpui_render/src/blur.rs`, `GAUSSIAN_CUTOFF_STANDARD_DEVIATIONS = 3.0`), and every covered pixel runs four integration samples (`gpui_render/src/shaders/shadows.rs`). A 50 px blur fills a region 300 px wider and taller than the element, every frame it is drawn. A shadow with blur 0 takes a cheap distance path. Keep large blurred shadows off moving surfaces, or swap them for a small one while moving.
- Blur and backdrop blur: the subtree renders offscreen, is downsampled by 2 and blurred with up to 32 samples per side. On Windows one blur anywhere in the scene sends the whole frame through an offscreen target. Never animate a blur or put one under a moving parent.
- Paths: `PathBuilder::build` tessellates on the CPU with lyon on every call and draws through an MSAA intermediate. Build a path once and keep it; never rebuild one per frame. A spinner arc is an svg, which avoids this.
- Svg: rasterised once per path and size into the sprite atlas, then drawn as one sprite with a transformation matrix, so rotating it costs nothing extra. An svg path that is not registered with the app's asset source draws nothing and is looked up again every frame.
- Text: the line layout cache keeps two frames; a line reshapes only when its key was not used in the previous frame. Text that changes every frame reshapes every frame.
- Layout: the layout engine is cleared every draw, and a re-rendered view rebuilds its layout tree. Animating margin, padding, width, height, top or left re-runs layout of at least the parent.
- Debug assertions: the gpui crate carries many `debug_assert!` and `cfg(debug_assertions)` blocks on the paint path. A build with debug assertions on is not the build to judge motion on; see gpui-verify.

## Why sub-pixel motion looks stepped

- Quads snap their bounds to whole device pixels when painted (`Window::snap_bounds`). A 6 px rise at scale 1 has six positions, whatever the curve.
- Glyph origins have four horizontal subpixel variants and one vertical, so text moves in quarter pixels across and whole pixels up and down.
- So motion of a few pixels is better as opacity, or as an offset large enough that whole-pixel steps are not seen.

## Never inside an animated render

Building a `Path`; a large blurred shadow that moves; changing margin, padding, size or position; animating blur; rendering a long list in the same view; `refresh`; reformatting or reshaping text that did not change; `with_max_fps` on an element repeated in a row.
