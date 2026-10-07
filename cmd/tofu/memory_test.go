package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type listedMemory struct {
	Scopes []struct {
		Scope   string `json:"scope"`
		Dir     string `json:"dir"`
		Bytes   int    `json:"bytes"`
		Limit   int    `json:"limit"`
		Entries []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
			Text string `json:"text"`
			Said string `json:"said"`
		} `json:"entries"`
	} `json:"scopes"`
}

func tofuMemory(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(append([]string{"memory"}, args...), strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

func memoryListing(t *testing.T) listedMemory {
	t.Helper()
	code, out, errOut := tofuMemory(t, "list", jsonFlag)
	var envelope struct {
		Data listedMemory `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); code != exitOK || err != nil || len(envelope.Data.Scopes) != 2 {
		t.Fatalf("memory list --json exited %d, %v, %d scopes: %s %s", code, err, len(envelope.Data.Scopes), out, errOut)
	}
	return envelope.Data
}

func TestMemoryKeepsEachScopeApartRefusesPastTheLimitAndUndoes(t *testing.T) {
	project := chdirTemp(t)
	home := os.Getenv("USERPROFILE")
	said := "remember: never run cargo with more than 2 jobs\nand say \"why\""

	code, out, errOut := tofuMemory(t, "add", "--global", "--said", said, "Cargo runs with at most 2 jobs.")
	if code != exitOK || !strings.Contains(out, "tofu memory remove --global m1") {
		t.Fatalf("add --global exited %d, out %q, err %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(home, ".tofu", "memory", "m1.yaml")); err != nil {
		t.Fatalf("the global entry is not in the home: %v", err)
	}
	code, out, errOut = tofuMemory(t, "add", "--kind", "reference", "The canvas runtime is not this repository's.")
	if code != exitOK || !strings.Contains(out, "tofu memory remove m2") {
		t.Fatalf("add to the project exited %d, out %q, err %q, want a new id m2 across both scopes", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(project, ".tofu")); err == nil {
		t.Errorf("project memory was written inside the project, not beside its sessions in the home")
	}

	listed := memoryListing(t)
	global, local := listed.Scopes[0], listed.Scopes[1]
	if global.Scope != "global" || local.Scope != "project" || len(global.Entries) != 1 || len(local.Entries) != 1 {
		t.Fatalf("the listing is %+v, want one global entry then one project entry", listed)
	}
	if global.Entries[0].Said != said || global.Entries[0].Kind != "person" || local.Entries[0].Kind != "reference" {
		t.Errorf("the entries read back as %+v and %+v, want the words kept exactly and each kind kept", global.Entries[0], local.Entries[0])
	}
	if !strings.HasPrefix(local.Dir, filepath.Join(home, ".tofu", "projects")) || global.Limit != 4096 || global.Bytes == 0 {
		t.Errorf("the project scope lives at %s with %d of %d bytes, want under the home projects folder and a counted size", local.Dir, global.Bytes, global.Limit)
	}

	long := strings.Repeat("Every reply stays short and plain. ", 30)
	sizeBefore := global.Bytes
	for range 4 {
		code, _, errOut = tofuMemory(t, "add", "--global", long)
		if code != exitOK {
			break
		}
	}
	if code == exitOK || !strings.Contains(errOut, "4096") || !strings.Contains(errOut, "m1") || !strings.Contains(errOut, "tofu memory remove") {
		t.Fatalf("a write past 4096 bytes exited %d, err %q, want a refusal naming the limit, the entries and the remove command", code, errOut)
	}
	after := memoryListing(t)
	if after.Scopes[0].Bytes > 4096 || after.Scopes[0].Bytes <= sizeBefore {
		t.Errorf("the global scope holds %d bytes after the refusal, want more than %d and at most 4096", after.Scopes[0].Bytes, sizeBefore)
	}
	if after.Scopes[1].Bytes != local.Bytes {
		t.Errorf("a full global scope changed the project scope from %d to %d bytes", local.Bytes, after.Scopes[1].Bytes)
	}

	code, _, errOut = tofuMemory(t, "add", "--global", "--replace", "m1", strings.Repeat("x", 30))
	if code != exitOK {
		t.Errorf("replacing m1 with a shorter statement in a full scope exited %d: %s", code, errOut)
	}

	if code, _, errOut = tofuMemory(t, "remove", "m1"); code == exitOK {
		t.Errorf("removing the global m1 from the project scope succeeded, want a refusal: %s", errOut)
	}
	code, out, errOut = tofuMemory(t, "remove", "--global", "m1")
	if code != exitOK || !strings.Contains(out, "tofu memory add --global") {
		t.Fatalf("remove --global m1 exited %d, out %q, err %q, want an undo that adds it back", code, out, errOut)
	}
	if code, _, _ = tofuMemory(t, "remove", "--global", "m1"); code == exitOK {
		t.Errorf("a second remove of m1 succeeded")
	}
	code, out, _ = tofuMemory(t, "add", "--global", "short")
	id := regexp.MustCompile(`remove --global (m\d+)`).FindStringSubmatch(out)
	if code != exitOK || id == nil || id[1] == "m1" || id[1] == "m2" {
		t.Errorf("an add after a remove reused an id: %q", out)
	}
}
