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

func NormalizePath(path string) string {
	return strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
}

func validateGlob(glob string) error {
	if glob == "" || !globCharset.MatchString(glob) {
		return UnparseableGlobError{Glob: glob}
	}
	return nil
}

func Matches(path string, owns []string) (bool, error) {
	target := NormalizePath(path)
	for _, glob := range owns {
		if err := validateGlob(glob); err != nil {
			return false, err
		}
		pattern := regexp.QuoteMeta(NormalizePath(glob))
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

func globSegments(glob string) ([]string, error) {
	if err := validateGlob(glob); err != nil {
		return nil, err
	}
	return strings.Split(NormalizePath(glob), "/"), nil
}

func segmentsOverlap(a, b string) bool {
	dp := make([][]bool, len(a)+1)
	for i := range dp {
		dp[i] = make([]bool, len(b)+1)
	}
	dp[0][0] = true
	for i := 1; i <= len(a); i++ {
		dp[i][0] = a[i-1] == '*' && dp[i-1][0]
	}
	for j := 1; j <= len(b); j++ {
		dp[0][j] = b[j-1] == '*' && dp[0][j-1]
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			switch {
			case a[i-1] == '*' && b[j-1] == '*':
				dp[i][j] = dp[i-1][j] || dp[i][j-1] || dp[i-1][j-1]
			case a[i-1] == '*':
				dp[i][j] = dp[i-1][j] || dp[i][j-1]
			case b[j-1] == '*':
				dp[i][j] = dp[i][j-1] || dp[i-1][j]
			default:
				dp[i][j] = a[i-1] == b[j-1] && dp[i-1][j-1]
			}
		}
	}
	return dp[len(a)][len(b)]
}

func pathSegmentsOverlap(a, b []string) bool {
	dp := make([][]bool, len(a)+1)
	for i := range dp {
		dp[i] = make([]bool, len(b)+1)
	}
	dp[0][0] = true
	for i := 1; i <= len(a); i++ {
		dp[i][0] = a[i-1] == "**" && dp[i-1][0]
	}
	for j := 1; j <= len(b); j++ {
		dp[0][j] = b[j-1] == "**" && dp[0][j-1]
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			switch {
			case a[i-1] == "**" && b[j-1] == "**":
				dp[i][j] = dp[i-1][j] || dp[i][j-1] || dp[i-1][j-1]
			case a[i-1] == "**":
				dp[i][j] = dp[i-1][j] || dp[i][j-1]
			case b[j-1] == "**":
				dp[i][j] = dp[i][j-1] || dp[i-1][j]
			default:
				dp[i][j] = segmentsOverlap(a[i-1], b[j-1]) && dp[i-1][j-1]
			}
		}
	}
	return dp[len(a)][len(b)]
}

func Overlap(a, b []string) (bool, error) {
	segsA := make([][]string, len(a))
	for i, glob := range a {
		segs, err := globSegments(glob)
		if err != nil {
			return false, err
		}
		segsA[i] = segs
	}
	segsB := make([][]string, len(b))
	for i, glob := range b {
		segs, err := globSegments(glob)
		if err != nil {
			return false, err
		}
		segsB[i] = segs
	}
	for _, sa := range segsA {
		for _, sb := range segsB {
			if pathSegmentsOverlap(sa, sb) {
				return true, nil
			}
		}
	}
	return false, nil
}
