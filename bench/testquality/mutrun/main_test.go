package main

import (
	"strings"
	"testing"

	"tofu/bench/mutate"
)

func TestGremlinsCarriesTheSafeBoundBeforeThisRunnerUsesIt(t *testing.T) {
	argv := strings.Join(mutate.Gremlins.Args, " ")
	if !strings.Contains(argv, "--workers 1") {
		t.Fatalf("mutate.Gremlins.Args = %q, want a single worker; a four-worker run once grew a child test binary to 30 GB", argv)
	}
	if !strings.Contains(argv, "--timeout-coefficient 5") {
		t.Fatalf("mutate.Gremlins.Args = %q, want the coefficient-5 timeout", argv)
	}
}
