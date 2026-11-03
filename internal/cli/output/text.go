package output

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const ReportIndent = "  "

func RelativeToRoot(root, text string) string {
	return strings.ReplaceAll(text, root+string(os.PathSeparator), "")
}

func Plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(count) + " " + noun + "s"
}

func OneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func NextArg(args []string, i *int, flag string) (string, error) {
	*i++
	if *i >= len(args) {
		return "", fmt.Errorf("%s needs a value", flag)
	}
	return args[*i], nil
}

func VerbArgs(args []string) ([]string, bool, error) {
	var handles []string
	asJSON := false
	for _, arg := range args {
		switch {
		case arg == JSONFlag:
			asJSON = true
		case strings.HasPrefix(arg, "-"):
			return nil, false, fmt.Errorf("unknown argument %q", arg)
		default:
			handles = append(handles, arg)
		}
	}
	return handles, asJSON, nil
}
