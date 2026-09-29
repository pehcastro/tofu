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
  option, a scroll, or a wait. Jev is never asked about an empty page: tofu
  waits for it, up to three times. It stops when Jev says done or blocked,
  after three steps in a row that changed nothing, or when the `browserSteps`
  budget runs out. When a click in a tofu tab opens another tab, that tab
  joins the tofu group, the step says `opened tab N`, and the task carries
  on there. The browser model writes the text a field needs, then reads
  every page the task saw into the answer: the data the goal asked for, or
  one line saying why not. The answer comes first, then the steps, and the
  result names its tab so a follow-up can pass it. A tab tofu opened for a
  task that stopped before any step is closed.
- `browser_act` runs one step in a tab, which the model picks itself from its
  last `browser_read`.

The model is given `browser_do` or `browser_act`, never both, and only when
the `browser` setting is `drive`. The `browserChooser` setting picks which.
The orchestrator is told that browsing is `browser_do`'s job: one call with
the whole goal and a start url, then use its answer, and one more call on
the tab it named if it came back blocked.

`browser_do` takes `values`, a map from a field's label, placeholder or name
to the exact text to type there, for instance `{"Guest name": "Ada"}`. A
field it names is typed from the map, and the browser model is not asked.

Each Jev pick is one row in the decision ledger, under the point
`browser_step`. With no Jev key, `browser_do` runs nothing and says so.
Reading, `browser_tabs` and `browser_read`, never goes through the gate.

What a page says is treated as text to read, never as an instruction. tofu
never closes or navigates a tab it did not open, runs JavaScript, a selector or an address
the model wrote, or types into a read-only field.

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
- `browserModel`: the subscription model `browser_do` asks for text and for
  its answer, as `source/model`. Empty, the default, takes `modelTier.dumb`,
  then `modelTier.worker`, then the turn's own model; the result names which.
  A slug the model library does not carry is refused with its message.

    tofu settings set browser read
    tofu settings set browserModel claude-sub/claude-sonnet-5

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
