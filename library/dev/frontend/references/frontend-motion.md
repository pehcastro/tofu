---
id: frontend-motion
domain: dev
document: whether to animate, duration, easing, springs, what to animate, interaction states, orchestration, performance and reduced motion
---

# Motion in an interface

Motion keeps a person oriented while the screen changes. When they notice the animation itself, there was too much of it. The commonest mistake is not a bad curve; it is animating something that should not move at all.

## How often it is seen decides

- Hundreds of times a day, a shortcut, a palette, a tab switch: no entrance and no exit. The press still answers at once.
- Dozens of times a day, hover, list navigation, a filter toggle: remove it or cut it to the least that reads.
- Occasionally, a modal, a drawer, a toast, a page change: a standard short animation.
- Once, onboarding or a first success: room for more.

## Duration and easing

- Press feedback 100 to 160 ms, a tooltip 125 to 200, a menu 150 to 250, a modal or drawer 180 to 280, a whole sequence 400 to 600.
- `ease-out` to enter or exit, `ease-in-out` between two places on screen, `ease` for a colour change, `linear` for constant motion.
- Slow where the person decides, fast where the system answers: a hold-to-delete fills over 2 s linear and snaps back in 200 ms when released.

## Shape of an entrance

- `opacity: 0` and a short `translateY`, 4 to 16 px for a small thing and 20 to 40 px for a large one.
- `translateY(100%)` moves an element by its own height, so a drawer hides offscreen with no number to go stale.
- A size change measures first. A FLIP measures the old box, applies the new layout, and animates a `transform` from the old box to the new.
- Order: backdrop, container, content, actions. Stagger 30 to 60 ms across three to seven related items, never blocking input, never on exit.
- Enter from the bottom, leave to the bottom. A toast leaves by the edge it came from.
- Hover answers at once and eases out over about 150 ms.

An entry with no script carries its first frame in `@starting-style`:

```css
.toast {
  transition: opacity 180ms ease-out, transform 180ms ease-out;
  @starting-style {
    opacity: 0;
    transform: translateY(100%);
  }
}
```

## Springs and gestures

A spring keeps its velocity when its target changes, so it suits drag and anything grabbed mid-flight; a duration suits a fade, progress, or motion that must end at a known time. Keep bounce low and spend it on drag-to-dismiss, never on a routine menu. A flick dismisses on distance over time, not only past a distance.

## View transitions

`document.startViewTransition(update)` snapshots the page, runs `update`, and crossfades; `@view-transition { navigation: auto; }` does the same across a same-origin navigation. Use them for a change of view, not for a control clicked repeatedly or motion that must be cancelled halfway, and put their animations under the reduced motion query too.

## Reduced motion

```css
@media (prefers-reduced-motion: reduce) {
  .panel { transition: opacity 150ms ease; transform: none; }
}
```

Infinite loops, parallax and gesture physics become static or instant. Opacity and colour that say something happened stay.

## Checking it

- `document.getAnimations()` lists every running CSS animation, transition and Web Animation; `effect.getComputedTiming().duration` is the real duration and `effect.getKeyframes()` names what moves.
- The Rendering panel in DevTools turns on reduced motion; in the project's own tests, a stubbed `matchMedia` reports it.
- The DevTools Animations panel plays motion at a quarter of the speed, where an overlapping crossfade, a wrong origin or two properties drifting apart become visible.
