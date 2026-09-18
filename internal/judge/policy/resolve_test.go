package policy

import "testing"

func enforcedFixturePolicy() Policy {
	pol := fixturePolicy()
	pol.Mode = ModeEnforced
	pol.ModeDeclared = true
	pol.SampleFloor = 300
	return pol
}

func validLockFor(pol Policy, build string) Lock {
	return Lock{
		Policy:           pol.Name,
		PolicyVersion:    pol.PolicyVersion,
		Questions:        pol.Questions,
		QuestionsVersion: pol.QuestionsVersion,
		Build:            build,
		NFit:             500,
		NVerify:          400,
	}
}

func TestResolveFallsBackToShadowWithNoLockFile(t *testing.T) {
	pol := enforcedFixturePolicy()
	current := Current{Build: "typesafe/jev-1.13-20260917", QuestionsVersion: 1, Known: true}
	res := Resolve(pol, LockLookup{}, current)
	if res.Mode != ModeShadow {
		t.Fatalf("mode = %s, want shadow", res.Mode)
	}
	want := "no lock file for tool_gate@1 at build typesafe/jev-1.13-20260917"
	if res.Reason != want {
		t.Fatalf("reason = %q, want %q", res.Reason, want)
	}
}

func TestResolveFallsBackToShadowOnABuildMismatch(t *testing.T) {
	pol := enforcedFixturePolicy()
	lock := validLockFor(pol, "typesafe/jev-1.12-20260901")
	current := Current{Build: "typesafe/jev-1.13-20260917", QuestionsVersion: 1, Known: true}
	res := Resolve(pol, LockLookup{Present: true, Lock: lock}, current)
	if res.Mode != ModeShadow {
		t.Fatalf("mode = %s, want shadow", res.Mode)
	}
	want := "lock build typesafe/jev-1.12-20260901 does not match current build typesafe/jev-1.13-20260917"
	if res.Reason != want {
		t.Fatalf("reason = %q, want %q", res.Reason, want)
	}
}

func TestResolveFallsBackToShadowOnAQuestionsVersionMismatch(t *testing.T) {
	pol := enforcedFixturePolicy()
	lock := validLockFor(pol, "typesafe/jev-1.13-20260917")
	lock.QuestionsVersion = 2
	current := Current{Build: "typesafe/jev-1.13-20260917", QuestionsVersion: 1, Known: true}
	res := Resolve(pol, LockLookup{Present: true, Lock: lock}, current)
	if res.Mode != ModeShadow {
		t.Fatalf("mode = %s, want shadow", res.Mode)
	}
	want := "lock questions_version 2 does not match current questions_version 1"
	if res.Reason != want {
		t.Fatalf("reason = %q, want %q", res.Reason, want)
	}
}

func TestResolveFallsBackToShadowBelowTheSampleFloor(t *testing.T) {
	pol := enforcedFixturePolicy()
	lock := validLockFor(pol, "typesafe/jev-1.13-20260917")
	lock.NFit, lock.NVerify = 50, 80
	current := Current{Build: "typesafe/jev-1.13-20260917", QuestionsVersion: 1, Known: true}
	res := Resolve(pol, LockLookup{Present: true, Lock: lock}, current)
	if res.Mode != ModeShadow {
		t.Fatalf("mode = %s, want shadow", res.Mode)
	}
	want := "sample size 50 is below the floor 300, .local/research/calibration-design.md §3"
	if res.Reason != want {
		t.Fatalf("reason = %q, want %q", res.Reason, want)
	}
}

func TestResolveEnforcesOnAValidLock(t *testing.T) {
	pol := enforcedFixturePolicy()
	lock := validLockFor(pol, "typesafe/jev-1.13-20260917")
	current := Current{Build: "typesafe/jev-1.13-20260917", QuestionsVersion: 1, Known: true}
	res := Resolve(pol, LockLookup{Present: true, Lock: lock}, current)
	if res.Mode != ModeEnforced {
		t.Fatalf("mode = %s, want enforced, reason %q", res.Mode, res.Reason)
	}
	if res.Reason != "" {
		t.Fatalf("reason = %q, want none", res.Reason)
	}
}

func TestResolveTreatsAnUndeclaredModeAsShadow(t *testing.T) {
	pol := fixturePolicy()
	res := Resolve(pol, LockLookup{}, Current{})
	if res.Mode != ModeShadow {
		t.Fatalf("mode = %s, want shadow", res.Mode)
	}
	if res.Reason != "the policy declares no mode" {
		t.Fatalf("reason = %q", res.Reason)
	}
}

func TestReasonRecordsTheModeAndIsDistinguishableAfterTheFact(t *testing.T) {
	pol := enforcedFixturePolicy()
	answers := neutralAnswers(pol)
	answers[pol.RiskQuestion] = scoreAnswerFixture(3.0)
	_, reason, err := Decide(answers, pol)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}

	shadow := reason
	shadow.Mode = Resolve(pol, LockLookup{}, Current{}).Mode

	lock := validLockFor(pol, "typesafe/jev-1.13-20260917")
	current := Current{Build: lock.Build, QuestionsVersion: pol.QuestionsVersion, Known: true}
	enforced := reason
	enforced.Mode = Resolve(pol, LockLookup{Present: true, Lock: lock}, current).Mode

	if shadow.Mode != ModeShadow {
		t.Fatalf("shadow row mode = %s, want shadow", shadow.Mode)
	}
	if enforced.Mode != ModeEnforced {
		t.Fatalf("enforced row mode = %s, want enforced", enforced.Mode)
	}
	if shadow == enforced {
		t.Fatalf("a shadow row and an enforced row over the same decision are not distinguishable")
	}
	if shadow.Value != enforced.Value || shadow.Comparison != enforced.Comparison || shadow.Threshold != enforced.Threshold {
		t.Fatalf("the mode changed the decision itself: shadow %+v, enforced %+v", shadow, enforced)
	}
}

func TestShadowNeverChangesTheExitCodeOnTheSameDenyState(t *testing.T) {
	if code := ExitCode(ModeEnforced, VerdictDeny); code != 1 {
		t.Fatalf("enforced deny: exit = %d, want 1", code)
	}
	if code := ExitCode(ModeShadow, VerdictDeny); code != 0 {
		t.Fatalf("shadow deny: exit = %d, want 0, shadow must never change the exit code", code)
	}
}

func TestModeStringPanicsOnAnUnknownMode(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Mode(\"bogus\").String() did not panic")
		}
	}()
	_ = Mode("bogus").String()
}
