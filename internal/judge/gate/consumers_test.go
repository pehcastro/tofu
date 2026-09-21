package gate

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"tofu/internal/sys"
)

var knownForeignSchemaConsumers = map[string][]string{
	"stop_check@1": {
		"internal/judge/state/stop_check_rule.go",
	},
	"shell_sift@1": {
		"bench/sift/arm.go",
	},
	"page_sift@1": {
		"bench/websift/arm.go",
	},
}

var pointRefLiteral = regexp.MustCompile(`"([a-z][a-z_]*@\d+)"`)

func TestEveryConsumerOfAForeignSchemaRuleIsAccountedFor(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	found := map[string][]string{}
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == sys.StateDirName || info.Name() == ".local" || info.Name() == sys.LegacyStateDirName || info.Name() == ".claude" || info.Name() == ".playground" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := relFromRoot(t, root, path)
		if rel == "internal/judge/gate/consumers_test.go" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, ref := range pointRefLiteral.FindAllStringSubmatch(string(body), -1) {
			if _, known := knownForeignSchemaConsumers[ref[1]]; known {
				found[ref[1]] = append(found[ref[1]], rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	for ref, want := range knownForeignSchemaConsumers {
		got := dedupSorted(found[ref])
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s is referenced by %v, want exactly %v; a new file naming this point must load it through the one function that assembles its foreign schema, then be added here", ref, got, want)
		}
	}
}

func dedupSorted(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func relFromRoot(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(rel)
}
