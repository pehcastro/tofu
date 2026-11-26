---
title: Observe
description: http calls the project's own server, record saves a command's http and replays it without the network, and profile says where the time or memory goes.
order: 9
updated: 2026-11-25
---

Three tools look at the project while it runs. `http` calls the project's own
server and says what it answered, `record` saves the plain http a command
makes and plays it back later, and `profile` runs one test or command under a
profiler. All three are tofu's own Go code.

## Why they exist

**An answer the model reads in one look.** `curl` prints the whole body, and
a raw profile is a file only a viewer reads. `http` gives the status, the time,
the headers worth reading and a JSON body as its type plus its first items.
`profile` gives the ten functions that cost the most, each at its
`path:line`, and the hottest call path.

**A test that needs an api runs without it.** `record` keeps what the api
answered once, so the same command runs again with the api down and with no
fake written by hand.

## http

`http` calls a server on this machine: `localhost`, `127.0.0.1` or `::1`.
Give `path` as `/orders/1/total`, and the server is found from the kept shell
that listens, or from the `shell` named, or from `port`. A full
`http://localhost:N/...` address works too. A redirect is shown, not followed.
Parameters: `method`, `path`, `body`, `headers`, `shell`, `port`.

```bash
tofu tool http '{"path":"/orders/1/total","port":8417}' --dir .
```

```text
http: GET http://localhost:8417/orders/1/total, port as given: 200 OK in 12 ms
Content-Type: text/plain; charset=utf-8
Content-Length: 24
body: 24 bytes:
{"order":1,"total":650}
shape: go from C:\work\shop\server\go.mod; the sample holds one value, typed Root

type Root struct {
	Order int64 `json:"order"`
	Total int64 `json:"total"`
}
```

A JSON array comes back as its first 3 items and the type of every item, with
the fields some items leave out marked:

```text
http: GET http://localhost:8417/orders/export, port as given: 200 OK in 65 ms
Content-Type: application/json
body: 4139696 bytes, an array of 27000; the first 3:
{"order":1,"customer":"customer-00000","status":"paid","items":[{"sku":"sku-000","cents":250},{"sku":"sku-gift","cents":1200}],"note":"leave at the door","coupon":null}
{"order":2,"customer":"customer-00001","status":"shipped","items":[{"sku":"sku-001","cents":251},{"sku":"sku-gift","cents":1200}],"coupon":null}
{"order":3,"customer":"customer-00002","status":"refunded","items":[{"sku":"sku-002","cents":252},{"sku":"sku-gift","cents":1200}],"coupon":null}
shape: go from C:\work\shop\server\go.mod; the sample holds an array of 27000, typed Root

type Root struct {
	Order    int64   `json:"order"`
	Customer string  `json:"customer"`
	Status   string  `json:"status"`
	Items    []Item  `json:"items"`
	Note     *string `json:"note,omitempty"`
	Coupon   any     `json:"coupon,omitempty"`
}

type Item struct {
	Sku   string `json:"sku"`
	Cents int64  `json:"cents"`
}

marked:
  Root.note optional: missing from 21600 of 27000, first in item 2
  Root.coupon optional: null in 27000 of 27000, first in item 1
```

A server that isn't up is named with the next step:

```text
http: no listener on localhost:8417, port as given: connectex: No connection could be made because the target machine actively refused it. start the server with bash background: true, or wait for it with shell wait for port:8417
```

An address off this machine is refused, and `fetch` is named for it:

```text
http: example.com is not this machine: http calls only a local server, on localhost, 127.0.0.1 or ::1. use fetch for a page on the web
```

Without `port` or a full address, `http` needs a kept shell that listens. A
`tofu tool` call has no kept shells, so there it answers `no shell of this
project listens on a port, running: none`.

## record

`record` with `mode` `start` runs `command` once under a proxy on `127.0.0.1`
that only that command sees, through `HTTP_PROXY` and `http_proxy`, and saves
every request and its answer under `name`. `mode` `replay` runs the command
again under a proxy that answers only from that recording. Give `command` as
words. Parameters: `mode`, `name`, `command`.

```bash
tofu tool record '{"mode":"start","name":"order-total","command":["curl","-s","http://127.0.0.1:8417/orders/1/total"]}' --dir .
```

