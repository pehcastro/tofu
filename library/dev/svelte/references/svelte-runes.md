---
id: svelte-runes
domain: dev
document: Svelte 5 runes, snippets and templates, with the compiler and runtime codes for each mistake
---

# Svelte 5 and runes

Svelte 5 compiles a component in runes mode as soon as it uses one rune, and in runes mode the Svelte 4 forms stop compiling. Write runes in every new component, and convert a legacy component whole when you change it.

## Svelte 4 and what replaces it

| Svelte 4 | Svelte 5 | The code that reports it |
|---|---|---|
| `export let title` | `let { title } = $props()` | `legacy_export_invalid` |
| `$: doubled = count * 2` | `let doubled = $derived(count * 2)` | `legacy_reactive_statement_invalid` |
| `$: console.log(count)` | `$effect(() => console.log(count))`, or `$inspect(count)` | `legacy_reactive_statement_invalid` |
| `$$props`, `$$restProps` | `let { a, ...rest } = $props()` | `legacy_props_invalid`, `legacy_rest_props_invalid` |
| `on:click={handler}` | `onclick={handler}` | `event_directive_deprecated` |
| `createEventDispatcher` | a callback prop, `onsave(item)` | its own deprecation note |
| `<slot>`, `<svelte:fragment>` | `{#snippet name()}` and `{@render name()}` | `slot_element_deprecated` |
| `<svelte:component this={C}>` | `<C>` | `svelte_component_deprecated` |
| `<svelte:self>` | `import Self from "./Self.svelte"` | `svelte_self_deprecated` |
| `use:action` | `{@attach fn}` | none |
| a `writable` store shared between components | a class with `$state` fields | none |
| `$app/stores` | `page` and `navigating` from `$app/state` | its own deprecation note |

## State

`$state` is for values that change and are read by the template, a `$derived` or an `$effect`. A constant, a lookup table or a value only a handler uses is a plain variable.

An object or array in `$state` is a deep proxy. Mutating it updates the screen, and every read pays for the proxy. Data that is replaced rather than edited, an API response, a page of results, goes in `$state.raw` and is reassigned:

```svelte
<script>
  let results = $state.raw([]);
  async function search(query) {
    results = await fetchResults(query);
  }
</script>
```

Three things break a proxy without an error. Destructuring it, `let { done } = todo`, copies the value once. Comparing it with `===` against the object it wrapped is always false. Passing it to `structuredClone` or a library that walks it fails or sees the proxy; pass `$state.snapshot(todo)`.

A module shares state as a class with `$state` fields, or as an object whose state is read through a getter. Exporting a `$state` variable that is reassigned does not compile: `state_invalid_export`.

## Derived values

A value computed from state or props is `$derived`. Props deserve the warning most: the script runs once, so a plain variable built from a prop never changes again.

```svelte
<script>
  let { type } = $props();
  let color = $derived(type === "danger" ? "red" : "green");
</script>
```

`$derived.by(() => { ... })` takes a function body. A derived value can be assigned, for an optimistic update, and goes back to its expression when an input changes. The compiler warns `state_referenced_locally` where a prop or state is read once outside a reactive place.

## Effects

`$effect` is the escape hatch. Before writing one, check the better home:

| The effect does | Write instead |
|---|---|
| sets state from other state | `$derived` |
| runs after a click or a submit | the code in that handler |
| sets up one element, a tooltip, a chart | `{@attach}` |
| adds a listener to `window` or `document` | `<svelte:window>` or `<svelte:document>` |
| subscribes to something outside Svelte | `createSubscriber` from `svelte/reactivity` |
| logs a value while debugging | `$inspect`, and `$inspect.trace()` inside an effect to see why it ran |

An effect that starts something returns its stop. Effects never run on the server. An effect tracks what it reads synchronously, so a value read after an `await` does not re-run it. An effect that writes state it reads loops until Svelte throws `effect_update_depth_exceeded`.

## Props and binding

Props are read-only. A child changes its parent's value by calling a callback prop, or through a prop the child marks `$bindable()` and the parent binds with `bind:`. Binding is the exception. In development, Svelte warns `ownership_invalid_mutation` when a child mutates a prop it was not bound to, and throws `bind_not_bindable` when a parent binds a prop that is not bindable.

`$props.id()` gives an id that matches between server and client.

## Lists

`{#each items as item (item.id)}` keys each row by its identity. The index is never a key for a list that changes order. Keep the item whole when a row binds to it: `bind:value={item.count}` works, a destructured `{#each items as { count }}` cannot be bound in runes mode (`each_item_invalid_assignment`). Two items with one key throw `each_key_duplicate`.

## Checking it

- `svelte-check --fail-on-warnings` in the gate. Every compiler code above is a warning or an error there, with the TypeScript errors.
- `<!-- svelte-ignore code -->` silences one code on the next element. Use it only where the warning is wrong for that line.
- Runtime warnings, `ownership_invalid_mutation`, `state_proxy_equality_mismatch` and the rest, go to `console.warn` in development. Running the flow on the dev server and reading the browser console catches them.
