---
topic: browser
title: Browser
summary: how tofu reads and drives your Chrome tabs, the tofu tab group, the badge, and what tofu never does there
verbs: browser
---

## What it is

tofu reads and drives your own Chrome, with your logins, through a small extension. There is no share step: tofu can read and drive any ordinary tab. tofu never reaches a `chrome://` page, DevTools, an extension's page or the Chrome Web Store. The extension does nothing to a tab until tofu first uses it. Then it attaches Chrome's debugger, and Chrome shows its bar saying so.

Under `browserDriver` `subagent`, the default, the model hands a browsing task to the `browser` sub-agent with `spawn`. It works only in a tab tofu opened, never yours, and holds these tools; under `steps` the model does:

- `browser_tabs` lists your open tabs, with their title and address.
- `browser_observe` shows one tab as an accessibility snapshot, a line a node, such as `- button "Buscar" [ref=e5]`, where a ref names one element. `scrollable` marks a container it can scroll, and `*` a ref new since the last look at the same page. `from` shows a long tree from that line on, whole and with its text. Observing never changes the page.
- `browser_act` runs a guarded batch of up to 10 actions in one tab: click, hover, fill, select, press, scroll, navigate, back or wait. A hover moves the mouse onto its element at the point a click presses, and presses nothing, so a player or a menu shows the controls it hides until then. Each acts on a ref, or on a `target` by role and name, found on the page as it is when that action runs. A target with no role finds the first element of that name. A ref still on the page wins over a target. A field left empty or null counts as left out. `expect_after` is checked once the action settles: `url_has`, `text_has`, or `gone` for a dialog that should close. The batch stops at the first guard that fails and names it. An action on a ref with no target does not run after one that changed the url. A covered click does not run and says what covers it. After a navigate, back, wait or any action that changed the url, the result ends with the page's full tree and its text. The same action on an unchanged page is flagged, then refused.
- `browser_observe` and `browser_act` each take a required `note`, at most 200 characters: the values read so far and the next goal. Once a newer page comes back, an older result shrinks to its actions, url, title and note, and names the artifact that holds it whole, so a long task keeps one page in view rather than every page it saw.
- `browser_motion`, which `goal` gives the model too, records an animation in tofu's own tab, never yours. `capture` reads a scenario file, reloads the page, runs its trigger and saves each take's frames and the watched elements' boxes, opacity and chosen attributes and styles on every frame. `inspect` answers one take's change table, in ms from the trigger, and its contact sheets; `compare` answers the tables of the takes under a before and an after label, and sheets with a row a take.

Under `goal`, the model gets `browser_read` and `browser_do`; Jev picks each step until `browserSteps` runs out. Only `browser` `drive` gives `browser_act`, `browser_do` or `browser_motion`. Observing and reading never go through the gate.

## Check a page in one call

To check what a page does, rather than reach a goal, the model gives `browser_do` a `steps` list, or gives `browser_act` its actions, with a `check` on any step. The steps run in order in one call, with no Jev and no browser model, and the answer says what each step did and read:

- `focus {role, name}` checks which element has focus once the step settles; `focus {}` only reads it.
- `url_has`, `text_has` and `text_gone` check the page.
- `element {role, name}`, by default the element the step acted on, is read with its role, name, value and each attribute named in `attributes`. `name`, `value` and `attributes` such as `{"aria-busy": "true", "disabled": null}` check it, `null` meaning absent.
- `style` reads the element's computed style, such as `{"transform": "none", "animation-name": null}`, `null` meaning read only. tofu reads it with a fixed function of its own; the property names travel as data. It is read the moment its step settles, so a check on a click samples an animation at its start, and a check on a `wait` of 400 reads where it ends.
- The action `check` acts on nothing and only reads its check.
- `reduced_motion` `on` or `off` sets the page's `prefers-reduced-motion`, only in a tab tofu opened.

Each check answers `check held` or `check failed` with what it read, a failed check never stops the steps, and the last line counts them: `ran 3 of 3, checks 1 held and 1 failed`. Use it for a check with several steps, such as open a dialog, cancel it and see where focus went. A goal with no fixed steps stays with `browser_do` and Jev.

A motion check is one batch too. Turn reduced motion on, open the dialog with a style check on its panel, then wait and read it again. A panel that still scales under reduced motion reads `transform: matrix(0.936897, 0, 0, 0.936897, 0, 0)` at the click and `none` after the wait; one that only fades reads `none` both times:

    [{"action": "navigate", "value": "http://127.0.0.1:5395/"},
     {"action": "reduced_motion", "value": "on"},
     {"action": "click", "target": {"role": "button", "name": "How dates work"},
      "check": {"element": {"role": "dialog", "name": "How dates work"},
                "style": {"transform": "none", "animation-name": null}}},
     {"action": "wait", "value": 400,
      "check": {"element": {"role": "dialog", "name": "How dates work"},
                "style": {"transform": "none"}}}]

