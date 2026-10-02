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
		{"cd src/api && sed -i s/a/b/ suppliers.ts", ""},
		{"cd src && sed -i s/a/b/ other.ts", refusedAs("src/other.ts")},
		{"cd src/api; sed -i s/a/b/ suppliers.ts", ""},
		{"cd src/api\nsed -i s/a/b/ suppliers.ts", ""},
		{"cd src && cd api && sed -i s/a/b/ suppliers.ts", ""},
		{"cd -P src/api && echo x > suppliers.ts", ""},
		{"cd src/api &&\nsed -i s/a/b/ suppliers.ts", ""},
		{"(cd src/api && true) && sed -i s/a/b/ suppliers.ts", refusedAs("suppliers.ts")},
		{"cd src/api | sed -i s/a/b/ suppliers.ts", refusedAs("suppliers.ts")},
		{"cd src/api & sed -i s/a/b/ suppliers.ts", refusedAs("suppliers.ts")},
		{"false || cd src/api && sed -i s/a/b/ suppliers.ts", refusedAs("suppliers.ts")},
		{"false && cd src/api; sed -i s/a/b/ suppliers.ts", refusedAs("suppliers.ts")},
		{"false &&\ncd src/api\nsed -i s/a/b/ suppliers.ts", refusedAs("suppliers.ts")},
		{"cd src/missing; sed -i s/a/b/ suppliers.ts", refusedAs("src/missing/suppliers.ts")},
		{"cd $DIR && sed -i s/a/b/ suppliers.ts", refusedAs("suppliers.ts")},
		{"cd src/api && sed -i s/a/b/ ../other.ts", refusedAs("src/api/../other.ts")},
		{"cd .. && sed -i s/a/b/ suppliers.ts", escaping},
		{"sed -i s/a/b/ /tmp/suppliers.ts", ""},
	} {
		boundary := &Boundary{Ticket: "ts-dev-3", Owns: []string{"src/api/suppliers.ts"}}
		err := boundary.Shell(driven.cmd)
		t.Logf("%q -> %v", driven.cmd, err)
		if !refusedAsWanted(err, driven.refusal) {
			t.Errorf("%q: want %q, got %v", driven.cmd, driven.refusal, err)
		}
	}
}
