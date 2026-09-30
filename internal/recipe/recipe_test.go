package recipe

import (
	"slices"
	"strings"
	"testing"

	"tofu/internal/konst"
)

func dirLearnedFrom(t *testing.T, visits ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, visit := range visits {
		if err := learn(dir, "a task", []string{visit}); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func foundHosts(t *testing.T, dir, task string) []string {
	t.Helper()
	found, err := Find(dir, task)
	if err != nil {
		t.Fatal(err)
	}
	hosts := []string{}
	for _, known := range found {
		hosts = append(hosts, known.Host)
	}
	slices.Sort(hosts)
	return hosts
}

func TestFindMatchesTheSiteNameAcrossCountrySuffixes(t *testing.T) {
	dir := dirLearnedFrom(t, "https://www.airbnb.com.br/s/Rio/homes?adults=2")
	if got := foundHosts(t, dir, "find a house for 2 in Rio on airbnb.com"); !slices.Equal(got, []string{"www.airbnb.com.br"}) {
		t.Errorf("a task saying airbnb.com found %v, want the www.airbnb.com.br recipe", got)
	}
	if got := foundHosts(t, dir, "find a hotel in Rio on booking.com"); len(got) != 0 {
		t.Errorf("a task saying only booking.com found %v", got)
	}
}

func TestFindGivesEveryNamedSiteAndNoSiteWhoseNameIsOnlyASubstring(t *testing.T) {
	dir := dirLearnedFrom(t,
		"https://www.google.com/search?q=x",
		"https://www.youtube.com/results?search_query=x",
		"https://www.tube.com/watch?v=x",
		"https://www.airbnb.com.br/s/Rio/homes",
	)
	want := []string{"www.google.com", "www.youtube.com"}
	if got := foundHosts(t, dir, "search google.com for a song, then play it on youtube.com"); !slices.Equal(got, want) {
		t.Errorf("a task naming google.com and youtube.com found %v, want %v", got, want)
	}
}

func TestFindDropsSetAsideRecipesBeforeTheLimit(t *testing.T) {
	dir := dirLearnedFrom(t,
		"https://www.airbnb.com.br/s/Rio/homes",
		"https://www.google.com/search?q=x",
		"https://www.youtube.com/results?search_query=x",
	)
	google, err := Find(dir, "google.com")
	if err != nil || len(google) != 1 {
		t.Fatalf("the google recipe was not found: %v %v", google, err)
	}
	google[0].FailedInARow = konst.RecipeFailuresAside - 1
	if err := Record(dir, google[0], false); err != nil {
		t.Fatal(err)
	}
	want := []string{"www.airbnb.com.br", "www.youtube.com"}
	if got := foundHosts(t, dir, "airbnb.com, google.com and youtube.com"); !slices.Equal(got, want) {
		t.Errorf("with the google recipe set aside the task found %v, want %v", got, want)
	}
}

func TestSettleLearnsTheLastHostWhileAnotherSitesRecipeWasGiven(t *testing.T) {
	dir := dirLearnedFrom(t, "https://www.google.com/search?q=x")
	task := "find the song on google.com, then play it on youtube.com"
	visits := []string{"https://www.google.com/search?q=song", "https://www.youtube.com/watch?v=abc"}
	if err := Settle(dir, task, visits, true); err != nil {
		t.Fatal(err)
	}
	want := []string{"www.google.com", "www.youtube.com"}
	if got := foundHosts(t, dir, task); !slices.Equal(got, want) {
		t.Errorf("after a working run ending on youtube the recipes are %v, want %v", got, want)
	}
}

func TestSettleNeverOverwritesAUsableRecipeForTheLastHost(t *testing.T) {
	dir := dirLearnedFrom(t, "https://www.google.com/search?q=x")
	if err := Settle(dir, "search on google.com", []string{"https://www.google.com/maps/place"}, true); err != nil {
		t.Fatal(err)
	}
	found, err := Find(dir, "google.com")
	if err != nil || len(found) != 1 || found[0].Uses != 1 || found[0].Pages[0].Template != "https://www.google.com/search?q={q}" {
		t.Errorf("a usable google recipe was relearned instead of recorded: %+v %v", found, err)
	}
}

func TestLearnKeepsTheLastTemplatePerPath(t *testing.T) {
	dir := t.TempDir()
	visits := []string{
		"https://www.airbnb.com.br/s/Rio/homes?a=1",
		"https://www.airbnb.com.br/s/Rio/homes?a=1&recent_search_suggested_filters=x",
		"https://www.airbnb.com.br/rooms/123",
	}
	if err := learn(dir, "a house in Rio on airbnb.com", visits); err != nil {
		t.Fatal(err)
	}
	found, err := Find(dir, "www.airbnb.com.br")
	if err != nil || len(found) != 1 {
		t.Fatalf("the learned recipe was not found: %v %v", found, err)
	}
	var searches []string
	for _, page := range found[0].Pages {
		if strings.Contains(page.Template, "/s/") {
			searches = append(searches, page.Template)
		}
	}
	want := "https://www.airbnb.com.br/s/Rio/homes?a={a}&recent_search_suggested_filters={recent_search_suggested_filters}"
	if !slices.Equal(searches, []string{want}) {
		t.Errorf("the /s/ templates kept are %v, want only %s", searches, want)
	}
}
