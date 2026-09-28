package subagent

import (
	"os"
	"strings"
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
		{"cd src/api && sed -i s/a/b/ ../other.ts", unreadable},
		{"cd .. && sed -i s/a/b/ suppliers.ts", unreadable},
		{"cd /tmp && sed -i s/a/b/ suppliers.ts", refusedAs("/tmp/suppliers.ts")},
	} {
		boundary := &Boundary{Ticket: "ts-dev-3", Owns: []string{"src/api/suppliers.ts"}}
		err := boundary.Shell(driven.cmd)
		t.Logf("%q -> %v", driven.cmd, err)
		switch {
		case driven.refusal == "" && err != nil:
			t.Errorf("%q: want allowed, got %v", driven.cmd, err)
		case driven.refusal != "" && (err == nil || !strings.Contains(err.Error(), driven.refusal)):
			t.Errorf("%q: want refused as %q, got %v", driven.cmd, driven.refusal, err)
		}
	}
}
