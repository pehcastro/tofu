---
id: react-rendering
domain: dev
document: rendering cost, keys, transitions, hydration and motion in React
---

# Rendering performance in React

A render is a function call over a subtree. It is cheap until it runs on every frame, over a large tree, or in front of a keystroke. Measure before changing anything: the React DevTools Profiler records each commit and which components rendered in it, and "Highlight updates when components render" in its settings, or React Scan, shows renders as they happen.

## What changes every frame stays out of state

Pointer position, scroll offset, drag distance and a size during resize change on every frame. Through `useState` each change renders the subtree.

```tsx
const dot = useRef<HTMLDivElement>(null);
useEffect(() => {
  const onMove = (e: PointerEvent) => {
    dot.current!.style.transform = `translate(${e.clientX}px, ${e.clientY}px)`;
  };
  window.addEventListener("pointermove", onMove);
  return () => window.removeEventListener("pointermove", onMove);
}, []);
```

- In Motion, `useMotionValue`, `useTransform`, `useScroll` and `useSpring` drive an element with no render at all.
- Motion's `x`, `y` and `scale` run on the main thread; `opacity`, `transform`, `filter` and `clipPath` can be handed to the compositor. When a motion must stay smooth while the page loads, animate the whole `transform`.
- When the screen needs only a threshold, subscribe to the threshold: a `matchMedia` query read through `useSyncExternalStore` renders when the query flips, not once per pixel.
- A listener that never calls `preventDefault()` on `touchstart` or `wheel` is added with `{ passive: true }`.

## Urgent and not urgent

Typing must feel immediate; the list it filters can wait a moment.

```tsx
const [query, setQuery] = useState("");
const deferred = useDeferredValue(query);
const results = useMemo(() => search(items, deferred), [items, deferred]);
const stale = query !== deferred;
```

`useTransition` does the same for an update started in a handler, and its `isPending` replaces a hand-managed `loading` flag that a thrown error leaves on. The input's own value always updates outside the transition.

## Keys

A key tells React which item is which across renders. From the data, stable and unique among siblings: an index key on a reorderable list moves one row's typed text and focus to another, and `key={Math.random()}` remounts every row. A key is also the way to reset a subtree on purpose: `<Editor key={documentId} />` starts each document with fresh state.

## Small things that render wrong

- `{count && <Badge />}` renders `0` when `count` is zero. Write `{count > 0 ? <Badge /> : null}`.
- `useState(buildIndex(items))` builds the index on every render and throws it away. Pass the function: `useState(() => buildIndex(items))`.
- A memoised component with a default prop of `() => {}` or `[]` gets a new value each render and never skips. Hoist the default to a module constant.

## Motion in React

- An entrance with no script uses `@starting-style`. A `mounted` flag set in an effect costs a render and can paint the end state first.
- `useReducedMotion()` returns the person's preference; set the distance to zero rather than deleting the animation. `<MotionConfig reducedMotion="user">` does it for every component below it: position, size and transform values jump, opacity still fades.

## Hydration

In a server-rendered app the first client render must produce the server's HTML. Dates, times and numbers formatted by locale or timezone, `window`, `localStorage` and `matchMedia` all differ between the two.

- Render a stable value first, or read the client value with `useSyncExternalStore` and a server snapshot.
- A theme or preference that must not flash is applied by a small inline script that runs before hydration, not by an effect after it.
- `suppressHydrationWarning` goes on the single element whose text is expected to differ, such as a relative timestamp, and nowhere else.
- An input typed into before hydration keeps its focus and its value.

React reports a mismatch in the console as `Hydration failed because the server rendered` or `A tree hydrated but some attributes of the server rendered HTML didn't match`. Opening the page on the dev server with a timezone and locale different from the server's and reading the console catches both.
