# longedit: rubric

Written 2026-10-14, before any run of this task.

The repository is `example.com/ledger`: six packages and a command, 14 Go files. The six steps of the prompt change one signature with 8 call sites in 6 files and 5 more in tests, rename a type used in 6 files, add a constructor parameter with 8 callers, and add two behaviours. No step names a call site.

Quality is out of 10.

Hidden, one point each, each copied into its package after the run, one at a time:

1. `notfound` (store): `Get` of a present id returns it with a nil error; `Get` of a missing id returns an error matching `store.ErrNotFound`.
2. `rename` (account): `account.Account` exists, `New(id, owner, currency)` sets all three, `Overdrawn` takes `[]Account`.
3. `currency` (transfer): EUR to USD returns `ErrCurrency` and changes neither balance; EUR to EUR moves; a missing target in `Move` and a missing source in `Exposure` both match `store.ErrNotFound`.
4. `statement` (report): `Ada (a1): 12.50 EUR` and `Bo (b2): -7.05 USD frozen`; a missing id matches `store.ErrNotFound`.
5. `service` (api): `Open` makes an EUR account, `Transfer` moves money and passes `transfer.ErrSameAccount` through, and `Balance`, `Freeze` and `Transfer` of a missing id all match `store.ErrNotFound`.

Rubric, one point each:

6. `go test ./...` passes.
7. `gofmt -l .` prints nothing.
8. `go vet ./...` is clean.
9. Old name gone: no `.go` file has the word `Acct`.
10. No stray files: only files under the six existing package directories, or a `_test.go` file, changed or were added.
