---
title: Browser extension
description: A Chrome extension and a native host that let tofu read and drive your own browser, with your logins.
order: 1
updated: 2026-10-29
---

A small Chrome extension, shipped inside the tofu binary, and a relay. When
Chrome starts the extension, it starts `tofu-browser`, a copy of tofu, as a
native messaging host, `com.ephem.tofu`, which listens on
`~/.tofu/browser/relay.sock`. A tofu
session reaches your browser through that socket, and the browser tools are
its only users. Nothing goes through a server.

The extension asks for `nativeMessaging`, `debugger`, `tabs`, `tabGroups` and
`storage`. It attaches Chrome's debugger to a tab only when tofu first uses
it, and Chrome shows its bar saying so.

## Your browser, your logins

tofu works in the Chrome you already have, with your logins, so a page behind
a sign-in needs no setup. There's no browser to download: headless Playwright
needed an install of 2.3 s and 19 MB before its first check, and over 24 runs
the relay left 0 files, 0 processes and 0 tabs behind.

Your own tabs stay yours. A tab tofu opens joins an orange `tofu` group and
closes when the run, or the sub-agent that opened it, ends. tofu never
reaches a `chrome://` page, DevTools or the Web Store, never runs script the
model wrote, and never navigates or closes one of your tabs.

> [!WARNING]
> With `browser` set to `drive`, tofu can do in an ordinary tab what you can,
> with your logins.

## Install and turn off

Run `tofu browser install`. It writes the extension to
`~/.tofu/browser/extension` on every system, and registers the host where
your browser looks for it:

```tabs
# Windows
In the registry, under
`HKEY_CURRENT_USER\Software\Google\Chrome\NativeMessagingHosts`, for Chrome.

# Linux
A file, `com.ephem.tofu.json`, in `~/.config/google-chrome/NativeMessagingHosts`
and `~/.config/chromium/NativeMessagingHosts`, and in Brave's and Edge's
when `~/.config/BraveSoftware/Brave-Browser` or `~/.config/microsoft-edge`
exists.

# macOS
A file, `com.ephem.tofu.json`, in `NativeMessagingHosts` under
`~/Library/Application Support/Google/Chrome` and
`~/Library/Application Support/Chromium`, and under
`BraveSoftware/Brave-Browser` or `Microsoft Edge` there when it exists.
```

The install prints each place it wrote. Then, in the browser:

1. In `chrome://extensions`, turn on **Developer mode**, click **Load
   unpacked**, and pick `~/.tofu/browser/extension`.
2. Pin the tofu icon. Its badge reads `on`, `read`, `act` or `off`.

Install a browser after tofu and `tofu browser install` again, so its
folder gets the file.

A new tofu rewrites the extension folder and Chrome reloads it. To turn the
browser off, press **ctrl+k**, type `browser`, and set **Browser** to `off`.
`read` keeps only the reading tools.

![Search with browser typed: the Browser, driver, steps, model, effort and cursor settings](./media/tui-search.png)

## How it runs

Chrome starts the relay, not tofu. Chrome runs a native messaging host for as
long as the extension is connected, which is as long as Chrome is open, so
the program it starts shows in your process list even when no tofu session
is open. That program is `tofu-browser`, a byte copy of the tofu that ran
`tofu browser install`. It runs only the relay: it reads the extension's
messages on standard input and serves `relay.sock`.

It is a copy because Chrome holds the program it started. On Windows a
running program can be renamed but never overwritten or deleted, so with
Chrome running `tofu.exe` itself, any install that writes over `tofu.exe`
fails while Chrome is open. With the copy, Chrome holds `tofu-browser.exe`
and `tofu.exe` stays free to replace. Linux and macOS work the same way, so
every system has one layout.

| System | The copy Chrome starts |
| --- | --- |
| Windows | `%USERPROFILE%\.tofu\bin\tofu-browser.exe` |
| macOS | `~/.tofu/bin/tofu-browser`, mode `0755` |
| Linux | `~/.tofu/bin/tofu-browser`, mode `0755` |

