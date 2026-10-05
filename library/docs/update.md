---
topic: update
title: Updating tofu
summary: how tofu update finds the latest release, checks it and replaces the tofu you run
verbs: update, version
---

## What it is

`tofu update` asks GitHub for the latest release of
github.com/pehcastro/tofu. When that release is newer than the tofu you
run, it downloads the archive for your system, checks its SHA-256 against
the release's `checksums.txt`, and puts the new tofu where the old one
was. It prints both versions. Nothing else in tofu reaches the network for
this, and nothing checks for updates in the background.

`tofu update --check` only says whether a newer release exists. It exits 0
when tofu is current and 10 when a newer release is available.

A tofu built from source, whose version ends `+dev`, is not a release, and
`tofu update` leaves it alone unless you add `--force`.

## Where it lives

The new tofu replaces the file you ran, which is the one the install
script put in the bin folder under your home. Windows cannot overwrite a
running program, so there the old one is renamed to `tofu.exe.old` beside
it, and the next update deletes it.

## Change it

    tofu update
    tofu update --force

The second installs the latest release even when it is not newer, which
also replaces a `+dev` build.

What a successful update prints:

    current   0.5.0
    latest    0.5.1
    from      https://api.github.com
    ~ tofu 0.5.0 → 0.5.1  ~/bin/tofu.exe

`from` names where the release was read. When it is not
`https://api.github.com`, the variable `TOFU_UPDATE_API` is set in your
shell; unset it.

What stops an update, with nothing written and the old tofu kept:

- `has SHA-256 ... and checksums.txt says ...`: the download does not
  match the sum published with it. Run `tofu update` again later.
- `has no checksums.txt` or `has no line for`: the release cannot be
  verified, so tofu does not install it.
- `has no tofu_<version>_<os>_<arch> for this system`: the release was not
  built for your system.
- `is not a release build`: this tofu was built from source.
- `is still running from an earlier update`: on Windows, a tofu started
  before the last update still holds `tofu.exe.old`. Close every tofu,
  including Chrome's browser relay, and run it again.

## Check it

    tofu update --check
    tofu version

## Undo it

On Windows, `tofu.exe.old` is the build you had: close every tofu and
rename it back to `tofu.exe`. Anywhere, the install script takes
`TOFU_VERSION` to install one version in particular.
