package subagent

import (
	"slices"
	"strings"
)

func sedReads(args []shellWord) error {
	var scripts []string
	named := false
	next := func(i *int) string {
		if *i+1 < len(args) {
			*i++
			return args[*i].text
		}
		return ""
	}
	for i := 0; i < len(args); i++ {
		text := args[i].text
		switch {
		case text == "--file" || strings.HasPrefix(text, "--file="):
			return ReadListError{Program: "sed", Why: "reads its script from a file this check cannot see"}
		case text == "--expression":
			named, scripts = true, append(scripts, next(&i))
		case strings.HasPrefix(text, "--expression="):
			named, scripts = true, append(scripts, strings.TrimPrefix(text, "--expression="))
		case text == "--line-length":
			next(&i)
		case strings.HasPrefix(text, "--"):
		case strings.HasPrefix(text, "-") && len(text) > 1:
			for at := 1; at < len(text); at++ {
				switch text[at] {
				case 'f':
					return ReadListError{Program: "sed", Why: "reads its script from a file this check cannot see"}
				case 'e', 'l':
					value := text[at+1:]
					if value == "" {
						value = next(&i)
					}
					if text[at] == 'e' {
						named, scripts = true, append(scripts, value)
					}
					at = len(text)
				case 'i':
					at = len(text)
				}
			}
		case !named && len(scripts) == 0:
			scripts = append(scripts, text)
		}
	}
	if slices.ContainsFunc(scripts, sedWrites) {
		return ReadListError{Program: "sed", Why: "has a script that writes a file or runs a command (w, W or e)"}
	}
	return nil
}

func sedField(script string, at int, delimiter byte) int {
	for at < len(script) && script[at] != delimiter {
		if script[at] == '\\' {
			at++
		}
		at++
	}
	return min(at+1, len(script))
}

func sedWrites(script string) bool {
	for at := 0; at < len(script); {
		c := script[at]
		switch {
		case strings.IndexByte(" \t\r\n;{}!,~+0123456789$pPdDnNgGhHxlLqQz=FIM", c) >= 0:
			at++
		case c == '/':
			at = sedField(script, at+1, '/')
		case c == '\\' && at+1 < len(script):
			at = sedField(script, at+2, script[at+1])
		case (c == 's' || c == 'y') && at+1 < len(script) && script[at+1] != '\\' && script[at+1] != '\n':
			at = sedField(script, sedField(script, at+2, script[at+1]), script[at+1])
			end := at + strings.IndexAny(script[at:]+"\n", ";\n}")
			if c == 's' && strings.ContainsAny(script[at:end], "we") {
				return true
			}
			at = end
		case strings.IndexByte("aicrRbtT:", c) >= 0:
			at += strings.IndexAny(script[at:]+"\n", "\n;") + 1
		default:
			return true
		}
	}
	return false
}
