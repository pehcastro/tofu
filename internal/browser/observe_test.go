package browser_test

import (
	"regexp"
	"testing"
)

func TestATargetWithAnEmptyRoleFindsTheFirstElementByNameAndAWrongRoleFindsNone(t *testing.T) {
	driver, _ := drivenPage(t, "rerender")
	refs := regexp.MustCompile(`button "Adicionar" \[[^\]]*ref=(e\d+)`).FindAllStringSubmatch(observe(t, driver, true), -1)
	if len(refs) != 3 {
		t.Fatalf("want three Adicionar refs, got %q", refs)
	}
	for _, target := range []struct {
		role string
		nth  int
		want string
	}{{"", 0, refs[0][1]}, {"", 2, refs[1][1]}, {"button", 3, refs[2][1]}, {"link", 0, ""}} {
		if ref, err := driver.Find(target.role, "adicionar", target.nth); ref != target.want || err != nil {
			t.Errorf("Find(%q, adicionar, %d) = %q, %v; want %q", target.role, target.nth, ref, err, target.want)
		}
	}
}
