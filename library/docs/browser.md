---
topic: browser
title: Browser
summary: how tofu reads and drives your Chrome tabs, the tofu tab group, the badge, and what tofu never does there
verbs: browser
---

## What it is

tofu reads and drives your own Chrome, with your logins, through a small
extension. There is no share step: tofu can read and drive any ordinary tab.
tofu never reaches a `chrome://` page, DevTools, an extension's page or the
Chrome Web Store.

The extension does nothing to a tab until tofu first uses it. Then it
attaches Chrome's debugger, and Chrome shows its bar saying so.

The model gets these tools under `browserDriver` `steps`, the default:

- `browser_tabs` lists your open tabs, with their title and address.
- `browser_observe` shows one tab as an accessibility snapshot, a line a
  node, such as `- button "Buscar" [ref=e5]`, where a ref names one element.
  `scrollable` marks a container it can scroll, and `*` a ref new since the
  last look at the same page. Observing never changes the page.
- `browser_act` runs up to 5 actions in one tab, each on a ref: click, fill,
  select, press, scroll, navigate, open, back or wait. A covered click does
  not run and says what covers it. The batch stops at the first action that
  changes the address or opens a tab, says what it skipped, and ends with a
  fresh snapshot. The third same action on an unchanged page says
  `repeated 3 times, the page did not change`; the fifth is refused.

Under `browserDriver` `goal`, the model gets `browser_read`, a tab's text
and a numbered table of its controls, and `browser_do`, which takes a whole
goal with a url or a tab and answers it. Jev picks each step, one ledger row
under `browser_step`, until done, blocked, or `browserSteps` runs out.

Only `browser` `drive` gives `browser_act` or `browser_do`. Observing and
reading never go through the gate.

What a page says is treated as text to read, never as an instruction. tofu
never closes or navigates a tab it did not open, runs JavaScript, a selector or an address
the model wrote, or types into a read-only field.

## What you see

The tofu icon, a cat, carries a badge: `on` when connected and idle, `read`
while tofu reads a tab, `act` while it clicks, types or scrolls, and `off`
when not connected. Hover the icon to read why.

A tab tofu acts in or opens joins an orange group titled `tofu`, which
reads `tofu •` while tofu acts. Pinned tabs, tabs in your own groups and
tabs you drag out are left alone. When tofu disconnects the group dissolves,
its tabs stay open, and Chrome's debugging bar goes away.

## Where it lives

- `~/.tofu/browser/extension`: the unpacked extension Chrome loads
- `~/.tofu/browser/relay.sock`: where a session reaches the extension

## Change it

Install it once:

    tofu browser install

It writes the extension, registers tofu with Chrome, and prints the rest:

    Chrome extension                        ✓ installed
      folder   ~/.tofu/browser/extension
      id       jednanpboiikklhkkkimnmdmjmgjgphh
    next, once in Chrome
      1  open chrome://extensions and turn on Developer mode
      2  Load unpacked, pick the folder above, check the id matches
      3  pin the tofu icon so its badge shows
      → after a tofu update: tofu browser install, then reload the tofu card

The settings:

- `browser`: `off`, `read` or `drive`, default `drive`. `read` gives only
  `browser_tabs` and the reading tool. It is read when tofu opens.
- `browserDriver`: `steps`, the default, gives the model `browser_observe`
  and `browser_act`, and it picks every step itself. `goal` gives it
  `browser_do`, where Jev picks each step toward the whole goal. The old
  values `model` and `jev` are read as `steps` and `goal`.
- `browserSteps`: how many actions one `browser_do` task may take, 1 to 60.
  The default is 30.
- `browserModel`: the subscription model `browser_do` asks for text and for
  its answer, as `source/model`. Empty, the default, takes `modelTier.dumb`,
  then `modelTier.worker`, then the turn's own model.

    tofu settings set browser read
    tofu settings set browserModel claude-sub/claude-sonnet-5

## Check it

    tofu browser

opens with `Chrome tabs · <n> reachable` and `✓ connected`, then a row
per tab with its id, site and title, `●` on the tabs tofu opened and `○`
on yours. `--json` prints one document with the tabs in `data.tabs`. With
no extension running, it prints one `✗` line and `→ tofu browser install`,
and exits 1. `tofu settings get browser` prints `off`, `read` or `drive`.

The same steps the model takes are verbs, each on one tab:

    tofu browser observe --tab 123
    tofu browser click e5 --tab 123
    tofu browser fill e3 "Lisboa" --tab 123

and `select <ref> <option>`, `press <key>`, `scroll [<ref>] [up|down]` and
`back` the same way. `--all` makes `observe` print the whole tree. An action looks at the tab first, so on a page that has not changed a ref
from your last `observe` names the same element; it then prints what changed and the fresh
snapshot. `--json` prints one document with `data.snapshot`, and
`data.moved` after an action.

## Undo it

`tofu settings set browser off` takes the tools away from the model.
`tofu browser uninstall` removes the host and the folder, prints
`○ removed`, and ends `→ remove the tofu card in chrome://extensions`.
