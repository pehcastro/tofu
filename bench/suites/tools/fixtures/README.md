# Tool fixtures

Tiny projects with mock data for checking tools by hand. Copy one to a scratch directory before a run; never run a tool inside this tree.

- `go/`: module `fixture.test/orders`; `go test -v .` gives one pass, one fail (got 650, want 675), one skip; `broken/` has one type error, seen by `go vet ./broken`; `order.go` calls `pricing` through the alias `pr`; `.env.example` names `ORDER_API_URL` and `ORDER_DB_PATH`.
- `rust/`: crate `orders`; `cargo test` gives one pass, one fail (got 650, want 675), one ignored; `broken/broken.rs` sits outside the crate with one type error, seen by `rustc --crate-type lib broken/broken.rs`; `lib.rs` calls `pricing` through `use crate::pricing as pr`; same `.env.example`.
- `python/`: `python -m unittest -v` gives one pass, one fail (got 650, want 675), one skip; `broken/label.py` has one type error, seen by `mypy broken` after `pip install -r requirements-dev.txt` in a venv; `order.py` calls `pricing` through `import pricing as pr`; same `.env.example`.
- `ts/`: `npm test` (node --test) gives one pass, one fail (got 650, want 675), one skip; `broken/label.ts` has one type error, seen by `npm run typecheck` after `npm install`; `order.ts` calls `pricing` through `import * as pr`; same `.env.example`.
- `server/`: module `fixture.test/server`, SQLite through `modernc.org/sqlite` (no cgo); `go run .` seeds `orders.db` from `seed.sql`, prints `listening on 127.0.0.1:8417` (`ORDER_ADDR` and `ORDER_DB_PATH` override) and logs one line per request; `GET /orders/1/total` gives 650, want 675; `GET /slow` burns CPU for a profile; with a fresh HOME set `GOMODCACHE` to the real module cache and `GOPROXY=off`.
- `mixed/`: a Go module and a TS package in one directory; `go test -v .` and `npm test` each give one pass.
