package turn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"tofu/internal/konst"
	"tofu/internal/rule"
	"tofu/internal/subagent"
	shipped "tofu/library"
)

func spec(rules ...rule.Rule) ComposeSpec {
	return ComposeSpec{
		Task:         "rename one symbol",
		Environment:  "<env>\nworking directory: /w\n</env>",
		ToolGuidance: "read reads a whole file",
		Rules:        rules,
	}
}

func firingRules() []rule.Rule {
	return []rule.Rule{
		{ID: "no_worktree", Concern: rule.ConcernSafety, Notes: "never git worktree", File: "library/dev/rules/no_worktree@1.yaml"},
		{ID: "em_dash", Concern: rule.ConcernOutputShape, Notes: "never a literal U+2014", File: "library/general/rules/em_dash@1.yaml"},
	}
}

func TestARuleThatFiresAndCarriesNoTextFailsTheComposition(t *testing.T) {
	hollow := rule.Rule{ID: "hollow", Concern: rule.ConcernCodeRules, File: "library/dev/rules/hollow@1.yaml"}
	_, err := Compose(spec(append(firingRules(), hollow)...))
	if err == nil {
		t.Fatal("a rule that fires and carries no text was dropped from the prompt in silence")
	}
	for _, wanted := range []string{"hollow", "always on", "library/dev/rules/hollow@1.yaml"} {
		if !strings.Contains(err.Error(), wanted) {
			t.Fatalf("the failure does not name %q: %v", wanted, err)
		}
	}
}

func TestADroppedAlwaysOnConcernFailsTheComposition(t *testing.T) {
	for _, one := range []struct {
		name    string
		spec    ComposeSpec
		concern rule.Concern
	}{
		{name: "no environment", spec: ComposeSpec{Task: "t", ToolGuidance: "g", Rules: firingRules()}, concern: rule.ConcernEnvironment},
		{name: "no tool guidance", spec: ComposeSpec{Task: "t", Environment: "e", Rules: firingRules()}, concern: rule.ConcernToolGuidance},
	} {
		t.Run(one.name, func(t *testing.T) {
			_, err := Compose(one.spec)
			if err == nil {
				t.Fatalf("composing dropped %s and said nothing", one.concern)
			}
			if !strings.Contains(err.Error(), string(one.concern)) {
				t.Fatalf("the failure does not name %s: %v", one.concern, err)
			}
		})
	}
}

func TestARuleWithTextSendsItsTextRatherThanItsNotes(t *testing.T) {
	human := rule.Rule{
		ID:      "quote",
		Concern: rule.ConcernTaskShaping,
		Text:    "cite the turn rather than paraphrase it",
		Notes:   "moved here by another ticket",
		File:    "library/general/rules/quote@1.yaml",
	}
	composed, err := Compose(spec(append(firingRules(), human)...))
	if err != nil {
		t.Fatal(err)
	}
	system := composed.System()
	if !strings.Contains(system, human.Text) {
		t.Fatalf("the composed prompt does not carry the text of the rule %s:\n%s", human.ID, system)
	}
	if strings.Contains(system, human.Notes) {
		t.Fatalf("the composed prompt sent what a maintainer reads rather than what the model reads:\n%s", system)
	}
}

func TestTheComposedSystemPromptNamesTheRuleEachPartCameFrom(t *testing.T) {
	composed, err := Compose(spec(firingRules()...))
	if err != nil {
		t.Fatal(err)
	}
	system := composed.System()
	for _, wanted := range []string{
		"[safety, from the rule no_worktree]",
		"never git worktree",
		"[output_shape, from the rule em_dash]",
		"[tool_guidance, from tofu itself]",
		"[format_contract, from tofu itself]",
	} {
		if !strings.Contains(system, wanted) {
			t.Fatalf("the composed system prompt does not carry %q:\n%s", wanted, system)
		}
	}
	if strings.Contains(system, "working directory: /w") {
		t.Fatalf("the environment reached the cached system prompt, where a per turn value does not belong:\n%s", system)
	}
}

