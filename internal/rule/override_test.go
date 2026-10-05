package rule

import (
	"strings"
	"testing"
	"testing/fstest"
)

func overrideLayer(t *testing.T, files map[string]string) []Rule {
	t.Helper()
	shipped := fstest.MapFS{}
	for name, body := range files {
		shipped[name] = &fstest.MapFile{Data: []byte(body)}
	}
	loaded, err := LoadFS(shipped, "layer")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	return loaded
}

func libraryAt(t *testing.T, version string) []Rule {
	t.Helper()
	return overrideLayer(t, map[string]string{
		"qa/rules/no_unit_test_after_code@" + version + ".yaml": "id: no_unit_test_after_code\ndomain: qa\nalso_reaches: work_on_tests\nkind: human\nconcern: code_rules\ntext: never a unit test after the code\nnotes: written after, it agrees with every bug\n",
		"general/rules/brevity@1.yaml":                          "id: brevity\ndomain: general\nkind: human\nconcern: output_shape\ntext: answer in one line\n",
	})
}

func TestAnOverrideFileIsRefusedForEveryShapeThatCannotSayWhyOrWhat(t *testing.T) {
	for _, c := range []struct{ name, body, names string }{
		{"no reason", "id: brevity\noverrides: brevity@1\nmode: off\nby: person\n", "reason"},
		{"empty reason", "id: brevity\noverrides: brevity@1\nmode: off\nreason: \n", "reason"},
		{"no version", "id: brevity\noverrides: brevity\nmode: off\nreason: r\n", "overrides"},
		{"version zero", "id: brevity\noverrides: brevity@0\nmode: off\nreason: r\n", "overrides"},
		{"version a word", "id: brevity\noverrides: brevity@two\nmode: off\nreason: r\n", "overrides"},
		{"no rule named", "id: brevity\noverrides: @1\nmode: off\nreason: r\n", "overrides"},
		{"both off and text", "id: brevity\noverrides: brevity@1\nmode: off\ntext: x\nreason: r\n", "off"},
		{"neither off nor text", "id: brevity\noverrides: brevity@1\nreason: r\n", "off"},
		{"a mode other than off", "id: brevity\noverrides: brevity@1\nmode: enforced\nreason: r\n", "mode"},
		{"a field of the base", "id: brevity\noverrides: brevity@1\nmode: off\nreason: r\ndomain: dev\n", "domain"},
		{"a trigger of the base", "id: brevity\noverrides: brevity@1\ntext: x\nreason: r\ncondition: (?i)release\n", "condition"},
		{"an unknown by", "id: brevity\noverrides: brevity@1\nmode: off\nreason: r\nby: model\n", "by"},
		{"a reason on a plain rule", "id: brevity\ndomain: general\nkind: human\nconcern: output_shape\ntext: x\nreason: r\n", "overrides"},
	} {
		_, err := LoadFS(fstest.MapFS{"rules/brevity@1.yaml": {Data: []byte(c.body)}}, "layer")
		if err == nil || !strings.Contains(err.Error(), c.names) || !strings.Contains(err.Error(), "brevity@1.yaml") {
			t.Errorf("%s: err = %v, want a refusal naming %q and the file", c.name, err, c.names)
		}
	}
}

func TestAFileNamedWithAVersionThatIsNotANumberIsRefused(t *testing.T) {
	_, err := LoadFS(fstest.MapFS{"rules/brevity@two.yaml": {Data: []byte("id: brevity\ndomain: general\nkind: human\nconcern: output_shape\ntext: x\n")}}, "layer")
	if err == nil || !strings.Contains(err.Error(), "brevity@two.yaml") {
		t.Errorf("err = %v, want a refusal naming the file", err)
	}
}

func TestAnOverrideAppliesOnlyToTheVersionItNamed(t *testing.T) {
	off := overrideLayer(t, map[string]string{"rules/no_unit_test_after_code@1.yaml": "id: no_unit_test_after_code\noverrides: no_unit_test_after_code@1\nmode: off\nreason: public SDK\nby: asked\nat: 2026-10-05\n"})

	current := Layer(libraryAt(t, "1"), off)
	if has(current, "no_unit_test_after_code") {
		t.Errorf("the override names @1 and the library is at @1, and the rule still runs: %+v", current)
	}
	moved := Layer(libraryAt(t, "2"), off)
	at := find(moved, "no_unit_test_after_code")
	if at < 0 || moved[at].Version != 2 || moved[at].Override.Of != "" {
		t.Errorf("the library moved to @2 and the @1 override still changed it: %+v", moved)
	}
	stale := Overrides(libraryAt(t, "2"), off)
	if len(stale) != 1 || !stale[0].Stale || stale[0].Base.Version != 2 || stale[0].Rule.Override.Reason != "public SDK" || stale[0].Rule.Override.By != ByAsked {
		t.Errorf("Overrides at @2 = %+v, want one stale override carrying its reason and by", stale)
	}
	if fresh := Overrides(libraryAt(t, "1"), off); len(fresh) != 1 || fresh[0].Stale {
		t.Errorf("Overrides at @1 = %+v, want one that is not stale", fresh)
	}
}

func TestAnOverrideOfARuleThatNoLongerRunsIsStaleAndAddsNothing(t *testing.T) {
	gone := overrideLayer(t, map[string]string{"rules/vanished@1.yaml": "id: vanished\noverrides: vanished@1\ntext: something new\nreason: r\n"})
	if layered := Layer(libraryAt(t, "1"), gone); has(layered, "vanished") {
		t.Errorf("an override of a rule that does not run was added as a rule of its own: %+v", layered)
	}
	if listed := Overrides(libraryAt(t, "1"), gone); len(listed) != 1 || !listed[0].Stale || listed[0].Base.ID != "" {
		t.Errorf("Overrides = %+v, want one stale override with no base", listed)
	}
}

