package crew

import (
	"fmt"
	"regexp"
	"strings"
)

type UnparseableGlobError struct {
	Glob string
}

func (e UnparseableGlobError) Error() string {
	return fmt.Sprintf("owns glob %q has a character boji's matcher does not understand", e.Glob)
}

var globCharset = regexp.MustCompile(`^[A-Za-z0-9_./*-]+$`)

func normalizePath(path string) string {
	return strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
}

func Matches(path string, owns []string) (bool, error) {
	target := normalizePath(path)
	for _, glob := range owns {
		if glob == "" || !globCharset.MatchString(glob) {
			return false, UnparseableGlobError{Glob: glob}
		}
		pattern := regexp.QuoteMeta(normalizePath(glob))
		pattern = strings.ReplaceAll(pattern, `\*\*`, `.*`)
		pattern = strings.ReplaceAll(pattern, `\*`, `[^/]*`)
		re, err := regexp.Compile("^" + pattern + "$")
		if err != nil {
			return false, err
		}
		if re.MatchString(target) {
			return true, nil
		}
	}
	return false, nil
}
