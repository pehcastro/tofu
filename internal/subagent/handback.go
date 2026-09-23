package subagent

import (
	"encoding/json"
	"fmt"
	"strconv"
)

type Completion int

const (
	Done Completion = iota
	DoneWithConcerns
	Blocked
	NeedsContext
	completionCount
)

func (c Completion) String() string {
	switch c {
	case Done:
		return "done"
	case DoneWithConcerns:
		return "done_with_concerns"
	case Blocked:
		return "blocked"
	case NeedsContext:
		return "needs_context"
	}
	panic("subagent: unknown completion " + strconv.Itoa(int(c)))
}

type Bucket int

const (
	ActOn Bucket = iota
	Consider
	Noted
	Dismissed
	bucketCount
)

func (b Bucket) String() string {
	switch b {
	case ActOn:
		return "act_on"
	case Consider:
		return "consider"
	case Noted:
		return "noted"
	case Dismissed:
		return "dismissed"
	}
	panic("subagent: unknown finding bucket " + strconv.Itoa(int(b)))
}

func (b Bucket) Concerns() bool { return b == ActOn || b == Consider }

type Finding struct {
	Bucket Bucket `json:"bucket"`
	Reason string `json:"reason"`
}

type Attempt struct {
	ID      string `json:"id"`
	Tried   string `json:"tried"`
	Outcome string `json:"outcome"`
}

type named interface {
	~int
	fmt.Stringer
}

func byName[T named](data []byte, past T, kind string) (T, error) {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return 0, err
	}
	for candidate := T(0); candidate < past; candidate++ {
		if candidate.String() == text {
			return candidate, nil
		}
	}
	return 0, fmt.Errorf("subagent: %q is not a %s this build knows", text, kind)
}

func (c Completion) MarshalJSON() ([]byte, error) { return json.Marshal(c.String()) }

func (c *Completion) UnmarshalJSON(data []byte) error {
	read, err := byName(data, completionCount, "completion")
	if err != nil {
		return err
	}
	*c = read
	return nil
}

func (b Bucket) MarshalJSON() ([]byte, error) { return json.Marshal(b.String()) }

func (b *Bucket) UnmarshalJSON(data []byte) error {
	read, err := byName(data, bucketCount, "finding bucket")
	if err != nil {
		return err
	}
	*b = read
	return nil
}
