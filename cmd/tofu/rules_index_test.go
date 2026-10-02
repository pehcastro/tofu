package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRulesIndexFiresAFrameworkRuleOnlyInItsOwnProject(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"lib/dev/rules/react_tests@1.yaml":  "id: react_tests\ndomain: dev\nkind: human\nconcern: code_rules\nframework: react\ntext: test react\n",
		"lib/dev/rules/svelte_tests@1.yaml": "id: svelte_tests\ndomain: dev\nkind: human\nconcern: code_rules\nframework: svelte\ntext: test svelte\n",
		"react/package.json":                `{"dependencies": {"react": "19"}}`,
		"svelte/package.json":               `{"devDependencies": {"svelte": "5"}}`,
		"mono/package.json":                 `{"workspaces": ["apps/*"]}`,
		"mono/apps/web/package.json":        `{"dependencies": {"react": "19"}}`,
		"broken/package.json":               `{"dependencies": `,
	}
	for name, body := range files {
		at := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(at, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	library := filepath.Join(root, "lib")
	for _, c := range []struct {
		dir   string
		paths []string
		fires map[string]bool
	}{
		{"react", nil, map[string]bool{"react_tests": true, "svelte_tests": false}},
		{"svelte", nil, map[string]bool{"react_tests": false, "svelte_tests": true}},
		{"mono", []string{"apps/web/src/App.tsx"}, map[string]bool{"react_tests": true, "svelte_tests": false}},
		{"mono", nil, map[string]bool{"react_tests": false, "svelte_tests": false}},
	} {
		args := append(append([]string{"rules", "index", "write the page"}, c.paths...), "--library", library, "--dir", filepath.Join(root, c.dir), jsonFlag)
		code, out, errOut := runOutput(args)
		var printed struct{ Data ruleIndexReport }
		if err := json.Unmarshal([]byte(out), &printed); code != exitOK || err != nil {
			t.Fatalf("%s %v exited %d (%v):\n%s%s", c.dir, c.paths, code, err, out, errOut)
		}
		for _, listed := range printed.Data.Rules {
			if listed.Fires != c.fires[listed.RuleID] {
				t.Errorf("%s %v: %s fires = %v, want %v: %s", c.dir, c.paths, listed.RuleID, listed.Fires, c.fires[listed.RuleID], listed.Why)
			}
		}
	}
	code, out, errOut := runOutput([]string{"rules", "index", "write the page", "--library", library, "--dir", filepath.Join(root, "broken")})
	if code == exitOK || !strings.Contains(out+errOut, "package.json") {
		t.Fatalf("a broken package.json exited %d, want a failure naming it:\n%s%s", code, out, errOut)
	}
}
