package motion

import (
	"cmp"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"tofu/internal/konst"
)

type ComparedTake struct {
	Label  string
	TakeID string
	Frames int
	Table  []string
}

type Comparison struct {
	Takes    []ComparedTake
	Sheets   []Sheet
	Warnings []string
}

type labelledTake struct {
	Take
	label string
	tiles []tile
}

func mustMatch(a, b Manifest) error {
	for _, field := range []struct {
		name string
		get  func(Manifest) any
	}{
		{"url", func(m Manifest) any { return m.Scenario.URL }},
		{"viewport", func(m Manifest) any { return m.Scenario.Viewport }},
		{"reduced_motion", func(m Manifest) any { return m.Scenario.ReducedMotion }},
		{"setup", func(m Manifest) any { return m.Scenario.Setup }},
		{"trigger", func(m Manifest) any { return m.Scenario.Trigger }},
		{"browser", func(m Manifest) any { return m.Browser }},
	} {
		if va, vb := field.get(a), field.get(b); !reflect.DeepEqual(va, vb) {
			return fmt.Errorf("%s differs: %s has %+v, %s has %+v; capture again with the same scenario", field.name, a.TakeID, va, b.TakeID, vb)
		}
	}
	return nil
}

func Compare(before, after []Take, from, to *float64, crop *Crop) (Comparison, error) {
	var got Comparison
	if len(before) == 0 || len(after) == 0 {
		return got, errors.New("compare needs at least one take before and one after")
	}
	w, err := WindowOf(append(slices.Clone(before), after...), from, to)
	if err != nil {
		return got, err
	}
	var takes []labelledTake
	for i, t := range before {
		takes = append(takes, labelledTake{Take: t, label: fmt.Sprintf("before %d", i+1), tiles: w.tiles(t)})
	}
	for i, t := range after {
		takes = append(takes, labelledTake{Take: t, label: fmt.Sprintf("after %d", i+1), tiles: w.tiles(t)})
	}
	ref := takes[0].Manifest
	watchDiffers := false
	blinked := blinking(append(slices.Clone(before), after...))
	for _, t := range takes {
		if err := mustMatch(ref, t.Manifest); err != nil {
			return got, err
		}
		watchDiffers = watchDiffers || !reflect.DeepEqual(t.Manifest.Scenario.Watch, ref.Scenario.Watch)
		table, _ := t.table(blinked, w)
		got.Takes = append(got.Takes, ComparedTake{Label: t.label, TakeID: t.Manifest.TakeID, Frames: len(t.tiles), Table: table})
	}
	if watchDiffers {
		got.Warnings = append(got.Warnings, "watch lists differ, so the tables may have different columns")
	}
	if len(before) < konst.MotionCompareTakesAdvised || len(after) < konst.MotionCompareTakesAdvised {
		got.Warnings = append(got.Warnings, fmt.Sprintf("fewer than %d takes on a side, so a one-frame defect can be missed", konst.MotionCompareTakesAdvised))
	}
	revisions := map[App]bool{}
	for _, t := range before {
		revisions[t.Manifest.App] = true
	}
	if !slices.ContainsFunc(after, func(t Take) bool { return !revisions[t.Manifest.App] }) {
		got.Warnings = append(got.Warnings, "before and after have the same app revision and diff, so the after takes may not include the change")
	}
	sheets, warning, err := compareSheets(takes, ref.Scenario.Viewport, crop)
	got.Sheets = sheets
	if warning != "" {
		got.Warnings = append(got.Warnings, warning)
	}
	return got, err
}

func compareSheets(takes []labelledTake, vp Viewport, crop *Crop) ([]Sheet, string, error) {
	type placed struct {
		tile
		take int
	}
	var merged []placed
	for k, t := range takes {
		for _, tl := range t.tiles {
			merged = append(merged, placed{tile: tl, take: k})
		}
	}
	if len(merged) == 0 {
		return nil, "", nil
	}
	slices.SortStableFunc(merged, func(a, b placed) int { return cmp.Compare(a.ms, b.ms) })
	lay, warning, err := layoutFor(merged[0].path, vp, crop, konst.MotionCompareTilePx, konst.MotionCompareColumns, konst.MotionCompareLabelScale)
	if err != nil {
		return nil, "", err
	}
	var cuts [][][]tile
	var spans [][2]float64
	for _, p := range merged {
		if len(cuts) == 0 || len(cuts[len(cuts)-1][p.take]) == lay.cols {
			cuts = append(cuts, make([][]tile, len(takes)))
			spans = append(spans, [2]float64{p.ms, p.ms})
		}
		cut := cuts[len(cuts)-1]
		cut[p.take] = append(cut[p.take], p.tile)
		spans[len(spans)-1][1] = p.ms
	}
	var sheets []Sheet
	for c, cut := range cuts {
		for k := range cut {
			if len(cut[k]) == 0 {
				cut[k] = []tile{{}}
			}
		}
		rendered, err := lay.sheets(cut)
		if err != nil {
			return nil, "", err
		}
		for _, r := range rendered {
			s := Sheet{PNG: r.png, FromMs: spans[c][0], ToMs: spans[c][1]}
			for _, k := range r.rows {
				s.Rows = append(s.Rows, takes[k].label)
				if cut[k][0].path != "" {
					s.Frames += len(cut[k])
				}
			}
			sheets = append(sheets, s)
		}
	}
	return sheets, warning, nil
}
