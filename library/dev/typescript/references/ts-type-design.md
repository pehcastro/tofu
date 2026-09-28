---
id: ts-type-design
domain: dev
document: Effective TypeScript, 2nd edition, by Dan Vanderkam, items 29, 32 to 34, 37, 59 and 64; pstack's typescript-best-practices skill and its patterns reference
found: the book's published code samples, kept in a local clone; pstack in the cursor-plugins collection
---

# Types that only admit valid states

A type that allows a state the program never means to be in makes every reader ask whether that state can happen. Build the type from the states that exist, and the question disappears along with the checks that answered it.

## One tag, one variant per state

```ts
type Load<T> =
  | { kind: "loading" }
  | { kind: "ready"; data: T }
  | { kind: "failed"; error: Error };
```

Not `{ loading: boolean; data?: T; error?: Error }`, which admits loading with data, or neither data nor error. Pick one tag name, `kind` or `type`, and keep it across the codebase. The compiler narrows on the tag in a `switch` or an `if`, so each branch sees only the fields its state has.

Two optional fields that are always set together are one variant in disguise. So is a boolean beside a nullable value: `{ done: boolean; doneAt?: Date }` is `doneAt: Date | null` with the boolean derived from it, or two variants.

## Handle every variant

```ts
function label(load: Load<string>): string {
  switch (load.kind) {
    case "loading":
      return "...";
    case "ready":
      return load.data;
    case "failed":
      return load.error.message;
    default: {
      const unhandled: never = load;
      throw new Error(`unhandled ${String(unhandled)}`);
    }
  }
}
```

Adding a fourth state is now a compile error in `label` and in every other switch like it, which is the point.

## Build the shape rather than check it

A list that must not be empty is a head and a rest, `[T, ...T[]]`, not a `T[]` with a length check every caller repeats. A range is a start and a duration, not two dates someone must keep in order. Strengthen a type only where the loose one forces a `!`, a cast or a throw that says it cannot happen: `sum(xs: number[])` is fine, because the empty sum is 0.

## Brands for values that share a primitive

```ts
type UserId = string & { readonly __brand: "UserId" };

function userId(raw: string): UserId {
  if (!/^u_[a-z0-9]+$/.test(raw)) throw new Error(`not a user id: ${raw}`);
  return raw as UserId;
}
```

The cast is earned: it sits right after the check, in the one function that makes the value. Downstream, a `UserId` cannot be passed where an `OrderId` is wanted, though both are strings. Brand when two arguments share a type and mean different things, not by reflex.

## Nulls at the edge

- Keep `null` out of a type alias. `type User = { ... } | null` makes every use of `User` nullable, including the places that already checked.
- Do not let one field's nullness depend on another's. Two values that are null together or present together belong in one object that is null or complete:

  ```ts
  type Bounds = { min: number; max: number };
  declare function bounds(xs: number[]): Bounds | null;
  ```

  not a `[number | null, number | null]` whose halves the reader must keep in step.
- Separate the shape you accept from the shape you use. Input may have optional fields; normalise it once at the boundary into a type where every field is required, and pass that inward.
