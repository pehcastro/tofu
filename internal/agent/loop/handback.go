package loop

import (
	"strings"

	"tofu/internal/konst"
	"tofu/internal/tools/agents/done"
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

func unmetNote(unmet []string) string {
	return "the turn does not end here, because " + done.Name + " reads these acceptance lines as no:\n" + strings.Join(unmet, "\n") +
		"\nthis is your one grace turn: fix what each check found, run the check again, and call " + done.Name + " with the new call id before you end. " +
		"a line you cannot meet is reported as unmet, never as met."
}

func handbackNote(closing string) string {
	return "you ended the turn by handing the person a step: \"" + closing + "\". " +
		"if you can take that step yourself, take it now and report what came of it, rather than offering it. " +
		"when only the person can settle it, such as spending money, a matter of taste, something that cannot be undone, or access you do not have, " +
		"your reply already put it to them: end the turn now in one line saying it waits on them, without repeating the question or its options."
}
