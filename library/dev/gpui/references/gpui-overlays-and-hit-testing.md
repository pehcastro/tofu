---
id: gpui-overlays-and-hit-testing
domain: dev
framework: gpui-ce
document: overlays and hit testing in gpui-ce: deferred priority and inherited text style, occlude per control, anchored placement and flip, window_control_area, absolute insets, hit areas under a transform, closing on mouse down out, device-pixel snapping
---

# Overlays and hit testing in gpui-ce

A popover, menu, tooltip or modal is a `deferred` element placed by `anchored`, and everything about whether it is on top and clickable is decided by hitbox order. Paths are in the vendored copy at revision 175ef66, under `crates/gpui/src`.

## deferred

- `deferred(child)` keeps the child's layout in the current tree and paints it after all its ancestors (`elements/deferred.rs:7-25`). Its prepaint hands the child to `window.defer_draw` (`elements/deferred.rs:59-62`), and the draws run after the main tree (`window.rs:3387`), sorted by priority, higher on top (`window.rs:3557`, `elements/deferred.rs:87-92`). `priority_auto()` sits one above the deferred element that caused it (`elements/deferred.rs:94-100`). Nesting stops with a panic past ten levels (`window.rs:3552`).
- A deferred draw keeps the view, the dispatch parent and the text style stack of the place it was declared (`window.rs:4276-4281`). So an overlay inherits the font, size and colour of whatever it sits in: set its own text style on its root. And it belongs to that view: a caret or a hover inside it dirties the declaring view and every ancestor (`window.rs:2178-2189`), so never declare an overlay inside a cached card that holds much else.

## Hitboxes and occlude

- Hit testing walks hitboxes from the last inserted to the first (`window.rs:1185-1186`), so whatever prepaints later is in front, deferred draws included.
- `occlude()` sets `HitboxBehavior::BlockMouse`: everything behind loses hover, clicks and tooltips (`window.rs:961-985`, `elements/div.rs:1423-1426`). `block_mouse_except_scroll()` keeps scrolling for what is behind, and the source says it is typically preferred (`elements/div.rs:1420-1421`, `1439-1442`). Occlude the overlay's own panel and controls, never a full-window layer unless the overlay is modal.
- `window_control_area(area)` counts only while its hitbox is in the hit test's scrollable set (`window.rs:1902-1906`), and an occluding hitbox ends that set (`window.rs:778-781`, `798-800`). An occluding hitbox above a title bar kills dragging and the caption buttons there; `block_mouse_except_scroll` does not. The vendored patch DESK-633 keeps control areas inside cached views (`window.rs:3733-3741`).

## Placing it

- `anchored()` is absolutely positioned, measures its laid-out children (`elements/anchored.rs:111-117`, `135-140`), by default switches to the other side of its anchor when it would leave the window (`elements/anchored.rs:31`, `155-178`), then snaps inside the window (`elements/anchored.rs:189-202`). Place menus and popovers with it, from their measured size, never from a guessed one.
- An `absolute()` child keeps its `auto` insets (`style.rs:1064`) unless you write them: give it an explicit `top` and `left`, or `right` and `bottom`.

## Clicks

- `on_mouse_down_out` fires in the capture phase on a mouse down outside the element (`elements/div.rs:263-275`). Put it on the overlay panel, not the anchor: a deferred panel is outside its anchor, so the anchor's handler closes the panel on the mouse down of a click inside it.
- `on_click` needs the mouse down and the mouse up in the same hitbox (`elements/div.rs:3383-3392`, `3495-3510`), so a row that closes or moves on mouse down never gets its click.

## Hit areas and pixels

- `svg().with_transformation(..)` changes only the rendering, never the hitbox or the layout (`elements/svg.rs:64-66`). A hit area for anything drawn under a transform is computed with the same transform as the paint.
- Quads snap their bounds to whole device pixels at paint (`window.rs:2964-2973`). Snap an edge once, in device pixels, and derive the rest from it; two edges rounded separately leave a one-pixel gap or overlap.
