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
		if err := learn(dir, "a task", []string{visit}, nil); err != nil {
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
	if err := Settle(dir, task, visits, nil, true); err != nil {
		t.Fatal(err)
	}
	want := []string{"www.google.com", "www.youtube.com"}
	if got := foundHosts(t, dir, task); !slices.Equal(got, want) {
		t.Errorf("after a working run ending on youtube the recipes are %v, want %v", got, want)
	}
}

func TestSettleNeverOverwritesAUsableRecipeForTheLastHost(t *testing.T) {
	dir := dirLearnedFrom(t, "https://www.google.com/search?q=x")
	if err := Settle(dir, "search on google.com", []string{"https://www.google.com/maps/place"}, nil, true); err != nil {
		t.Fatal(err)
	}
	found, err := Find(dir, "google.com")
	if err != nil || len(found) != 1 || found[0].Uses != 1 || found[0].Pages[0].Template != "https://www.google.com/search" {
		t.Errorf("a usable google recipe was relearned instead of recorded: %+v %v", found, err)
	}
}

const ubatubaBrief = `Start url: https://www.google.com

Do this whole task in ONE tab, from start to end. Do not book anything.

1. From Google's search box, search for "airbnb" and open Airbnb from the organic result (not a sponsored/ad result). Do not navigate to airbnb.com directly from the address bar.
2. On Airbnb, search destination "Ubatuba, Sao Paulo, Brazil" (pick the matching suggestion).
3. Set check-in 4 November 2026 and check-out 9 November 2026.
4. Set guests to 3 adults.
5. Run the search, then open the filters panel and apply:
   - Type of place: Entire home/place ("Entire home")
   - Price range: maximum R$3,500 for the total stay (if Airbnb shows a per-night/total toggle, use total prices)
   - Bedrooms: at least 2
   - Amenities: Wifi
   Then apply/show the results.
6. Browse the resulting listings. Open the first 3 listings that match the filters, one after another, in the same tab (click the listing, read the page, then go back for the next; the third one stays open).
7. For EACH of the 3 listings report: exact listing name, total price shown for the stay, number of bedrooms, whether wifi is offered, and the rating (with review count if shown).
8. Leave the browser on the third listing page. Do not start a booking or enter payment details.

If a filter or a control cannot be reached after two different approaches, build the URL from Airbnb's own query parameters and navigate to it in the same tab before calling anything blocked.`

const atibaiaTask = "1. Open Airbnb from Google search.\n2. Search for Atibaia, Sao Paulo.\n3. Set check-in to October 9, 2026 and checkout to October 15, 2026.\n4. Set guests to 2 adults.\n5. Apply the \"Entire home\" accommodation type.\n6. Apply a price filter with a maximum of R$4,000 total.\n7. Require at least 2 bedrooms.\n8. Require a swimming pool."

