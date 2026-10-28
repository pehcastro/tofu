---
id: gpui-layout
domain: dev
framework: gpui-ce
document: laying out in gpui-ce: flex inside the area the parent gives, min_w_0 and truncate for text that grows, shrink by basis, sized cached views, uniform_list rows, scrolling and the narrow-width check
---

# Layout in gpui-ce

gpui-ce lays out with taffy, which implements CSS flexbox and grid. A layout defect is nearly always a flex fact, not a missing pixel. Paths are in the vendored copy at revision 175ef66: under `crates/gpui/src`, except `examples/...`, which is under `crates/gpui`, and a path naming its own crate.

## The defaults

- A `Style` starts as `display: Block`, `position: Relative`, every inset `auto`, `min_size: auto`, and as a flex item `flex_grow: 0`, `flex_shrink: 1`, `flex_basis: auto` (`style.rs:1053`, `1063`, `1064`, `1069`, `1081-1083`). Write `.flex()` and a direction; nothing is a row until you say so.
- `flex_1()` is grow 1, shrink 1, basis 0, so siblings share the space evenly; `flex_auto()` keeps basis auto, so a long label takes more (`styled.rs:290-303`).

## Lay out inside the given area

- Size from the parent: `flex_1`, `size_full`, `w_full`. A fixed number is only a `min_*` or `max_*`; mockup coordinates are never a layout.
- Controls sit in flow inside their strip, never `absolute` over it: an absolute child leaves the flow and paints over its siblings at any width.
- A screen is one frame. A card drawn inside a card is a layout defect, not a style.

## Text that can grow

- `min_size: auto` gives a flex item an automatic minimum of its content, so a row holding long text will not shrink below the text and pushes its siblings out of the box. Put `min_w_0()` on every flex item between the row and the text (`min_w` with suffix `0`, `crates/gpui_macros/src/styles.rs:1360`, `1419`). The automatic minimum is 0 only when the item's overflow is not visible (`style.rs:1470`).
- `truncate()` is `overflow_hidden().whitespace_nowrap().text_ellipsis()` (`styled.rs:241-243`); `line_clamp(n)` caps wrapped lines (`styled.rs:247`). Every text that can grow gets one of them, and its parents get `min_w_0`.

## Shrink by basis

- Taffy shrinks each item by `flex_shrink` times its basis, so with many tabs or files a close button shrinks with the labels until it is gone. Give a control that must stay whole `flex_none()` or `flex_shrink_0()` (`styled.rs:317-322`, `361-364`), and let the label take `flex_1` with `min_w_0` and `truncate`.

## Cached views need a size

- `.cached(style)` is laid out from `style` and is not measured from its content: caching requires a definite size (`view.rs:280-284`). Give it `size_full()` or a definite height, as `examples/view_example/example_input.rs:119` does. A content-sized cached view changes its bounds, and changed bounds miss the cache (`view.rs:431-435`).

## Lists

- `uniform_list(id, count, render)` renders only the visible range (`elements/uniform_list.rs:473-490`) and needs a parent with a fixed or max height (`uniform_list.rs:17-19`).
- It measures one item, item 0 or the one named by `with_width_from_item`, at max-content width (`uniform_list.rs:626-628`, `672-683`) and gives every row that height. Rows of different heights belong in `list(..)`.
- Each row is laid out as a root in the list's width (`uniform_list.rs:500-509`), so a row without `w_full()` is as wide as its content and its hover fill stops short.

## Scrolling

- `overflow_y_scroll()` is on `StatefulInteractiveElement` (`elements/div.rs:1479`, `1747`), so the element needs an `.id(..)`. gpui draws no scrollbar: `scrollbar_width` only reserves space (`style.rs:1471-1474`). Every list or panel that can outgrow its box scrolls inside it with the project's own visible bar component.

## Check

- Look at every new view at the narrowest width the window allows and at the largest, with the longest real label you can find. A view that only fits the mockup width is not done.
