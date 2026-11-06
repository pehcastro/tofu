# gpui-ce, vendored

- Source: https://github.com/gpui-ce/gpui-ce
- Revision: `175ef66578817bf96b2e26b0cd568dd4f4793529`, committed Fri Oct 2 00:49:37 2026 +0000
- Patches: the DESK-609 liquid glass patch was taken out again by DESK-682; `style.rs`, `scene.rs`, gpui_render and `directx_renderer.rs` match upstream, and the desk is frost only. The glass backdrop patch from `refs/gpui-glass-demo` is not applied.
- Patches: DESK-633 window control areas. `PaintIndex` carries `window_control_hitboxes_index` and `reuse_paint` replays `window_control_hitboxes` (`window.rs`), so a cached view keeps its drag, minimize, maximize and close areas.
- Patches: DESK-672 nested cached views. A cached view replays only ranges recorded the previous frame (`Window::frames_drawn` in `window.rs`, the stored frame checked before reuse in `view.rs`), so a cached child inside a replayed parent repaints instead of pointing at shifted content.
- Patches: DESK-683 shell accent reset. On Windows a live change away from a transparent appearance first sets accent state 0 (disabled) and then the new state, because Windows 10 can ignore a direct accent change; each `SetWindowCompositionAttribute` call is printed to stderr.
- Patches: DESK-685 accent switch, reset only. `set_background_appearance` (`gpui_windows/src/window.rs`) always sets accent state 0 and then the new state on a live change away from a transparent appearance; the probe variable and the direct and twice strategies were measured and removed, since both left the window clear above 50%.
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