func ubatubaVisits(prices ...string) []string {
	first := "https://www.airbnb.com.br/s/Ubatuba--SP/homes?place_id=ChIJmadeUpUbatubaPlace01&refinement_paths%5B%5D=%2Fhomes&location_bb=a1b2C3d4E5f6G7h8I9j0Kw%3D%3D&acp_id=11111111-2222-4333-8444-555555555555&date_picker_type=calendar&checkin=2026-11-04&checkout=2026-11-09&adults=3&search_type=autocomplete_click"
	bedrooms := "https://www.airbnb.com.br/s/Ubatuba--SP/homes?acp_id=11111111-2222-4333-8444-555555555555&adults=3&checkin=2026-11-04&checkout=2026-11-09&date_picker_type=calendar&ref_fsid=aaaaaaaa-0000-4000-8000-000000000001&ref_search_session_id=bbbbbbbb-0000-4000-8000-000000000002&place_id=ChIJmadeUpUbatubaPlace01&refinement_paths%5B%5D=%2Fhomes&flexible_trip_lengths%5B%5D=one_week&monthly_start_date=2026-10-01&monthly_length=3&monthly_end_date=2027-01-01&search_mode=regular_search&price_filter_input_type=2&price_filter_num_nights=5&channel=EXPLORE&search_type=filter_change&min_bedrooms=2&selected_filter_order%5B%5D=min_bedrooms%3A2%3Abar"
	wifi := bedrooms + "&selected_filter_order%5B%5D=amenities%3A4%3Abar&amenities%5B%5D=4"
	room := func(id string) string {
		return "https://www.airbnb.com.br/rooms/" + id + "?adults=3&check_in=2026-11-04&check_out=2026-11-09&search_mode=regular_search&amenities%5B%5D=4&source_impression_id=p3_made_up&previous_page_section_name=1000&federated_search_id=cccccccc-0000-4000-8000-000000000003"
	}
	visits := []string{"https://www.google.com/search?q=airbnb", "https://www.airbnb.com.br/", first, bedrooms, wifi}
	for _, price := range prices {
		visits = append(visits, wifi+"&room_types%5B%5D=Entire%20home%2Fapt&price_max="+price+"&selected_filter_order%5B%5D=room_types%3AEntire%20home%2Fapt&selected_filter_order%5B%5D=price_max%3A"+price+"&update_selected_filters=true")
	}
	visits = append(visits, room("52655988"), room("1705306239457373986"), room("1253177679311009223"))
	for _, price := range prices {
		visits = append(visits, wifi+"&room_types%5B%5D=Entire%20home%2Fapt&price_max="+price+"&update_selected_filters=true")
	}
	return append(visits, room("1253177679311009223"))
}

func learnedPages(t *testing.T, task string, reached, typed []string) []Page {
	t.Helper()
	dir := t.TempDir()
	if err := learn(dir, task, reached, typed); err != nil {
		t.Fatal(err)
	}
	recipes, err := List(dir)
	if err != nil || len(recipes) > 1 {
		t.Fatalf("the recipes learned are %+v %v", recipes, err)
	}
	if len(recipes) == 0 {
		return nil
	}
	return recipes[0].Pages
}

func templates(pages []Page) []string {
	var all []string
	for _, page := range pages {
		all = append(all, page.Template)
	}
	return all
}

