package state

import "testing"

func TestSubjectReadsTheToolAndItsTargetFromARecordedState(t *testing.T) {
	for _, c := range []struct {
		state string
		want  Subject
	}{
		{`{"agent":"","tool":"bash","input":{"command":"git status","timeout":30},"cwd":"/p"}`, Subject{Tool: "bash", Command: "git status"}},
		{`{"tool":"edit","input":{"path":"a.go","old":"x"}}`, Subject{Tool: "edit", Path: "a.go"}},
		{`{"tool":"web_fetch","input":{"url":"https://example.com"}}`, Subject{Tool: "web_fetch", URL: "https://example.com"}},
		{`{"tool":"odd","input":{"command":42,"path":["a"]}}`, Subject{Tool: "odd"}},
		{`{"task":"stop check state with no tool"}`, Subject{}},
		{`{"task":"t","command":"npm test","exit_code":1,"stream":"stdout","position":"end","chunk":"x"}`, Subject{Command: "npm test"}},
		{`{"tool":"bash","command":"outer","input":{"command":"inner"}}`, Subject{Tool: "bash", Command: "inner"}},
	} {
		got, err := SubjectOf([]byte(c.state))
		if err != nil || got != c.want {
			t.Errorf("subject of %s = %+v, %v, want %+v", c.state, got, err, c.want)
		}
	}
	if _, err := SubjectOf([]byte(`not json`)); err == nil {
		t.Error("a state that is not JSON gave a subject")
	}
}
