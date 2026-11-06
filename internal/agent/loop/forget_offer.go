package loop

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"tofu/internal/agent/context/recall"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/tools/agents/forget"
)

func forgetOffered(message core.Message) bool {
	return message.Role == core.RoleUser && message.Origin.Source == core.SourceNotice && message.Origin.Name == forget.Name
}

func forgetShareTokens(budget recall.Budget) int {
	return int(float64(budget.Ceiling()) * konst.ContextForgetOfferShare)
}

func forgetOffer(messages []core.Message, estimate recall.Config, used, ceiling int) string {
	breakpoint := len(messages)
	for breakpoint > 0 && messages[breakpoint-1].Role != core.RoleAssistant {
		breakpoint--
	}
	largest := func(among []core.Message) string {
		var biggest core.Message
		for _, message := range among {
			if message.Role == core.RoleTool && len(message.Content) > len(biggest.Content) {
				biggest = message
			}
		}
		if biggest.ToolCallID == "" {
			return ""
		}
		called := ""
		for _, message := range messages[:breakpoint] {
			if at := slices.IndexFunc(message.ToolCalls, func(call core.ToolCall) bool { return call.ID == biggest.ToolCallID }); at >= 0 {
				var args struct{ Path, Command string }
				_ = json.Unmarshal(message.ToolCalls[at].Arguments, &args)
				shown, _, _ := strings.Cut(cmp.Or(args.Path, args.Command), "\n")
				called = strings.TrimSpace(message.ToolCalls[at].Name + " " + shown[:min(len(shown), konst.ContextLabelChars)])
			}
		}
		return fmt.Sprintf("call %s, %s, %d bytes, about %d tokens", biggest.ToolCallID, called, len(biggest.Content), estimate.Tokens(biggest.Content))
	}
	said := fmt.Sprintf("your context passed %.0f%% of its %d token ceiling, about %d tokens. ", konst.ContextForgetOfferShare*100, ceiling, used)
	if stale := largest(messages[:max(breakpoint-1, 0)]); stale != "" {
		said += "the largest result an earlier request showed you is " + stale + "; " + forget.Name + " cannot drop it now, since it sits before the cache breakpoint, so keep what you still need from it with note and compaction trims it when the window needs room. "
	} else {
		said += "no result an earlier request showed you is in it, so nothing stale is named. "
	}
	if fresh := largest(messages[breakpoint:]); fresh != "" {
		return said + "the largest result " + forget.Name + " can still drop is " + fresh + ": your next reply is the only one that can drop it, so read it now, keep what you still need from it with note, and call " + forget.Name + " with that call id in that same reply. keep it if you will need it whole: reading it again costs more than keeping it."
	}
	return said + "this request carries no new result, so " + forget.Name + " has nothing to drop."
}
