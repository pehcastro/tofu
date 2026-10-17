# tofu desktop

## What is in the folder

A release build, made by `scripts/release.ps1`, lands in `target_release/motion/`:

- `tofu-desktop.exe` is the app. Open it; it carries the light cat icon.
- `bin/tofu-desk.exe` is the command line that drives a running app, for example `bin\tofu-desk.exe screen.open settings`. It opens no window of its own.
- `tofu.exe` is not here. It is the separate agent the app talks to, installed on its own and found on your PATH.

A dev or agent build, from `cargo build -p desk` into any other target folder, is the same `tofu-desktop.exe` with the purple cat icon, so the two are told apart in the taskbar. The icon is picked at build time by `TOFU_DESKTOP_CHANNEL`: `release` gives the light cat, anything else the purple one.
