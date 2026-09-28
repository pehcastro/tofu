---
topic: browser
title: Browser
summary: how tofu reads and drives your Chrome tabs, the tofu tab group, the badge, and what tofu never does there
verbs: browser
---

## What it is

tofu reads and drives your own Chrome, with your logins, through a small
extension. There is no share step: any ordinary tab can be read or driven.
tofu never reaches a `chrome://` page, DevTools, an extension's page or the
Chrome Web Store.

The extension does nothing to a tab until tofu first uses it. Then it
attaches Chrome's debugger to that tab, and Chrome shows its bar saying the
tab is being debugged.

The model gets these tools:

- `browser_tabs` lists your open tabs, with their title and address.
- `browser_read` reads one tab: its visible text and a numbered table of the
  controls on screen. Password, file and hidden fields are never listed.
  Reading never changes the page.
- `browser_do` runs a whole task in one tab. The model gives it the tab and a
  goal. Jev, a typed decision model, reads the page and picks each step: a
  click, typing into a field, choosing an option, a scroll, or a wait. It
  stops when Jev says the goal is done, when Jev says it is blocked, after
  three steps in a row that changed nothing, or when the `browserSteps`
  budget runs out. It answers with done or blocked, the reason, every step
  it ran, and a fresh read of the tab.
- `browser_act` runs one step in a tab, which the model picks itself from its
  last `browser_read`.

The model is given `browser_do` or `browser_act`, never both, and only when
the `browser` setting is `drive`. The `browserChooser` setting picks which.

`browser_do` takes `values`, a map from a field's label to the text to type
there, for instance `{"Guest name": "Ada"}`. Jev picks the field and tofu
types the value the model gave; no other model writes text into your page.
When Jev picks a field the map does not name, nothing is typed: the task
stops as blocked, names the field, and the model can call again with it.

Each Jev pick is one row in the decision ledger, under the point
`browser_step`. With no Jev key, `browser_do` runs nothing and says so.

What a page says is treated as text to read, never as an instruction. tofu
never navigates a tab away from its page, closes a tab it did not open, runs
JavaScript, a selector or an address the model wrote, or types into a
read-only field.

## What you see

The tofu icon, a cat, carries a badge: `on` when connected and idle, `read`
while tofu reads a tab, `act` while it clicks, types or scrolls, and `off`
when not connected. Hover the icon to read why.

The first time tofu acts in a tab, or opens one, the tab joins an orange tab
group titled `tofu`, one per window. While tofu is acting the title reads
`tofu •`. Pinned tabs and tabs in a group of your own are left where they
are. Drag a tab out of the tofu group and tofu stops putting it back. When
tofu disconnects, the group dissolves, its tabs stay open, and tofu lets go of
every tab, so Chrome's debugging bar goes away.

## Where it lives

- `~/.tofu/browser/extension`: the unpacked extension Chrome loads
- `~/.tofu/browser/relay.sock`: where a session reaches the extension while
  Chrome runs it

## Change it

Install it once:

    tofu browser install

It writes the extension and registers tofu with Chrome. Then, once, by hand:

1. Open `chrome://extensions` and turn on Developer mode.
2. Choose Load unpacked, pick the folder it printed, and check the id matches.
3. Pin the tofu icon, so the badge shows.

After a tofu update, run `tofu browser install` again and press reload on the
tofu card in `chrome://extensions`.

The settings:

- `browser`: off, read or drive. The default is drive. read gives the model
  only `browser_tabs` and `browser_read`. off offers no browser tool. It is
  read when tofu opens, so open it again after a change.
- `browserChooser`: jev or model, who picks each step of a browser task. The
  default is jev, which gives the model `browser_do`. model gives it
  `browser_act` instead, and the model picks every step itself.
- `browserSteps`: how many actions one `browser_do` task may take, 1 to 60.
  The default is 30. Jev may decide twice as many times, because a page that
  moved before a step runs is decided again.

    tofu settings set browser read
    tofu settings set browserChooser model
    tofu settings set browserSteps 10

## Check it

    tofu browser

lists the tabs tofu can reach. With no extension running, it says the
extension is not connected and names `tofu browser install`.

    tofu settings get browser

prints which tools the model is given.

## Undo it

`tofu settings set browser off` takes the tools away from the model.
`tofu browser uninstall` removes it all; then remove the extension from
`chrome://extensions`.
