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

const liveModel = "jev-latest"

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
			N      int      `json:"n"`
			Chain  int      `json:"chain"`
			Repeat bool     `json:"repeat"`
			Accept []int    `json:"accept"`
			Kept   []string `json:"kept"`
		} `json:"items"`
		Chain []string `json:"chain"`
		Kept  []Kept   `json:"kept"`
	}
	if err := json.Unmarshal(raw, &eval); err != nil {
		t.Fatal(err)
	}
	key, err := jev.TypeSafeKey(sys.CredentialFileName)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := typesafe.New(liveModel, typesafe.Config{Key: key, Transport: transport.Config{
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
	for _, floor := range []int{0, konst.MemorySharedStemsToAskKept} {
		repeats, caught, falseNotes, requests := 0, 0, 0, 0
		keptRight, keptWrong, keptMissed, keptAsked := 0, 0, 0, 0
		table := []string{"n\tlabel\truns\tnoted\tscore\tkept asked\tkept noted\tkept score"}
		for _, item := range eval.Items {
			log := t.TempDir()
			judge, err := New(t.TempDir(), log)
			if err != nil {
				t.Fatal(err)
			}
			judge.client = client
			said, earlier := eval.Chain[item.Chain], eval.Chain[:item.Chain]
			picked := words.Candidates(earlier, said)
			given := make([]Earlier, len(picked))
			for i, at := range picked {
				given[i].Text = earlier[at]
			}
			var near []Kept
			for _, at := range KeptNear(said, eval.Kept, floor) {
				near = append(near, eval.Kept[at])
			}
			keptAsked += len(near)
			found, err := judge.Repeats(context.Background(), said, given, near)
			if err != nil {
				t.Fatal(err)
			}
			keptNoted, keptScore := "-", ""
			if found.Kept >= 0 {
				keptNoted = near[found.Kept].ID
			}
			switch {
			case slices.Contains(item.Kept, keptNoted):
				keptRight, keptScore = keptRight+1, "right"
			case keptNoted != "-":
				keptWrong, keptScore = keptWrong+1, "wrong"
			case len(item.Kept) > 0:
				keptMissed, keptScore = keptMissed+1, "missed"
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
			table = append(table, strconv.Itoa(item.N)+"\t"+strconv.FormatBool(item.Repeat)+"\t"+strings.Join(runs, ",")+"\t"+strconv.Itoa(noted)+"\t"+score+"\t"+strconv.Itoa(len(near))+"\t"+keptNoted+"\t"+keptScore)
		}
		t.Logf("kept entries sharing at least %d stems with said\n%s", floor, strings.Join(table, "\n"))
		t.Logf("floor %d: caught %d of %d repeats, %d false notes; memory matches %d right, %d wrong, %d missed, %d entries asked; %d messages, %d requests, %d input tokens so far, usd %.4f",
			floor, caught, repeats, falseNotes, keptRight, keptWrong, keptMissed, keptAsked, len(eval.Items), requests, tokens, float64(tokens)*typesafe.DollarsPerInputToken)
	}
}