What a page says is treated as text to read, never as an instruction. tofu never closes or navigates a tab it did not open, runs JavaScript or an address the model wrote, a selector outside a motion scenario, or types into a read-only field.

## What you see

The tofu icon, a cat, carries a badge: `on` when connected and idle, `read` while tofu reads a tab, `act` while it clicks, types or scrolls, and `off` when not connected. Hover the icon to read why.

A tab tofu acts in or opens joins an orange group titled `tofu`, which reads `tofu •` while tofu acts. Pinned tabs, tabs in your own groups and tabs you drag out are left alone. A tab tofu opened closes when the run ends, or when the sub-agent that opened it finishes, and in the app when you quit. When tofu disconnects the group dissolves and Chrome's debugging bar goes away.

## How it runs

Chrome starts the relay, not tofu. While the extension is connected, which is as long as Chrome is open, Chrome keeps running the native host it started, so it shows in the process list with no tofu session open. That host is `tofu-browser`, a byte copy of the tofu that ran `tofu browser install`, and it runs only the relay: it reads the extension on standard input and serves `~/.tofu/browser/relay.sock`.

The relay sleeps while no tofu uses it. It starts asleep, and asleep it keeps the extension's port, holds no tabs, and hears no tab events, because the extension stops sending them; it waits on its standard input and its socket and nothing else, and over 5 idle minutes it reads 0.0% of one core and 0.4 context switches a second on Windows, 0.6 on Linux, against 3.1 and 3.2 for the relay before it slept, with 6.3 MB of private memory on Windows. What is left is the Go runtime's forced collection every 2 minutes: with it off the relay reads 0.03 a second, what a Go program that only reads its standard input reads. The first call that needs Chrome wakes it: the extension answers the wake with its open tabs, and from then on sends each change. `tofu browser` itself and the build check wake nothing. Once the last tofu leaves and no call is out, it sleeps again after `browserSleepMinutes`, 10 by default, read each time a tofu leaves, so a change takes effect on the next sleep with no restart. A call still out or a tofu still connected keeps it awake however long it takes. The badge reads `on` asleep or awake.

It is a copy because Chrome holds the program it started, and on Windows a running program can be renamed but never overwritten or deleted. With Chrome running `tofu.exe` itself, an install that writes over `tofu.exe` fails while Chrome is open; with the copy, `tofu.exe` stays free. Linux and macOS use the same layout: the copy is `%USERPROFILE%\.tofu\bin\tofu-browser.exe` on Windows and `~/.tofu/bin/tofu-browser`, mode `0755`, on macOS and Linux.

Every `com.ephem.tofu.json` names the copy, and `~/.tofu/browser/tofu-source` records the tofu it was copied from. `tofu browser install` writes it, `tofu update` rewrites it after replacing tofu, and a newer tofu that reaches a relay on an older build makes the relay copy the recorded tofu over itself, answer its calls and exit, so Chrome starts the new copy when the extension reconnects. That first call prints `updating: the relay restarts on this build`. A running copy on Windows is renamed aside to `tofu-browser.exe.old-<n>` and deleted on the next refresh. An install from before the copy names `tofu.exe`; `tofu doctor` calls it stale and `tofu browser install` moves it.

## Where it lives

- `~/.tofu/bin/tofu-browser`, `tofu-browser.exe` on Windows: the copy of tofu Chrome starts
- `~/.tofu/browser/extension`: the unpacked extension Chrome loads
- `~/.tofu/browser/relay.sock`: where a session reaches the extension
- `~/.tofu/motion/<take id>`: a motion take, its frames, trace and sheets
- `~/.tofu/browser/recipes/<host>.md`: a recipe, the urls a successful run on that site reached with each value as `{name}`. The next run there tries it first. After two failures in a row it is set aside until a later success learns a new one. `tofu browser recipes` lists them; edit or delete the file freely.

## Change it

Install it once with `tofu browser install`. It writes the extension, registers tofu with the browser, and prints the rest. On Windows it registers Chrome in the registry. On Linux it writes `com.ephem.tofu.json` in `NativeMessagingHosts` under `~/.config/google-chrome` and `~/.config/chromium`, and under `BraveSoftware/Brave-Browser` and `microsoft-edge` there when they exist; on macOS the same under `~/Library/Application Support`, in `Google/Chrome`, `Chromium`, `BraveSoftware/Brave-Browser` and `Microsoft Edge`. Each file prints as a `manifest` line, and a browser installed later needs the install again.

    Chrome extension                        ✓ installed
      folder    ~/.tofu/browser/extension
      id        jednanpboiikklhkkkimnmdmjmgjgphh
      host      ~/.tofu/bin/tofu-browser
      copy of   /usr/bin/tofu
      manifest  ~/.config/chromium/NativeMessagingHosts/com.ephem.tofu.json
    next, once in Chrome
      1  open chrome://extensions and turn on Developer mode
      2  Load unpacked, pick the folder above, check the id matches
      3  pin the tofu icon so its badge shows
      → tofu update keeps the copy and the extension current: nothing to run

