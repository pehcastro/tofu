package boardy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoveToDoingNamesACallerByItsPackage(t *testing.T) {
	m := managedBoard(t, "DEMO")
	m.Store.Root = t.TempDir()
	for name, body := range map[string]string{
		"go.mod":            "module demo\n",
		"pkg/pkg.go":        "package pkg\n\ntype Cache struct{}\n\nfunc Open() *Cache { return &Cache{} }\n\nfunc (c *Cache) Compact() {}\n\nfunc Prune() {}\n",
		"pkg/trim.go":       "package pkg\n\nimport (\n\t\"slices\"\n\n\t\"demo/other\"\n)\n\nfunc trim(xs []int) []int {\n\tother.Prune()\n\treturn slices.Compact(xs)\n}\n",
		"app/use.go":        "package app\n\nimport p \"demo/pkg\"\n\nfunc run() {\n\tp.Open().Compact()\n\tp.Prune()\n}\n",
		"app/json.go":       "package app\n\nimport (\n\t\"bytes\"\n\t\"encoding/json\"\n\ts \"slices\"\n)\n\nfunc f(b *bytes.Buffer, raw []byte) error {\n\t_ = s.Compact([]int{})\n\treturn json.Compact(b, raw)\n}\n",
		"app/words.go":      "package app\n\nvar said = \"x.Compact() and Prune()\"\n",
		"broken/broken.go":  "package broken\n\nfunc f( {\n\tx.Compact()\n}\n",
		"other/other.go":    "package other\n\nfunc Prune() {}\n",
		"pkg/pkg_x_test.go": "package pkg_test\n\nimport \"demo/pkg\"\n\nfunc g() { pkg.Prune() }\n",
	} {
		full := filepath.Join(m.Store.Root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
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
