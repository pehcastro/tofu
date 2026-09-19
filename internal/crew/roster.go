package crew

import "fmt"

type CollisionError struct {
	Child      string
	Glob       string
	Holder     string
	HolderGlob string
}

func (e CollisionError) Error() string {
	return fmt.Sprintf("%s cannot hold %q: %s already holds %q and the two overlap", e.Child, e.Glob, e.Holder, e.HolderGlob)
}

type hold struct {
	child string
	owns  []string
}

type Roster struct {
	holds []hold
}

func (r *Roster) Hold(child string, owns []string) error {
	for _, glob := range owns {
		if err := validGlob(glob); err != nil {
			return err
		}
		for _, held := range r.holds {
			for _, other := range held.owns {
				if overlap(glob, other) {
					return CollisionError{Child: child, Glob: glob, Holder: held.child, HolderGlob: other}
				}
			}
		}
	}
	r.holds = append(r.holds, hold{child: child, owns: owns})
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

func overlap(a, b string) bool {
	left, right := globTokens(normalizePath(a)), globTokens(normalizePath(b))
	memo := make(map[[2]int]bool, len(left)*len(right))
	var sharesAPath func(i, j int) bool
	sharesAPath = func(i, j int) bool {
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
			answer = sharesAPath(i+1, j) || sharesAPath(i, j+1)
		case right[j].kind == tokenTree:
			answer = sharesAPath(i, j+1) || sharesAPath(i+1, j)
		case left[i].kind == tokenSegment:
			answer = sharesAPath(i+1, j) || (canEmitASegmentChar(right[j]) && sharesAPath(i, j+1))
		case right[j].kind == tokenSegment:
			answer = sharesAPath(i, j+1) || (canEmitASegmentChar(left[i]) && sharesAPath(i+1, j))
		default:
			answer = left[i].char == right[j].char && sharesAPath(i+1, j+1)
		}
		memo[[2]int{i, j}] = answer
		return answer
	}
	return sharesAPath(0, 0)
}
