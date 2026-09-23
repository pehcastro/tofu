package secret

import "strings"

type substitution struct {
	real     string
	scrubbed string
}

var identitySubstitutions = []substitution{
	{"pehcastro@gmail.com", "owner@example.com"},
	{"F--localhost-ephem-sh-bob", "R--work-ephem-sh-bob"},
	{"C:/Users/Luiz", "C:/Users/owner"},
	{"F:/localhost", "R:/work"},
	{"/f/localhost", "/r/work"},
	{"F:/", "R:/"},
	{"Luiz", "owner"},
	{"pehcastro", "owner-handle"},
}

var separatorsInOrderOfLength = []string{`\\`, `\`, "/"}

var credentialSubstitutions = []substitution{
	{"sk-or-v1-", "redacted-openrouter-key-"},
	{"sk-ant-api", "redacted-anthropic-api-key"},
	{"sk-ant-oat", "redacted-anthropic-oauth-token"},
	{"sk-proj-", "redacted-openai-key-"},
	{"ghp_", "redacted-github-token-"},
	{"gho_", "redacted-github-oauth-token-"},
	{"github_pat_", "redacted-github-pat-"},
	{"AKIA", "redacted-aws-key-"},
	{"xoxb-", "redacted-slack-bot-token-"},
	{"xoxp-", "redacted-slack-user-token-"},
	{"eyJhbGciOi", "redacted-jwt-algorithm-"},
	{"eyJ0eXAiOi", "redacted-jwt-type-"},
	{"-----BEGIN ", "redacted-pem-block "},
}

func Scrub(text string) string {
	for _, substitution := range identitySubstitutions {
		for _, separator := range separatorsInOrderOfLength {
			from := strings.ReplaceAll(substitution.real, "/", separator)
			to := strings.ReplaceAll(substitution.scrubbed, "/", separator)
			text = strings.ReplaceAll(text, from, to)
		}
	}
	for _, substitution := range credentialSubstitutions {
		text = strings.ReplaceAll(text, substitution.real, substitution.scrubbed)
	}
	return text
}

func LeaksIn(text string) []string {
	var found []string
	for _, substitution := range identitySubstitutions {
		for _, separator := range separatorsInOrderOfLength {
			identity := strings.ReplaceAll(substitution.real, "/", separator)
			if strings.Contains(text, identity) {
				found = append(found, identity)
				break
			}
		}
	}
	return append(found, CredentialsIn(text)...)
}

func CredentialsIn(text string) []string {
	var found []string
	for _, substitution := range credentialSubstitutions {
		if strings.Contains(text, substitution.real) {
			found = append(found, substitution.real)
		}
	}
	return found
}
