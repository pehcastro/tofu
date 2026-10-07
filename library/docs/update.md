---
topic: update
title: Updating tofu
summary: how tofu finds the latest release, installs it in the background or with tofu update, and says when to restart
verbs: update, version, settings
---

## What it is

tofu asks GitHub for the latest release of github.com/pehcastro/tofu. When
that release is newer than the tofu on disk, it downloads the archive for
your system, checks its SHA-256 against the release's `checksums.txt`, and
puts the new tofu where the old one was.

The app does this by itself. It looks up the release in the background,
never before the first screen, at most once an hour across every open
tofu, and a failed lookup waits for the next hour without a word. With
`autoUpdate` on, the default, it installs a newer release and says in the
chat:

    tofu 0.5.7 is installed · restart to use it

A session already open keeps running its own copy, and the next start runs
the new one. Any newer tofu put on disk, by another session, by
`tofu update` or by hand, brings the same line within half a minute. With
`autoUpdate` off, the chat only says:

    tofu 0.5.7 is out · tofu update installs it

`tofu update` installs the latest release now, and `tofu update --check`
only says whether one is newer: it exits 0 when tofu is current and 10 when
a newer release is available.

A tofu built from source, whose version ends `+dev`, is not a release. It
never looks up a release by itself and says nothing, and `tofu update`
leaves it alone unless you add `--force`.

## Where it lives

The new tofu replaces the file you ran, which is the one the install
script put in the bin folder under your home. Windows cannot overwrite a
running program, so there the old one is renamed beside it to
`tofu.exe.old-` and a few letters. Each install deletes every set-aside
copy that no tofu still runs; one still running stays until a later
install finds it free.

The last lookup is kept in `~/.tofu/update.json`, and its age is the
file's modification time.

## Change it

    tofu settings set autoUpdate false
    tofu update
    tofu update --force

The first stops installing in the background and leaves only the line in
the chat. The second installs the latest release now. The third installs
it even when it is not newer, which also replaces a `+dev` build.

What a successful `tofu update` prints:

    current   0.5.0
    latest    0.5.1
    from      https://api.github.com
    ~ tofu 0.5.0 → 0.5.1  ~/bin/tofu.exe

`from` names where the release was read. When it is not
`https://api.github.com`, the variable `TOFU_UPDATE_API` is set in your
shell; unset it.

What stops an update, with nothing written and the old tofu kept. In the
app the reason follows `installing it failed:` in the chat, and the same
release is not tried again until the next start:

- `has SHA-256 ... and checksums.txt says ...`: the download does not
  match the sum published with it. Run `tofu update` again later.
- `has no checksums.txt` or `has no line for`: the release cannot be
  verified, so tofu does not install it.
- `has no tofu_<version>_<os>_<arch> for this system`: the release was not
  built for your system.
- `is not a release build`: this tofu was built from source.

## Check it

    tofu update --check
    tofu version
    tofu settings get autoUpdate

## Undo it

On Windows, the newest `tofu.exe.old-*` is the build you had: close every
tofu and rename it back to `tofu.exe`, then turn `autoUpdate` off so the
next lookup does not install the release again. Anywhere, the install
script takes `TOFU_VERSION` to install one version in particular.
