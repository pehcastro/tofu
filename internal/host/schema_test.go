package host

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

var updatePin = flag.Bool("update", false, "rewrite testdata/schema.json from Schema")

func TestSchemaMatchesPin(t *testing.T) {
	got, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	pin := filepath.Join("testdata", "schema.json")
	if *updatePin {
		if err := os.WriteFile(pin, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(pin)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got, want) {
		return
	}
	var gotTop, wantTop map[string]json.RawMessage
	if json.Unmarshal(got, &gotTop) != nil || json.Unmarshal(want, &wantTop) != nil {
		t.Fatalf("%s is not one json object", pin)
	}
	moved := []string{}
	for _, key := range unionKeys(gotTop, wantTop) {
		if bytes.Equal(gotTop[key], wantTop[key]) {
			continue
		}
		var gotEntries, wantEntries map[string]json.RawMessage
		if json.Unmarshal(gotTop[key], &gotEntries) != nil || json.Unmarshal(wantTop[key], &wantEntries) != nil {
			moved = append(moved, key)
			continue
		}
		for _, name := range unionKeys(gotEntries, wantEntries) {
			if !bytes.Equal(gotEntries[name], wantEntries[name]) {
				moved = append(moved, key+"/"+name)
			}
		}
	}
	t.Fatalf("serve --schema moved from %s at %v, an empty list meaning bytes outside every entry such as line endings", pin, moved)
}

func unionKeys(one, other map[string]json.RawMessage) []string {
	keys := map[string]json.RawMessage{}
	maps.Copy(keys, one)
	maps.Copy(keys, other)
	return slices.Sorted(maps.Keys(keys))
}

func TestSchemaRefusesUnpinnedStruct(t *testing.T) {
	defer func() {
		refusal := fmt.Sprint(recover())
		if !strings.Contains(refusal, "strings.Builder") {
			t.Fatalf("an unpinned struct from another package was named by its path, refusal %q", refusal)
		}
	}()
	schemaDefs{}.of(reflect.TypeFor[strings.Builder]())
}
