package subagent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEveryCompletionAndBucketHasItsOwnName(t *testing.T) {
	seen := map[string]bool{}
	for completion := Done; completion < completionCount; completion++ {
		name := completion.String()
		if name == "" || seen[name] {
			t.Fatalf("completion %d reads %q, and a repeated or empty name hides one state behind another", completion, name)
		}
		seen[name] = true
	}
	if len(seen) != 4 {
		t.Fatalf("there are %d completions and the contract is four: done, done with concerns, blocked, needs context", len(seen))
	}
	for bucket := ActOn; bucket < bucketCount; bucket++ {
		name := bucket.String()
		if name == "" || seen[name] {
			t.Fatalf("bucket %d reads %q and it is not its own name", bucket, name)
		}
		seen[name] = true
	}
	t.Logf("names: %v", seen)
}

func TestAnUnknownCompletionOrBucketPanicsByName(t *testing.T) {
	for _, c := range []struct {
		what  string
		name  func() string
		wants string
	}{
		{"completion", Completion(9).String, "unknown completion 9"},
		{"past the last completion", completionCount.String, "unknown completion 4"},
		{"bucket", Bucket(7).String, "unknown finding bucket 7"},
	} {
		func() {
			defer func() {
				raised, isText := recover().(string)
				if !isText || !strings.Contains(raised, c.wants) {
					t.Fatalf("an unknown %s raised %v, and it has to name the value: %q", c.what, raised, c.wants)
				}
			}()
			_ = c.name()
		}()
	}
}

func TestConcernsSeparatesADefectFromANit(t *testing.T) {
	for bucket, concerns := range map[Bucket]bool{ActOn: true, Consider: true, Noted: false, Dismissed: false} {
		if bucket.Concerns() != concerns {
			t.Fatalf("%s concerns the parent: %v, want %v", bucket, bucket.Concerns(), concerns)
		}
	}
}

func TestACompletionAndABucketCrossJSONAsTheirNames(t *testing.T) {
	finding := Finding{Bucket: Consider, Reason: "the output was short of what it asked for"}
	written, err := json.Marshal(struct {
		Completion Completion `json:"completion"`
		Finding    Finding    `json:"finding"`
	}{DoneWithConcerns, finding})
	if err != nil {
		t.Fatalf("marshalling the handback: %v", err)
	}
	if !strings.Contains(string(written), `"completion":"done_with_concerns"`) || !strings.Contains(string(written), `"bucket":"consider"`) {
		t.Fatalf("the handback carries a number where a parent needs a name: %s", written)
	}

	var read struct {
		Completion Completion `json:"completion"`
		Finding    Finding    `json:"finding"`
	}
	if err := json.Unmarshal(written, &read); err != nil {
		t.Fatalf("reading the handback back: %v", err)
	}
	if read.Completion != DoneWithConcerns || read.Finding != finding {
		t.Fatalf("the handback read back as %+v", read)
	}

	if err := json.Unmarshal([]byte(`{"completion":"mostly_done"}`), &read); err == nil {
		t.Fatal("a completion this build does not know was accepted, so a parent would read it as done")
	} else {
		t.Logf("refused: %v", err)
	}
	if err := json.Unmarshal([]byte(`{"finding":{"bucket":"maybe"}}`), &read); err == nil {
		t.Fatal("an unknown bucket was accepted")
	}
}
