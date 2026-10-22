package repeat

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/typesafe"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/learn/words"
	"tofu/internal/sys"
	"tofu/internal/transport"
)

type metered struct {
	jev.Wire
	tokens *int
}

func (m metered) Post(ctx context.Context, body []byte) (jev.Raw, error) {
	raw, err := m.Wire.Post(ctx, body)
	if response, decoded := jev.Decode(raw.Body); err == nil && decoded == nil {
		*m.tokens += response.Usage.InputTokens
	}
	return raw, err
}

func TestEvalCorpus(t *testing.T) {
	corpus := os.Getenv("TOFU_REPEAT_EVAL")
	if corpus == "" {
		t.Skip("TOFU_REPEAT_EVAL names no corpus")
	}
	sys.AllowLiveCredential(t)
	raw, err := os.ReadFile(corpus)
	if err != nil {
		t.Fatal(err)
	}
	var eval struct {
		Items []struct {
			N      int   `json:"n"`
			Chain  int   `json:"chain"`
			Repeat bool  `json:"repeat"`
			Accept []int `json:"accept"`
		} `json:"items"`
		Chain []string `json:"chain"`
	}
	if err := json.Unmarshal(raw, &eval); err != nil {
		t.Fatal(err)
	}
	key, err := jev.TypeSafeKey(sys.CredentialFileName)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := typesafe.New(typesafe.Config{Key: key, Transport: transport.Config{
		AttemptTimeout: konst.JudgeTimeoutMillis * time.Millisecond,
		Retries:        konst.JudgeRetries,
		Backoff:        konst.JudgeBackoffMillis * time.Millisecond,
		Concurrency:    1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	tokens := 0
	client, err := jev.NewClient(jev.Config{Wire: metered{Wire: wire, tokens: &tokens}})
	if err != nil {
		t.Fatal(err)
	}
	repeats, caught, falseNotes, requests := 0, 0, 0, 0
	table := []string{"n\tlabel\truns\tnoted\tscore"}
	for _, item := range eval.Items {
		log := t.TempDir()
		judge, err := New(t.TempDir(), log)
		if err != nil {
			t.Fatal(err)
		}
		judge.client = client
		earlier := eval.Chain[:item.Chain]
		picked := words.Candidates(earlier, eval.Chain[item.Chain])
		given := make([]Earlier, len(picked))
		for i, at := range picked {
			given[i].Text = earlier[at]
		}
		found, err := judge.Repeats(context.Background(), eval.Chain[item.Chain], given, nil)
		if err != nil {
			t.Fatal(err)
		}
		var runs []string
		if _, err := ledger.NewReader(log).Each(ledger.Filter{}, func(row ledger.Row) error {
			requests++
			at, named := strings.CutPrefix(row.Reason.Question, repeatsName+"_")
			if !named {
				runs = append(runs, "-")
				return nil
			}
			i, err := strconv.Atoi(at)
			runs = append(runs, strconv.Itoa(picked[i]))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		noted, score := -1, ""
		if found.Earlier >= 0 {
			noted = picked[found.Earlier]
		}
		right := item.Repeat && slices.Contains(item.Accept, noted)
		switch {
		case right:
			caught, score = caught+1, "caught"
		case noted >= 0:
			falseNotes, score = falseNotes+1, "false"
		case item.Repeat:
			score = "missed"
		}
		if item.Repeat {
			repeats++
		}
		table = append(table, strconv.Itoa(item.N)+"\t"+strconv.FormatBool(item.Repeat)+"\t"+strings.Join(runs, ",")+"\t"+strconv.Itoa(noted)+"\t"+score)
	}
	t.Log("\n" + strings.Join(table, "\n"))
	t.Logf("caught %d of %d repeats, %d false notes, %d messages, %d requests, %d input tokens, usd %.4f",
		caught, repeats, falseNotes, len(eval.Items), requests, tokens, float64(tokens)*typesafe.DollarsPerInputToken)
}
