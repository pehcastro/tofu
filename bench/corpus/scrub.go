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
	{"F:/", "R:/"},
	{"Luiz", "owner"},
}

var separatorsInOrderOfLength = []string{`\\`, `\`, "/"}

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
