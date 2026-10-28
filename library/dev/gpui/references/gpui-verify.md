---
id: gpui-verify
domain: dev
framework: gpui-ce
document: judging gpui-ce motion and frame cost: the build to judge on, the window state, reading present intervals, and what a measurement must carry
---

# Verifying gpui-ce motion

Motion is judged by feel and proven by numbers. A clean `cargo clippy` says nothing about either, and an eye on a debug build sees the build, not the motion.

## The build

- Never judge on the dev profile. It runs gpui-ce with debug assertions on, and the gpui crate has many `debug_assert!` and `cfg(debug_assertions)` blocks on the paint path.
- Use an optimized profile. A judging profile that rebuilds quickly inherits release and keeps incremental builds and parallel codegen:

```toml
[profile.motion]
inherits = "release"
lto = false
codegen-units = 16
incremental = true
debug = "line-tables-only"
```

- When the project already names a judging profile, use that one. When it has none, say so in the report rather than judging on dev.
- `lto` and `codegen-units = 1` cost build time for little frame time; they belong to the shipped release, not to iteration.

## The window

- Focused. An unfocused window with something animating is capped at 30 fps by `inactive_frame_interval` (see gpui-frames-and-cost), so an automated run that opens its window without focus measures the cap.
- At the display's real refresh rate. gpui-ce exposes no refresh rate; measure it as the median present interval over two seconds of motion. At 144 Hz a frame is 6.94 ms, at 60 Hz 16.7 ms. A frame budget is derived from the measured interval, never carried from another machine.
- With nothing else drawing in the same view, unless that is what is being measured.

## Reading frame numbers

- The gpui `profiler` feature (`gpui-ce` crate feature `profiler`) enables `Window::frame_duration_snapshot()`, whose `present_interval_histogram` is the number that shows judder, and `draw_duration_histogram`, the CPU time in `draw`. `Window::input_latency_snapshot()` gives input to present latency.
- The same feature enables a debug frame overlay painted straight into the scene: `Window::set_debug_frame_overlay_mode` and `cycle_debug_frame_overlay_mode`. It shows draw duration, not present interval. Setting its mode refreshes the window once.
- An overlay of your own that calls `refresh` while open disables every cached view and perturbs what it measures.
- Without the feature: record `Instant::now()` in a `canvas` paint callback, push `now - last`, and report the count, p50, p95, max, and the count of intervals over 1.5 times the refresh interval (10.4 ms at 144 Hz, 25 ms at 60 Hz).

## What a measurement carries

One line opens every measurement, and two runs whose lines differ are not compared:

- the GPU, its vendor and driver, from `Window::gpu_specs()`, and whether it is software emulated. On Windows the `Using GPU` log line is printed before the adapter is tested, so with several adapters it can name one that failed.
- the backend: Direct3D 11 on Windows, Metal on macOS, wgpu with Vulkan or GL on Linux.
- the scale factor (`Window::scale_factor()`), the measured refresh interval, the build profile, whether the window was focused, and whether reduced motion was on.

## The checks a person runs

- Trigger the motion and trigger it again halfway: it must turn around from where it is, with no jump back to its start and no stop.
- Sweep the pointer across five items quickly and reverse: a shared highlight never blinks between items.
- Leave the window idle: no frames are drawn, so the present interval histogram stops growing.
- Turn reduced motion on: positional motion is gone and nothing is left in a half-state.
- Look at text only within one OS. DirectWrite, CoreText and swash rasterise the same string at different weights, and a transparent window turns subpixel text off on all three.

## The report line

Name each motion as property, from and to, and either a spring (stiffness, damping, mass) or a duration and a curve, with where the numbers came from. Then the measured frame numbers with the line above. A motion with no numbers beside it was not judged.
