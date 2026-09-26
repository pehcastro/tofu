package ledger

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"tofu/internal/sys"
)

func realLedgerDir() string { return sys.RecordedStateDir("log") }

func TestSchemaBumpedForTheFingerprint(t *testing.T) {
	if SchemaVersion < FingerprintSchema {
		t.Fatalf("schema version = %d, below %d, where the fingerprint arrived", SchemaVersion, FingerprintSchema)
	}
	if FingerprintSchema <= StateBodySchema {
		t.Fatalf("the fingerprint schema is %d and the state body schema is %d, an added field moves the version forward", FingerprintSchema, StateBodySchema)
	}
}

func fingerprintFitsSchema(row Row) error {
	if row.Fingerprint != "" && row.Schema < FingerprintSchema {
		return fmt.Errorf("row %s is at schema %d, below the schema that added the field, and carries fingerprint %q", row.ID, row.Schema, row.Fingerprint)
	}
	return nil
}

func gateRowCarriesAFingerprint(row Row) error {
	if err := fingerprintFitsSchema(row); err != nil {
		return err
	}
	if row.Schema >= FingerprintSchema && row.Fingerprint == "" {
		return fmt.Errorf("row %s is at schema %d, where the gate records a fingerprint, and carries none", row.ID, row.Schema)
	}
	return nil
}

func TestTheFingerprintIsReadAgainstTheSchemaThatAddedIt(t *testing.T) {
	cases := []struct {
		name    string
		row     Row
		claim   func(Row) error
		refusal string
	}{
		{name: "the gate at the fingerprint schema, carrying one", row: Row{ID: "01", Schema: FingerprintSchema, Fingerprint: "bash.b7c1f0"}, claim: gateRowCarriesAFingerprint},
		{name: "below it, carrying none", row: Row{ID: "02", Schema: StateBodySchema}, claim: fingerprintFitsSchema},
		{name: "at the fingerprint schema, carrying none, written by a point that computes none", row: Row{ID: "03", Schema: FingerprintSchema}, claim: fingerprintFitsSchema},
		{name: "the gate at the fingerprint schema, carrying none", row: Row{ID: "04", Schema: FingerprintSchema}, claim: gateRowCarriesAFingerprint, refusal: "carries none"},
		{name: "below it, carrying one", row: Row{ID: "05", Schema: StateBodySchema, Fingerprint: "bash.b7c1f0"}, claim: fingerprintFitsSchema, refusal: "below the schema that added the field"},
	}
	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			err := each.claim(each.row)
			if each.refusal == "" {
				if err != nil {
					t.Fatalf("a row a writer in this tree produces was refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("a row no writer produces passed, want a refusal naming %q", each.refusal)
			}
			if !strings.Contains(err.Error(), each.refusal) {
				t.Fatalf("the refusal does not say %q: %v", each.refusal, err)
			}
		})
	}
}

func TestEveryRowOnDiskReadsAndItsFingerprintFitsItsSchema(t *testing.T) {
	if _, err := os.Stat(realLedgerDir()); err != nil {
		t.Skipf("skipped, not counted as a pass: this machine has no ledger at %s (%v)", realLedgerDir(), err)
	}
	bySchema := map[int]int{}
	withFingerprint := 0
	report, err := NewReader(realLedgerDir()).Each(Filter{}, func(row Row) error {
		bySchema[row.Schema]++
		if row.Fingerprint != "" {
			withFingerprint++
		}
		return fingerprintFitsSchema(row)
	})
	if err != nil {
		t.Fatalf("walking the real ledger at %s: %v", realLedgerDir(), err)
	}
	if report.Scanned == 0 {
		t.Fatalf("%s holds no rows, so this proves nothing", realLedgerDir())
	}
	if len(report.Corrupt) > 0 {
		t.Fatalf("%d of %d rows stopped reading, first: %+v", len(report.Corrupt), report.Scanned, report.Corrupt[0])
	}
	t.Logf("%d rows across %d files read, none corrupt, %d carry a fingerprint; by schema: %v", report.Scanned, report.Files, withFingerprint, bySchema)
}
