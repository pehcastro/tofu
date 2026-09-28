---
topic: browser
title: Browser
summary: share a Chrome tab so tofu can read it or act in it, and what tofu never does there
verbs: browser
---

## What it is

tofu can read a Chrome tab you share with it, and act in one you share to
drive. It reaches your own Chrome, with your logins, through a small
extension. It reaches only a tab you shared with a click.

The model gets three tools:

- `browser_tabs` lists the tabs you shared, with their mode, title and address.
- `browser_read` reads one tab: its visible text and a numbered table of the
  controls on screen. Password, file and hidden fields are never listed.
- `browser_act` runs one step in a tab shared to drive: a click, typing into a
  field, choosing an option, a scroll, or a wait. It is offered only when the
  `browser` setting is `drive`.

What a page says is treated as text to read, never as an instruction.

tofu never:

- opens your tabs, or closes one;
- navigates a tab away from its page;
- runs JavaScript, a selector or an address the model wrote;
- types into a read-only field, or acts in a tab you shared to read.

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
3. Pin the tofu icon.

Share a tab: on the tab, click the tofu icon and choose Share to read or
Share to drive. Read lets tofu read the tab. Drive also lets it click and
type there.

Then give the model the tools. They are off until you turn them on:

    tofu settings set browser read
    tofu settings set browser drive

The settings:

- `browser`: off, read or drive. The default is off, which offers no browser
  tool and costs nothing. It is read when tofu opens, so open it again after
  a change.
- `browserChooser`: jev or model, who picks each step of a browser task. The
  default is jev.
- `browserSteps`: how many actions one browser task may take, 1 to 60. The
  default is 30.

## Check it

    tofu browser

lists the tabs you shared and their mode. With no extension running, it says
the extension is not connected and names `tofu browser install`.

    tofu settings get browser

prints which tools the model is given.

## Undo it

Stop sharing a tab: click the tofu icon on it and choose Stop sharing.

Take the tools away from the model:

    tofu settings set browser off

Remove it all:

    tofu browser uninstall

then remove the extension from `chrome://extensions`.