func TestATextOverrideKeepsWhereTheRuleReachesAndOnlyChangesWhatItSays(t *testing.T) {
	text := overrideLayer(t, map[string]string{"rules/no_unit_test_after_code@1.yaml": "id: no_unit_test_after_code\noverrides: no_unit_test_after_code@1\ntext: unit tests are the contract here\nreason: public SDK\n"})
	layered := Layer(libraryAt(t, "1"), text)
	at := find(layered, "no_unit_test_after_code")
	if at < 0 {
		t.Fatalf("the text override dropped the rule: %+v", layered)
	}
	got := layered[at]
	if got.Text != "unit tests are the contract here" || got.Domain != DomainQA || got.AlsoReaches != ReachWorkOnTests || got.Kind != KindHuman || got.Concern != ConcernCodeRules || got.Notes == "" {
		t.Errorf("the replaced rule = %+v, want the new text over the base's domain, reach, kind, concern and notes", got)
	}
	if got.Override.Reason != "public SDK" || got.Override.By != ByPerson || !strings.HasSuffix(got.File, "no_unit_test_after_code@1.yaml") || !strings.HasPrefix(got.File, "layer") {
		t.Errorf("the replaced rule = %+v, want the override's reason, by person when unsaid, and its own file", got)
	}
}

func TestAProjectOverrideStillAppliesOverAGlobalOneOfTheSameLibraryVersion(t *testing.T) {
	global := overrideLayer(t, map[string]string{"rules/no_unit_test_after_code@1.yaml": "id: no_unit_test_after_code\noverrides: no_unit_test_after_code@2\ntext: global words\nreason: g\n"})
	project := overrideLayer(t, map[string]string{"rules/no_unit_test_after_code@1.yaml": "id: no_unit_test_after_code\noverrides: no_unit_test_after_code@2\ntext: project words\nreason: p\n"})
	afterGlobal := Layer(libraryAt(t, "2"), global)
	if listed := Overrides(afterGlobal, project); len(listed) != 1 || listed[0].Stale {
		t.Fatalf("the project override names the library's @2 and reads as %+v over a global override", listed)
	}
	layered := Layer(afterGlobal, project)
	if at := find(layered, "no_unit_test_after_code"); at < 0 || layered[at].Text != "project words" {
		t.Errorf("layered = %+v, want the project's words", layered)
	}
}

func TestANewIDThatOverridesARuleTakesItsPlace(t *testing.T) {
	swap := overrideLayer(t, map[string]string{"rules/tests_are_contract@1.yaml": "id: tests_are_contract\noverrides: no_unit_test_after_code@1\ntext: unit tests are the contract here\nreason: public SDK\n"})
	library := libraryAt(t, "1")
	layered := Layer(library, swap)
	if has(layered, "no_unit_test_after_code") || find(layered, "tests_are_contract") != find(library, "no_unit_test_after_code") {
		t.Errorf("layered = %+v, want tests_are_contract in the old rule's place and the old rule gone", layered)
	}
}

func TestAnOffFileWithoutOverridesStillSwitchesTheRuleOffAndIsListedWithNoReason(t *testing.T) {
	legacy := overrideLayer(t, map[string]string{"rules/brevity@1.yaml": "id: brevity\ndomain: general\nkind: human\nconcern: code_rules\nmode: off\ntext: switched off by tofu rules off\n"})
	if has(Layer(libraryAt(t, "1"), legacy), "brevity") {
		t.Error("an off file written before overrides existed no longer switches its rule off")
	}
	listed := Overrides(libraryAt(t, "1"), legacy)
	if len(listed) != 1 || listed[0].Stale || listed[0].Rule.Override.Reason != "" || listed[0].Base.ID != "brevity" {
		t.Errorf("Overrides = %+v, want one listing of brevity with no reason", listed)
	}
	own := overrideLayer(t, map[string]string{"rules/mine@1.yaml": "id: mine\ndomain: general\nkind: human\nconcern: code_rules\ntext: my own\n"})
	if listed := Overrides(libraryAt(t, "1"), own); len(listed) != 0 {
		t.Errorf("a rule of the person's own that overrides nothing is listed as an override: %+v", listed)
	}
}

func TestOverrideFileWritesWhatLoadReadsBack(t *testing.T) {
	data, err := OverrideFile("no_unit_test_after_code", "", Override{Of: "no_unit_test_after_code", Version: 1, Reason: "public SDK", By: ByAsked, At: "2026-10-05"})
	if err != nil {
		t.Fatalf("OverrideFile: %v", err)
	}
	read := overrideLayer(t, map[string]string{"rules/no_unit_test_after_code@1.yaml": string(data)})
	if len(read) != 1 || read[0].Mode != ModeOff || read[0].Override != (Override{Of: "no_unit_test_after_code", Version: 1, Reason: "public SDK", By: ByAsked, At: "2026-10-05"}) {
		t.Errorf("read back %+v from %s", read, data)
	}
	for _, refused := range []Override{
		{Of: "x", Version: 1, By: ByPerson},
		{Of: "x", Version: 1, Reason: "two\nlines", By: ByPerson},
	} {
		if data, err := OverrideFile("x", "", refused); err == nil {
			t.Errorf("OverrideFile(%+v) wrote %s, want a refusal", refused, data)
		}
	}
	if data, err := OverrideFile("../x", "", Override{Of: "x", Version: 1, Reason: "r", By: ByPerson}); err == nil {
		t.Errorf("an id with a path in it wrote %s", data)
	}
}

func find(rules []Rule, id string) int {
	for i, r := range rules {
		if r.ID == id {
			return i
		}
	}
	return -1
}

func has(rules []Rule, id string) bool { return find(rules, id) >= 0 }
