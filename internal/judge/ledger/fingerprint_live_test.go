package ledger

import (
	"os"
	"testing"
)

const realLedgerDir = "../../../.tofu/log"

func TestSchemaBumpedForTheFingerprint(t *testing.T) {
	if SchemaVersion != FingerprintSchema {
		t.Fatalf("schema version = %d, want %d, the fingerprint arrived there", SchemaVersion, FingerprintSchema)
	}
	if FingerprintSchema <= StateBodySchema {
		t.Fatalf("the fingerprint schema is %d and the state body schema is %d, an added field moves the version forward", FingerprintSchema, StateBodySchema)
	}
}

func TestEveryRowOnDiskReadsAndCarriesNoFingerprint(t *testing.T) {
	if _, err := os.Stat(realLedgerDir); err != nil {
		t.Skipf("skipped, not counted as a pass: this machine has no ledger at %s (%v)", realLedgerDir, err)
	}
	bySchema := map[int]int{}
	report, err := NewReader(realLedgerDir).Each(Filter{}, func(row Row) error {
		bySchema[row.Schema]++
		if row.Fingerprint != "" {
			t.Fatalf("row %s was written before the fingerprint existed and carries %q", row.ID, row.Fingerprint)
		}
		if row.Schema >= FingerprintSchema {
			t.Fatalf("row %s claims schema %d, which this ticket has only just defined", row.ID, row.Schema)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reading the real ledger: %v", err)
	}
	if report.Scanned == 0 {
		t.Fatalf("%s holds no rows, so this proves nothing", realLedgerDir)
	}
	if len(report.Corrupt) > 0 {
		t.Fatalf("%d of %d rows stopped reading after the schema bump, first: %+v", len(report.Corrupt), report.Scanned, report.Corrupt[0])
	}
	t.Logf("%d rows across %d files still read, none corrupt, every fingerprint absent; by schema: %v", report.Scanned, report.Files, bySchema)
}
