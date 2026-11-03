package models

import "testing"

func TestTheShippedLibraryLoadsBesideItsPriceOverrides(t *testing.T) {
	shippedLibrary(t)
	registry, err := ShippedRegistry()
	if err != nil {
		t.Fatalf("reading the shipped registry: %v", err)
	}
	for slug, want := range map[string]Rates{
		"meta/muse-spark-1.3-contributor": {Input: 0.10, Output: 0.20, CacheRead: 0.002},
		"meta/muse-spark-1.2-contributor": {Input: 0.10, Output: 0.20, CacheRead: 0.002},
		"meta/muse-spark-1.3":             {Input: 1.25, Output: 4.25, CacheRead: 0.15},
		"meta/muse-spark-1.2":             {Input: 1.25, Output: 4.25, CacheRead: 0.15},
		"meta/muse-spark-1.1":             {Input: 1.25, Output: 4.25, CacheRead: 0.15},
	} {
		if price := registry.Prices[slug]; price.Rates != want || price.From != "library/models/prices/meta.yaml, models.dev as oh-my-pi catalog 18.3.0 lists it" {
			t.Fatalf("%s read %+v, want %+v from the shipped override", slug, price, want)
		}
	}
}
