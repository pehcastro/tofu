package browser_test

import (
	"regexp"
	"slices"
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

func TestATargetNamedByTheStartOfOneLongNameFindsItAndAnExactNameStillWins(t *testing.T) {
	driver, page := drivenPage(t, "rerender")
	page.mu.Lock()
	for i, name := range []string{"Concluido. Pesquisar voos de ida e volta com a partida em 14 de novembro", "Concluido. Pesquisar voos so de ida", "Concluido"} {
		page.Nodes[i+1].Name = name
	}
	page.mu.Unlock()
	refs := regexp.MustCompile(`button "Concluido[^"]*" \[[^\]]*ref=(e\d+)`).FindAllStringSubmatch(observe(t, driver, true), -1)
	if len(refs) != 3 {
		t.Fatalf("want three Concluido refs, got %q", refs)
	}
	long, short := "Concluido. Pesquisar voos de ida e volta com a partida em 14 de novembro", "Concluido. Pesquisar voos so de ida"
	for _, target := range []struct {
		name, want string
		fits       []string
	}{
		{"concluido. pesquisar voos de ida", refs[0][1], nil},
		{"partida em 14", refs[0][1], nil},
		{"Concluido", refs[2][1], nil},
		{"Concluido. Pesquisar voos", "", []string{long, short}},
		{"voos", "", []string{long, short}},
		{"", "", nil},
	} {
		if ref, fits, err := driver.FindTarget("button", target.name, 1); ref != target.want || !slices.Equal(fits, target.fits) || err != nil {
			t.Errorf("FindTarget(button, %q, 1) = %q, %q, %v; want %q, %q", target.name, ref, fits, err, target.want, target.fits)
		}
	}
	if ref, err := driver.Find("button", "Concluido. Pesquisar voos de ida", 1); ref != "" || err != nil {
		t.Errorf("Find matched a shortened name as %q, %v; want the exact match only", ref, err)
	}
}
