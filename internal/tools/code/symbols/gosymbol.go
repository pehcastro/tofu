package symbols

import (
	"fmt"
	"go/parser"
	"go/token"
	"strings"
)

func goParse(shown, body string) error {
	_, err := parser.ParseFile(token.NewFileSet(), shown, body, parser.SkipObjectResolution)
	return err
}

func GoStillParses(shown, before, after string) error {
	if !strings.HasSuffix(shown, ".go") || goParse(shown, before) != nil {
		return nil
	}
	if err := goParse(shown, after); err != nil {
		return fmt.Errorf("edit: this edit would leave %s unable to parse as go: %w. nothing was written, and %s parses as it stands", shown, err, shown)
	}
	return nil
}
