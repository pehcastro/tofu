# tofu binary size and start time with and without cat-grep

Written by `go run ./bench/suites/perf/binsize` on 2026-11-09, go1.27.1, windows/amd64. Each language row is a build with that grammar added to every row above it. init is the sum of every package init clock in `GODEBUG=inittrace=1 tofu version`, and parser init the part spent in the parser and cat-grep packages, each the median of 7 runs.

| build | bytes | MiB | bytes added | init ms | parser init ms |
|---|---:|---:|---:|---:|---:|
| without cat-grep | 65264128 | 62.24 |  | 5.25 | 0.00 |
| cat-grep, no language | 77198848 | 73.62 | 11934720 | 5.74 | 0.00 |
| + bash | 78488064 | 74.85 | 1289216 | 4.99 | 0.51 |
| + c | 78602240 | 74.96 | 114176 | 5.85 | 0.52 |
| + c_sharp | 79016960 | 75.36 | 414720 | 5.63 | 0.52 |
| + clojure | 79041536 | 75.38 | 24576 | 5.17 | 0.51 |
| + cmake | 79079424 | 75.42 | 37888 | 5.81 | 0.51 |
| + cpp | 79859200 | 76.16 | 779776 | 6.26 | 0.52 |
| + css | 79954944 | 76.25 | 95744 | 5.20 | 0.00 |
| + dart | 80089088 | 76.38 | 134144 | 6.16 | 0.00 |
| + dockerfile | 80151552 | 76.44 | 62464 | 5.25 | 0.52 |
| + elixir | 80457728 | 76.73 | 306176 | 5.24 | 0.52 |
| + erlang | 80525824 | 76.80 | 68096 | 6.27 | 0.53 |
| + go | 80655872 | 76.92 | 130048 | 4.72 | 0.51 |
| + groovy | 80983040 | 77.23 | 327168 | 4.95 | 0.00 |
| + haskell | 81488896 | 77.71 | 505856 | 6.24 | 0.51 |
| + hcl | 81541120 | 77.76 | 52224 | 6.18 | 0.04 |
| + html | 81582592 | 77.80 | 41472 | 4.98 | 0.52 |
| + java | 81654784 | 77.87 | 72192 | 5.70 | 0.52 |
| + javascript | 81803264 | 78.01 | 148480 | 5.38 | 0.52 |
| + json | 81807872 | 78.02 | 4608 | 5.23 | 0.52 |
| + julia | 82142720 | 78.34 | 334848 | 5.37 | 0.52 |
| + kotlin | 82680832 | 78.85 | 538112 | 6.25 | 0.52 |
| + less | 82717184 | 78.89 | 36352 | 6.14 | 0.02 |
| + lua | 82747392 | 78.91 | 30208 | 5.20 | 0.51 |
| + make | 82777600 | 78.94 | 30208 | 5.26 | 0.52 |
| + markdown | 82909184 | 79.07 | 131584 | 5.76 | 0.52 |
| + nix | 82940928 | 79.10 | 31744 | 5.69 | 0.04 |
| + objc | 83222528 | 79.37 | 281600 | 5.17 | 0.52 |
| + ocaml | 83813888 | 79.93 | 591360 | 6.24 | 0.51 |
| + php | 83977216 | 80.09 | 163328 | 5.73 | 0.54 |
| + powershell | 84116480 | 80.22 | 139264 | 5.79 | 0.52 |
| + proto | 84136448 | 80.24 | 19968 | 6.69 | 0.52 |
| + python | 84366336 | 80.46 | 229888 | 5.72 | 0.53 |
| + r | 84462592 | 80.55 | 96256 | 5.76 | 0.01 |
| + ruby | 84719616 | 80.79 | 257024 | 5.72 | 0.53 |
| + rust | 85055488 | 81.12 | 335872 | 4.84 | 0.52 |
| + scala | 85818880 | 81.84 | 763392 | 5.82 | 0.53 |
| + scss | 85976064 | 81.99 | 157184 | 5.37 | 0.52 |
| + solidity | 86031360 | 82.05 | 55296 | 5.65 | 0.52 |
| + sql | 87078912 | 83.04 | 1047552 | 6.39 | 0.51 |
| + svelte | 87110656 | 83.08 | 31744 | 4.76 | 0.01 |
| + swift | 87714304 | 83.65 | 603648 | 6.99 | 1.00 |
| + toml | 87734272 | 83.67 | 19968 | 5.73 | 0.52 |
| + tsx | 88091136 | 84.01 | 356864 | 6.25 | 0.53 |
| + typescript | 88462848 | 84.36 | 371712 | 6.07 | 0.52 |
| + vue | 88481280 | 84.38 | 18432 | 5.01 | 0.00 |
| + yaml | 88664064 | 84.56 | 182784 | 5.16 | 0.54 |
| + zig | 88727552 | 84.62 | 63488 | 6.27 | 0.05 |
| every language, built with -tags grammar_subset and one tag per language | 88335872 | 84.24 | -391680 | 6.97 | 0.52 |

With cat-grep and every language that built (47) the binary is 88727552 bytes, 84.62 MiB, 23463424 bytes more than without cat-grep, and the parser packages take 0.05 ms of init. Built with the tags below, which drop the scanners of every grammar tofu does not use, it is 88335872 bytes and 0.52 ms.

```
-tags grammar_subset,grammar_subset_bash,grammar_subset_c,grammar_subset_c_sharp,grammar_subset_clojure,grammar_subset_cmake,grammar_subset_cpp,grammar_subset_css,grammar_subset_dart,grammar_subset_dockerfile,grammar_subset_elixir,grammar_subset_erlang,grammar_subset_go,grammar_subset_groovy,grammar_subset_haskell,grammar_subset_hcl,grammar_subset_html,grammar_subset_java,grammar_subset_javascript,grammar_subset_json,grammar_subset_julia,grammar_subset_kotlin,grammar_subset_less,grammar_subset_lua,grammar_subset_make,grammar_subset_markdown,grammar_subset_nix,grammar_subset_objc,grammar_subset_ocaml,grammar_subset_php,grammar_subset_powershell,grammar_subset_proto,grammar_subset_python,grammar_subset_r,grammar_subset_ruby,grammar_subset_rust,grammar_subset_scala,grammar_subset_scss,grammar_subset_solidity,grammar_subset_sql,grammar_subset_svelte,grammar_subset_swift,grammar_subset_toml,grammar_subset_tsx,grammar_subset_typescript,grammar_subset_vue,grammar_subset_yaml,grammar_subset_zig
```
