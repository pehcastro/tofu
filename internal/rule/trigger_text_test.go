package rule

import "testing"

func TestTriggerStringSaysEveryDeclaredFieldAndAlwaysOnWhenNone(t *testing.T) {
	for _, c := range []struct {
		declared string
		want     string
	}{
		{"", "always on"},
		{"condition: (?i)changelog\nscope: library/**\n", "condition (?i)changelog; scope library/**"},
		{"role: sub-agent\ntouches: _test\\.go\nlanguage: typescript, svelte\nframework: react\ntask: review\n",
			"role sub-agent; touches _test\\.go; language typescript, svelte; framework react; task review"},
	} {
		r, err := parseRule([]byte("id: r\ndomain: dev\nkind: human\nconcern: code_rules\ntext: x\n"+c.declared), "r@1.yaml")
		if err != nil {
			t.Fatalf("parseRule %q: %v", c.declared, err)
		}
		if got := r.Trigger.String(); got != c.want {
			t.Errorf("trigger of %q reads %q, want %q", c.declared, got, c.want)
		}
	}
}
