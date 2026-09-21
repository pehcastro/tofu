package testquality

type Arm string

const (
	ArmRulesOn  Arm = "on"
	ArmRulesOff Arm = "off"
)

func RuleGuidance() string {
	return "Hold every test you write to three rules. " +
		"No assertion that cannot fail: a test whose only claim is that a value exists or has a type is not a test. " +
		"Mocks at the boundary only: never mock an internal helper, and never assert that an internal method was called in a particular order. " +
		"Every test file covers the empty case and the boundary: nil, zero, empty string, the maximum, the malformed input."
}

func Prompt(description string, arm Arm) string {
	if arm == ArmRulesOn {
		return description + "\n\n" + RuleGuidance()
	}
	return description
}
