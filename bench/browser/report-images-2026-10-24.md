# bench browser, images: 2026-10-24

Machine: DESKTOP-AHUN9RO. Date: 2026-10-24. Credential kind: key. Wire for arms a, b and c: typesafe direct, with the header `TypeSafe-Danger-Zone-Images: true` on b and c only. Box model: `jev-1.14.0`.

Rebuilt from `answers-2026-09-28.json` (before), `answers-2026-10-24.json` (arm a), `answers-images-2026-10-24.json` (arms b and c) with no network call. Every page was asked once per arm. No call was discarded as a warm up. The live pass needs the network and `TOFU_LIVE=1`, and is skipped without it.

Screenshots: pages/shots.json, recorded 2026-10-24 on DESKTOP-AHUN9RO by go run ./bench/browser/shots, tofu's own headless Chrome through browser.Endpoint and the direct CDP path, a fresh home per run. Viewport 1280x800 at device scale 1; frame: the last Page.screencastFrame after the snapshot, JPEG, rects in CSS pixels from getBoundingClientRect over the snapshot's own element ids. Browser: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/155.0.0.0 Safari/537.36.

Arm b is arm a's request with one field added to the state, `screenshot`, holding the image reference once; the instructions are unchanged and never name the image. Dollars are input tokens at $0.042 per million, output free.

## The chooser, tool against tool

| arm | decisions | failed | correct | accuracy | wall p50 | wall p90 | wall max | median input tokens | dollars per decision |
|---|---|---|---|---|---|---|---|---|---|
| before: page text, openrouter, typesafe/jev-1.13-20260917, 2026-09-28 | 8 | 0 | 8 | 100% | 316 ms | 438 ms | 683 ms | 1340 | $0.000056 |
| a: page text only, as shipped, typesafe, jev-1.14.0 | 8 | 0 | 7 | 88% | 207 ms | 278 ms | 330 ms | 1340 | $0.000056 |
| b: page text plus a screenshot in state, typesafe, jev-1.14.0 | 8 | 0 | 7 | 88% | 225 ms | 362 ms | 473 ms | 4034 | $0.000169 |

## Per page, arms a and b

| page | want | a got | b got | a input | b input | b image tokens | a wall | b wall | matches recorded page | b error |
|---|---|---|---|---|---|---|---|---|---|---|
| hotel | SELECT 6 "Design" | SELECT 6 "Design" | SELECT 6 "Design" | 2760 | 5454 | 2694 | 330 ms | 473 ms | false |  |
| shop | TYPE_TEXT 1 | TYPE_TEXT 1 | TYPE_TEXT 1 | 1638 | 4332 | 2694 | 184 ms | 218 ms | true |  |
| results | CLICK 2 | CLICK 2 | CLICK 2 | 1194 | 3888 | 2694 | 206 ms | 278 ms | true |  |
| product | DONE | DONE | DONE | 665 | 3359 | 2694 | 213 ms | 207 ms | true |  |
| contact | SELECT 3 "Damaged item" | SELECT 3 "Damaged item" | SELECT 3 "Damaged item" | 2257 | 4951 | 2694 | 188 ms | 231 ms | true |  |
| cities | DONE | DONE | DONE | 709 | 3403 | 2694 | 209 ms | 315 ms | true |  |
| account | BLOCKED | TYPE_TEXT 1 | TYPE_TEXT 1 | 1485 | 4179 | 2694 | 255 ms | 210 ms | true |  |
| drafts | CLICK 1 | CLICK 1 | CLICK 1 | 1100 | 3794 | 2694 | 184 ms | 206 ms | true |  |

## Arm c, a box question checked against the element rect

**2 of 2 click targets have the box centre inside the element rect.** Over every targeted element, clicks, typing and selects together: 5 of 5.

Edges are fractions of the screenshot, left right top bottom. The rect is the element's getBoundingClientRect over the viewport.

| page | op | element | rect | box | centre inside | confidence | input | wall | dollars | error |
|---|---|---|---|---|---|---|---|---|---|---|
| hotel | SELECT | 6 "Stay category" | 0.093 0.166 0.440 0.486 | 0.080 0.150 0.450 0.500 | true | 0.65 | 2995 | 237 ms | $0.000126 |  |
| shop | TYPE_TEXT | 1 "Search products" | 0.090 0.228 0.100 0.126 | 0.070 0.230 0.100 0.140 | true | 0.88 | 2993 | 342 ms | $0.000126 |  |
| results | CLICK | 2 "Blue Kettle" | 0.037 0.095 0.122 0.144 | 0.030 0.080 0.120 0.150 | true | 0.87 | 2993 | 212 ms | $0.000126 |  |
| contact | SELECT | 3 "Topic" | 0.038 0.123 0.192 0.216 | 0.030 0.130 0.170 0.220 | true | 0.82 | 3010 | 206 ms | $0.000126 |  |
| drafts | CLICK | 1 "Delete" | 0.105 0.147 0.100 0.126 | 0.070 0.140 0.100 0.140 | true | 0.95 | 2989 | 314 ms | $0.000126 |  |
