package decisions_test

import (
	"os"
	"strings"
	"testing"

	"tofu/internal/judge/method"
)

func TestTheFileShortlistReasonDoesNotRestateTheWithdrawnRatio(t *testing.T) {
	table, err := method.Load(os.DirFS(".."))
	if err != nil {
		t.Fatalf("loading the shipped method table: %v", err)
	}
	chosen, err := table.Of("file_shortlist")
	if err != nil {
		t.Fatal(err)
	}
	for _, withdrawn := range []string{"4 of 4", "4/4", "4.00x", "100%"} {
		if strings.Contains(chosen.Why, withdrawn) {
			t.Errorf("%s:%d says %q, and that figure was withdrawn on 2026-09-23 because three of the four questions name their own answer", table.File, chosen.Line, withdrawn)
		}
	}
	for _, park := range []string{"parked", "name their own answer", "7 recordable"} {
		if !strings.Contains(chosen.Why, park) {
			t.Errorf("%s:%d does not say %q, so the reason does not cite the park: %q", table.File, chosen.Line, park, chosen.Why)
		}
	}
}
