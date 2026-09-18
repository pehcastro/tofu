package cost

import "regexp"

var (
	regexExfil = regexp.MustCompile(`(?i)curl\s+[^\n]*-d\s+@|curl\s+[^\n]*\|\s*sh`)
	regexForce = regexp.MustCompile(`(?i)push\s+.*--force|--force\b|force-push`)
	regexRM    = regexp.MustCompile(`(?i)\brm\s+-rf\b`)
	regexWord  = regexp.MustCompile(`(?i)\bforce\b`)
)

func RegexDecide(command, userMessage string) Verdict {
	switch {
	case regexExfil.MatchString(command):
		return Block
	case regexForce.MatchString(command):
		if regexWord.MatchString(userMessage) {
			return Proceed
		}
		return Block
	case regexRM.MatchString(command):
		return Block
	default:
		return Proceed
	}
}

func extractCommand(state any) string {
	root, ok := state.(map[string]any)
	if !ok {
		return ""
	}
	input, ok := root["input"].(map[string]any)
	if !ok {
		return ""
	}
	command, _ := input["command"].(string)
	return command
}

func extractUserMessage(state any) string {
	root, ok := state.(map[string]any)
	if !ok {
		return ""
	}
	context, ok := root["context"].(map[string]any)
	if !ok {
		return ""
	}
	messages, ok := context["user_recent_messages"].([]any)
	if !ok || len(messages) == 0 {
		return ""
	}
	message, _ := messages[0].(string)
	return message
}
