package instruction

import (
	"regexp"
	"strings"
)

var DirectivePatterns = []string{
	`\bmust not\b`,
	`\bdo not\b`,
	`\bdon't\b`,
	`\bnever\b`,
	`\byou must\b`,
	`\byou should\b`,
	`\bplease\b`,
	`\bignore (the |all )?(previous|prior) instructions?\b`,
	`\bdisregard\b`,
	`\bfrom now on\b`,
	`\bcall [a-z_]+ with\b`,
	`\brun (the|this) (command|script)\b`,
	`\b(important|caution|warning|note)\s*:`,
	`\byou are now\b`,
	`\boverride\b`,
}

var directive = regexp.MustCompile(`(?i)(` + strings.Join(DirectivePatterns, "|") + `)`)

func ArmRegex(content string) bool {
	return directive.MatchString(content)
}
