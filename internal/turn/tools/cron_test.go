package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"tofu/internal/cron"
)

func TestCronToolChangesAJobAsTheAgent(t *testing.T) {
	now := time.Date(2026, 10, 5, 14, 30, 0, 0, time.Local)
	book := &cron.Book{}
	if _, err := book.Create(cron.Spec{Schedule: "every 1m", Prompt: "check the build"}, cron.Person, "made by /loop", now); err != nil {
		t.Fatal(err)
	}
	var told []string
	tool := Cron{Book: book, Now: func() time.Time { return now }, Told: func(said string) { told = append(told, said) }}
	call := func(args map[string]any) error {
		raw, _ := json.Marshal(args)
		_, err := tool.Run(context.Background(), raw)
		return err
	}
	if err := call(map[string]any{"action": "update", "id": "c1", "schedule": "every 10m", "reason": "five fires found the build unchanged"}); err != nil {
		t.Fatal(err)
	}
	job, _ := book.Job("c1")
	if last := job.Versions[len(job.Versions)-1]; last.By != cron.Agent || last.Reason != "five fires found the build unchanged" || last.Spec.Schedule != "every 10m" {
		t.Errorf("the tool's update wrote %+v, want every 10m by agent with its reason", last)
	}
	if len(told) != 1 || !strings.Contains(told[0], "by agent") {
		t.Errorf("the person was told %q, want one line naming the agent", told)
	}
	for name, args := range map[string]map[string]any{
		"no reason":          {"action": "update", "id": "c1", "schedule": "every 20m"},
		"a month of life":    {"action": "update", "id": "c1", "expires_in": "30d", "reason": "keep it"},
		"an unknown action":  {"action": "rewrite", "id": "c1", "reason": "x"},
		"a job that is gone": {"action": "pause", "id": "c9", "reason": "x"},
	} {
		if err := call(args); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if job, _ = book.Job("c1"); job.Version() != 2 || len(told) != 1 {
		t.Errorf("a refused call changed the job or told the person: v%d, %q", job.Version(), told)
	}
}
