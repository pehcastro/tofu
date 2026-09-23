package anthropic

import (
	"bytes"
	"encoding/json"
	"testing"

	"tofu/internal/llm"
)

const (
	injectedSystemPrefixBytesBound   = 256
	realisticSystemPrefixBytesBound  = 2300
	varyingSystemPrefixBytesPerToken = 4
)

const realisticSystemPromptFloor = "[tool_guidance, from tofu itself]\n" +
	"you are working inside one directory. every path you name is relative to it and nothing above it exists. " +
	"prefer the tool that does the thing over a shell command that imitates it: " +
	"project_report answers what is this repository in one call, glob finds files by name, " +
	"search finds text and returns the whole declaration a match sits inside, read reads one file whole, " +
	"edit replaces one exact stretch of text inside one, and write creates one or replaces it whole. " +
	"never run find, ls -R, du or wc over the tree. " +
	"find descends into every directory git ignores, because -not -path filters what it prints and does not stop it walking, " +
	"so one recorded run on this kind of tree spent 68 seconds on a find and then 153 seconds on another, " +
	"while glob answered its question in 15 milliseconds. " +
	"bash is for the project's own commands, its package manager, its build and its tests, " +
	"and for nothing one of those tools already does, and a command it runs is killed at its deadline and its output is lost. " +
	"never write a throwaway script to change a file: that is what edit is. " +
	"tofu_lint_comments, tofu_rules_check and tofu_judge run tofu's own checks with tofu's own parser, " +
	"so use one of those rather than a shell command or a guess in prose when you want to know whether the work holds up. " +
	"spawn hands one piece of work to a child with its own context and its own list of paths it may write, " +
	"and returns what the child did rather than its transcript: use it when a piece of the task is separable and its paths do not overlap another child's.\n\n" +
	"[format_contract, from tofu itself]\n" +
	"finish by saying what changed, naming every file you wrote, " +
	"and pasting the real output of every command you ran. " +
	"a command you did not run is not evidence, and a claim with no command behind it is one the next reader has to redo. " +
	"say what you could not verify and why in the same answer rather than leaving it out."

func fingerprintedFirstMessage(charAtIndex7 byte) string {
	body := []byte("0123456x0123456789012345")
	body[7] = charAtIndex7
	return string(body)
}

func requestWithFirstMessage(content string, system []string) Request {
	request := minimalRequest()
	request.Messages = []llm.Message{{Role: llm.RoleUser, Content: content}}
	request.System = system
	return request
}

func encodedSystemPrefix(t *testing.T, request Request) []byte {
	t.Helper()
	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var decoded struct {
		System json.RawMessage `json:"system"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return decoded.System
}

func assertPrefixesDifferAndReport(t *testing.T, prefixA, prefixB []byte, bound int, label string) {
	t.Helper()
	if len(prefixA) != len(prefixB) {
		t.Fatalf("%s: prefix lengths differ: %d vs %d, the measurement assumes equal length", label, len(prefixA), len(prefixB))
	}
	if bytes.Equal(prefixA, prefixB) {
		t.Fatalf("%s: two first messages that differ at a fingerprinted index produced an identical system prefix", label)
	}

	measuredBytes := len(prefixA)
	estimatedTokens := measuredBytes / varyingSystemPrefixBytesPerToken
	t.Logf("%s: %d bytes, %d estimated tokens", label, measuredBytes, estimatedTokens)

	if measuredBytes > bound {
		t.Fatalf("%s: grew to %d bytes, past the %d byte bound", label, measuredBytes, bound)
	}
}

func TestVaryingSystemPrefixBytesOnAMinimalRequest(t *testing.T) {
	prefixA := encodedSystemPrefix(t, requestWithFirstMessage(fingerprintedFirstMessage('a'), nil))
	prefixB := encodedSystemPrefix(t, requestWithFirstMessage(fingerprintedFirstMessage('b'), nil))
	assertPrefixesDifferAndReport(t, prefixA, prefixB, injectedSystemPrefixBytesBound, "injected block only, no caller system prompt")
}

func TestVaryingSystemPrefixBytesWithTheCallersSystemPrompt(t *testing.T) {
	prefixA := encodedSystemPrefix(t, requestWithFirstMessage(fingerprintedFirstMessage('a'), []string{realisticSystemPromptFloor}))
	prefixB := encodedSystemPrefix(t, requestWithFirstMessage(fingerprintedFirstMessage('b'), []string{realisticSystemPromptFloor}))
	assertPrefixesDifferAndReport(t, prefixA, prefixB, realisticSystemPrefixBytesBound, "with the caller's tool guidance and format contract")
}