Every `com.ephem.tofu.json` names the copy, and
`~/.tofu/browser/tofu-source` records which tofu it was copied from. It
stays current three ways:

- `tofu browser install` writes it again.
- `tofu update` rewrites it after replacing tofu, and prints a line saying so.
- A newer tofu that reaches a relay on an older build makes the relay copy
  the recorded tofu over itself, answer the calls it has out, and exit. The
  extension reconnects and Chrome starts the new copy. The first call says
  `updating: the relay restarts on this build` and then goes through.

A running copy is replaced by renaming it aside to `tofu-browser.exe.old-<n>`
on Windows, deleted on the next refresh, and by a rename over it on Linux
and macOS. An install from before the copy names `tofu.exe` itself;
`tofu doctor` calls it stale and `tofu browser install` moves it to the copy.

## Asleep and awake

The relay sleeps while no tofu uses it, so a Chrome left open all day costs
next to nothing. It starts asleep. Asleep it keeps the extension's port open,
which keeps Chrome's service worker alive, holds no tabs, and hears no tab
events, because the extension stops sending them while still keeping its own
bookkeeping. The relay then waits on its standard input and its socket and
does nothing else.

- **What wakes it:** the first call that needs Chrome, such as
  `tofu browser tabs`, a read or an action. The relay tells the extension
  `wake`, the extension answers with its open tabs, and from then on sends
  each change. `tofu browser` and the build check answer without waking it.
- **When it sleeps:** once the last tofu has left and no call is out, after
  `browserSleepMinutes`, 10 by default. The relay reads the setting each time
  a tofu leaves, so `tofu settings set browserSleepMinutes 1` takes effect on
  the next sleep with no restart. A call still out, or a tofu still
  connected, keeps it awake however long it takes.
- **What sleeping does:** the relay tells the extension `sleep`, drops its
  tab list and gives its free memory back to the system.

Over 5 idle minutes, started the way Chrome starts it, three fresh starts
each, the median:

| Idle for 5 minutes | Windows | Linux |
| --- | --- | --- |
| Context switches a second, the relay asleep | 0.4 | 0.6 |
| Context switches a second, the relay before it slept, 0.5.20 | 3.1 | 3.2 |
| Context switches a second, a Go program that only reads its input | 0.03 | 0.03 |
| CPU, % of one core, the relay asleep | 0.0 | 0.0 |
| Private memory, the relay asleep | 6.3 MB | 7.8 MB |

The context switches left asleep are the Go runtime's forced collection,
every 2 minutes: with collection off, `GOGC=off`, the asleep relay reads
0.03 a second on Windows and 0.06 on Linux, next to 0.03 for a Go program
that only reads its standard input.

## Commands

`tofu browser` prints the relay without waking it:

```text
Chrome relay                                                            ○ asleep

  build      995fa8a7a901
  extension  995fa8a7a901
  memory     7.7 MB held from the system
  sessions   0 other tofu connected
  sleeps     10m after the last tofu leaves

  → the first call that needs Chrome wakes it: tofu browser tabs is one
```

Awake, the verdict reads `● awake`, and `--json` carries the same in `data`:
`awake`, `builds`, `held_bytes`, `other_sessions` and `sleep_after_ns`.

`tofu browser tabs` wakes it and lists the tabs tofu reaches and the builds
on each side:

```text
Chrome tabs                                          ✓ updated from 6bd3adb862e4
  4 reachable · extension 5fa612641718 · tofu 5fa612641718
```

The verdict is `connected` when nothing changed. A row per tab follows, `●`
on tofu's and `○` on yours, and `--json` prints them in `data.tabs`.
`tofu browser uninstall` removes the host, every `com.ephem.tofu.json` it
wrote, the copy, and the folder. When Chrome still runs the copy on Windows
it is renamed aside and the output names it.
