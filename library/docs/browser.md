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

The model gets these tools:

- `browser_tabs` lists your open tabs, with their title and address.
- `browser_read` reads one tab: its visible text and a numbered table of the
  controls on screen, with each link's address. Password, file and hidden
  fields are never listed. Reading never changes the page.
- `browser_do` does a whole goal in one call and answers it. The model gives
  it the goal and a url, a tab to work in, or both:
  - a url alone opens a background tab of tofu's own and waits for the page
    to load, or reuses the tab tofu already has on that site;
  - a tab alone works in that tab as it is;
  - a tab and a url send a tab tofu opened to the url. Your own tabs are
    never sent anywhere.

  Jev, a typed decision model, picks each step: a click, typing, choosing an
  option, a scroll, or a wait, never on an empty page. It stops when Jev
  says done or blocked, after three steps that changed nothing, or when
  `browserSteps` runs out. A tab a click opens joins the tofu group and the
  task carries on there. The browser model writes the text a field needs,
  then answers from every page the task saw, and the result names its tab.
  A tab tofu opened for a task that took no step is closed.
- `browser_act` runs one step in a tab, which the model picks itself from its
  last `browser_read`.

The model is given `browser_do` or `browser_act`, never both, and only when
the `browser` setting is `drive`; `browserChooser` picks which.
`browser_do` takes `values`, a map from a field's label, placeholder or name
to the exact text to type, for instance `{"Guest name": "Ada"}`.

Each Jev pick is one row in the decision ledger, under `browser_step`. With
no Jev key, `browser_do` runs nothing and says so. Reading never goes
through the gate.

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
  `browser_tabs` and `browser_read`. It is read when tofu opens.
- `browserChooser`: jev or model, who picks each step of a browser task. The
  default is jev, which gives the model `browser_do`. model gives it
  `browser_act` instead, and the model picks every step itself.
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

## Undo it

`tofu settings set browser off` takes the tools away from the model.
`tofu browser uninstall` removes the host and the folder, prints
`○ removed`, and ends `→ remove the tofu card in chrome://extensions`.
