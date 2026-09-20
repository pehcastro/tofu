package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPipedVerbCarriesNoEscapeSequence(t *testing.T) {
	for _, verb := range []string{"doctor", "models", "usage"} {
		t.Run(verb, func(t *testing.T) {
			isolateHome(t)
			path := filepath.Join(t.TempDir(), verb+".txt")
			file, err := os.Create(path)
			if err != nil {
				t.Fatalf("creating %s: %v", path, err)
			}
			run([]string{verb}, strings.NewReader(""), file, file)
			if err := file.Close(); err != nil {
				t.Fatalf("closing %s: %v", path, err)
			}
			written, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			if len(written) == 0 {
				t.Fatalf("boji %s wrote nothing to the file", verb)
			}
			if index := strings.IndexByte(string(written), 0x1b); index >= 0 {
				t.Fatalf("boji %s wrote an escape sequence at byte %d:\n%q", verb, index, string(written))
			}
		})
	}
}

func TestAColouredWriterPaintsTheVerdictAndAPlainOneDoesNot(t *testing.T) {
	painted := coloured.unsettled(doctorNotReady.String())
	if !strings.ContainsRune(painted, 0x1b) {
		t.Fatalf("a coloured palette painted nothing: %q", painted)
	}
	if plain.unsettled(doctorNotReady.String()) != doctorNotReady.String() {
		t.Fatal("a plain palette painted the verdict")
	}
	if got := headline("boji 0.0.0", painted, len(doctorNotReady.String())); !strings.HasSuffix(got, painted) {
		t.Fatalf("the headline dropped the painted verdict: %q", got)
	}
}
