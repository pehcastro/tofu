package rules

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

const colourRule = "id: raw_colour\ndomain: dev\nkind: structural\nconcern: code_rules\nchecker: pattern\nmode: enforced\nscope: \"apps/desk/**\"\nexcept_paths: \"apps/desk/crates/ui/src/theme*, **/themes.rs\"\npattern: \"\\b(rgba?|hsla)\\(\"\ntext: a colour comes from the theme\n"

func patternRules(t *testing.T, files map[string]string) []Rule {
	t.Helper()
	shipped := fstest.MapFS{}
	for name, body := range files {
		shipped["rules/"+name] = &fstest.MapFile{Data: []byte(body)}
	}
	rules, err := LoadFS(shipped, "project")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	return rules
}

func TestCheckWriteRefusesOnlyWhatTheWriteAdds(t *testing.T) {
	rules := patternRules(t, map[string]string{
		"raw_colour@1.yaml": colourRule,
		"off@1.yaml":        strings.ReplaceAll(strings.ReplaceAll(colourRule, "raw_colour", "dormant"), "mode: enforced", "mode: off"),
		"schema@1.yaml":     "id: schema\ndomain: dev\nkind: structural\nconcern: code_rules\nchecker: pattern\nmode: enforced\nscope: apps/desk/crates/core/schema/tofu.host.json\npattern: (?s).+\ntext: regenerate it\n",
		"comments@1.yaml":   "id: comments\ndomain: dev\nkind: structural\nconcern: code_rules\nchecker: comments\nmode: enforced\n",
		"multi_line@1.yaml": "id: multi_line\ndomain: dev\nkind: structural\nconcern: code_rules\nchecker: pattern\nmode: enforced\nscope: \"apps/desk/**\"\npattern: Command::new\\([^;]*?\\)\\s*\\.output\\(\ntext: no process on the ui thread\n",
		"ui_block@1.yaml":   "id: ui_block\ndomain: dev\nkind: structural\nconcern: code_rules\nchecker: pattern\nmode: enforced\nscope: \"apps/desk/**\"\npaths: \"apps/desk/crates/desk/**, apps/desk/crates/ui/**\"\npattern: block_on\\(\ntext: no blocking on the ui thread\n",
		"everywhere@1.yaml": "id: everywhere\ndomain: dev\nkind: structural\nconcern: code_rules\nchecker: pattern\nmode: shadow\npattern: Box::leak\ntext: own the data\n",
	})
	const desk = "apps/desk/crates/desk/src/head.rs"
	const schema = "apps/desk/crates/core/schema/tofu.host.json"
	cases := []struct {
		name, path, before, after string
		want                      []string
	}{
		{"a new colour in scope", desk, "fn a() {}\n", "fn a() {}\nlet c = rgb(0x1e1e1e);\n", []string{"raw_colour " + desk + ":2 let c = rgb(0x1e1e1e);"}},
		{"an old colour and an unrelated line", desk, "let c = rgb(1);\n", "let c = rgb(1);\nfn b() {}\n", nil},
		{"an old colour moved down", desk, "let c = rgb(1);\nfn a() {}\n", "fn a() {}\nlet c = rgb(1);\n", nil},
		{"a second copy of an old colour", desk, "let c = rgb(1);\n", "let c = rgb(1);\nlet d = rgb(1);\n", []string{"raw_colour " + desk + ":2 let d = rgb(1);"}},
		{"outside the scope", "internal/turn/head.go", "", "rgb(1)\n", nil},
		{"under an except path", "apps/desk/crates/ui/src/theme.rs", "", "rgb(1)\n", nil},
		{"under a nested except path", "apps/desk/crates/book/src/themes.rs", "", "rgb(1)\n", nil},
		{"under vendor", "apps/desk/vendor/gpui-ce/src/color.rs", "", "rgb(1)\n", nil},
		{"carriage returns", desk, "", "a\r\nb\r\nrgb(1)\r\n", []string{"raw_colour " + desk + ":3 rgb(1)"}},
		{"a match across lines", desk, "", "fn a() {\n    let out = Command::new(\"git\")\n        .arg(\"x\")\n        .output();\n}\n", []string{"multi_line " + desk + ":2 let out = Command::new(\"git\")"}},
		{"any change to the schema", schema, "{\n}\n", "{\"a\":1}\n", []string{"schema " + schema + ":1 {\"a\":1}"}},
		{"the schema unchanged", schema, "{\n}\n", "{\n}\n", nil},
		{"inside the paths", "apps/desk/crates/ui/src/a.rs", "", "x = block_on(f);\n", []string{"ui_block apps/desk/crates/ui/src/a.rs:1 x = block_on(f);"}},
		{"in scope and outside the paths", "apps/desk/crates/core/src/a.rs", "", "x = block_on(f);\n", nil},
		{"an unscoped rule anywhere", "internal/a.go", "", "x := Box::leak(y)\n", []string{"everywhere internal/a.go:1 x := Box::leak(y)"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			for _, f := range CheckWrite(rules, c.path, c.before, c.after) {
				for _, found := range f.Findings {
					got = append(got, f.RuleID+" "+found.Target+" "+found.Detail)
				}
			}
			if !slices.Equal(got, c.want) {
				t.Fatalf("findings = %q, want %q", got, c.want)
			}
		})
	}
}

