package main

import (
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tofu/internal/rule"
	"tofu/internal/turn"
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

func TestRulesIndexRoleFiresTheRulesOfThatRole(t *testing.T) {
	library := writeFixtureModule(t, map[string]string{
		"dev/go/rules/go_ctx@1.yaml":       "id: go_ctx\ndomain: dev\nkind: human\nconcern: code_rules\nlanguage: go\ntext: pass ctx\n",
		"general/rules/lead_only@1.yaml":   "id: lead_only\ndomain: general\nkind: human\nconcern: process_discipline\nrole: orchestrator\ntext: spawn\n",
		"general/rules/sub_only@1.yaml":    "id: sub_only\ndomain: general\nkind: human\nconcern: process_discipline\nrole: sub-agent\ntext: stay in your paths\n",
		"general/rules/everyone@1.yaml":    "id: everyone\ndomain: general\nkind: human\nconcern: output_shape\ntext: be short\n",
		"qa/general/rules/qa_only@1.yaml":  "id: qa_only\ndomain: qa\nkind: human\nconcern: code_rules\ntext: split coverage\n",
		"qa/general/rules/qa_tests@1.yaml": "id: qa_tests\ndomain: qa\nalso_reaches: work_on_tests\nkind: human\nconcern: code_rules\ntext: end to end first\n",
		"dev/rules/write_small@1.yaml":     "id: write_small\ndomain: dev\nkind: human\nconcern: process_discipline\nshapes: writing\ntext: smallest diff\n",
		"dev/go/rules/go_states@1.yaml":    "id: go_states\ndomain: dev\nkind: human\nconcern: code_rules\nlanguage: go\nshapes: design\ntext: every view has four states\n",
	})
	project := t.TempDir()
	for _, c := range []struct {
		role  string
		fires map[string]bool
	}{
		{"", map[string]bool{"go_ctx": true, "everyone": true, "write_small": true, "go_states": true}},
		{"orchestrator", map[string]bool{"lead_only": true, "everyone": true, "go_states": true}},
		{"sub-agent", map[string]bool{"go_ctx": true, "sub_only": true, "everyone": true, "write_small": true, "go_states": true}},
	} {
		args := []string{"rules", "index", "fix the parser", "parser.go", "--library", library, "--dir", project}
		if c.role != "" {
			args = append(args, "--role", c.role)
		}
		code, out, errOut := runOutput(append(args, jsonFlag))
		var keys struct{ Data map[string]json.RawMessage }
		var printed struct{ Data ruleIndexReport }
		if err := errors.Join(json.Unmarshal([]byte(out), &keys), json.Unmarshal([]byte(out), &printed)); code != exitOK || err != nil {
			t.Fatalf("%v exited %d (%v):\n%s%s", c.role, code, err, out, errOut)
		}
		if _, carried := keys.Data["role"]; carried != (c.role != "") || printed.Data.Role != c.role {
			t.Errorf("%q: the JSON carries role %q (present %v)", c.role, printed.Data.Role, carried)
		}
		for _, listed := range printed.Data.Rules {
			if listed.Fires != c.fires[listed.RuleID] || strings.HasPrefix(listed.RuleID, "qa_") && !strings.Contains(listed.Why, "qa") {
				t.Errorf("%v: %s fires = %v, want %v: %s", c.role, listed.RuleID, listed.Fires, c.fires[listed.RuleID], listed.Why)
			}
		}
		code, out, errOut = runOutput(args)
		if code != exitOK || strings.Contains(out, "role") != (c.role != "") || !strings.Contains(out, c.role) {
			t.Errorf("%q: the text output exited %d and does not say the role:\n%s%s", c.role, code, out, errOut)
		}
	}
	for _, bad := range [][]string{{"--role", "nobody"}, {"--role", ""}, {"--role", "Orchestrator"}, {"--role"}} {
		code, out, errOut := runOutput(append([]string{"rules", "index", "fix the parser", "parser.go", "--library", library, "--dir", project}, bad...))
		if code != exitUsage || !strings.Contains(errOut, "orchestrator") || !strings.Contains(errOut, "sub-agent") {
			t.Errorf("%q exited %d, want %d naming orchestrator and sub-agent:\n%s%s", bad, code, exitUsage, out, errOut)
		}
	}
	misspelt := writeFixtureModule(t, map[string]string{
		"dev/rules/write_small@1.yaml": "id: write_small\ndomain: dev\nkind: human\nconcern: process_discipline\nshapes: Writing\ntext: smallest diff\n",
	})
	code, out, errOut := runOutput([]string{"rules", "index", "fix the parser", "parser.go", "--library", misspelt, "--dir", project, "--role", "orchestrator"})
	if code == exitOK || !strings.Contains(out+errOut, `"Writing"`) || !strings.Contains(out+errOut, "write_small@1.yaml") {
		t.Errorf("a misspelt shape exited %d, want a failure naming the file and the value:\n%s%s", code, out, errOut)
	}
}

func TestRulesIndexFiresWhatTheComposerSendsForEveryRole(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	library := filepath.Join("..", "..", "library")
	rules, err := rule.LoadDir(library)
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	for _, task := range []string{"fix the parser", "add tests for the parser"} {
		for _, role := range []rule.Role{rule.RoleAny, rule.RoleOrchestrator, rule.RoleSubAgent} {
			composed, err := turn.Compose(turn.ComposeSpec{Task: task, Paths: []string{"parser.go"}, Environment: "e", ToolGuidance: "g", Rules: rules, Role: role})
			if err != nil {
				t.Fatal(err)
			}
			var sent, listed []string
			for _, part := range composed.Parts {
				if part.RuleID != "" {
					sent = append(sent, part.RuleID)
				}
			}
			args := []string{"rules", "index", task, "parser.go", "--library", library, "--dir", project, "--task", string(composed.Task.Verb), jsonFlag}
			if role != rule.RoleAny {
				args = append(args, "--role", string(role))
			}
			code, out, errOut := runOutput(args)
			var printed struct{ Data ruleIndexReport }
			if err := json.Unmarshal([]byte(out), &printed); code != exitOK || err != nil {
				t.Fatalf("%q %q exited %d (%v):\n%s%s", task, role, code, err, out, errOut)
			}
			for _, r := range printed.Data.Rules {
				if r.Fires {
					listed = append(listed, r.RuleID)
				}
			}
			slices.Sort(sent)
			slices.Sort(listed)
			if !slices.Equal(sent, listed) {
				t.Errorf("%q as %q: the composer sends %v\nand the index fires %v", task, role, sent, listed)
			}
		}
	}
}

func vueAndReactProjects(t *testing.T) (string, string) {
	vue, react := t.TempDir(), t.TempDir()
	for dir, body := range map[string]string{vue: `{"dependencies": {"vue": "3"}}`, react: `{"dependencies": {"react": "19"}}`} {
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return vue, react
}

func TestRulesIndexFiresATouchesRuleForASubAgentOnlyWhenItsTaskOrPathsNameIt(t *testing.T) {
	library := writeFixtureModule(t, map[string]string{
		"dev/vue/rules/vue_guard@1.yaml": "id: vue_guard\ndomain: dev\nkind: human\nconcern: code_rules\nshapes: design\nframework: vue\ntouches: (?i)router|guard\ntext: guards return\n",
	})
	vue, react := vueAndReactProjects(t)
	for _, c := range []struct {
		role, task, dir string
		paths           []string
		fires           bool
	}{
		{"sub-agent", "make delete undoable", vue, nil, false},
		{"sub-agent", "add a Router guard", vue, nil, true},
		{"sub-agent", "fix it", vue, []string{"src/router/index.ts"}, true},
		{"orchestrator", "make delete undoable", vue, nil, true},
		{"", "make delete undoable", vue, nil, true},
		{"sub-agent", "add a router guard", react, nil, false},
	} {
		args := append(append([]string{"rules", "index", c.task}, c.paths...), "--library", library, "--dir", c.dir, jsonFlag)
		if c.role != "" {
			args = append(args, "--role", c.role)
		}
		code, out, errOut := runOutput(args)
		var printed struct{ Data ruleIndexReport }
		if err := json.Unmarshal([]byte(out), &printed); code != exitOK || err != nil || len(printed.Data.Rules) != 1 {
			t.Fatalf("%q %q exited %d (%v):\n%s%s", c.role, c.task, code, err, out, errOut)
		}
		if listed := printed.Data.Rules[0]; listed.Fires != c.fires {
			t.Errorf("%q %q %v: vue_guard fires = %v, want %v: %s", c.role, c.task, c.paths, listed.Fires, c.fires, listed.Why)
		}
	}
	for name, body := range map[string]string{
		"dev/rules/broken@1.yaml": "id: broken\ndomain: dev\nkind: human\nconcern: code_rules\ntouches: (\ntext: x\n",
		"dev/rules/shape@1.yaml":  "id: shape\ndomain: dev\nkind: human\nconcern: output_shape\ntouches: x\ntext: x\n",
	} {
		code, out, errOut := runOutput([]string{"rules", "index", "x", "--library", writeFixtureModule(t, map[string]string{name: body}), "--dir", vue})
		if code == exitOK || !strings.Contains(out+errOut, path.Base(name)) {
			t.Errorf("%s exited %d, want a failure naming it:\n%s%s", name, code, out, errOut)
		}
	}
}

func TestRulesIndexKeepsTheShippedFrontendRulesASubAgentTaskNames(t *testing.T) {
	vue, react := vueAndReactProjects(t)
	for _, c := range []struct {
		task, dir string
		fires     map[string]bool
	}{
		{"Deleting an item takes one click, and people lose items by mistake. Fix that. Add tests and run them.", vue,
			map[string]bool{"fe_consequential_actions": true, "fe_motion_timing": false, "fe_dialog": false, "vue_router_guards": false, "vue_ssr_state": false}},
		{"Add a Save button above the list that sends the current items to `store.save` from src/store.ts, and tell the person how it went.", react,
			map[string]bool{"fe_control_states": true, "fe_consequential_actions": true, "react_hydration": false, "fe_reduced_motion": false}},
		{"Ask before leaving the settings page with unsaved changes: open a dialog from a router guard.", vue,
			map[string]bool{"fe_dialog": true, "vue_router_guards": true, "fe_form_validation": true, "vue_ssr_state": false}},
	} {
		code, out, errOut := runOutput([]string{"rules", "index", c.task, "--role", "sub-agent", "--library", filepath.Join("..", "..", "library"), "--dir", c.dir, jsonFlag})
		var printed struct{ Data ruleIndexReport }
		if err := json.Unmarshal([]byte(out), &printed); code != exitOK || err != nil {
			t.Fatalf("%q exited %d (%v):\n%s%s", c.task, code, err, out, errOut)
		}
		for _, listed := range printed.Data.Rules {
			if want, named := c.fires[listed.RuleID]; named && listed.Fires != want {
				t.Errorf("%q: %s fires = %v, want %v: %s", c.task, listed.RuleID, listed.Fires, want, listed.Why)
			}
		}
	}
}
