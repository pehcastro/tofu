# tofu binary size and start time with and without cat-grep

Written by `go run ./bench/suites/perf/binsize` on 2026-11-07, go1.27.1, windows/amd64. Each language row is a build with that grammar added to every row above it. init is the sum of every package init clock in `GODEBUG=inittrace=1 tofu version`, and parser init the part spent in the parser and cat-grep packages, each the median of 7 runs.

| build | bytes | MiB | bytes added | init ms | parser init ms |
|---|---:|---:|---:|---:|---:|
| without cat-grep | 65163264 | 62.14 |  | 7.46 | 0.00 |
| cat-grep, no language | 78374912 | 74.74 | 13211648 | 11.80 | 0.49 |
| + bash | 79766528 | 76.07 | 1391616 | 16.22 | 2.99 |
| + c | 79821824 | 76.12 | 55296 | 11.80 | 1.49 |
| + c_sharp | 80180224 | 76.47 | 358400 | 10.90 | 1.50 |
| + clojure | 80194048 | 76.48 | 13824 | 10.92 | 1.49 |
| + cmake | 80236544 | 76.52 | 42496 | 11.40 | 1.49 |
| + cpp | 80662528 | 76.93 | 425984 | 13.88 | 1.49 |
| + css | 80696832 | 76.96 | 34304 | 11.40 | 1.49 |
| + dart | 80811520 | 77.07 | 114688 | 11.89 | 1.49 |
| + dockerfile | 80847872 | 77.10 | 36352 | 12.40 | 1.49 |
| + elixir | 81088000 | 77.33 | 240128 | 11.92 | 1.49 |
| + erlang | 81140736 | 77.38 | 52736 | 12.42 | 1.49 |
| + go | 81203200 | 77.44 | 62464 | 11.92 | 1.49 |
| + groovy | 81280512 | 77.52 | 77312 | 11.81 | 1.89 |
| + haskell | 81697792 | 77.91 | 417280 | 11.80 | 1.90 |
| + hcl | 81735168 | 77.95 | 37376 | 13.45 | 1.50 |
| + html | 81782784 | 77.99 | 47616 | 12.41 | 1.98 |
| + java | 81832448 | 78.04 | 49664 | 12.91 | 1.99 |
| + javascript | 81899008 | 78.10 | 66560 | 12.81 | 1.50 |
| + json | 81901056 | 78.11 | 2048 | 12.73 | 1.50 |
| + julia | 82168320 | 78.36 | 267264 | 11.78 | 2.00 |
| + kotlin | 82597888 | 78.77 | 429568 | 12.24 | 1.99 |
| + less | 82624000 | 78.80 | 26112 | 14.29 | 2.39 |
| + lua | 82651648 | 78.82 | 27648 | 11.38 | 1.50 |
| + make | 82684928 | 78.85 | 33280 | 11.33 | 1.90 |
| + markdown | 82778624 | 78.94 | 93696 | 12.92 | 1.90 |
| + nix | 82821120 | 78.98 | 42496 | 12.70 | 1.90 |
| + objc | 83080192 | 79.23 | 259072 | 11.81 | 2.39 |
| + ocaml | 83512320 | 79.64 | 432128 | 11.91 | 1.89 |
| + php | 83637248 | 79.76 | 124928 | 12.32 | 1.50 |
| + powershell | 83727360 | 79.85 | 90112 | 12.28 | 1.99 |
| + proto | 83744768 | 79.87 | 17408 | 12.45 | 1.49 |
| + python | 83853312 | 79.97 | 108544 | 12.42 | 1.90 |
| + r | 83921408 | 80.03 | 68096 | 14.33 | 3.40 |
| + ruby | 84137984 | 80.24 | 216576 | 19.32 | 4.89 |
| + rust | 84278784 | 80.37 | 140800 | 38.29 | 11.99 |
| + scala | 84893696 | 80.96 | 614912 | 34.90 | 12.49 |
| + scss | 84928512 | 80.99 | 34816 | 12.40 | 1.50 |
| + solidity | 84971520 | 81.04 | 43008 | 10.90 | 1.48 |
| + sql | 85574656 | 81.61 | 603136 | 10.93 | 1.49 |
| + svelte | 85606400 | 81.64 | 31744 | 16.91 | 3.40 |
| + swift | 86022144 | 82.04 | 415744 | 12.32 | 1.90 |
| + toml | 86034944 | 82.05 | 12800 | 11.41 | 1.99 |
| + tsx | 86193664 | 82.20 | 158720 | 11.39 | 1.49 |
| + typescript | 86336000 | 82.34 | 142336 | 11.89 | 1.49 |
| + vue | 86363648 | 82.36 | 27648 | 11.81 | 1.90 |
| + yaml | 86530048 | 82.52 | 166400 | 11.84 | 1.98 |
| + zig | 86578176 | 82.57 | 48128 | 10.91 | 1.50 |
| every language, built with -tags grammar_subset and one tag per language | 86191104 | 82.20 | -387072 | 11.87 | 0.99 |

With cat-grep and every language (47) the binary is 86578176 bytes, 82.57 MiB, 21414912 bytes more than without cat-grep, and the parser packages take 1.50 ms of init. Built with the tags below, which drop the scanners of every grammar tofu does not use, it is 86191104 bytes and 0.99 ms.

```
-tags grammar_subset,grammar_subset_bash,grammar_subset_c,grammar_subset_c_sharp,grammar_subset_clojure,grammar_subset_cmake,grammar_subset_cpp,grammar_subset_css,grammar_subset_dart,grammar_subset_dockerfile,grammar_subset_elixir,grammar_subset_erlang,grammar_subset_go,grammar_subset_groovy,grammar_subset_haskell,grammar_subset_hcl,grammar_subset_html,grammar_subset_java,grammar_subset_javascript,grammar_subset_json,grammar_subset_julia,grammar_subset_kotlin,grammar_subset_less,grammar_subset_lua,grammar_subset_make,grammar_subset_markdown,grammar_subset_nix,grammar_subset_objc,grammar_subset_ocaml,grammar_subset_php,grammar_subset_powershell,grammar_subset_proto,grammar_subset_python,grammar_subset_r,grammar_subset_ruby,grammar_subset_rust,grammar_subset_scala,grammar_subset_scss,grammar_subset_solidity,grammar_subset_sql,grammar_subset_svelte,grammar_subset_swift,grammar_subset_toml,grammar_subset_tsx,grammar_subset_typescript,grammar_subset_vue,grammar_subset_yaml,grammar_subset_zig
```
