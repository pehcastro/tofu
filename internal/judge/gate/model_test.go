package gate

import (
	"testing"

	"tofu/internal/judge/ledger"
)

func TestUnknownVerdictIsFatal(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Verdict(\"bogus\").String() did not panic")
		}
	}()
	_ = Verdict("bogus").String()
}

func TestKnownVerdictsPrint(t *testing.T) {
	for _, v := range AllVerdicts() {
		if v.String() == "" {
			t.Errorf("Verdict(%q).String() is empty", v)
		}
	}
}

func TestEveryGateVerdictHasALedgerVerdict(t *testing.T) {
	want := map[Verdict]ledger.Verdict{
		VerdictAllow: ledger.VerdictAllow,
		VerdictAsk:   ledger.VerdictAsk,
		VerdictDeny:  ledger.VerdictDeny,
	}
	for _, v := range AllVerdicts() {
		expected, named := want[v]
		if !named {
			t.Fatalf("%q carries no expected ledger verdict, so a new gate verdict can reach Verdict.Ledger untested", string(v))
		}
		if got := v.Ledger(); got != expected {
			t.Errorf("Verdict(%s).Ledger() = %s, want %s", v, got, expected)
		}
	}
}

func TestEveryGateModeHasALedgerMode(t *testing.T) {
	want := map[Mode]ledger.Mode{
		ModeShadow:   ledger.ModeShadow,
		ModeEnforced: ledger.ModeEnforced,
	}
	for _, m := range AllModes() {
		expected, named := want[m]
		if !named {
			t.Fatalf("%q carries no expected ledger mode, so a new gate mode can reach Mode.Ledger untested", string(m))
		}
		if got := m.Ledger(); got != expected {
			t.Errorf("Mode(%s).Ledger() = %s, want %s", m, got, expected)
		}
	}
}

func TestEveryLedgerVerdictHasAGateVerdict(t *testing.T) {
	want := map[ledger.Verdict]Verdict{
		ledger.VerdictAllow: VerdictAllow,
		ledger.VerdictAsk:   VerdictAsk,
		ledger.VerdictDeny:  VerdictDeny,
	}
	for _, v := range ledger.AllVerdicts() {
		if v == ledger.VerdictUnset {
			continue
		}
		expected, named := want[v]
		if !named {
			t.Fatalf("%s carries no expected gate verdict, so a new ledger verdict can reach VerdictOf untested", v)
		}
		if got := VerdictOf(v); got != expected {
			t.Errorf("VerdictOf(%s) = %s, want %s", v, got, expected)
		}
	}
}

func TestAnUnsetLedgerVerdictIsFatal(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("VerdictOf(unset) did not panic: the impossible state is now reachable")
		}
	}()
	_ = VerdictOf(ledger.VerdictUnset)
}

func TestEveryLedgerModeIsAccountedFor(t *testing.T) {
	known := map[ledger.Mode]bool{
		ledger.ModeUnknown:  true,
		ledger.ModeShadow:   true,
		ledger.ModeEnforced: true,
	}
	for _, m := range ledger.AllModes() {
		if !known[m] {
			t.Errorf("ledger mode %s has no counterpart in the gate, so a row carrying it reads back as a mode nothing maps", m)
		}
	}
}
