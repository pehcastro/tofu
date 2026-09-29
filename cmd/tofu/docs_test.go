package main

import (
	"bytes"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"tofu/internal/settings"
	"tofu/library/docs"
)

func TestDocsSettingsNamesEveryDeclaredKey(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var out, errOut bytes.Buffer
	if code := run([]string{"docs", "settings"}, strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatalf("tofu docs settings exited %d: %s", code, errOut.String())
	}
	rows := map[string]bool{}
	for _, line := range strings.Split(out.String(), "\n") {
		if fields := strings.Fields(line); len(fields) > 1 {
			rows[fields[0]] = true
		}
	}
	for _, spec := range settings.Default() {
		if !rows[spec.Key] {
			t.Errorf("tofu docs settings has no table row for %s", spec.Key)
		}
	}
}

func TestDocsReportsARoutedVerbNoPageNames(t *testing.T) {
	listed := usageVerbs()
	for _, verb := range routedVerbs(t) {
		if strings.Contains(usage, "\n  "+verb+" ") && !slices.Contains(listed, verb) {
			t.Errorf("tofu routes %q and the docs check never asks for its page", verb)
		}
	}
	_, missing := docsCoverage(append(listed, "frobnicate"))
	if len(missing) != 1 || !strings.Contains(missing[0].Error(), "the verb frobnicate") {
		t.Fatalf("a routed verb no page names produced %v, want one line naming frobnicate", missing)
	}
	t.Logf("refused: %v", missing[0])
}

func TestDocsEveryPageKeepsTheContract(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	sections := []string{"What it is", "Where it lives", "Change it", "Check it", "Undo it"}
	corpus, err := loadDocs()
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{"models", "skills", "instructions", "gate", "sessions", "doctor"} {
		if !slices.Contains(corpus.topics(), wanted) {
			t.Errorf("no page has the topic %s", wanted)
		}
	}
	for _, page := range corpus.Pages {
		var out, errOut bytes.Buffer
		if code := run([]string{"docs", page.Topic}, strings.NewReader(""), &out, &errOut); code != exitOK {
			t.Errorf("tofu docs %s exited %d: %s", page.Topic, code, errOut.String())
			continue
		}
		printed := out.String()
		if !strings.HasPrefix(printed, page.Title+"\n") {
			t.Errorf("tofu docs %s does not open with its title %q", page.Topic, page.Title)
		}
		if strings.Contains(printed, "\n## ") {
			t.Errorf("tofu docs %s prints a raw markdown heading", page.Topic)
		}
		at := 0
		for _, section := range sections {
			found := strings.Index(printed[at:], "\n"+section+"\n")
			if found < 0 {
				t.Errorf("tofu docs %s has no %q after the sections before it", page.Topic, section)
				break
			}
			at += found + 1
		}
		source, err := fs.ReadFile(docs.Files(), page.Topic+".md")
		if err != nil {
			t.Errorf("the page for %s is not %s.md: %v", page.Topic, page.Topic, err)
			continue
		}
		if lines := strings.Count(string(source), "\n"); lines < 60 || lines > 120 {
			t.Errorf("%s.md runs %d lines, and a page runs 60 to 120", page.Topic, lines)
		}
		if strings.Contains(printed, "TOFU-") {
			t.Errorf("tofu docs %s names a ticket", page.Topic)
		}
		t.Logf("tofu docs %s: %d bytes, %s", page.Topic, len(printed), strings.SplitN(printed, "\n", 2)[0])
	}
}