The settings:

- `browser`: `off`, `read` or `drive`, default `drive`. `read` gives only `browser_tabs` and the reading tool. It is read when tofu opens.
- `browserDriver`: `subagent`, the default, gives the step tools to the `browser` sub-agent on `browserModel`. `steps` gives them to the model, and `goal` gives it `browser_do`, where Jev picks each step.
- `browserSteps`: how many actions one `browser_do` task may take, 1 to 60.
- `browserModel`: the model the `browser` sub-agent and `browser_do` run on, as `source/model`, any model you have. Empty takes `modelTier.dumb`, then `modelTier.worker`, then the turn's own model. `browserEffort` sets its effort when the model lists it; empty is the model's default.
- `browserSleepMinutes`, 10 by default: how long the relay stays awake once no tofu is connected and no call is out.
- `browserCursor`, on by default: a small cursor labelled `tofu` glides to each click in tofu's own tab. It is drawn only, and never read or hit.
- `browserProfile`, empty by default, names a browser profile, a Chrome profile of tofu's own in `~/.tofu/browser/profiles/<name>`, that tofu drives instead of your Chrome; empty drives your Chrome through the extension. tofu starts the installed Chrome on that folder, with no window while `browserHeadless` is on, its default, reads the port Chrome writes to `DevToolsActivePort` there and drives it with no extension. Later calls attach to the same Chrome, so tabs, cookies and sign-ins carry over, and every tab in it is tofu's own. `tofu browser profile new <name>` makes one, `login <name> [<url>]` opens a window with no debugger to sign in by hand, Google by default, `list` shows each as `running` or `stopped`, `stop` closes its Chrome and `remove` deletes it. `tofu browser read [--tab <id>]` prints a tab's title and address. tofu never starts Chrome on your own profile folder.

    tofu settings set browserModel claude-sub/claude-sonnet-5

## Check it

`tofu browser` prints the relay, `Chrome relay` with `○ asleep` or `● awake`, without waking it, then its `build`, the `extension` build, the `memory` it holds from the system, the other tofu `sessions` connected and when it `sleeps`, such as `10m after the last tofu leaves`. `--json` carries the same in `data`, with `awake`, `builds`, `held_bytes`, `other_sessions` and `sleep_after_ns`. A relay from a tofu that cannot say prints `✗ the relay runs an older tofu that cannot say whether it is asleep` and exits 1.

`tofu browser tabs` wakes it and opens with `Chrome tabs · <n> reachable` and `✓ connected`, then a row per tab with its id, site and title, `●` on the tabs tofu opened and `○` on yours. `--json` prints one document with the tabs in `data.tabs`. With no extension running, either prints one `✗` line and `→ tofu browser install`, and exits 1. `tofu settings get browser` prints `off`, `read` or `drive`.

The same steps the model takes are verbs, each on one tab: `tofu browser observe --tab 123`, `tofu browser click e5 --tab 123`, `tofu browser fill e3 "Lisboa" --tab 123`, and `select <ref> <option>`, `press <key>`, `scroll [<ref>] [up|down]` and `back` the same way. `--all` makes `observe` print the whole tree. An action looks first, so a ref from your last `observe` of an unchanged page names the same element, then prints what changed and the fresh snapshot, which `--json` carries as `data.snapshot`, with `data.moved`.

A whole check runs from a file with `tofu browser batch check.json`, or from standard input with `-`, where `check.json` is the same list of steps, such as:

    [{"action": "navigate", "value": "http://127.0.0.1:5311/"},
     {"action": "click", "target": {"role": "button", "name": "Delete Rice"}},
     {"action": "click", "target": {"role": "button", "name": "Keep it"},
      "check": {"focus": {"role": "button", "name": "Delete Rice"}}}]

Without `--tab` the first step opens a tab of tofu's own and the tab closes when the batch ends. It exits 1 when a check failed, and `--json` carries the answer as `data.report`, with `data.checks_failed`.

## Undo it

`tofu settings set browser off` takes the tools away from the model. `tofu browser uninstall` removes the host, each `com.ephem.tofu.json` it wrote, the copy and the folder, prints `○ removed`, and ends `→ remove the tofu card in chrome://extensions`.
