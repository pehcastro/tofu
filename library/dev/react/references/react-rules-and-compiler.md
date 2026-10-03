---
id: react-rules-and-compiler
domain: dev
document: react.dev, the Rules of React, Components and Hooks must be pure, React calls Components and Hooks, Rules of Hooks, and the React Compiler introduction; the eslint-plugin-react-hooks rule list and its lint pages; Vercel composition-patterns
found: local copies of the react.dev pages and a local clone of the Vercel skills, kept outside this repository
---

# The rules of React, and the compiler that relies on them

React may render a component any number of times, in any order, and throw a render away. That is only safe because of three rules. The React Compiler memoises code on the assumption that they hold, and skips a component it can see breaking one.

## Render is pure

The same props, state and context give the same JSX, and rendering changes nothing outside itself.

- No `Math.random()`, `Date.now()`, `new Date()`, `crypto.randomUUID()` or `performance.now()` in the body. Take the value once with `useState(() => crypto.randomUUID())`, or read it in an effect or a handler.
- No writes to a module variable, `window`, `document` or a cache object during render. Put a counter in state, a shared value in context, and a write to the page in an effect.
- No `ref.current` read or write during render. A ref holds what the screen does not show; the one exception is filling an empty ref once: `if (ref.current === null) ref.current = create()`.
- No state setter called unconditionally during render. Conditionally, against a stored previous prop, is allowed and rarely needed.

## Values are snapshots

Props, state, hook arguments, hook return values and anything already passed to JSX are not changed in place. Create the next value and pass it to the setter:

```tsx
setItems([...items, item]);
setItems(items.toSorted(byName));
setUser({ ...user, settings: { ...user.settings, theme: "dark" } });
```

`items.push(x); setItems(items)` hands React the same reference, so it skips the update. An object created during this render is local and may be mutated before it is returned.

## React calls components and hooks

- A component is used as JSX, never called as a function.
- A component is declared at module level. One declared inside another is a new type each render: it remounts, losing state, focus, scroll and running its effects again. Pass the values it needed as props.
- Hooks are called at the top level of a component or of a function whose name starts with `use`, before any early return. Never in a condition, a loop, a handler, a callback passed to another hook, or a `try` block.
- A hook is not passed around as a value, chosen at runtime, or built by a factory.

## Errors and loading

`try`/`catch` cannot catch an error thrown while a child renders, and `use(promise)` suspends rather than throws. An error boundary catches the first, a `Suspense` fallback shows during the second. Place one of each around every region that fails or loads on its own.

## The compiler

The React Compiler memoises components and hooks at build time, usually more precisely than hand-written `useMemo`, `useCallback` and `memo`. In a project that runs it:

- New code is written plain. `useMemo` and `useCallback` stay available for the case that needs control, such as a value an effect depends on that must not change identity.
- Existing memoisation stays unless a test covers removing it: removing it can change the compiled output.
- Each diagnostic the lint reports marks a component the compiler left uncompiled. Fixing the rule it names makes that component faster; wrapping it in `memo` does not.

In a project that does not run it, memoise after a profile shows the cost, never before.

## Component shape

- A component that grows a boolean prop for each variant, `isCompact`, `isEditing`, `hideFooter`, becomes explicit variants built from shared parts, or a compound component whose parts share state through context.
- `children` composes better than a `renderHeader` prop.
- In React 19, `ref` is an ordinary prop, so new code needs no `forwardRef`.

## The lints, in eslint-plugin-react-hooks

| Rule | Catches |
|---|---|
| `rules-of-hooks` | a hook in a condition, loop, handler or non-hook function |
| `exhaustive-deps` | a dependency list that does not match the code |
| `purity` | a known impure call during render |
| `globals` | a global assigned or mutated during render |
| `refs` | `ref.current` read or written during render |
| `immutability` | props, state or a hook value mutated |
| `set-state-in-render` | an unconditional set during render |
| `set-state-in-effect` | a synchronous set in an effect body |
| `static-components` | a component created inside another |
| `component-hook-factories` | a function that builds components or hooks |
| `preserve-manual-memoization` | memoisation the compiler cannot keep |
| `use-memo` | a `useMemo` with no return value |
| `error-boundaries` | `try`/`catch` around rendering or `use` |
| `unsupported-syntax` | syntax the compiler cannot compile |
| `incompatible-library` | a library that cannot be memoised safely |

The compiler rules report even where the compiler is not installed, so they are worth turning on first.
