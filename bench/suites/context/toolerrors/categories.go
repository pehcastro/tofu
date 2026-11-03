package toolerrors

import (
	"strings"

	"tofu/bench/corpus"
)

type Category string

const (
	BadArguments          Category = "bad arguments"
	NotFoundInEnvironment Category = "not found in this environment"
	Timeout               Category = "timeout"
	RefusedByRule         Category = "refused by a rule"
	ProviderError         Category = "provider error"
	Unknown               Category = "unknown"
)

var Categories = []Category{BadArguments, NotFoundInEnvironment, Timeout, RefusedByRule, ProviderError, Unknown}

const exitCommandNotFound = 127
const exitFoundButNotExecutable = 126

func Failed(call corpus.RecordedCall) bool {
	return call.Error != "" || (call.ExitCode != nil && *call.ExitCode != 0)
}

func Classify(call corpus.RecordedCall) Category {
	if call.Error == "" {
		if code := call.ExitCode; code != nil && (*code == exitCommandNotFound || *code == exitFoundButNotExecutable) {
			return NotFoundInEnvironment
		}
		return Unknown
	}
	text := strings.ToLower(call.Error)
	switch {
	case strings.Contains(text, "killed at its deadline"),
		strings.Contains(text, "context canceled"):
		return Timeout
	case strings.Contains(text, "name the one you mean"),
		strings.Contains(text, "matches no line"),
		strings.Contains(text, "matches ") && strings.Contains(text, "lines"),
		strings.Contains(text, "was asked for occurrence"):
		return BadArguments
	case strings.Contains(text, "is not a file under the working directory"),
		strings.Contains(text, "is not a path under the working directory"),
		strings.Contains(text, "not found in this environment"),
		strings.Contains(text, "found but not executable"):
		return NotFoundInEnvironment
	case strings.Contains(text, "refused"), strings.Contains(text, "denied"), strings.Contains(text, "not allowed"):
		return RefusedByRule
	case strings.Contains(text, "provider"), strings.Contains(text, "upstream"), strings.Contains(text, "rate limit"):
		return ProviderError
	default:
		return Unknown
	}
}