func TestWeighRefusesOnlyAnEnforcedFireAndNamesTheRule(t *testing.T) {
	rules := patternRules(t, map[string]string{
		"raw_colour@1.yaml": colourRule,
		"leak@1.yaml":       strings.ReplaceAll(strings.ReplaceAll(colourRule, "raw_colour", "leak"), "mode: enforced", "mode: shadow"),
	})
	_, err := Weigh(CheckWrite(rules, "apps/desk/a.rs", "", "x\nrgb(0x1e1e1e)\n"))
	if err == nil {
		t.Fatal("an enforced finding did not refuse the write")
	}
	for _, want := range []string{"raw_colour", "apps/desk/a.rs:2", "rgb(0x1e1e1e)", "a colour comes from the theme"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not say %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "leak") {
		t.Fatalf("the refusal names the shadow rule: %v", err)
	}
	notes, err := Weigh(CheckWrite(rules[:1], "apps/desk/a.rs", "", "rgb(1)\n"))
	if err != nil || len(notes) != 1 || !strings.Contains(notes[0], "leak (shadow)") {
		t.Fatalf("a shadow finding = %q, %v, want one note naming leak as shadow", notes, err)
	}
	if _, err := Weigh(CheckWrite(rules, "apps/desk/a.rs", "rgb(1)\n", "rgb(1)\nx\n")); err != nil {
		t.Fatalf("an edit that adds no finding was refused: %v", err)
	}
}

func TestLoadRefusesAPatternRuleItCannotRun(t *testing.T) {
	const head = "id: p\ndomain: dev\nkind: structural\nconcern: code_rules\n"
	cases := []struct{ name, body, want string }{
		{"a regex that does not compile", head + "checker: pattern\npattern: rgb(\n", "rgb("},
		{"a pattern checker with no pattern", head + "checker: pattern\n", "needs a pattern"},
		{"a pattern under another checker", head + "checker: comments\npattern: rgb\n", "needs a pattern"},
		{"except paths under another checker", head + "checker: comments\nexcept_paths: a/**\n", "except_paths"},
		{"paths under another checker", head + "checker: comments\npaths: a/**\n", "paths"},
		{"an except path that is not a glob", head + "checker: pattern\npattern: rgb\nexcept_paths: \"a/**, \"\n", "not a path glob"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := LoadFS(fstest.MapFS{"rules/p@1.yaml": {Data: []byte(c.body)}}, "project")
			if err == nil {
				t.Fatal("LoadFS accepted the rule")
			}
			if !strings.Contains(err.Error(), "project/rules/p@1.yaml") || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("the refusal does not name the file and %q: %v", c.want, err)
			}
		})
	}
}
