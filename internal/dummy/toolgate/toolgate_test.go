package toolgate

import "testing"

func TestTheCorpusIsAsciiBecauseTheRepositoryRefusesAnEmDash(t *testing.T) {
	for _, name := range []string{Cases, CasesWhole} {
		raw, err := Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, b := range raw {
			if b > 127 {
				t.Fatalf("%s byte %d is 0x%x, the corpus must be ascii", name, i, b)
			}
		}
	}
}
