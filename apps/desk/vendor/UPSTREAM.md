# gpui-ce, vendored

- Source: https://github.com/gpui-ce/gpui-ce
- Revision: `175ef66578817bf96b2e26b0cd568dd4f4793529`, committed Fri Oct 2 00:49:37 2026 +0000
- Patches: none. The glass backdrop patch from `refs/gpui-glass-demo` is not applied; the desk is frost only.
- License: Apache-2.0, kept in `gpui-ce/LICENSE.md`.

## How it was obtained

On 2026-10-06, by a shallow fetch of that one revision, with its `.git` removed afterwards:

```
git init
git remote add origin https://github.com/gpui-ce/gpui-ce
git fetch --depth 1 origin 175ef66578817bf96b2e26b0cd568dd4f4793529
git checkout FETCH_HEAD
```

As a cleanliness check, `git apply --check` of `gpui-backdrop.patch` from the glass demo succeeds against this copy, which it could not if that patch were already applied.

## Syncing

`scripts/sync-gpui.ps1 -Rev <sha>` replaces `vendor/gpui-ce` with a clean fetch of that revision. Bump the revision here in the same commit. A sync is its own ticket.

The workspace pins its dependency versions with the vendor's own `Cargo.lock`, copied into `apps/desk/Cargo.lock` at the time of vendoring. A sync copies it again.
