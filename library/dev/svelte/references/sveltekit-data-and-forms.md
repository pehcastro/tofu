---
id: sveltekit-data-and-forms
domain: dev
document: the Svelte llms-small docs, SvelteKit routing, loading data, headers and cookies, errors and redirects, form actions, state management, hooks and server-only modules; the messages as @sveltejs/kit 2.49 ships them in its export validator, its action runner, its page renderer, its serialisation check and its vite plugin
found: a local copy of the Svelte docs and the @sveltejs/kit package in a local Svelte 5 project, both outside this repository
---

# SvelteKit data and forms

A route's files split by where they run. Choosing the wrong file is the usual way a secret reaches the browser or a page shows stale data.

| File | Runs | Exports |
|---|---|---|
| `+page.svelte`, `+layout.svelte` | server, then browser | the component |
| `+page.js`, `+layout.js` | server, then browser | `load`, `prerender`, `ssr`, `csr`, `trailingSlash`, `config`, and `entries` on a page |
| `+page.server.js` | server only | the above, `actions` |
| `+layout.server.js` | server only | `load` and the page options |
| `+server.js` | server only | `GET`, `POST` and the other methods, `fallback` |

Any other export fails with `Invalid export '<name>' in <file>`, and the message names the file where it would be valid.

## Loading

A `load` in a `.server.js` file reads the database, `cookies`, `locals` and private env. A `load` in `+page.js` is for public data both sides may fetch. Both use the `fetch` they are given, which carries cookies on the server and reuses the server's response during hydration.

```ts
import type { PageServerLoad } from "./$types";
import { error } from "@sveltejs/kit";

export const load: PageServerLoad = async ({ params, locals }) => {
  const post = await locals.db.post(params.slug);
  if (!post) error(404, "not found");
  return { post };
};
```

The page reads it as `let { data }: PageProps = $props()`. What a server `load` returns must be serialisable; otherwise SvelteKit reports that the data returned from `load` while rendering the route is not serializable, and names the path to the bad value. Return a promise that is not awaited to stream a slow part, and render it with `{#await}`.

A `load` changes nothing outside itself: no module variable, no store. On the server one module instance serves every visitor, and anything written to it leaks between them.

## Auth

A layout's server `load` is not re-run on every client navigation, so a check there does not guard a child page reached later. Check in the `handle` hook in `hooks.server.js`, which runs on every request, or in each protected page's server `load`. Set cookies with `cookies.set`; a `set-cookie` through `setHeaders` fails with an error that names `event.cookies.set`.

Private env, `$env/static/private` and `$env/dynamic/private`, and anything in `src/lib/server/` or named `*.server.js`, can only be imported by server code. A chain of imports that brings one into the browser fails the build with `Cannot import <module> into code that runs in the browser, as this could leak sensitive information.`

## Form actions

```ts
import type { Actions } from "./$types";
import { fail, redirect } from "@sveltejs/kit";

export const actions: Actions = {
  create: async ({ request, locals }) => {
    const form = await request.formData();
    const title = String(form.get("title") ?? "");
    if (!title) return fail(400, { title, missing: true });
    const id = await locals.db.create(title);
    redirect(303, `/posts/${id}`);
  },
};
```

```svelte
<script>
  import { enhance } from "$app/forms";
  let { form } = $props();
</script>

<form method="POST" action="?/create" use:enhance>
  <input name="title" value={form?.title ?? ""} aria-invalid={form?.missing} />
  <button>Create</button>
</form>
```

- The form works with JavaScript off. `use:enhance` adds the rest: no full reload, focus restored, data invalidated.
- `fail` re-renders the page with `form` set. Throwing shows the error page and loses what was typed.
- Named actions and a `default` cannot coexist: `When using named actions, the default action cannot be used`.
- A page with actions cannot be prerendered: `Cannot prerender pages with actions`.
- A custom submit handler reads the response with `deserialize` and applies it with `applyAction`. `JSON.parse` gets the encoding wrong.

## Checking it

- `svelte-check` types `load`, `actions`, `data` and `form` from the generated `./$types`.
- A Playwright test with `javaScriptEnabled: false` submits each form.
- A test signs two people in from two browser contexts and asserts neither page shows the other's data.
