---
id: frontend-performance
domain: dev
document: Core Web Vitals, LCP, INP and CLS, and keeping animation and layout off the main thread
---

# Frontend performance: the three numbers and the main thread

## The numbers

Each Core Web Vital is judged at the 75th percentile of real page loads, mobile and desktop apart: LCP at or under 2.5 s, INP at or under 200 ms, CLS at or under 0.1. Read them with the web-vitals library, `onLCP`, `onINP` and `onCLS`, which applies the reporting rules a raw `PerformanceObserver` does not. A lab run is one sample under one setting; never compare it with a field p75 as if they were the same thing. Without a page to run, name the likely cause in the code and do not claim a metric fails.

## Layout shift

- A skeleton of the wrong height shifts twice: once when it appears and once when it leaves.
- Content that arrives late, a banner, a consent bar, an ad slot, has its space reserved or overlays the page.
- A web font that swaps late moves text; preload the font that renders above the fold, or match the fallback's metrics.

## Responsiveness

INP has three parts, each with its own fix: the wait before the handler runs, the handler itself, and the time to paint after it.

- Paint the visible change first, then do the work.
- Break a long task and yield between chunks, `await new Promise(r => setTimeout(r))`, or `scheduler.yield()` where the browser has it, so the next input is not queued behind it.
- More than about fifty rich rows are paginated or virtualised, chosen on purpose. Infinite scroll loses the footer and the place a person comes back to; it suits a feed, not a table.
- A mutation aims to answer within 500 ms.

## Loading

- The LCP element is in the first HTML, not fetched by script after load. An LCP image has `fetchpriority="high"` and is never lazy; images below the fold are `loading="lazy"`.
- Preload only what a trace shows is discovered late; speculative preloads compete for the same bandwidth.
- A third party origin used early gets `<link rel="preconnect">`.

## Cost of motion

Changing `transform` or `opacity` can stay on the compositor. Colour, border, shadow and filter repaint every frame, which is fine briefly on a small control and wrong on a large surface. Size and position redo layout and cost the most.

- CSS for motion known in advance, script only for motion that follows input; a script animation drops frames exactly when the page is busiest.
- Grain and noise layers sit on a fixed layer with `pointer-events: none`, never inside a scrolling container.
- One animation system per element.

## Measuring it

- Chrome DevTools: the Performance panel with CPU throttling at 4x and a slow network. A frame past 50 ms is a long task.
- Tofu's browser tools record the motion on the dev server frame by frame; a frame past 50 ms there is the same long task.
- Measure with browser extensions off; they add work of their own.
