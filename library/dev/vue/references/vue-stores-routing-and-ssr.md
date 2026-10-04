---
id: vue-stores-routing-and-ssr
domain: dev
document: Pinia stores, Vue Router guards, Nuxt data fetching and server rendering
---

# Pinia, Vue Router and server rendering

## Pinia

A store holds state several features share. State one component owns stays in that component; state a subtree owns can be provided.

```ts
export const useCartStore = defineStore("cart", () => {
  const items = ref<CartItem[]>([]);
  const total = computed(() => items.value.reduce((sum, item) => sum + item.price, 0));
  function add(item: CartItem) {
    items.value.push(item);
  }
  return { items, total, add };
});
```

- A setup store returns every `ref` it declares. State left out of the return is not serialised for server rendering and is invisible to DevTools and plugins.
- Components change state through actions, so every change has one place to read.
- `const { items, total } = storeToRefs(cart)` keeps reactivity; `const { add } = cart` is fine for actions. `const { items } = cart` copies once and goes stale.
- `useCartStore()` is called inside setup, a composable, an action or a router guard. At module level it runs before `app.use(pinia)` and throws `"getActivePinia()" was called but there was no active Pinia`.

In a component test, `createTestingPinia` from `@pinia/testing` installs a store with `initialState` and stubbed actions, so the test asserts what the component renders and which action it called.

## Vue Router

A guard returns its answer:

```ts
router.beforeEach(async (to) => {
  if (to.meta.requiresAuth && to.name !== "sign-in" && !(await session.check())) {
    return { name: "sign-in", query: { next: to.fullPath } };
  }
});
```

- Return nothing or `true` to continue, `false` to cancel, a location to redirect. The third `next` argument is legacy; with it, a forgotten call hangs the navigation (`The "next" callback was never called`) and a second call misroutes (`was called more than once`).
- A redirect excludes its own target, or the guard loops.
- `beforeRouteEnter` runs before the component exists and has no `this`.
- A component reused for a new param does not remount. Data keyed by a param is loaded by a watcher on it, or in `onBeforeRouteUpdate`.
- A guard in the browser decides what to show. The server checks every request on its own.

## Server rendering and Nuxt

On the server one module instance serves every request.

| Need | Write | Not |
|---|---|---|
| state shared across components for one visitor | `useState("key", () => initial)` in Nuxt, state created per request in plain Vue SSR | a `ref` at module level, which every visitor shares |
| a page's first data | `await useFetch(url)` or `await useAsyncData("key", fn)` | `await $fetch(url)` in setup, which runs on the server and again on the client |
| a request after a click | `$fetch` in the handler | `useFetch` in a handler |
| a value only the browser has | `onMounted`, or `useCookie` for a cookie | `localStorage` or `window` during setup |

Nuxt composables run only in setup, a plugin, a route middleware or a Nuxt hook. Outside them Nuxt throws `A composable that requires access to the Nuxt instance was called outside of a plugin, Nuxt hook, Nuxt middleware, or Vue setup function`.

The first client render must match the server's HTML. Locale formatting, the time, a random value or a browser API read during setup differ between the two, and Vue warns `Hydration completed but contains mismatches.` after a warning that names the node.

## Checking it

- The browser console on the dev server shows no error or warning during the first load, which catches a hydration mismatch and an early store.
- A test opens a guarded route signed out and asserts it reaches the sign-in page in one redirect.
- A test signs two people in from two browser contexts against one server and asserts neither sees the other's data.
