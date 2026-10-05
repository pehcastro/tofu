---
title: Browser extension
description: A Chrome extension and a native host that let tofu read and drive your own browser, with your logins.
order: 1
updated: 2026-10-05
---

A small Chrome extension, shipped inside the tofu binary, and a relay. When
Chrome starts the extension, it starts tofu as a native messaging host,
`com.ephem.tofu`, which listens on `~/.tofu/browser/relay.sock`. A tofu
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

## Commands

`tofu browser` lists the tabs tofu reaches and the builds on each side:

```text
Chrome tabs                                          ✓ updated from 6bd3adb862e4
  4 reachable · extension 5fa612641718 · tofu 5fa612641718
```

The verdict is `connected` when nothing changed. A row per tab follows, `●`
on tofu's and `○` on yours, and `--json` prints them in `data.tabs`.
`tofu browser uninstall` removes the host, every `com.ephem.tofu.json` it
wrote, and the folder.
