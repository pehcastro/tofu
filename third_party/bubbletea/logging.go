package tea

import (
	"fmt"
	"io"
	"log"
	"os"
	"unicode"
)

func LogToFile(path string, prefix string) (*os.File, error) {
	return LogToFileWith(path, prefix, log.Default())
}

type LogOptionsSetter interface {
	SetOutput(io.Writer)
	SetPrefix(string)
}

func LogToFileWith(path string, prefix string, log LogOptionsSetter) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600) //nolint:mnd
	if err != nil {
		return nil, fmt.Errorf("error opening file for logging: %w", err)
	}
	log.SetOutput(f)

	if len(prefix) > 0 {
		finalChar := prefix[len(prefix)-1]
		if !unicode.IsSpace(rune(finalChar)) {
			prefix += " "
		}
	}
	log.SetPrefix(prefix)

	return f, nil
}
