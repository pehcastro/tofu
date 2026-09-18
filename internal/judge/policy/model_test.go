package policy

import "testing"

func TestUnknownVerdictIsFatal(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Verdict(\"bogus\").String() did not panic")
		}
	}()
	_ = Verdict("bogus").String()
}

func TestKnownVerdictsPrint(t *testing.T) {
	for _, v := range []Verdict{VerdictAllow, VerdictAsk, VerdictDeny} {
		if v.String() == "" {
			t.Errorf("Verdict(%q).String() is empty", v)
		}
	}
}
