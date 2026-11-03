package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/agent/loop"
	"tofu/internal/store/session"
)

func pythonAsk(code string) turn.GateRequest {
	args, _ := json.Marshal(map[string]string{"command": `python -c "` + code + `"`})
	return turn.GateRequest{Tool: "bash", Args: args}
}

func TestAnAlwaysOnInterpreterCodeCoversThatCodeAloneAndNeverReachesTheFile(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	first, _ := New(Config{Dir: project})
	here, err := first.OpenFresh()
	if err != nil {
		t.Fatal(err)
	}
	answeredStanding(t, first, pythonAsk("import os; print(1)"), AlwaysHere)
	answeredStanding(t, first, pythonAsk("import os; print(9)"), NeverHere)
	if answer := unasked(t, first, pythonAsk("import os; print(1)")); answer != turn.PersonAlwaysHere {
		t.Errorf("the exact code allowed always answered %v, want it run unasked", answer)
	}
	if answer := answeredStanding(t, first, pythonAsk("import os; print(2)"), Denied); answer != turn.PersonDenied {
		t.Errorf("different code after an always on python -c answered %v without asking", answer)
	}
	store, err := session.OpenIn(project)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(store.Dir(here), "standing.json")
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var kept []StandingAnswer
	if err := json.Unmarshal(body, &kept); err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].Decision != RejectAlways || !turn.PlacesCode(kept[0].Target) {
		t.Fatalf("standing.json holds %s, want only the reject_always on code", body)
	}
	first.Close()

	planted := strings.Replace(string(body), string(RejectAlways), string(AllowAlways), 1)
	if err := os.WriteFile(file, []byte(planted), 0o600); err != nil {
		t.Fatal(err)
	}
	second, _ := New(Config{Dir: project, Resumed: Carry{Session: here}})
	defer second.Close()
	if answer := answeredStanding(t, second, pythonAsk("import os; print(9)"), Denied); answer != turn.PersonDenied {
		t.Errorf("an allow on code written into standing.json answered %v without asking", answer)
	}
}
