package turn

import (
	"strings"

	"tofu/internal/konst"
)

func handbackOffers() []string {
	return []string{
		"want me to", "do you want me", "would you like me to", "shall i", "should i ", "or should i", "if you'd like", "if you want", "i can do that",
		"let me know if", "let me know when", "tell me if", "tell me when", "tell me whether", "say the word", "rather i hold",
		"your call", "decision for you", "until you decide", "ok to proceed", "okay to proceed",
	}
}

func handedBack(reply string) string {
	text := strings.TrimSpace(reply)
	closing := strings.TrimSpace(text[strings.LastIndex(text, "\n\n")+1:])
	lower := strings.ToLower(strings.ReplaceAll(closing, "’", "'"))
	for _, offer := range handbackOffers() {
		if strings.Contains(lower, offer) {
			return runeSafeHead(closing, konst.LeadHandbackQuoteBytes)
		}
	}
	return ""
}

func pollNote(open []string) string {
	return "your " + AskPersonToolName + " questions are still open, and the person can answer them after this turn:\n" + strings.Join(open, "\n") +
		"\nend your report with them as a poll: each question, its options, and the option you took for now."
}

func handbackNote(closing string, tools Registry) string {
	ask := "then ask that one question, and the turn ends."
	if _, offered := tools.byName[AskPersonToolName]; offered {
		ask = "then put that one question to the person with " + AskPersonToolName + ", with its options and the one you recommend, and keep working."
	}
	return "you ended the turn by handing the person a step: \"" + closing + "\". " +
		"if you can take that step yourself, take it now and report what came of it, rather than offering it. " +
		"end on a question only when nothing but the person can settle it: spending money, a matter of taste, something that cannot be undone, " +
		"or access you do not have. " + ask
}
