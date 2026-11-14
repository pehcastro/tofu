package subagents

import (
	"slices"
	"strings"
)

func (g Grammar) handed(wrapper string, words []shellWord) error {
	if len(words) == 0 {
		return nil
	}
	if err := bare(words[0]); err != nil {
		return err
	}
	tool, args := toolName(words[0].text), words[1:]
	inPlace := tool == "sed" && slices.ContainsFunc(args, func(arg shellWord) bool {
		return strings.HasPrefix(arg.text, "--in-place") || strings.HasPrefix(arg.text, "-") && !strings.HasPrefix(arg.text, "--") && strings.Contains(arg.text, "i")
	})
	if inPlace || rewrites(tool, args) || slices.Contains([]string{"tee", "rm", "rmdir", "unlink", "mkdir", "touch", "mv", "cp", "uniq", "tofu"}, tool) {
		return ReadListError{Program: wrapper + " " + tool, Why: "changes the files it is handed, which this check cannot see"}
	}
	runs, err := g.readOnly(tool, args)
	if err == nil && runs {
		return ReadListError{Program: wrapper + " " + tool, Why: "runs a check on paths it is handed, which this check cannot see: run the check by name"}
	}
	return err
}

func xargsCommand(args []shellWord) []shellWord {
	valued := []string{"-I", "-n", "-L", "-P", "-s", "-d", "-E", "-a", "--arg-file", "--delimiter", "--max-args", "--max-lines", "--max-procs", "--max-chars", "--replace", "--eof", "--process-slot-var"}
	for len(args) > 0 && strings.HasPrefix(args[0].text, "-") {
		switch {
		case args[0].text == "--":
			return args[1:]
		case slices.Contains(valued, args[0].text) && len(args) > 1:
			args = args[2:]
		default:
			args = args[1:]
		}
	}
	return args
}

func (g Grammar) findReads(args []shellWord) error {
	for i := 0; i < len(args); i++ {
		switch text := args[i].text; {
		case text == "-exec" || text == "-execdir":
			inner := args[i+1:]
			if end := slices.IndexFunc(inner, func(arg shellWord) bool { return arg.text == ";" || arg.text == "+" }); end >= 0 {
				inner = inner[:end]
			}
			if err := g.handed("find "+text, inner); err != nil {
				return err
			}
			i += len(inner)
		case slices.Contains(findActions(), text):
			return ReadListError{Program: "find", Why: "with " + text + " writes a file or runs another program"}
		}
	}
	return nil
}
