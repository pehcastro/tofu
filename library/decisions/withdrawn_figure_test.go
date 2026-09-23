package decisions_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"tofu/internal/judge/method"
)

var recordableCount = regexp.MustCompile(`\d+ recordable`)
var floorCount = regexp.MustCompile(`floor of \d+ questions`)

func parkCitationIssues(why string) []string {
	var missing []string
	for _, park := range []string{"parked", "name their own answer"} {
		if !strings.Contains(why, park) {
			missing = append(missing, park)
		}
	}
	if !recordableCount.MatchString(why) {
		missing = append(missing, "a recordable-turn count")
	}
	if !floorCount.MatchString(why) {
		missing = append(missing, "the sample-size floor")
	}
	return missing
}

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
	for _, missing := range parkCitationIssues(chosen.Why) {
		t.Errorf("%s:%d does not cite %s, so the reason does not say why the point is parked: %q", table.File, chosen.Line, missing, chosen.Why)
	}
}

func TestTheParkCitationCheckCatchesAReasonThatDropsIt(t *testing.T) {
	why := "the old accuracy figure is withdrawn and nothing here explains what replaces it."
	if missing := parkCitationIssues(why); len(missing) == 0 {
		t.Fatal("a reason that never says why the point is parked passed the park-citation check")
	}
}