func TestTheComposedSystemPromptOverTheRealLibraryCarriesNoHostPath(t *testing.T) {
	library, err := filepath.Abs(filepath.Join("..", "..", "library"))
	if err != nil {
		t.Fatal(err)
	}
	rules, err := rule.LoadDir(library)
	if err != nil {
		t.Fatal(err)
	}
	composed, err := Compose(ComposeSpec{
		Task:         "write a test in internal/turn/compose.go",
		Environment:  "<env>\nworking directory: /w\n</env>",
		ToolGuidance: "read reads a whole file",
		Rules:        rules,
	})
	if err != nil {
		t.Fatal(err)
	}
	system := composed.System()
	root := filepath.ToSlash(filepath.Dir(library))
	for _, spelling := range []string{root, filepath.Dir(library)} {
		if strings.Contains(system, spelling) {
			t.Fatalf("the composed system prompt sends the host path %s to a provider:\n%s", spelling, system)
		}
	}
	fired := 0
	for _, part := range composed.Parts {
		if part.RuleID == "" {
			continue
		}
		fired++
		if !strings.Contains(system, "from the rule "+part.RuleID+"]") {
			t.Fatalf("the composed system prompt does not name the rule %s:\n%s", part.RuleID, system)
		}
		if !strings.Contains(part.From(), part.File) {
			t.Fatalf("--show-prompt lost the file of the rule %s, which reads %s", part.RuleID, part.From())
		}
	}
	if fired == 0 {
		t.Fatalf("no rule in the real library at %s fired, so the prompt was never at risk of carrying a path", library)
	}
}

func TestToolPickReachesTheLeadAndASubAgentInTheCachedHeadUnderToolGuidance(t *testing.T) {
	rules, err := rule.LoadFS(shipped.Files(), "library")
	if err != nil {
		t.Fatal(err)
	}
	for name, one := range map[string]ComposeSpec{
		"lead":      {Role: rule.RoleOrchestrator},
		"sub-agent": {Agent: subagent.Definition{Name: "go-dev", Origin: "library", Domain: rule.DomainDev, Language: "go", Instructions: "write go"}},
	} {
		one.Task, one.Environment, one.ToolGuidance, one.Rules = "fix the failing test", "<env/>", "every path is relative", rules
		composed, err := Compose(one)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		head := composed.Head()
		at := strings.Index(head, "[tool_guidance, from the rule tool_pick]\n")
		if at < 0 || !strings.Contains(head[at:], "sed -i") {
			t.Fatalf("the %s head does not carry tool_pick under tool_guidance:\n%s", name, head)
		}
		if builtin, safety := strings.Index(head, "[tool_guidance, from tofu itself]"), strings.Index(head, "[safety,"); builtin > at || (safety >= 0 && safety < at) {
			t.Fatalf("the %s head moved tool_pick out of the tool_guidance place:\n%s", name, head)
		}
	}
	for _, layered := range []struct {
		name, absent, present string
		over                  rule.Rule
	}{
		{"replaced", "sed -i", "use the project's own tools", rule.Rule{ID: "tool_pick", Kind: rule.KindHuman, Concern: rule.ConcernToolGuidance, Text: "use the project's own tools"}},
		{"off", "from the rule tool_pick", "from tofu itself", rule.Rule{ID: "tool_pick", Mode: rule.ModeOff}},
	} {
		composed, err := Compose(ComposeSpec{Task: "t", Environment: "e", ToolGuidance: "g", Rules: rule.Layer(rules, []rule.Rule{layered.over})})
		if err != nil {
			t.Fatal(err)
		}
		if head := composed.Head(); strings.Contains(head, layered.absent) || !strings.Contains(head, layered.present) {
			t.Fatalf("a project tool_pick %s did not change the head:\n%s", layered.name, head)
		}
	}
}

