package session

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

const nameSeparator = "-"

const (
	nameQualities = "amber brisk calm clear crisp deft eager fair fond glad keen lucid mild plain quick quiet rapid ready solid spry stout sunny tidy tough vivid warm wise young zesty"
	nameHues      = "ashen azure birch brass cedar coral dusty flint frost grey inky ivory jade lilac linen maple moss olive onyx opal pearl rust sable sage sandy sepia slate tawny teal umber wheat"
	nameCreatures = "crane crow dove eagle egret finch gecko hare hawk heron ibis kite lark loon lynx mink mole moth newt otter pike quail raven robin seal shrew snipe stoat stork swan swift tern toad vole wren"
)

func newName() string {
	return pickWord(nameQualities) + nameSeparator + pickWord(nameHues) + nameSeparator + pickWord(nameCreatures)
}

func pickWord(list string) string {
	words := strings.Fields(list)
	return words[rand.IntN(len(words))]
}

func slugOf(given string) (string, error) {
	words := strings.FieldsFunc(strings.ToLower(given), func(letter rune) bool {
		return (letter < 'a' || letter > 'z') && (letter < '0' || letter > '9')
	})
	if len(words) == 0 {
		return "", fmt.Errorf("session: %q holds no letter and no digit, and a name is what a person types", given)
	}
	return strings.Join(words, nameSeparator), nil
}
