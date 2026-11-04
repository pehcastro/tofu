package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"tofu/apps/cli/output"
	loginverb "tofu/apps/cli/verbs/login"
	"tofu/internal/konst"
	"tofu/internal/sys"
	"tofu/internal/testkit/cli"
	"tofu/internal/testkit/golden"
)

func TestLoginStatusJSONIsOneEnvelopeOfCodes(t *testing.T) {
	var out bytes.Buffer
	t.Run("in a scratch home", func(t *testing.T) {
		cli.ScratchProject(t)
		for _, variable := range sys.KeyNames() {
			t.Setenv(variable, "")
		}
		savedAccount(t)
		storeGateKey(t, madeUpGateKey)
		var errOut bytes.Buffer
		if code := loginverb.StatusVerb([]string{output.JSONFlag}, &out, &errOut, fixtureMoment(), nil); code != output.ExitOK {
			t.Fatalf("login --status --json exited %d:\n%s", code, errOut.String())
		}
	})
	golden.Assert(t, "status-report.json.golden", strings.ReplaceAll(out.String(), konst.Version, "VERSION"))
}

func TestDisableJSONIsOneEnvelope(t *testing.T) {
	cli.ScratchProject(t)
	savedAccount(t)
	var out, errOut bytes.Buffer
	if code := loginverb.Verb([]string{"--disable", "1", output.JSONFlag}, nil, &out, &errOut); code != output.ExitOK {
		t.Fatalf("login --disable 1 --json exited %d:\n%s", code, errOut.String())
	}
	type accountReceipt struct {
		ID      int64  `json:"id"`
		Source  string `json:"source"`
		Account string `json:"account"`
		Change  string `json:"change"`
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
	want := accountReceipt{ID: 1, Source: "claude-sub", Account: "ada@example.com", Change: "set_aside"}
	if document.Verb != "login --disable" || !document.OK || document.Data != want {
		t.Fatalf("login --disable --json said %+v", document)
	}
}