func TestAScopedRuleReachesTheSubAgentOwningItsTreeAndAFrameworkRuleSitsInTheHead(t *testing.T) {
	rules, err := rule.LoadFS(fstest.MapFS{
		"src_only@1.yaml":    {Data: []byte("id: src_only\ndomain: dev\nkind: human\nconcern: code_rules\nscope: src/**\ntext: src rule\n")},
		"react_tests@1.yaml": {Data: []byte("id: react_tests\ndomain: dev\nkind: human\nconcern: code_rules\nframework: react\ntext: react rule\n")},
	}, "library")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		owns, frameworks []string
		src, react       bool
	}{
		{[]string{"src/**"}, []string{"react"}, true, true},
		{[]string{"docs/**"}, []string{"svelte"}, false, false},
	} {
		composed, err := Compose(ComposeSpec{Task: "write the page", Paths: c.owns, Frameworks: c.frameworks, Environment: "e", ToolGuidance: "g", Rules: rules, Role: rule.RoleSubAgent})
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(composed.System(), "src rule"); got != c.src {
			t.Errorf("owning %v: the scoped rule fires = %v, want %v", c.owns, got, c.src)
		}
		if got := strings.Contains(composed.Head(), "react rule"); got != c.react {
			t.Errorf("frameworks %v: the react rule in the head = %v, want %v", c.frameworks, got, c.react)
		}
	}
}

func TestASubAgentOwningAMonorepoAppGetsThatAppsFrameworkRules(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"package.json":           `{"workspaces": ["apps/*"]}`,
		"apps/web/package.json":  `{"dependencies": {"react": "19"}}`,
		"apps/docs/package.json": `{"dependencies": {"svelte": "5"}}`,
	} {
		at := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(at, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rules, err := rule.LoadFS(fstest.MapFS{
		"react_tests@1.yaml": {Data: []byte("id: react_tests\ndomain: dev\nkind: human\nconcern: code_rules\nframework: react\ntext: react rule\n")},
		"vue_tests@1.yaml":   {Data: []byte("id: vue_tests\ndomain: dev\nkind: human\nconcern: code_rules\nframework: vue\ntext: vue rule\n")},
	}, "library")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		owns           []string
		lead           []string
		react, vueRule bool
	}{
		{[]string{"apps/web/**"}, nil, true, false},
		{[]string{"apps/docs/**"}, nil, false, false},
		{[]string{"apps/docs/**"}, []string{"vue"}, false, true},
	} {
		agents := SubAgents{Root: root, Prompt: ComposeSpec{Environment: "e", ToolGuidance: "g", Rules: rules, Frameworks: c.lead}}
		system, _, err := agents.prompt(Config{}, subagent.Definition{}, "write the page", c.owns)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(system, "react rule"); got != c.react {
			t.Errorf("owning %v: react rule = %v, want %v", c.owns, got, c.react)
		}
		if got := strings.Contains(system, "vue rule"); got != c.vueRule {
			t.Errorf("owning %v with the lead's %v: vue rule = %v, want %v", c.owns, c.lead, got, c.vueRule)
		}
	}
}

