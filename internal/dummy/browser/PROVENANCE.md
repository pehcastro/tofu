# browser

Read by `internal/tools/web/browser/loop`, `motion` and `driver`.

| file | origin | readers |
|---|---|---|
| hotel.json | a page snapshot of a local hotel search fixture served at 127.0.0.1:8000, recorded when the Jev chooser and step loop were ported from jev-ultrafast (1b09098a, 2026-09-28) | loop/choose_test.go, loop/loop_test.go, driver/tab_test.go |
| hotel_answer.json | a Jev answer in the TypeSafe response shape (id gen-hotel-1) to the browser step question over hotel.json, written with the test in the same commit | loop/choose_test.go, loop/loop_test.go, driver/tab_test.go |
| field-notes-close.json | a motion scenario written by hand for the relay screencast (a27cf227, 2026-09-29): click the first FAQ question on a local Field Notes page and watch the answer close | motion/motion_test.go, driver/capture_test.go |

Moved here unchanged from `browser/loop/testdata` and `browser/motion/testdata` by TOFU-2281. Nothing is fetched; nothing is regenerated.
