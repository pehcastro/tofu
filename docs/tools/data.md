---
title: Data
description: shape prints the type of a JSON sample in the project's language, and db reads the project's SQLite or Postgres database and never writes to it.
order: 10
updated: 2026-11-06
---

Two tools answer questions about data. `shape` turns a JSON sample into a type
in the project's language, and `db` reads the project's database, read only.
Both are tofu's own Go code.

## Why they exist

**A type from every item, not the first.** Guessing a type from the first item
of a sample misses a field that is `null` in item 40 or a number that is a
string in one record. `shape` merges every item, marks each field that is
optional or a union, and names the item where it first happens.

**A database read that can't write.** `sqlite3` and `psql` in the shell print
wide tables, ask for a password, and nothing stops them writing. `db` opens
SQLite with `mode=ro`, `PRAGMA query_only` and no `ATTACH`, and Postgres
inside `BEGIN READ ONLY`, so the database itself refuses a write. The
connection is never printed.

## shape

`shape` prints a Go struct, a TypeScript interface, a Rust serde struct or a
Python `TypedDict`. The language comes from `go.mod`, `Cargo.toml`, a Python
project file, `tsconfig.json` or `package.json`, or from `language` (`go`,
`ts`, `rust` or `python`). Give the JSON as `sample` or a file as `path`;
several JSON values one after another (JSON lines) are read as the items of
one array. `name` names the top type, `Root` when left out.

On three orders where `total` is a number in two and a string in one, and
`coupon` is null once and missing once:

```bash
tofu tool shape '{"path":"orders.json","name":"Order"}' --dir .
```

```text
shape: go from C:\work\shop\go\go.mod; orders.json holds an array of 3, typed Order

type Order struct {
	ID     int64   `json:"id"`
	Total  any     `json:"total"`
	Status string  `json:"status"`
	Coupon *string `json:"coupon,omitempty"`
}

marked:
  Order.total union: int64 | string, typed any
  Order.coupon optional: missing from 1 of 3, first in item 3
  Order.coupon optional: null in 1 of 3, first in item 2
```

A nested object becomes a type of its own:

```bash
tofu tool shape '{"sample":"{\"id\":1,\"lines\":[{\"sku\":\"A1\",\"qty\":2}]}","name":"Order","language":"ts"}' --dir .
```

```text
shape: ts as asked; the sample holds one value, typed Order

export interface Order {
  id: number;
  lines: Line[];
}

export interface Line {
  sku: string;
  qty: number;
}
```

Invalid JSON is refused with its line, column and byte:

```text
shape: the sample is not JSON: line 2, column 9, byte 17: invalid character '}' looking for beginning of value
  "total":}
          ^
```

A directory with none of the project files asks for the language:

```text
shape: no go.mod, Cargo.toml, python project file, tsconfig.json or package.json in C:\work\shop\python or above it: name the language
```

`http` gives a JSON answer the same treatment. See [Observe](/docs/tools/observe).

## db

`db` with no `query` prints the schema, one line per column with its type,
`not null` and `primary key`, for every table or for the `table` named. With
`query` it runs that one statement and prints the first 20 rows and the full
count. The connection is the value of a key in the project's `.env` or the
environment, `DATABASE_URL` unless `env` names another: a `postgres://` url,
or a SQLite file path, with `sqlite:` or `file:` in front allowed.
Parameters: `query`, `table`, `env`.

With a `.env` holding `ORDER_DB_PATH=orders.db`:

```bash
tofu tool db '{"env":"ORDER_DB_PATH"}' --dir .
```

```text
items.id INTEGER primary key
items.order_id INTEGER not null
items.sku TEXT not null
items.cents INTEGER not null
orders.id INTEGER primary key
orders.customer TEXT not null
2 tables
```

```bash
tofu tool db '{"env":"ORDER_DB_PATH","query":"SELECT sku, cents FROM items WHERE order_id = 1"}' --dir .
```

```text
sku | cents
MOCK-SKU-A | 25
MOCK-SKU-B | 300
MOCK-SKU-C | 350
3 rows shown of 3
```

A write is refused by the database:

```text
db: refused, ORDER_DB_PATH is opened read only and this statement writes: attempt to write a readonly database (8)
```

A missing key names the parameter that fixes it, and a failing query says
what the database said:

```text
db: no connection: DATABASE_URL is not set in .env or the environment; name the key holding it with env
```

```text
db: the query failed: SQL logic error: no such table: order_lines (1)
```

## Commands

You don't call these tools; the model does. `tofu tool shape` and `tofu tool
db` run one call by hand and print what the model would read. See [One tool
call by hand](/docs/cli/tool).
