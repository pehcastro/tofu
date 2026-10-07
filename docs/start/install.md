---
title: Install
description: Install tofu with one command on Linux, macOS or Windows, let it update itself, and remove it.
order: 2
updated: 2026-10-07
---

tofu is one binary, `tofu`, with no runtime and no daemon. An install script
puts the latest release in `~/.local/bin`, and tofu keeps it current. Everything tofu keeps goes under `~/.tofu`.

## Install with the script

````tabs
# Linux and macOS

`curl -fsSL https://raw.githubusercontent.com/pehcastro/tofu/release/scripts/install.sh | sh`


# Windows
In PowerShell:


`irm https://raw.githubusercontent.com/pehcastro/tofu/release/scripts/install.ps1 | iex`

````

The script picks the build for your system and processor (amd64 or arm64),
downloads it from the GitHub release with its `checksums.txt`, and refuses to
install when the checksum does not match. It then moves `tofu` into
`~/.local/bin`, prints `tofu version`, and tells you how to add that folder to
your PATH if it is not there yet.

Releases are built from one branch for Windows, Linux and macOS, so the same
command always gets a tested binary and nothing needs Go on your machine. To
build it yourself, see [Build from source](/docs/start/build-from-source).

## Choose the version or the folder

The script reads three environment variables:

| Variable | Does |
|---|---|
| `TOFU_VERSION` | install that version, such as `0.5.0`, instead of the latest |
| `TOFU_INSTALL_DIR` | install into this folder instead of `~/.local/bin` |
| `TOFU_RELEASE_URL` | read the release files from this address |

````tabs
# Linux and macOS

`curl -fsSL https://raw.githubusercontent.com/pehcastro/tofu/release/scripts/install.sh | TOFU_VERSION=0.5.0 sh`


# Windows
`
$env:TOFU_VERSION = '0.5.0'
irm https://raw.githubusercontent.com/pehcastro/tofu/release/scripts/install.ps1 | iex
`
````

## Update

tofu updates itself. The app looks up the latest release at most once an
hour, installs a newer one in the background, and says in the chat
`tofu 0.5.7 is installed · restart to use it`. Sessions already open keep
running their copy until you restart them. To only be told, turn it off:

```
tofu settings set autoUpdate false
```

`tofu update` installs the latest release now, whatever the setting.
`tofu changelog` then prints what changed since the version you last read.

```
tofu update
tofu changelog
```

Running the install script again also installs the latest release over the
current one.

## Uninstall

If you installed the browser extension, remove it first, then delete the
binary. Delete `~/.tofu` too if you want your settings, credentials and
sessions gone.

````tabs
# Linux and macOS
`
tofu browser uninstall`

`rm ~/.local/bin/tofu`

`rm -rf ~/.tofu`

# Windows

`tofu browser uninstall`

`Remove-Item $HOME\.local\bin\tofu.exe`

`Remove-Item -Recurse $HOME\.tofu`

````

> [!WARNING]
> Deleting `~/.tofu` removes your stored credentials and every recorded
> session. It cannot be undone.

## Check the install

```
tofu version
```

```
version   0.5.0-rc-fix24+dev
commit    d8184775c9bfd4a1db16d70da3b3be4529b87418-dirty
go        go1.27.1
```

A release build prints its own version and commit. Next:
[Setup](/docs/start/setup).
