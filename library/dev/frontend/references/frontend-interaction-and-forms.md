---
id: frontend-interaction-and-forms
domain: dev
document: loading, empty and error states, feedback after an action, form fields and validation, URL state and real content
---

# Interaction, states and forms

A person should never have to wonder whether a press worked, whether something can be clicked, or what just changed.

## States and errors

- Empty is the first thing a new person sees: one line on what belongs here and the next step. Never invent a create action for data the person cannot create.
- Offline, a timeout, no permission, a rate limit, invalid input and a server fault are six recoveries, not one message. Retry where retrying can work; sign in again where the session ended.
- An error never blames: enter a date after today, not invalid input.
- When one part of a page fails, the rest stays usable and what was entered stays.
- A spinner suits an action; a skeleton suits content whose shape is known.
- Success shows what happened and the obvious next step, not only a toast.

The project's own tests answer a request however a state needs: an empty list, a 500, a delay that holds the loading state open. One test per state, then a look at each on the dev server through tofu's browser tools.

## Words on controls

- A button names its outcome, Save changes or Send invite, never Submit or OK alone.
- An option that opens a further step ends in an ellipsis: Rename…
- One word per concept everywhere: a run in the list is not a job in the toast.
- 1 run, 2 runs, no runs; never 1 run(s).

## Fields beyond the label

- Labels above fields, one column for a sequence of questions.
- `spellcheck="false"` on an email, a code or a username.
- An input's font is at least 16 px on mobile, or iOS zooms the page when it takes focus.
- Accept what is meant: trim spaces, strip spaces and dashes from a pasted card or phone number, ignore case where case means nothing.
- Autofocus rarely on mobile, where the keyboard then covers the page.
- Enter submits from a single-line field; in a `<textarea>`, Ctrl or Cmd with Enter submits.
- A checkbox or radio and its label are one target with no gap between them.
- A long code or number is shown in groups as it is typed.
- Ask for the least: each field needs a reason to be asked now. Mark the optional ones, not the required ones.

## The project's components, not the browser's

Where the project has a component library or its own styling, the browser's own UI never ships: no `alert`, `confirm` or `prompt`, no native date, time or colour picker, no `title` tooltip. They ignore the theme and look like another program. Use the project's dialog, picker and tooltip, keeping the label, the keyboard and the focus the native one had. eslint's core `no-alert` catches the first three.

## Validation in the platform

The browser already validates `required`, `type`, `min`, `max`, `step` and `pattern`. `:invalid` matches from the first render, so it paints an untouched form red; `:user-invalid` matches only after the person has interacted, and is the one to style. `setCustomValidity(message)` adds a check of your own; an empty string clears it. With `novalidate` on the form, script owns every message, and must then set `aria-invalid` and `aria-describedby` itself.

Read the result as a person would: on the dev server, tofu's browser tools submit the form and show which field holds focus and the error message tied to it.

## Real content

- Design with the longest name, the missing avatar, the empty list and the four-hundred-row list.
- Numbers that compare are right-aligned in `font-variant-numeric: tabular-nums`, with units. Dates, times and numbers go through `Intl.DateTimeFormat` and `Intl.NumberFormat`.
- Relative time for a recent event, an absolute date for an old one, the exact value on hover or focus.
- A sort or filter in force is visible and clears in one action.
