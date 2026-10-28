---
id: gpui-motion
domain: dev
framework: gpui-ce
document: motion in gpui-ce: which primitive carries velocity, springs and their numbers, eased transitions, presence, a gliding highlight, spinners and reduced motion
---

# Motion in gpui-ce

The rules that make motion feel right are framework neutral and live in interface-craft. This page is how to get them out of gpui-ce. Paths are in the gpui-ce repository under `crates/gpui/src`.

## Which primitive to reach for

| Primitive | Keeps position on retarget | Keeps velocity | Use it for |
|---|---|---|---|
| `SpringConfig::step(state, target, dt)` (`spring.rs`) | yes | yes | anything a person can trigger again mid-flight, held in your own state |
| `with_spring(id, SpringAnimation::new(config).to(target), animator)` (`elements/animation.rs`) | yes | yes | one scalar on one element, keyed by a stable id |
| `.transitions(\|t\| t.bg(m).opacity(m))` on an interactive element | yes | no | colour, opacity, radius and size changes of a styled element |
| `Animated<T>::set` (`animated.rs`), `Window::use_keyed_transition` (`transition.rs`) | yes | no | a value with a duration that may be interrupted |
| `with_animation(id, Animation::new(motion), animator)` | no, restarts | no | motion that runs once or loops: an entrance nobody retriggers, a spinner |

- `SpringConfig::step` is an analytic step, independent of frame rate, that preserves velocity, so retargeting is passing a new `target` with the old `SpringState`. `step_ramp` follows a target moving at a constant rate, such as a pointer. `is_settled(state, target, epsilon)` and `settle_time` say when to stop asking for frames.
- `with_spring` stores position and velocity in element state under its id and steps toward whatever target the latest render gives. A new mount starts at its target unless `.from(initial)`. It asks for frames while unsettled. The id must be the same every render: an id that carries an index or a version number makes a new spring each change and nothing carries. Its animator receives a `SpringAnimationElement`, which is not `Styled`, so one `with_spring` drives one scalar; four scalars are four nested wrappers or four `SpringState`s of your own.
- A spring given to a style transition or to `Animated` (`Motion::spring(config)`, or a `SpringConfig` converted into `AnyMotion`) is sampled from rest at zero progress each time it starts. It keeps the position it was interrupted at and drops the velocity.
- A bare `Duration` converted into motion is linear: `Easing::default()` is `linear` (`motion.rs`). Give every tween a curve: `Duration::with_easing(curve)` from `MotionDurationExt`, or `Motion::new(d).with_easing(curve)`. gpui-ce ships `ease_in_out`, `ease_out_quint` and `quadratic`; a cubic Bezier is a small function of your own.

## Spring numbers

`SpringConfig::new(stiffness, damping, mass)`; `canonical()` returns the natural frequency and the damping ratio.

- Transform default (motion.dev): stiffness 500, damping 25, mass 1. Damping ratio 0.56: one small overshoot, settles in about 0.3 s.
- Scale: 550 and 30. Snappy UI: 600 and 25. Gentler: 400 and 25.
- No overshoot, for a highlight that glides or a value that must not ring: damping near `2 * sqrt(stiffness * mass)`, so 600 and 49.
- From a visual duration `vd` in seconds and no bounce: `w = 2 * pi / (1.2 * vd)`, `stiffness = w * w`, `damping = 2 * w`. 0.2 s gives 685 and 52; 0.15 s gives 1218 and 70; 0.25 s gives 438 and 42. Most of the travel is done at `vd` and the tail runs about 20 percent past it.
- An epsilon in the units of the value: half a pixel or less for position, 0.002 for a 0 to 1 progress.

## Eased tweens

- Opacity, colour and anything not on a spring: `cubic-bezier(0.23, 1, 0.32, 1)` or `cubic-bezier(0.16, 1, 0.3, 1)`. Both start fast and settle long.
- Linear only for constant motion: a spinner, a progress bar that tracks real progress.

## Presence: exit before unmount

gpui unmounts an element the frame it leaves the tree. To animate an exit, keep the entry rendered:

- Hold, per key, the phase (present or exiting) and its spring state in the parent's entity. A key missing from the new list flips to exiting and stays in the render list; drop it once its springs have settled.
- If the key returns while exiting, retarget its springs toward present from where they are. Never restart from zero.
- Exit faster than enter: an exit spring or duration about 0.6 to 0.7 of the enter.

## A highlight that glides between items

One shared highlight that moves between rows or tabs reads as one object; a hover background on each item reads as two overlapping fades and flickers in the gaps.

1. Container `div().relative()`; the highlight first, then the items, so items paint above it. Items carry no `.hover` background of their own.
2. `Div::on_children_prepainted` gives every child's bounds in window coordinates after layout. Store them only when they changed, or the notify loops. Subtract the container's origin.
3. One `on_mouse_move` and one `on_hover` on the container. The hovered item is the one whose stored bounds contain the pointer; in a gap between items keep the previous one. Leaving the container hides the highlight. Keyboard focus sets the hovered item directly, since hover does not change under keyboard input.
4. Step x, y, width and height with `SpringConfig::step` toward the hovered bounds, without resetting velocity when the item changes. Ask for frames only while unsettled.
5. On the first entry place the rectangle at the target at once and fade only opacity in; on leave fade opacity out and keep the rectangle. A highlight that grows from zero or slides away reads as noise.
6. The highlight's rectangle is the item's layout box, the same rectangle the hit test uses. Padding outside that box makes the lit area larger than what it sits on.

## Spinners

- An `svg()` arc with `with_transformation(Transformation::rotate(percentage(t)))` under `with_animation(id, Animation::new(period).repeat_synced(), ..)`. The svg is rasterised once per size and rotated on the GPU.
- Never `with_max_fps`: a 30 fps cap with a fixed angular rate moves the rim of a large spinner several pixels per update, held for several frames at 144 Hz, which reads as stutter.
- Every size a spinner is drawn at must be registered with the asset source; an unregistered svg path draws nothing.
- Many spinners in one cached view, phase-locked by `repeat_synced`.

## Reduced motion

- gpui-ce does not read the operating system setting. The application reads it and calls `App::set_reduce_motion(on)` once at start and when it changes; `App::reduce_motion()` reads it back.
- With it set, `with_animation` renders the resting state (the end of a one-shot, the start of a loop) and schedules nothing, `with_spring` jumps to its target, and style transitions jump to their endpoint. That is every property, opacity included.
- In state you step yourself, apply the finer rule: position, size, offset and scale jump to the target; opacity and colour may still fade, since they carry the information that something changed. A loop that only decorates stops.
