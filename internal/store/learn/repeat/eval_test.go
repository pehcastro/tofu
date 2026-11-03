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
	"tofu/internal/store/learn/words"
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

type keptScores struct {
	standing float64
	kept     map[string]float64
	covers   []string
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
			Said   string   `json:"said"`
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
	var memory []keptScores
	var thresholds Thresholds
	repeats, caught, falseNotes, covered := 0, 0, 0, 0
	table := []string{"n\tlabel\truns\tnoted\tscore\tkept scores\tkept noted\tkept label"}
	for _, item := range eval.Items {
		log := t.TempDir()
		judge, err := New(t.TempDir(), log)
		if err != nil {
			t.Fatal(err)
		}
		judge.client, thresholds = client, judge.thresholds
		said, earlier := item.Said, []string(nil)
		if said == "" {
			said, earlier = eval.Chain[item.Chain], eval.Chain[:item.Chain]
		}
		picked := words.Candidates(earlier, said)
		given := make([]Earlier, len(picked))
		for i, at := range picked {
			given[i].Text = earlier[at]
		}
		var near []Kept
		for _, at := range KeptNear(said, eval.Kept, konst.MemorySharedStemsToAskKept) {
			near = append(near, eval.Kept[at])
		}
		found, err := judge.Repeats(context.Background(), said, given, near)
		if err != nil {
			t.Fatal(err)
		}
		keptNoted := "-"
		if found.Kept >= 0 {
			keptNoted = near[found.Kept].ID + "@" + found.Row
		}
		one := keptScores{kept: map[string]float64{}, covers: item.Kept}
		var runs, shown []string
		if _, err := ledger.NewReader(log).Each(ledger.Filter{}, func(row ledger.Row) error {
			one.standing = row.Answers[0].Noul
			for _, answer := range row.Answers {
				if at, isKept := strings.CutPrefix(answer.Question, keptName+"_"); isKept {
					i, err := strconv.Atoi(at)
					if err != nil {
						return err
					}
					one.kept[near[i].ID] = answer.Noul
					shown = append(shown, near[i].ID+"="+strconv.FormatFloat(answer.Noul, 'f', 3, 64))
				}
			}
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
		memory = append(memory, one)
		if len(item.Kept) > 0 {
			covered++
		}
		noted, score := -1, ""
		if found.Earlier >= 0 {
			noted = picked[found.Earlier]
		}
		switch {
		case item.Repeat && slices.Contains(item.Accept, noted):
			caught, score = caught+1, "caught"
		case noted >= 0:
			falseNotes, score = falseNotes+1, "false"
		case item.Repeat:
			score = "missed"
		}
		if item.Repeat {
			repeats++
		}
		table = append(table, strconv.Itoa(item.N)+"\t"+strconv.FormatBool(item.Repeat)+"\t"+strings.Join(runs, ",")+"\t"+strconv.Itoa(noted)+"\t"+score+"\t"+strings.Join(shown, ",")+"\t"+keptNoted+"\t"+strings.Join(item.Kept, ","))
	}
	t.Logf("kept entries sharing at least %d stems with said\n%s", konst.MemorySharedStemsToAskKept, strings.Join(table, "\n"))
	t.Logf("caught %d of %d repeats, %d false notes; %d messages, %d input tokens, usd %.4f", caught, repeats, falseNotes, len(eval.Items), tokens, float64(tokens)*typesafe.DollarsPerInputToken)
	sweep := []string{"kept_at\tnotes\tright\twrong\tprecision\trecall of " + strconv.Itoa(covered)}
	for _, at := range []float64{0.5, 0.6, 0.7, 0.8, 0.85, 0.9, 0.95, 0.97, 0.99, thresholds.Kept} {
		right, wrong := 0, 0
		for _, one := range memory {
			best, value := "", 0.0
			for id, score := range one.kept {
				if one.standing >= thresholds.Standing && score >= at && score > value {
					best, value = id, score
				}
			}
			switch {
			case best == "":
			case slices.Contains(one.covers, best):
				right++
			default:
				wrong++
			}
		}
		precision := 1.0
		if right+wrong > 0 {
			precision = float64(right) / float64(right+wrong)
		}
		sweep = append(sweep, strconv.FormatFloat(at, 'f', 2, 64)+"\t"+strconv.Itoa(right+wrong)+"\t"+strconv.Itoa(right)+"\t"+strconv.Itoa(wrong)+"\t"+strconv.FormatFloat(precision, 'f', 2, 64)+"\t"+strconv.FormatFloat(float64(right)/float64(covered), 'f', 2, 64))
	}
	t.Logf("memory match over %d messages, standing at or above %.2f, the last row is kept_at as shipped\n%s", len(memory), thresholds.Standing, strings.Join(sweep, "\n"))
}
