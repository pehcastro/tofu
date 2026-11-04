package output

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	cli "tofu/apps/cli/page"
)

const (
	ReportIndent  = "  "
	FactSeparator = " · "
)

type MarkNoun struct {
	Mark cli.Mark
	Noun string
}

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

func EditDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			substitution := previous[j-1]
			if a[i-1] != b[j-1] {
				substitution++
			}
			current[j] = min(previous[j]+1, current[j-1]+1, substitution)
		}
		previous = current
	}
	return previous[len(b)]
}

func PrintFailure(errOut io.Writer, code int, what, hint string) int {
	page := cli.Detect(errOut, os.Environ())
	_ = page.Print(errOut, page.ErrorLine(what, hint))
	return code
}

func NonZero(count int) string {
	if count == 0 {
		return ""
	}
	return strconv.Itoa(count)
}

func CountsSaid(counts map[cli.Mark]int, nouns []MarkNoun) string {
	var parts []string
	for _, each := range nouns {
		if counts[each.Mark] > 0 {
			parts = append(parts, strconv.Itoa(counts[each.Mark])+" "+each.Noun)
		}
	}
	return strings.Join(parts, FactSeparator)
}
