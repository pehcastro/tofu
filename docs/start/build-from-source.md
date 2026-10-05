---
title: Build from source
description: Build tofu yourself from a clone of the repository, for development or for a branch that is not released.
order: 6
updated: 2026-10-05
---

Building from source gives you the same `tofu` binary the install script
downloads, made on your own machine from any branch. Use it to work on tofu,
or to run a branch that has not been released. For everyday use, the
[install script](/docs/start/install) is simpler.

## Why it builds from a clone

tofu is plain Go with no cgo, so the Go toolchain is all it needs. The module
is named `tofu`, not by its GitHub path, so `go install
github.com/pehcastro/tofu/...@latest` does not work: build from a checkout.
Only the `release` branch is published as a release; every other branch is
built locally like this.

## Build

You need Go 1.25.8 or later and git.

````tabs
# Linux and macOS

`git clone https://github.com/pehcastro/tofu`

`cd tofu`

`go build -o ~/.local/bin/tofu ./cmd/tofu`


# Windows

`git clone https://github.com/pehcastro/tofu`

`cd tofu`

`go build -o $HOME\.local\bin\tofu.exe ./cmd/tofu`

````

`go install ./cmd/tofu` also works and puts the binary in Go's bin directory.
Building to `~/.local/bin` replaces a script-installed `tofu`, so you run
your own build under the same name.

## Update a source build

Pull and build again. `tofu update` replaces the binary with the latest
release, so skip it while you run your own build.

```
git pull
go build -o ~/.local/bin/tofu ./cmd/tofu
```

## Check the build

`tofu version` names the commit the binary was built from, with `-dirty` when
the tree had uncommitted changes, and a `+dev` version for a build that is
not a release:

```
version   0.5.0-rc-fix24+dev
commit    d8184775c9bfd4a1db16d70da3b3be4529b87418-dirty
go        go1.27.1
```
