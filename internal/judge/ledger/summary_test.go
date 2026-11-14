package ledger

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func filledRow(v reflect.Value) {
	switch v.Type() {
	case reflect.TypeFor[time.Time]():
		v.Set(reflect.ValueOf(time.Date(2026, 11, 14, 1, 2, 3, 0, time.UTC)))
		return
	case reflect.TypeFor[json.RawMessage]():
		v.SetBytes([]byte(`{"a":[1,"b",null]}`))
		return
	case reflect.TypeFor[Verdict]():
		v.SetString("allow")
		return
	case reflect.TypeFor[Mode]():
		v.SetString("shadow")
		return
	case reflect.TypeFor[AnswerKind]():
		v.SetString("choice")
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Int, reflect.Int64:
		v.SetInt(1)
	case reflect.Float64:
		v.SetFloat(0.5)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		filledRow(v.Elem())
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		filledRow(v.Index(0))
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				filledRow(v.Field(i))
			}
		}
	}
}

func eachMutation(node any, set func(any), emit func()) {
	for _, probe := range []any{"s", json.Number("5"), json.Number("1.5"), true, nil, map[string]any{}, []any{}, json.Number("1e400"), json.Number("-1"), "2026-13-01", []any{nil}, []any{json.Number("5")}, map[string]any{"option": json.Number("5")}} {
		set(probe)
		emit()
	}
	set(node)
	switch typed := node.(type) {
	case map[string]any:
		for _, key := range slices.Sorted(maps.Keys(typed)) {
			eachMutation(typed[key], func(v any) { typed[key] = v }, emit)
			typed[strings.ToUpper(key)] = "s"
			emit()
			delete(typed, strings.ToUpper(key))
		}
	case []any:
		for i, child := range typed {
			eachMutation(child, func(v any) { typed[i] = v }, emit)
		}
	}
}

func TestSummaryRejectsEveryLineTheFullDecodeRejects(t *testing.T) {
	var row Row
	filledRow(reflect.ValueOf(&row).Elem())
	base, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseRow(base); err != nil {
		t.Fatalf("the filled row does not parse: %v", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(base)))
	decoder.UseNumber()
	var tree map[string]any
	if err := decoder.Decode(&tree); err != nil {
		t.Fatal(err)
	}
	lines := [][]byte{base}
	eachMutation(tree, func(any) {}, func() {
		line, err := json.Marshal(tree)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	})
	stamp := `"at":"2026-11-14T01:02:03Z","schema":9`
	for _, hand := range []string{
		`{"id": "cut off`,
		`{"answers":[{"question":"risk","kind":"guess","choice":"low"}],` + stamp + `}`,
		`{"answers":[{"question":"risk","wording":"one","kind":"choice","choice":"low"}],` + stamp + `}`,
		`{"answers":[{"question":"risk","Wording":"one","kind":"choice","choice":"low"}],` + stamp + `}`,
		`{"answers":[null],` + stamp + `}`,
		`{"answers":[{"question":"risk","choice":"0.4"}],` + stamp + `}`,
		`{"answers":[{"question":"risk","choice":"high"}],` + stamp + `}`,
		`{"latency_ms":1.5,` + stamp + `}`,
		`{"state":"just text",` + stamp + `}`,
		`{"state":null,"reason":null,"usage":null,"answers":null,"verdict":null,` + stamp + `}`,
		`{"schema":10,"at":"2026-11-14T01:02:03Z"}`,
		`{"schema":0,"at":"2026-11-14T01:02:03Z"}`,
		`{"at":"yesterday","schema":9}`,
		`[]`,
		`"row"`,
		`{` + stamp + `} trailing`,
	} {
		lines = append(lines, []byte(hand))
	}
	dir := t.TempDir()
	day := filepath.Join(dir, "2026-11-14.jsonl")
	mismatched, rejected := 0, 0
	for _, line := range lines {
		if err := os.WriteFile(day, append(line, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		stats, err := Summary(dir)
		if err != nil {
			t.Fatal(err)
		}
		_, full := parseRow(line)
		if full != nil {
			rejected++
		}
		if (full != nil) != (stats.Corrupt == 1) || stats.Corrupt+stats.Rows != 1 {
			mismatched++
			t.Errorf("full decode error %v, summary %d unreadable %d readable: %s", full, stats.Corrupt, stats.Rows, line)
		}
	}
	t.Logf("%d lines, %d rejected by the full decode, %d where summary disagrees", len(lines), rejected, mismatched)
}
