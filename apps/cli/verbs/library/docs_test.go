package library

import (
	"bytes"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"tofu/apps/cli/output"
	"tofu/internal/project/guard"
	"tofu/library/docs"
)

func TestDocsEveryPageKeepsTheContract(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	corpus, err := loadDocs()
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{"models", "skills", "instructions", "gate", "sessions", "doctor"} {
		if !slices.Contains(topics(corpus), wanted) {
			t.Errorf("no page has the topic %s", wanted)
		}
	}
	for _, page := range corpus.Pages {
		var out, errOut bytes.Buffer
		if code := DocsVerb([]string{page.Topic}, &out, &errOut); code != output.ExitOK {
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
		for _, section := range guard.DocsSections() {
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
		for _, broken := range guard.DocsBroken(page.Topic+".md", string(source)) {
			t.Error(broken)
		}
		t.Logf("tofu docs %s: %d bytes, %s", page.Topic, len(printed), strings.SplitN(printed, "\n", 2)[0])
	}
}
