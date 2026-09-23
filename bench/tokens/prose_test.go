package tokens

import (
	"strings"
	"testing"

	"tofu/bench/schemas"
)

func TestMeasureProseSplitsEveryToolIntoDescriptionAndParameters(t *testing.T) {
	defs := schemas.FullRegistry(t.TempDir()).Definitions()
	whole := schemas.Measure(defs)
	prose := MeasureProse(defs, whole)
	if len(prose.Tools) != len(defs) {
		t.Fatalf("got %d per-tool rows, want %d", len(prose.Tools), len(defs))
	}
	for _, tp := range prose.Tools {
		if tp.DescBytes <= 0 {
			t.Fatalf("tool %s carries no description bytes", tp.Name)
		}
		if tp.ParamBytes <= 0 {
			t.Fatalf("tool %s carries no parameter bytes", tp.Name)
		}
		if tp.DescBytes+tp.ParamBytes > tp.SchemaBytes {
			t.Fatalf("tool %s: description %d plus parameters %d exceeds its own schema bytes %d", tp.Name, tp.DescBytes, tp.ParamBytes, tp.SchemaBytes)
		}
	}
	if prose.StructuralBytes <= 0 {
		t.Fatalf("structural bytes should be positive, got %d", prose.StructuralBytes)
	}
}

func TestMeasureProseAgreesWithTheSchemasWireFigure(t *testing.T) {
	defs := schemas.FullRegistry(t.TempDir()).Definitions()
	whole := schemas.Measure(defs)
	prose := MeasureProse(defs, whole)
	if prose.SchemaBytes != whole.WireBytes {
		t.Fatalf("got %d schema bytes, want the wire figure %d", prose.SchemaBytes, whole.WireBytes)
	}
	if prose.DescBytes+prose.ParamBytes+prose.StructuralBytes != prose.SchemaBytes {
		t.Fatalf("description %d plus parameters %d plus structural %d should sum to the whole %d",
			prose.DescBytes, prose.ParamBytes, prose.StructuralBytes, prose.SchemaBytes)
	}
}

func TestMeasureProseIsDeterministicAcrossTwoRuns(t *testing.T) {
	defs := schemas.FullRegistry(t.TempDir()).Definitions()
	whole := schemas.Measure(defs)
	first := MeasureProse(defs, whole)
	second := MeasureProse(defs, whole)
	if first.DescBytes != second.DescBytes || first.ParamBytes != second.ParamBytes {
		t.Fatalf("two runs disagreed: %d/%d against %d/%d", first.DescBytes, first.ParamBytes, second.DescBytes, second.ParamBytes)
	}
}

func TestProseReportRendersTheRequiredSections(t *testing.T) {
	defs := schemas.FullRegistry(t.TempDir()).Definitions()
	whole := schemas.Measure(defs)
	prose := MeasureProse(defs, whole)
	report := ProseReport{
		Date:      "2026-09-23",
		Machine:   "DESKTOP-AHUN9RO",
		BuildNote: "tofu's own claude-sub credential, wire anthropic",
		ToolCount: schemas.FullToolCount,
		Whole:     whole,
		Prose:     prose,
		TestCount: 5,
	}
	rendered := report.Render()
	if rendered == "" {
		t.Fatal("the report rendered empty")
	}
	for _, section := range []string{"## What was counted", "## Per-tool split", "## Against their number"} {
		if !strings.Contains(rendered, section) {
			t.Fatalf("the report is missing the %q section", section)
		}
	}
}
