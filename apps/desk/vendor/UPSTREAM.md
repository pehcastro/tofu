# gpui-ce, vendored

- Source: https://github.com/gpui-ce/gpui-ce
- Revision: `175ef66578817bf96b2e26b0cd568dd4f4793529`, committed Fri Oct 2 00:49:37 2026 +0000
- Patches: DESK-609 liquid glass. `Filter::Refract` in gpui (`style.rs`, `scene.rs`, `window.rs`), the `refract_composite` pass in gpui_render (`shaders/filters.rs`, `shaders/interface.rs`, `blur.rs`) and gpui_windows (`directx_renderer.rs`); a frosted lens draws the blurred capture, then the sharp lens copy faded from the rim into the frost. DirectX only; other renderers draw the blur alone. The glass backdrop patch from `refs/gpui-glass-demo` is not applied.
- Patches: DESK-633 window control areas. `PaintIndex` carries `window_control_hitboxes_index` and `reuse_paint` replays `window_control_hitboxes` (`window.rs`), so a cached view keeps its drag, minimize, maximize and close areas.
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
