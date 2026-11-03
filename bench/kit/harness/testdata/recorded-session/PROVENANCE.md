# The turn that was here now lives in the corpus

`turn-18d6a5df2caeac68.json` was committed here and at `bench/stopcheck/corpus/turn-18d6a5df2caeac68.json`, byte for byte the same 1,420 bytes, sha256 `8b6af1820f7a65d764b6d11c156e059930307736588a8f4f73ee2a27bf5a4d89`. TOFU-458 removed this copy on 2026-09-23 after hashing both, and `session_test.go` now reads the corpus path.

It is the one recorded turn in the tree that reports cache reads and cache writes, which is why `TestBilledInputCountsCacheTheWayTheClaudeArmCountsIt` uses it and no other.

`report-2026-09-20.md` in this package cites the old path. The report is dated and was not edited; the bytes it measured are the corpus file under the same name, so its figures stand.
