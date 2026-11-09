package ccore_test

import (
	"testing"

	"github.com/odvcencio/gotreesitter"
	golang "github.com/odvcencio/gotreesitter/grammars/go"
	"github.com/odvcencio/gotreesitter/grammars/javascript"
	"github.com/odvcencio/gotreesitter/grammars/python"
	"github.com/odvcencio/gotreesitter/grammars/rust"
	"github.com/odvcencio/gotreesitter/grammars/tsx"
	"github.com/odvcencio/gotreesitter/grammars/typescript"
	"github.com/odvcencio/gotreesitter/internal/ccore"
)

func compile(states []gotreesitter.LexState) ccore.Lexer {
	var lexStates []ccore.LexState
	var transitions []ccore.LexTransition
	for _, s := range states {
		start := uint32(len(transitions))
		for _, tr := range s.Transitions {
			transitions = append(transitions, ccore.LexTransition{Lo: tr.Lo, Hi: tr.Hi, Next: tr.NextState, Skip: tr.Skip})
		}
		lexStates = append(lexStates, ccore.LexState{EOF: int32(s.EOF), TransStart: start, TransEnd: uint32(len(transitions))})
	}
	return ccore.NewLexer(lexStates, transitions)
}

func newCoreLexers() map[string]ccore.Lexer {
	out := map[string]ccore.Lexer{}
	for name, load := range map[string]func() *gotreesitter.Language{
		"go": golang.LanguageC, "python": python.LanguageC, "rust": rust.LanguageC,
		"typescript": typescript.LanguageC, "tsx": tsx.LanguageC, "javascript": javascript.LanguageC,
	} {
		lang := load()
		out[name+" main"] = compile(lang.LexStates)
		out[name+" keywords"] = compile(lang.KeywordLexStates)
	}
	return out
}

func disagreement(dfa *ccore.Lexer) (state, c int32, found bool) {
	lookaheads := []int32{ccore.InvalidRune, ccore.DFAInvalidRune}
	for b := int32(0); b <= 0xFF; b++ {
		lookaheads = append(lookaheads, b)
	}
	for _, c := range lookaheads {
		for s := range dfa.States {
			gotNext, gotSkip := dfa.Step(int32(s), c)
			wantNext, wantSkip := dfa.Scan(int32(s), c)
			if gotNext != wantNext || gotNext >= 0 && gotSkip != wantSkip {
				return int32(s), c, true
			}
		}
	}
	return 0, 0, false
}

func TestDenseLexerAgreesWithRangeScan(t *testing.T) {
	for name, dfa := range newCoreLexers() {
		if s, c, bad := disagreement(&dfa); bad {
			t.Fatalf("%s: state %d lookahead %#x: dense and range scan differ", name, s, c)
		}
		t.Logf("%s: %d states, %d dense bytes", name, len(dfa.States), dfa.DenseBytes())
	}
}

func TestMovedClassBoundaryIsCaught(t *testing.T) {
	for name, dfa := range newCoreLexers() {
		classes, moved := dfa.Classes(), 0
		for b := 1; b < len(classes); b++ {
			if classes[b] == classes[b-1] {
				continue
			}
			for _, shift := range [][2]int{{b, b - 1}, {b - 1, b}} {
				original := classes[shift[0]]
				classes[shift[0]] = classes[shift[1]]
				if _, _, bad := disagreement(&dfa); !bad {
					t.Fatalf("%s: class boundary at byte %#x moved onto %#x is not caught", name, b, shift[0])
				}
				classes[shift[0]] = original
				moved++
			}
		}
		t.Logf("%s: %d one-byte boundary moves, each caught", name, moved)
	}
}
