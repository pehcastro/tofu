## Where page-corpus.jsonl came from

21 real pages, fetched once on 2026-09-20 by `TestBuildTheCorpusByFetchingEveryPageOnce` in `build_test.go` (`TOFU_BUILD_CORPUS=1`), read here, never re-fetched by any other test.

| source | url | task |
|---|---|---|
| mdn-array-map | developer.mozilla.org/.../Array/map | does Array.prototype.map mutate the original array |
| redis-set | redis.io/docs/latest/commands/set/ | does the redis SET command support an expiry option |
| go-blog-errors | go.dev/blog/error-handling-and-go | what is the idiomatic way to handle an error in Go |
| vue-intro | vuejs.org/guide/introduction.html | what is Vue.js used for |
| rust-mutability | doc.rust-lang.org/.../ch03-01-variables-and-mutability.html | how do you make a variable mutable in Rust |
| mdn-http-404 | developer.mozilla.org/.../HTTP/Status/404 | what does HTTP status 404 mean |
| java-arrays | docs.oracle.com/.../arrays.html | how do you declare an array in Java |
| go-tutorial-start | go.dev/doc/tutorial/getting-started | which command initializes a new Go module |
| prettier-docs | prettier.io/docs/en/index.html | which file formats does prettier support |
| man-ls | man7.org/linux/man-pages/man1/ls.1.html | which ls flag shows hidden files |
| man-grep | man7.org/linux/man-pages/man1/grep.1.html | which grep flag makes the search case-insensitive |
| python-faq | docs.python.org/3/faq/general.html | who created python |
| jsonlines | jsonlines.org | what file extension does JSON Lines use |
| go-doc-install | go.dev/doc/install | how do I verify a go installation was successful |
| pip-getting-started | pip.pypa.io/en/stable/getting-started/ | which command upgrades pip itself |
| conventional-commits | conventionalcommits.org/en/v1.0.0/ | what type of change does a commit prefixed feat represent |
| keep-a-changelog | keepachangelog.com/en/1.1.0/ | what does the Unreleased section hold |
| twelve-factor-config | 12factor.net/config | should configuration be stored in the code according to twelve-factor |
| nodejs-about | nodejs.org/en/about | when was Node.js created |
| python-venv | docs.python.org/3/tutorial/venv.html | which command creates a virtual environment in python |
| opensource-mit | opensource.org/licenses/MIT | does the MIT license require you to include the original copyright notice |

## What was removed before it was written here

Every field of every row went through `bench/corpus.Scrub` inside the same test that fetched it, before the row was ever encoded. `build_test.go` also runs `corpus.LeaksIn` on the encoded row and fails the build if anything survives. None of the 21 pages carried anything the scrub matches: they are public documentation, man pages and specs with no personal path or credential shape in them, so the scrub pass is a proof of absence rather than a record of a substitution, unlike the shell corpus.

## Why these 21 and not larger reference pages

Pages such as `nodejs.org/api/fs.html` or `pkg.go.dev/strings` were fetched and measured first and dropped: `internal/web.Units` still turns them into hundreds to thousands of units, and this point sends one Jev request per page carrying one noul question per candidate unit. Jev's own state ceiling (`konst.JudgeStateTokenCeiling`, about 90,000 bytes once encoded) is a request-level limit, and a page with many hundred candidates multiplies the question instructions by every one of them, not just the state. The 21 kept here are focused, single-topic pages of the kind a fetch is actually run for, and every one of them was checked against that ceiling before being kept.

## What a row is

One JSON object per line: `source`, `url`, `task`, `content_type`, `body`. `body` is the raw fetched HTML, scrubbed. The needle is never stored: `Plant` inserts it into one paragraph unit at read time, the same way `bench/sift.Plant` does for shell output.
