package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/golden"
	"tofu/internal/konst"
	"tofu/internal/sys"
)

func TestLoginStatusJSONIsOneEnvelopeOfCodes(t *testing.T) {
	var out bytes.Buffer
	t.Run("in a scratch home", func(t *testing.T) {
		scratchProject(t)
		for _, variable := range sys.KeyNames() {
			t.Setenv(variable, "")
		}
		savedAccount(t)
		storeGateKey(t, madeUpGateKey)
		var errOut bytes.Buffer
		if code := statusVerb([]string{jsonFlag}, &out, &errOut, fixtureMoment(), nil); code != exitOK {
			t.Fatalf("login --status --json exited %d:\n%s", code, errOut.String())
		}
	})
	golden.Assert(t, "status-report.json.golden", strings.ReplaceAll(out.String(), konst.Version, "VERSION"))
}

func TestDisableJSONIsOneEnvelope(t *testing.T) {
	scratchProject(t)
	savedAccount(t)
	var out, errOut bytes.Buffer
	if code := loginVerb([]string{"--disable", "1", jsonFlag}, nil, &out, &errOut); code != exitOK {
		t.Fatalf("login --disable 1 --json exited %d:\n%s", code, errOut.String())
	}
	var document struct {
		Verb string
		OK   bool
		Data accountReceipt
	}
	decoder := json.NewDecoder(&out)
	if err := decoder.Decode(&document); err != nil || decoder.More() {
		t.Fatalf("stdout is not one document (%v):\n%s", err, out.String())
	}
	want := accountReceipt{ID: 1, Source: "claude-sub", Account: "ada@example.com", Change: changeSetAside}
	if document.Verb != "login --disable" || !document.OK || document.Data != want {
		t.Fatalf("login --disable --json said %+v", document)
	}
}
