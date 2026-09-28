---
id: ts-strict-config
domain: dev
document: Effective TypeScript, 2nd edition, by Dan Vanderkam, items 2 and 48 on strict mode and unsound lookups; the TypeScript 7 changes as checked against the TypeScript team's announcements
found: the book's published code samples, a clone of the effective-typescript repository kept locally and not committed here
---

# What the compiler flags catch

Read the project's tsconfig.json, and every file it `extends` or `references`, before you judge an error. **Obey the flags the project sets, and never turn a new one on.** A new flag turns a working tree red in files nobody asked you to touch. This page is here so you can read an error correctly, not so you can change the config.

## strict

`strict` is a family. It turns on, among others:

- `noImplicitAny`: a parameter or variable whose type cannot be inferred is an error rather than `any`.
- `strictNullChecks`: `null` and `undefined` are their own types, so `user.name` on a `User | undefined` is an error until you narrow.
- `useUnknownInCatchVariables`: `catch (error)` gives `unknown`, so read `error.message` only after `error instanceof Error`.
- `strictFunctionTypes`, `strictPropertyInitialization`, `strictBindCallApply`, `noImplicitThis`, `alwaysStrict`.

## Beyond strict, and what each one catches

These are off under `strict` alone. A project that sets them has decided something, and code written without them in mind fails there.

- `noUncheckedIndexedAccess`: `list[i]` and `record[key]` read as `T | undefined`. It closes the most common unsound lookup: an index past the end typed as a value.

  ```ts
  const first = names[0];
  first.toUpperCase();
  ```

  The second line is an error here. Narrow with `if (first === undefined) return;`, not with `!`.

- `exactOptionalPropertyTypes`: `{ name?: string }` means the key may be missing, not that it may hold `undefined`. Writing `{ name: undefined }` is an error; leave the key out instead.
- `noImplicitOverride`: a method that replaces a parent's must say `override`, so renaming the parent's method breaks the child loudly.
- `noPropertyAccessFromIndexSignature`: a key that only an index signature allows must be read as `obj["key"]`, which makes the unchecked read visible.
- `noFallthroughCasesInSwitch`: a non-empty `case` must end in `break`, `return` or `throw`.
- `verbatimModuleSyntax`: an import used only as a type must be written `import type`, and nothing else is elided. See `ts-boundaries`.
- `erasableSyntaxOnly`: `enum`, value `namespace` and constructor parameter properties are errors, because a tool that only strips types cannot run them.
- `isolatedModules`: each file must compile alone, so re-exporting a type needs `export type`.

## TypeScript 7

- 7.0 is the Go compiler.
- It removes baseUrl, moduleResolution node, target es5, and outFile.
- `types` defaults to `[]`.
- typescript-eslint still needs TS 6 through `@typescript/typescript6`.

What this means in practice: a project on 7 that relied on a global type package, such as `@types/node`, now lists it in `types` itself, and an error about a missing global is fixed there rather than with a declaration file. A project still on `baseUrl` path aliases is on 6 or earlier; do not migrate it unasked.
