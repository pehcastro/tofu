package boardy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func planted(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMoveToDoingNamesACallerByItsPackage(t *testing.T) {
	m := managedBoard(t, "DEMO")
	m.Store.Root = t.TempDir()
	planted(t, m.Store.Root, map[string]string{
		"go.mod":            "module demo\n",
		"pkg/pkg.go":        "package pkg\n\ntype Cache struct{}\n\nfunc Open() *Cache { return &Cache{} }\n\nfunc (c *Cache) Compact() {}\n\nfunc Prune() {}\n",
		"pkg/trim.go":       "package pkg\n\nimport (\n\t\"slices\"\n\n\t\"demo/other\"\n)\n\nfunc trim(xs []int) []int {\n\tother.Prune()\n\treturn slices.Compact(xs)\n}\n",
		"app/use.go":        "package app\n\nimport p \"demo/pkg\"\n\nfunc run() {\n\tp.Open().Compact()\n\tp.Prune()\n}\n",
		"app/json.go":       "package app\n\nimport (\n\t\"bytes\"\n\t\"encoding/json\"\n\ts \"slices\"\n)\n\nfunc f(b *bytes.Buffer, raw []byte) error {\n\t_ = s.Compact([]int{})\n\treturn json.Compact(b, raw)\n}\n",
		"app/words.go":      "package app\n\nvar said = \"x.Compact() and Prune()\"\n",
		"broken/broken.go":  "package broken\n\nfunc f( {\n\tx.Compact()\n}\n",
		"other/other.go":    "package other\n\nfunc Prune() {}\n",
		"pkg/pkg_x_test.go": "package pkg_test\n\nimport \"demo/pkg\"\n\nfunc g() { pkg.Prune() }\n",
	})
	gitIn(t, m.Store.Root, "init", "-q")
	made := ticketWith("trim Cache.Compact and Prune", "pkg/pkg.go")
	made.Scope, made.Status = "change method Compact and func Prune", Review
	ticket, err := m.Local.Create("DEMO", made, "test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Move(ticket.ID, Doing, "", manager)
	refused(t, err, "app/use.go:6: p.Open().Compact()")
	refused(t, err, "app/use.go:7: p.Prune()")
	refused(t, err, "pkg/pkg_x_test.go:5")
	refused(t, err, "broken/broken.go:4: x.Compact(), matched by name: the file does not parse")
	refused(t, err, "matched by name: the receiver type is not resolved")
	for _, quiet := range []string{"pkg/trim.go", "app/json.go", "app/words.go", "other/other.go"} {
		if strings.Contains(err.Error(), quiet) {
			t.Errorf("the refusal lists %s:\n%v", quiet, err)
		}
	}
}

func TestImpactNamesACallerByItsPackage(t *testing.T) {
	root := t.TempDir()
	planted(t, root, map[string]string{
		"go.mod":           "module demo\n",
		"pkg/pkg.go":       "package pkg\n\ntype Cache struct{}\n\nfunc (c *Cache) Compact() {}\n\nfunc Compact(c *Cache) {}\n",
		"app/use.go":       "package app\n\nimport cache \"demo/pkg\"\n\nfunc run(c *cache.Cache) {\n\tcache.Compact(c)\n\tc.Compact()\n}\n",
		"app/trim.go":      "package app\n\nimport (\n\t\"bytes\"\n\t\"encoding/json\"\n\t\"slices\"\n)\n\nfunc f(b *bytes.Buffer, raw []byte) error {\n\t_ = slices.Compact([]int{})\n\treturn json.Compact(b, raw)\n}\n",
		"broken/broken.go": "package broken\n\nfunc f( {\n\tx.Compact()\n}\n",
	})
	gitIn(t, root, "init", "-q")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-qm", "x")
	planted(t, root, map[string]string{"pkg/pkg.go": "package pkg\n\ntype Cache struct{}\n\nfunc (c *Cache) Compact() { _ = c }\n\nfunc Compact(c *Cache) { c.Compact() }\n"})
	var listed []string
	for _, item := range ImpactOf(root, []string{"pkg/pkg.go"}).Items {
		listed = append(listed, item.Item+": "+strings.Join(item.Callers, ", "))
	}
	text := strings.Join(listed, "\n")
	for _, want := range []string{
		"func Compact in pkg/pkg.go: app/use.go:6",
		"app/use.go:7, matched by name: the receiver type is not resolved",
		"broken/broken.go:4, matched by name: the file does not parse",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the impact lacks %q:\n%s", want, text)
		}
	}
	for _, quiet := range []string{"app/trim.go", "pkg/pkg.go:7"} {
		if strings.Contains(text, quiet) {
			t.Errorf("the impact lists %s:\n%s", quiet, text)
		}
	}
}