```text
{"order":1,"total":650}
record: saved 1 requests as order-total at C:\Users\dev\.tofu\projects\server-4ea7b235\scratchpad\shared\record\order-total.json
GET http://127.0.0.1:8417/orders/1/total 200, 24 bytes
```

With the server stopped, the same command in `replay` gets the same answer:

```text
{"order":1,"total":650}
record: answered 1 requests from the 1 recorded in C:\Users\dev\.tofu\projects\server-4ea7b235\scratchpad\shared\record\order-total.json
```

A request the recording doesn't hold fails, named by its url:

```text
record: replay order-total: no recording for GET http://127.0.0.1:8417/orders/2/total
record: no recording for GET http://127.0.0.1:8417/orders/2/total
```

A name with no recording asks for `start` first:

```text
record: no recording named nothing-here at C:\Users\dev\.tofu\projects\server-4ea7b235\scratchpad\shared\record\nothing-here.json: run mode start first
```

`https` isn't intercepted: a `CONNECT` is refused, so record against `http`
urls. A client that skips the proxy for `localhost`, such as Go's `net/http`,
isn't seen; `curl` and Python `requests` are. Recordings live in the
project's shared scratchpad under `~/.tofu`, never in the project.

## profile

`profile` runs one test or command under a profiler and returns the ten
functions where the cpu time or the memory goes, each with `flat` (spent in
the function itself), `cum` (with what it calls) and the `path:line` it is
defined at, then the hottest call path. Parameters: `command` as words,
`kind` as `cpu` or `mem`. A Go profile is read from Go's
[pprof](https://github.com/google/pprof) format inside tofu, so no `go tool
pprof` runs.

| Command | Profiler |
|---|---|
| `go test` with one package | `-cpuprofile` or `-memprofile` |
| `node` with a script | `--cpu-prof` or `--heap-prof` |
| `python` with a script | `py-spy`, cpu only |

```bash
tofu tool profile '{"command":["go","test","./handler","-run","TestHandle"],"kind":"cpu"}' --dir .
```

```text
profile cpu of go test ./handler -run TestHandle: 1510.0 ms from 15 recorded stacks, top 10 of 27 functions by flat
flat ms  flat%  cum ms  cum%   func                                                     path:line
1480.0   98.0%  1490.0  98.7%  slow/handler.checksum                                    handler/handler.go:30
10.0     0.7%   1500.0  99.3%  slow/handler.Handle                                      handler/handler.go:13
10.0     0.7%   10.0    0.7%   runtime.asyncPreempt                                     .../src/runtime/preempt_amd64.s:8
10.0     0.7%   10.0    0.7%   runtime.cgocall                                          .../src/runtime/cgocall.go:134
0.0      0.0%   1500.0  99.3%  slow/handler.TestHandle                                  handler/handler_test.go:5
0.0      0.0%   1500.0  99.3%  testing.tRunner                                          .../src/testing/testing.go:2030
0.0      0.0%   10.0    0.7%   internal/syscall/windows/registry.Key.GetMUIStringValue  .../windows/registry/value.go:118
0.0      0.0%   10.0    0.7%   internal/syscall/windows/registry.regLoadMUIString       .../windows/registry/zsyscall_windows.go:84
0.0      0.0%   10.0    0.7%   runtime/pprof.(*profileBuilder).readMapping              .../runtime/pprof/proto_windows.go:15
0.0      0.0%   10.0    0.7%   runtime/pprof.newProfileBuilder                          .../runtime/pprof/proto.go:259
hot path: testing.tRunner > slow/handler.TestHandle > slow/handler.Handle > slow/handler.checksum
```

For a Node script, the first rows of the same answer:

```bash
tofu tool profile '{"command":["node","report.js"],"kind":"cpu"}' --dir .
```

```text
profile cpu of node report.js: 89.4 ms from 53 recorded stacks, top 10 of 43 functions by flat
flat ms  flat%  cum ms  cum%    func                      path:line
75.8     84.9%  75.8    84.9%   checksum                  report.js:1
```

A profiler missing from `PATH` is named with how to install it, and any other
command is refused:

```text
profile: py-spy is not on PATH: install it with pip install py-spy, then run profile again
```

```text
profile: profile runs go test, node or python, not "cargo"
```

## Commands

You don't call these tools; the model does. `tofu tool http`, `tofu tool
record` and `tofu tool profile` run one call by hand and print what the model
would read. See [One tool call by hand](/docs/cli/tool).
