package models

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

const pricedTable = `{"meta":{"models":{` +
	`"muse-spark-1.3-contributor":{"limit":{"context":1048576},"cost":{"input":0.10,"output":0.20,"cache_read":0.002}},` +
	`"muse-spark-1.3":{"limit":{"context":1048576},"cost":{"input":1.25,"output":4.25,"cache_read":0.15,` +
	`"tiers":[{"input":2.5,"output":8.5,"cache_read":0.3,"tier":{"type":"context","size":272000}}]}},` +
	`"muse-spark-1.1":{"limit":{"context":1048576}},` +
	`"muse-spark-free":{"limit":{"context":1048576},"cost":{"input":0,"output":0}},` +
	`"muse-spark-hourly":{"limit":{"context":1048576},"cost":{"input":1,"output":2,"tiers":[{"input":3,"output":4,"tier":{"type":"hour","size":9}}]}}` +
	`}}}`

func TestModelsDevCostIsAPriceWithItsSource(t *testing.T) {
	registry := registryOf(t, pricedTable)
	price, priced := registry.Prices["meta/muse-spark-1.3-contributor"]
	if !priced || price.Input != 0.10 || price.Output != 0.20 || price.CacheRead != 0.002 || price.CacheWrite != 0 {
		t.Fatalf("muse-spark-1.3-contributor read %+v, priced %v, want input 0.10, output 0.20, cache_read 0.002", price, priced)
	}
	if price.From != "models.dev under test" {
		t.Fatalf("the price says it came from %q", price.From)
	}
	tiered := registry.Prices["meta/muse-spark-1.3"]
	if len(tiered.Tiers) != 1 || tiered.Tiers[0].AboveTokens != 272000 || tiered.Tiers[0].Input != 2.5 || tiered.Tiers[0].CacheRead != 0.3 {
		t.Fatalf("the context tier read %+v", tiered.Tiers)
	}
}

func TestAModelWithNoCostHasNoPrice(t *testing.T) {
	registry := registryOf(t, pricedTable)
	if price, priced := registry.Prices["meta/muse-spark-1.1"]; priced {
		t.Fatalf("a model models.dev lists without cost came back priced %+v", price)
	}
	if price, priced := registry.Prices["meta/muse-spark-hourly"]; priced {
		t.Fatalf("a tier that is not a context tier was read as one: %+v", price)
	}
	if free, priced := registry.Prices["meta/muse-spark-free"]; !priced || free.Input != 0 || free.Output != 0 {
		t.Fatalf("a model models.dev lists at zero is free, not unpriced: %+v, priced %v", free, priced)
	}
}

func TestAShippedOverrideWinsOverModelsDev(t *testing.T) {
	overrides := fstest.MapFS{"models/prices/meta.yaml": {Data: []byte(
		"muse-spark-1.3-contributor: input 0.11, output 0.22, cache_read 0.003, taken 2026-09-29\r\n\n" +
			"muse-spark-1.1: input 1.25, output 4.25\n")}}
	registry, err := withPriceOverrides(registryOf(t, pricedTable), overrides)
	if err != nil {
		t.Fatalf("applying the override: %v", err)
	}
	won := registry.Prices["meta/muse-spark-1.3-contributor"]
	if won.Input != 0.11 || won.Output != 0.22 || won.CacheRead != 0.003 || won.From != "library/models/prices/meta.yaml" || won.Taken != "2026-09-29" {
		t.Fatalf("the override lost to models.dev: %+v", won)
	}
	if filled, priced := registry.Prices["meta/muse-spark-1.1"]; !priced || filled.Output != 4.25 {
		t.Fatalf("a model models.dev leaves unpriced took no price from the override: %+v", filled)
	}
	if kept := registry.Prices["meta/muse-spark-1.3"]; kept.Input != 1.25 {
		t.Fatalf("a model the override does not name lost its models.dev price: %+v", kept)
	}
	for _, broken := range []string{"muse-spark-1.3: input 1, output 2, cached 3\n", "muse-spark-1.3: input 1\n", "muse-spark-1.3: input one, output 2\n"} {
		if _, err := withPriceOverrides(registryOf(t, pricedTable), fstest.MapFS{"models/prices/meta.yaml": {Data: []byte(broken)}}); err == nil {
			t.Fatalf("the override %q was read as a price card", broken)
		}
	}
}

func TestAReloadedRegistryKeepsItsPricesAndTheDayItWasTaken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := registryOf(t, pricedTable).Store(path); err != nil {
		t.Fatalf("storing: %v", err)
	}
	stored, err := RegistryAt(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	price, priced := stored.Prices["meta/muse-spark-free"]
	if !priced || price.From != "models.dev under test" || len(price.Taken) != len("2026-09-29") {
		t.Fatalf("the stored price read %+v", price)
	}
	if overridden := stored.Prices["meta/muse-spark-1.3-contributor"]; overridden.From != "library/models/prices/meta.yaml" {
		t.Fatalf("the shipped override did not win over a stored registry: %+v", overridden)
	}
	older := filepath.Join(t.TempDir(), "older.json")
	if err := os.WriteFile(older, []byte(`{"from":"a table from before prices","windows":{"meta/muse-spark-1.3":1048576}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RegistryAt(older); err != nil {
		t.Fatalf("a registry stored before prices existed no longer reads: %v", err)
	}
}
