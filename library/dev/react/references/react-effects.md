---
id: react-effects
domain: dev
document: when an effect is needed, its dependencies, its cleanup and what to do instead
---

# Effects, and when not to write one

An effect keeps a component in step with a system React does not own while the component is on screen: a socket, a subscription, a third-party widget, a fetch keyed by what is shown. Anything else has a better home, and most effects in a review are that anything else.

## The question that decides

Why does this code run? If the answer is "because the person did something", it belongs in that handler. If the answer is "because the component is showing", it is an effect. If the answer is "because a value changed", it is usually a calculation and belongs in the render body.

| The effect does | Write instead |
|---|---|
| sets state from props or other state | compute it during render |
| filters or sorts a list into state | compute it during render, `useMemo` only if a profile shows the cost |
| clears all state when an id prop changes | `<Profile key={userId} />` |
| runs when a `submitted` flag flips | the code in the submit handler |
| calls the parent's `onChange` after local state changes | call it in the same handler that sets the state |
| copies a browser value into state on an event | `useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot)` |
| runs app setup with an empty list | a call in the entry module |
| sets a `mounted` flag to start an entrance | `@starting-style` in CSS |

A chain of effects, each setting state that triggers the next, renders once per link and is hard to follow. Compute what you can during render and do the rest in the handler that started it.

## An effect that is right

Every effect that starts something stops it:

```tsx
useEffect(() => {
  const connection = createConnection(roomId);
  connection.connect();
  return () => connection.disconnect();
}, [roomId]);
```

A fetch in an effect races: a slow answer for the previous input can land after the fast one for the current input. Drop the stale one.

```tsx
useEffect(() => {
  let ignore = false;
  fetchResults(query).then(json => {
    if (!ignore) setResults(json);
  });
  return () => { ignore = true; };
}, [query]);
```

When the project has a data library or a framework loader, use it rather than this: it already handles caching, races and Back.

Strict Mode runs setup, cleanup and setup again in development. An effect that misbehaves under that has a missing or wrong cleanup; it is not a Strict Mode problem to turn off.

## Dependencies follow the code

The list is not a choice. It is every reactive value the effect reads: props, state, and anything declared in the component body. When the lint names a missing one, the fix is in the code:

- A function or object built in the component and used only by the effect moves inside the effect, or out of the component when it reads nothing reactive.
- State read only to compute the next state becomes an updater: `setMessages(current => [...current, message])`.
- An object dependency becomes the primitive actually read: `user.id`, not `user`.
- A value the effect must read without re-running when it changes, such as the current theme for a "connected" toast, goes through an effect event:

```tsx
const onConnected = useEffectEvent(() => showToast("Connected", theme));
useEffect(() => {
  const connection = createConnection(roomId);
  connection.on("connected", onConnected);
  connection.connect();
  return () => connection.disconnect();
}, [roomId]);
```

An effect event is called only from inside effects, never listed as a dependency and never passed to another component or hook. It is not a way to silence the lint: if the value should re-run the effect, it is a dependency.

Two unrelated jobs in one effect re-run each other. Split them, each with its own list.

## Checking it

- eslint-plugin-react-hooks with `exhaustive-deps` as an error, and `set-state-in-effect` for state set synchronously in an effect body.
- A search of the source for `react-hooks/exhaustive-deps` in a disable directive finds nothing. Each one found is a stale value waiting to happen.
- A test rendered in `<StrictMode>` that presses a control once and counts the requests, and one that changes the input twice before the first response resolves.
