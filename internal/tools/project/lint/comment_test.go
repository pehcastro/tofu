package lint

import "testing"

func mark(b ...byte) string {
	return string(b)
}

func TestCommentViolations(t *testing.T) {
	slash := mark('/', '/')
	blockOpen := mark('/', '*')
	blockClose := mark('*', '/')

	cases := []struct {
		name string
		file string
		src  string
		want int
	}{
		{
			name: "line comment",
			file: "p.go",
			src:  "package p\n" + slash + " a line comment\n" + "var x = 1\n",
			want: 1,
		},
		{
			name: "block comment",
			file: "p.go",
			src:  "package p\n" + blockOpen + " a block comment " + blockClose + "\n" + "var x = 1\n",
			want: 1,
		},
		{
			name: "doc comment on exported identifier",
			file: "p.go",
			src:  "package p\n" + slash + " Exported does a thing.\n" + "func Exported() {}\n",
			want: 1,
		},
		{
			name: "trailing comment",
			file: "p.go",
			src:  "package p\n" + "var x = 1 " + slash + " trailing\n",
			want: 1,
		},
		{
			name: "go build directive allowed",
			file: "p.go",
			src:  "//go:build linux\n" + "package p\n",
			want: 0,
		},
		{
			name: "go generate directive allowed",
			file: "p.go",
			src:  "package p\n" + "//go:generate stringer -type=Kind\n" + "type Kind int\n",
			want: 0,
		},
		{
			name: "go embed directive allowed",
			file: "p.go",
			src:  "package p\n" + "import _ \"embed\"\n" + "//go:embed data.txt\n" + "var data string\n",
			want: 0,
		},
		{
			name: "nolint directive allowed",
			file: "p.go",
			src:  "package p\n" + "var x = 1 //nolint:errcheck\n",
			want: 0,
		},
		{
			name: "go directive written with a space is a comment",
			file: "p.go",
			src:  slash + " go:build linux\n" + "package p\n",
			want: 1,
		},
		{
			name: "string literal with slashes is not a comment",
			file: "p.go",
			src:  "package p\n" + `var url = "https:` + slash + `example.com"` + "\n",
			want: 0,
		},
		{
			name: "rust line comment",
			file: "lib.rs",
			src:  "fn main() {}\n" + slash + " a note\n",
			want: 1,
		},
		{
			name: "rust doc comment",
			file: "lib.rs",
			src:  slash + "/ Runs the thing.\nfn run() {}\n",
			want: 1,
		},
		{
			name: "rust block comment",
			file: "lib.rs",
			src:  "fn run() { " + blockOpen + " inline " + blockClose + " }\n",
			want: 1,
		},
		{
			name: "rust inner attribute is not a comment",
			file: "lib.rs",
			src:  "#![allow(dead_code)]\nfn run() {}\n",
			want: 0,
		},
		{
			name: "rust string with slashes is not a comment",
			file: "lib.rs",
			src:  "fn run() -> &'static str { \"a" + slash + "b\" }\n",
			want: 0,
		},
		{
			name: "typescript line comment",
			file: "a.ts",
			src:  "const a = 1;\n" + slash + " a note\n",
			want: 1,
		},
		{
			name: "typescript ts-expect-error directive allowed",
			file: "a.ts",
			src:  slash + " @ts-expect-error\nconst a: number = \"x\";\n",
			want: 0,
		},
		{
			name: "tsx block comment",
			file: "a.tsx",
			src:  blockOpen + " a note " + blockClose + "\nexport const A = () => <div />;\n",
			want: 1,
		},
		{
			name: "python line comment",
			file: "a.py",
			src:  "x = 1  # a note\n",
			want: 1,
		},
		{
			name: "python shebang on the first line allowed",
			file: "a.py",
			src:  "#!/usr/bin/env python3\nx = 1\n",
			want: 0,
		},
		{
			name: "python shebang past the first line is a comment",
			file: "a.py",
			src:  "x = 1\n#!/usr/bin/env python3\n",
			want: 1,
		},
		{
			name: "python noqa and type directives allowed",
			file: "a.py",
			src:  "import os  # noqa: F401\nx = []  # type: list[int]\n",
			want: 0,
		},
		{
			name: "python hash inside a string is not a comment",
			file: "a.py",
			src:  "x = \"# not a comment\"\n",
			want: 0,
		},
		{
			name: "zig line comment",
			file: "main.zig",
			src:  "const a = 1;\n" + slash + " a note\n",
			want: 1,
		},
		{
			name: "zig doc comment",
			file: "main.zig",
			src:  slash + "/ The answer.\npub const a = 1;\n",
			want: 1,
		},
		{
			name: "bash shebang and shellcheck directive allowed",
			file: "run.sh",
			src:  "#!/bin/sh\n# shellcheck disable=SC2086\necho $a\n",
			want: 0,
		},
		{
			name: "markdown is prose and is not read",
			file: "README.md",
			src:  "<!-- a note -->\n# Title\n",
			want: 0,
		},
		{
			name: "a file of no language is not read",
			file: "notes.txt",
			src:  slash + " a note\n",
			want: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sourceCommentViolations(tc.file, []byte(tc.src))
			if err != nil {
				t.Fatalf("sourceCommentViolations: %v", err)
			}
			if len(got) != tc.want {
				t.Fatalf("violations = %d, want %d: %v", len(got), tc.want, got)
			}
		})
	}
}

func TestTreeCommentViolationsSelf(t *testing.T) {
	violations, _, err := TreeCommentViolations(".")
	if err != nil {
		t.Fatalf("TreeCommentViolations: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("comment violations in internal/tools/project/lint: %v", violations)
	}
}
