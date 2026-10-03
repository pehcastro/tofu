---
id: vue-components-and-reactivity
domain: dev
document: vuejs-ai vue-best-practices, its workflow and its reactivity, sfc, component-data-flow and composables references; vuejs-ai vue-debug-guides on computed and watchers; antfu skills vue, its preferences, script-setup-macros and core-new-apis; the rules, categories and configs as eslint-plugin-vue 10.11 ships them; the development warnings as @vue/runtime-core 3.5 ships them; the vue-tsc 3.3 package
found: local clones of vuejs-ai/skills and antfu/skills, and the eslint-plugin-vue, @vue/runtime-core and vue-tsc packages unpacked from the registry, all outside this repository
---

# Vue components and reactivity

The default is Vue 3 with the Composition API in `<script setup lang="ts">`. A project written in the Options API keeps it; a mixed one writes new code in `<script setup>`.

## A component

```vue
<script setup lang="ts">
import { computed } from "vue";

const props = withDefaults(defineProps<{ items: Item[]; filter?: string }>(), { filter: "" });
const emit = defineEmits<{ select: [id: string] }>();
const query = defineModel<string>("query");

const visible = computed(() => props.items.filter((item) => item.name.includes(props.filter)));
</script>

<template>
  <input v-model="query" />
  <ul>
    <li v-for="item in visible" :key="item.id" @click="emit('select', item.id)">{{ item.name }}</li>
  </ul>
</template>
```

- `defineProps`, `defineEmits`, `defineModel`, `defineExpose`, `defineOptions` and `defineSlots` are compiler macros. They are never imported.
- Props go down, events come up. A child never writes to a prop; it emits, or uses `defineModel` where the parent binds `v-model`.
- Events do not bubble. A parent that needs a grandchild's event gets it re-emitted by the child in between.
- `provide` and `inject` with a typed `InjectionKey` carry a value several levels down. The provider owns changes to it.
- A template ref, from `useTemplateRef`, is for imperative calls, focus, scroll, a media element. Data travels through props and events.

## Choosing the reactive primitive

| Value | Use |
|---|---|
| replaced whole, or not to be proxied: an API response, a class instance, a library handle | `shallowRef` |
| an object or list edited in place | `ref` or `reactive` |
| computed from other state | `computed` |
| a side effect when something changes | `watch` with a getter, or `watchEffect` |

## Where reactivity is lost

Each of these keeps working until the value changes, then shows the old value with no error.

- Destructuring a `reactive()` object or `props`: `const { count } = state`. Read `state.count`, or `toRefs(state)`.
- Passing a plain value into a composable: `useUser(props.id)` sees the first id forever. Pass `() => props.id` and read it with `toValue()` inside a `computed` or a watcher.
- Using a ref as a value in script: `count + 1`. Read `count.value`.
- `watch(state.count, ...)`: it receives a number. Write `watch(() => state.count, ...)`.
- A watcher, lifecycle hook or `defineExpose` registered after an `await` in setup: it never attaches.

## Computed or watch

A `computed` is the answer whenever the result is a value. A `watch` that assigns a ref from other refs is a `computed` written the long way, and it renders once with the stale value first. A getter does no work besides computing: no request, no `emit`, no mutation, no `await`.

A `watch` that starts a request cancels the previous one:

```ts
watch(() => route.params.id, async (id, _previous, onCleanup) => {
  const controller = new AbortController();
  onCleanup(() => controller.abort());
  user.value = await fetchUser(id, controller.signal);
}, { immediate: true });
```

A route component stays mounted when only its params change, so `onMounted` runs once for `/users/1` and not again for `/users/2`. Data keyed by a param comes from a watcher like the one above.

## Templates

- Every `v-for` has a `:key` from the data. The index only for a list that never changes order.
- `v-if` and `v-for` never share an element: loop over a filtered `computed`.
- `{{ }}` escapes. `v-html` does not, and gets only sanitised markup.

## The lints, in eslint-plugin-vue

`flat/essential` is errors that are nearly always bugs, `flat/strongly-recommended` adds readability, `flat/recommended` adds conventions. The rules this library names:

| Rule | Config | Catches |
|---|---|---|
| `no-mutating-props` | essential | a write to a prop |
| `no-side-effects-in-computed-properties` | essential | a mutation inside a `computed` |
| `no-async-in-computed-properties` | essential | `await` or a timer inside a `computed` |
| `return-in-computed-property` | essential | a getter with a path that returns nothing |
| `no-use-computed-property-like-method` | essential | a `computed` called as a function |
| `no-watch-after-await`, `no-lifecycle-after-await`, `no-expose-after-await` | essential | registration after an `await` |
| `no-ref-as-operand` | essential | a ref used where its value was meant |
| `require-v-for-key`, `valid-v-for`, `no-use-v-if-with-v-for`, `no-v-for-template-key-on-child` | essential | list rendering |
| `no-export-in-script-setup`, `valid-define-props`, `valid-define-emits` | essential | macros used wrongly |
| `require-explicit-emits` | strongly recommended | an emitted event nobody declared |
| `no-v-html` | recommended | every `v-html` |
| `component-api-style`, `define-props-declaration`, `define-emits-declaration`, `no-import-compiler-macros`, `prefer-define-options`, `no-setup-props-reactivity-loss`, `no-ref-object-reactivity-loss`, `require-typed-ref`, `prefer-use-template-ref` | none, turned on by hand | style and reactivity loss |

## Checking it

- `vue-tsc --noEmit`: `tsc` does not read `.vue` files, so it misses every error in a component.
- eslint with `flat/recommended` and the hand-picked rules above.
- In development Vue warns `Component emitted event ... but it is neither declared in the emits option` and `Duplicate keys found during update`. A test that fails on a console warning catches both.
