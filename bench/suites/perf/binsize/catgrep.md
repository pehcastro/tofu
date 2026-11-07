# tofu binary size and start time with and without cat-grep

Written by `go run ./bench/suites/perf/binsize` on 2026-11-07, go1.27.1, windows/amd64. Each language row is a build with that grammar added to every row above it. init is the sum of every package init clock in `GODEBUG=inittrace=1 tofu version`, and parser init the part spent in the parser and cat-grep packages, each the median of 7 runs.

| build | bytes | MiB | bytes added | init ms | parser init ms |
|---|---:|---:|---:|---:|---:|
| without cat-grep | 65188352 | 62.17 |  | 5.95 | 0.00 |
| cat-grep, no language | 78495232 | 74.86 | 13306880 | 9.93 | 0.50 |
| + bash | 79888384 | 76.19 | 1393152 | 10.83 | 1.50 |
| + c | 79963648 | 76.26 | 75264 | 11.29 | 1.49 |
| + c_sharp | 80291328 | 76.57 | 327680 | 14.29 | 1.50 |
| + clojure | 80329728 | 76.61 | 38400 | 11.44 | 1.49 |
| + cmake | 80356352 | 76.63 | 26624 | 12.91 | 1.49 |
| + cpp | 80791552 | 77.05 | 435200 | 10.84 | 1.49 |
| + css | 80808960 | 77.07 | 17408 | 10.97 | 1.49 |
| + dart | 80939520 | 77.19 | 130560 | 10.78 | 1.48 |
| + dockerfile | 80970752 | 77.22 | 31232 | 11.34 | 1.49 |
| + elixir | 81207808 | 77.45 | 237056 | 11.28 | 1.90 |
| + erlang | 81263104 | 77.50 | 55296 | 10.93 | 1.49 |
| + go | 81318912 | 77.55 | 55808 | 10.83 | 1.50 |
| + groovy | 81415680 | 77.64 | 96768 | 11.33 | 1.49 |
| + haskell | 81813504 | 78.02 | 397824 | 12.41 | 1.49 |
| + hcl | 81871360 | 78.08 | 57856 | 11.25 | 1.49 |
| + html | 81905152 | 78.11 | 33792 | 11.84 | 1.49 |
| + java | 81953280 | 78.16 | 48128 | 9.95 | 1.49 |
| + javascript | 82018304 | 78.22 | 65024 | 11.31 | 1.49 |
| + json | 82038272 | 78.24 | 19968 | 12.91 | 1.49 |
| + julia | 82297344 | 78.48 | 259072 | 11.92 | 1.49 |
| + kotlin | 82728448 | 78.90 | 431104 | 10.90 | 1.49 |
| + less | 82733568 | 78.90 | 5120 | 10.89 | 1.49 |
| + lua | 82783232 | 78.95 | 49664 | 11.87 | 1.49 |
| + make | 82799616 | 78.96 | 16384 | 10.03 | 1.49 |
| + markdown | 82913280 | 79.07 | 113664 | 10.93 | 1.49 |
| + nix | 82944000 | 79.10 | 30720 | 9.95 | 1.49 |
| + objc | 83200512 | 79.35 | 256512 | 11.43 | 1.49 |
| + ocaml | 83652608 | 79.78 | 452096 | 12.82 | 1.98 |
| + php | 83764736 | 79.88 | 112128 | 12.39 | 1.49 |
| + powershell | 83863040 | 79.98 | 98304 | 11.44 | 1.90 |
| + proto | 83869696 | 79.98 | 6656 | 11.80 | 1.50 |
| + python | 83970048 | 80.08 | 100352 | 10.92 | 1.49 |
| + r | 84055552 | 80.16 | 85504 | 10.95 | 1.49 |
| + ruby | 84260864 | 80.36 | 205312 | 11.39 | 1.49 |
| + rust | 84399104 | 80.49 | 138240 | 11.43 | 1.50 |
| + scala | 85015552 | 81.08 | 616448 | 11.42 | 1.49 |
| + scss | 85050880 | 81.11 | 35328 | 11.41 | 1.89 |
| + solidity | 85086208 | 81.14 | 35328 | 11.26 | 1.90 |
| + sql | 85696000 | 81.73 | 609792 | 9.90 | 1.49 |
| + svelte | 85725184 | 81.75 | 29184 | 11.44 | 1.49 |
| + swift | 86146048 | 82.16 | 420864 | 11.43 | 1.49 |
| + toml | 86164480 | 82.17 | 18432 | 10.89 | 1.49 |
| + tsx | 86317568 | 82.32 | 153088 | 11.80 | 1.90 |
| + typescript | 86474240 | 82.47 | 156672 | 11.90 | 1.49 |
| + vue | 86484992 | 82.48 | 10752 | 11.95 | 1.99 |
| + yaml | 86650368 | 82.64 | 165376 | 10.43 | 1.50 |
| + zig | 86694400 | 82.68 | 44032 | 10.93 | 1.49 |
| every language, built with -tags grammar_subset and one tag per language | 86308864 | 82.31 | -385536 | 9.46 | 0.50 |

With cat-grep and every language that built (47) the binary is 86694400 bytes, 82.68 MiB, 21506048 bytes more than without cat-grep, and the parser packages take 1.49 ms of init. Built with the tags below, which drop the scanners of every grammar tofu does not use, it is 86308864 bytes and 0.50 ms.

```
-tags grammar_subset,grammar_subset_bash,grammar_subset_c,grammar_subset_c_sharp,grammar_subset_clojure,grammar_subset_cmake,grammar_subset_cpp,grammar_subset_css,grammar_subset_dart,grammar_subset_dockerfile,grammar_subset_elixir,grammar_subset_erlang,grammar_subset_go,grammar_subset_groovy,grammar_subset_haskell,grammar_subset_hcl,grammar_subset_html,grammar_subset_java,grammar_subset_javascript,grammar_subset_json,grammar_subset_julia,grammar_subset_kotlin,grammar_subset_less,grammar_subset_lua,grammar_subset_make,grammar_subset_markdown,grammar_subset_nix,grammar_subset_objc,grammar_subset_ocaml,grammar_subset_php,grammar_subset_powershell,grammar_subset_proto,grammar_subset_python,grammar_subset_r,grammar_subset_ruby,grammar_subset_rust,grammar_subset_scala,grammar_subset_scss,grammar_subset_solidity,grammar_subset_sql,grammar_subset_svelte,grammar_subset_swift,grammar_subset_toml,grammar_subset_tsx,grammar_subset_typescript,grammar_subset_vue,grammar_subset_yaml,grammar_subset_zig
```