func TestLearnFromTheUbatubaRunKeepsOnlyTheFiltersTheTaskSet(t *testing.T) {
	got := templates(learnedPages(t, ubatubaBrief, ubatubaVisits("700", "3500"), nil))
	want := []string{
		"https://www.airbnb.com.br/s/{place}/homes?adults={adults}&checkin={checkin}&checkout={checkout}&price_filter_input_type={price_filter_input_type}&price_filter_num_nights={price_filter_num_nights}&min_bedrooms={min_bedrooms}&amenities[]={amenities}&room_types[]={room_types}&price_max={price_max}",
		"https://www.airbnb.com.br/rooms/{id}?adults={adults}&check_in={check_in}&check_out={check_out}",
	}
	if !slices.Equal(got, want) {
		t.Errorf("the learned templates are\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestLearnNothingWhenNoUrlCarriesEveryTaskValue(t *testing.T) {
	if got := templates(learnedPages(t, ubatubaBrief, ubatubaVisits("700"), nil)); len(got) != 0 {
		t.Errorf("a run that never reached R$3,500 learned %v", got)
	}
}

func TestARecipeForAnotherPlaceOffersThePlaceSlotAndNotUbatuba(t *testing.T) {
	dir := t.TempDir()
	if err := learn(dir, ubatubaBrief, ubatubaVisits("3500"), nil); err != nil {
		t.Fatal(err)
	}
	found, err := Find(dir, "find a house in Atibaia on airbnb.com")
	if err != nil || len(found) != 1 {
		t.Fatalf("the airbnb recipe was not found: %v %v", found, err)
	}
	if brief := found[0].Brief(); !strings.Contains(brief, "/s/{place}/homes?") || strings.Contains(brief, "Ubatuba") || strings.Contains(brief, "ChIJ") {
		t.Errorf("an Atibaia task is given\n%s", brief)
	}
}

func TestARecipeBriefKeepsThePathTheTaskNames(t *testing.T) {
	brief := Recipe{Host: "www.airbnb.com.br", Pages: []Page{{Template: "https://www.airbnb.com.br/s/{place}/homes"}}}.Brief()
	if strings.Contains(brief, "first") || !strings.Contains(brief, "once the task's own path has reached the site") {
		t.Errorf("a recipe brief can override the path a task names\n%s", brief)
	}
}

func TestLearnKeepsWhatTheAgentTypedIntoAUrlItBuiltButNotIntoOneItCopied(t *testing.T) {
	built := "https://www.airbnb.com.br/s/Atibaia--SP/homes?query=Atibaia%2C%20SP&checkin=2026-10-09&checkout=2026-10-15&adults=2&room_types%5B%5D=Entire%20home%2Fapt&price_max=4000&display_total_price=true&min_bedrooms=2&amenities%5B%5D=7"
	got := templates(learnedPages(t, atibaiaTask, []string{"https://www.google.com/search?q=airbnb", built}, []string{built}))
	want := "https://www.airbnb.com.br/s/{place}/homes?query={query}&checkin={checkin}&checkout={checkout}&adults={adults}&room_types[]={room_types}&price_max={price_max}&display_total_price={display_total_price}&min_bedrooms={min_bedrooms}&amenities[]={amenities}"
	if !slices.Equal(got, []string{want}) {
		t.Errorf("a url the agent built itself taught %v, want %s", got, want)
	}
	copied := "https://www.airbnb.com.br/s/Atibaia--SP/homes?acp_id=11111111-2222-4333-8444-555555555555&checkin=2026-10-09&checkout=2026-10-15&adults=2&price_max=4000&channel=EXPLORE"
	got = templates(learnedPages(t, atibaiaTask, []string{copied}, []string{copied}))
	want = "https://www.airbnb.com.br/s/{place}/homes?checkin={checkin}&checkout={checkout}&adults={adults}&price_max={price_max}"
	if !slices.Equal(got, []string{want}) {
		t.Errorf("a url the agent copied from a page taught %v, want %s", got, want)
	}
}

func TestLearnReadsAStateTheSiteEncodedInOneParameter(t *testing.T) {
	task := "Search a round trip from San Francisco to Tokyo, leaving November 14, 2026 and returning November 25, 2026, for 2 passenger(s)."
	search := "https://www.google.com/travel/flights/search?tfs=CBwQAhojEgoyMDI2LTExLTE0agcIARIDU0ZPcgwIAhIIL20vMDdkZmsaIxIKMjAyNi0xMS0yNWoMCAISCC9tLzA3ZGZrcgcIARIDU0ZPQAFAAUgBcAGCAQsI____________AZgBAQ&tfu=EgoIABAAGAAgAigB&hl=en&gl=US"
	visits := []string{"https://www.google.com/travel/flights?tfs=CBwQARoJagcIARIDU0ZPGglyBwgBEgNTRk9AAUABSAFwAYIBCwj___________8BmAEB&tfu=KgIIAw&hl=en&gl=US", search}
	if got := templates(learnedPages(t, task, visits, nil)); !slices.Equal(got, []string{"https://www.google.com/travel/flights/search?tfs={tfs}"}) {
		t.Errorf("the flights search taught %v", got)
	}
}

func TestTypedReadsTheUrlsANavigateAsksFor(t *testing.T) {
	args := `{"tab":3,"actions":[{"action":"navigate","url":"https://www.airbnb.com.br/s/homes?adults=2&checkin=2026-10-09"},{"action":"click","target":"Search"}]}`
	if got := Typed([]byte(args)); !slices.Equal(got, []string{"https://www.airbnb.com.br/s/homes?adults=2&checkin=2026-10-09"}) {
		t.Errorf("the navigate arguments gave %v", got)
	}
}
