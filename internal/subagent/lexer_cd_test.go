package subagent

import (
	"os"
	"testing"
)

func TestShellResolvesAWriteAgainstTheDirectoryALeadingCdEntered(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("src/api", 0o755); err != nil {
		t.Fatal(err)
	}
	refusedAs := func(path string) string { return `"` + path + `" ` + notOwned }
	for _, driven := range []struct {
		cmd, refusal string
	}{
		{"cd src/api && sed -i s/a/b/ suppliers.json", ""},
		{"cd src && sed -i s/a/b/ other.json", refusedAs("src/other.json")},
		{"cd src/api; sed -i s/a/b/ suppliers.json", ""},
		{"cd src/api\nsed -i s/a/b/ suppliers.json", ""},
		{"cd src && cd api && sed -i s/a/b/ suppliers.json", ""},
		{"cd -P src/api && echo x > suppliers.json", ""},
		{"cd src/api &&\nsed -i s/a/b/ suppliers.json", ""},
		{"(cd src/api && true) && sed -i s/a/b/ suppliers.json", refusedAs("suppliers.json")},
		{"cd src/api | sed -i s/a/b/ suppliers.json", refusedAs("suppliers.json")},
		{"cd src/api & sed -i s/a/b/ suppliers.json", refusedAs("suppliers.json")},
		{"false || cd src/api && sed -i s/a/b/ suppliers.json", refusedAs("suppliers.json")},
		{"false && cd src/api; sed -i s/a/b/ suppliers.json", refusedAs("suppliers.json")},
		{"false &&\ncd src/api\nsed -i s/a/b/ suppliers.json", refusedAs("suppliers.json")},
		{"cd src/missing; sed -i s/a/b/ suppliers.json", refusedAs("src/missing/suppliers.json")},
		{"cd $DIR && sed -i s/a/b/ suppliers.json", refusedAs("suppliers.json")},
		{"cd src/api && sed -i s/a/b/ ../other.json", refusedAs("src/api/../other.json")},
		{"cd .. && sed -i s/a/b/ suppliers.json", escaping},
		{"sed -i s/a/b/ /tmp/suppliers.json", ""},
	} {
		boundary := NewBoundary("ts-dev-3", "", []string{"src/api/suppliers.json"})
		err := boundary.Shell(driven.cmd)
		t.Logf("%q -> %v", driven.cmd, err)
		if !refusedAsWanted(err, driven.refusal) {
			t.Errorf("%q: want %q, got %v", driven.cmd, driven.refusal, err)
		}
	}
}
