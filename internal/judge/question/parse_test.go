package question

import (
	"strings"
	"testing"
)

func TestParseFoldedAndLiteralBlocks(t *testing.T) {
	root, err := parse("x.yaml", []byte(`folded: >-
  one
  two

  three
literal: |-
  one
  two
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	folded, _ := root.child("folded")
	if folded.text != "one two\nthree" {
		t.Errorf("folded = %q", folded.text)
	}
	literal, _ := root.child("literal")
	if literal.text != "one\ntwo" {
		t.Errorf("literal = %q", literal.text)
	}
}

func TestParseQuotedScalars(t *testing.T) {
	root, err := parse("x.yaml", []byte(`plain: a: b
double: "a \"b\" c \u2014 d"
single: 'it''s here'
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	plain, _ := root.child("plain")
	if plain.text != "a: b" {
		t.Errorf("plain = %q", plain.text)
	}
	double, _ := root.child("double")
	want := `a "b" c ` + string(rune(0x2014)) + " d"
	if double.text != want {
		t.Errorf("double = %q, want %q", double.text, want)
	}
	single, _ := root.child("single")
	if single.text != "it's here" {
		t.Errorf("single = %q", single.text)
	}
}

func TestParseListOfMaps(t *testing.T) {
	root, err := parse("x.yaml", []byte(`options:
  - name: read
    criteria: a file
  - name: none
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	options, _ := root.child("options")
	if options.kind != nodeList || len(options.items) != 2 {
		t.Fatalf("options = %#v", options)
	}
	name, _ := options.items[0].child("name")
	criteria, _ := options.items[0].child("criteria")
	if name.text != "read" || criteria.text != "a file" {
		t.Errorf("first option = %q %q", name.text, criteria.text)
	}
	second, _ := options.items[1].child("name")
	if second.text != "none" {
		t.Errorf("second option = %q", second.text)
	}
}

func TestParseRefusesMalformedFiles(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"a repeated key", "a: 1\na: 2\n", "appears twice"},
		{"a tab indent", "a:\n\tb: 1\n", "tab in the indent"},
		{"an unknown escape", "a: \"b \\q c\"\n", "unknown escape"},
		{"an unclosed quote", "a: \"b\n", "does not close"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parse("x.yaml", []byte(c.body))
			if err == nil {
				t.Fatalf("parse accepted %q", c.body)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want it to mention %q", err, c.want)
			}
		})
	}
}
