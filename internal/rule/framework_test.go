package rule

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func project(t *testing.T, files map[string]string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "project")
	for name, body := range files {
		at := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(at, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func frameworkRule(t *testing.T, framework string) Rule {
	t.Helper()
	r, err := parseRule([]byte("id: framework_tests\ndomain: dev\nkind: human\nconcern: code_rules\nframework: "+framework+"\ntext: test it\n"), "framework_tests@1.yaml")
	if err != nil {
		t.Fatalf("parseRule: %v", err)
	}
	return r
}

func TestAFrameworkRuleFiresOnlyInAProjectWhosePackageJSONListsIt(t *testing.T) {
	packages := map[string]string{
		"react":  `{"dependencies": {"react": "^19.0.0", "react-dom": "^19.0.0"}}`,
		"svelte": `{"devDependencies": {"svelte": "^5.0.0"}}`,
		"vue":    `{"peerDependencies": {"vue": "^3.5.0"}}`,
	}
	for listed, body := range packages {
		frameworks, err := Frameworks(project(t, map[string]string{"package.json": body}), nil)
		if err != nil {
			t.Fatalf("Frameworks for %s: %v", listed, err)
		}
		for declared := range packages {
			fires, why := frameworkRule(t, declared).Trigger.firesFor(Task{Frameworks: frameworks})
			if fires != (declared == listed) {
				t.Errorf("framework: %s in a %s project: fires = %v: %s", declared, listed, fires, why)
			}
		}
	}
}

func TestAMonorepoAppPackageNamesTheFrameworkForATaskInThatApp(t *testing.T) {
	root := project(t, map[string]string{
		"package.json":           `{"workspaces": ["apps/*"]}`,
		"apps/web/package.json":  `{"dependencies": {"react": "19"}}`,
		"apps/docs/package.json": `{"dependencies": {"svelte": "5"}}`,
	})
	react := frameworkRule(t, "react")
	cases := []struct {
		paths []string
		fires bool
	}{
		{[]string{"apps/web/src/App.tsx"}, true},
		{[]string{"apps/web/**"}, true},
		{[]string{"apps/docs/src/page.svelte"}, false},
		{nil, false},
	}
	for _, c := range cases {
		frameworks, err := Frameworks(root, c.paths)
		if err != nil {
			t.Fatalf("Frameworks %v: %v", c.paths, err)
		}
		if fires, why := react.Trigger.firesFor(Task{Paths: c.paths, Frameworks: frameworks}); fires != c.fires {
			t.Errorf("paths %v read %v: fires = %v, want %v: %s", c.paths, frameworks, fires, c.fires, why)
		}
	}
}

func TestAPackageJSONAboveTheProjectRootIsNotRead(t *testing.T) {
	root := project(t, map[string]string{"../package.json": `{"dependencies": {"react": "19"}}`, "src/main.go": "package main"})
	frameworks, err := Frameworks(root, []string{"src/main.go"})
	if err != nil || len(frameworks) != 0 {
		t.Fatalf("Frameworks = %v, %v, want none: the react package.json sits above the project", frameworks, err)
	}
}

func TestAPackageJSONThatIsNotJSONFailsByPath(t *testing.T) {
	_, err := Frameworks(project(t, map[string]string{"package.json": `{"dependencies": `}), nil)
	if err == nil || !strings.Contains(err.Error(), "package.json") {
		t.Fatalf("err = %v, want a failure naming package.json", err)
	}
}

func TestAnUnknownFrameworkIsRefusedAtLoadAloneOrInAList(t *testing.T) {
	for _, declared := range []string{"ember", "react, ember", "react,"} {
		_, err := parseRule([]byte("id: ember\ndomain: dev\nkind: human\nconcern: code_rules\nframework: "+declared+"\ntext: x\n"), "ember@1.yaml")
		if err == nil || !strings.Contains(err.Error(), "ember") || !strings.Contains(err.Error(), "framework") {
			t.Errorf("framework: %s: err = %v, want a refusal naming the rule and the field", declared, err)
		}
	}
}

func TestAFrameworkListFiresInAnyListedProjectAndNotInANodeBackend(t *testing.T) {
	frontend := frameworkRule(t, "react, svelte, vue")
	for body, want := range map[string]string{
		`{"dependencies": {"react": "19"}}`:    "a package.json lists react",
		`{"devDependencies": {"svelte": "5"}}`: "a package.json lists svelte",
		`{"dependencies": {"vue": "3"}}`:       "a package.json lists vue",
		`{"dependencies": {"express": "4"}}`:   "",
	} {
		frameworks, err := Frameworks(project(t, map[string]string{"package.json": body}), nil)
		if err != nil {
			t.Fatalf("Frameworks for %s: %v", body, err)
		}
		fires, why := frontend.Trigger.firesFor(Task{Paths: []string{"server/db.ts"}, Frameworks: frameworks})
		if fires != (want != "") || (fires && why != want) {
			t.Errorf("%s: fires = %v, why %q, want %q", body, fires, why, want)
		}
	}
}

func TestEveryFrameworkAndTheMetaFrameworksThatImplyOneAreDetected(t *testing.T) {
	for dependency, want := range map[string]string{
		"solid-js":      "solid",
		"@angular/core": "angular",
		"astro":         "astro",
		"preact":        "preact",
		"next":          "react",
		"@sveltejs/kit": "svelte",
		"nuxt":          "vue",
	} {
		frameworks, err := Frameworks(project(t, map[string]string{"package.json": `{"dependencies": {"` + dependency + `": "1"}}`}), nil)
		if err != nil || len(frameworks) != 1 || frameworks[0] != want {
			t.Errorf("a package.json listing %s reads %v, %v, want [%s]", dependency, frameworks, err, want)
		}
	}
	both, err := Frameworks(project(t, map[string]string{"package.json": `{"dependencies": {"next": "15", "react": "19"}}`}), nil)
	if err != nil || len(both) != 1 || both[0] != "react" {
		t.Errorf("next and react together read %v, %v, want [react] once", both, err)
	}
}

func TestSvelteAndVueFilesHaveTheirOwnLanguage(t *testing.T) {
	for file, want := range map[string]string{"App.svelte": "svelte", "App.vue": "vue"} {
		if got := LanguageOf(file); got != want {
			t.Errorf("LanguageOf(%s) = %q, want %q", file, got, want)
		}
	}
}