func TestEveryLibraryAgentGetsEachSkillItNamesWhole(t *testing.T) {
	found := subagent.Definitions(subagent.Scan{Library: shipped.Files()})
	if len(found.Broken) > 0 {
		t.Fatalf("library agents do not read: %+v", found.Broken)
	}
	named := 0
	for _, definition := range found.Definitions {
		composed, err := Compose(ComposeSpec{Task: "t", Environment: "e", ToolGuidance: "g", Agent: definition})
		if err != nil {
			t.Fatalf("%s: %v", definition.Path, err)
		}
		onDisk := filepath.Join("..", "..", "library")
		for _, name := range definition.Skills {
			named++
			files, _ := filepath.Glob(filepath.Join(onDisk, "*", "skills", name+".md"))
			deeper, _ := filepath.Glob(filepath.Join(onDisk, "*", "*", "skills", name+".md"))
			if files = append(files, deeper...); len(files) != 1 {
				t.Fatalf("%s names the skill %s, which the library holds %d times on disk", definition.Path, name, len(files))
			}
			data, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			at := strings.Index(string(data), "\n# ")
			heading, _, _ := strings.Cut(string(data)[at+1:], "\n")
			if wanted := "the skill " + name + ", loaded before your task:\n" + strings.TrimSpace(heading); at < 0 || !strings.Contains(composed.System(), wanted) {
				t.Fatalf("%s names the skill %s and its prompt lacks %q:\n%s", definition.Path, name, wanted, composed.System())
			}
		}
	}
	if named == 0 {
		t.Fatal("no library agent names a skill, so nothing here was at risk")
	}
}

func TestALibraryAgentsSkillsFailLoudly(t *testing.T) {
	library := fstest.MapFS{
		"qa/skills/one.md":          {Data: []byte("---\r\nname: one\r\ndomain: qa\r\n---\r\n# One\r\nfirst\r\n")},
		"qa/general/skills/two.md":  {Data: []byte("---\nname: two\n---\n\n# Two\n")},
		"dev/skills/twin.md":        {Data: []byte("---\nname: twin\n---\n# Twin\n")},
		"qa/general/skills/twin.md": {Data: []byte("---\nname: twin\n---\n# Twin\n")},
		"qa/skills/huge.md":         {Data: []byte("---\nname: huge\n---\n" + strings.Repeat("x", konst.SubAgentReferenceBytes))},
	}
	for _, c := range []struct {
		name   string
		skills []string
		loads  []string
		fails  string
	}{
		{name: "crlf at two depths", skills: []string{"one", "two"}, loads: []string{"one, loaded before your task:\n# One\n", "two, loaded before your task:\n# Two"}},
		{name: "missing", skills: []string{"ghost"}, fails: "ghost"},
		{name: "shipped twice", skills: []string{"twin"}, fails: "twin"},
		{name: "over the limit", skills: []string{"huge"}, fails: "16384"},
		{name: "none named"},
	} {
		t.Run(c.name, func(t *testing.T) {
			text, err := librarySkills(library, subagent.Definition{Name: "x", Origin: "library", Skills: c.skills})
			if c.fails != "" {
				if err == nil || !strings.Contains(err.Error(), c.fails) {
					t.Fatalf("want a failure naming %q, got %v with %q", c.fails, err, text)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, wanted := range c.loads {
				if !strings.Contains(text, wanted) || strings.Contains(text, "name:") {
					t.Fatalf("want %q and no front matter in:\n%q", wanted, text)
				}
			}
			if len(c.loads) == 0 && text != "" {
				t.Fatalf("a definition naming no skill loaded %q", text)
			}
		})
	}
}

func TestTheTaskNamesItsPathsAndItsVerb(t *testing.T) {
	task := TaskNamed("debug the failing test in internal/turn/loop_test.go and in README.md, not a.b")
	if want := []string{"internal/turn/loop_test.go", "README.md"}; len(task.Paths) != len(want) {
		t.Fatalf("expected the paths %v, got %v", want, task.Paths)
	}
	for i, want := range []string{"internal/turn/loop_test.go", "README.md"} {
		if task.Paths[i] != want {
			t.Fatalf("expected path %d to be %s, got %s", i, want, task.Paths[i])
		}
	}
	if task.Verb != rule.VerbDebug {
		t.Fatalf("expected the verb %s, got %q", rule.VerbDebug, task.Verb)
	}
	if plain := TaskNamed("rename one symbol"); len(plain.Paths) != 0 || plain.Verb != rule.VerbNone {
		t.Fatalf("a task naming no path and no verb read as paths %v and verb %q", plain.Paths, plain.Verb)
	}
}
