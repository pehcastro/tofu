---
id: frontend-accessibility
domain: dev
document: accessible names, contrast, focus, dialogs, keyboard patterns and form semantics, with the axe, jsx-a11y, Svelte compiler and vuejs-accessibility checks that catch each
---

# Accessibility that a check can see

The target is WCAG 2.2 AA. Every item also helps a person with no impairment: in sunlight, one-handed, on a slow phone, tired.

## Native first, primitives second

A native element brings its role, its focus and its keys. A `div` with a click handler has none of the three, and adding them by hand is more code than the button. Where nothing native fits, use the project's accessible primitives, Base UI, Radix, React Aria, Bits UI, Reka UI or its own, for tabs, menus, selects, comboboxes, popovers and dialogs. Restyle a primitive freely; never rewrite its keyboard handling.

State goes in the semantics as well as the look: `aria-pressed` on a toggle button, `aria-selected` on a tab, `aria-current="page"` on the current link, `aria-expanded` with `aria-controls` on a disclosure. Headings go in order without skipping a level.

## Keyboard patterns

Inside a composite widget, the arrow keys move and the widget is one tab stop. Keep one stop with a roving `tabindex`, `0` on the active item and `-1` on the rest, or keep focus on the container and point at the active item with `aria-activedescendant`. Enter and Space activate, Escape closes, Home and End jump. The keys for each widget are in its Authoring Practices pattern; follow them rather than inventing new ones.

## Dialogs

A native `<dialog>` opened with `showModal()` makes the rest of the page inert and closes on Escape, which is most of the work. A primitive does the same through its own focus trap. Either way, check that focus lands inside on open and goes back to the trigger on close, because both break when the trigger unmounts or the dialog is rendered conditionally.

## Size, text and reflow

The page reflows at 320 px wide; a wide table may scroll inside a labelled region that the keyboard can reach. Text at 200 percent, and spacing overridden to line height 1.5, letter spacing 0.12em and word spacing 0.16em, still fits without clipping. The viewport meta never sets `user-scalable=no` or `maximum-scale=1`.

## Announcing

A live region exists in the page before its text changes; one inserted together with its text is often not read. A region still loading carries `aria-busy="true"`.

## The checks, by framework

axe-core runs on the rendered page in every framework. Run it through the project's existing test setup, or the axe DevTools extension on the dev server, and expect no violations. axe ships `target-size` turned off; `.options({ rules: { "target-size": { enabled: true } } })` runs it. axe cannot see focus visibility, focus order, a keyboard trap or whether a name makes sense, so a keyboard test stays.

The static checks run on the source:

- JSX, in React, Preact or Solid: eslint-plugin-jsx-a11y.
- Svelte: the compiler's own `a11y_` warnings, printed by the build and by svelte-check.
- Vue: eslint-plugin-vuejs-accessibility.

Turning a check off is not a fix. Where one is wrong for one element, disable it on that line with the reason and say so in the report.
