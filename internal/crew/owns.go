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

type DeniedError struct {
	Path string
	Owns []string
}

func (e DeniedError) Error() string {
	return fmt.Sprintf("%q is outside the paths this agent holds (%s): report it, do not edit it", e.Path, strings.Join(e.Owns, ", "))
}

var globCharset = regexp.MustCompile(`^[A-Za-z0-9_./*-]+$`)

func normalizePath(path string) string {
	return strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
}

func validGlob(glob string) error {
	if glob == "" || !globCharset.MatchString(glob) {
		return UnparseableGlobError{Glob: glob}
	}
	return nil
}

func Allow(path string, owns []string) error {
	matched, err := Matches(path, owns)
	if err != nil {
		return err
	}
	if !matched {
		return DeniedError{Path: path, Owns: owns}
	}
	return nil
}

func Matches(path string, owns []string) (bool, error) {
	target := normalizePath(path)
	for _, glob := range owns {
		if err := validGlob(glob); err != nil {
			return false, err
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
