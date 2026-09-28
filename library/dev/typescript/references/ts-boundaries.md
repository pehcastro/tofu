---
id: ts-boundaries
domain: dev
document: Effective TypeScript, 2nd edition, by Dan Vanderkam, items 46, 72 and 74; pstack's typescript-best-practices skill and its patterns reference; the Standard Schema interface published at standardschema.dev
found: the book's published code samples, kept in a local clone; pstack in the cursor-plugins collection
---

# Where types meet the running program

Types are erased before the code runs. Anything that arrives from outside, `JSON.parse`, `fetch`, a file, `process.env`, argv, a message from a worker or a model, has no type at all until code checks it. Give it `unknown`, parse it once where it enters, and trust the result everywhere after.

## One schema owns the shape

Use the schema library the project already has, and derive the type from the schema so there is one definition that cannot drift from a second:

```ts
import { z } from "zod";

const User = z.object({
  id: z.string(),
  role: z.enum(["admin", "member"]),
});
type User = z.infer<typeof User>;

const result = User.safeParse(JSON.parse(body));
if (!result.success) return fail(result.error);
use(result.data);
```

`parse` throws and suits a boundary where bad input is a bug. `safeParse` returns a result and suits one where bad input is expected, such as a request body. Valibot and ArkType have the same two paths under other names. Do not add a schema library to a project for one check; a short hand-written parse that checks every field is fine there.

## Standard Schema

Zod, Valibot and ArkType all implement Standard Schema: an object carrying a `~standard` property with a `validate` function and the inferred input and output types. Code that accepts any schema, such as a form helper or a config loader, takes a `StandardSchemaV1` rather than a type from one library, so the caller chooses the library.

## Syntax that disappears

Write TypeScript that becomes JavaScript by removing the types. Node runs `.ts` files by stripping types, and bundlers that transpile one file at a time do the same; neither can run syntax that generates code.

```ts
const Role = { Admin: "admin", Member: "member" } as const;
type Role = (typeof Role)[keyof typeof Role];
```

That replaces an `enum`. A class declares its fields and assigns them in the constructor rather than using `constructor(private name: string)`. A `namespace` that holds values becomes a module.

## Type-only imports

```ts
import type { User } from "./user";
import { parseUser, type Session } from "./session";
```

Under `verbatimModuleSyntax` an import that is only a type must say so, and every other import stays in the output exactly as written. That makes side effects predictable: an import you wrote runs, and one marked `type` never does. Re-export a type with `export type`.
