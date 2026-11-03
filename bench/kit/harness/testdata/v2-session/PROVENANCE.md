# The v2 maintenance turn now lives in the corpus

`turn-18d6a27c7dfb1644.json` was committed here and at `bench/stopcheck/corpus/turn-18d6a27c7dfb1644.json`, byte for byte the same 64,794 bytes, sha256 `7d635bbcbf21f5ec183cd7e6143b6497659f6d91260c899a1965a2193837bb52`. TOFU-458 removed this copy on 2026-09-23 after hashing both. It is the run of 2026-09-19 described in `bench/harness/report-2026-09-20.md`, which cites the old path and was not edited because it is dated.

Three tests read the corpus path by name now: `TestTransformArmsOverTheV2Session` here, `TestBothArmsOverTheV2MaintenanceSession` in `bench/transform`, and `TestStoreTheV2Row` under `task`.

This directory is still where `TestV2TofuArmRunsLiveAndProducesARow` stores a fresh live recording, through `StoreTranscript`. A live test must not write into another package's committed corpus, so a new recording lands here and is measured only once a person moves it into `bench/stopcheck/corpus`, where the uniqueness test keeps one copy of it.
