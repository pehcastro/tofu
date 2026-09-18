package sys

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
		src  string
		want int
	}{
		{
			name: "line comment",
			src:  "package p\n" + slash + " a line comment\n" + "var x = 1\n",
			want: 1,
		},
		{
			name: "block comment",
			src:  "package p\n" + blockOpen + " a block comment " + blockClose + "\n" + "var x = 1\n",
			want: 1,
		},
		{
			name: "doc comment on exported identifier",
			src:  "package p\n" + slash + " Exported does a thing.\n" + "func Exported() {}\n",
			want: 1,
		},
		{
			name: "trailing comment",
			src:  "package p\n" + "var x = 1 " + slash + " trailing\n",
			want: 1,
		},
		{
			name: "go build directive allowed",
			src:  "//go:build linux\n" + "package p\n",
			want: 0,
		},
		{
			name: "go generate directive allowed",
			src:  "package p\n" + "//go:generate stringer -type=Kind\n" + "type Kind int\n",
			want: 0,
		},
		{
			name: "go embed directive allowed",
			src:  "package p\n" + "import _ \"embed\"\n" + "//go:embed data.txt\n" + "var data string\n",
			want: 0,
		},
		{
			name: "nolint directive allowed",
			src:  "package p\n" + "var x = 1 //nolint:errcheck\n",
			want: 0,
		},
		{
			name: "string literal with slashes is not a comment",
			src:  "package p\n" + `var url = "https:` + slash + `example.com"` + "\n",
			want: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			comments, err := parseComments(tc.name+".go", tc.src)
			if err != nil {
				t.Fatalf("parseComments: %v", err)
			}
			got := violationsOf(comments)
			if len(got) != tc.want {
				t.Fatalf("violations = %d, want %d: %v", len(got), tc.want, got)
			}
		})
	}
}

func TestTreeCommentViolationsSelf(t *testing.T) {
	violations, err := TreeCommentViolations(".")
	if err != nil {
		t.Fatalf("TreeCommentViolations: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("comment violations in internal/sys: %v", violations)
	}
}
