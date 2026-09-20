package corpus

import "strings"

type identitySubstitution struct {
	real     string
	scrubbed string
}

var identitySubstitutions = []identitySubstitution{
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

var credentialMarkers = []string{
	"sk-or-v1-",
	"sk-ant-api",
	"sk-ant-oat",
	"sk-proj-",
	"ghp_",
	"gho_",
	"github_pat_",
	"AKIA",
	"xoxb-",
	"xoxp-",
	"eyJhbGciOi",
	"eyJ0eXAiOi",
	"-----BEGIN ",
}

func Scrub(text string) string {
	for _, substitution := range identitySubstitutions {
		for _, separator := range separatorsInOrderOfLength {
			from := strings.ReplaceAll(substitution.real, "/", separator)
			to := strings.ReplaceAll(substitution.scrubbed, "/", separator)
			text = strings.ReplaceAll(text, from, to)
		}
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
	for _, marker := range credentialMarkers {
		if strings.Contains(text, marker) {
			found = append(found, marker)
		}
	}
	return found
}
