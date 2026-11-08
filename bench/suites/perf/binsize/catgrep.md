# tofu binary size and start time with and without cat-grep

Written by `go run ./bench/suites/perf/binsize` on 2026-11-08, go1.27.1, windows/amd64. Each language row is a build with that grammar added to every row above it. init is the sum of every package init clock in `GODEBUG=inittrace=1 tofu version`, and parser init the part spent in the parser and cat-grep packages, each the median of 7 runs.

| build | bytes | MiB | bytes added | init ms | parser init ms |
|---|---:|---:|---:|---:|---:|
| without cat-grep | 65264640 | 62.24 |  | 5.84 | 0.00 |
| cat-grep, no language | 76972032 | 73.41 | 11707392 | 6.33 | 0.00 |
| + bash | 78266368 | 74.64 | 1294336 | 5.36 | 0.50 |
| + c | 78374912 | 74.74 | 108544 | 5.27 | 0.53 |
| + c_sharp | 78798336 | 75.15 | 423424 | 6.00 | 0.53 |
| + clojure | 78814208 | 75.16 | 15872 | 7.92 | 0.53 |
| + cmake | 78857728 | 75.20 | 43520 | 5.26 | 0.54 |
| + cpp | 79627776 | 75.94 | 770048 | 5.97 | 0.53 |
| + css | 79733760 | 76.04 | 105984 | 6.27 | 0.53 |
| + dart | 79873024 | 76.17 | 139264 | 6.97 | 0.52 |
| + dockerfile | 79920128 | 76.22 | 47104 | 6.21 | 0.07 |
| + elixir | 80227328 | 76.51 | 307200 | 5.96 | 0.99 |
| + erlang | 80301056 | 76.58 | 73728 | 5.85 | 0.02 |
| + go | 80378368 | 76.65 | 77312 | 6.62 | 0.00 |
| + groovy | 80485888 | 76.76 | 107520 | 5.87 | 0.52 |
| + haskell | 80973824 | 77.22 | 487936 | 5.27 | 0.52 |
| + hcl | 81028096 | 77.27 | 54272 | 5.97 | 0.99 |
| + html | 81071616 | 77.32 | 43520 | 6.27 | 0.54 |
| + java | 81147392 | 77.39 | 75776 | 5.96 | 0.00 |
| + javascript | 81236992 | 77.47 | 89600 | 5.84 | 0.54 |
| + json | 81243136 | 77.48 | 6144 | 5.72 | 0.53 |
| + julia | 81575936 | 77.80 | 332800 | 5.96 | 0.52 |
| + kotlin | 82112000 | 78.31 | 536064 | 6.41 | 0.52 |
| + less | 82141696 | 78.34 | 29696 | 6.86 | 0.56 |
| + lua | 82181632 | 78.37 | 39936 | 5.38 | 0.50 |
| + make | 82225664 | 78.42 | 44032 | 5.97 | 1.00 |
| + markdown | 82332160 | 78.52 | 106496 | 5.39 | 0.53 |
| + nix | 82377728 | 78.56 | 45568 | 6.94 | 1.00 |
| + objc | 82651648 | 78.82 | 273920 | 5.38 | 0.50 |
| + ocaml | 83248640 | 79.39 | 596992 | 5.96 | 0.53 |
| + php | 83408384 | 79.54 | 159744 | 5.95 | 0.00 |
| + powershell | 83560960 | 79.69 | 152576 | 7.27 | 0.53 |
| + proto | 83564544 | 79.69 | 3584 | 5.97 | 0.99 |
| + python | 83713536 | 79.84 | 148992 | 6.55 | 0.51 |
| + r | 83810304 | 79.93 | 96768 | 6.82 | 0.54 |
| + ruby | 84056576 | 80.16 | 246272 | 6.96 | 0.99 |
| + rust | 84240896 | 80.34 | 184320 | 5.09 | 0.00 |
| + scala | 85009920 | 81.07 | 769024 | 6.06 | 0.52 |
| + scss | 85158912 | 81.21 | 148992 | 6.74 | 0.54 |
| + solidity | 85214208 | 81.27 | 55296 | 6.95 | 0.50 |
| + sql | 86267904 | 82.27 | 1053696 | 6.72 | 0.53 |
| + svelte | 86300160 | 82.30 | 32256 | 6.82 | 0.55 |
| + swift | 86893568 | 82.87 | 593408 | 5.98 | 0.53 |
| + toml | 86920192 | 82.89 | 26624 | 6.97 | 0.04 |
| + tsx | 87119872 | 83.08 | 199680 | 5.95 | 0.00 |
| + typescript | 87305728 | 83.26 | 185856 | 6.99 | 0.53 |
| + vue | 87334912 | 83.29 | 29184 | 5.97 | 0.12 |
| + yaml | 87514112 | 83.46 | 179200 | 6.38 | 0.53 |
| + zig | 87580672 | 83.52 | 66560 | 7.33 | 0.53 |
| every language, built with -tags grammar_subset and one tag per language | 87190528 | 83.15 | -390144 | 5.81 | 0.08 |

With cat-grep and every language that built (47) the binary is 87580672 bytes, 83.52 MiB, 22316032 bytes more than without cat-grep, and the parser packages take 0.53 ms of init. Built with the tags below, which drop the scanners of every grammar tofu does not use, it is 87190528 bytes and 0.08 ms.

```
-tags grammar_subset,grammar_subset_bash,grammar_subset_c,grammar_subset_c_sharp,grammar_subset_clojure,grammar_subset_cmake,grammar_subset_cpp,grammar_subset_css,grammar_subset_dart,grammar_subset_dockerfile,grammar_subset_elixir,grammar_subset_erlang,grammar_subset_go,grammar_subset_groovy,grammar_subset_haskell,grammar_subset_hcl,grammar_subset_html,grammar_subset_java,grammar_subset_javascript,grammar_subset_json,grammar_subset_julia,grammar_subset_kotlin,grammar_subset_less,grammar_subset_lua,grammar_subset_make,grammar_subset_markdown,grammar_subset_nix,grammar_subset_objc,grammar_subset_ocaml,grammar_subset_php,grammar_subset_powershell,grammar_subset_proto,grammar_subset_python,grammar_subset_r,grammar_subset_ruby,grammar_subset_rust,grammar_subset_scala,grammar_subset_scss,grammar_subset_solidity,grammar_subset_sql,grammar_subset_svelte,grammar_subset_swift,grammar_subset_toml,grammar_subset_tsx,grammar_subset_typescript,grammar_subset_vue,grammar_subset_yaml,grammar_subset_zig
```
