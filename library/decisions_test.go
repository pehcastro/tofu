package library_test

import (
	"testing"

	"tofu/internal/judge/method"
	"tofu/library"
)

func TestTheBinaryCarriesTheMethodTableAndEveryWiredPointStatesItsCost(t *testing.T) {
	table, err := method.Load(library.Files())
	if err != nil {
		t.Fatalf("the binary does not ship %s: %v", method.TableFile, err)
	}
	if table.Notes == "" {
		t.Error("the shipped table carries no notes, and the one place says what it is for")
	}
	wired := 0
	for _, chosen := range table.Choices {
		if chosen.Method == method.Unwired {
			continue
		}
		wired++
		if chosen.Cost == "" || chosen.Measured == "" {
			t.Errorf("%s is decided by %s and states cost %q from %q", chosen.Point, chosen.Method, chosen.Cost, chosen.Measured)
		}
	}
	if wired == 0 {
		t.Fatal("the shipped table wires nothing, so this test proves nothing")
	}
}
