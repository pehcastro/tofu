package state

import "testing"

type versionEnvelopeA struct {
	Foo string `json:"foo"`
	Bar string `json:"bar"`
}

type versionEnvelopeAAgain struct {
	Foo string `json:"foo"`
	Bar string `json:"bar"`
}

type versionEnvelopeB struct {
	Foo string `json:"foo"`
	Bar string `json:"bar"`
	Baz string `json:"baz"`
}

func TestDeriveVersionDiffersOnShapeAndIsStableOnRepeat(t *testing.T) {
	versionA := deriveVersion("point", versionEnvelopeA{})
	versionB := deriveVersion("point", versionEnvelopeB{})
	if versionA == versionB {
		t.Fatalf("envelopes that differ in which fields they carry produced the same version %q", versionA)
	}

	for i := 0; i < 100; i++ {
		if got := deriveVersion("point", versionEnvelopeA{}); got != versionA {
			t.Fatalf("run %d: version drifted from %q to %q for an unchanged shape", i, versionA, got)
		}
		if got := deriveVersion("point", versionEnvelopeAAgain{}); got != versionA {
			t.Fatalf("run %d: an identically shaped envelope produced %q, want %q", i, got, versionA)
		}
	}
}
