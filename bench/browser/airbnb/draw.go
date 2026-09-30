package airbnb

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"time"
)

type place struct {
	search     string
	titleToken string
	nightlyBRL int
}

var places = []place{
	{"Atibaia, São Paulo, Brazil", "Atibaia", 450},
	{"Gramado, Rio Grande do Sul, Brazil", "Gramado", 550},
	{"Florianópolis, Santa Catarina, Brazil", "Florianópolis", 500},
	{"Paraty, Rio de Janeiro, Brazil", "Paraty", 500},
	{"Campos do Jordão, São Paulo, Brazil", "Campos do Jordão", 600},
	{"Ubatuba, São Paulo, Brazil", "Ubatuba", 450},
	{"Lisbon, Portugal", "Lisboa", 900},
	{"Barcelona, Spain", "Barcelona", 1100},
	{"Rome, Italy", "Roma", 1000},
	{"Buenos Aires, Argentina", "Buenos Aires", 500},
	{"Tulum, Mexico", "Tulum", 900},
	{"Porto, Portugal", "Porto", 800},
}

type amenity struct {
	name  string
	id    string
	field Field
}

var amenities = []amenity{
	{"a swimming pool", "7", "pool"},
	{"wifi", "4", "wifi"},
	{"pets allowed", "12", "pets"},
	{"a kitchen", "8", "kitchen"},
}

const budgetStepBRL = 500

func Draw(seed int64, drawnOn time.Time) Task {
	draw := rand.New(rand.NewPCG(uint64(seed), 0))
	where := places[draw.IntN(len(places))]
	checkin := drawnOn.AddDate(0, 0, 14+draw.IntN(43))
	nights := 3 + draw.IntN(5)
	checkout := checkin.AddDate(0, 0, nights)
	adults := 1 + draw.IntN(4)
	needs := amenities[draw.IntN(len(amenities))]
	bedrooms := max(1, (adults+1)/2)
	budget := (where.nightlyBRL*nights*max(adults, 2)/2/budgetStepBRL + 1) * budgetStepBRL
	day := func(date time.Time) string { return date.Format(time.DateOnly) }
	count := func(n int, noun string) string {
		if n == 1 {
			return "1 " + noun
		}
		return strconv.Itoa(n) + " " + noun + "s"
	}
	return Task{
		Prompt: fmt.Sprintf("1. Open Airbnb from Google search, not by directly navigating to it.\n2. Search for %s.\n3. Set check-in to %s and checkout to %s.\n4. Set guests to %s.\n"+
			"5. Apply the \"Entire home\" accommodation type.\n6. Apply a price filter with a maximum of R$%d total.\n7. Require at least %s.\n8. Require %s.\n"+
			"9. Sort/browse the resulting listings.\n10. Open the first 3 listings that match the filters.\n11. For each one, inspect the listing page and report:\n    - name\n    - total price shown\n    - number of bedrooms\n    - whether it has %s\n    - rating\n"+
			"12. Leave the browser on the third listing without booking anything.",
			where.search, checkin.Format("January 2, 2006"), checkout.Format("January 2, 2006"), count(adults, "adult"), budget, count(bedrooms, "bedroom"), needs.name, needs.name),
		Steps: []Step{
			{Step: 1, Says: "Airbnb was reached from a Google search", Check: Check{Kind: SearchedFirst, From: "google.", Value: "airbnb."}},
			{Step: 2, Says: "every reported listing is in " + where.titleToken, Check: Check{Kind: ListingsIn, Value: where.titleToken}},
			{Step: 3, Says: "check-in " + day(checkin) + " and checkout " + day(checkout), Check: Check{Kind: SearchParams, Params: map[string]string{"checkin": day(checkin), "checkout": day(checkout)}}},
			{Step: 4, Says: strconv.Itoa(adults) + " adults", Check: Check{Kind: SearchParams, Params: map[string]string{"adults": strconv.Itoa(adults)}}},
			{Step: 5, Says: "entire home only", Check: Check{Kind: SearchParams, Params: map[string]string{"room_types[]": "Entire home/apt"}}},
			{Step: 6, Says: "a maximum of " + strconv.Itoa(budget) + " on the total price", Check: Check{Kind: SearchParams, Params: map[string]string{"price_max": strconv.Itoa(budget)},
				AnyOf: map[string]string{"display_total_price": "true", "price_filter_input_type": "2"}}},
			{Step: 7, Says: "at least " + strconv.Itoa(bedrooms) + " bedrooms", Check: Check{Kind: SearchParams, Params: map[string]string{"min_bedrooms": strconv.Itoa(bedrooms)}}},
			{Step: 8, Says: "the " + string(needs.field) + " filter is on", Check: Check{Kind: SearchParams, Params: map[string]string{"amenities[]": needs.id}}},
			{Step: 9, Says: "3 listings opened", Check: Check{Kind: RoomsOpened, Count: 3}},
			{Step: 10, Says: "the report names 3 of the opened listings", Check: Check{Kind: ReportListings, Count: 3}},
			{Step: 11, Says: "each named listing has a price, bedrooms, a " + string(needs.field) + " answer and a rating", Check: Check{Kind: ReportFields, Fields: []Field{"price", "bedrooms", needs.field, "rating"}}},
			{Step: 12, Says: "the final tab is the third listing, with nothing booked", Check: Check{Kind: FinalIsNthListing, Count: 3}},
		},
	}
}
