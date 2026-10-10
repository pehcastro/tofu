package subagent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	gopath "path"
)

type UnparseableGlobError struct {
	Glob string
}

func (e UnparseableGlobError) Error() string {
	return fmt.Sprintf("owns glob %q has a character tofu's matcher does not understand", e.Glob)
}

type UnparseablePathError struct {
	Path string
}

func (e UnparseablePathError) Error() string {
	return fmt.Sprintf("%q is not a path this check can read, so it is refused rather than decided on", e.Path)
}

type EscapingPathError struct {
	Path     string
	Resolved string
}

func (e EscapingPathError) Error() string {
	return fmt.Sprintf("%q resolves to %q, outside the tree this agent works in: refuse it, do not follow it", e.Path, e.Resolved)
}

type DeniedError struct {
	Path string
	Owns []string
}

func (e DeniedError) Error() string {
	return fmt.Sprintf("%q is outside the paths this agent holds (%s): name it in your report, which asks the orchestrator to grant it, and do not edit it another way", e.Path, strings.Join(e.Owns, ", "))
}

var globCharset = regexp.MustCompile(`^[A-Za-z0-9_./*$@+()-]+$`)

func normalizePath(path string) string {
	slashed := strings.ReplaceAll(path, `\`, "/")
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.ToLower(slashed)
	}
	return slashed
}

func validGlob(glob string) error {
	if glob == "" || !globCharset.MatchString(glob) {
		return UnparseableGlobError{Glob: glob}
	}
	return nil
}

func targetPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" || strings.ContainsAny(path, "*?\x00") {
		return "", UnparseablePathError{Path: path}
	}
	link, statErr := os.Lstat(path)
	if statErr != nil || link.Mode()&os.ModeSymlink == 0 {
		named := gopath.Clean(normalizePath(path))
		if named == ".." || strings.HasPrefix(named, "../") {
			return "", EscapingPathError{Path: path, Resolved: named}
		}
		return named, nil
	}
	tree, treeErr := os.Getwd()
	if treeErr == nil {
		tree, treeErr = filepath.EvalSymlinks(tree)
	}
	resolved, linkErr := filepath.EvalSymlinks(path)
	if treeErr != nil || linkErr != nil {
		return "", UnparseablePathError{Path: path}
	}
	inside, err := filepath.Rel(tree, resolved)
	reached := normalizePath(inside)
	if err != nil || reached == ".." || strings.HasPrefix(reached, "../") {
		return "", EscapingPathError{Path: path, Resolved: resolved}
	}
	return reached, nil
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

type tokenKind int

const (
	tokenLiteral tokenKind = iota
	tokenSegment
	tokenTree
)

type globToken struct {
	kind tokenKind
	char byte
}

func globTokens(glob string) []globToken {
	var tokens []globToken
	for i := 0; i < len(glob); i++ {
		switch {
		case glob[i] == '*' && i+1 < len(glob) && glob[i+1] == '*':
			tokens = append(tokens, globToken{kind: tokenTree})
			i++
		case glob[i] == '*':
			tokens = append(tokens, globToken{kind: tokenSegment})
		default:
			tokens = append(tokens, globToken{kind: tokenLiteral, char: glob[i]})
		}
	}
	return tokens
}

func canEmitASegmentChar(token globToken) bool {
	return token.kind != tokenLiteral || token.char != '/'
}

func allWildcards(tokens []globToken) bool {
	for _, token := range tokens {
		if token.kind == tokenLiteral {
			return false
		}
	}
	return true
}

func sharesAPath(a, b string) bool {
	left, right := globTokens(a), globTokens(b)
	memo := make(map[[2]int]bool, len(left)*len(right))
	var reaches func(i, j int) bool
	reaches = func(i, j int) bool {
		if known, seen := memo[[2]int{i, j}]; seen {
			return known
		}
		var answer bool
		switch {
		case i == len(left):
			answer = allWildcards(right[j:])
		case j == len(right):
			answer = allWildcards(left[i:])
		case left[i].kind == tokenTree:
			answer = reaches(i+1, j) || reaches(i, j+1)
		case right[j].kind == tokenTree:
			answer = reaches(i, j+1) || reaches(i+1, j)
		case left[i].kind == tokenSegment:
			answer = reaches(i+1, j) || (canEmitASegmentChar(right[j]) && reaches(i, j+1))
		case right[j].kind == tokenSegment:
			answer = reaches(i, j+1) || (canEmitASegmentChar(left[i]) && reaches(i+1, j))
		default:
			answer = left[i].char == right[j].char && reaches(i+1, j+1)
		}
		memo[[2]int{i, j}] = answer
		return answer
	}
	return reaches(0, 0)
}

func ownedTree(glob string) string {
	if directory, ok := strings.CutSuffix(glob, "/"); ok {
		return directory + "/**"
	}
	if !strings.Contains(glob, "*") && gopath.Ext(glob) == "" {
		return glob + "/**"
	}
	return glob
}

func globForms(glob string) []string {
	if directory := strings.TrimSuffix(glob, "/**"); directory != glob {
		return []string{glob, directory}
	}
	return []string{glob}
}

func ownedDirectory(glob string) (string, bool) {
	if dir, ok := strings.CutSuffix(glob, "/**"); ok {
		return dir, true
	}
	if strings.ContainsAny(glob, "*") {
		return "", false
	}
	return gopath.Dir(glob), true
}

func withinPackageDirectory(glob, target string) bool {
	pattern, ok := strings.CutSuffix(target, "/...")
	if !ok || pattern == "" {
		return false
	}
	dir, ok := ownedDirectory(glob)
	return ok && dir == pattern
}

func Overlap(a, b string) bool {
	return globsMeet(ownedTree(a), ownedTree(b))
}

func globsMeet(a, b string) bool {
	left, right := normalizePath(a), normalizePath(b)
	for _, l := range globForms(left) {
		for _, r := range globForms(right) {
			if sharesAPath(l, r) {
				return true
			}
		}
	}
	return withinPackageDirectory(left, right) || withinPackageDirectory(right, left)
}

func Matches(path string, owns []string) (bool, error) {
	target, err := targetPath(path)
	if err != nil {
		return false, err
	}
	for _, glob := range owns {
		if err := validGlob(glob); err != nil {
			return false, err
		}
		if globsMeet(ownedTree(glob), target) {
			return true, nil
		}
	}
	return false, nil
}
